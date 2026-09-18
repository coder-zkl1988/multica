package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

type issueStartFixture struct {
	taskID, issueID string
	claimGeneration int64
	baseRevision    int64
}

func newIssueStartFixture(t *testing.T, title string) issueStartFixture {
	t.Helper()
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	var agentID, runtimeID string
	dbfx.QueryRow(t, `SELECT a.id, a.runtime_id FROM agent a WHERE a.workspace_id=$1 LIMIT 1`, testWorkspaceID).Scan(&agentID, &runtimeID)
	issueID := dbfx.Issue(t, "typed start "+title, testutil.Cols{
		"status":        "todo",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "issue_id": issueID, "status": "dispatched",
		"dispatched_at": testutil.Raw("clock_timestamp()"),
	})
	var fixture issueStartFixture
	fixture.taskID, fixture.issueID = taskID, issueID
	dbfx.QueryRow(t, `SELECT floor(extract(epoch from dispatched_at)*1000000)::bigint FROM agent_task_queue WHERE id=$1`, taskID).Scan(&fixture.claimGeneration)
	dbfx.QueryRow(t, `SELECT revision FROM issue WHERE id=$1`, issueID).Scan(&fixture.baseRevision)
	return fixture
}

func (f issueStartFixture) start(t *testing.T, body any) *testutil.Response {
	t.Helper()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+f.taskID+"/start", body, testWorkspaceID, "typed-start")
	req = withURLParam(req, "taskId", f.taskID)
	return testutil.Call(t, testHandler.StartTask, req)
}

func (f issueStartFixture) body(generation, revision int64, status string) map[string]any {
	return map[string]any{
		"claim_generation": generation,
		"issue_start": map[string]any{
			"version": 1, "base_revision": revision,
			"base_etag": issueTaskSnapshotETag(f.issueID, revision), "base_status": status,
		},
	}
}

func TestStartTaskLegacyDoesNotMoveIssueStatus(t *testing.T) {
	fx := newIssueStartFixture(t, "legacy")
	fx.start(t, nil).Want(http.StatusOK)
	assertTaskAndIssueStatus(t, fx, "running", "todo")
}

func TestStartTaskTypedIssueStartMovesMatchingTodoToInProgress(t *testing.T) {
	fx := newIssueStartFixture(t, "matching")
	w := fx.start(t, fx.body(fx.claimGeneration, fx.baseRevision, "todo")).Want(http.StatusOK)
	var response AgentTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.IssueStart == nil || !response.IssueStart.Applied || !response.IssueStart.BaselineAccepted ||
		response.IssueStart.Status != "in_progress" || response.IssueStart.Revision != fx.baseRevision+1 {
		t.Fatalf("issue_start response = %#v", response.IssueStart)
	}
	assertTaskAndIssueStatus(t, fx, "running", "in_progress")
}

func TestStartTaskTypedIssueStartRejectsStaleGenerationAndRollsBackStart(t *testing.T) {
	fx := newIssueStartFixture(t, "stale generation")
	fx.start(t, fx.body(fx.claimGeneration-1, fx.baseRevision, "todo")).Want(http.StatusConflict)
	assertTaskAndIssueStatus(t, fx, "dispatched", "todo")
}

func TestStartTaskTypedIssueStartDoesNotMoveChangedIssue(t *testing.T) {
	fx := newIssueStartFixture(t, "changed revision")
	dbfx.Exec(t, `UPDATE issue SET revision=revision+1 WHERE id=$1`, fx.issueID)
	w := fx.start(t, fx.body(fx.claimGeneration, fx.baseRevision, "todo")).Want(http.StatusOK)
	var response AgentTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.IssueStart == nil || response.IssueStart.Applied || response.IssueStart.BaselineAccepted || response.IssueStart.Status != "todo" {
		t.Fatalf("issue_start response = %#v", response.IssueStart)
	}
	assertTaskAndIssueStatus(t, fx, "running", "todo")
}

func TestStartTaskTypedIssueStartDoesNotMoveReassignedIssue(t *testing.T) {
	fx := newIssueStartFixture(t, "reassigned")
	dbfx.Exec(t, `UPDATE issue SET assignee_id='00000000-0000-0000-0000-000000000001' WHERE id=$1`, fx.issueID)
	w := fx.start(t, fx.body(fx.claimGeneration, fx.baseRevision, "todo")).Want(http.StatusOK)
	var response AgentTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.IssueStart == nil || response.IssueStart.Applied || response.IssueStart.BaselineAccepted {
		t.Fatalf("issue_start response = %#v", response.IssueStart)
	}
	assertTaskAndIssueStatus(t, fx, "running", "todo")
}

func TestStartTaskRejectsTypedIssueStartForSpecializedTask(t *testing.T) {
	fx := newIssueStartFixture(t, "specialized")
	dbfx.Exec(t, `UPDATE agent_task_queue SET context='{"type":"quick_create"}'::jsonb WHERE id=$1`, fx.taskID)
	fx.start(t, fx.body(fx.claimGeneration, fx.baseRevision, "todo")).Want(http.StatusBadRequest)
	assertTaskAndIssueStatus(t, fx, "dispatched", "todo")
}

func TestStartTaskTypedIssueStartUsesCustomStatusCategory(t *testing.T) {
	for _, tc := range []struct {
		name, key, category, wantStatus string
		wantApplied                     bool
	}{
		{name: "custom todo advances", key: "r06_ready_to_begin", category: "unstarted", wantStatus: "in_progress", wantApplied: true},
		{name: "custom in progress remains", key: "r06_implementing", category: "started", wantStatus: "r06_implementing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := newIssueStartFixture(t, tc.name)
			dbfx.Insert(t, "issue_status", testutil.Cols{
				"workspace_id": testWorkspaceID, "key": tc.key, "name": tc.name,
				"description": "R06 start contract test", "category": tc.category,
				"color": "#475569", "position": 1,
			})
			dbfx.Exec(t, `UPDATE issue SET status=$2 WHERE id=$1`, fx.issueID, tc.key)
			w := fx.start(t, fx.body(fx.claimGeneration, fx.baseRevision, tc.key)).Want(http.StatusOK)
			var response AgentTaskResponse
			if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
				t.Fatal(err)
			}
			if response.IssueStart == nil || response.IssueStart.Applied != tc.wantApplied || !response.IssueStart.BaselineAccepted || response.IssueStart.Status != tc.wantStatus {
				t.Fatalf("issue_start response = %#v", response.IssueStart)
			}
			assertTaskAndIssueStatus(t, fx, "running", tc.wantStatus)
		})
	}
}

func assertTaskAndIssueStatus(t *testing.T, fx issueStartFixture, wantTask, wantIssue string) {
	t.Helper()
	var taskStatus, issueStatus string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id=$1`, fx.taskID).Scan(&taskStatus)
	dbfx.QueryRow(t, `SELECT status FROM issue WHERE id=$1`, fx.issueID).Scan(&issueStatus)
	if taskStatus != wantTask || issueStatus != wantIssue {
		t.Fatalf("statuses task=%q issue=%q, want task=%q issue=%q", taskStatus, issueStatus, wantTask, wantIssue)
	}
}
