package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// TestCreateIssueQuickCreateSuppressesAssigneeRun exercises the real HTTP
// CreateIssue path with terminal task rows and the same task-owned agent
// identity and quick-create contexts the daemon uses. Suppression is limited
// to the context's effective assignee; malformed or mismatched provenance is
// rejected or enqueues normally. Ordinary member-created assignment remains
// unchanged.
func TestCreateIssueQuickCreateSuppressesAssigneeRun(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "quick-create-assignment-agent", nil)
	otherAgentID := createHandlerTestAgent(t, "quick-create-assignment-other-agent", nil)
	squadID := dbfx.Squad(t, "quick-create-assignment-squad", agentID)
	otherSquadID := dbfx.Squad(t, "quick-create-assignment-other-squad", otherAgentID)

	quickContext := func(squadID string) testutil.Raw {
		squadField := ""
		if squadID != "" {
			squadField = fmt.Sprintf(`,"squad_id":"%s"`, squadID)
		}
		return testutil.Raw(fmt.Sprintf(
			`'{"type":"quick_create","prompt":"create an issue","requester_id":"%s","workspace_id":"%s"%s}'::jsonb`,
			testUserID, testWorkspaceID, squadField))
	}
	terminalOriginTask := func(label, contextSquadID string) string {
		return dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id":          handlerTestRuntimeID(t),
			"status":              "completed",
			"started_at":          testutil.Raw("now() - interval '1 minute'"),
			"completed_at":        testutil.Raw("now()"),
			"originator_user_id":  testUserID,
			"accountable_user_id": testUserID,
			"context":             quickContext(contextSquadID),
			"failure_reason":      label,
		})
	}

	defaultTaskID := terminalOriginTask("quick-create-default", "")
	selectedSquadTaskID := terminalOriginTask("quick-create-selected-squad", squadID)
	mismatchTaskID := terminalOriginTask("quick-create-mismatch", "")

	createIssue := func(t *testing.T, title, assigneeType, assigneeID, taskID, originID string) string {
		t.Helper()
		body := map[string]any{
			"title":         title,
			"assignee_type": assigneeType,
			"assignee_id":   assigneeID,
		}
		req := newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, body)
		if taskID != "" {
			body["origin_type"] = "quick_create"
			body["origin_id"] = originID
			req = newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, body)
			req.Header.Set("X-Agent-ID", agentID)
			req.Header.Set("X-Task-ID", taskID)
		}
		w := httptest.NewRecorder()
		testHandler.CreateIssue(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateIssue(%s): expected 201, got %d: %s", title, w.Code, w.Body.String())
		}
		var issue IssueResponse
		if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
			t.Fatalf("decode created issue %s: %v", title, err)
		}
		dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issue.ID)
		dbfx.Cleanup(t, `DELETE FROM issue WHERE id = $1`, issue.ID)
		return issue.ID
	}

	createRejectedIssue := func(t *testing.T, title, assigneeID, taskID, originID string) {
		t.Helper()
		body := map[string]any{
			"title":         title,
			"assignee_type": "agent",
			"assignee_id":   assigneeID,
			"origin_type":   "quick_create",
			"origin_id":     originID,
		}
		req := newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, body)
		req.Header.Set("X-Agent-ID", agentID)
		req.Header.Set("X-Task-ID", taskID)
		w := httptest.NewRecorder()
		testHandler.CreateIssue(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("CreateIssue(%s): expected 400, got %d: %s", title, w.Code, w.Body.String())
		}
		if got := dbfx.Count(t, `SELECT COUNT(*) FROM issue WHERE workspace_id = $1 AND title = $2`, testWorkspaceID, title); got != 0 {
			t.Fatalf("rejected quick-create created %d issues", got)
		}
	}

	countIssueTasks := func(t *testing.T, issueID string) int {
		t.Helper()
		return dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1`, issueID)
	}

	defaultIssueID := createIssue(t, "quick-create default agent assignment", "agent", agentID, defaultTaskID, defaultTaskID)
	if got := countIssueTasks(t, defaultIssueID); got != 0 {
		t.Fatalf("default quick-create agent assignment enqueued %d follow-on tasks, want 0", got)
	}

	selectedIssueID := createIssue(t, "quick-create selected squad assignment", "squad", squadID, selectedSquadTaskID, selectedSquadTaskID)
	if got := countIssueTasks(t, selectedIssueID); got != 0 {
		t.Fatalf("selected-squad quick-create assignment enqueued %d follow-on tasks, want 0", got)
	}

	differentAgentIssueID := createIssue(t, "quick-create different agent assignment", "agent", otherAgentID, defaultTaskID, defaultTaskID)
	if got := countIssueTasks(t, differentAgentIssueID); got != 1 {
		t.Fatalf("different-agent quick-create assignment enqueued %d tasks, want 1", got)
	}

	differentSquadIssueID := createIssue(t, "quick-create different squad assignment", "squad", otherSquadID, selectedSquadTaskID, selectedSquadTaskID)
	if got := countIssueTasks(t, differentSquadIssueID); got != 1 {
		t.Fatalf("different-squad quick-create assignment enqueued %d tasks, want 1", got)
	}

	createRejectedIssue(t, "quick-create same-agent task mismatch", agentID, mismatchTaskID, defaultTaskID)

	ordinaryIssueID := createIssue(t, "ordinary agent assignment", "agent", agentID, "", "")
	if got := countIssueTasks(t, ordinaryIssueID); got != 1 {
		t.Fatalf("ordinary agent assignment enqueued %d tasks, want 1", got)
	}
}
