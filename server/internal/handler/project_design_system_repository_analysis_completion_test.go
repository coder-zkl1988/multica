package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/projectdesignsystem"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const repositoryDesignContextMarker = "REPOSITORY_DESIGN_CONTEXT_JSON:"

func TestCompleteProjectDesignSystemRepositoryAnalysisPersistsValidatedContext(t *testing.T) {
	fixture := createProjectDesignSystemCompletionFixture(t, service.ProjectDesignSystemRepositoryAnalysis)
	before := projectDesignSystemInputSnapshot{
		AgentID:  fixture.AgentID,
		Platform: "web",
		Brief:    "Keep the CRM compact.",
		References: []projectDesignSystemReferenceSnapshot{{
			Kind:  "url",
			Label: "Product brief",
			Value: "https://example.com/brief",
		}},
	}
	beforeJSON := setProjectDesignSystemInputSnapshotForTest(t, fixture.System.ID, before)
	var beforeDocument map[string]json.RawMessage
	if err := json.Unmarshal(beforeJSON, &beforeDocument); err != nil {
		t.Fatalf("decode input snapshot document: %v", err)
	}
	beforeDocument["future_input_contract"] = json.RawMessage(`{"enabled":true,"mode":"preserve"}`)
	beforeWithSentinel, err := json.Marshal(beforeDocument)
	if err != nil {
		t.Fatalf("marshal input snapshot sentinel: %v", err)
	}
	beforeJSON = setProjectDesignSystemRawInputSnapshotForTest(t, fixture.System.ID, beforeWithSentinel)

	analysis := projectdesignsystem.RepositoryDesignContext{
		SchemaVersion:  projectdesignsystem.RepositoryDesignContextSchemaVersion,
		Summary:        "  Compact CRM repository  ",
		SuggestedBrief: "  Reuse the dense customer workflow.  ",
		Facts: []projectdesignsystem.RepositoryDesignFact{{
			Kind:        "  layout  ",
			Label:       "  Customer list  ",
			Value:       "  Dense filter and table layout  ",
			SourcePaths: []string{" ./apps/web/customer/page.tsx "},
			Confidence:  0.9,
		}},
		SourceFiles: []projectdesignsystem.RepositoryDesignSourceFile{{
			Path: "./apps/web/customer/page.tsx",
			Kind: " page ",
		}},
		Confidence: 0.9,
	}
	outputJSON, err := json.Marshal(analysis)
	if err != nil {
		t.Fatalf("marshal repository analysis output: %v", err)
	}
	w := completeProjectDesignSystemTaskForTest(t, fixture.TaskID, map[string]any{
		"output": repositoryDesignContextMarker + string(outputJSON),
	})
	if w.Code != http.StatusOK {
		t.Fatalf("CompleteTask status = %d, want 200: %s", w.Code, w.Body.String())
	}

	queries := db.New(testPool)
	task, err := queries.GetAgentTask(context.Background(), parseUUID(fixture.TaskID))
	if err != nil {
		t.Fatalf("load completed repository analysis task: %v", err)
	}
	if task.Status != "completed" {
		t.Fatalf("task status = %q, want completed", task.Status)
	}
	system, err := queries.GetProjectDesignSystemInWorkspace(context.Background(), db.GetProjectDesignSystemInWorkspaceParams{
		ID: fixture.System.ID, WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load completed project design system: %v", err)
	}
	if system.ActiveTaskID.Valid || system.ActiveOperation.Valid {
		t.Fatalf("completed repository analysis still active: %+v", system)
	}
	var stored projectDesignSystemInputSnapshot
	if err := json.Unmarshal(system.InputSnapshot, &stored); err != nil {
		t.Fatalf("decode completed input snapshot: %v", err)
	}
	var original projectDesignSystemInputSnapshot
	if err := json.Unmarshal(beforeJSON, &original); err != nil {
		t.Fatalf("decode original input snapshot: %v", err)
	}
	storedAnalysis := stored.RepositoryAnalysis
	stored.RepositoryAnalysis = nil
	if !equalProjectDesignSystemInputSnapshot(stored, original) {
		t.Fatalf("non-analysis input fields changed: got %+v want %+v", stored, original)
	}
	if storedAnalysis == nil || storedAnalysis.Summary != "Compact CRM repository" || storedAnalysis.SuggestedBrief != "Reuse the dense customer workflow." {
		t.Fatalf("stored repository analysis was not normalized: %+v", storedAnalysis)
	}
	if len(storedAnalysis.SourceFiles) != 1 || storedAnalysis.SourceFiles[0].Path != "apps/web/customer/page.tsx" || storedAnalysis.SourceFiles[0].Kind != "page" {
		t.Fatalf("stored source files were not normalized: %+v", storedAnalysis.SourceFiles)
	}
	var originalDocument map[string]json.RawMessage
	var storedDocument map[string]json.RawMessage
	if err := json.Unmarshal(beforeJSON, &originalDocument); err != nil {
		t.Fatalf("decode original input document: %v", err)
	}
	if err := json.Unmarshal(system.InputSnapshot, &storedDocument); err != nil {
		t.Fatalf("decode stored input document: %v", err)
	}
	if !bytes.Equal(storedDocument["future_input_contract"], originalDocument["future_input_contract"]) {
		t.Fatalf("future input field changed: got %s want %s", storedDocument["future_input_contract"], originalDocument["future_input_contract"])
	}
	assertNoProjectDesignSystemPackages(t, queries, fixture.System.ID)
}

func TestCompleteProjectDesignSystemRepositoryAnalysisReturnsServerErrorWhenFailTaskRollsBack(t *testing.T) {
	fixture := createProjectDesignSystemCompletionFixture(t, service.ProjectDesignSystemRepositoryAnalysis)
	queries := db.New(testPool)
	before, err := queries.GetProjectDesignSystemInWorkspace(context.Background(), db.GetProjectDesignSystemInWorkspaceParams{
		ID: fixture.System.ID, WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load active project design system: %v", err)
	}
	// Corrupt the context's agent identity instead of its workspace: since
	// MUL-8154 the daemon gate refuses a task whose context workspace no
	// longer matches its runtime (404 before CompleteTask runs), so the
	// server-side failure must come from a field only the completion path
	// validates — here the failure propagation's agent identity check, which
	// makes FailTask itself roll back and CompleteTask report 500.
	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent_task_queue
		SET context = jsonb_set(context, '{agent_id}', to_jsonb($1::text))
		WHERE id = $2
	`, "30000000-0000-4000-8000-000000000001", fixture.TaskID); err != nil {
		t.Fatalf("invalidate repository analysis agent context: %v", err)
	}

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+fixture.TaskID+"/complete", map[string]any{"output": `{}`}, testWorkspaceID, "project-design-system-test")
	req = withURLParam(req, "taskId", fixture.TaskID)
	testHandler.CompleteTask(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("CompleteTask status = %d, want 500: %s", w.Code, w.Body.String())
	}
	task, err := queries.GetAgentTask(context.Background(), parseUUID(fixture.TaskID))
	if err != nil {
		t.Fatalf("load retryable repository analysis task: %v", err)
	}
	if task.Status != "running" {
		t.Fatalf("task status = %q, want running", task.Status)
	}
	system, err := queries.GetProjectDesignSystemInWorkspace(context.Background(), db.GetProjectDesignSystemInWorkspaceParams{
		ID: fixture.System.ID, WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load retryable project design system: %v", err)
	}
	if !system.ActiveTaskID.Valid || uuidToString(system.ActiveTaskID) != fixture.TaskID ||
		!system.ActiveOperation.Valid || system.ActiveOperation.String != string(service.ProjectDesignSystemRepositoryAnalysis) {
		t.Fatalf("repository analysis operation is not retryable: %+v", system)
	}
	if system.LastError != nil {
		t.Fatalf("last_error changed after rolled-back failure: %s", system.LastError)
	}
	if !bytes.Equal(system.InputSnapshot, before.InputSnapshot) {
		t.Fatalf("input snapshot changed after rolled-back failure: got %s want %s", system.InputSnapshot, before.InputSnapshot)
	}
}

func TestCompleteProjectDesignSystemRepositoryAnalysisRejectsInvalidOutput(t *testing.T) {
	valid := `{"schema_version":"multica.repository-design-context/v1","summary":"CRM","suggested_brief":"","facts":[],"source_files":[],"representative_workflows":[],"confidence":0.8,"conflicts":[]}`
	tests := []struct {
		name   string
		output string
	}{
		{name: "missing marker", output: valid},
		{name: "duplicate marker", output: repositoryDesignContextMarker + valid + repositoryDesignContextMarker + valid},
		{name: "markdown fence", output: "```json\n" + repositoryDesignContextMarker + valid + "\n```"},
		{name: "leading prose", output: "Repository analysis:\n" + repositoryDesignContextMarker + valid},
		{name: "trailing prose", output: repositoryDesignContextMarker + valid + "\nDone"},
		{name: "malformed JSON", output: repositoryDesignContextMarker + `{"schema_version":`},
		{name: "trailing JSON", output: repositoryDesignContextMarker + valid + `{}`},
		{name: "unknown field", output: repositoryDesignContextMarker + strings.TrimSuffix(valid, "}") + `,"unexpected":true}`},
		{name: "unsafe source path", output: repositoryDesignContextMarker + `{"schema_version":"multica.repository-design-context/v1","summary":"CRM","facts":[],"source_files":[{"path":"../secret","kind":"page"}],"representative_workflows":[],"confidence":0.8,"conflicts":[]}`},
		{name: "oversized payload", output: repositoryDesignContextMarker + `{"schema_version":"multica.repository-design-context/v1","summary":"` + strings.Repeat("x", projectdesignsystem.MaxRepositoryDesignContextBytes) + `","facts":[],"source_files":[],"representative_workflows":[],"confidence":0.8,"conflicts":[]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := createProjectDesignSystemCompletionFixture(t, service.ProjectDesignSystemRepositoryAnalysis)
			queries := db.New(testPool)
			draftDigest := strings.Repeat("a", 64)
			savedDigest := strings.Repeat("b", 64)
			upsertProjectDesignSystemPackageForTest(t, queries, fixture.System.ID, "draft", "draft-before-analysis-failure", draftDigest)
			upsertProjectDesignSystemPackageForTest(t, queries, fixture.System.ID, "saved", "saved-before-analysis-failure", savedDigest)
			beforeJSON := setProjectDesignSystemInputSnapshotForTest(t, fixture.System.ID, projectDesignSystemInputSnapshot{
				AgentID: fixture.AgentID, Platform: "web", Brief: "Original CRM brief.",
			})

			w := completeProjectDesignSystemTaskForTest(t, fixture.TaskID, map[string]any{"output": tt.output})
			if w.Code != http.StatusBadRequest {
				t.Fatalf("CompleteTask status = %d, want 400: %s", w.Code, w.Body.String())
			}
			assertProjectDesignSystemTaskFailed(t, fixture.TaskID, "project_design_system_repository_analysis_invalid_output")
			assertProjectDesignSystemFailureState(t, fixture.System.ID, fixture.TaskID, "project_design_system_repository_analysis_invalid_output")
			system, err := queries.GetProjectDesignSystemInWorkspace(context.Background(), db.GetProjectDesignSystemInWorkspaceParams{
				ID: fixture.System.ID, WorkspaceID: parseUUID(testWorkspaceID),
			})
			if err != nil {
				t.Fatalf("load failed project design system: %v", err)
			}
			if !bytes.Equal(system.InputSnapshot, beforeJSON) {
				t.Fatalf("input snapshot changed on invalid output:\n got %s\nwant %s", system.InputSnapshot, beforeJSON)
			}
			assertProjectDesignSystemPackageDigest(t, queries, fixture.System.ID, "draft", draftDigest)
			assertProjectDesignSystemPackageDigest(t, queries, fixture.System.ID, "saved", savedDigest)
		})
	}
}

func setProjectDesignSystemInputSnapshotForTest(t *testing.T, systemID pgtype.UUID, input projectDesignSystemInputSnapshot) []byte {
	t.Helper()
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input snapshot: %v", err)
	}
	return setProjectDesignSystemRawInputSnapshotForTest(t, systemID, encoded)
}

func setProjectDesignSystemRawInputSnapshotForTest(t *testing.T, systemID pgtype.UUID, encoded []byte) []byte {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `UPDATE project_design_system SET input_snapshot = $1 WHERE id = $2`, encoded, systemID); err != nil {
		t.Fatalf("set input snapshot: %v", err)
	}
	system, err := db.New(testPool).GetProjectDesignSystemInWorkspace(context.Background(), db.GetProjectDesignSystemInWorkspaceParams{
		ID: systemID, WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("reload input snapshot: %v", err)
	}
	return system.InputSnapshot
}

func equalProjectDesignSystemInputSnapshot(left, right projectDesignSystemInputSnapshot) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return bytes.Equal(leftJSON, rightJSON)
}

func assertNoProjectDesignSystemPackages(t *testing.T, queries *db.Queries, systemID pgtype.UUID) {
	t.Helper()
	for _, slot := range []string{"draft", "saved"} {
		_, err := queries.GetProjectDesignSystemPackageBySlot(context.Background(), db.GetProjectDesignSystemPackageBySlotParams{
			DesignSystemID: systemID,
			Slot:           slot,
			WorkspaceID:    parseUUID(testWorkspaceID),
		})
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("package slot %q exists or lookup failed: %v", slot, err)
		}
	}
}
