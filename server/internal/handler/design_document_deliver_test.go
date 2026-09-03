package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func performDesignDocumentDeliver(t *testing.T, documentID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := withURLParam(newRequest(http.MethodPost, "/api/design-documents/"+documentID+"/deliver", body), "id", documentID)
	testHandler.DeliverDesignDocument(recorder, request)
	return recorder
}

func createDeliveryTestIssue(t *testing.T, projectID pgtype.UUID, title string) pgtype.UUID {
	t.Helper()
	var issueID pgtype.UUID
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, project_id, title, status, priority, creator_type, creator_id, number)
		VALUES ($1, $2, $3, 'todo', 'medium', 'member', $4,
		        COALESCE((SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0) + 1)
		RETURNING id
	`, testWorkspaceID, projectID, title, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create delivery test issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})
	return issueID
}

// A draft is a work in progress, not a promise. Delivering one would let an
// agent build from something the designer never stood behind (P-011 / DC-034).
func TestDeliverRefusesADocumentWithNoSavedRevision(t *testing.T) {
	fixture := createDesignDocumentRevisionFixture(t)
	issueID := createDeliveryTestIssue(t, fixture.Document.ProjectID, "Implement orders")

	response := performDesignDocumentDeliver(t, uuidToString(fixture.Document.ID), map[string]any{
		"issue_id": uuidToString(issueID),
	})
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "no_saved_revision") {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestDeliverMissingIssueKeepsProjectDesignSystemEnvelope(t *testing.T) {
	fixture := createDesignDocumentRevisionFixture(t)
	if _, err := db.New(testPool).SaveDesignDocumentDraft(context.Background(), db.SaveDesignDocumentDraftParams{
		ID:                      fixture.Document.ID,
		WorkspaceID:             parseUUID(testWorkspaceID),
		ExpectedDraftRevisionID: fixture.Revision.ID,
	}); err != nil {
		t.Fatalf("save draft: %v", err)
	}

	response := performDesignDocumentDeliver(t, uuidToString(fixture.Document.ID), map[string]any{
		"issue_id": "11111111-1111-1111-1111-111111111111",
	})
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing issue status = %d, want 404: %s", response.Code, response.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode missing issue response: %v", err)
	}
	if body["code"] != "issue_not_found" || body["error"] != "issue not found" {
		t.Fatalf("unexpected missing issue response: %#v", body)
	}
}

// The whole point of the slice: a saved design reaches the task that
// implements it, as the exact revision that was saved.
func TestDeliverLinksTheIssueAndReachesTheImplementingTask(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	fixture := createDesignDocumentRevisionFixture(t)
	issueID := createDeliveryTestIssue(t, fixture.Document.ProjectID, "Implement orders")

	saved, err := queries.SaveDesignDocumentDraft(ctx, db.SaveDesignDocumentDraftParams{
		ID:                      fixture.Document.ID,
		WorkspaceID:             parseUUID(testWorkspaceID),
		ExpectedDraftRevisionID: fixture.Revision.ID,
	})
	if err != nil {
		t.Fatalf("save draft: %v", err)
	}

	response := performDesignDocumentDeliver(t, uuidToString(saved.ID), map[string]any{
		"issue_id": uuidToString(issueID),
	})
	if response.Code != http.StatusOK {
		t.Fatalf("deliver: status = %d, body = %s", response.Code, response.Body.String())
	}
	var delivered DesignDocumentResponse
	if err := json.NewDecoder(response.Body).Decode(&delivered); err != nil {
		t.Fatal(err)
	}
	if delivered.IssueID != uuidToString(issueID) {
		t.Fatalf("document issue = %q, want %q", delivered.IssueID, uuidToString(issueID))
	}

	// The delivery is visible to a human on the issue, not only to the agent.
	var comments int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE issue_id = $1 AND type = 'system'`, issueID).Scan(&comments); err != nil {
		t.Fatal(err)
	}
	if comments != 1 {
		t.Fatalf("system comments on the issue = %d, want 1", comments)
	}

	// And an implementation task for that issue resolves to the saved
	// revision — the same digest the package was gated at.
	delivery := testHandler.designDeliveryContextForIssue(ctx, parseUUID(testWorkspaceID), issueID)
	if delivery == nil {
		t.Fatal("no delivery resolved for the issue")
	}
	if delivery.RevisionID != uuidToString(fixture.Revision.ID) {
		t.Fatalf("delivered revision = %q, want the saved %q", delivery.RevisionID, uuidToString(fixture.Revision.ID))
	}
	if delivery.ContentDigest != fixture.Revision.ContentDigest {
		t.Fatalf("delivered digest = %q, want %q", delivery.ContentDigest, fixture.Revision.ContentDigest)
	}
	// The page index travels too, so the agent knows what it is building
	// before opening a file.
	if len(delivery.Pages) == 0 || delivery.PrototypeEntry == "" {
		t.Fatalf("delivery carries no page index: %+v", delivery)
	}

	// Detaching takes the delivery back: the task stops seeing a design.
	if response := performDesignDocumentDeliver(t, uuidToString(saved.ID), map[string]any{"issue_id": ""}); response.Code != http.StatusOK {
		t.Fatalf("detach: status = %d, body = %s", response.Code, response.Body.String())
	}
	if delivery := testHandler.designDeliveryContextForIssue(ctx, parseUUID(testWorkspaceID), issueID); delivery != nil {
		t.Fatalf("a detached document is still delivered: %+v", delivery)
	}
}

// A design delivered across projects would be untraceable from either side.
func TestDeliverRefusesAnIssueFromAnotherProject(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	fixture := createDesignDocumentRevisionFixture(t)
	otherProject := createProjectDesignSystemProject(t, testWorkspaceID, fmt.Sprintf("Other %d", time.Now().UnixNano()))
	issueID := createDeliveryTestIssue(t, otherProject, "Elsewhere")

	if _, err := queries.SaveDesignDocumentDraft(ctx, db.SaveDesignDocumentDraftParams{
		ID:                      fixture.Document.ID,
		WorkspaceID:             parseUUID(testWorkspaceID),
		ExpectedDraftRevisionID: fixture.Revision.ID,
	}); err != nil {
		t.Fatalf("save draft: %v", err)
	}

	response := performDesignDocumentDeliver(t, uuidToString(fixture.Document.ID), map[string]any{
		"issue_id": uuidToString(issueID),
	})
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "issue_project_mismatch") {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

// An issue nobody delivered a design to must not acquire one.
func TestUndeliveredIssueResolvesNoDesign(t *testing.T) {
	fixture := createDesignDocumentRevisionFixture(t)
	issueID := createDeliveryTestIssue(t, fixture.Document.ProjectID, "No design here")

	if delivery := testHandler.designDeliveryContextForIssue(context.Background(), parseUUID(testWorkspaceID), issueID); delivery != nil {
		t.Fatalf("resolved a delivery for an undelivered issue: %+v", delivery)
	}
}
