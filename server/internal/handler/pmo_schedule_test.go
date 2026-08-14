package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// makeRunScheduledForTest flips a run row to trigger=scheduled so the
// completion path exercises the scheduled auto-apply hook.
func makeRunScheduledForTest(t *testing.T, runID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`UPDATE pmo_sync_run SET trigger = 'scheduled' WHERE id = $1`, runID); err != nil {
		t.Fatalf("mark run scheduled: %v", err)
	}
}

func TestCompleteScheduledPMOSyncTaskAutoApplies(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	config := createPMOConfigForTest(t)
	run := startPMORunForTest(t, config.ID)
	makeRunScheduledForTest(t, run.ID)
	markAgentTaskRunningForTest(t, *run.AgentTaskID)

	w := pmoCompleteTaskForTest(t, *run.AgentTaskID, validPMOSnapshotForTest(t))
	if w.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", w.Code, w.Body.String())
	}

	var status string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status FROM pmo_sync_run WHERE id = $1`, run.ID).Scan(&status); err != nil {
		t.Fatalf("read run: %v", err)
	}
	if status != "applied" && status != "applied_with_review" {
		t.Fatalf("scheduled run status = %q, want applied/applied_with_review", status)
	}
	// The snapshot must have been persisted before apply ran.
	var snapshot json.RawMessage
	if err := testPool.QueryRow(context.Background(),
		`SELECT source_snapshot FROM pmo_sync_run WHERE id = $1`, run.ID).Scan(&snapshot); err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if len(snapshot) == 0 || !strings.Contains(string(snapshot), "EXT-P-001") {
		t.Fatalf("source_snapshot missing: %s", snapshot)
	}
}

func TestCompleteScheduledPMOSyncTaskInvalidOutputStillFails(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	config := createPMOConfigForTest(t)
	run := startPMORunForTest(t, config.ID)
	makeRunScheduledForTest(t, run.ID)
	markAgentTaskRunningForTest(t, *run.AgentTaskID)

	w := pmoCompleteTaskForTest(t, *run.AgentTaskID, `{"not":"a snapshot"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("complete: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var status, errorCode string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status, COALESCE(error_code, '') FROM pmo_sync_run WHERE id = $1`, run.ID).Scan(&status, &errorCode); err != nil {
		t.Fatalf("read run: %v", err)
	}
	if status != "failed" || errorCode != "pmo_invalid_output" {
		t.Fatalf("run = %q/%q, want failed/pmo_invalid_output (no auto-apply on invalid output)", status, errorCode)
	}
}

// TestCompleteScheduledPMOSyncApplyFailurePreservesPreview: an apply failure
// must keep the run preview_ready with a REDACTED, bounded error — the
// acquired snapshot is never discarded.
func TestCompleteScheduledPMOSyncApplyFailurePreservesPreview(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	config := createPMOConfigForTest(t)
	run := startPMORunForTest(t, config.ID)
	makeRunScheduledForTest(t, run.ID)
	markAgentTaskRunningForTest(t, *run.AgentTaskID)

	testHandler.PMOService.SetApplyTestHook(func(_ context.Context, _ *db.Queries) error {
		return pgx.ErrTxClosed
	})
	t.Cleanup(func() { testHandler.PMOService.SetApplyTestHook(nil) })

	w := pmoCompleteTaskForTest(t, *run.AgentTaskID, validPMOSnapshotForTest(t))
	if w.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", w.Code, w.Body.String())
	}

	var status, errorCode, errorMessage string
	var snapshot []byte
	if err := testPool.QueryRow(context.Background(), `
		SELECT status, COALESCE(error_code, ''), COALESCE(error_message, ''), source_snapshot
		FROM pmo_sync_run WHERE id = $1
	`, run.ID).Scan(&status, &errorCode, &errorMessage, &snapshot); err != nil {
		t.Fatalf("read run: %v", err)
	}
	if status != "preview_ready" {
		t.Fatalf("run status = %q, want preview_ready after failed auto-apply", status)
	}
	if errorCode == "" || len(errorMessage) == 0 {
		t.Fatalf("apply error not recorded: code=%q msg=%q", errorCode, errorMessage)
	}
	if len(errorMessage) > 200 {
		t.Fatalf("apply error not bounded: %d bytes", len(errorMessage))
	}
	if len(snapshot) == 0 || !strings.Contains(string(snapshot), "EXT-P-001") {
		t.Fatalf("snapshot discarded by failed auto-apply: %s", snapshot)
	}
}

// TestCompleteManualPMOSyncTaskStaysPreviewReady guards that manual runs are
// never auto-applied — the preview is the review surface.
func TestCompleteManualPMOSyncTaskStaysPreviewReady(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	config := createPMOConfigForTest(t)
	run := startPMORunForTest(t, config.ID)
	markAgentTaskRunningForTest(t, *run.AgentTaskID)

	w := pmoCompleteTaskForTest(t, *run.AgentTaskID, validPMOSnapshotForTest(t))
	if w.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", w.Code, w.Body.String())
	}
	var status string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status FROM pmo_sync_run WHERE id = $1`, run.ID).Scan(&status); err != nil {
		t.Fatalf("read run: %v", err)
	}
	if status != "preview_ready" {
		t.Fatalf("manual run status = %q, want preview_ready", status)
	}
}

func TestCompleteScheduledPMOSyncTaskAutoMapsAssigneeByEmail(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	account := "pmo-scheduled-auto-map"
	userID := createPMOEmailMemberForTest(t, account)
	config := createPMOConfigForTest(t)
	run := startPMORunForTest(t, config.ID)
	makeRunScheduledForTest(t, run.ID)
	markAgentTaskRunningForTest(t, *run.AgentTaskID)

	w := pmoCompleteTaskForTest(t, *run.AgentTaskID, validPMOSnapshotForTestWithOwner(t, account))
	if w.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", w.Code, w.Body.String())
	}

	var status string
	var summaryRaw []byte
	if err := testPool.QueryRow(context.Background(),
		`SELECT status, summary FROM pmo_sync_run WHERE id = $1`, run.ID,
	).Scan(&status, &summaryRaw); err != nil {
		t.Fatalf("read run: %v", err)
	}
	if status != "applied" {
		t.Fatalf("scheduled run status = %q, want applied", status)
	}
	var summary struct {
		UnresolvedAssignees int `json:"unresolved_assignees"`
	}
	if err := json.Unmarshal(summaryRaw, &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if summary.UnresolvedAssignees != 0 {
		t.Fatalf("scheduled unresolved assignees = %d, want 0", summary.UnresolvedAssignees)
	}

	var localType, localID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT COALESCE(local_type, ''), COALESCE(local_id::text, '')
		FROM pmo_sync_link
		WHERE config_id = $1 AND external_type = 'assignee' AND external_key = $2
	`, config.ID, account).Scan(&localType, &localID); err != nil {
		t.Fatalf("read assignee link: %v", err)
	}
	if localType != "member" || localID != userID {
		t.Fatalf("scheduled assignee link = %q/%s, want member/%s", localType, localID, userID)
	}
}
