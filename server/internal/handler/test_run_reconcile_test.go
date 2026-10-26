package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The runtime sweeper, orphan recovery and cancellations end a task without
// the daemon's /fail hook. The reconciler settles such a case as blocked once
// the hook's grace has passed, and converges the round when it was the last
// open case; a task that ended inside the grace is left to its hook.
func TestReconcilerSettlesCasesWhoseTaskEndedOutsideTheHook(t *testing.T) {
	projectID := newTestRunProject(t)
	a := createTestCaseForRun(t, projectID)
	b := createTestCaseForRun(t, projectID)
	runtimeID := dbfx.Runtime(t, "reconcile-runtime", testutil.Cols{"daemon_id": "daemon-reconcile"})
	agentID := dbfx.Agent(t, "reconcile-agent", runtimeID)
	run := createTestRunFromCases(t, "Reconcile run", []string{a.ID, b.ID})
	w := httptest.NewRecorder()
	req := withURLParam(
		newRequest("POST", "/api/test-runs/"+run.ID+"/dispatch?workspace_id="+testWorkspaceID, map[string]any{"agent_id": agentID}),
		"id", run.ID,
	)
	testHandler.DispatchTestRun(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("dispatch: got %d: %s", w.Code, w.Body.String())
	}

	ctx := context.Background()
	taskOf := func(testCaseID string) string {
		var taskID string
		if err := testPool.QueryRow(ctx,
			`SELECT agent_task_id FROM test_run_case WHERE run_id = $1 AND test_case_id = $2`, run.ID, testCaseID,
		).Scan(&taskID); err != nil {
			t.Fatal(err)
		}
		return taskID
	}
	caseResult := func(testCaseID string) (string, string) {
		var result, notes string
		if err := testPool.QueryRow(ctx,
			`SELECT result, notes FROM test_run_case WHERE run_id = $1 AND test_case_id = $2`, run.ID, testCaseID,
		).Scan(&result, &notes); err != nil {
			t.Fatal(err)
		}
		return result, notes
	}
	runStatus := func() string {
		var status string
		if err := testPool.QueryRow(ctx, `SELECT status FROM test_run WHERE id = $1`, run.ID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		return status
	}
	// End a task the way the sweepers do: a direct status write, no hook.
	endTask := func(taskID, status, errText, age string) {
		if _, err := testPool.Exec(ctx,
			`UPDATE agent_task_queue SET status = $2, error = NULLIF($3, ''), completed_at = now() - $4::interval WHERE id = $1`,
			taskID, status, errText, age,
		); err != nil {
			t.Fatal(err)
		}
	}

	taskA := taskOf(a.ID)
	task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskA))
	if err != nil {
		t.Fatal(err)
	}
	if err := testHandler.markTestRunRunning(ctx, task); err != nil {
		t.Fatal(err)
	}
	endTask(taskA, "failed", "runtime went offline", "2 minutes")
	endTask(taskOf(b.ID), "cancelled", "", "0 seconds")

	if _, err := testHandler.ReconcileTestRunCasesWithEndedTasks(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if got, notes := caseResult(a.ID); got != "blocked" || !strings.Contains(notes, "runtime went offline") {
		t.Errorf("case A after reconcile = %s / %q, want blocked with the task error", got, notes)
	}
	if got, _ := caseResult(b.ID); got != "pending" {
		t.Errorf("case B inside the hook's grace = %s, want pending", got)
	}
	if runStatus() != "running" {
		t.Errorf("run with case B still open = %s, want running", runStatus())
	}

	endTask(taskOf(b.ID), "cancelled", "", "2 minutes")
	if _, err := testHandler.ReconcileTestRunCasesWithEndedTasks(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if got, notes := caseResult(b.ID); got != "blocked" || !strings.Contains(notes, "cancelled") {
		t.Errorf("case B after reconcile = %s / %q, want blocked as cancelled", got, notes)
	}
	if runStatus() != "completed" {
		t.Errorf("run after its last case was reconciled = %s, want completed", runStatus())
	}
}

// A hook reads the round, then settles its case. An abort that commits in
// between must still keep the settle from becoming the case's latest result:
// the query re-checks the round's status itself rather than trusting the
// hook's read.
func TestASettleAfterAnAbortLeavesTheCaseUnstamped(t *testing.T) {
	projectID := newTestRunProject(t)
	a := createTestCaseForRun(t, projectID)
	runtimeID := dbfx.Runtime(t, "abort-race-runtime", testutil.Cols{"daemon_id": "daemon-abort-race"})
	agentID := dbfx.Agent(t, "abort-race-agent", runtimeID)
	run := createTestRunFromCases(t, "Abort race", []string{a.ID})
	testutil.Call(t, testHandler.DispatchTestRun, withURLParam(
		newRequest("POST", "/api/test-runs/"+run.ID+"/dispatch?workspace_id="+testWorkspaceID, map[string]any{"agent_id": agentID}),
		"id", run.ID,
	)).Want(http.StatusCreated)

	ctx := context.Background()
	stale, err := testHandler.Queries.GetTestRunInWorkspace(ctx, db.GetTestRunInWorkspaceParams{ID: parseUUID(run.ID), WorkspaceID: parseUUID(testWorkspaceID)})
	if err != nil {
		t.Fatal(err)
	}
	var runCaseID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM test_run_case WHERE run_id = $1`, run.ID).Scan(&runCaseID); err != nil {
		t.Fatal(err)
	}
	rc, err := testHandler.Queries.GetTestRunCaseInWorkspace(ctx, db.GetTestRunCaseInWorkspaceParams{ID: parseUUID(runCaseID), WorkspaceID: parseUUID(testWorkspaceID)})
	if err != nil {
		t.Fatal(err)
	}

	testutil.Call(t, testHandler.AbortTestRun, withURLParam(
		newRequest("POST", "/api/test-runs/"+run.ID+"/abort?workspace_id="+testWorkspaceID, nil), "id", run.ID,
	)).Want(http.StatusOK)
	if stale.Status == "aborted" {
		t.Fatal("the hook's read must predate the abort")
	}
	settled, err := settleOpenTestRunCase(ctx, testHandler.Queries, stale, rc, "blocked", "the agent task failed before recording a result")
	if err != nil || settled == nil {
		t.Fatalf("settle = %v, %v; want the open case settled", settled, err)
	}
	if settled.Result != "blocked" || settled.ExecutedAt.Valid {
		t.Fatalf("case settled into an aborted round = %s executed_at %v, want blocked with no execution time", settled.Result, settled.ExecutedAt)
	}
}
