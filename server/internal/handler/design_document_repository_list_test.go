package handler

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestListDesignDocumentsByRepositoryIsExact(t *testing.T) {
	projectID := dbfx.Project(t, "design-document-repository-list")
	otherProjectID := dbfx.Project(t, "design-document-repository-list-other")
	resourceA := repositoryListResource(t, projectID, "github_repo", "repository list document A")
	resourceB := repositoryListResource(t, projectID, "github_repo", "repository list document B")
	foreignResource := repositoryListResource(t, otherProjectID, "github_repo", "repository list document foreign")
	nonRepository := repositoryListResource(t, projectID, "notion_page", "repository list document non-repository")
	documentA := repositoryListDesignDocument(t, projectID, resourceA, "repository document A")
	documentB := repositoryListDesignDocument(t, projectID, resourceB, "repository document B")
	unlinked := repositoryListDesignDocument(t, projectID, nil, "repository document unlinked")

	projectPayload := listDesignDocumentsForRepositoryTest(t, projectID, "")
	assertRepositoryDesignDocumentIDs(t, projectPayload.Documents, documentA, documentB, unlinked)

	payloadA := listDesignDocumentsForRepositoryTest(t, projectID, resourceA)
	assertRepositoryDesignDocumentIDs(t, payloadA.Documents, documentA)

	payloadB := listDesignDocumentsForRepositoryTest(t, projectID, resourceB)
	assertRepositoryDesignDocumentIDs(t, payloadB.Documents, documentB)

	foreign := testutil.Call(t, testHandler.ListDesignDocuments, repositoryListDesignDocumentsRequest(projectID, foreignResource))
	assertProjectDesignSystemErrorCode(t, foreign.ResponseRecorder, http.StatusConflict, "project_resource_project_mismatch")

	nonRepo := testutil.Call(t, testHandler.ListDesignDocuments, repositoryListDesignDocumentsRequest(projectID, nonRepository))
	assertProjectDesignSystemErrorCode(t, nonRepo.ResponseRecorder, http.StatusBadRequest, "project_resource_not_repository")
}

func TestListDesignDocumentsRejectsIssueAndRepositoryScope(t *testing.T) {
	projectID := dbfx.Project(t, "design-document-repository-invalid-scope")
	resourceID := repositoryListResource(t, projectID, "github_repo", "repository invalid scope")
	issueID := dbfx.Issue(t, "repository invalid scope issue")

	issueAndRepository := testutil.Call(t, testHandler.ListDesignDocuments,
		newRequest(http.MethodGet, "/api/design-documents?workspace_id="+testWorkspaceID+"&issue_id="+issueID+"&project_resource_id="+resourceID, nil))
	assertProjectDesignSystemErrorCode(t, issueAndRepository.ResponseRecorder, http.StatusBadRequest, "invalid_request")

	repositoryOnly := testutil.Call(t, testHandler.ListDesignDocuments, repositoryListDesignDocumentsRequest("", resourceID))
	assertProjectDesignSystemErrorCode(t, repositoryOnly.ResponseRecorder, http.StatusBadRequest, "invalid_request")
}

func repositoryListDesignDocumentsRequest(projectID, resourceID string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, repositoryListQuery("/api/design-documents", projectID, resourceID), nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	return req
}

func listDesignDocumentsForRepositoryTest(t *testing.T, projectID, resourceID string) struct {
	Documents []DesignDocumentResponse `json:"documents"`
} {
	t.Helper()
	var payload struct {
		Documents []DesignDocumentResponse `json:"documents"`
	}
	testutil.Call(t, testHandler.ListDesignDocuments, repositoryListDesignDocumentsRequest(projectID, resourceID)).
		Want(http.StatusOK).JSON(&payload)
	return payload
}

func assertRepositoryDesignDocumentIDs(t *testing.T, documents []DesignDocumentResponse, want ...string) {
	t.Helper()
	got := make([]string, len(documents))
	updatedAt := make([]string, len(documents))
	for i, document := range documents {
		got[i] = document.ID
		updatedAt[i] = document.UpdatedAt
	}
	assertRepositoryIDsExact(t, "design document IDs", got, want)
	assertRepositoryUpdatedAtDescending(t, "design document", updatedAt)
}

func assertRepositoryIDsExact(t *testing.T, label string, got, want []string) {
	t.Helper()
	gotCounts := make(map[string]int, len(got))
	for _, id := range got {
		gotCounts[id]++
	}
	wantCounts := make(map[string]int, len(want))
	for _, id := range want {
		wantCounts[id]++
	}
	if len(got) == len(want) && maps.Equal(gotCounts, wantCounts) {
		return
	}
	t.Fatalf("%s = %v, want exactly %v", label, got, want)
}

func assertRepositoryUpdatedAtDescending(t *testing.T, label string, updatedAt []string) {
	t.Helper()
	for i := 1; i < len(updatedAt); i++ {
		current, err := time.Parse(time.RFC3339Nano, updatedAt[i])
		if err != nil {
			t.Fatalf("parse %s updated_at %q: %v", label, updatedAt[i], err)
		}
		previous, err := time.Parse(time.RFC3339Nano, updatedAt[i-1])
		if err != nil {
			t.Fatalf("parse %s updated_at %q: %v", label, updatedAt[i-1], err)
		}
		if current.After(previous) {
			t.Fatalf("%s updated_at must be descending, got %q before %q", label, updatedAt[i-1], updatedAt[i])
		}
	}
}

func repositoryListQuery(path, projectID, resourceID string) string {
	query := "?workspace_id=" + testWorkspaceID
	if projectID != "" {
		query += "&project_id=" + projectID
	}
	if resourceID != "" {
		query += "&project_resource_id=" + resourceID
	}
	return path + query
}

func repositoryListRequest(path, projectID, resourceID string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, repositoryListQuery(path, projectID, resourceID), nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	return req
}

func repositoryListResource(t testing.TB, projectID, resourceType, name string) string {
	t.Helper()
	return dbfx.Insert(t, "project_resource", testutil.Cols{
		"project_id":    projectID,
		"workspace_id":  testWorkspaceID,
		"resource_type": resourceType,
		"resource_ref":  testutil.Raw(`'{"name":"` + name + `"}'::jsonb`),
		"created_by":    testUserID,
	})
}

func repositoryListDesignFile(t testing.TB, projectID string, resourceID any, title string) string {
	t.Helper()
	return dbfx.Insert(t, "design_file", testutil.Cols{
		"workspace_id":        testWorkspaceID,
		"project_id":          projectID,
		"title":               title,
		"source_type":         "upload",
		"source_ref":          testutil.Raw(`'{"asset_type":"upload"}'::jsonb`),
		"project_resource_id": resourceID,
		"created_by":          testUserID,
	})
}

func repositoryListDesignDocument(t testing.TB, projectID string, resourceID any, title string) string {
	t.Helper()
	return dbfx.Insert(t, "design_document", testutil.Cols{
		"workspace_id":        testWorkspaceID,
		"project_id":          projectID,
		"project_resource_id": resourceID,
		"title":               title,
		"platform":            "web",
		"recipe":              "default",
		"created_by":          testUserID,
	})
}
