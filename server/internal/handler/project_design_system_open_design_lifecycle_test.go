package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/opendesign"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	testOpenDesignDesignMD    = "# CRM Design System\n\n## Principles\n\nUse clear hierarchy and compact data presentation.\n"
	testOpenDesignTokensCSS   = ":root { --color-primary: #1677ff; } body { color: var(--color-primary); }"
	testOpenDesignUIKitHTML   = "<!doctype html><html><body><main data-design-node-id=\"crm-table\">CRM table</main><script src=\"./components/table.js\"></script></body></html>"
	testOpenDesignComponentJS = "window.CRMTable = { mounted: true };"
)

func TestFinalizeOpenDesignRunAcceptsAgentFailureBeforeWorkerRunStarts(t *testing.T) {
	previous := testHandler.cfg.OpenDesignEnabled
	testHandler.cfg.OpenDesignEnabled = true
	t.Cleanup(func() { testHandler.cfg.OpenDesignEnabled = previous })

	projectID := createProjectForDesignTest(t, "Open Design pre-run failure")
	agentID, runtimeID := createProjectDesignSystemAgent(t, "online")
	// Since MUL-8154 a daemon token reaches only runtimes carrying its own
	// daemon_id, so bind the runtime to the callback token used below.
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_runtime SET daemon_id = 'project-design-system-test' WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("bind Open Design runtime daemon id: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_runtime SET provider = 'opencode' WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("configure Open Design runtime: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE agent SET model = 'anthropic/claude-sonnet-4-5' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("configure Open Design agent: %v", err)
	}

	response := performProjectDesignSystemRequest(t, testHandler.CreateProjectDesignSystem, http.MethodPost, "/api/project-design-systems", map[string]any{
		"project_id": projectID,
		"agent_id":   agentID,
		"platform":   "web",
		"brief":      "Create a source-grounded CRM design system.",
	})
	if response.Code != http.StatusAccepted {
		t.Fatalf("CreateProjectDesignSystem: status = %d, body = %s", response.Code, response.Body.String())
	}
	var created ProjectDesignSystemResponse
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.ActiveTask == nil {
		t.Fatal("create response has no active task")
	}
	seedHistoricalOpenDesignRun(t, projectID, created.ID, created.ActiveTask.ID, agentID)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE open_design_run
		SET status = 'ready', preflight = '{"status":"ready"}'::jsonb
		WHERE task_id = $1
	`, created.ActiveTask.ID); err != nil {
		t.Fatalf("prepare ready Open Design run: %v", err)
	}

	failure := json.RawMessage(`{"code":"open_design_workspace_prepare_failed","message":"scratch import failed"}`)
	path := "/api/daemon/tasks/" + created.ActiveTask.ID + "/open-design/terminal"
	for attempt := 0; attempt < 2; attempt++ {
		terminalResponse := performOpenDesignDaemonCallbackForTest(t, testHandler.FinalizeOpenDesignRun, created.ActiveTask.ID, path, opendesign.RunTerminalRequest{
			Status:  opendesign.RunStatusAgentFailed,
			Failure: failure,
		})
		if terminalResponse.Code != http.StatusOK {
			t.Fatalf("FinalizeOpenDesignRun attempt %d: status = %d, body = %s", attempt+1, terminalResponse.Code, terminalResponse.Body.String())
		}
	}

	var (
		status           string
		openDesignRunID  pgtype.Text
		persistedFailure []byte
		finishedAt       pgtype.Timestamptz
	)
	if err := testPool.QueryRow(context.Background(), `
		SELECT status, open_design_run_id, failure, finished_at
		FROM open_design_run
		WHERE task_id = $1
	`, created.ActiveTask.ID).Scan(&status, &openDesignRunID, &persistedFailure, &finishedAt); err != nil {
		t.Fatalf("load pre-run failure: %v", err)
	}
	if status != string(opendesign.RunStatusAgentFailed) || openDesignRunID.Valid || !finishedAt.Valid {
		t.Fatalf("pre-run failure = status:%q run_id:%+v finished_at:%+v", status, openDesignRunID, finishedAt)
	}
	assertJSONEqual(t, persistedFailure, string(failure))
}

func TestRecordOpenDesignRunAuditPersistsDigestBoundPassingReceiptIdempotently(t *testing.T) {
	taskID, runID, contentDigest := prepareOpenDesignRunForAuditTest(t, "Open Design passing audit")
	receipt := opendesign.PackageAuditReceipt{
		Schema:        opendesign.PackageAuditReceiptSchema,
		Engine:        opendesign.PinnedEngineIdentity(),
		ContentDigest: contentDigest,
		Audit: opendesign.PackageAudit{
			OK:             true,
			FilesInspected: 797,
			Errors:         []opendesign.PackageAuditIssue{},
			Warnings:       []opendesign.PackageAuditIssue{},
		},
	}
	auditPath := "/api/daemon/tasks/" + taskID + "/open-design/audit"

	mismatched := receipt
	mismatched.ContentDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	mismatchResponse := performOpenDesignDaemonCallbackForTest(t, testHandler.RecordOpenDesignRunAudit, taskID, auditPath, opendesign.RunAuditRequest{
		OpenDesignRunID: runID,
		AuditReport:     mismatched,
	})
	assertProjectDesignSystemErrorCode(t, mismatchResponse, http.StatusConflict, "open_design_audit_conflict")

	requestBody := opendesign.RunAuditRequest{OpenDesignRunID: runID, AuditReport: receipt}
	for attempt := 0; attempt < 2; attempt++ {
		response := performOpenDesignDaemonCallbackForTest(t, testHandler.RecordOpenDesignRunAudit, taskID, auditPath, requestBody)
		if response.Code != http.StatusOK {
			t.Fatalf("RecordOpenDesignRunAudit attempt %d: status = %d, body = %s", attempt+1, response.Code, response.Body.String())
		}
	}

	var (
		status      string
		auditReport []byte
		failure     []byte
		finishedAt  pgtype.Timestamptz
	)
	if err := testPool.QueryRow(context.Background(), `
		SELECT status, audit_report, failure, finished_at
		FROM open_design_run
		WHERE task_id = $1
	`, taskID).Scan(&status, &auditReport, &failure, &finishedAt); err != nil {
		t.Fatalf("load passing Open Design audit: %v", err)
	}
	if status != string(opendesign.RunStatusRunSucceeded) || finishedAt.Valid {
		t.Fatalf("passing Open Design audit = status:%q finished_at:%+v", status, finishedAt)
	}
	expectedReport, err := json.Marshal(receipt)
	if err != nil {
		t.Fatalf("marshal passing audit receipt: %v", err)
	}
	assertJSONEqual(t, auditReport, string(expectedReport))
	assertJSONEqual(t, failure, `{}`)

	conflicting := receipt
	conflicting.Audit.FilesInspected++
	conflictResponse := performOpenDesignDaemonCallbackForTest(t, testHandler.RecordOpenDesignRunAudit, taskID, auditPath, opendesign.RunAuditRequest{
		OpenDesignRunID: runID,
		AuditReport:     conflicting,
	})
	assertProjectDesignSystemErrorCode(t, conflictResponse, http.StatusConflict, "open_design_audit_conflict")

	terminalPath := "/api/daemon/tasks/" + taskID + "/open-design/terminal"
	terminalConflict := performOpenDesignDaemonCallbackForTest(t, testHandler.FinalizeOpenDesignRun, taskID, terminalPath, opendesign.RunTerminalRequest{
		OpenDesignRunID: runID,
		Status:          opendesign.RunStatusAuditFailed,
		Failure:         json.RawMessage(`{"code":"open_design_package_audit_callback_failed","message":"late conflicting callback"}`),
	})
	assertProjectDesignSystemErrorCode(t, terminalConflict, http.StatusConflict, "open_design_terminal_conflict")
}

func TestRecordOpenDesignRunAuditAtomicallyPersistsRejectedReportAndTerminalState(t *testing.T) {
	taskID, runID, contentDigest := prepareOpenDesignRunForAuditTest(t, "Open Design rejected audit")
	receipt := opendesign.PackageAuditReceipt{
		Schema:        opendesign.PackageAuditReceiptSchema,
		Engine:        opendesign.PinnedEngineIdentity(),
		ContentDigest: contentDigest,
		Audit: opendesign.PackageAudit{
			OK:             false,
			FilesInspected: 796,
			Errors: []opendesign.PackageAuditIssue{{
				Severity: "error",
				Code:     "missing_required_file",
				Message:  "DESIGN.md is required as the canonical system rules",
				Path:     "DESIGN.md",
			}},
			Warnings: []opendesign.PackageAuditIssue{},
		},
	}
	auditPath := "/api/daemon/tasks/" + taskID + "/open-design/audit"
	requestBody := opendesign.RunAuditRequest{OpenDesignRunID: runID, AuditReport: receipt}
	for attempt := 0; attempt < 2; attempt++ {
		response := performOpenDesignDaemonCallbackForTest(t, testHandler.RecordOpenDesignRunAudit, taskID, auditPath, requestBody)
		if response.Code != http.StatusOK {
			t.Fatalf("RecordOpenDesignRunAudit attempt %d: status = %d, body = %s", attempt+1, response.Code, response.Body.String())
		}
	}

	var (
		status      string
		auditReport []byte
		failure     []byte
		finishedAt  pgtype.Timestamptz
	)
	if err := testPool.QueryRow(context.Background(), `
		SELECT status, audit_report, failure, finished_at
		FROM open_design_run
		WHERE task_id = $1
	`, taskID).Scan(&status, &auditReport, &failure, &finishedAt); err != nil {
		t.Fatalf("load rejected Open Design audit: %v", err)
	}
	if status != string(opendesign.RunStatusAuditFailed) || !finishedAt.Valid {
		t.Fatalf("rejected Open Design audit = status:%q finished_at:%+v", status, finishedAt)
	}
	expectedReport, err := json.Marshal(receipt)
	if err != nil {
		t.Fatalf("marshal rejected audit receipt: %v", err)
	}
	assertJSONEqual(t, auditReport, string(expectedReport))
	var persistedFailure struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(failure, &persistedFailure); err != nil {
		t.Fatalf("decode rejected audit failure: %v", err)
	}
	if persistedFailure.Code != "open_design_package_audit_failed" {
		t.Fatalf("rejected audit failure = %s", failure)
	}

	conflicting := receipt
	conflicting.Audit.Errors[0].Message = "different report"
	conflictResponse := performOpenDesignDaemonCallbackForTest(t, testHandler.RecordOpenDesignRunAudit, taskID, auditPath, opendesign.RunAuditRequest{
		OpenDesignRunID: runID,
		AuditReport:     conflicting,
	})
	assertProjectDesignSystemErrorCode(t, conflictResponse, http.StatusConflict, "open_design_audit_conflict")
}

func TestRecordOpenDesignRunAuditAcceptsWarningOnlyStrictReceipt(t *testing.T) {
	taskID, runID, contentDigest := prepareOpenDesignRunForAuditTest(t, "Open Design warning-only audit")
	receipt := opendesign.PackageAuditReceipt{
		Schema:        opendesign.PackageAuditReceiptSchema,
		Engine:        opendesign.PinnedEngineIdentity(),
		ContentDigest: contentDigest,
		Audit: opendesign.PackageAudit{
			OK:             false,
			FilesInspected: 39,
			Errors:         []opendesign.PackageAuditIssue{},
			Warnings: []opendesign.PackageAuditIssue{{
				Severity: "warning",
				Code:     "readme_missing_package_reuse_guide",
				Message:  "README.md needs a concrete package reuse guide",
				Path:     "README.md",
			}},
		},
	}
	response := performOpenDesignDaemonCallbackForTest(
		t,
		testHandler.RecordOpenDesignRunAudit,
		taskID,
		"/api/daemon/tasks/"+taskID+"/open-design/audit",
		opendesign.RunAuditRequest{OpenDesignRunID: runID, AuditReport: receipt},
	)
	if response.Code != http.StatusOK {
		t.Fatalf("RecordOpenDesignRunAudit warning-only receipt: status = %d, body = %s", response.Code, response.Body.String())
	}

	var (
		status      string
		auditReport []byte
		failure     []byte
		finishedAt  pgtype.Timestamptz
	)
	if err := testPool.QueryRow(context.Background(), `
		SELECT status, audit_report, failure, finished_at
		FROM open_design_run
		WHERE task_id = $1
	`, taskID).Scan(&status, &auditReport, &failure, &finishedAt); err != nil {
		t.Fatalf("load warning-only Open Design audit: %v", err)
	}
	if status != string(opendesign.RunStatusAuditFailed) || !finishedAt.Valid {
		t.Fatalf("warning-only audit = status:%q finished_at:%+v", status, finishedAt)
	}
	expectedReport, err := json.Marshal(receipt)
	if err != nil {
		t.Fatalf("marshal warning-only audit receipt: %v", err)
	}
	assertJSONEqual(t, auditReport, string(expectedReport))
	var persistedFailure struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(failure, &persistedFailure); err != nil {
		t.Fatalf("decode warning-only audit failure: %v", err)
	}
	if persistedFailure.Code != "open_design_package_audit_failed" || !strings.Contains(persistedFailure.Message, "1 warning(s)") {
		t.Fatalf("warning-only audit failure = %s", failure)
	}
}

func TestRecordOpenDesignRunPreviewCompletesTaskAndRunIdempotently(t *testing.T) {
	taskID, runID, contentDigest := prepareOpenDesignRunForPreviewTest(t, "Open Design passing Preview")
	var designSystemID pgtype.UUID
	if err := testPool.QueryRow(context.Background(), `
		SELECT design_system_id
		FROM open_design_run
		WHERE task_id = $1
	`, taskID).Scan(&designSystemID); err != nil {
		t.Fatalf("load Open Design system: %v", err)
	}
	upsertProjectDesignSystemPackageForTest(t, db.New(testPool), designSystemID, "saved", "saved-before-open-design", "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd")
	upsertProjectDesignSystemPackageForTest(t, db.New(testPool), designSystemID, "draft", "stale-draft-before-open-design", "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")
	savedBefore := getProjectDesignSystemPackageForTest(t, designSystemID, "saved")
	receipt := validOpenDesignPreviewReceipt(t, contentDigest, true)
	previewPath := "/api/daemon/tasks/" + taskID + "/open-design/preview"

	mismatchedDigest := receipt
	mismatchedDigest.ContentDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	digestResponse := performOpenDesignDaemonCallbackForTest(t, testHandler.RecordOpenDesignRunPreview, taskID, previewPath, opendesign.RunPreviewRequest{
		OpenDesignRunID: runID,
		PreviewReceipt:  mismatchedDigest,
	})
	assertProjectDesignSystemErrorCode(t, digestResponse, http.StatusConflict, "open_design_preview_conflict")

	mismatchedEngine := receipt
	mismatchedEngine.Engine.Release = "open-design-v0.16.1-other"
	engineResponse := performOpenDesignDaemonCallbackForTest(t, testHandler.RecordOpenDesignRunPreview, taskID, previewPath, opendesign.RunPreviewRequest{
		OpenDesignRunID: runID,
		PreviewReceipt:  mismatchedEngine,
	})
	assertProjectDesignSystemErrorCode(t, engineResponse, http.StatusConflict, "open_design_preview_conflict")

	requestBody := opendesign.RunPreviewRequest{OpenDesignRunID: runID, PreviewReceipt: receipt}
	for attempt := 0; attempt < 2; attempt++ {
		response := performOpenDesignDaemonCallbackForTest(t, testHandler.RecordOpenDesignRunPreview, taskID, previewPath, requestBody)
		if response.Code != http.StatusOK {
			t.Fatalf("RecordOpenDesignRunPreview attempt %d: status = %d, body = %s", attempt+1, response.Code, response.Body.String())
		}
	}

	var (
		taskStatus     string
		runStatus      string
		previewReceipt []byte
		failure        []byte
		finishedAt     pgtype.Timestamptz
	)
	if err := testPool.QueryRow(context.Background(), `
		SELECT task.status, run.status, run.preview_receipt, run.failure, run.finished_at
		FROM agent_task_queue task
		JOIN open_design_run run ON run.task_id = task.id
		WHERE task.id = $1
	`, taskID).Scan(&taskStatus, &runStatus, &previewReceipt, &failure, &finishedAt); err != nil {
		t.Fatalf("load successful Open Design Preview: %v", err)
	}
	if taskStatus != "completed" || runStatus != string(opendesign.RunStatusSucceeded) || !finishedAt.Valid {
		t.Fatalf("successful Preview = task:%q run:%q finished_at:%+v", taskStatus, runStatus, finishedAt)
	}
	expectedReceipt, err := json.Marshal(receipt)
	if err != nil {
		t.Fatalf("marshal successful Preview receipt: %v", err)
	}
	assertJSONEqual(t, previewReceipt, string(expectedReceipt))
	assertJSONEqual(t, failure, `{}`)

	draft := getProjectDesignSystemPackageForTest(t, designSystemID, "draft")
	if draft.DesignMd != testOpenDesignDesignMD || draft.TokensCss != testOpenDesignTokensCSS || draft.ComponentsHtml != testOpenDesignUIKitHTML {
		t.Fatalf("Open Design draft compatibility mirrors do not match archive sources: %+v", draft)
	}
	if draft.RenderStatus != "passed" || uuidToString(draft.SourceTaskID) != taskID {
		t.Fatalf("Open Design draft gate metadata = render:%q source_task:%q", draft.RenderStatus, uuidToString(draft.SourceTaskID))
	}
	if draft.IntegritySha256 != contentDigest[len("sha256:"):] {
		t.Fatalf("Open Design draft digest = %q, want %q", draft.IntegritySha256, contentDigest[len("sha256:"):])
	}
	var manifest struct {
		Schema        string          `json:"schema"`
		Format        string          `json:"format"`
		ResultPackage json.RawMessage `json:"result_package"`
		Archive       struct {
			ObjectKey     string `json:"object_key"`
			ContentDigest string `json:"content_digest"`
		} `json:"archive"`
	}
	if err := json.Unmarshal(draft.Manifest, &manifest); err != nil {
		t.Fatalf("decode Open Design draft manifest: %v", err)
	}
	if manifest.Schema != "multica.open-design-draft-package/v1" || manifest.Format != "open-design-project-archive" || manifest.Archive.ContentDigest != contentDigest || len(manifest.ResultPackage) == 0 {
		t.Fatalf("Open Design draft manifest is incomplete: %s", draft.Manifest)
	}
	var validation struct {
		Schema  string          `json:"schema"`
		Passed  bool            `json:"passed"`
		Audit   json.RawMessage `json:"audit"`
		Preview json.RawMessage `json:"preview"`
	}
	if err := json.Unmarshal(draft.Validation, &validation); err != nil {
		t.Fatalf("decode Open Design draft validation: %v", err)
	}
	if validation.Schema != "multica.open-design-draft-validation/v1" || !validation.Passed || len(validation.Audit) == 0 || len(validation.Preview) == 0 {
		t.Fatalf("Open Design draft validation is incomplete: %s", draft.Validation)
	}
	savedAfter := getProjectDesignSystemPackageForTest(t, designSystemID, "saved")
	if !reflect.DeepEqual(savedAfter, savedBefore) {
		t.Fatalf("saved package changed while Open Design draft was persisted:\nbefore: %+v\nafter:  %+v", savedBefore, savedAfter)
	}
	var activeTaskID pgtype.UUID
	if err := testPool.QueryRow(context.Background(), `SELECT active_task_id FROM project_design_system WHERE id = $1`, designSystemID).Scan(&activeTaskID); err != nil {
		t.Fatalf("load Open Design active task: %v", err)
	}
	if activeTaskID.Valid {
		t.Fatalf("Open Design active task was not cleared: %s", uuidToString(activeTaskID))
	}

	conflicting := receipt
	conflicting.Verification.Browser.Version = "different-browser-version"
	conflictResponse := performOpenDesignDaemonCallbackForTest(t, testHandler.RecordOpenDesignRunPreview, taskID, previewPath, opendesign.RunPreviewRequest{
		OpenDesignRunID: runID,
		PreviewReceipt:  conflicting,
	})
	assertProjectDesignSystemErrorCode(t, conflictResponse, http.StatusConflict, "open_design_preview_conflict")
}

func TestRecordOpenDesignRunPreviewDoesNotCompleteWithoutStoredArchive(t *testing.T) {
	taskID, runID, contentDigest := prepareOpenDesignRunForPreviewTest(t, "Open Design missing stored archive")
	var (
		designSystemID   pgtype.UUID
		archiveObjectKey string
	)
	if err := testPool.QueryRow(context.Background(), `
		SELECT design_system_id, archive_object_key
		FROM open_design_run
		WHERE task_id = $1
	`, taskID).Scan(&designSystemID, &archiveObjectKey); err != nil {
		t.Fatalf("load Open Design archive identity: %v", err)
	}
	store, ok := testHandler.Storage.(*mockStorage)
	if !ok {
		t.Fatalf("Open Design test storage = %T, want *mockStorage", testHandler.Storage)
	}
	store.Delete(context.Background(), archiveObjectKey)

	response := performOpenDesignDaemonCallbackForTest(t, testHandler.RecordOpenDesignRunPreview, taskID, "/api/daemon/tasks/"+taskID+"/open-design/preview", opendesign.RunPreviewRequest{
		OpenDesignRunID: runID,
		PreviewReceipt:  validOpenDesignPreviewReceipt(t, contentDigest, true),
	})
	assertProjectDesignSystemErrorCode(t, response, http.StatusServiceUnavailable, "open_design_draft_archive_unavailable")

	var (
		taskStatus     string
		runStatus      string
		previewReceipt []byte
		activeTaskID   pgtype.UUID
		draftCount     int
	)
	if err := testPool.QueryRow(context.Background(), `
		SELECT task.status,
		       run.status,
		       COALESCE(run.preview_receipt, 'null'::jsonb),
		       system.active_task_id,
		       (SELECT count(*) FROM project_design_system_package package WHERE package.design_system_id = system.id AND package.slot = 'draft')
		FROM agent_task_queue task
		JOIN open_design_run run ON run.task_id = task.id
		JOIN project_design_system system ON system.id = run.design_system_id
		WHERE task.id = $1
	`, taskID).Scan(&taskStatus, &runStatus, &previewReceipt, &activeTaskID, &draftCount); err != nil {
		t.Fatalf("load missing-archive gate state: %v", err)
	}
	if taskStatus != "running" || runStatus != string(opendesign.RunStatusRunSucceeded) || uuidToString(activeTaskID) != taskID || draftCount != 0 {
		t.Fatalf("missing-archive gate state = task:%q run:%q active:%q drafts:%d", taskStatus, runStatus, uuidToString(activeTaskID), draftCount)
	}
	assertJSONEqual(t, previewReceipt, `null`)
}

func TestRecordOpenDesignRunPreviewRollsBackWhenActiveTaskChanged(t *testing.T) {
	taskID, runID, contentDigest := prepareOpenDesignRunForPreviewTest(t, "Open Design active task conflict")
	if _, err := testPool.Exec(context.Background(), `
		UPDATE project_design_system system
		SET active_task_id = NULL,
		    active_operation = NULL
		FROM open_design_run run
		WHERE run.task_id = $1
		  AND system.id = run.design_system_id
	`, taskID); err != nil {
		t.Fatalf("change Open Design active task: %v", err)
	}

	response := performOpenDesignDaemonCallbackForTest(t, testHandler.RecordOpenDesignRunPreview, taskID, "/api/daemon/tasks/"+taskID+"/open-design/preview", opendesign.RunPreviewRequest{
		OpenDesignRunID: runID,
		PreviewReceipt:  validOpenDesignPreviewReceipt(t, contentDigest, true),
	})
	assertProjectDesignSystemErrorCode(t, response, http.StatusConflict, "open_design_preview_conflict")

	var (
		taskStatus     string
		runStatus      string
		previewReceipt []byte
		draftCount     int
	)
	if err := testPool.QueryRow(context.Background(), `
		SELECT task.status,
		       run.status,
		       COALESCE(run.preview_receipt, 'null'::jsonb),
		       (SELECT count(*) FROM project_design_system_package package WHERE package.design_system_id = run.design_system_id AND package.slot = 'draft')
		FROM agent_task_queue task
		JOIN open_design_run run ON run.task_id = task.id
		WHERE task.id = $1
	`, taskID).Scan(&taskStatus, &runStatus, &previewReceipt, &draftCount); err != nil {
		t.Fatalf("load active-task conflict state: %v", err)
	}
	if taskStatus != "running" || runStatus != string(opendesign.RunStatusRunSucceeded) || draftCount != 0 {
		t.Fatalf("active-task conflict state = task:%q run:%q drafts:%d", taskStatus, runStatus, draftCount)
	}
	assertJSONEqual(t, previewReceipt, `null`)
}

func TestRecordOpenDesignRunPreviewPersistsRejectedReceiptAndTerminalState(t *testing.T) {
	taskID, runID, contentDigest := prepareOpenDesignRunForPreviewTest(t, "Open Design rejected Preview")
	receipt := validOpenDesignPreviewReceipt(t, contentDigest, false)
	previewPath := "/api/daemon/tasks/" + taskID + "/open-design/preview"
	requestBody := opendesign.RunPreviewRequest{OpenDesignRunID: runID, PreviewReceipt: receipt}

	for attempt := 0; attempt < 2; attempt++ {
		response := performOpenDesignDaemonCallbackForTest(t, testHandler.RecordOpenDesignRunPreview, taskID, previewPath, requestBody)
		if response.Code != http.StatusOK {
			t.Fatalf("RecordOpenDesignRunPreview attempt %d: status = %d, body = %s", attempt+1, response.Code, response.Body.String())
		}
	}

	var (
		taskStatus     string
		runStatus      string
		previewReceipt []byte
		failure        []byte
		finishedAt     pgtype.Timestamptz
	)
	if err := testPool.QueryRow(context.Background(), `
		SELECT task.status, run.status, run.preview_receipt, run.failure, run.finished_at
		FROM agent_task_queue task
		JOIN open_design_run run ON run.task_id = task.id
		WHERE task.id = $1
	`, taskID).Scan(&taskStatus, &runStatus, &previewReceipt, &failure, &finishedAt); err != nil {
		t.Fatalf("load rejected Open Design Preview: %v", err)
	}
	if taskStatus != "running" || runStatus != string(opendesign.RunStatusPreviewFailed) || !finishedAt.Valid {
		t.Fatalf("rejected Preview = task:%q run:%q finished_at:%+v", taskStatus, runStatus, finishedAt)
	}
	expectedReceipt, err := json.Marshal(receipt)
	if err != nil {
		t.Fatalf("marshal rejected Preview receipt: %v", err)
	}
	assertJSONEqual(t, previewReceipt, string(expectedReceipt))
	var persistedFailure struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(failure, &persistedFailure); err != nil {
		t.Fatalf("decode rejected Preview failure: %v", err)
	}
	if persistedFailure.Code != "open_design_preview_failed" {
		t.Fatalf("rejected Preview failure = %s", failure)
	}

	conflicting := receipt
	conflicting.Verification.Browser.Version = "different-browser-version"
	conflictResponse := performOpenDesignDaemonCallbackForTest(t, testHandler.RecordOpenDesignRunPreview, taskID, previewPath, opendesign.RunPreviewRequest{
		OpenDesignRunID: runID,
		PreviewReceipt:  conflicting,
	})
	assertProjectDesignSystemErrorCode(t, conflictResponse, http.StatusConflict, "open_design_preview_conflict")
}

func TestRecordOpenDesignRunPreviewRequiresPassingAudit(t *testing.T) {
	tests := []struct {
		name         string
		persistAudit bool
	}{
		{name: "missing audit"},
		{name: "rejected audit", persistAudit: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taskID, runID, contentDigest := prepareOpenDesignRunForAuditTest(t, "Open Design Preview without passing audit")
			if _, err := testPool.Exec(context.Background(), `
				UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1
			`, taskID); err != nil {
				t.Fatalf("prepare running Open Design task: %v", err)
			}
			if tt.persistAudit {
				auditReceipt := validOpenDesignAuditReceipt(contentDigest, false)
				auditResponse := performOpenDesignDaemonCallbackForTest(t, testHandler.RecordOpenDesignRunAudit, taskID, "/api/daemon/tasks/"+taskID+"/open-design/audit", opendesign.RunAuditRequest{
					OpenDesignRunID: runID,
					AuditReport:     auditReceipt,
				})
				if auditResponse.Code != http.StatusOK {
					t.Fatalf("RecordOpenDesignRunAudit: status = %d, body = %s", auditResponse.Code, auditResponse.Body.String())
				}
			}

			previewResponse := performOpenDesignDaemonCallbackForTest(t, testHandler.RecordOpenDesignRunPreview, taskID, "/api/daemon/tasks/"+taskID+"/open-design/preview", opendesign.RunPreviewRequest{
				OpenDesignRunID: runID,
				PreviewReceipt:  validOpenDesignPreviewReceipt(t, contentDigest, true),
			})
			assertProjectDesignSystemErrorCode(t, previewResponse, http.StatusConflict, "open_design_preview_conflict")

			var taskStatus string
			var previewReceipt []byte
			if err := testPool.QueryRow(context.Background(), `
				SELECT task.status, COALESCE(run.preview_receipt, 'null'::jsonb)
				FROM agent_task_queue task
				JOIN open_design_run run ON run.task_id = task.id
				WHERE task.id = $1
			`, taskID).Scan(&taskStatus, &previewReceipt); err != nil {
				t.Fatalf("load rejected Preview precondition state: %v", err)
			}
			if taskStatus != "running" {
				t.Fatalf("task status after rejected Preview = %q, want running", taskStatus)
			}
			assertJSONEqual(t, previewReceipt, `null`)
		})
	}
}

func prepareOpenDesignRunForPreviewTest(t *testing.T, name string) (string, string, string) {
	t.Helper()
	taskID, runID, contentDigest := prepareOpenDesignRunForAuditTest(t, name)
	auditResponse := performOpenDesignDaemonCallbackForTest(t, testHandler.RecordOpenDesignRunAudit, taskID, "/api/daemon/tasks/"+taskID+"/open-design/audit", opendesign.RunAuditRequest{
		OpenDesignRunID: runID,
		AuditReport:     validOpenDesignAuditReceipt(contentDigest, true),
	})
	if auditResponse.Code != http.StatusOK {
		t.Fatalf("RecordOpenDesignRunAudit: status = %d, body = %s", auditResponse.Code, auditResponse.Body.String())
	}
	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1
	`, taskID); err != nil {
		t.Fatalf("prepare running Open Design task: %v", err)
	}
	return taskID, runID, contentDigest
}

func validOpenDesignAuditReceipt(contentDigest string, passed bool) opendesign.PackageAuditReceipt {
	receipt := opendesign.PackageAuditReceipt{
		Schema:        opendesign.PackageAuditReceiptSchema,
		Engine:        opendesign.PinnedEngineIdentity(),
		ContentDigest: contentDigest,
		Audit: opendesign.PackageAudit{
			OK:             passed,
			FilesInspected: 797,
			Errors:         []opendesign.PackageAuditIssue{},
			Warnings:       []opendesign.PackageAuditIssue{},
		},
	}
	if !passed {
		receipt.Audit.Errors = []opendesign.PackageAuditIssue{{
			Severity: "error",
			Code:     "missing_required_file",
			Message:  "DESIGN.md is required",
			Path:     "DESIGN.md",
		}}
	}
	return receipt
}

func validOpenDesignPreviewReceipt(t *testing.T, contentDigest string, passed bool) opendesign.PreviewVerificationReceipt {
	t.Helper()
	target := opendesign.PreviewTarget{Kind: opendesign.PreviewTargetKindUIKit, ID: "app", Path: "ui_kits/app/index.html"}
	capture := opendesign.PreviewCapture{
		Target:                    target,
		DocumentLoaded:            true,
		DOMPresent:                true,
		ComputedVisibilityVisible: true,
		RenderedElementCount:      24,
		VisibleTextLength:         180,
		BodyWidth:                 1440,
		BodyHeight:                1000,
		Screenshot: opendesign.PreviewScreenshot{
			SHA256:           "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			Bytes:            4096,
			Width:            1440,
			Height:           1000,
			Entropy:          4.2,
			MaxChannelStddev: 22,
		},
	}
	if !passed {
		capture.Screenshot.Entropy = 0
		capture.Screenshot.MaxChannelStddev = 0
	}
	targetVerification := opendesign.EvaluatePreviewCapture(capture, opendesign.PinnedPreviewVerificationPolicy())
	verification := opendesign.PreviewVerification{
		Browser: opendesign.PreviewBrowserIdentity{Name: "Chromium", Version: "150.0.0.0"},
		Policy:  opendesign.PinnedPreviewVerificationPolicy(),
		Targets: []opendesign.PreviewTargetVerification{targetVerification},
		Passed:  targetVerification.Passed,
	}
	receipt, err := opendesign.NewPreviewVerificationReceipt(opendesign.PinnedEngineIdentity(), contentDigest, verification)
	if err != nil {
		t.Fatalf("NewPreviewVerificationReceipt: %v", err)
	}
	return receipt
}

func prepareOpenDesignRunForAuditTest(t *testing.T, name string) (string, string, string) {
	t.Helper()
	previous := testHandler.cfg.OpenDesignEnabled
	testHandler.cfg.OpenDesignEnabled = true
	t.Cleanup(func() { testHandler.cfg.OpenDesignEnabled = previous })

	projectID := createProjectForDesignTest(t, name)
	agentID, runtimeID := createProjectDesignSystemAgent(t, "online")
	// Since MUL-8154 a daemon token reaches only runtimes carrying its own
	// daemon_id, so bind the runtime to the callback token used below.
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_runtime SET daemon_id = 'project-design-system-test' WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("bind Open Design runtime daemon id: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_runtime SET provider = 'opencode' WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("configure Open Design runtime: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE agent SET model = 'anthropic/claude-sonnet-4-5' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("configure Open Design agent: %v", err)
	}
	response := performProjectDesignSystemRequest(t, testHandler.CreateProjectDesignSystem, http.MethodPost, "/api/project-design-systems", map[string]any{
		"project_id": projectID,
		"agent_id":   agentID,
		"platform":   "web",
		"brief":      "Create a source-grounded CRM design system.",
	})
	if response.Code != http.StatusAccepted {
		t.Fatalf("CreateProjectDesignSystem: status = %d, body = %s", response.Code, response.Body.String())
	}
	var created ProjectDesignSystemResponse
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.ActiveTask == nil {
		t.Fatal("create response has no active task")
	}
	seedHistoricalOpenDesignRun(t, projectID, created.ID, created.ActiveTask.ID, agentID)
	runID := "11111111-1111-4111-8111-111111111111"
	archive, resultPackage, artifactIndex, contentDigest := openDesignDraftArchiveFixture(t, runID)
	archiveObjectKey := "workspaces/test/open-design-package.zip"
	previousStorage := testHandler.Storage
	store := &mockStorage{}
	testHandler.Storage = store
	t.Cleanup(func() { testHandler.Storage = previousStorage })
	if _, err := store.Upload(context.Background(), archiveObjectKey, archive, opendesign.RunArchiveContentType, "open-design-package.zip"); err != nil {
		t.Fatalf("seed Open Design archive: %v", err)
	}
	artifactIndexJSON, err := json.Marshal(artifactIndex)
	if err != nil {
		t.Fatalf("marshal Open Design artifact index: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		UPDATE open_design_run
		SET status = 'run_succeeded',
			open_design_run_id = $2,
			result_package = $3::jsonb,
			artifact_index = $4::jsonb,
			archive_object_key = $5,
			content_digest = $6,
			started_at = now()
		WHERE task_id = $1
	`, created.ActiveTask.ID, runID, resultPackage, artifactIndexJSON, archiveObjectKey, contentDigest); err != nil {
		t.Fatalf("prepare run-succeeded Open Design row: %v", err)
	}
	return created.ActiveTask.ID, runID, contentDigest
}

func seedHistoricalOpenDesignRun(t *testing.T, projectID, designSystemID, taskID, agentID string) {
	t.Helper()
	identity := opendesign.PinnedEngineIdentity()
	var supervisorRunID string
	if err := testPool.QueryRow(context.Background(), `SELECT gen_random_uuid()`).Scan(&supervisorRunID); err != nil {
		t.Fatalf("create historical Open Design run ID: %v", err)
	}
	contextJSON, err := json.Marshal(map[string]any{
		"schema": opendesign.RunSchema,
		"run_id": supervisorRunID,
		"engine": identity,
		"agent":  map[string]any{"multica_agent_id": agentID, "adapter_id": "opencode"},
	})
	if err != nil {
		t.Fatalf("marshal historical Open Design context: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent_task_queue
		SET context = jsonb_set(context, '{open_design_run}', $2::jsonb)
		WHERE id = $1
	`, taskID, contextJSON); err != nil {
		t.Fatalf("seed historical Open Design task context: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO open_design_run (
			id, workspace_id, project_id, design_system_id, task_id, operation, status,
			engine_release, engine_commit, engine_lockfile_sha256, engine_dist_sha256,
			agent_id, agent_snapshot, adapter_id, input_snapshot, workspace_provenance
		) VALUES (
			$1, $2, $3, $4, $5, 'generate', 'preflight_pending',
			$6, $7, $8, $9,
			$10, $11::jsonb, 'opencode', $12::jsonb, $13::jsonb
		)
	`, supervisorRunID, testWorkspaceID, projectID, designSystemID, taskID,
		identity.Release, identity.Commit, identity.LockfileSHA256, identity.DistSHA256,
		agentID, `{"multica_agent_id":"`+agentID+`","adapter_id":"opencode"}`, `{}`, `{"kind":"historical"}`); err != nil {
		t.Fatalf("seed historical Open Design run: %v", err)
	}
}

func openDesignDraftArchiveFixture(t *testing.T, runID string) ([]byte, json.RawMessage, []opendesign.ArtifactIndexEntry, string) {
	t.Helper()
	files := []struct {
		path string
		body string
		mime string
		role string
	}{
		{path: "DESIGN.md", body: testOpenDesignDesignMD, mime: "text/markdown; charset=utf-8", role: "source"},
		{path: "colors_and_type.css", body: testOpenDesignTokensCSS, mime: "text/css; charset=utf-8", role: "artifact"},
		{path: "ui_kits/app/index.html", body: testOpenDesignUIKitHTML, mime: "text/html; charset=utf-8", role: "entry"},
		{path: "ui_kits/app/components/table.js", body: testOpenDesignComponentJS, mime: "text/javascript; charset=utf-8", role: "artifact"},
	}
	var archive bytes.Buffer
	zipWriter := zip.NewWriter(&archive)
	manifestFiles := make([]map[string]any, 0, len(files))
	for _, file := range files {
		entry, err := zipWriter.Create(file.path)
		if err != nil {
			t.Fatalf("create Open Design ZIP entry %q: %v", file.path, err)
		}
		if _, err := entry.Write([]byte(file.body)); err != nil {
			t.Fatalf("write Open Design ZIP entry %q: %v", file.path, err)
		}
		manifestFiles = append(manifestFiles, map[string]any{
			"name": file.path, "size": len(file.body), "mime": file.mime, "included": true, "role": file.role,
		})
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close Open Design ZIP: %v", err)
	}
	manifest, err := json.Marshal(map[string]any{
		"schema": opendesign.ProjectExportManifestSchema, "projectId": "project-1", "files": manifestFiles,
	})
	if err != nil {
		t.Fatalf("marshal Open Design export manifest: %v", err)
	}
	resultPackage := json.RawMessage(fmt.Sprintf(`{"schema":%q,"run":{"id":%q,"status":"succeeded"}}`, opendesign.RunResultPackageSchema, runID))
	collected, err := opendesign.CollectWorkerRunResult(resultPackage, manifest, archive.Bytes(), runID, "project-1")
	if err != nil {
		t.Fatalf("collect Open Design test result: %v", err)
	}
	return archive.Bytes(), collected.ResultPackage, collected.ArtifactIndex, collected.ContentDigest
}

func TestRecoverOrphanedTasksFinalizesOpenDesignRunIdempotently(t *testing.T) {
	previous := testHandler.cfg.OpenDesignEnabled
	testHandler.cfg.OpenDesignEnabled = true
	t.Cleanup(func() { testHandler.cfg.OpenDesignEnabled = previous })

	tests := []struct {
		name        string
		taskStatus  string
		runStatus   string
		workerRunID string
	}{
		{name: "before worker run", taskStatus: "dispatched", runStatus: "preflight_pending"},
		{name: "during worker run", taskStatus: "running", runStatus: "running", workerRunID: "22222222-2222-4222-8222-222222222222"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testRecoverOrphanedOpenDesignRun(t, tt.taskStatus, tt.runStatus, tt.workerRunID)
		})
	}
}

func testRecoverOrphanedOpenDesignRun(t *testing.T, initialTaskStatus, initialRunStatus, workerRunID string) {
	t.Helper()
	projectID := createProjectForDesignTest(t, "Open Design orphan recovery")
	agentID, runtimeID := createProjectDesignSystemAgent(t, "online")
	// Since MUL-8154 a daemon token reaches only runtimes carrying its own
	// daemon_id, so bind the runtime to the callback token used below.
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_runtime SET daemon_id = 'project-design-system-test' WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("bind Open Design runtime daemon id: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_runtime SET provider = 'opencode' WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("configure Open Design runtime: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE agent SET model = 'anthropic/claude-sonnet-4-5' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("configure Open Design agent: %v", err)
	}

	response := performProjectDesignSystemRequest(t, testHandler.CreateProjectDesignSystem, http.MethodPost, "/api/project-design-systems", map[string]any{
		"project_id": projectID,
		"agent_id":   agentID,
		"platform":   "web",
		"brief":      "Create a source-grounded CRM design system.",
	})
	if response.Code != http.StatusAccepted {
		t.Fatalf("CreateProjectDesignSystem: status = %d, body = %s", response.Code, response.Body.String())
	}
	var created ProjectDesignSystemResponse
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.ActiveTask == nil {
		t.Fatal("create response has no active task")
	}
	seedHistoricalOpenDesignRun(t, projectID, created.ID, created.ActiveTask.ID, agentID)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent_task_queue
		SET status = $2,
			dispatched_at = now(),
			started_at = CASE WHEN $2 = 'running' THEN now() ELSE NULL END
		WHERE id = $1
	`, created.ActiveTask.ID, initialTaskStatus); err != nil {
		t.Fatalf("prepare orphaned Open Design task: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		UPDATE open_design_run
		SET status = $2,
			open_design_run_id = NULLIF($3, ''),
			started_at = CASE WHEN $2 = 'running' THEN now() ELSE NULL END
		WHERE task_id = $1
	`, created.ActiveTask.ID, initialRunStatus, workerRunID); err != nil {
		t.Fatalf("prepare orphaned Open Design run: %v", err)
	}

	for attempt, wantOrphaned := range []int{1, 0} {
		recovery := httptest.NewRecorder()
		request := newDaemonTokenRequest(
			http.MethodPost,
			"/api/daemon/runtimes/"+runtimeID+"/recover-orphans",
			nil,
			testWorkspaceID,
			"project-design-system-test",
		)
		request = withURLParam(request, "runtimeId", runtimeID)
		testHandler.RecoverOrphanedTasks(recovery, request)
		if recovery.Code != http.StatusOK {
			t.Fatalf("RecoverOrphanedTasks attempt %d: status = %d, body = %s", attempt+1, recovery.Code, recovery.Body.String())
		}
		var payload struct {
			Orphaned int `json:"orphaned"`
		}
		if err := json.NewDecoder(recovery.Body).Decode(&payload); err != nil {
			t.Fatalf("decode recovery attempt %d: %v", attempt+1, err)
		}
		if payload.Orphaned != wantOrphaned {
			t.Fatalf("recovery attempt %d orphaned = %d, want %d", attempt+1, payload.Orphaned, wantOrphaned)
		}
	}

	var (
		taskStatus     string
		taskFailure    pgtype.Text
		runStatus      string
		runFailure     []byte
		runFinishedAt  pgtype.Timestamptz
		persistedRunID pgtype.Text
	)
	if err := testPool.QueryRow(context.Background(), `
		SELECT task.status, task.failure_reason, run.status, run.failure, run.finished_at, run.open_design_run_id
		FROM agent_task_queue task
		JOIN open_design_run run ON run.task_id = task.id
		WHERE task.id = $1
	`, created.ActiveTask.ID).Scan(&taskStatus, &taskFailure, &runStatus, &runFailure, &runFinishedAt, &persistedRunID); err != nil {
		t.Fatalf("load recovered Open Design task: %v", err)
	}
	if taskStatus != "failed" || !taskFailure.Valid || taskFailure.String != "runtime_recovery" {
		t.Fatalf("recovered task = status:%q failure:%+v", taskStatus, taskFailure)
	}
	if runStatus != string(opendesign.RunStatusAgentFailed) || !runFinishedAt.Valid {
		t.Fatalf("recovered run = status:%q finished_at:%+v run_id:%+v", runStatus, runFinishedAt, persistedRunID)
	}
	if workerRunID == "" && persistedRunID.Valid {
		t.Fatalf("pre-run recovery persisted unexpected worker run id: %+v", persistedRunID)
	}
	if workerRunID != "" && (!persistedRunID.Valid || persistedRunID.String != workerRunID) {
		t.Fatalf("active-run recovery worker run id = %+v, want %q", persistedRunID, workerRunID)
	}
	assertJSONEqual(t, runFailure, `{"code":"runtime_recovery","message":"daemon restarted while task was in flight"}`)
}

func performOpenDesignDaemonCallbackForTest(
	t *testing.T,
	handler http.HandlerFunc,
	taskID string,
	path string,
	body any,
) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	// Callbacks authenticate as the "project-design-system-test" daemon, so
	// since MUL-8154 the runtime under test must carry that daemon_id.
	request := newDaemonTokenRequest(
		http.MethodPost,
		path,
		body,
		testWorkspaceID,
		"project-design-system-test",
	)
	request = withURLParam(request, "taskId", taskID)
	handler(response, request)
	return response
}
