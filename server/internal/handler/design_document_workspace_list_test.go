package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// The home page's "recent" strip reads the workspace-wide listing with a
// bounded limit. The project create picker reads the same endpoint without
// one, and must keep receiving every document: an absent limit is the historic
// contract, not "zero rows".
func TestListDesignDocumentsInWorkspaceLimitAndScope(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Workspace list limit", "workspace-list-limit")
	dbfx.Member(t, workspaceID, testUserID, "owner")
	projectID := dbfx.Project(t, "workspace list limit", testutil.Cols{"workspace_id": workspaceID})
	otherProjectID := dbfx.Project(t, "workspace list limit other", testutil.Cols{"workspace_id": workspaceID})
	foreignWorkspaceID := dbfx.Workspace(t, "Workspace list limit foreign", "workspace-list-limit-foreign")
	foreignProjectID := dbfx.Project(t, "workspace list limit foreign", testutil.Cols{"workspace_id": foreignWorkspaceID})

	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	oldest := workspaceListDocument(t, workspaceID, projectID, "oldest", base)
	middle := workspaceListDocument(t, workspaceID, otherProjectID, "middle", base.Add(time.Hour))
	newest := workspaceListDocument(t, workspaceID, projectID, "newest", base.Add(2*time.Hour))
	workspaceListDocument(t, foreignWorkspaceID, foreignProjectID, "foreign", base.Add(3*time.Hour))

	// No limit: every document in this workspace, newest first. A document
	// from another workspace is never in the answer, limit or not.
	unlimited := listWorkspaceDesignDocuments(t, workspaceID, "")
	assertWorkspaceListIDs(t, unlimited.Documents, newest, middle, oldest)

	// A limit takes the newest rows, because the SQL orders before it limits.
	limited := listWorkspaceDesignDocuments(t, workspaceID, "?limit=2")
	if len(limited.Documents) != 2 {
		t.Fatalf("limit=2 returned %d documents, want 2", len(limited.Documents))
	}
	assertWorkspaceListIDs(t, limited.Documents, newest, middle)

	one := listWorkspaceDesignDocuments(t, workspaceID, "?limit=1")
	assertWorkspaceListIDs(t, one.Documents, newest)

	// Above the server cap the request still succeeds rather than failing the
	// caller; it can simply never return more than the cap.
	overCap := listWorkspaceDesignDocuments(t, workspaceID, "?limit=999999")
	assertWorkspaceListIDs(t, overCap.Documents, newest, middle, oldest)

	for _, raw := range []string{"0", "-1", "abc", "1.5"} {
		response := testutil.Call(t, testHandler.ListDesignDocuments,
			workspaceListRequest(t, workspaceID, "?limit="+raw))
		assertProjectDesignSystemErrorCode(t, response.ResponseRecorder, http.StatusBadRequest, "invalid_limit")
	}
}

func listWorkspaceDesignDocuments(t *testing.T, workspaceID, query string) struct {
	Documents []DesignDocumentResponse `json:"documents"`
} {
	t.Helper()
	var payload struct {
		Documents []DesignDocumentResponse `json:"documents"`
	}
	testutil.Call(t, testHandler.ListDesignDocuments, workspaceListRequest(t, workspaceID, query)).
		Want(http.StatusOK).JSON(&payload)
	return payload
}

func workspaceListRequest(t *testing.T, workspaceID, query string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/design-documents"+query, nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", workspaceID)
	return req
}

func assertWorkspaceListIDs(t *testing.T, documents []DesignDocumentResponse, want ...string) {
	t.Helper()
	if len(documents) != len(want) {
		t.Fatalf("design document count = %d, want %d", len(documents), len(want))
	}
	for i, document := range documents {
		if document.ID != want[i] {
			t.Fatalf("design document %d = %s, want %s (order must be newest first)", i, document.ID, want[i])
		}
	}
}

// workspaceListDocument inserts one document at a fixed updated_at so the test
// controls the newest-first order the endpoint must return.
func workspaceListDocument(t testing.TB, workspaceID, projectID, title string, updatedAt time.Time) string {
	t.Helper()
	return dbfx.Insert(t, "design_document", testutil.Cols{
		"workspace_id": workspaceID,
		"project_id":   projectID,
		"title":        title,
		"platform":     "web",
		"recipe":       "default",
		"created_by":   testUserID,
		"updated_at":   updatedAt,
	})
}
