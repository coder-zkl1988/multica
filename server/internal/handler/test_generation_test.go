package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Fixed task ids for the writeback gate. They never have to exist as rows: the
// gate compares the job's agent_task_id against the middleware-injected header,
// which is exactly the check under test.
const (
	testGenerationTaskUUID      = "11111111-2222-3333-4444-555555555555"
	otherTestGenerationTaskUUID = "99999999-8888-7777-6666-555555555555"
)

// newTestGenerationJob creates a job row directly so tests do not have to stand
// up an agent with a live runtime just to exercise the review gates.
func newTestGenerationJob(t *testing.T, projectID string) string {
	t.Helper()

	var jobID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO test_generation_job (workspace_id, project_id, status, input)
		VALUES ($1, $2, 'queued', '{}')
		RETURNING id
	`, testWorkspaceID, projectID).Scan(&jobID); err != nil {
		t.Fatalf("create fixture generation job: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM test_case_proposal WHERE job_id = $1`, jobID)
		testPool.Exec(ctx, `DELETE FROM test_generation_plan WHERE job_id = $1`, jobID)
		testPool.Exec(ctx, `DELETE FROM test_generation_job WHERE id = $1`, jobID)
	})
	return jobID
}

func generatePlanForTest(t *testing.T, jobID string) TestGenerationPlanResponse {
	t.Helper()

	w := httptest.NewRecorder()
	req := withURLParam(newRequest("POST", "/api/test-generation-jobs/"+jobID+"/plan/generate?workspace_id="+testWorkspaceID, nil), "id", jobID)
	testHandler.GenerateTestGenerationPlan(w, req)
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("generate plan status = %d, want 200/201: %s", w.Code, w.Body.String())
	}
	var resp TestGenerationPlanResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	return resp
}

func approvePlanForTest(t *testing.T, jobID string) {
	t.Helper()

	w := httptest.NewRecorder()
	req := withURLParam(newRequest("POST", "/api/test-generation-jobs/"+jobID+"/plan/approve?workspace_id="+testWorkspaceID, nil), "id", jobID)
	testHandler.ApproveTestGenerationPlan(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("approve plan status = %d, want 200: %s", w.Code, w.Body.String())
	}
}

// bindJobToTask fakes a dispatch by pinning the job to an agent task id, which
// is what the writeback gate checks.
func bindJobToTask(t *testing.T, jobID, taskID string) {
	t.Helper()

	if _, err := testPool.Exec(context.Background(),
		`UPDATE test_generation_job SET agent_task_id = $2 WHERE id = $1`, jobID, taskID); err != nil {
		t.Fatalf("bind job to task: %v", err)
	}
}

func proposeRequest(t *testing.T, jobID, taskID string, items []map[string]any) *httptest.ResponseRecorder {
	t.Helper()

	req := withURLParam(newRequest("POST", "/api/test-generation-jobs/"+jobID+"/propose?workspace_id="+testWorkspaceID,
		map[string]any{"items": items}), "id", jobID)
	if taskID != "" {
		req.Header.Set("X-Actor-Source", "task_token")
		req.Header.Set("X-Task-ID", taskID)
	}
	w := httptest.NewRecorder()
	testHandler.ProposeTestCases(w, req)
	return w
}

func TestGenerateTestGenerationPlanSeedsScopeFromProjectResources(t *testing.T) {
	projectID := newTestCaseProject(t)
	newTestCaseRepoResource(t, projectID, "https://github.com/acme/billing-api.git")
	jobID := newTestGenerationJob(t, projectID)

	plan := generatePlanForTest(t, jobID)
	if plan.Status != "draft" {
		t.Fatalf("plan status = %q, want draft", plan.Status)
	}
	repos, _ := plan.Plan["repos"].([]any)
	if len(repos) != 1 {
		t.Fatalf("plan repos = %v, want the project's one github_repo resource", plan.Plan["repos"])
	}
	// The whole point of the feature is coverage beyond code, so the default
	// scope has to ask for business-level case types.
	types, _ := plan.Plan["expected_case_types"].([]any)
	if len(types) == 0 {
		t.Fatalf("plan expected_case_types is empty; generated cases would default to code-level only")
	}
}

func TestUpdateTestGenerationPlanRejectsNonDraft(t *testing.T) {
	projectID := newTestCaseProject(t)
	newTestCaseRepoResource(t, projectID, "https://github.com/acme/billing-api.git")
	jobID := newTestGenerationJob(t, projectID)
	generatePlanForTest(t, jobID)
	approvePlanForTest(t, jobID)

	w := httptest.NewRecorder()
	req := withURLParam(newRequest("PUT", "/api/test-generation-jobs/"+jobID+"/plan?workspace_id="+testWorkspaceID,
		map[string]any{"review_notes": "late edit"}), "id", jobID)
	testHandler.UpdateTestGenerationPlan(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 once the plan is approved: %s", w.Code, w.Body.String())
	}
}

func TestApproveTestGenerationPlanRejectsEmptyScope(t *testing.T) {
	// A project with no repositories and no documents gives the agent nothing
	// to read; approving would burn a run for certain.
	projectID := newTestCaseProject(t)
	jobID := newTestGenerationJob(t, projectID)
	generatePlanForTest(t, jobID)

	w := httptest.NewRecorder()
	req := withURLParam(newRequest("POST", "/api/test-generation-jobs/"+jobID+"/plan/approve?workspace_id="+testWorkspaceID, nil), "id", jobID)
	testHandler.ApproveTestGenerationPlan(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an empty scope: %s", w.Code, w.Body.String())
	}
}

func TestDispatchTestGenerationJobRequiresApprovedPlan(t *testing.T) {
	projectID := newTestCaseProject(t)
	newTestCaseRepoResource(t, projectID, "https://github.com/acme/billing-api.git")
	jobID := newTestGenerationJob(t, projectID)
	generatePlanForTest(t, jobID)

	w := httptest.NewRecorder()
	req := withURLParam(newRequest("POST", "/api/test-generation-jobs/"+jobID+"/dispatch?workspace_id="+testWorkspaceID,
		map[string]any{"agent_id": createHandlerTestAgent(t, "gen-agent-"+jobID[:8], nil)}), "id", jobID)
	testHandler.DispatchTestGenerationJob(w, req)

	// There is deliberately no skip_plan escape hatch: review is the feature.
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 before the plan is approved: %s", w.Code, w.Body.String())
	}
}

func TestDispatchTestGenerationJobRejectsWhileRunning(t *testing.T) {
	projectID := newTestCaseProject(t)
	newTestCaseRepoResource(t, projectID, "https://github.com/acme/billing-api.git")
	jobID := newTestGenerationJob(t, projectID)
	generatePlanForTest(t, jobID)
	approvePlanForTest(t, jobID)
	if _, err := testPool.Exec(context.Background(),
		`UPDATE test_generation_job SET status = 'running' WHERE id = $1`, jobID); err != nil {
		t.Fatalf("mark job running: %v", err)
	}

	w := httptest.NewRecorder()
	req := withURLParam(newRequest("POST", "/api/test-generation-jobs/"+jobID+"/dispatch?workspace_id="+testWorkspaceID,
		map[string]any{"agent_id": createHandlerTestAgent(t, "gen-agent-"+jobID[:8], nil)}), "id", jobID)
	testHandler.DispatchTestGenerationJob(w, req)

	// design_restore skips this check and orphans the first agent task.
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 for a second dispatch: %s", w.Code, w.Body.String())
	}
}

func TestUpdateTestGenerationJobKeepsErrorOnPartialUpdate(t *testing.T) {
	projectID := newTestCaseProject(t)
	jobID := newTestGenerationJob(t, projectID)
	ctx := context.Background()
	if _, err := testPool.Exec(ctx,
		`UPDATE test_generation_job SET error = 'runtime offline' WHERE id = $1`, jobID); err != nil {
		t.Fatalf("seed error: %v", err)
	}

	// A status-only update must not blank the error column. The design_restore
	// equivalent omits COALESCE here and silently loses the failure reason.
	if _, err := testPool.Exec(ctx, `
		UPDATE test_generation_job
		SET status = COALESCE($2, status), error = COALESCE($3, error), updated_at = now()
		WHERE id = $1
	`, jobID, "running", nil); err != nil {
		t.Fatalf("partial update: %v", err)
	}

	var storedError *string
	if err := testPool.QueryRow(ctx, `SELECT error FROM test_generation_job WHERE id = $1`, jobID).Scan(&storedError); err != nil {
		t.Fatalf("read back error: %v", err)
	}
	if storedError == nil || *storedError != "runtime offline" {
		t.Fatalf("error = %v, want it preserved across a status-only update", storedError)
	}
}

func TestProposeTestCasesRequiresTaskToken(t *testing.T) {
	projectID := newTestCaseProject(t)
	jobID := newTestGenerationJob(t, projectID)
	bindJobToTask(t, jobID, testGenerationTaskUUID)

	w := proposeRequest(t, jobID, "", []map[string]any{
		{"kind": "new", "case": map[string]any{"title": "should never land", "steps": executableSteps}},
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 without a task token: %s", w.Code, w.Body.String())
	}
}

func TestProposeTestCasesRejectsForeignTaskToken(t *testing.T) {
	projectID := newTestCaseProject(t)
	jobID := newTestGenerationJob(t, projectID)
	bindJobToTask(t, jobID, testGenerationTaskUUID)

	// A run authorized for another task must not be able to write into this job.
	w := proposeRequest(t, jobID, otherTestGenerationTaskUUID, []map[string]any{
		{"kind": "new", "case": map[string]any{"title": "cross-job write", "steps": executableSteps}},
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a token bound to a different task: %s", w.Code, w.Body.String())
	}
}

func TestProposeTestCasesCreatesDraftAICases(t *testing.T) {
	projectID := newTestCaseProject(t)
	jobID := newTestGenerationJob(t, projectID)
	bindJobToTask(t, jobID, testGenerationTaskUUID)

	w := proposeRequest(t, jobID, testGenerationTaskUUID, []map[string]any{
		{"kind": "new", "case": map[string]any{
			"title":     "调价后进行中订单仍按原价结算",
			"case_type": "business_flow",
			"steps":     []map[string]any{{"action": "后台调价", "expected": "保存成功"}},
		}},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Stats     map[string]int     `json:"stats"`
		TestCases []TestCaseResponse `json:"test_cases"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.TestCases) != 1 {
		t.Fatalf("test_cases = %d, want 1", len(resp.TestCases))
	}
	created := resp.TestCases[0]
	if created.Status != "draft" || created.Origin != "ai" {
		t.Fatalf("status/origin = %q/%q, want draft/ai — generated cases are unreviewed by definition", created.Status, created.Origin)
	}
	if resp.Stats["new"] != 1 {
		t.Fatalf("stats.new = %d, want 1", resp.Stats["new"])
	}
}

func TestProposeTestCasesQueuesProposalForApprovedCase(t *testing.T) {
	projectID := newTestCaseProject(t)
	jobID := newTestGenerationJob(t, projectID)
	bindJobToTask(t, jobID, testGenerationTaskUUID)
	// An active case has been through human review; rewriting it silently would
	// make that review meaningless.
	existing := createTestCaseForTest(t, map[string]any{
		"project_id": projectID,
		"title":      "原始用例",
		"steps":      executableSteps,
	})

	w := proposeRequest(t, jobID, testGenerationTaskUUID, []map[string]any{
		{"kind": "update", "target": existing.Key, "rationale": "接口新增分页参数",
			"case": map[string]any{"title": "原始用例（含分页）", "steps": executableSteps}},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}

	var storedTitle, storedStatus string
	if err := testPool.QueryRow(context.Background(),
		`SELECT title, status FROM test_case WHERE id = $1`, existing.ID).Scan(&storedTitle, &storedStatus); err != nil {
		t.Fatalf("read back case: %v", err)
	}
	if storedTitle != "原始用例" {
		t.Fatalf("title = %q, want the approved case left untouched", storedTitle)
	}

	var pending int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM test_case_proposal WHERE target_case_id = $1 AND status = 'pending'`,
		existing.ID).Scan(&pending); err != nil {
		t.Fatalf("count proposals: %v", err)
	}
	if pending != 1 {
		t.Fatalf("pending proposals = %d, want 1", pending)
	}
}

func TestProposeTestCasesRewritesDraftTargetDirectly(t *testing.T) {
	projectID := newTestCaseProject(t)
	jobID := newTestGenerationJob(t, projectID)
	bindJobToTask(t, jobID, testGenerationTaskUUID)
	// Nobody has reviewed a draft yet, so a later run refines it in place.
	// Otherwise every re-run would add review work instead of removing it.
	draft := createTestCaseForTest(t, map[string]any{
		"project_id": projectID,
		"title":      "草稿用例",
		"status":     "draft",
	})

	w := proposeRequest(t, jobID, testGenerationTaskUUID, []map[string]any{
		{"kind": "update", "target": draft.Key, "case": map[string]any{"title": "草稿用例（已细化）", "steps": executableSteps}},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}

	var storedTitle string
	if err := testPool.QueryRow(context.Background(),
		`SELECT title FROM test_case WHERE id = $1`, draft.ID).Scan(&storedTitle); err != nil {
		t.Fatalf("read back case: %v", err)
	}
	if storedTitle != "草稿用例（已细化）" {
		t.Fatalf("title = %q, want the draft rewritten in place", storedTitle)
	}

	var proposals int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM test_case_proposal WHERE target_case_id = $1`, draft.ID).Scan(&proposals); err != nil {
		t.Fatalf("count proposals: %v", err)
	}
	if proposals != 0 {
		t.Fatalf("proposals = %d, want 0 for a draft target", proposals)
	}
}

func TestProposeTestCasesIsAtomicAcrossTheBatch(t *testing.T) {
	projectID := newTestCaseProject(t)
	jobID := newTestGenerationJob(t, projectID)
	bindJobToTask(t, jobID, testGenerationTaskUUID)

	w := proposeRequest(t, jobID, testGenerationTaskUUID, []map[string]any{
		{"kind": "new", "case": map[string]any{"title": "valid first entry", "steps": executableSteps}},
		{"kind": "new", "case": map[string]any{"title": "bad enum", "priority": "urgent", "steps": executableSteps}},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}

	var created int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM test_case WHERE project_id = $1`, projectID).Scan(&created); err != nil {
		t.Fatalf("count cases: %v", err)
	}
	if created != 0 {
		t.Fatalf("test_case rows = %d, want 0 — a bad entry must not leave earlier ones written", created)
	}
}

func TestProposeTestCasesRejectsCrossRepoCaseWithOneRole(t *testing.T) {
	projectID := newTestCaseProject(t)
	resourceID := newTestCaseRepoResource(t, projectID, "https://github.com/acme/api.git")
	jobID := newTestGenerationJob(t, projectID)
	bindJobToTask(t, jobID, testGenerationTaskUUID)

	w := proposeRequest(t, jobID, testGenerationTaskUUID, []map[string]any{
		{"kind": "new", "case": map[string]any{
			"title": "mislabelled cross-repo case",
			"scope": "cross_repo",
			"steps": executableSteps,
			"repos": []map[string]any{
				{"project_resource_id": resourceID, "alias": "api", "role": "under_test"},
			},
		}},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: a cross_repo case with one role has nothing to correlate: %s", w.Code, w.Body.String())
	}
}

func TestAcceptTestCaseProposalAppliesAndSnapshots(t *testing.T) {
	projectID := newTestCaseProject(t)
	jobID := newTestGenerationJob(t, projectID)
	bindJobToTask(t, jobID, testGenerationTaskUUID)
	existing := createTestCaseForTest(t, map[string]any{"project_id": projectID, "title": "原始用例", "steps": executableSteps})

	if w := proposeRequest(t, jobID, testGenerationTaskUUID, []map[string]any{
		{"kind": "update", "target": existing.Key, "case": map[string]any{"title": "采纳后的标题", "steps": executableSteps}},
	}); w.Code != http.StatusCreated {
		t.Fatalf("seed proposal: %s", w.Body.String())
	}
	var proposalID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM test_case_proposal WHERE target_case_id = $1`, existing.ID).Scan(&proposalID); err != nil {
		t.Fatalf("read proposal id: %v", err)
	}

	w := httptest.NewRecorder()
	req := withURLParam(newRequest("POST", "/api/test-case-proposals/"+proposalID+"/accept?workspace_id="+testWorkspaceID, nil), "id", proposalID)
	testHandler.AcceptTestCaseProposal(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("accept status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var storedTitle string
	if err := testPool.QueryRow(context.Background(),
		`SELECT title FROM test_case WHERE id = $1`, existing.ID).Scan(&storedTitle); err != nil {
		t.Fatalf("read back case: %v", err)
	}
	if storedTitle != "采纳后的标题" {
		t.Fatalf("title = %q, want the proposal applied", storedTitle)
	}

	// Accepting must be reversible, so the pre-change state is snapshotted.
	var revisions int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM test_case_revision WHERE test_case_id = $1 AND change_kind = 'proposal_accepted'`,
		existing.ID).Scan(&revisions); err != nil {
		t.Fatalf("count revisions: %v", err)
	}
	if revisions != 1 {
		t.Fatalf("proposal_accepted revisions = %d, want 1", revisions)
	}
}

func TestDeleteTestCaseSweepsProposals(t *testing.T) {
	projectID := newTestCaseProject(t)
	jobID := newTestGenerationJob(t, projectID)
	bindJobToTask(t, jobID, testGenerationTaskUUID)
	existing := createTestCaseForTest(t, map[string]any{"project_id": projectID, "title": "待删除用例", "steps": executableSteps})

	if w := proposeRequest(t, jobID, testGenerationTaskUUID, []map[string]any{
		{"kind": "update", "target": existing.Key, "case": map[string]any{"title": "建议标题", "steps": executableSteps}},
	}); w.Code != http.StatusCreated {
		t.Fatalf("seed proposal: %s", w.Body.String())
	}

	w := httptest.NewRecorder()
	req := withURLParam(newRequest("DELETE", "/api/test-cases/"+existing.Key+"?workspace_id="+testWorkspaceID, nil), "ref", existing.Key)
	testHandler.DeleteTestCase(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200: %s", w.Code, w.Body.String())
	}

	// There is no foreign key, so the delete transaction has to sweep these.
	var orphans int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM test_case_proposal WHERE target_case_id = $1`, existing.ID).Scan(&orphans); err != nil {
		t.Fatalf("count proposals: %v", err)
	}
	if orphans != 0 {
		t.Fatalf("orphan proposals = %d, want 0", orphans)
	}
}

// TestGenerationJobHealsWhenAgentTaskDies covers the read-time reconcile:
// task cancellation has no writeback hook (agent edits, runtime unbinds and
// user cancels all end the task without touching the job), so a queued job
// whose task died must flip to failed on the next read instead of showing
// "queued" forever and wedging the dispatch guard.
func TestGenerationJobHealsWhenAgentTaskDies(t *testing.T) {
	projectID := newTestCaseProject(t)
	jobID := newTestGenerationJob(t, projectID)
	agentID := createHandlerTestAgent(t, "gen-heal-"+jobID[:8], nil)

	ctx := context.Background()
	runtimeID := handlerTestRuntimeID(t)
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, context, completed_at)
		VALUES ($1, $2, 'cancelled', '{}', now())
		RETURNING id
	`, agentID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("create cancelled fixture task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	if _, err := testPool.Exec(ctx, `
		UPDATE test_generation_job SET agent_id = $2, agent_task_id = $3 WHERE id = $1
	`, jobID, agentID, taskID); err != nil {
		t.Fatalf("point job at fixture task: %v", err)
	}

	w := httptest.NewRecorder()
	req := withURLParam(newRequest("GET", "/api/test-generation-jobs/"+jobID+"?workspace_id="+testWorkspaceID, nil), "id", jobID)
	testHandler.GetTestGenerationJob(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Status string  `json:"status"`
		Error  *string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "failed" {
		t.Fatalf("status = %q, want failed after the task died", resp.Status)
	}
	if resp.Error == nil || !strings.Contains(*resp.Error, "cancelled") {
		t.Fatalf("error = %v, want a cancellation explanation", resp.Error)
	}
}

// A generated case nobody can check fills the library with coverage that never
// finds a bug; the writeback refuses one whatever its kind.
func TestProposeTestCasesRejectsUncheckableSteps(t *testing.T) {
	projectID := newTestCaseProject(t)
	jobID := newTestGenerationJob(t, projectID)
	bindJobToTask(t, jobID, testGenerationTaskUUID)

	w := proposeRequest(t, jobID, testGenerationTaskUUID, []map[string]any{
		{"kind": "new", "case": map[string]any{
			"title": "退款后积分回滚",
			"steps": []map[string]any{{"action": "发起退款"}},
		}},
	})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "step 1 has no expected result") {
		t.Fatalf("status = %d, want 400 naming the step without an expected result: %s", w.Code, w.Body.String())
	}
	var landed int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM test_case WHERE project_id = $1`, projectID).Scan(&landed); err != nil {
		t.Fatal(err)
	}
	if landed != 0 {
		t.Fatalf("cases written = %d, want none", landed)
	}
}

// A proposal queued before the steps rule could still carry an empty
// procedure; accepting it must not silently turn an active case unexecutable.
func TestAcceptTestCaseProposalRefusesToEmptyAnActiveCase(t *testing.T) {
	projectID := newTestCaseProject(t)
	jobID := newTestGenerationJob(t, projectID)
	bindJobToTask(t, jobID, testGenerationTaskUUID)
	existing := createTestCaseForTest(t, map[string]any{"project_id": projectID, "title": "原始用例", "steps": executableSteps})

	if w := proposeRequest(t, jobID, testGenerationTaskUUID, []map[string]any{
		{"kind": "update", "target": existing.Key, "case": map[string]any{"title": "旧规则下的建议", "steps": executableSteps}},
	}); w.Code != http.StatusCreated {
		t.Fatalf("seed proposal: %s", w.Body.String())
	}
	var proposalID string
	if err := testPool.QueryRow(context.Background(),
		`UPDATE test_case_proposal SET payload = payload - 'steps' WHERE target_case_id = $1 RETURNING id`, existing.ID).Scan(&proposalID); err != nil {
		t.Fatalf("age the proposal: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.AcceptTestCaseProposal(w, withURLParam(newRequest("POST", "/api/test-case-proposals/"+proposalID+"/accept?workspace_id="+testWorkspaceID, nil), "id", proposalID))
	if w.Code != http.StatusConflict {
		t.Fatalf("accept status = %d, want 409: %s", w.Code, w.Body.String())
	}
	var storedTitle string
	if err := testPool.QueryRow(context.Background(), `SELECT title FROM test_case WHERE id = $1`, existing.ID).Scan(&storedTitle); err != nil {
		t.Fatal(err)
	}
	if storedTitle != "原始用例" {
		t.Fatalf("title = %q, want the case untouched", storedTitle)
	}
}

// A generated case says which conditions of its requirement it verifies, and
// may only claim conditions of the requirements the run was scoped to.
func TestProposeTestCasesLinksDeclaredTestPoints(t *testing.T) {
	projectID := newTestCaseProject(t)
	jobID := newTestGenerationJob(t, projectID)
	bindJobToTask(t, jobID, testGenerationTaskUUID)
	inScope := dbfx.Issue(t, "Balance checkout")
	outOfScope := dbfx.Issue(t, "Unrelated requirement")
	dbfx.Exec(t, `UPDATE test_generation_job SET input = jsonb_build_object('issue_ids', jsonb_build_array($1::text)) WHERE id = $2`, inScope, jobID)
	refusal := createTestPoint(t, inScope, map[string]any{"title": "Paying over the balance is refused", "category": "negative"})
	foreign := createTestPoint(t, outOfScope, map[string]any{"title": "Something else"})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM test_case_test_point WHERE test_point_id IN ($1, $2)`, refusal.ID, foreign.ID)
		testPool.Exec(context.Background(), `DELETE FROM test_case_issue WHERE issue_id IN ($1, $2)`, inScope, outOfScope)
	})

	w := proposeRequest(t, jobID, testGenerationTaskUUID, []map[string]any{
		{"kind": "new", "case": map[string]any{"title": "Over-balance order", "steps": executableSteps, "test_points": []string{foreign.ID}}},
	})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "outside this run's scope") {
		t.Fatalf("out-of-scope point: status = %d, want 400 naming the scope: %s", w.Code, w.Body.String())
	}

	w = proposeRequest(t, jobID, testGenerationTaskUUID, []map[string]any{
		{"kind": "new", "case": map[string]any{"title": "Over-balance order", "case_type": "exception", "steps": executableSteps,
			"test_points": []string{issueIdentifier(t, inScope) + "/TP-1"}}},
		{"kind": "new", "case": map[string]any{"title": "Says nothing about points", "steps": executableSteps}},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}
	var resp struct {
		TestCases     []TestCaseResponse `json:"test_cases"`
		WithoutPoints int                `json:"new_cases_without_test_points"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.WithoutPoints != 1 {
		t.Errorf("new_cases_without_test_points = %d, want 1", resp.WithoutPoints)
	}
	var origin string
	if err := testPool.QueryRow(context.Background(),
		`SELECT origin FROM test_case_test_point WHERE test_case_id = $1 AND test_point_id = $2`, resp.TestCases[0].ID, refusal.ID).Scan(&origin); err != nil || origin != "ai" {
		t.Fatalf("point link of the generated case = %q (%v), want an ai link", origin, err)
	}
}

// A queued proposal against an approved case carries its points as ids and
// links them when a person accepts it.
func TestAcceptTestCaseProposalLinksItsTestPoints(t *testing.T) {
	projectID := newTestCaseProject(t)
	jobID := newTestGenerationJob(t, projectID)
	bindJobToTask(t, jobID, testGenerationTaskUUID)
	issueID := dbfx.Issue(t, "Refund rules")
	point := createTestPoint(t, issueID, map[string]any{"title": "A refund returns spent points"})
	existing := createTestCaseForTest(t, map[string]any{"project_id": projectID, "title": "Refund", "steps": executableSteps})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM test_case_test_point WHERE test_case_id = $1`, existing.ID)
		testPool.Exec(context.Background(), `DELETE FROM test_case_issue WHERE test_case_id = $1`, existing.ID)
	})

	if w := proposeRequest(t, jobID, testGenerationTaskUUID, []map[string]any{
		{"kind": "update", "target": existing.Key, "case": map[string]any{"title": "Refund returns points", "steps": executableSteps,
			"test_points": []string{issueIdentifier(t, issueID) + "/TP-1"}}},
	}); w.Code != http.StatusCreated {
		t.Fatalf("seed proposal: %s", w.Body.String())
	}
	var proposalID, storedRef string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id, payload->'test_points'->>0 FROM test_case_proposal WHERE target_case_id = $1`, existing.ID).Scan(&proposalID, &storedRef); err != nil {
		t.Fatal(err)
	}
	if storedRef != point.ID {
		t.Fatalf("stored point ref = %q, want the point's id %s", storedRef, point.ID)
	}
	w := httptest.NewRecorder()
	testHandler.AcceptTestCaseProposal(w, withURLParam(newRequest("POST", "/api/test-case-proposals/"+proposalID+"/accept?workspace_id="+testWorkspaceID, nil), "id", proposalID))
	if w.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", w.Code, w.Body.String())
	}
	var linked int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM test_case_test_point WHERE test_case_id = $1 AND test_point_id = $2`, existing.ID, point.ID).Scan(&linked); err != nil || linked != 1 {
		t.Fatalf("point links after accept = %d (%v), want 1", linked, err)
	}
}
