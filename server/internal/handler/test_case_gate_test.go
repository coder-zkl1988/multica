package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// An active case is the thing a run checks the product against. With no step,
// or a step whose expected result is blank, an execution can only ever come
// back "passed" — so a case becomes active only once it is executable.

func TestCreateTestCaseWithoutExecutableStepsStartsAsDraft(t *testing.T) {
	projectID := newTestCaseProject(t)
	// The issue page's "new case" button sends a title only.
	created := createTestCaseForTest(t, map[string]any{"project_id": projectID, "title": "Coupon stacking"})
	if created.Status != "draft" {
		t.Errorf("a case with no steps started as %q, want draft", created.Status)
	}
	executable := createTestCaseForTest(t, map[string]any{"project_id": projectID, "title": "Coupon applies", "steps": executableSteps})
	if executable.Status != "active" {
		t.Errorf("a case with executable steps started as %q, want active", executable.Status)
	}
}

func TestCreateActiveTestCaseNeedsExecutableSteps(t *testing.T) {
	projectID := newTestCaseProject(t)
	for _, tc := range []struct {
		name  string
		steps []map[string]any
		want  string
	}{
		{"no steps", nil, "at least one step"},
		{"step without expected", []map[string]any{{"action": "Open the cart"}}, "step 1 has no expected result"},
		{"step without action", []map[string]any{{"action": "Open the cart", "expected": "One item"}, {"expected": "Paid"}}, "step 2 has no action"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := testutil.Call(t, testHandler.CreateTestCase, newRequest("POST", "/api/test-cases?workspace_id="+testWorkspaceID, map[string]any{
				"project_id": projectID, "title": "Active " + tc.name, "status": "active", "steps": tc.steps,
			})).Want(http.StatusBadRequest)
			if !strings.Contains(resp.Text(), tc.want) {
				t.Errorf("refused with %q, want it to say %q", resp.Text(), tc.want)
			}
		})
	}
}

func TestUpdateActiveTestCaseCannotDropItsSteps(t *testing.T) {
	projectID := newTestCaseProject(t)
	active := createTestCaseForTest(t, map[string]any{"project_id": projectID, "title": "Coupon applies", "steps": executableSteps})
	testutil.Call(t, testHandler.UpdateTestCase, withURLParam(newRequest("PUT", "/api/test-cases/"+active.Key+"?workspace_id="+testWorkspaceID,
		map[string]any{"steps": []map[string]any{}}), "ref", active.Key)).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.UpdateTestCase, withURLParam(newRequest("PUT", "/api/test-cases/"+active.Key+"?workspace_id="+testWorkspaceID,
		map[string]any{"steps": []map[string]any{{"action": "Open the cart", "expected": ""}}}), "ref", active.Key)).Want(http.StatusBadRequest)
	// Other fields stay editable without restating the steps.
	testutil.Call(t, testHandler.UpdateTestCase, withURLParam(newRequest("PUT", "/api/test-cases/"+active.Key+"?workspace_id="+testWorkspaceID,
		map[string]any{"title": "Coupon applies once"}), "ref", active.Key)).Want(http.StatusOK)

	// Activating a step-less draft by status is the same promotion as approve.
	draft := createTestCaseForTest(t, map[string]any{"project_id": projectID, "title": "Coupon stacking"})
	testutil.Call(t, testHandler.UpdateTestCase, withURLParam(newRequest("PUT", "/api/test-cases/"+draft.Key+"?workspace_id="+testWorkspaceID,
		map[string]any{"status": "active"}), "ref", draft.Key)).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.UpdateTestCase, withURLParam(newRequest("PUT", "/api/test-cases/"+draft.Key+"?workspace_id="+testWorkspaceID,
		map[string]any{"status": "active", "steps": executableSteps}), "ref", draft.Key)).Want(http.StatusOK)
}

func TestApproveTestCaseNeedsExecutableSteps(t *testing.T) {
	projectID := newTestCaseProject(t)
	draft := createTestCaseForTest(t, map[string]any{"project_id": projectID, "title": "Coupon stacking", "status": "draft"})
	resp := testutil.Call(t, testHandler.ApproveTestCase, withURLParam(newRequest("POST", "/api/test-cases/"+draft.Key+"/approve?workspace_id="+testWorkspaceID, nil), "ref", draft.Key)).
		Want(http.StatusBadRequest)
	if !strings.Contains(resp.Text(), "step") {
		t.Errorf("approve refused with %q, want the reason to name the steps", resp.Text())
	}
}

// The editor sends the steps with every save; re-sending a legacy active
// case's unchanged (empty) steps must not block an unrelated title fix.
func TestLegacyActiveCaseStaysEditable(t *testing.T) {
	projectID := newTestCaseProject(t)
	legacy := createTestCaseForTest(t, map[string]any{"project_id": projectID, "title": "Legacy case", "steps": executableSteps})
	dbfx.Exec(t, `UPDATE test_case SET steps = '[]'::jsonb WHERE id = $1`, legacy.ID)
	testutil.Call(t, testHandler.UpdateTestCase, withURLParam(newRequest("PUT", "/api/test-cases/"+legacy.Key+"?workspace_id="+testWorkspaceID,
		map[string]any{"title": "Legacy case, renamed", "steps": []map[string]any{}}), "ref", legacy.Key)).Want(http.StatusOK)
	// Changing the steps still has to leave it executable.
	testutil.Call(t, testHandler.UpdateTestCase, withURLParam(newRequest("PUT", "/api/test-cases/"+legacy.Key+"?workspace_id="+testWorkspaceID,
		map[string]any{"steps": []map[string]any{{"action": "Open the page"}}}), "ref", legacy.Key)).Want(http.StatusBadRequest)
}

// A person's case created from a title alone is a draft only because there
// was nothing to check yet. The save that gives it executable steps makes it
// live; otherwise it would sit outside every round and coverage count.
func TestPersonsDraftGoesLiveWhenItsStepsAreFilledIn(t *testing.T) {
	projectID := newTestCaseProject(t)
	update := func(key string, body map[string]any, headers ...string) TestCaseResponse {
		req := withURLParam(newRequest("PUT", "/api/test-cases/"+key+"?workspace_id="+testWorkspaceID, body), "ref", key)
		for i := 0; i+1 < len(headers); i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
		var out TestCaseResponse
		testutil.Call(t, testHandler.UpdateTestCase, req).Want(http.StatusOK).JSON(&out)
		return out
	}

	filled := createTestCaseForTest(t, map[string]any{"project_id": projectID, "title": "Coupon stacking"})
	if got := update(filled.Key, map[string]any{"steps": executableSteps}); got.Status != "active" {
		t.Errorf("a person's draft with its steps filled in is %q, want active", got.Status)
	}

	// A save that names the status keeps the author's choice.
	parked := createTestCaseForTest(t, map[string]any{"project_id": projectID, "title": "Coupon expiry"})
	if got := update(parked.Key, map[string]any{"steps": executableSteps, "status": "draft"}); got.Status != "draft" {
		t.Errorf("a draft saved with status draft is %q, want draft", got.Status)
	}

	// Editing a draft that already had executable steps is not the moment it
	// became checkable; it stays where its author left it.
	explicit := createTestCaseForTest(t, map[string]any{"project_id": projectID, "title": "Coupon limits", "status": "draft", "steps": executableSteps})
	if got := update(explicit.Key, map[string]any{"steps": []map[string]any{{"action": "Open the cart", "expected": "Two coupons"}}}); got.Status != "draft" {
		t.Errorf("an explicitly parked draft became %q after an edit, want draft", got.Status)
	}

	// An AI-written draft still waits for a member's approval.
	agentDraft := createTestCaseAsAgent(t, map[string]any{"project_id": projectID, "title": "Coupon reuse"})
	if got := update(agentDraft.Key, map[string]any{"steps": executableSteps}); got.Status != "draft" {
		t.Errorf("an AI draft with its steps filled in by a member is %q, want draft until approved", got.Status)
	}

	// A draft its author created as such, steps or not, was parked on
	// purpose.
	parkedEmpty := createTestCaseForTest(t, map[string]any{"project_id": projectID, "title": "Coupon audit", "status": "draft"})
	if got := update(parkedEmpty.Key, map[string]any{"steps": executableSteps}); got.Status != "draft" {
		t.Errorf("a draft created as a draft is %q after its steps were filled, want draft", got.Status)
	}
	// Once the author has settled the status, a later save does not
	// promote it either.
	if got := update(parked.Key, map[string]any{"title": "Coupon expiry, edited", "steps": []map[string]any{{"action": "Open the cart", "expected": "Coupon expired"}}}); got.Status != "draft" {
		t.Errorf("a draft whose author kept it a draft is %q after a later save, want draft", got.Status)
	}
	// Steps an agent wrote into a person's draft are no longer only the
	// person's: the case waits for approval.
	filledByAgent := createTestCaseForTest(t, map[string]any{"project_id": projectID, "title": "Coupon stacking limits"})
	update(filledByAgent.Key, map[string]any{"steps": []map[string]any{{"action": "Stack two coupons", "expected": ""}}}, testCaseAgentHeaders...)
	if got := update(filledByAgent.Key, map[string]any{"steps": executableSteps}); got.Status != "draft" {
		t.Errorf("a draft an agent wrote steps into is %q after a person's save, want draft", got.Status)
	}
}

var testCaseAgentHeaders = []string{"X-Actor-Source", "task_token", "X-Agent-ID", "00000000-0000-0000-0000-0000000000a3"}

func asTestCaseAgent(req *http.Request) *http.Request {
	for i := 0; i+1 < len(testCaseAgentHeaders); i += 2 {
		req.Header.Set(testCaseAgentHeaders[i], testCaseAgentHeaders[i+1])
	}
	return req
}

func createTestCaseAsAgent(t *testing.T, body map[string]any) TestCaseResponse {
	t.Helper()
	var out TestCaseResponse
	testutil.Call(t, testHandler.CreateTestCase, asTestCaseAgent(newRequest("POST", "/api/test-cases?workspace_id="+testWorkspaceID, body))).
		Want(http.StatusCreated).JSON(&out)
	return out
}

// An agent's case is AI-written whichever command wrote it. It lands as a
// draft with origin ai and goes live only when a member approves it — the
// same review gate a generation run's cases pass through.
func TestAgentCaseWaitsForAMembersApproval(t *testing.T) {
	projectID := newTestCaseProject(t)
	draft := createTestCaseAsAgent(t, map[string]any{"project_id": projectID, "title": "Refund to card", "steps": executableSteps})
	if draft.Status != "draft" || draft.Origin != "ai" {
		t.Fatalf("agent case = status %q origin %q, want a draft with origin ai", draft.Status, draft.Origin)
	}
	testutil.Call(t, testHandler.CreateTestCase, asTestCaseAgent(newRequest("POST", "/api/test-cases?workspace_id="+testWorkspaceID,
		map[string]any{"project_id": projectID, "title": "Refund to wallet", "status": "active", "steps": executableSteps}))).
		Want(http.StatusForbidden)

	approvePath := "/api/test-cases/" + draft.Key + "/approve?workspace_id=" + testWorkspaceID
	testutil.Call(t, testHandler.ApproveTestCase, asTestCaseAgent(withURLParam(newRequest("POST", approvePath, nil), "ref", draft.Key))).
		Want(http.StatusForbidden)
	testutil.Call(t, testHandler.UpdateTestCase, asTestCaseAgent(withURLParam(newRequest("PUT", "/api/test-cases/"+draft.Key+"?workspace_id="+testWorkspaceID,
		map[string]any{"status": "active"}), "ref", draft.Key))).Want(http.StatusForbidden)
	// The agent may still refine its own draft.
	testutil.Call(t, testHandler.UpdateTestCase, asTestCaseAgent(withURLParam(newRequest("PUT", "/api/test-cases/"+draft.Key+"?workspace_id="+testWorkspaceID,
		map[string]any{"title": "Refund to the original card"}), "ref", draft.Key))).Want(http.StatusOK)

	var approved TestCaseResponse
	testutil.Call(t, testHandler.ApproveTestCase, withURLParam(newRequest("POST", approvePath, nil), "ref", draft.Key)).
		Want(http.StatusOK).JSON(&approved)
	if approved.Status != "active" {
		t.Fatalf("member approval left the case %q, want active", approved.Status)
	}

	// Who may change the status is answered before what the steps allow.
	stepless := createTestCaseAsAgent(t, map[string]any{"project_id": projectID, "title": "Refund to cash"})
	testutil.Call(t, testHandler.UpdateTestCase, asTestCaseAgent(withURLParam(newRequest("PUT", "/api/test-cases/"+stepless.Key+"?workspace_id="+testWorkspaceID,
		map[string]any{"status": "active"}), "ref", stepless.Key))).Want(http.StatusForbidden)
	// A retired case is brought back by a member, and the agent is told so.
	testutil.Call(t, testHandler.UpdateTestCase, withURLParam(newRequest("PUT", "/api/test-cases/"+draft.Key+"?workspace_id="+testWorkspaceID,
		map[string]any{"status": "deprecated"}), "ref", draft.Key)).Want(http.StatusOK)
	resp := testutil.Call(t, testHandler.UpdateTestCase, asTestCaseAgent(withURLParam(newRequest("PUT", "/api/test-cases/"+draft.Key+"?workspace_id="+testWorkspaceID,
		map[string]any{"status": "active"}), "ref", draft.Key))).Want(http.StatusForbidden)
	if !strings.Contains(resp.Text(), "reactivate") {
		t.Errorf("reactivation refused with %q, want it to say a person reactivates it", resp.Text())
	}
}

// A failing case is the signal a round exists to produce; an agent cannot
// make it go away by retiring the case.
func TestAgentCannotRetireAnActiveCase(t *testing.T) {
	projectID := newTestCaseProject(t)
	active := createTestCaseForTest(t, map[string]any{"project_id": projectID, "title": "Refund keeps the order", "steps": executableSteps})
	testutil.Call(t, testHandler.UpdateTestCase, asTestCaseAgent(withURLParam(newRequest("PUT", "/api/test-cases/"+active.Key+"?workspace_id="+testWorkspaceID,
		map[string]any{"status": "deprecated"}), "ref", active.Key))).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.DeleteTestCase, asTestCaseAgent(withURLParam(newRequest("DELETE", "/api/test-cases/"+active.Key+"?workspace_id="+testWorkspaceID, nil), "ref", active.Key))).
		Want(http.StatusForbidden)

	// Unlinking a live case from what it verifies takes it out of coverage
	// as surely as deprecating it.
	issueID := dbfx.Issue(t, "Refund rules", testutil.Cols{"project_id": projectID})
	t.Cleanup(func() {
		testPool.Exec(t.Context(), `DELETE FROM test_case_test_point WHERE test_point_id IN (SELECT id FROM issue_test_point WHERE issue_id = $1)`, issueID)
		testPool.Exec(t.Context(), `DELETE FROM test_case_issue WHERE issue_id = $1`, issueID)
		testPool.Exec(t.Context(), `DELETE FROM issue_test_point WHERE issue_id = $1`, issueID)
	})
	point := createTestPoint(t, issueID, map[string]any{"title": "A refund keeps the order record"}, testCaseAgentHeaders...)
	linkCaseTestPoints(t, active.Key, []string{point.ID}).Want(http.StatusOK)
	testutil.Call(t, testHandler.UnlinkTestCaseTestPoint, asTestCaseAgent(withURLParams(newRequest("DELETE", "/api/test-cases/"+active.Key+"/test-points/"+point.ID+"?workspace_id="+testWorkspaceID, nil),
		"ref", active.Key, "pointId", point.ID))).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.UnlinkTestCaseIssue, asTestCaseAgent(withURLParams(newRequest("DELETE", "/api/test-cases/"+active.Key+"/issues/"+issueID+"?workspace_id="+testWorkspaceID, nil),
		"ref", active.Key, "issueId", issueID))).Want(http.StatusForbidden)
	// Nor may it drop the condition the live case is checked against.
	testutil.Call(t, testHandler.UpdateTestPoint, asTestCaseAgent(withURLParam(newRequest("PATCH", "/api/test-points/"+point.ID+"?workspace_id="+testWorkspaceID,
		map[string]any{"status": "dropped"}), "pointId", point.ID))).Want(http.StatusForbidden)
	loose := createTestPoint(t, issueID, map[string]any{"title": "A refund is logged"}, testCaseAgentHeaders...)
	testutil.Call(t, testHandler.UpdateTestPoint, asTestCaseAgent(withURLParam(newRequest("PATCH", "/api/test-points/"+loose.ID+"?workspace_id="+testWorkspaceID,
		map[string]any{"status": "dropped"}), "pointId", loose.ID))).Want(http.StatusOK)

	// An AI-written draft nobody approved is the agent's to clean up; a
	// person's draft is not — the delete takes its revisions with it.
	draft := createTestCaseAsAgent(t, map[string]any{"project_id": projectID, "title": "Duplicate refund case"})
	testutil.Call(t, testHandler.DeleteTestCase, asTestCaseAgent(withURLParam(newRequest("DELETE", "/api/test-cases/"+draft.Key+"?workspace_id="+testWorkspaceID, nil), "ref", draft.Key))).
		Want(http.StatusOK)
	persons := createTestCaseForTest(t, map[string]any{"project_id": projectID, "title": "Refund to a closed card"})
	testutil.Call(t, testHandler.DeleteTestCase, asTestCaseAgent(withURLParam(newRequest("DELETE", "/api/test-cases/"+persons.Key+"?workspace_id="+testWorkspaceID, nil), "ref", persons.Key))).
		Want(http.StatusForbidden)
}

// A plan's cases, rule and status decide what every later round runs.
func TestAgentCannotNarrowARegressionPlan(t *testing.T) {
	projectID := newTestRunProject(t)
	plan := createTestPlanForRun(t, projectID)
	tc := createTestCaseForRun(t, projectID)
	addCaseToPlan(t, plan.ID, tc.ID, 1)
	planPath := "/api/test-plans/" + plan.ID + "?workspace_id=" + testWorkspaceID
	testutil.Call(t, testHandler.RemoveTestPlanCase, asTestCaseAgent(withURLParams(newRequest("DELETE", "/api/test-plans/"+plan.ID+"/cases/"+tc.ID+"?workspace_id="+testWorkspaceID, nil),
		"id", plan.ID, "caseId", tc.ID))).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.UpdateTestPlan, asTestCaseAgent(withURLParam(newRequest("PUT", planPath, map[string]any{"selector": map[string]any{"modules": []string{"checkout"}}}), "id", plan.ID))).
		Want(http.StatusForbidden)
	testutil.Call(t, testHandler.UpdateTestPlan, asTestCaseAgent(withURLParam(newRequest("PUT", planPath, map[string]any{"status": "archived"}), "id", plan.ID))).
		Want(http.StatusForbidden)
	testutil.Call(t, testHandler.DeleteTestPlan, asTestCaseAgent(withURLParam(newRequest("DELETE", planPath, nil), "id", plan.ID))).
		Want(http.StatusForbidden)
	testutil.Call(t, testHandler.UpdateTestPlan, asTestCaseAgent(withURLParam(newRequest("PUT", planPath, map[string]any{"title": "Nightly refunds"}), "id", plan.ID))).
		Want(http.StatusOK)
}

// The history TS-043 leans on has to say who changed a case.
func TestAgentEditsAreRecordedAsTheAgents(t *testing.T) {
	projectID := newTestCaseProject(t)
	tc := createTestCaseForTest(t, map[string]any{"project_id": projectID, "title": "Refund note", "steps": executableSteps})
	testutil.Call(t, testHandler.UpdateTestCase, asTestCaseAgent(withURLParam(newRequest("PUT", "/api/test-cases/"+tc.Key+"?workspace_id="+testWorkspaceID,
		map[string]any{"title": "Refund note, clarified"}), "ref", tc.Key))).Want(http.StatusOK)
	var changedByType string
	dbfx.QueryRow(t, `SELECT changed_by_type FROM test_case_revision WHERE test_case_id = $1 ORDER BY created_at DESC LIMIT 1`, tc.ID).Scan(&changedByType)
	if changedByType != "agent" {
		t.Fatalf("revision changed_by_type = %q, want agent", changedByType)
	}
}

// A person who drops an AI point has rejected it; a later run may not revive
// it.
func TestDroppingAnAIPointMakesItAPersons(t *testing.T) {
	issueID := dbfx.Issue(t, "Refund points")
	t.Cleanup(func() { testPool.Exec(t.Context(), `DELETE FROM issue_test_point WHERE issue_id = $1`, issueID) })
	point := createTestPoint(t, issueID, map[string]any{"title": "A refund notifies the buyer"}, testCaseAgentHeaders...)
	var dropped TestPointResponse
	testutil.Call(t, testHandler.UpdateTestPoint, withURLParam(newRequest("PATCH", "/api/test-points/"+point.ID+"?workspace_id="+testWorkspaceID,
		map[string]any{"status": "dropped"}), "pointId", point.ID)).Want(http.StatusOK).JSON(&dropped)
	if dropped.Origin != "human" {
		t.Fatalf("origin after a person dropped it = %q, want human", dropped.Origin)
	}
	testutil.Call(t, testHandler.UpdateTestPoint, asTestCaseAgent(withURLParam(newRequest("PATCH", "/api/test-points/"+point.ID+"?workspace_id="+testWorkspaceID,
		map[string]any{"status": "active"}), "pointId", point.ID))).Want(http.StatusForbidden)
}
