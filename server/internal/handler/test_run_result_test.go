package handler

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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
		if _, err := testHandler.completeTestRunTask(ctx, testHandler.Queries, task, output); err != nil {
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

func uploadRunCaseEvidence(t *testing.T, h *Handler, runCaseID string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "seen.png")
	part.Write([]byte("\\x89PNG\\r\\n\\x1a\\nscreen"))
	writer.WriteField("test_run_case_id", runCaseID)
	writer.WriteField("kind", "screenshot")
	writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/upload-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	w := httptest.NewRecorder()
	h.UploadFile(w, req)
	return w
}

// A person executing a round by hand attaches what they saw, as an agent
// does; without it a manual verdict is as unauditable as a bare agent pass.
func TestMembersCanAttachEvidenceToARunCase(t *testing.T) {
	h := *testHandler
	h.Storage = &mockStorage{}
	projectID := newTestRunProject(t)
	tc := createTestCaseForRun(t, projectID)
	run := createTestRunFromCases(t, "Manual round", []string{tc.ID})
	runCaseID := listRunCases(t, run.ID)[0].ID
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM attachment WHERE test_run_case_id = $1`, runCaseID)
	})

	if w := uploadRunCaseEvidence(t, &h, runCaseID); w.Code != http.StatusOK {
		t.Fatalf("member evidence upload: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Evidence []AttachmentResponse `json:"evidence"`
	}
	testutil.Call(t, testHandler.ListTestRunCaseEvidence, withURLParam(
		newRequest("GET", "/api/test-run-cases/"+runCaseID+"/evidence?workspace_id="+testWorkspaceID, nil), "id", runCaseID,
	)).Want(http.StatusOK).JSON(&out)
	if len(out.Evidence) != 1 || out.Evidence[0].Filename != "seen.png" {
		t.Fatalf("evidence = %+v, want the uploaded file", out.Evidence)
	}

	dbfx.Exec(t, `UPDATE test_run SET status = 'aborted' WHERE id = $1`, run.ID)
	if w := uploadRunCaseEvidence(t, &h, runCaseID); w.Code != http.StatusConflict {
		t.Fatalf("upload to an aborted round: %d, want 409: %s", w.Code, w.Body.String())
	}
}

// Per-case dispatch stamps the first case's task onto the round; that task
// still owns only its own case and cannot record or attach for a sibling.
func TestPerCaseTaskCannotWriteASiblingCase(t *testing.T) {
	ctx := context.Background()
	projectID := newTestRunProject(t)
	a := createTestCaseForRun(t, projectID)
	b := createTestCaseForRun(t, projectID)
	run := createTestRunFromCases(t, "Sibling round", []string{a.ID, b.ID})
	runtimeID := dbfx.Runtime(t, "sibling-runtime", testutil.Cols{"daemon_id": "daemon-sibling"})
	agentID := dbfx.Agent(t, "sibling-agent", runtimeID)
	if w := dispatchRun(t, run.ID, agentID); w.Code != http.StatusCreated {
		t.Fatalf("dispatch: %d %s", w.Code, w.Body.String())
	}
	cases := runCasesInOrder(t, run.ID)
	var roundTask string
	if err := testPool.QueryRow(ctx, `SELECT agent_task_id FROM test_run WHERE id = $1`, run.ID).Scan(&roundTask); err != nil {
		t.Fatal(err)
	}
	if roundTask != *cases[0].taskID {
		t.Fatalf("round task = %s, want the first case's task %s", roundTask, *cases[0].taskID)
	}

	resp := testutil.Call(t, testHandler.UpdateTestRunCaseResult, agentResultRequest(cases[1].id, roundTask, map[string]any{"result": "blocked"})).
		Want(http.StatusForbidden)
	if !strings.Contains(resp.Text(), "another case") {
		t.Errorf("sibling write refused with %q", resp.Text())
	}
	testutil.Call(t, testHandler.UpdateTestRunCaseResult, agentResultRequest(cases[0].id, roundTask, map[string]any{"result": "blocked"})).
		Want(http.StatusOK)

	h := *testHandler
	h.Storage = &mockStorage{}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "x.png")
	part.Write([]byte("\x89PNG\r\n\x1a\n"))
	writer.WriteField("test_run_case_id", cases[1].id)
	writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/upload-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", roundTask)
	w := httptest.NewRecorder()
	h.UploadFile(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("sibling evidence upload: %d, want 403: %s", w.Code, w.Body.String())
	}
}

// executed_at orders a case's results across rounds; a note or a correction
// on an old round fixes that execution's record and must not make it newest.
func TestCorrectingARecordedResultKeepsItsExecutionTime(t *testing.T) {
	projectID := newTestRunProject(t)
	tc := createTestCaseForRun(t, projectID)
	run := createTestRunFromCases(t, "Old round", []string{tc.ID})
	runCaseID := listRunCases(t, run.ID)[0].ID
	setRunCaseResult(t, runCaseID, "failed")
	dbfx.Exec(t, `UPDATE test_run_case SET executed_at = now() - interval '7 days' WHERE id = $1`, runCaseID)
	var before time.Time
	dbfx.QueryRow(t, `SELECT executed_at FROM test_run_case WHERE id = $1`, runCaseID).Scan(&before)

	testutil.Call(t, testHandler.UpdateTestRunCaseResult, withURLParam(
		newRequest("PUT", "/api/test-run-cases/"+runCaseID+"/result?workspace_id="+testWorkspaceID, map[string]any{"result": "failed", "notes": "flaky on retry"}),
		"id", runCaseID,
	)).Want(http.StatusOK)
	var after time.Time
	dbfx.QueryRow(t, `SELECT executed_at FROM test_run_case WHERE id = $1`, runCaseID).Scan(&after)
	if !after.Equal(before) {
		t.Fatalf("executed_at moved from %s to %s on a note save", before, after)
	}
}

// A write without step results keeps the stored ones, so its verdict must
// agree with them: no pass over a recorded failed step.
func TestVerdictMustAgreeWithStoredStepResults(t *testing.T) {
	projectID := newTestRunProject(t)
	tc := createTestCaseForRun(t, projectID)
	run := createTestRunFromCases(t, "Stored steps", []string{tc.ID})
	runCaseID := listRunCases(t, run.ID)[0].ID
	testutil.Call(t, testHandler.UpdateTestRunCaseResult, withURLParam(
		newRequest("PUT", "/api/test-run-cases/"+runCaseID+"/result?workspace_id="+testWorkspaceID, map[string]any{
			"result": "failed", "step_results": []map[string]any{{"index": 1, "result": "failed", "actual": "blank page"}},
		}), "id", runCaseID,
	)).Want(http.StatusOK)
	resp := testutil.Call(t, testHandler.UpdateTestRunCaseResult, withURLParam(
		newRequest("PUT", "/api/test-run-cases/"+runCaseID+"/result?workspace_id="+testWorkspaceID, map[string]any{"result": "passed"}), "id", runCaseID,
	)).Want(http.StatusBadRequest)
	if !strings.Contains(resp.Text(), "step 1 is failed") {
		t.Errorf("refused with %q", resp.Text())
	}
	testutil.Call(t, testHandler.UpdateTestRunCaseResult, withURLParam(
		newRequest("PUT", "/api/test-run-cases/"+runCaseID+"/result?workspace_id="+testWorkspaceID, map[string]any{
			"result": "passed", "step_results": []map[string]any{{"index": 1, "result": "passed"}},
		}), "id", runCaseID,
	)).Want(http.StatusOK)
}

// A case frozen before steps were required gives an agent nothing to check.
func TestAgentCannotPassACaseFrozenWithoutSteps(t *testing.T) {
	taskID := uuidToString(dbid.NewV7())
	_, runCaseID := agentOwnedRunCase(t, taskID)
	dbfx.Exec(t, `UPDATE test_run_case SET case_snapshot = case_snapshot - 'steps' WHERE id = $1`, runCaseID)
	addRunCaseEvidence(t, runCaseID)
	resp := testutil.Call(t, testHandler.UpdateTestRunCaseResult, agentResultRequest(runCaseID, taskID, map[string]any{"result": "passed"})).
		Want(http.StatusBadRequest)
	if !strings.Contains(resp.Text(), "frozen without steps") {
		t.Errorf("refused with %q", resp.Text())
	}
	testutil.Call(t, testHandler.UpdateTestRunCaseResult, agentResultRequest(runCaseID, taskID, map[string]any{"result": "blocked", "notes": "no steps"})).
		Want(http.StatusOK)
}

// A stale task cannot change the evidence of a round that settled.
func TestAgentEvidenceUploadRefusedOnSettledRounds(t *testing.T) {
	taskID := uuidToString(dbid.NewV7())
	runID, runCaseID := agentOwnedRunCase(t, taskID)
	var executor string
	dbfx.QueryRow(t, `SELECT executor_id FROM test_run WHERE id = $1`, runID).Scan(&executor)
	dbfx.Exec(t, `UPDATE test_run SET status = 'aborted' WHERE id = $1`, runID)
	h := *testHandler
	h.Storage = &mockStorage{}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "late.png")
	part.Write([]byte("\x89PNG\r\n\x1a\n"))
	writer.WriteField("test_run_case_id", runCaseID)
	writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/upload-file", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", executor)
	req.Header.Set("X-Task-ID", taskID)
	w := httptest.NewRecorder()
	h.UploadFile(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("upload into an aborted round: %d, want 409: %s", w.Code, w.Body.String())
	}
}

// The hooks' fallback settle never overwrites a verdict recorded meanwhile,
// and in an aborted round it leaves executed_at unset.
func TestHookSettleOnlyTouchesOpenCases(t *testing.T) {
	ctx := context.Background()
	projectID := newTestRunProject(t)
	tc := createTestCaseForRun(t, projectID)
	run := createTestRunFromCases(t, "Hook settle", []string{tc.ID})
	runCaseID := listRunCases(t, run.ID)[0].ID
	setRunCaseResult(t, runCaseID, "passed")
	dbRun, err := testHandler.Queries.GetTestRunInWorkspace(ctx, db.GetTestRunInWorkspaceParams{ID: parseUUID(run.ID), WorkspaceID: parseUUID(testWorkspaceID)})
	if err != nil {
		t.Fatal(err)
	}
	rc, err := testHandler.Queries.GetTestRunCaseInWorkspace(ctx, db.GetTestRunCaseInWorkspaceParams{ID: parseUUID(runCaseID), WorkspaceID: parseUUID(testWorkspaceID)})
	if err != nil {
		t.Fatal(err)
	}
	// The hook read the case while it was still running.
	rc.Result = "running"
	settled, err := settleOpenTestRunCase(ctx, testHandler.Queries, dbRun, rc, "blocked", "never recorded")
	if err != nil || settled != nil {
		t.Fatalf("settle over a recorded pass = %v, %v; want no write", settled, err)
	}
	var result string
	dbfx.QueryRow(t, `SELECT result FROM test_run_case WHERE id = $1`, runCaseID).Scan(&result)
	if result != "passed" {
		t.Fatalf("result = %s, want the recorded pass kept", result)
	}

	second := createTestCaseForRun(t, projectID)
	aborted := createTestRunFromCases(t, "Aborted hook settle", []string{second.ID})
	abortedCase := listRunCases(t, aborted.ID)[0].ID
	dbfx.Exec(t, `UPDATE test_run SET status = 'aborted' WHERE id = $1`, aborted.ID)
	abortedRun, _ := testHandler.Queries.GetTestRunInWorkspace(ctx, db.GetTestRunInWorkspaceParams{ID: parseUUID(aborted.ID), WorkspaceID: parseUUID(testWorkspaceID)})
	abortedRC, _ := testHandler.Queries.GetTestRunCaseInWorkspace(ctx, db.GetTestRunCaseInWorkspaceParams{ID: parseUUID(abortedCase), WorkspaceID: parseUUID(testWorkspaceID)})
	if settled, err := settleOpenTestRunCase(ctx, testHandler.Queries, abortedRun, abortedRC, "blocked", "task died"); err != nil || settled == nil {
		t.Fatalf("settle in an aborted round = %v, %v; want the case closed", settled, err)
	}
	var executedAt *time.Time
	dbfx.QueryRow(t, `SELECT executed_at FROM test_run_case WHERE id = $1`, abortedCase).Scan(&executedAt)
	if executedAt != nil {
		t.Fatalf("executed_at = %v in an aborted round, want unset so it is never the latest result", executedAt)
	}
}

// A converger holding a stale snapshot cannot complete a round that was
// aborted in between, nor complete one twice.
func TestConvergeNeverRevivesAnAbortedRound(t *testing.T) {
	ctx := context.Background()
	projectID := newTestRunProject(t)
	tc := createTestCaseForRun(t, projectID)
	run := createTestRunFromCases(t, "Converge race", []string{tc.ID})
	runCaseID := listRunCases(t, run.ID)[0].ID
	dbfx.Exec(t, `UPDATE test_run SET status = 'running' WHERE id = $1`, run.ID)
	stale, err := testHandler.Queries.GetTestRunInWorkspace(ctx, db.GetTestRunInWorkspaceParams{ID: parseUUID(run.ID), WorkspaceID: parseUUID(testWorkspaceID)})
	if err != nil {
		t.Fatal(err)
	}
	dbfx.Exec(t, `UPDATE test_run_case SET result = 'passed' WHERE id = $1`, runCaseID)
	dbfx.Exec(t, `UPDATE test_run SET status = 'aborted' WHERE id = $1`, run.ID)
	completed, err := testHandler.convergeTestRun(ctx, testHandler.Queries, stale)
	if err != nil || completed != nil {
		t.Fatalf("converge over an abort = %v, %v; want nothing", completed, err)
	}
	var status string
	dbfx.QueryRow(t, `SELECT status FROM test_run WHERE id = $1`, run.ID).Scan(&status)
	if status != "aborted" {
		t.Fatalf("status = %s, want aborted kept", status)
	}

	dbfx.Exec(t, `UPDATE test_run SET status = 'running' WHERE id = $1`, run.ID)
	first, err := testHandler.convergeTestRun(ctx, testHandler.Queries, stale)
	if err != nil || first == nil {
		t.Fatalf("first converge = %v, %v; want it to complete the round", first, err)
	}
	second, err := testHandler.convergeTestRun(ctx, testHandler.Queries, stale)
	if err != nil || second != nil {
		t.Fatalf("second converge = %v, %v; want only one completion", second, err)
	}
}
