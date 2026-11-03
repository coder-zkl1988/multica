package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// renameDocumentRequest issues a PATCH scoped to one workspace. The user is
// the shared test member; workspace B cases only change the document, because
// the point is that a document outside the request's workspace never resolves.
func renameDocumentRequest(t *testing.T, workspaceID, documentID string, body any) *http.Request {
	t.Helper()
	request := newRequest(http.MethodPatch, "/api/design-documents/"+documentID, body)
	request.Header.Set("X-Workspace-ID", workspaceID)
	return withURLParam(request, "id", documentID)
}

func insertRenameDocument(t testing.TB, workspaceID, projectID, title string, updatedAt time.Time) string {
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

// A rename is a title write and nothing else: GET sees the new name, the
// stored row carries it trimmed, and updated_at moves so the newest-first
// lists put the document where the user just touched it.
func TestUpdateDesignDocumentRenamesAndGetReflectsIt(t *testing.T) {
	projectID := dbfx.Project(t, "rename", testutil.Cols{"workspace_id": testWorkspaceID})
	oldUpdatedAt := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	documentID := insertRenameDocument(t, testWorkspaceID, projectID, "旧标题", oldUpdatedAt)

	var renamed DesignDocumentResponse
	testutil.Call(t, testHandler.UpdateDesignDocument,
		renameDocumentRequest(t, testWorkspaceID, documentID, map[string]any{"title": "  新标题  "})).
		Want(http.StatusOK).JSON(&renamed)
	if renamed.Title != "新标题" {
		t.Fatalf("title = %q, want the trimmed 新标题", renamed.Title)
	}

	var fetched DesignDocumentResponse
	testutil.Call(t, testHandler.GetDesignDocument,
		withURLParam(newRequest(http.MethodGet, "/api/design-documents/"+documentID, nil), "id", documentID)).
		Want(http.StatusOK).JSON(&fetched)
	if fetched.Title != "新标题" {
		t.Fatalf("GET title = %q, want 新标题", fetched.Title)
	}

	var storedTitle string
	var storedUpdatedAt time.Time
	if err := testPool.QueryRow(context.Background(),
		`SELECT title, updated_at FROM design_document WHERE id = $1`, documentID,
	).Scan(&storedTitle, &storedUpdatedAt); err != nil {
		t.Fatalf("read renamed document: %v", err)
	}
	if storedTitle != "新标题" {
		t.Fatalf("stored title = %q, want 新标题", storedTitle)
	}
	if !storedUpdatedAt.After(oldUpdatedAt) {
		t.Fatalf("updated_at = %v, want after %v — the lists order by this column", storedUpdatedAt, oldUpdatedAt)
	}
}

// The title is validated with the same cap the create endpoint uses, and an
// empty or whitespace-only name is refused before any row is written: a blank
// header would leave the document nameless in every list.
func TestUpdateDesignDocumentValidatesTheTitle(t *testing.T) {
	projectID := dbfx.Project(t, "rename validation", testutil.Cols{"workspace_id": testWorkspaceID})
	documentID := insertRenameDocument(t, testWorkspaceID, projectID, "原名称", time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))

	for _, title := range []string{"", "   ", "\n\t"} {
		response := testutil.Call(t, testHandler.UpdateDesignDocument,
			renameDocumentRequest(t, testWorkspaceID, documentID, map[string]any{"title": title}))
		assertProjectDesignSystemErrorCode(t, response.ResponseRecorder, http.StatusBadRequest, "title_required")
	}

	tooLong := strings.Repeat("a", designDocumentMaxTitleBytes+1)
	response := testutil.Call(t, testHandler.UpdateDesignDocument,
		renameDocumentRequest(t, testWorkspaceID, documentID, map[string]any{"title": tooLong}))
	assertProjectDesignSystemErrorCode(t, response.ResponseRecorder, http.StatusBadRequest, "title_too_large")

	var storedTitle string
	if err := testPool.QueryRow(context.Background(),
		`SELECT title FROM design_document WHERE id = $1`, documentID,
	).Scan(&storedTitle); err != nil {
		t.Fatalf("read document: %v", err)
	}
	if storedTitle != "原名称" {
		t.Fatalf("a rejected rename wrote %q over the stored title", storedTitle)
	}

	// Exactly the cap is still valid: the limit is enforced at the boundary,
	// not one byte early.
	atCap := strings.Repeat("a", designDocumentMaxTitleBytes)
	testutil.Call(t, testHandler.UpdateDesignDocument,
		renameDocumentRequest(t, testWorkspaceID, documentID, map[string]any{"title": atCap})).
		Want(http.StatusOK)
}

// A document from another workspace must not resolve through this workspace's
// request scope: the loader filters on workspace_id, so the rename is a 404
// and the foreign row keeps its name.
func TestUpdateDesignDocumentRejectsAnotherWorkspacesDocument(t *testing.T) {
	foreignWorkspaceID := dbfx.Workspace(t, "Rename foreign", "rename-foreign")
	foreignProjectID := dbfx.Project(t, "rename foreign", testutil.Cols{"workspace_id": foreignWorkspaceID})
	foreignDocumentID := insertRenameDocument(t, foreignWorkspaceID, foreignProjectID, "别人的标题", time.Now().UTC())

	response := testutil.Call(t, testHandler.UpdateDesignDocument,
		renameDocumentRequest(t, testWorkspaceID, foreignDocumentID, map[string]any{"title": "偷改"}))
	if response.ResponseRecorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", response.ResponseRecorder.Code, response.ResponseRecorder.Body.String())
	}

	var storedTitle string
	if err := testPool.QueryRow(context.Background(),
		`SELECT title FROM design_document WHERE id = $1`, foreignDocumentID,
	).Scan(&storedTitle); err != nil {
		t.Fatalf("read foreign document: %v", err)
	}
	if storedTitle != "别人的标题" {
		t.Fatalf("cross-workspace rename changed the title to %q", storedTitle)
	}
}

// A malformed id is a bad request, not a not-found: the path parameter is a
// pure UUID input and is parsed before any lookup. The UUID parser uses the
// shared `error` envelope rather than the design-center code envelope, so the
// assertion reads the message the client is actually given.
func TestUpdateDesignDocumentRejectsAMalformedID(t *testing.T) {
	response := testutil.Call(t, testHandler.UpdateDesignDocument,
		renameDocumentRequest(t, testWorkspaceID, "not-a-uuid", map[string]any{"title": "新名称"}))
	if response.ResponseRecorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", response.ResponseRecorder.Code, response.ResponseRecorder.Body.String())
	}
	if !strings.Contains(response.ResponseRecorder.Body.String(), "invalid id") {
		t.Fatalf("body = %s, want the invalid id message", response.ResponseRecorder.Body.String())
	}
}

// Renaming must not touch the pointers that record what content the user
// approved, nor arm or clear a run. This is the whole safety argument for
// allowing the write while a task is live.
func TestUpdateDesignDocumentLeavesPointersAndRunUntouched(t *testing.T) {
	projectID := dbfx.Project(t, "rename pointers", testutil.Cols{"workspace_id": testWorkspaceID})
	documentID := insertRenameDocument(t, testWorkspaceID, projectID, "指针测试", time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))
	// The pointer columns have no foreign keys, so a synthetic id is enough
	// to prove the rename leaves them alone.
	if _, err := testPool.Exec(context.Background(), `
		UPDATE design_document
		SET draft_revision_id = '11111111-1111-4111-8111-111111111111',
		    saved_revision_id = '22222222-2222-4222-8222-222222222222',
		    last_error = '{"code":"previous_failure"}'::jsonb
		WHERE id = $1
	`, documentID); err != nil {
		t.Fatalf("seed pointers: %v", err)
	}

	testutil.Call(t, testHandler.UpdateDesignDocument,
		renameDocumentRequest(t, testWorkspaceID, documentID, map[string]any{"title": "指针测试改名"})).
		Want(http.StatusOK)

	var draftID, savedID string
	var lastError string
	if err := testPool.QueryRow(context.Background(), `
		SELECT draft_revision_id::text, saved_revision_id::text, last_error::text
		FROM design_document WHERE id = $1
	`, documentID).Scan(&draftID, &savedID, &lastError); err != nil {
		t.Fatalf("read pointers: %v", err)
	}
	if draftID != "11111111-1111-4111-8111-111111111111" || savedID != "22222222-2222-4222-8222-222222222222" {
		t.Fatalf("rename moved the pointers: draft = %s, saved = %s", draftID, savedID)
	}
	if !strings.Contains(lastError, "previous_failure") {
		t.Fatalf("rename cleared last_error: %s", lastError)
	}
}
