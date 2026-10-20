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
