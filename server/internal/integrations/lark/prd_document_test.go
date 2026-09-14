package lark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The fake exposes only the real OpenAPI paths used by publication. Unexpected
// calls (including public sharing, blank-doc creation or source edits) fail.
type prdDocumentFake struct {
	mu                         sync.Mutex
	t                          *testing.T
	server                     *httptest.Server
	blocks                     []map[string]any
	owner                      string
	metadataURL                string
	revision                   int64
	copies, inserts, transfers int
	copyStatus                 int
	copyCode                   int
	copyRaw                    string
	copyDisconnect             bool
	copyRedirect               string
	wikiType                   string
	loseInsertResponse         bool
	ignoreInsert               bool
	ignoreTransfer             bool
	loopPage                   bool
	rejectPinnedBlocks         bool
	tokens                     map[string]bool
}

func prdTestText(text string) map[string]any {
	return map[string]any{"elements": []any{map[string]any{"text_run": map[string]string{"content": text}}}}
}

func newPRDDocumentFake(t *testing.T) (*prdDocumentFake, PRDDocumentClient) {
	t.Helper()
	f := &prdDocumentFake{t: t, owner: "ou_bot", metadataURL: "https://tenant.feishu.cn/docx/copy1", revision: 1, wikiType: "docx", tokens: map[string]bool{}}
	f.blocks = []map[string]any{
		{"block_id": "copy1", "block_type": 1, "children": []string{"h1", "placeholder", "table", "h2", "h3"}},
		{"block_id": "h1", "parent_id": "copy1", "block_type": 3, "heading1": prdTestText("需求背景")},
		{"block_id": "placeholder", "parent_id": "copy1", "block_type": 2, "text": prdTestText("【待确认】原模板提示")},
		{"block_id": "table", "parent_id": "copy1", "block_type": 31, "table": map[string]any{"property": map[string]int{"row_size": 1, "column_size": 1}}},
		{"block_id": "h2", "parent_id": "copy1", "block_type": 4, "heading2": prdTestText("项目目标")},
		{"block_id": "h3", "parent_id": "copy1", "block_type": 3, "heading1": prdTestText("附录")},
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f, NewHTTPAPIClient(HTTPClientConfig{BaseURL: f.server.URL}).(PRDDocumentClient)
}

func TestPRDSnapshotFallsBackToLatestBlocksWhenPinnedRevisionForbidden(t *testing.T) {
	f, client := newPRDDocumentFake(t)
	f.rejectPinnedBlocks = true
	c := client.(*httpAPIClient)
	snapshot, err := c.prdSnapshot(context.Background(), InstallationCredentials{AppID: "test-app", AppSecret: "test-secret"}, "copy1")
	if err != nil {
		t.Fatalf("prdSnapshot: %v", err)
	}
	if snapshot.Revision != f.revision {
		t.Fatalf("revision = %d, want %d", snapshot.Revision, f.revision)
	}
	if len(snapshot.Blocks) != len(f.blocks) {
		t.Fatalf("blocks = %d, want %d", len(snapshot.Blocks), len(f.blocks))
	}
}

func (f *prdDocumentFake) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal" {
		writeJSON(w, map[string]any{"code": 0, "tenant_access_token": "testtoken", "expire": 7200})
		return
	}
	if r.Header.Get("Authorization") != "Bearer testtoken" {
		f.t.Error("missing installation token")
	}
	respond := func(data any) { writeJSON(w, map[string]any{"code": 0, "data": data}) }
	switch r.Method + " " + r.URL.Path {
	case "GET /open-apis/wiki/v2/spaces/get_node":
		if r.URL.Query().Get("token") != "wiki1" || r.URL.Query().Get("obj_type") != "wiki" {
			f.t.Error("incorrect wiki resolution")
		}
		respond(map[string]any{"node": map[string]string{"node_token": "wiki1", "obj_token": "source1", "obj_type": f.wikiType}})
	case "POST /open-apis/drive/v1/metas/batch_query":
		var body struct {
			Docs []struct {
				Token string `json:"doc_token"`
				Type  string `json:"doc_type"`
			} `json:"request_docs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Docs) != 1 {
			f.t.Error("invalid metadata request")
			w.WriteHeader(400)
			return
		}
		if body.Docs[0].Type != "docx" || r.URL.Query().Get("user_id_type") != "open_id" {
			f.t.Error("metadata must use docx and open_id")
		}
		respond(map[string]any{"metas": []any{map[string]string{"doc_token": body.Docs[0].Token, "doc_type": "docx", "owner_id": f.owner, "url": f.metadataURL}}})
	case "GET /open-apis/drive/explorer/v2/root_folder/meta":
		respond(map[string]string{"token": "root1"})
	case "POST /open-apis/drive/v1/files/source1/copy":
		f.copies++
		if f.copyRedirect != "" {
			http.Redirect(w, r, f.copyRedirect, http.StatusTemporaryRedirect)
			return
		}
		if f.copyDisconnect {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				f.t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.t.Error(err)
		}
		if body["type"] != "docx" || body["folder_token"] != "root1" || body["name"] == "" {
			f.t.Error("copy must preserve template type and use resolved root")
		}
		if f.copyStatus != 0 {
			w.WriteHeader(f.copyStatus)
		}
		if f.copyRaw != "" {
			_, _ = w.Write([]byte(f.copyRaw))
			return
		}
		if f.copyCode != 0 {
			writeJSON(w, map[string]any{"code": f.copyCode, "msg": "copy rejected"})
			return
		}
		respond(map[string]any{"file": map[string]string{"token": "copy1", "type": "docx", "url": "https://tenant.feishu.cn/docx/copy1"}})
	case "GET /open-apis/docx/v1/documents/copy1":
		respond(map[string]any{"document": map[string]any{"document_id": "copy1", "revision_id": f.revision}})
	case "GET /open-apis/docx/v1/documents/copy1/blocks":
		if f.rejectPinnedBlocks && r.URL.Query().Get("document_revision_id") != "-1" {
			writeJSON(w, map[string]any{"code": 1770032, "msg": "forbidden"})
			return
		}
		if !f.rejectPinnedBlocks && r.URL.Query().Get("document_revision_id") != strconv.FormatInt(f.revision, 10) {
			f.t.Error("block reads must pin revision")
		}
		if f.loopPage {
			respond(map[string]any{"items": []any{f.blocks[0]}, "has_more": true, "page_token": "loop"})
			return
		}
		if r.URL.Query().Get("page_token") == "" {
			respond(map[string]any{"items": f.blocks[:2], "has_more": true, "page_token": "next"})
		} else {
			respond(map[string]any{"items": f.blocks[2:], "has_more": false})
		}
	case "POST /open-apis/docx/v1/documents/copy1/blocks/copy1/children":
		var body struct {
			Children []map[string]any `json:"children"`
			Index    int              `json:"index"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.t.Error(err)
			w.WriteHeader(400)
			return
		}
		if r.URL.Query().Get("document_revision_id") != strconv.FormatInt(f.revision, 10) {
			w.WriteHeader(400)
			writeJSON(w, map[string]any{"code": 1770002})
			return
		}
		token := r.URL.Query().Get("client_token")
		if token == "" {
			f.t.Error("missing idempotency token")
		}
		if f.tokens[token] {
			respond(map[string]any{})
			return
		}
		f.tokens[token] = true
		f.inserts++
		if !f.ignoreInsert {
			root := f.blocks[0]["children"].([]string)
			ids := make([]string, 0, len(body.Children))
			for i, block := range body.Children {
				id := fmt.Sprintf("insert%d_%d", f.inserts, i)
				block["block_id"], block["parent_id"] = id, "copy1"
				ids = append(ids, id)
				f.blocks = append(f.blocks, block)
			}
			updated := append([]string{}, root[:body.Index]...)
			updated = append(updated, ids...)
			updated = append(updated, root[body.Index:]...)
			f.blocks[0]["children"] = updated
			f.revision++
		}
		if f.loseInsertResponse {
			f.loseInsertResponse = false
			w.WriteHeader(500)
			_, _ = w.Write([]byte("lost response"))
			return
		}
		respond(map[string]any{})
	case "POST /open-apis/drive/v1/permissions/copy1/members/transfer_owner":
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.t.Error(err)
		}
		if body["member_type"] != "openid" || r.URL.Query().Get("type") != "docx" || r.URL.Query().Get("old_owner_perm") != "edit" || r.URL.Query().Get("remove_old_owner") != "false" {
			f.t.Error("owner transfer must preserve only bot edit access")
		}
		f.transfers++
		if !f.ignoreTransfer {
			f.owner = body["member_id"]
		}
		respond(map[string]any{})
	default:
		f.t.Errorf("unexpected OpenAPI operation: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestPRDDocumentTemplateCopyFillAndOwner(t *testing.T) {
	f, client := newPRDDocumentFake(t)
	ctx := context.Background()
	token, err := client.ResolvePRDTemplate(ctx, testCreds(), "wiki1")
	if err != nil || token != "source1" {
		t.Fatalf("template resolution: %q %v", token, err)
	}
	doc, err := client.CopyPRDTemplate(ctx, testCreds(), token, "PRD-验证")
	if err != nil || doc.ID != "copy1" || doc.URL != "https://tenant.feishu.cn/docx/copy1" {
		t.Fatalf("copy: %+v %v", doc, err)
	}
	sections := []PRDSection{{Heading: "需求背景", Body: "**literal text**\n字段：原样保留"}, {Heading: "项目目标", Body: ""}}
	if err := client.FillPRDDocument(ctx, testCreds(), doc.ID, sections); err != nil {
		t.Fatal(err)
	}
	if err := client.FillPRDDocument(ctx, testCreds(), doc.ID, sections); err != nil {
		t.Fatal(err)
	}
	if err := client.FinalizePRDOwner(ctx, testCreds(), doc.ID, "ou_requester"); err != nil {
		t.Fatal(err)
	}
	if err := client.FinalizePRDOwner(ctx, testCreds(), doc.ID, "ou_requester"); err != nil {
		t.Fatal(err)
	}
	if err := client.VerifyPRDDocument(ctx, testCreds(), doc.ID, sections, "ou_requester"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.copies != 1 || f.inserts != 2 || f.transfers != 1 {
		t.Fatalf("duplicate side effects: copy=%d insert=%d transfer=%d", f.copies, f.inserts, f.transfers)
	}
	want := []string{"h1", "insert1_0", "insert1_1", "placeholder", "table", "h2", "insert2_0", "h3"}
	if !reflect.DeepEqual(f.blocks[0]["children"], want) {
		t.Fatalf("template order/structure lost: %v", f.blocks[0]["children"])
	}
	if !reflect.DeepEqual(f.blocks[2]["text"], prdTestText("【待确认】原模板提示")) || f.blocks[3]["block_type"] != 31 {
		t.Fatal("template placeholder or table changed")
	}
}

func TestPRDDocumentResumesAcceptedFillAfterLostResponse(t *testing.T) {
	f, client := newPRDDocumentFake(t)
	f.loseInsertResponse = true
	sections := []PRDSection{{Heading: "需求背景", Body: "saved before response"}, {Heading: "项目目标", Body: "next section"}}
	if err := client.FillPRDDocument(context.Background(), testCreds(), "copy1", sections); err == nil {
		t.Fatal("expected lost-response error")
	}
	if err := client.FillPRDDocument(context.Background(), testCreds(), "copy1", sections); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.inserts != 2 {
		t.Fatalf("resume duplicated saved content: %d writes", f.inserts)
	}
}

func TestPRDDocumentRejectsInvalidTemplateBeforeWriting(t *testing.T) {
	for _, mode := range []string{"missing", "ambiguous", "pagination"} {
		t.Run(mode, func(t *testing.T) {
			f, client := newPRDDocumentFake(t)
			sections := []PRDSection{{Heading: "需求背景", Body: "valid first"}, {Heading: "项目目标", Body: "second"}}
			switch mode {
			case "missing":
				sections[1].Heading = "not a template heading"
			case "ambiguous":
				f.blocks[5]["heading1"] = prdTestText("项目目标")
			case "pagination":
				f.loopPage = true
			}
			if err := client.FillPRDDocument(context.Background(), testCreds(), "copy1", sections); err == nil {
				t.Fatal("invalid template accepted")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.inserts != 0 {
				t.Fatal("partially wrote before template validation")
			}
		})
	}
}

func TestPRDDocumentRequiresReadback(t *testing.T) {
	for _, mode := range []string{"fill", "owner", "wrong_owner", "wrong_section"} {
		t.Run(mode, func(t *testing.T) {
			f, client := newPRDDocumentFake(t)
			sections := []PRDSection{{Heading: "需求背景", Body: "expected"}}
			var err error
			switch mode {
			case "fill":
				f.ignoreInsert = true
				err = client.FillPRDDocument(context.Background(), testCreds(), "copy1", sections)
			case "owner":
				f.ignoreTransfer = true
				err = client.FinalizePRDOwner(context.Background(), testCreds(), "copy1", "ou_requester")
			case "wrong_owner":
				if fillErr := client.FillPRDDocument(context.Background(), testCreds(), "copy1", sections); fillErr != nil {
					t.Fatal(fillErr)
				}
				err = client.VerifyPRDDocument(context.Background(), testCreds(), "copy1", sections, "ou_other")
			case "wrong_section":
				err = client.VerifyPRDDocument(context.Background(), testCreds(), "copy1", sections, "ou_bot")
			}
			if err == nil {
				t.Fatal("unverified publication accepted")
			}
		})
	}
}

func TestPRDCopyErrorsNeverBlindlyRetry(t *testing.T) {
	cases := []struct {
		name         string
		status, code int
		raw          string
		unknown      bool
		knownID      string
	}{
		{name: "forbidden", status: 403, code: 1061004},
		{name: "token", status: 400, code: 99991663},
		{name: "rate_limit", status: 400, code: 99991400},
		{name: "internal", status: 500, code: 1061001, unknown: true},
		{name: "contention", status: 400, code: 1061045, unknown: true},
		{name: "malformed", raw: "not json", unknown: true},
		{name: "missing_code", raw: `{"data":{"file":{"token":"copy1"}}}`, unknown: true},
		{name: "missing_id", raw: `{"code":0,"data":{"file":{"type":"docx"}}}`, unknown: true},
		{name: "unsafe_url", raw: `{"code":0,"data":{"file":{"token":"copy1","type":"docx","url":"https://evil.example/docx/copy1"}}}`, unknown: true, knownID: "copy1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, client := newPRDDocumentFake(t)
			f.copyStatus, f.copyCode, f.copyRaw = tc.status, tc.code, tc.raw
			doc, err := client.CopyPRDTemplate(context.Background(), testCreds(), "source1", "PRD")
			var copyErr *PRDCopyError
			if !errors.As(err, &copyErr) || copyErr.OutcomeUnknown != tc.unknown {
				t.Fatalf("classification: %v", err)
			}
			if doc.ID != tc.knownID {
				t.Fatalf("lost known copy ID: %+v", doc)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.copies != 1 {
				t.Fatalf("copy attempted %d times", f.copies)
			}
		})
	}
}

func TestPRDDocumentRejectsNonDocxAndInvalidInput(t *testing.T) {
	f, client := newPRDDocumentFake(t)
	f.wikiType = "sheet"
	if _, err := client.ResolvePRDTemplate(context.Background(), testCreds(), "wiki1"); err == nil {
		t.Fatal("sheet template accepted")
	}
	if _, err := client.ResolvePRDTemplate(context.Background(), testCreds(), "https://evil.example/wiki/x"); err == nil {
		t.Fatal("arbitrary URL accepted")
	}
	_, err := client.CopyPRDTemplate(context.Background(), testCreds(), "source1", strings.Repeat("界", 86))
	var copyErr *PRDCopyError
	if !errors.As(err, &copyErr) || copyErr.OutcomeUnknown {
		t.Fatalf("title validation is a known unattempted copy: %v", err)
	}
	if err := client.FillPRDDocument(context.Background(), testCreds(), "copy1", []PRDSection{{Heading: "需求背景", Body: strings.Repeat("x\n", 51)}}); err == nil {
		t.Fatal("unbounded block insertion accepted")
	}
	if err := client.FinalizePRDOwner(context.Background(), testCreds(), "copy1", "cli_bot"); err == nil {
		t.Fatal("non-user owner accepted")
	}
}

func TestPRDCopyCanceledPreflightIsKnown(t *testing.T) {
	f, client := newPRDDocumentFake(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.CopyPRDTemplate(ctx, testCreds(), "source1", "PRD")
	var copyErr *PRDCopyError
	if !errors.As(err, &copyErr) || copyErr.OutcomeUnknown {
		t.Fatalf("preflight cancellation cannot have copied: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.copies != 0 {
		t.Fatal("canceled preflight copied")
	}
}

func TestPRDMessageReadbackCarriesActualChat(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("tok", 7200)
	response := func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"items": []any{map[string]any{"message_id": "om_confirm", "chat_id": "oc_actual", "thread_id": "omt_actual", "msg_type": "text", "body": map[string]string{"content": `{"text":"confirmation"}`}}}}})
	}
	fake.mux.HandleFunc("/open-apis/im/v1/messages/", response)
	fake.mux.HandleFunc("/open-apis/im/v1/messages", response)
	client := newTestClient(fake, time.Now)
	messages, err := client.GetMessage(context.Background(), testCreds(), "om_confirm")
	if err != nil || len(messages) != 1 || messages[0].ChatID != "oc_actual" {
		t.Fatalf("get message scope evidence: %+v %v", messages, err)
	}
	messages, err = client.ListChatMessages(context.Background(), testCreds(), ListMessagesParams{ChatID: "oc_requested", PageSize: 1})
	if err != nil || len(messages) != 1 || messages[0].ChatID != "oc_actual" {
		t.Fatalf("list must not infer chat from request: %+v %v", messages, err)
	}
}

func TestPRDCopyLostResponseIsUnknown(t *testing.T) {
	f, client := newPRDDocumentFake(t)
	f.copyDisconnect = true
	_, err := client.CopyPRDTemplate(context.Background(), testCreds(), "source1", "PRD")
	var copyErr *PRDCopyError
	if !errors.As(err, &copyErr) || !copyErr.OutcomeUnknown {
		t.Fatalf("lost response must remain unknown: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.copies != 1 {
		t.Fatalf("uncertain copy replayed: %d", f.copies)
	}
}

func TestPRDDocumentRefusesRedirectAndOversizedResponse(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("document request followed an arbitrary redirect")
	}))
	defer target.Close()
	for _, mode := range []string{"redirect", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			f, client := newPRDDocumentFake(t)
			if mode == "redirect" {
				f.copyRedirect = target.URL
			} else {
				f.copyRaw = strings.Repeat("x", prdMaxResponseBytes+1)
			}
			_, err := client.CopyPRDTemplate(context.Background(), testCreds(), "source1", "PRD")
			var copyErr *PRDCopyError
			if !errors.As(err, &copyErr) || !copyErr.OutcomeUnknown {
				t.Fatalf("invalid copy response accepted: %v", err)
			}
		})
	}
}

func TestPRDDocumentRecoversKnownCopyURLWithoutRecreating(t *testing.T) {
	f, client := newPRDDocumentFake(t)
	f.copyRaw = `{"code":0,"data":{"file":{"token":"copy1","type":"docx"}}}`
	doc, err := client.CopyPRDTemplate(context.Background(), testCreds(), "source1", "PRD")
	if err == nil || doc.ID != "copy1" || doc.URL != "" {
		t.Fatalf("expected known ID without usable URL: %+v %v", doc, err)
	}
	link, err := client.GetPRDDocumentURL(context.Background(), testCreds(), doc.ID)
	if err != nil || link != "https://tenant.feishu.cn/docx/copy1" {
		t.Fatalf("metadata recovery: %q %v", link, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.copies != 1 {
		t.Fatalf("URL recovery recreated document: %d copies", f.copies)
	}
}

func TestPRDDocumentURLRecoveryRejectsWrongResource(t *testing.T) {
	for _, link := range []string{"", "https://evil.example/docx/copy1", "https://tenant.feishu.cn/docx/other", "http://tenant.feishu.cn/docx/copy1"} {
		t.Run(link, func(t *testing.T) {
			f, client := newPRDDocumentFake(t)
			f.metadataURL = link
			if _, err := client.GetPRDDocumentURL(context.Background(), testCreds(), "copy1"); err == nil {
				t.Fatal("unsafe or unrelated URL accepted")
			}
		})
	}
}
