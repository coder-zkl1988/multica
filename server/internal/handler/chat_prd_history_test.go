package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The workspace ledger must project the durable draft rows — including the
// initiator's Multica user binding — for back-office review, and stay
// read-only: reading it creates nothing.
func TestChatPRDDraftHistoryListsWorkspaceDrafts(t *testing.T) {
	f := newChatPRDFixture(t)
	published := f.draft(t, prdFixtureContent())
	confirmation := f.confirm(published)
	testutil.Call(t, f.h.PublishChatPRD, f.publishRequest(published, confirmation.MessageID)).Want(http.StatusOK)

	// The ledger is workspace-member scoped, not task-scoped: simulate the
	// RequireWorkspaceMember middleware injection other handler tests use.
	memberRow, err := f.h.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      util.MustParseUUID(testUserID),
		WorkspaceID: util.MustParseUUID(f.workspaceID),
	})
	if err != nil {
		t.Fatalf("member lookup: %v", err)
	}
	newHistoryRequest := func(rawQuery string) *http.Request {
		request := f.request(http.MethodGet, "/api/chat/prd/history"+rawQuery, nil)
		request.Header.Del("X-Actor-Source")
		request.Header.Del("X-Task-ID")
		request.Header.Set("X-User-ID", testUserID)
		return request.WithContext(middleware.SetMemberContext(request.Context(), f.workspaceID, memberRow))
	}

	var body struct {
		Items []ChatPRDDraftHistoryItem `json:"items"`
		Count int                       `json:"count"`
	}
	testutil.Call(t, f.h.ListChatPRDDraftHistory, newHistoryRequest("")).Want(http.StatusOK).JSON(&body)
	if body.Count != 1 || len(body.Items) != 1 {
		t.Fatalf("history count=%d items=%d want 1", body.Count, len(body.Items))
	}
	item := body.Items[0]
	if item.ID != published.ID || item.Status != "published" || item.Version != 1 {
		t.Fatalf("history item mismatch: %+v", item)
	}
	if item.InitiatorOpenID != f.root.SenderID || item.InitiatorMulticaUserID == nil || *item.InitiatorMulticaUserID != testUserID {
		t.Fatalf("initiator binding not projected: %+v", item)
	}
	if item.DocumentURL == "" || item.DocumentID == "" {
		t.Fatalf("publication not projected: %+v", item)
	}

	// Status filter narrows rather than duplicates.
	testutil.Call(t, f.h.ListChatPRDDraftHistory, newHistoryRequest("?status=draft")).Want(http.StatusOK).JSON(&body)
	if body.Count != 0 || len(body.Items) != 0 {
		t.Fatalf("status filter returned rows: %+v", body.Items)
	}
	// Invalid status is rejected, not silently ignored.
	testutil.Call(t, f.h.ListChatPRDDraftHistory, newHistoryRequest("?status=bogus")).Want(http.StatusBadRequest)

	// A task-token (agent) caller is refused even though the middleware
	// layer would resolve its originator as a member: the ledger is for
	// human back-office review, not agent enumeration.
	taskToken := f.request(http.MethodGet, "/api/chat/prd/history", nil)
	testutil.Call(t, f.h.ListChatPRDDraftHistory, taskToken).Want(http.StatusForbidden)
}
