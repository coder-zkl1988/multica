package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
)

type staticIssueWindowProvider struct {
	decision entitlement.Decision
}

type countingIssueWindowDB struct {
	dbExecutor
	windowQueries int
}

func (db *countingIssueWindowDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "issue_window_base") {
		db.windowQueries++
	}
	return db.dbExecutor.Query(ctx, sql, args...)
}

func (p staticIssueWindowProvider) Gate(context.Context, uuid.UUID, entitlement.GateName) entitlement.Decision {
	return p.decision
}

func issueWindowProvider(action entitlement.Action, limit int) staticIssueWindowProvider {
	return staticIssueWindowProvider{decision: entitlement.Decision{
		Gate:           entitlement.Gate{Action: action, Limit: &limit},
		PolicyRevision: 7,
	}}
}

func TestIssueWindowPolicyFailsOpen(t *testing.T) {
	workspaceID := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	tests := []struct {
		name     string
		provider entitlement.Provider
	}{
		{name: "nil provider"},
		{name: "off", provider: issueWindowProvider(entitlement.ActionOff, 17)},
		{name: "zero limit", provider: issueWindowProvider(entitlement.ActionEnforce, 0)},
		{name: "negative limit", provider: issueWindowProvider(entitlement.ActionObserve, -1)},
		{name: "oversized limit", provider: issueWindowProvider(entitlement.ActionEnforce, maxIssueWindowLimit+1)},
		{name: "unknown action", provider: issueWindowProvider(entitlement.Action("future"), 17)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{Entitlements: tt.provider}
			if _, enabled := h.issueWindowPolicy(context.Background(), workspaceID); enabled {
				t.Fatal("malformed or disabled policy must fail open")
			}
		})
	}
}

func TestIssueWindowPredicateUsesCreationNumberAndAncestors(t *testing.T) {
	predicate := issueWindowPredicate("i", "$1", "$2")
	for _, want := range []string{
		"WHERE workspace_id = $1",
		"ORDER BY number DESC",
		"LIMIT $2",
		"child.parent_issue_id = parent.id",
		"parent.workspace_id = $1",
	} {
		if !strings.Contains(predicate, want) {
			t.Fatalf("predicate missing %q:\n%s", want, predicate)
		}
	}
	if strings.Contains(predicate, "last_activity_at") || strings.Contains(predicate, "created_at") {
		t.Fatalf("creation window must not depend on mutable/timestamp ordering:\n%s", predicate)
	}
}

func TestAppendIssueWindowOnlyEnforces(t *testing.T) {
	for _, action := range []entitlement.Action{entitlement.ActionOff, entitlement.ActionObserve} {
		args := []any{"workspace"}
		where := appendIssueWindow([]string{"i.workspace_id = $1"}, func(value any) string {
			args = append(args, value)
			return "$2"
		}, issueWindowPolicy{action: action, limit: 17}, "$1", "i")
		if len(where) != 1 || len(args) != 1 {
			t.Fatalf("%s changed the legacy query: where=%v args=%v", action, where, args)
		}
	}
}

func TestIssueTableReusesOneVisibleIDSnapshot(t *testing.T) {
	counter := &countingIssueWindowDB{dbExecutor: testHandler.DB}
	h := *testHandler
	h.DB = counter
	h.Entitlements = issueWindowProvider(entitlement.ActionEnforce, 1)
	h.issueTableWindowCache = &issueTableWindowCache{}
	spec := issueTableQuerySpec{
		Scope: issueTableScope{Kind: "workspace"},
		Sort:  issueTableSortRequest{Field: "position", Direction: "asc"},
	}
	for range 2 {
		recorder := httptest.NewRecorder()
		compiled, ok := h.compileIssueTableQuery(recorder, newRequest(http.MethodPost, "/api/issues/table/rows", nil), spec)
		if !ok {
			t.Fatalf("compile table query: %d %s", recorder.Code, recorder.Body.String())
		}
		if !strings.Contains(compiled.where, "i.id = ANY(") || strings.Contains(compiled.where, "issue_window_base") {
			t.Fatalf("table query did not use cached ids: %s", compiled.where)
		}
	}
	if counter.windowQueries != 1 {
		t.Fatalf("visible window queries = %d, want one per table snapshot", counter.windowQueries)
	}
}

func TestIssueTableLargeWindowStaysInPostgres(t *testing.T) {
	counter := &countingIssueWindowDB{dbExecutor: testHandler.DB}
	h := *testHandler
	h.DB = counter
	h.Entitlements = issueWindowProvider(entitlement.ActionEnforce, issueTableWindowIDCacheLimit+1)
	h.issueTableWindowCache = &issueTableWindowCache{}
	spec := issueTableQuerySpec{
		Scope: issueTableScope{Kind: "workspace"},
		Sort:  issueTableSortRequest{Field: "position", Direction: "asc"},
	}
	for range 2 {
		recorder := httptest.NewRecorder()
		compiled, ok := h.compileIssueTableQuery(recorder, newRequest(http.MethodPost, "/api/issues/table/rows", nil), spec)
		if !ok {
			t.Fatalf("compile table query: %d %s", recorder.Code, recorder.Body.String())
		}
		if !strings.Contains(compiled.where, "issue_window_base") || strings.Contains(compiled.where, "i.id = ANY(") {
			t.Fatalf("large table window did not stay in PostgreSQL: %s", compiled.where)
		}
	}
	if counter.windowQueries != 0 {
		t.Fatalf("large table window preloads = %d, want none", counter.windowQueries)
	}
}

func TestIssueWindowUsageOffDoesNotNeedDatabase(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/issues/window-usage", nil)
	req.Header.Set("X-Workspace-ID", uuid.NewString())
	recorder := httptest.NewRecorder()
	h.GetIssueWindowUsage(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("off usage status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var body IssueWindowUsageResponse
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode off usage: %v", err)
	}
	if body.Action != string(entitlement.ActionOff) || body.Used != nil || body.Limit != nil {
		t.Fatalf("unexpected off usage: %#v", body)
	}
}

func TestIssueCreationWindowEnforcesNewestBaseAndAncestorChain(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Issue window", "issue-window-"+uuid.NewString(), testutil.Cols{
		"issue_prefix": "WIN",
	})
	dbfx.Member(t, workspaceID, testUserID, "owner")

	rootID := dbfx.Issue(t, "old root", testutil.Cols{"workspace_id": workspaceID, "number": 1})
	hiddenSiblingID := dbfx.Issue(t, "hidden sibling", testutil.Cols{
		"workspace_id":    workspaceID,
		"number":          50,
		"parent_issue_id": rootID,
	})
	newestChildID := dbfx.Issue(t, "newest child", testutil.Cols{
		"workspace_id":    workspaceID,
		"number":          100,
		"parent_issue_id": rootID,
	})
	// Activity on an older issue must not move it into a creation window.
	dbfx.Exec(t, `UPDATE issue SET last_activity_at = now() + interval '1 hour' WHERE id = $1`, hiddenSiblingID)

	h := *testHandler
	h.Entitlements = issueWindowProvider(entitlement.ActionEnforce, 1)
	policy, enabled := h.issueWindowPolicy(context.Background(), parseUUID(workspaceID))
	if !enabled {
		t.Fatal("expected enforce policy")
	}
	newestUUID := parseUUID(newestChildID)
	if visible, err := h.issueIDsWithinWindow(context.Background(), parseUUID(workspaceID), policy, []pgtype.UUID{newestUUID, newestUUID}); err != nil || !visible {
		t.Fatalf("duplicate visible ids = %v, %v", visible, err)
	}

	request := func(issueID string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/api/issues/"+issueID, nil)
		req.Header.Set("X-User-ID", testUserID)
		req.Header.Set("X-Workspace-ID", workspaceID)
		return req
	}
	for _, visibleID := range []string{newestChildID, rootID} {
		recorder := httptest.NewRecorder()
		if _, ok := h.loadIssueForUser(recorder, request(visibleID), visibleID); !ok {
			t.Fatalf("expected %s to be visible, got %d: %s", visibleID, recorder.Code, recorder.Body.String())
		}
	}

	blocked := httptest.NewRecorder()
	if _, ok := h.loadIssueForUser(blocked, request(hiddenSiblingID), hiddenSiblingID); ok {
		t.Fatal("hidden sibling unexpectedly passed direct access")
	}
	if blocked.Code != http.StatusPaymentRequired {
		t.Fatalf("hidden direct access status = %d, want 402: %s", blocked.Code, blocked.Body.String())
	}
	var blockedBody map[string]any
	if err := json.NewDecoder(blocked.Body).Decode(&blockedBody); err != nil {
		t.Fatalf("decode blocked response: %v", err)
	}
	if blockedBody["code"] != issueWindowErrorCode || blockedBody["error"] != "This issue is outside the workspace's recently created issue window." || blockedBody["limit"] != float64(1) {
		t.Fatalf("unexpected blocked response: %#v", blockedBody)
	}

	listRecorder := httptest.NewRecorder()
	listReq := request("")
	listReq.URL.Path = "/api/issues"
	h.ListIssues(listRecorder, listReq)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", listRecorder.Code, listRecorder.Body.String())
	}
	var listBody struct {
		Issues []IssueResponse `json:"issues"`
		Total  int64           `json:"total"`
	}
	if err := json.NewDecoder(listRecorder.Body).Decode(&listBody); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if listBody.Total != 2 || len(listBody.Issues) != 2 {
		t.Fatalf("visible list = %d/%d, want newest child + ancestor: %#v", len(listBody.Issues), listBody.Total, listBody.Issues)
	}
	for _, issue := range listBody.Issues {
		if issue.ID == hiddenSiblingID {
			t.Fatal("ancestor supplementation exposed a hidden sibling")
		}
	}
	usageRecorder := httptest.NewRecorder()
	usageReq := request("")
	usageReq.URL.Path = "/api/issues/window-usage"
	h.GetIssueWindowUsage(usageRecorder, usageReq)
	if usageRecorder.Code != http.StatusOK {
		t.Fatalf("usage status = %d: %s", usageRecorder.Code, usageRecorder.Body.String())
	}
	var usage IssueWindowUsageResponse
	if err := json.NewDecoder(usageRecorder.Body).Decode(&usage); err != nil {
		t.Fatalf("decode usage: %v", err)
	}
	if usage.Used == nil || *usage.Used != 1 || usage.Limit == nil || *usage.Limit != 1 || usage.HasMore == nil || !*usage.HasMore {
		t.Fatalf("unexpected bounded usage: %#v", usage)
	}
	childrenRecorder := httptest.NewRecorder()
	childrenReq := withURLParam(request(rootID), "id", rootID)
	h.ListChildIssues(childrenRecorder, childrenReq)
	if childrenRecorder.Code != http.StatusOK {
		t.Fatalf("children status = %d: %s", childrenRecorder.Code, childrenRecorder.Body.String())
	}
	var childrenBody struct {
		Issues []IssueResponse `json:"issues"`
	}
	if err := json.NewDecoder(childrenRecorder.Body).Decode(&childrenBody); err != nil {
		t.Fatalf("decode children: %v", err)
	}
	if len(childrenBody.Issues) != 1 || childrenBody.Issues[0].ID != newestChildID {
		t.Fatalf("children bypassed window: %#v", childrenBody.Issues)
	}

	// Loading the same UUID through another workspace still produces a 404,
	// never the commercial response that would reveal same-workspace existence.
	otherWorkspaceID := dbfx.Workspace(t, "Other issue window", "other-window-"+uuid.NewString())
	crossReq := request(hiddenSiblingID)
	crossReq.Header.Set("X-Workspace-ID", otherWorkspaceID)
	cross := httptest.NewRecorder()
	if _, ok := h.loadIssueForUser(cross, crossReq, hiddenSiblingID); ok || cross.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace load = ok:%v status:%d body:%s", ok, cross.Code, cross.Body.String())
	}
}

func TestIssueCreationWindowBatchReparentCannotExposeHiddenParent(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Batch reparent issue window", "batch-reparent-window-"+uuid.NewString(), testutil.Cols{
		"issue_prefix": "BRW",
	})
	dbfx.Member(t, workspaceID, testUserID, "owner")
	var maxNumber int
	if err := testPool.QueryRow(context.Background(), `SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1`, workspaceID).Scan(&maxNumber); err != nil {
		t.Fatalf("load issue number: %v", err)
	}
	hiddenParentID := dbfx.Issue(t, "hidden parent", testutil.Cols{
		"workspace_id": workspaceID,
		"number":       maxNumber + 1,
	})
	targetID := dbfx.Issue(t, "visible target", testutil.Cols{
		"workspace_id": workspaceID,
		"number":       maxNumber + 2,
	})

	h := *testHandler
	h.Entitlements = issueWindowProvider(entitlement.ActionEnforce, 1)
	recorder := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-update?workspace_id="+workspaceID, map[string]any{
		"issue_ids": []string{targetID},
		"updates": map[string]any{
			"parent_issue_id": hiddenParentID,
		},
	})
	req.Header.Set("X-Workspace-ID", workspaceID)
	h.BatchUpdateIssues(recorder, req)
	if recorder.Code != http.StatusPaymentRequired {
		t.Fatalf("batch reparent status = %d, want 402: %s", recorder.Code, recorder.Body.String())
	}
	var parentID *string
	if err := testPool.QueryRow(context.Background(), `SELECT parent_issue_id::text FROM issue WHERE id = $1`, targetID).Scan(&parentID); err != nil {
		t.Fatalf("reload target: %v", err)
	}
	if parentID != nil {
		t.Fatalf("target was reparented to hidden issue %q after authorization failure", *parentID)
	}
}

func TestIssueCreationWindowObserveDoesNotFilter(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Observed issue window", "observed-window-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	oldID := dbfx.Issue(t, "observed old", testutil.Cols{"workspace_id": workspaceID, "number": 1})
	_ = dbfx.Issue(t, "observed new", testutil.Cols{"workspace_id": workspaceID, "number": 2})

	h := *testHandler
	h.Entitlements = issueWindowProvider(entitlement.ActionObserve, 1)
	req := httptest.NewRequest(http.MethodGet, "/api/issues/"+oldID, nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", workspaceID)
	recorder := httptest.NewRecorder()
	if _, ok := h.loadIssueForUser(recorder, req, oldID); !ok {
		t.Fatalf("observe changed direct response: %d %s", recorder.Code, recorder.Body.String())
	}
	listRecorder := httptest.NewRecorder()
	listReq := req.Clone(req.Context())
	listReq.URL.Path = "/api/issues"
	h.ListIssues(listRecorder, listReq)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("observe list status = %d: %s", listRecorder.Code, listRecorder.Body.String())
	}
	var body struct {
		Issues []IssueResponse `json:"issues"`
		Total  int64           `json:"total"`
	}
	if err := json.NewDecoder(listRecorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode observe list: %v", err)
	}
	if len(body.Issues) != 2 || body.Total != 2 {
		t.Fatalf("observe filtered the list: %#v", body)
	}
}

func TestIssueCreationWindowPreservesUnreadInboxSemantics(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Inbox issue window", "inbox-window-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	hiddenIssueID := dbfx.Issue(t, "hidden inbox issue", testutil.Cols{"workspace_id": workspaceID, "number": 1})
	visibleIssueID := dbfx.Issue(t, "visible inbox issue", testutil.Cols{"workspace_id": workspaceID, "number": 2})

	insertInbox := func(title, issueID string, read bool, createdAt string) {
		t.Helper()
		dbfx.Insert(t, "inbox_item", testutil.Cols{
			"workspace_id":   workspaceID,
			"recipient_type": "member",
			"recipient_id":   testUserID,
			"type":           "issue_assigned",
			"severity":       "info",
			"issue_id":       issueID,
			"title":          title,
			"read":           read,
			"archived":       false,
			"created_at":     testutil.Raw(createdAt),
		})
	}
	// The visible issue's newest row is read, so its older unread sibling must
	// not light the cross-workspace summary (MUL-3695).
	insertInbox("visible older unread", visibleIssueID, false, "now() - interval '2 minutes'")
	insertInbox("visible newest read", visibleIssueID, true, "now() - interval '1 minute'")
	insertInbox("hidden newest unread", hiddenIssueID, false, "now()")

	request := func(path string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-User-ID", testUserID)
		req.Header.Set("X-Workspace-ID", workspaceID)
		return req
	}
	assertCounts := func(t *testing.T, action entitlement.Action, wantWorkspaceCount, wantSummaryCount int64) {
		t.Helper()
		h := *testHandler
		h.Entitlements = issueWindowProvider(action, 1)

		countRecorder := httptest.NewRecorder()
		middleware.RequireWorkspaceMember(h.Queries)(http.HandlerFunc(h.CountUnreadInbox)).ServeHTTP(
			countRecorder,
			request("/api/inbox/unread-count"),
		)
		if countRecorder.Code != http.StatusOK {
			t.Fatalf("unread count status = %d: %s", countRecorder.Code, countRecorder.Body.String())
		}
		var countBody map[string]int64
		if err := json.NewDecoder(countRecorder.Body).Decode(&countBody); err != nil {
			t.Fatalf("decode unread count: %v", err)
		}
		if countBody["count"] != wantWorkspaceCount {
			t.Fatalf("unread count = %d, want %d", countBody["count"], wantWorkspaceCount)
		}

		summaryRecorder := httptest.NewRecorder()
		h.UnreadInboxSummary(summaryRecorder, request("/api/inbox/unread-summary"))
		if summaryRecorder.Code != http.StatusOK {
			t.Fatalf("unread summary status = %d: %s", summaryRecorder.Code, summaryRecorder.Body.String())
		}
		var summary []InboxWorkspaceUnreadResponse
		if err := json.NewDecoder(summaryRecorder.Body).Decode(&summary); err != nil {
			t.Fatalf("decode unread summary: %v", err)
		}
		var got int64
		for _, row := range summary {
			if row.WorkspaceID == workspaceID {
				got = row.Count
			}
		}
		if got != wantSummaryCount {
			t.Fatalf("unread summary = %d, want %d: %#v", got, wantSummaryCount, summary)
		}
	}

	t.Run("observe leaves both legacy responses unchanged", func(t *testing.T) {
		assertCounts(t, entitlement.ActionObserve, 2, 1)
	})
	t.Run("enforce filters hidden issues without reviving older siblings", func(t *testing.T) {
		assertCounts(t, entitlement.ActionEnforce, 1, 0)
	})
}

func TestIssueCreationWindowFiltersArchivedInboxBeforeGroupLimit(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Archived inbox issue window", "archived-inbox-window-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	parentID := dbfx.Issue(t, "visible archived parent", testutil.Cols{
		"workspace_id": workspaceID,
		"number":       1,
	})
	for number := 2; number <= 201; number++ {
		cols := testutil.Cols{
			"workspace_id": workspaceID,
			"number":       number,
		}
		// Keep the newest issue in the window connected to the visible parent so
		// the ancestor backfill is exercised by the fixture.
		if number == 201 {
			cols["parent_issue_id"] = parentID
		}
		issueID := dbfx.Issue(t, "hidden archived issue", cols)
		dbfx.Insert(t, "inbox_item", testutil.Cols{
			"workspace_id":   workspaceID,
			"recipient_type": "member",
			"recipient_id":   testUserID,
			"type":           "status_changed",
			"severity":       "info",
			"issue_id":       issueID,
			"title":          "hidden archived notification",
			"archived":       true,
		})
	}
	dbfx.Insert(t, "inbox_item", testutil.Cols{
		"workspace_id":   workspaceID,
		"recipient_type": "member",
		"recipient_id":   testUserID,
		"type":           "status_changed",
		"severity":       "info",
		"issue_id":       parentID,
		"title":          "visible archived notification",
		"archived":       true,
	})

	h := *testHandler
	h.Entitlements = issueWindowProvider(entitlement.ActionEnforce, 1)
	req := inboxRequest(http.MethodGet, "/api/inbox/archived", workspaceID)
	recorder := httptest.NewRecorder()
	middleware.RequireWorkspaceMember(h.Queries)(http.HandlerFunc(h.ListArchivedInbox)).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("archived inbox status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var items []InboxItemResponse
	if err := json.NewDecoder(recorder.Body).Decode(&items); err != nil {
		t.Fatalf("decode archived inbox: %v", err)
	}
	for _, item := range items {
		if item.IssueID != nil && *item.IssueID == parentID {
			return
		}
	}
	t.Fatalf("visible ancestor archived group missing from response: %#v", items)
}

func TestIssueCreationWindowChildProgressHidesOutsideWindow(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Progress issue window", "progress-window-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	parentID := dbfx.Issue(t, "progress parent", testutil.Cols{"workspace_id": workspaceID, "number": 100})
	_ = dbfx.Issue(t, "hidden done child", testutil.Cols{
		"workspace_id": workspaceID, "number": 1, "parent_issue_id": parentID, "status": "done",
	})
	_ = dbfx.Issue(t, "hidden open child", testutil.Cols{
		"workspace_id": workspaceID, "number": 2, "parent_issue_id": parentID, "status": "todo",
	})
	_ = dbfx.Issue(t, "unrelated recent issue", testutil.Cols{"workspace_id": workspaceID, "number": 200})
	_ = dbfx.Issue(t, "visible open child", testutil.Cols{
		"workspace_id": workspaceID, "number": 300, "parent_issue_id": parentID, "status": "todo",
	})
	visibleParentOnlyID := dbfx.Issue(t, "visible parent with hidden child", testutil.Cols{
		"workspace_id": workspaceID,
		"number":       400,
	})
	_ = dbfx.Issue(t, "hidden only child", testutil.Cols{
		"workspace_id":    workspaceID,
		"number":          250,
		"parent_issue_id": visibleParentOnlyID,
	})

	h := *testHandler
	h.Entitlements = issueWindowProvider(entitlement.ActionEnforce, 2)
	req := httptest.NewRequest(http.MethodGet, "/api/issues/child-progress", nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", workspaceID)
	recorder := httptest.NewRecorder()
	h.ChildIssueProgress(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("child progress status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Progress []struct {
			ParentIssueID string `json:"parent_issue_id"`
			Total         int64  `json:"total"`
			Done          int64  `json:"done"`
		} `json:"progress"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode child progress: %v", err)
	}
	foundVisibleParent := false
	for _, progress := range body.Progress {
		switch progress.ParentIssueID {
		case parentID:
			if progress.Total != 1 || progress.Done != 0 {
				t.Fatalf("child progress = %#v, want visible 0/1 without hidden totals", progress)
			}
			foundVisibleParent = true
		case visibleParentOnlyID:
			t.Fatalf("child progress exposed hidden-only children: %#v", progress)
		}
	}
	if !foundVisibleParent {
		t.Fatalf("visible parent missing from child progress: %#v", body.Progress)
	}
}

func TestIssueCreationWindowProtectsBatchMutationsAndParentedCreates(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Issue window mutations", "issue-window-mutations-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	hiddenID := dbfx.Issue(t, "hidden issue", testutil.Cols{
		"workspace_id": workspaceID,
		"number":       1,
	})
	visibleID := dbfx.Issue(t, "visible issue", testutil.Cols{
		"workspace_id": workspaceID,
		"number":       2,
	})

	h := *testHandler
	h.Entitlements = issueWindowProvider(entitlement.ActionEnforce, 1)
	issueService := *h.IssueService
	issueService.Entitlements = h.Entitlements
	h.IssueService = &issueService
	request := func(method, path string, body string) *http.Request {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("X-User-ID", testUserID)
		req.Header.Set("X-Workspace-ID", workspaceID)
		return req
	}

	createRecorder := httptest.NewRecorder()
	h.CreateIssue(createRecorder, request(http.MethodPost, "/api/issues", fmt.Sprintf(`{"title":"child under hidden","parent_issue_id":%q}`, hiddenID)))
	if createRecorder.Code != http.StatusPaymentRequired {
		t.Fatalf("parented create status = %d, want 402: %s", createRecorder.Code, createRecorder.Body.String())
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM issue WHERE workspace_id = $1 AND title = $2", workspaceID, "child under hidden"); got != 0 {
		t.Fatalf("window-blocked child create wrote %d rows", got)
	}

	updateRecorder := httptest.NewRecorder()
	h.BatchUpdateIssues(updateRecorder, request(http.MethodPatch, "/api/issues/batch", fmt.Sprintf(`{"issue_ids":[%q],"updates":{"title":"should not update"}}`, hiddenID)))
	if updateRecorder.Code != http.StatusPaymentRequired {
		t.Fatalf("batch update status = %d, want 402: %s", updateRecorder.Code, updateRecorder.Body.String())
	}
	var title string
	dbfx.QueryRow(t, "SELECT title FROM issue WHERE id = $1", hiddenID).Scan(&title)
	if title != "hidden issue" {
		t.Fatalf("window-blocked batch update changed title to %q", title)
	}

	deleteRecorder := httptest.NewRecorder()
	h.BatchDeleteIssues(deleteRecorder, request(http.MethodPost, "/api/issues/batch-delete", fmt.Sprintf(`{"issue_ids":[%q]}`, hiddenID)))
	if deleteRecorder.Code != http.StatusPaymentRequired {
		t.Fatalf("batch delete status = %d, want 402: %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM issue WHERE id = $1", hiddenID); got != 1 {
		t.Fatalf("window-blocked batch delete removed hidden issue; count=%d", got)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM issue WHERE id = $1", visibleID); got != 1 {
		t.Fatalf("control visible issue count=%d, want 1", got)
	}
}

func TestIssueCreationWindowProtectsInboxBatchMutations(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Inbox window mutations", "inbox-window-mutations-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	h := *testHandler
	h.Entitlements = issueWindowProvider(entitlement.ActionEnforce, 1)
	request := func(path string) *http.Request {
		req := inboxRequest(http.MethodPost, path, workspaceID)
		return req
	}
	call := func(fn http.HandlerFunc, path string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		middleware.RequireWorkspaceMember(h.Queries)(fn).ServeHTTP(recorder, request(path))
		return recorder
	}
	insert := func(issueID, title string, read, archived bool) {
		dbfx.Insert(t, "inbox_item", testutil.Cols{
			"workspace_id":   workspaceID,
			"recipient_type": "member",
			"recipient_id":   testUserID,
			"type":           "status_changed",
			"severity":       "info",
			"issue_id":       issueID,
			"title":          title,
			"read":           read,
			"archived":       archived,
		})
	}
	newIssuePair := func(hiddenNumber, visibleNumber int32) (string, string) {
		hiddenID := dbfx.Issue(t, "hidden inbox issue", testutil.Cols{
			"workspace_id": workspaceID,
			"number":       hiddenNumber,
		})
		visibleID := dbfx.Issue(t, "visible inbox issue", testutil.Cols{
			"workspace_id": workspaceID,
			"number":       visibleNumber,
		})
		return hiddenID, visibleID
	}

	hiddenID, visibleID := newIssuePair(1, 2)
	insert(hiddenID, "hidden mark", false, false)
	insert(visibleID, "visible mark", false, false)
	if recorder := call(h.MarkAllInboxRead, "/api/inbox/mark-all-read"); recorder.Code != http.StatusOK {
		t.Fatalf("mark-all status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var hiddenRead, visibleRead bool
	dbfx.QueryRow(t, "SELECT read FROM inbox_item WHERE issue_id = $1", hiddenID).Scan(&hiddenRead)
	dbfx.QueryRow(t, "SELECT read FROM inbox_item WHERE issue_id = $1", visibleID).Scan(&visibleRead)
	if hiddenRead || !visibleRead {
		t.Fatalf("mark-all read states hidden=%v visible=%v", hiddenRead, visibleRead)
	}

	hiddenID, visibleID = newIssuePair(3, 4)
	insert(hiddenID, "hidden archive", false, false)
	insert(visibleID, "visible archive", false, false)
	if recorder := call(h.ArchiveAllInbox, "/api/inbox/archive-all"); recorder.Code != http.StatusOK {
		t.Fatalf("archive-all status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id = $1 AND archived", hiddenID); got != 0 {
		t.Fatalf("archive-all archived hidden rows=%d", got)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id = $1 AND archived", visibleID); got != 1 {
		t.Fatalf("archive-all archived visible rows=%d, want 1", got)
	}

	hiddenID, visibleID = newIssuePair(5, 6)
	insert(hiddenID, "hidden read archive", true, false)
	insert(visibleID, "visible read archive", true, false)
	if recorder := call(h.ArchiveAllReadInbox, "/api/inbox/archive-all-read"); recorder.Code != http.StatusOK {
		t.Fatalf("archive-all-read status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id = $1 AND archived", hiddenID); got != 0 {
		t.Fatalf("archive-all-read archived hidden rows=%d", got)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id = $1 AND archived", visibleID); got != 1 {
		t.Fatalf("archive-all-read archived visible rows=%d, want 1", got)
	}

	hiddenID, visibleID = newIssuePair(7, 8)
	dbfx.Exec(t, "UPDATE issue SET status = 'done' WHERE id = ANY($1::uuid[])", []string{hiddenID, visibleID})
	insert(hiddenID, "hidden completed", false, false)
	insert(visibleID, "visible completed", false, false)
	if recorder := call(h.ArchiveCompletedInbox, "/api/inbox/archive-completed"); recorder.Code != http.StatusOK {
		t.Fatalf("archive-completed status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id = $1 AND archived", hiddenID); got != 0 {
		t.Fatalf("archive-completed archived hidden rows=%d", got)
	}
	if got := dbfx.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id = $1 AND archived", visibleID); got != 1 {
		t.Fatalf("archive-completed archived visible rows=%d, want 1", got)
	}
}
