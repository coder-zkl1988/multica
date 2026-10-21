package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestCaseProject creates a project in the shared handler test workspace and
// registers cleanup. Test cases are project-scoped, so every case test needs one.
func newTestCaseProject(t *testing.T) string {
	t.Helper()

	ctx := context.Background()
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, created_by)
		VALUES ($1, $2, (SELECT id FROM "user" LIMIT 1))
		RETURNING id
	`, testWorkspaceID, "Test case fixture project").Scan(&projectID); err != nil {
		t.Fatalf("create fixture project: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		testPool.Exec(cleanupCtx, `DELETE FROM test_case_repo WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(cleanupCtx, `DELETE FROM test_case_revision WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(cleanupCtx, `DELETE FROM test_case WHERE project_id = $1`, projectID)
		testPool.Exec(cleanupCtx, `DELETE FROM project_resource WHERE project_id = $1`, projectID)
		testPool.Exec(cleanupCtx, `DELETE FROM project WHERE id = $1`, projectID)
	})
	return projectID
}

// newTestCaseRepoResource attaches a github_repo resource to the project so a
// case has something legitimate to bind to.
func newTestCaseRepoResource(t *testing.T, projectID, url string) string {
	t.Helper()

	var resourceID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project_resource (project_id, workspace_id, resource_type, resource_ref)
		VALUES ($1, $2, 'github_repo', $3)
		RETURNING id
	`, projectID, testWorkspaceID, `{"url":"`+url+`"}`).Scan(&resourceID); err != nil {
		t.Fatalf("create fixture project resource: %v", err)
	}
	return resourceID
}

func createTestCaseForTest(t *testing.T, body map[string]any) TestCaseResponse {
	t.Helper()

	w := httptest.NewRecorder()
	testHandler.CreateTestCase(w, newRequest("POST", "/api/test-cases?workspace_id="+testWorkspaceID, body))
	if w.Code != http.StatusCreated {
		t.Fatalf("create test case status = %d, want 201: %s", w.Code, w.Body.String())
	}
	var resp TestCaseResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode created test case: %v", err)
	}
	return resp
}

func TestCreateTestCaseAllocatesSequentialKeys(t *testing.T) {
	projectID := newTestCaseProject(t)
	body := map[string]any{
		"project_id": projectID,
		"title":      "下单成功",
		"steps": []map[string]any{
			{"action": "点击下单", "expected": "跳转支付页"},
		},
	}

	first := createTestCaseForTest(t, body)
	if !strings.HasPrefix(first.Key, "TC-") {
		t.Fatalf("key = %q, want a TC- prefix", first.Key)
	}
	if first.Origin != "human" {
		t.Fatalf("origin = %q, want human", first.Origin)
	}
	if first.Status != "active" {
		t.Fatalf("status = %q, want active", first.Status)
	}
	if len(first.Steps) != 1 || first.Steps[0].Index != 1 {
		t.Fatalf("steps = %+v, want one step numbered 1", first.Steps)
	}
	if first.Repos == nil {
		t.Fatal("repos must serialize as an empty array, not null")
	}

	second := createTestCaseForTest(t, body)
	if second.CaseNumber != first.CaseNumber+1 {
		t.Fatalf("case_number = %d, want %d", second.CaseNumber, first.CaseNumber+1)
	}
}

func TestCreateTestCaseRenumbersSteps(t *testing.T) {
	projectID := newTestCaseProject(t)
	created := createTestCaseForTest(t, map[string]any{
		"project_id": projectID,
		"title":      "步骤重编号",
		"steps": []map[string]any{
			{"index": 9, "action": "a", "expected": "x"},
			{"index": 3, "action": "b", "expected": "y"},
		},
	})
	if len(created.Steps) != 2 {
		t.Fatalf("steps length = %d, want 2", len(created.Steps))
	}
	if created.Steps[0].Index != 1 || created.Steps[1].Index != 2 {
		t.Fatalf("step indexes = [%d %d], want [1 2]", created.Steps[0].Index, created.Steps[1].Index)
	}
}

func TestCreateTestCaseRejectsUnknownPriority(t *testing.T) {
	projectID := newTestCaseProject(t)
	w := httptest.NewRecorder()
	testHandler.CreateTestCase(w, newRequest("POST", "/api/test-cases?workspace_id="+testWorkspaceID, map[string]any{
		"project_id": projectID,
		"title":      "x",
		"priority":   "urgent",
	}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "p0") {
		t.Fatalf("error should list the valid values, got %s", w.Body.String())
	}
}

func TestCreateTestCaseRejectsForeignProjectResource(t *testing.T) {
	projectID := newTestCaseProject(t)
	otherProjectID := newTestCaseProject(t)
	foreignResourceID := newTestCaseRepoResource(t, otherProjectID, "https://github.com/acme/other")

	w := httptest.NewRecorder()
	testHandler.CreateTestCase(w, newRequest("POST", "/api/test-cases?workspace_id="+testWorkspaceID, map[string]any{
		"project_id": projectID,
		"title":      "跨项目绑定",
		"repos": []map[string]any{
			{"project_resource_id": foreignResourceID, "alias": "other", "role": "under_test"},
		},
	}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "different project") {
		t.Fatalf("error should name the mismatch, got %s", w.Body.String())
	}
}

func TestCreateTestCaseBindsRepositories(t *testing.T) {
	projectID := newTestCaseProject(t)
	adminID := newTestCaseRepoResource(t, projectID, "https://github.com/acme/admin-web")
	mobileID := newTestCaseRepoResource(t, projectID, "https://github.com/acme/mobile-app")

	created := createTestCaseForTest(t, map[string]any{
		"project_id": projectID,
		"title":      "后台调价后移动端展示新价",
		"scope":      "cross_repo",
		"repos": []map[string]any{
			{"project_resource_id": adminID, "alias": "admin-web", "role": "driver"},
			{"project_resource_id": mobileID, "alias": "mobile-app", "role": "verifier", "path_globs": []string{"src/order/**"}},
		},
	})
	if len(created.Repos) != 2 {
		t.Fatalf("repos length = %d, want 2: %+v", len(created.Repos), created.Repos)
	}
	byAlias := map[string]TestCaseRepoResponse{}
	for _, repo := range created.Repos {
		byAlias[repo.Alias] = repo
	}
	if byAlias["admin-web"].Role != "driver" {
		t.Fatalf("admin-web role = %q, want driver", byAlias["admin-web"].Role)
	}
	if globs := byAlias["mobile-app"].PathGlobs; len(globs) != 1 || globs[0] != "src/order/**" {
		t.Fatalf("mobile-app path_globs = %+v, want [src/order/**]", globs)
	}
}

func TestGetTestCaseAcceptsKeyAndUUID(t *testing.T) {
	projectID := newTestCaseProject(t)
	created := createTestCaseForTest(t, map[string]any{"project_id": projectID, "title": "引用解析"})

	for _, ref := range []string{created.Key, created.ID} {
		w := httptest.NewRecorder()
		req := withURLParam(newRequest("GET", "/api/test-cases/"+ref+"?workspace_id="+testWorkspaceID, nil), "ref", ref)
		testHandler.GetTestCase(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("get by %q status = %d, want 200: %s", ref, w.Code, w.Body.String())
		}
		var resp TestCaseResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode get by %q: %v", ref, err)
		}
		if resp.ID != created.ID {
			t.Fatalf("get by %q returned id %s, want %s", ref, resp.ID, created.ID)
		}
	}
}

func TestUpdateTestCaseWritesRevisionSnapshot(t *testing.T) {
	projectID := newTestCaseProject(t)
	created := createTestCaseForTest(t, map[string]any{"project_id": projectID, "title": "原标题"})

	newTitle := "新标题"
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("PUT", "/api/test-cases/"+created.Key+"?workspace_id="+testWorkspaceID,
		map[string]any{"title": newTitle}), "ref", created.Key)
	testHandler.UpdateTestCase(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var updated TestCaseResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	if updated.Title != newTitle {
		t.Fatalf("title = %q, want %q", updated.Title, newTitle)
	}
	if updated.Version != created.Version+1 {
		t.Fatalf("version = %d, want %d", updated.Version, created.Version+1)
	}

	revW := httptest.NewRecorder()
	revReq := withURLParam(newRequest("GET", "/api/test-cases/"+created.Key+"/revisions?workspace_id="+testWorkspaceID, nil), "ref", created.Key)
	testHandler.ListTestCaseRevisions(revW, revReq)
	if revW.Code != http.StatusOK {
		t.Fatalf("list revisions status = %d, want 200: %s", revW.Code, revW.Body.String())
	}
	var revisions struct {
		Revisions []TestCaseRevisionResponse `json:"revisions"`
	}
	if err := json.NewDecoder(revW.Body).Decode(&revisions); err != nil {
		t.Fatalf("decode revisions: %v", err)
	}
	if len(revisions.Revisions) != 1 {
		t.Fatalf("revisions length = %d, want 1", len(revisions.Revisions))
	}
	revision := revisions.Revisions[0]
	if revision.Version != created.Version {
		t.Fatalf("revision version = %d, want the pre-update version %d", revision.Version, created.Version)
	}
	if revision.ChangeKind != "human_edit" {
		t.Fatalf("change_kind = %q, want human_edit", revision.ChangeKind)
	}
	if title, _ := revision.Snapshot["title"].(string); title != "原标题" {
		t.Fatalf("snapshot title = %v, want the pre-update title", revision.Snapshot["title"])
	}
}

func TestUpdateTestCaseRejectsEmptyTitle(t *testing.T) {
	projectID := newTestCaseProject(t)
	created := createTestCaseForTest(t, map[string]any{"project_id": projectID, "title": "有标题"})

	blank := "   "
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("PUT", "/api/test-cases/"+created.Key+"?workspace_id="+testWorkspaceID,
		map[string]any{"title": blank}), "ref", created.Key)
	testHandler.UpdateTestCase(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestUpdateTestCaseReplacesRepoBindings(t *testing.T) {
	projectID := newTestCaseProject(t)
	adminID := newTestCaseRepoResource(t, projectID, "https://github.com/acme/admin-web")
	mobileID := newTestCaseRepoResource(t, projectID, "https://github.com/acme/mobile-app")
	created := createTestCaseForTest(t, map[string]any{
		"project_id": projectID,
		"title":      "换绑仓库",
		"repos": []map[string]any{
			{"project_resource_id": adminID, "alias": "admin-web", "role": "driver"},
		},
	})

	w := httptest.NewRecorder()
	req := withURLParam(newRequest("PUT", "/api/test-cases/"+created.Key+"?workspace_id="+testWorkspaceID, map[string]any{
		"repos": []map[string]any{
			{"project_resource_id": mobileID, "alias": "mobile-app", "role": "verifier"},
		},
	}), "ref", created.Key)
	testHandler.UpdateTestCase(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var updated TestCaseResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	if len(updated.Repos) != 1 || updated.Repos[0].Alias != "mobile-app" {
		t.Fatalf("repos = %+v, want only mobile-app", updated.Repos)
	}
}

func TestApproveTestCaseOnlyFromDraft(t *testing.T) {
	projectID := newTestCaseProject(t)
	created := createTestCaseForTest(t, map[string]any{
		"project_id": projectID,
		"title":      "待审用例",
		"status":     "draft",
		"steps":      executableSteps,
	})

	w := httptest.NewRecorder()
	req := withURLParam(newRequest("POST", "/api/test-cases/"+created.Key+"/approve?workspace_id="+testWorkspaceID, nil), "ref", created.Key)
	testHandler.ApproveTestCase(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("approve status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var approved TestCaseResponse
	if err := json.NewDecoder(w.Body).Decode(&approved); err != nil {
		t.Fatalf("decode approve: %v", err)
	}
	if approved.Status != "active" {
		t.Fatalf("status = %q, want active", approved.Status)
	}
	if approved.ReviewedBy == nil || approved.ReviewedAt == nil {
		t.Fatalf("approve must stamp reviewed_by and reviewed_at, got %+v / %+v", approved.ReviewedBy, approved.ReviewedAt)
	}

	again := httptest.NewRecorder()
	againReq := withURLParam(newRequest("POST", "/api/test-cases/"+created.Key+"/approve?workspace_id="+testWorkspaceID, nil), "ref", created.Key)
	testHandler.ApproveTestCase(again, againReq)
	if again.Code != http.StatusConflict {
		t.Fatalf("second approve status = %d, want 409: %s", again.Code, again.Body.String())
	}
}

func TestDeleteTestCaseRemovesRepoBindingsAndRevisions(t *testing.T) {
	projectID := newTestCaseProject(t)
	adminID := newTestCaseRepoResource(t, projectID, "https://github.com/acme/admin-web")
	created := createTestCaseForTest(t, map[string]any{
		"project_id": projectID,
		"title":      "待删除",
		"repos": []map[string]any{
			{"project_resource_id": adminID, "alias": "admin-web", "role": "driver"},
		},
	})

	// Produce a revision so the sweep has something to remove.
	updateW := httptest.NewRecorder()
	updateReq := withURLParam(newRequest("PUT", "/api/test-cases/"+created.Key+"?workspace_id="+testWorkspaceID,
		map[string]any{"module": "订单"}), "ref", created.Key)
	testHandler.UpdateTestCase(updateW, updateReq)
	if updateW.Code != http.StatusOK {
		t.Fatalf("seed update status = %d, want 200: %s", updateW.Code, updateW.Body.String())
	}

	w := httptest.NewRecorder()
	req := withURLParam(newRequest("DELETE", "/api/test-cases/"+created.Key+"?workspace_id="+testWorkspaceID, nil), "ref", created.Key)
	testHandler.DeleteTestCase(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200: %s", w.Code, w.Body.String())
	}

	ctx := context.Background()
	for table, query := range map[string]string{
		"test_case":          `SELECT count(*) FROM test_case WHERE id = $1`,
		"test_case_repo":     `SELECT count(*) FROM test_case_repo WHERE test_case_id = $1`,
		"test_case_revision": `SELECT count(*) FROM test_case_revision WHERE test_case_id = $1`,
	} {
		var count int
		if err := testPool.QueryRow(ctx, query, created.ID).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s still has %d row(s) after delete", table, count)
		}
	}
}

func TestListTestCasesFiltersAndGroups(t *testing.T) {
	projectID := newTestCaseProject(t)
	createTestCaseForTest(t, map[string]any{"project_id": projectID, "title": "订单用例", "module": "订单", "priority": "p0"})
	createTestCaseForTest(t, map[string]any{"project_id": projectID, "title": "计费用例", "module": "计费", "priority": "p2"})

	w := httptest.NewRecorder()
	testHandler.ListTestCases(w, newRequest("GET",
		"/api/test-cases?workspace_id="+testWorkspaceID+"&project_id="+projectID+"&module=订单", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var list struct {
		TestCases []TestCaseResponse `json:"test_cases"`
		Total     int                `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if list.Total != 1 || len(list.TestCases) != 1 || list.TestCases[0].Module != "订单" {
		t.Fatalf("module filter returned %+v", list)
	}

	modW := httptest.NewRecorder()
	testHandler.ListTestCaseModules(modW, newRequest("GET",
		"/api/test-cases/modules?workspace_id="+testWorkspaceID+"&project_id="+projectID, nil))
	if modW.Code != http.StatusOK {
		t.Fatalf("modules status = %d, want 200: %s", modW.Code, modW.Body.String())
	}
	var modules struct {
		Modules []struct {
			Module    string `json:"module"`
			CaseCount int64  `json:"case_count"`
		} `json:"modules"`
	}
	if err := json.NewDecoder(modW.Body).Decode(&modules); err != nil {
		t.Fatalf("decode modules: %v", err)
	}
	if len(modules.Modules) != 2 {
		t.Fatalf("modules = %+v, want two groups", modules.Modules)
	}
	for _, module := range modules.Modules {
		if module.CaseCount != 1 {
			t.Fatalf("module %q count = %d, want 1", module.Module, module.CaseCount)
		}
	}
}

func TestGetTestCaseRejectsUnknownReference(t *testing.T) {
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("GET", "/api/test-cases/TC-999999?workspace_id="+testWorkspaceID, nil), "ref", "TC-999999")
	testHandler.GetTestCase(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
	}
}
