package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestClaimTaskByRuntimeCarriesAuthorizedIssueSnapshot(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Issue snapshot claim runtime")
	// Since MUL-8154 a daemon token reaches only runtimes carrying its own
	// daemon_id, so bind the claim runtime to the token used below.
	dbfx.Exec(t, `UPDATE agent_runtime SET daemon_id = 'issue-snapshot-claim' WHERE id = $1`, runtimeID)
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Issue snapshot claim agent")
	dbfx.Exec(t, `
		UPDATE issue
		SET title = 'Snapshot title', description = 'Snapshot description', status = 'todo', revision = 17
		WHERE id = $1
	`, issueID)
	taskID := createDispatchedClaimFixtureTask(t, ctx, agentID, runtimeID, issueID, "120 seconds", false)

	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "issue-snapshot-claim")
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)

	var response struct {
		Task *struct {
			ID                             string             `json:"id"`
			IssueSnapshot                  *IssueTaskSnapshot `json:"issue_snapshot"`
			ClaimAttempt                   int                `json:"claim_attempt"`
			ClaimGeneration                int64              `json:"claim_generation"`
			IssueCompletionContractVersion int                `json:"issue_completion_contract_version"`
			IssueStartContractVersion      int                `json:"issue_start_contract_version"`
		} `json:"task"`
	}
	w.JSON(&response)
	if response.Task == nil || response.Task.ID != taskID {
		t.Fatalf("claimed task = %+v, want %s", response.Task, taskID)
	}
	snapshot := response.Task.IssueSnapshot
	if snapshot == nil {
		t.Fatalf("claim omitted issue_snapshot: %s", w.Body.String())
	}
	if response.Task.ClaimAttempt <= 0 {
		t.Fatalf("claim_attempt = %d, want task attempt counter", response.Task.ClaimAttempt)
	}
	if response.Task.ClaimGeneration <= 0 {
		t.Fatalf("claim_generation = %d, want a positive lease generation", response.Task.ClaimGeneration)
	}
	if response.Task.IssueCompletionContractVersion != 1 {
		t.Fatalf("issue_completion_contract_version = %d, want 1", response.Task.IssueCompletionContractVersion)
	}
	if response.Task.IssueStartContractVersion != 1 {
		t.Fatalf("issue_start_contract_version = %d, want 1", response.Task.IssueStartContractVersion)
	}
	if snapshot.SchemaVersion != issueTaskSnapshotSchemaVersion || !snapshot.Complete ||
		snapshot.Scope != issueTaskSnapshotScopeIssueBody || snapshot.Metadata == nil ||
		snapshot.IssueID != issueID || snapshot.Title != "Snapshot title" || snapshot.Description == nil ||
		*snapshot.Description != "Snapshot description" || snapshot.Status != "todo" || snapshot.Revision != 17 ||
		snapshot.ETag != issueTaskSnapshotETag(issueID, 17) || snapshot.CapturedAt == "" || snapshot.ExpiresAt == "" ||
		snapshot.CreatedAt == "" || snapshot.UpdatedAt == "" {
		t.Fatalf("claim issue_snapshot = %+v", snapshot)
	}
}
