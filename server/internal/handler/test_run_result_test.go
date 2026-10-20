package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// The rules that make a recorded result trustworthy. The pure checks are
// table-tested here; the handler tests below cover their wiring.

func TestStepResultsProblem(t *testing.T) {
	steps := func(results ...string) []TestRunStepResult {
		out := make([]TestRunStepResult, len(results))
		for i, result := range results {
			out[i] = TestRunStepResult{Index: int32(i + 1), Result: result}
		}
		return out
	}
	snapshot := []int32{1, 2, 3}
	cases := []struct {
		name            string
		result          string
		steps           []TestRunStepResult
		requireComplete bool
		wantProblem     string
	}{
		{name: "a person may record a bare pass", result: "passed"},
		{name: "a person may record a bare fail", result: "failed"},
		{name: "an agent pass must cover every step", result: "passed", steps: steps("passed", "passed"), requireComplete: true, wantProblem: "step 3 has none"},
		{name: "an agent pass with every step passed", result: "passed", steps: steps("passed", "passed", "passed"), requireComplete: true},
		{name: "a skipped step does not stop a pass", result: "passed", steps: steps("passed", "skipped", "passed"), requireComplete: true},
		{name: "a pass cannot hide a failed step", result: "passed", steps: steps("passed", "failed", "passed"), wantProblem: "step 2 is failed"},
		{name: "a pass cannot hide a step never reached", result: "passed", steps: steps("passed", "passed", "not_run"), wantProblem: "step 3 is not_run"},
		{name: "a fail must name the failed step", result: "failed", steps: steps("passed", "blocked", "not_run"), wantProblem: "at least one failed step"},
		{name: "a fail with its failing step", result: "failed", steps: steps("passed", "failed", "not_run"), requireComplete: true},
		{name: "a blocked case needs no complete steps", result: "blocked", steps: steps("passed", "blocked")},
		{name: "unknown step verdict", result: "passed", steps: []TestRunStepResult{{Index: 1, Result: "ok"}}, wantProblem: "must be one of"},
		{name: "step outside the snapshot", result: "blocked", steps: []TestRunStepResult{{Index: 9, Result: "blocked"}}, wantProblem: "not a step of this case"},
		{name: "repeated step", result: "blocked", steps: []TestRunStepResult{{Index: 1, Result: "passed"}, {Index: 1, Result: "blocked"}}, wantProblem: "repeats step 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stepResultsProblem(tc.result, tc.steps, snapshot, tc.requireComplete)
			if tc.wantProblem == "" && got != "" {
				t.Fatalf("problem = %q, want none", got)
			}
			if tc.wantProblem != "" && !strings.Contains(got, tc.wantProblem) {
				t.Fatalf("problem = %q, want it to mention %q", got, tc.wantProblem)
			}
		})
	}
}

func TestSnapshotStepIndexes(t *testing.T) {
	got := snapshotStepIndexes([]byte(`{"steps":[{"index":0,"action":"a"},{"index":0,"action":"b"}]}`))
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("legacy snapshot indexes = %v, want positional [1 2]", got)
	}
	if got := snapshotStepIndexes([]byte(`not json`)); len(got) != 0 {
		t.Fatalf("unreadable snapshot indexes = %v, want none", got)
	}
}

func TestRunAcceptsResultWrite(t *testing.T) {
	cases := []struct {
		status  string
		byAgent bool
		want    bool
	}{
		{"pending", true, true},
		{"running", true, true},
		{"running", false, true},
		{"completed", false, true},
		{"completed", true, false},
		{"blocked", true, false},
		{"aborted", false, false},
		{"aborted", true, false},
	}
	for _, tc := range cases {
		if got, _ := runAcceptsResultWrite(tc.status, tc.byAgent); got != tc.want {
			t.Errorf("runAcceptsResultWrite(%q, agent=%v) = %v, want %v", tc.status, tc.byAgent, got, tc.want)
		}
	}
}

// agentOwnedRunCase makes a round of one three-step case whose case task is
// the given task id, as dispatch would, and returns the run and run case ids.
func agentOwnedRunCase(t *testing.T, taskID string) (string, string) {
	t.Helper()
	projectID := newTestRunProject(t)
	var tc TestCaseResponse
	testutil.Call(t, testHandler.CreateTestCase, newRequest("POST", "/api/test-cases?workspace_id="+testWorkspaceID, map[string]any{
		"project_id": projectID,
		"title":      "Three-step case " + t.Name(),
		"steps": []map[string]any{
			{"action": "Open the cart", "expected": "The cart lists one item"},
			{"action": "Apply coupon SAVE10", "expected": "The total drops by 10%"},
			{"action": "Pay", "expected": "The order confirmation shows the discounted total"},
		},
	})).Want(http.StatusCreated).JSON(&tc)
	run := createTestRunFromCases(t, "Agent round "+t.Name(), []string{tc.ID})
	runCaseID := listRunCases(t, run.ID)[0].ID
	dbfx.Exec(t, `UPDATE test_run SET agent_task_id = $1 WHERE id = $2`, taskID, run.ID)
	dbfx.Exec(t, `UPDATE test_run_case SET agent_task_id = $1 WHERE id = $2`, taskID, runCaseID)
	return run.ID, runCaseID
}

func agentResultRequest(runCaseID, taskID string, body map[string]any) *http.Request {
	req := withURLParam(newRequest("PUT", "/api/test-run-cases/"+runCaseID+"/result?workspace_id="+testWorkspaceID, body), "id", runCaseID)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Task-ID", taskID)
	return req
}

func addRunCaseEvidence(t *testing.T, runCaseID string) {
	t.Helper()
	dbfx.Insert(t, "attachment", testutil.Cols{
		"id":               uuidToString(dbid.NewV7()),
		"workspace_id":     testWorkspaceID,
		"uploader_type":    "member",
		"uploader_id":      testUserID,
		"filename":         "final.png",
		"url":              "https://example.test/evidence/final.png",
		"content_type":     "image/png",
		"size_bytes":       10,
		"test_run_case_id": runCaseID,
	})
}

// An agent's pass is the claim the testing center exists to check: it must
// say what happened at every step and leave something a person can look at.
func TestAgentPassNeedsEveryStepAndEvidence(t *testing.T) {
	taskID := uuidToString(dbid.NewV7())
	_, runCaseID := agentOwnedRunCase(t, taskID)
	allPassed := []map[string]any{
		{"index": 1, "result": "passed"},
		{"index": 2, "result": "passed", "actual": "Total went from 100.00 to 90.00"},
		{"index": 3, "result": "passed"},
	}

	resp := testutil.Call(t, testHandler.UpdateTestRunCaseResult, agentResultRequest(runCaseID, taskID, map[string]any{"result": "passed"})).Want(http.StatusBadRequest)
	if !strings.Contains(resp.Text(), "every step") {
		t.Errorf("bare agent pass refused with %q, want the every-step rule", resp.Text())
	}
	testutil.Call(t, testHandler.UpdateTestRunCaseResult, agentResultRequest(runCaseID, taskID, map[string]any{
		"result": "passed", "step_results": allPassed[:2],
	})).Want(http.StatusBadRequest)

	resp = testutil.Call(t, testHandler.UpdateTestRunCaseResult, agentResultRequest(runCaseID, taskID, map[string]any{
		"result": "passed", "step_results": allPassed,
	})).Want(http.StatusConflict)
	if !strings.Contains(resp.Text(), "evidence") {
		t.Errorf("evidence-less pass refused with %q, want the evidence rule", resp.Text())
	}

	addRunCaseEvidence(t, runCaseID)
	var recorded TestRunCaseResponse
	testutil.Call(t, testHandler.UpdateTestRunCaseResult, agentResultRequest(runCaseID, taskID, map[string]any{
		"result": "passed", "step_results": allPassed,
	})).Want(http.StatusOK).JSON(&recorded)
	if recorded.Result != "passed" {
		t.Fatalf("result = %q, want passed", recorded.Result)
	}
	var stored []TestRunStepResult
	dbfx.QueryRow(t, `SELECT step_results FROM test_run_case WHERE id = $1`, runCaseID).Scan(&stored)
	if len(stored) != 3 || stored[1].Actual != "Total went from 100.00 to 90.00" {
		t.Errorf("stored step results = %+v, want the three typed steps with the observed total", stored)
	}
}

// A blocked case may have nothing to show — the phone was gone — so it needs
// neither every step nor a screenshot, but what it does say is still checked.
func TestAgentBlockedNeedsNoEvidence(t *testing.T) {
	taskID := uuidToString(dbid.NewV7())
	_, runCaseID := agentOwnedRunCase(t, taskID)
	testutil.Call(t, testHandler.UpdateTestRunCaseResult, agentResultRequest(runCaseID, taskID, map[string]any{
		"result": "blocked", "notes": "device_offline", "step_results": []map[string]any{{"index": 7, "result": "blocked"}},
	})).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.UpdateTestRunCaseResult, agentResultRequest(runCaseID, taskID, map[string]any{
		"result": "blocked", "notes": "device_offline",
	})).Want(http.StatusOK)
}

func TestAgentCannotRecordAProgressState(t *testing.T) {
	taskID := uuidToString(dbid.NewV7())
	_, runCaseID := agentOwnedRunCase(t, taskID)
	testutil.Call(t, testHandler.UpdateTestRunCaseResult, agentResultRequest(runCaseID, taskID, map[string]any{"result": "pending"})).Want(http.StatusBadRequest)
}

// Aborting is final: neither the stale agent task nor a person writing late
// can bring the round back, and a late write must not flip it to completed.
func TestAbortedRoundRefusesResultWrites(t *testing.T) {
	taskID := uuidToString(dbid.NewV7())
	runID, runCaseID := agentOwnedRunCase(t, taskID)
	dbfx.Exec(t, `UPDATE test_run SET status = 'aborted' WHERE id = $1`, runID)

	testutil.Call(t, testHandler.UpdateTestRunCaseResult, withURLParam(
		newRequest("PUT", "/api/test-run-cases/"+runCaseID+"/result?workspace_id="+testWorkspaceID, map[string]any{"result": "passed"}),
		"id", runCaseID,
	)).Want(http.StatusConflict)
	testutil.Call(t, testHandler.UpdateTestRunCaseResult, agentResultRequest(runCaseID, taskID, map[string]any{"result": "blocked"})).Want(http.StatusConflict)

	var status string
	dbfx.QueryRow(t, `SELECT status FROM test_run WHERE id = $1`, runID).Scan(&status)
	if status != "aborted" {
		t.Errorf("run status after refused writes = %q, want aborted", status)
	}
}

// A settled round can still be corrected by a person (a mis-clicked result),
// never by an agent task that outlived it.
func TestCompletedRoundTakesCorrectionsOnlyFromPeople(t *testing.T) {
	taskID := uuidToString(dbid.NewV7())
	runID, runCaseID := agentOwnedRunCase(t, taskID)
	setRunCaseResult(t, runCaseID, "failed")
	if got := getTestRun(t, runID); got.Status != "completed" {
		t.Fatalf("run after its only case settled = %q, want completed", got.Status)
	}
	testutil.Call(t, testHandler.UpdateTestRunCaseResult, agentResultRequest(runCaseID, taskID, map[string]any{"result": "blocked"})).Want(http.StatusConflict)
	setRunCaseResult(t, runCaseID, "passed")
}

// The fallback marker settles a case whose agent never made the CLI write, but
// only with a verdict that claims nothing it cannot back: a bare `passed` is
// held as blocked, a `failed` stands.
func TestMarkerFallbackNeverYieldsAPass(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct{ marker, want string }{{"passed", "blocked"}, {"failed", "failed"}} {
		projectID := newTestRunProject(t)
		caseRow := createTestCaseForRun(t, projectID)
		run := createTestRunFromCases(t, "Marker round "+tc.marker, []string{caseRow.ID})
		runtimeID := dbfx.Runtime(t, "marker-runtime-"+tc.marker, testutil.Cols{"daemon_id": "daemon-marker-" + tc.marker})
		agentID := dbfx.Agent(t, "marker-agent-"+tc.marker, runtimeID)
		dispatchRun(t, run.ID, agentID)
		cases := runCasesInOrder(t, run.ID)
		if cases[0].taskID == nil {
			t.Fatalf("dispatch left the case without a task")
		}
		task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(*cases[0].taskID))
		if err != nil {
			t.Fatal(err)
		}
		output := "TEST_RUN_CASE_RESULT_JSON: {\"result\":\"" + tc.marker + "\",\"summary\":\"the total looked right\"}"
		if err := testHandler.completeTestRunTask(ctx, testHandler.Queries, task, output); err != nil {
			t.Fatal(err)
		}
		var result, notes string
		dbfx.QueryRow(t, `SELECT result, notes FROM test_run_case WHERE id = $1`, cases[0].id).Scan(&result, &notes)
		if result != tc.want || !strings.Contains(notes, "the total looked right") {
			t.Errorf("marker %s settled as %s / %q, want %s keeping the summary", tc.marker, result, notes, tc.want)
		}
	}
}

// A reviewer checks a verdict against what the executor uploaded for it; the
// list is per run case and carries nothing from a sibling case.
func TestListTestRunCaseEvidenceReturnsOnlyThatCasesFiles(t *testing.T) {
	_, mine := agentOwnedRunCase(t, uuidToString(dbid.NewV7()))
	_, sibling := agentOwnedRunCase(t, uuidToString(dbid.NewV7()))
	addRunCaseEvidence(t, mine)
	addRunCaseEvidence(t, sibling)

	var out struct {
		Evidence []AttachmentResponse `json:"evidence"`
	}
	testutil.Call(t, testHandler.ListTestRunCaseEvidence, withURLParam(
		newRequest("GET", "/api/test-run-cases/"+mine+"/evidence?workspace_id="+testWorkspaceID, nil), "id", mine,
	)).Want(http.StatusOK).JSON(&out)
	if len(out.Evidence) != 1 || out.Evidence[0].Filename != "final.png" || out.Evidence[0].DownloadURL == "" {
		t.Fatalf("evidence = %+v, want this case's one file with a download URL", out.Evidence)
	}
}
