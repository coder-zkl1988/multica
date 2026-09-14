package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestDeleteProjectResourceCleansDesignRepoAnalysisInWorkspace(t *testing.T) {
	ctx := context.Background()
	foreignWorkspaceID := uuid.NewString()
	foreignProjectID := uuid.NewString()
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, created_by)
		VALUES ($1, $2, (SELECT id FROM "user" LIMIT 1))
		RETURNING id
	`, testWorkspaceID, "project-resource-cleanup-"+uuid.NewString()).Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO workspace (id, name, slug, issue_prefix)
		VALUES ($1, $2, $3, $4)
	`, foreignWorkspaceID, "Foreign resource cleanup", "foreign-resource-cleanup-"+uuid.NewString(), "FRC"); err != nil {
		t.Fatalf("insert foreign workspace: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO project (id, workspace_id, title, created_by)
		VALUES ($1, $2, $3, (SELECT id FROM "user" LIMIT 1))
	`, foreignProjectID, foreignWorkspaceID, "Foreign resource cleanup project"); err != nil {
		t.Fatalf("insert foreign project: %v", err)
	}

	var targetResourceID, unrelatedResourceID, foreignResourceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project_resource (project_id, workspace_id, resource_type, resource_ref, created_by)
		VALUES ($1, $2, 'github_repo', $3::jsonb, $4)
		RETURNING id
	`, projectID, testWorkspaceID, `{"url":"https://example.test/target.git"}`, testUserID).Scan(&targetResourceID); err != nil {
		t.Fatalf("insert target project resource: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project_resource (project_id, workspace_id, resource_type, resource_ref, created_by)
		VALUES ($1, $2, 'github_repo', $3::jsonb, $4)
		RETURNING id
	`, projectID, testWorkspaceID, `{"url":"https://example.test/unrelated.git"}`, testUserID).Scan(&unrelatedResourceID); err != nil {
		t.Fatalf("insert unrelated project resource: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project_resource (project_id, workspace_id, resource_type, resource_ref, created_by)
		VALUES ($1, $2, 'github_repo', $3::jsonb, $4)
		RETURNING id
	`, foreignProjectID, foreignWorkspaceID, `{"url":"https://example.test/foreign.git"}`, testUserID).Scan(&foreignResourceID); err != nil {
		t.Fatalf("insert foreign project resource: %v", err)
	}

	var targetAnalysisID, unrelatedAnalysisID, foreignAnalysisID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO design_repo_analysis (workspace_id, project_id, project_resource_id)
		VALUES ($1, $2, $3)
		RETURNING id
	`, testWorkspaceID, projectID, targetResourceID).Scan(&targetAnalysisID); err != nil {
		t.Fatalf("insert target analysis: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO design_repo_analysis (workspace_id, project_id, project_resource_id)
		VALUES ($1, $2, $3)
		RETURNING id
	`, testWorkspaceID, projectID, unrelatedResourceID).Scan(&unrelatedAnalysisID); err != nil {
		t.Fatalf("insert unrelated analysis: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO design_repo_analysis (workspace_id, project_id, project_resource_id)
		VALUES ($1, $2, $3)
		RETURNING id
	`, foreignWorkspaceID, foreignProjectID, foreignResourceID).Scan(&foreignAnalysisID); err != nil {
		t.Fatalf("insert foreign analysis: %v", err)
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_repo_analysis WHERE id IN ($1, $2, $3)`, targetAnalysisID, unrelatedAnalysisID, foreignAnalysisID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project_resource WHERE id IN ($1, $2, $3)`, targetResourceID, unrelatedResourceID, foreignResourceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id IN ($1, $2)`, projectID, foreignProjectID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, foreignWorkspaceID)
	})

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/projects/"+projectID+"/resources/"+targetResourceID, nil)
	req = withURLParams(req, "id", projectID, "resourceId", targetResourceID)
	testHandler.DeleteProjectResource(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteProjectResource: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	requireProjectResourceCleanupRowCount(t, "project_resource", targetResourceID, 0)
	requireProjectResourceCleanupRowCount(t, "design_repo_analysis", targetAnalysisID, 0)
	requireProjectResourceCleanupRowCount(t, "design_repo_analysis", unrelatedAnalysisID, 1)
	requireProjectResourceCleanupRowCount(t, "design_repo_analysis", foreignAnalysisID, 1)
}

func TestDeleteProjectResourceDetachesDesignFileRepository(t *testing.T) {
	projectID, resourceID, fileID, documentID := seedProjectResourceDesignRepository(t, "design-file-detach")

	deleteProjectResourceForCleanupTest(t, projectID, resourceID)

	requireProjectResourceCleanupRowCount(t, "project_resource", resourceID, 0)
	requireProjectResourceCleanupRowCount(t, "design_file", fileID, 1)
	requireProjectResourceCleanupRowCount(t, "design_document", documentID, 1)
	requireProjectResourceCleanupDesignRepositoryID(t, "design_file", fileID, "")
	requireProjectResourceCleanupDesignRepositoryID(t, "design_document", documentID, "")
}

func TestDeleteProjectResourceDetachesDesignDocumentRepository(t *testing.T) {
	projectID, resourceID, fileID, documentID := seedProjectResourceDesignRepository(t, "design-document-detach")

	deleteProjectResourceForCleanupTest(t, projectID, resourceID)

	requireProjectResourceCleanupRowCount(t, "project_resource", resourceID, 0)
	requireProjectResourceCleanupRowCount(t, "design_file", fileID, 1)
	requireProjectResourceCleanupRowCount(t, "design_document", documentID, 1)
	requireProjectResourceCleanupDesignRepositoryID(t, "design_file", fileID, "")
	requireProjectResourceCleanupDesignRepositoryID(t, "design_document", documentID, "")
}

func TestDeleteProjectResourceKeepsDesignRows(t *testing.T) {
	projectID, resourceID, fileID, documentID := seedProjectResourceDesignRepository(t, "design-row-preservation")

	deleteProjectResourceForCleanupTest(t, projectID, resourceID)

	if got := dbfx.Count(t, `SELECT count(*) FROM design_file WHERE id = $1`, fileID); got != 1 {
		t.Fatalf("design_file row count = %d, want 1", got)
	}
	if got := dbfx.Count(t, `SELECT count(*) FROM design_document WHERE id = $1`, documentID); got != 1 {
		t.Fatalf("design_document row count = %d, want 1", got)
	}
	requireProjectResourceCleanupDesignRepositoryID(t, "design_file", fileID, "")
	requireProjectResourceCleanupDesignRepositoryID(t, "design_document", documentID, "")
}

func seedProjectResourceDesignRepository(t *testing.T, title string) (projectID, resourceID, fileID, documentID string) {
	t.Helper()

	projectID = dbfx.Project(t, "project-resource-"+title+"-"+uuid.NewString())
	resourceID = dbfx.Insert(t, "project_resource", testutil.Cols{
		"project_id":    projectID,
		"workspace_id":  testWorkspaceID,
		"resource_type": "github_repo",
		"resource_ref":  testutil.Raw(`'{}'::jsonb`),
		"created_by":    testUserID,
	})
	fileID = dbfx.Insert(t, "design_file", testutil.Cols{
		"workspace_id":        testWorkspaceID,
		"project_id":          projectID,
		"title":               title + " file",
		"source_type":         "upload",
		"source_ref":          testutil.Raw(`'{}'::jsonb`),
		"project_resource_id": resourceID,
		"created_by":          testUserID,
	})
	documentID = dbfx.Insert(t, "design_document", testutil.Cols{
		"workspace_id":        testWorkspaceID,
		"project_id":          projectID,
		"project_resource_id": resourceID,
		"title":               title + " document",
		"platform":            "web",
		"recipe":              "default",
		"active_task_id":      uuid.NewString(),
		"input_snapshot":      testutil.Raw(`'{}'::jsonb`),
		"created_by":          testUserID,
	})
	return projectID, resourceID, fileID, documentID
}

func deleteProjectResourceForCleanupTest(t *testing.T, projectID, resourceID string) {
	t.Helper()

	req := newRequest(http.MethodDelete, "/api/projects/"+projectID+"/resources/"+resourceID, nil)
	req = withURLParams(req, "id", projectID, "resourceId", resourceID)
	testutil.Call(t, testHandler.DeleteProjectResource, req).Want(http.StatusNoContent)
}

func requireProjectResourceCleanupDesignRepositoryID(t *testing.T, table, id, want string) {
	t.Helper()

	var got *string
	dbfx.QueryRow(t, "SELECT project_resource_id::text FROM "+table+" WHERE id = $1", id).Scan(&got)
	if want == "" {
		if got != nil {
			t.Fatalf("%s %s project_resource_id = %q, want NULL", table, id, *got)
		}
		return
	}
	if got == nil || *got != want {
		t.Fatalf("%s %s project_resource_id = %v, want %q", table, id, got, want)
	}
}

func requireProjectResourceCleanupRowCount(t *testing.T, table, id string, want int) {
	t.Helper()
	var got int
	query := "SELECT count(*) FROM " + table + " WHERE id = $1"
	if err := testPool.QueryRow(context.Background(), query, id).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s row count = %d, want %d", table, got, want)
	}
}
