package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/integrations/lark"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

type chatPRDFakeDocument struct {
	content []lark.PRDSection
	owner   string
}

type chatPRDFakeClient struct {
	lark.APIClient
	messages         map[string]lark.LarkMessage
	documents        map[string]*chatPRDFakeDocument
	templateHeadings []string
	failPhase        string
	copyUnknown      bool
	copyKnownError   bool
	onCopy           func()
	onVerify         func()
}

func (f *chatPRDFakeClient) GetMessage(_ context.Context, _ lark.InstallationCredentials, id string) ([]lark.LarkMessage, error) {
	message, ok := f.messages[id]
	if !ok {
		return nil, errors.New("message not found")
	}
	return []lark.LarkMessage{message}, nil
}
func (f *chatPRDFakeClient) ReadPRDTemplateHeadings(context.Context, lark.InstallationCredentials, string) ([]string, error) {
	if f.failPhase == "template" {
		return nil, errors.New("template permission denied")
	}
	return f.templateHeadings, nil
}
func (f *chatPRDFakeClient) ResolvePRDTemplate(context.Context, lark.InstallationCredentials, string) (string, error) {
	if f.failPhase == "resolve" {
		return "", errors.New("template permission denied")
	}
	return "template", nil
}
func (f *chatPRDFakeClient) CopyPRDTemplate(context.Context, lark.InstallationCredentials, string, string) (lark.PRDDocument, error) {
	if f.onCopy != nil {
		f.onCopy()
	}
	id := "document" + strconv.Itoa(len(f.documents)+1)
	f.documents[id] = &chatPRDFakeDocument{}
	if f.copyUnknown {
		return lark.PRDDocument{}, &lark.PRDCopyError{OutcomeUnknown: true, Err: errors.New("response lost")}
	}
	document := lark.PRDDocument{ID: id, URL: "https://example.feishu.cn/docx/" + id}
	if f.copyKnownError {
		document.URL = ""
		return document, &lark.PRDCopyError{OutcomeUnknown: true, Err: errors.New("malformed copy metadata")}
	}
	return document, nil
}
func (f *chatPRDFakeClient) FillPRDDocument(_ context.Context, _ lark.InstallationCredentials, id string, sections []lark.PRDSection) error {
	f.documents[id].content = append([]lark.PRDSection(nil), sections...)
	if f.failPhase == "fill" {
		return errors.New("fill acknowledgement lost")
	}
	return nil
}
func (f *chatPRDFakeClient) FinalizePRDOwner(_ context.Context, _ lark.InstallationCredentials, id, owner string) error {
	if f.failPhase == "owner" {
		return errors.New("owner permission denied")
	}
	f.documents[id].owner = owner
	return nil
}
func (f *chatPRDFakeClient) GetPRDDocumentURL(_ context.Context, _ lark.InstallationCredentials, id string) (string, error) {
	if f.documents[id] == nil {
		return "", errors.New("document missing")
	}
	return "https://example.feishu.cn/docx/" + id, nil
}
func (f *chatPRDFakeClient) VerifyPRDDocument(_ context.Context, _ lark.InstallationCredentials, id string, sections []lark.PRDSection, owner string) error {
	if f.onVerify != nil {
		f.onVerify()
	}
	if f.failPhase == "verify" {
		return errors.New("verification unavailable")
	}
	doc := f.documents[id]
	if doc == nil || doc.owner != owner || !reflect.DeepEqual(doc.content, sections) {
		return errors.New("document differs from confirmed snapshot")
	}
	return nil
}

type chatPRDFixture struct {
	h              Handler
	client         *chatPRDFakeClient
	workspaceID    string
	sessionID      string
	taskID         string
	installationID string
	bindingID      string
	root           lark.LarkMessage
}

func newChatPRDFixture(t *testing.T) *chatPRDFixture {
	t.Helper()
	if testHandler == nil || testPool == nil {
		t.Skip("requires test database")
	}
	t.Setenv("MULTICA_PRD_TEMPLATE_WIKI_TOKEN", "fixture_wiki")
	box, err := secretbox.New(make([]byte, secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	installations, err := lark.NewInstallationService(testHandler.Queries, box)
	if err != nil {
		t.Fatal(err)
	}
	workspaceID := dbfx.Workspace(t, "PRD fixture", "prd-fixture-"+uuid.NewString())
	dbfx := testutil.New(testPool, workspaceID, testUserID)
	dbfx.Member(t, workspaceID, testUserID, "owner")
	runtimeID := dbfx.Runtime(t, "PRD fixture runtime")
	agentID := dbfx.Agent(t, "PRD fixture "+uuid.NewString(), runtimeID)
	sessionID := dbfx.ChatSession(t, agentID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"chat_session_id": sessionID, "runtime_id": runtimeID, "status": "running",
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	inst, err := installations.Upsert(context.Background(), lark.InstallationParams{
		WorkspaceID: parseUUID(workspaceID), AgentID: parseUUID(agentID), AppID: "cli_" + uuid.NewString(),
		AppSecret: "fake-test-secret", BotOpenID: "ou_fixture_bot", InstallerUserID: parseUUID(testUserID),
	})
	if err != nil {
		t.Fatal(err)
	}
	installationID := uuidToString(inst.ID)
	dbfx.Cleanup(t, `DELETE FROM channel_installation WHERE id=$1`, installationID)
	route := []byte(`{"chat_id":"oc_fixture_chat"}`)
	bindingID := dbfx.Insert(t, "channel_chat_session_binding", testutil.Cols{
		"chat_session_id": sessionID, "installation_id": installationID, "channel_type": "feishu",
		"channel_chat_id": "oc_fixture_chat:omt_fixture_topic", "chat_type": "group", "config": route,
		"last_message_id": "om_fixture_root", "last_thread_id": "omt_fixture_topic", "route_revision": 1,
	})
	dbfx.InsertNoID(t, "channel_task_delivery", testutil.Cols{
		"task_id": taskID, "binding_id": bindingID, "installation_id": installationID, "channel_type": "feishu",
		"channel_chat_id": "oc_fixture_chat:omt_fixture_topic", "chat_type": "group", "config": route,
		"channel_message_id": "om_fixture_root", "channel_thread_id": "omt_fixture_topic", "route_revision": 1,
	}, "task_id=$1", taskID)
	dbfx.Cleanup(t, `DELETE FROM chat_prd_draft WHERE installation_id=$1`, installationID)
	root := lark.LarkMessage{MessageID: "om_fixture_root", ChatID: "oc_fixture_chat", ThreadID: "omt_fixture_topic",
		MessageType: "text", Content: `{"text":"@_user_1 请生成订单导出 PRD"}`, SenderType: "user", SenderID: "ou_original_human",
		CreateTime: strconv.FormatInt(time.Now().Add(-time.Minute).UnixMilli(), 10),
		Mentions:   []lark.LarkMessageMention{{Key: "@_user_1", ID: inst.AppID, IDType: "app_id"}},
	}
	dbfx.Insert(t, "channel_user_binding", testutil.Cols{
		"workspace_id": workspaceID, "multica_user_id": testUserID, "installation_id": installationID,
		"channel_type": "feishu", "channel_user_id": root.SenderID,
	})
	client := &chatPRDFakeClient{
		messages: map[string]lark.LarkMessage{root.MessageID: root}, documents: make(map[string]*chatPRDFakeDocument),
		templateHeadings: []string{"需求背景", "功能需求"},
	}
	h := *testHandler
	h.LarkInstallations, h.LarkAPIClient = installations, client
	return &chatPRDFixture{h: h, client: client, workspaceID: workspaceID, sessionID: sessionID, taskID: taskID, installationID: installationID, bindingID: bindingID, root: root}
}

func (f *chatPRDFixture) request(method, path string, body any) *http.Request {
	request := newRequest(method, path, body)
	request.Header.Set("X-Workspace-ID", f.workspaceID)
	request.Header.Set("X-Actor-Source", "task_token")
	request.Header.Set("X-Task-ID", f.taskID)
	return request
}

func prdFixtureContent() ChatPRDContent {
	return ChatPRDContent{Title: "PRD-订单导出", Sections: []lark.PRDSection{{Heading: "需求背景", Body: "已确认：导出订单。\n【待确认】字段范围。"}}}
}

func (f *chatPRDFixture) draft(t *testing.T, content ChatPRDContent) ChatPRDDraft {
	t.Helper()
	var draft ChatPRDDraft
	testutil.Call(t, f.h.SaveChatPRDDraft, f.request(http.MethodPost, "/api/chat/prd/draft", map[string]any{
		"source_message_id": f.root.MessageID, "content": content,
	})).Want(http.StatusOK).JSON(&draft)
	return draft
}

func (f *chatPRDFixture) confirm(draft ChatPRDDraft) lark.LarkMessage {
	body, _ := json.Marshal(map[string]string{"text": "@_user_1 " + draft.Confirmation})
	message := lark.LarkMessage{MessageID: "om_confirmation", ChatID: f.root.ChatID, ThreadID: f.root.ThreadID,
		RootID: f.root.MessageID, ParentID: "om_mika_draft_reply", MessageType: "text", Content: string(body),
		SenderType: "user", SenderID: f.root.SenderID, CreateTime: strconv.FormatInt(draft.VersionCreatedAt.Add(time.Second).UnixMilli(), 10),
		Mentions: []lark.LarkMessageMention{{Key: "@_user_1", ID: f.root.Mentions[0].ID, IDType: f.root.Mentions[0].IDType}},
	}
	f.client.messages[message.MessageID] = message
	return message
}

func (f *chatPRDFixture) publishRequest(draft ChatPRDDraft, confirmationID string) *http.Request {
	return f.request(http.MethodPost, "/api/chat/prd/publish", map[string]any{
		"draft_id": draft.ID, "version": draft.Version, "confirmation_message_id": confirmationID,
	})
}

func TestChatPRDDraftVersionAndHumanRoot(t *testing.T) {
	f := newChatPRDFixture(t)
	content := prdFixtureContent()
	first := f.draft(t, content)
	again := f.draft(t, content)
	if first.ID != again.ID || first.Version != 1 || again.Version != first.Version || !again.VersionCreatedAt.Equal(first.VersionCreatedAt) {
		t.Fatalf("identical drafts changed identity/version: first=%+v again=%+v", first, again)
	}
	confirmation := f.confirm(first)
	content.Sections[0].Body = "已确认：导出包括当前筛选。"
	second := f.draft(t, content)
	if second.ID != first.ID || second.Version != 2 || second.ConfirmationMessageID != "" || second.Content.Sections[0].Body != content.Sections[0].Body {
		t.Fatalf("revised draft did not advance cleanly: %+v", second)
	}
	testutil.Call(t, f.h.PublishChatPRD, f.publishRequest(first, confirmation.MessageID)).Want(http.StatusConflict)
	testutil.Call(t, f.h.PublishChatPRD, f.publishRequest(second, confirmation.MessageID)).Want(http.StatusForbidden)
	if len(f.client.documents) != 0 {
		t.Fatal("draft or stale confirmation created a document")
	}
}

func TestChatPRDRejectsUntrustedSource(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*lark.LarkMessage)
	}{
		{"bot root", func(m *lark.LarkMessage) { m.SenderType = "app" }},
		{"another topic", func(m *lark.LarkMessage) { m.ThreadID = "omt_other" }},
		{"reply instead of root", func(m *lark.LarkMessage) { m.ParentID = "om_other"; m.RootID = "om_other" }},
		{"not a PRD request", func(m *lark.LarkMessage) { m.Content = `{"text":"@_user_1 晚上好"}` }},
		{"no genuine mention", func(m *lark.LarkMessage) { m.Mentions = nil }},
		{"metadata without text mention", func(m *lark.LarkMessage) { m.Content = `{"text":"请生成订单导出 PRD"}` }},
		{"different app", func(m *lark.LarkMessage) { m.Mentions[0].ID = "cli_other_app" }},
		{"forged key", func(m *lark.LarkMessage) { m.Mentions[0].Key = "@_user_" }},
		{"mention prefix collision", func(m *lark.LarkMessage) { m.Content = `{"text":"@_user_10 请生成订单导出 PRD"}` }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newChatPRDFixture(t)
			message := f.root
			tc.change(&message)
			f.client.messages[message.MessageID] = message
			response := testutil.Call(t, f.h.SaveChatPRDDraft, f.request(http.MethodPost, "/api/chat/prd/draft", map[string]any{
				"source_message_id": message.MessageID, "content": prdFixtureContent(),
			})).Want(http.StatusForbidden)
			var rejection chatPRDRejection
			response.JSON(&rejection)
			if rejection.Code == "" || rejection.Guidance == "" {
				t.Fatalf("rejection missing code/guidance: %+v", rejection)
			}
			// The read path surfaces an advisory refusal when the topic's
			// root stays readable-but-invalid (before any drafting work).
			// When the mutation makes the root itself unreadable, the
			// advisory check cannot classify and the durable read falls
			// back to not-found.
			rootUnreadable := message.ThreadID != f.root.ThreadID || message.RootID != ""
			wantRead := http.StatusForbidden
			if rootUnreadable {
				wantRead = http.StatusNotFound
			}
			testutil.Call(t, f.h.GetChatPRD, f.request(http.MethodGet, "/api/chat/prd", nil)).Want(wantRead)
			if len(f.client.documents) != 0 {
				t.Fatal("untrusted source created a document")
			}
		})
	}
}

func TestChatPRDConfirmationGate(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*lark.LarkMessage)
	}{
		{"bot", func(m *lark.LarkMessage) { m.SenderType = "app" }},
		{"other human", func(m *lark.LarkMessage) { m.SenderID = "ou_other_human" }},
		{"other chat", func(m *lark.LarkMessage) { m.ChatID = "oc_other" }},
		{"other topic", func(m *lark.LarkMessage) { m.ThreadID = "omt_other" }},
		{"forwarded approval", func(m *lark.LarkMessage) { m.UpperMessageID = "om_someone_elses_forward" }},
		{"rich text", func(m *lark.LarkMessage) { m.MessageType = "post" }},
		{"revoked", func(m *lark.LarkMessage) { m.Deleted = true }},
		{"old timestamp", func(m *lark.LarkMessage) { m.CreateTime = "1" }},
		{"not a real mention", func(m *lark.LarkMessage) { m.Mentions = nil }},
		{"metadata without text mention", func(m *lark.LarkMessage) {
			m.Content = strings.ReplaceAll(m.Content, "@_user_1 ", "")
		}},
		{"quoted exact confirmation", func(m *lark.LarkMessage) {
			m.Content = strings.ReplaceAll(m.Content, "@_user_1 ", "> @_user_1 ")
		}},
		{"mention prefix collision", func(m *lark.LarkMessage) {
			m.Content = strings.ReplaceAll(m.Content, "@_user_1", "@_user_10")
		}},
		{"forged key erases quotation", func(m *lark.LarkMessage) {
			m.Mentions[0].Key = "@_user_1 >"
			m.Content = strings.ReplaceAll(m.Content, "@_user_1 ", "@_user_1 > ")
		}},
		{"bare exact phrase", func(m *lark.LarkMessage) {
			var body map[string]string
			_ = json.Unmarshal([]byte(m.Content), &body)
			body["text"] = strings.TrimPrefix(body["text"], "@_user_1 ")
			raw, _ := json.Marshal(body)
			m.Content, m.Mentions = string(raw), nil
		}},
		{"different app mention", func(m *lark.LarkMessage) { m.Mentions[0].ID = "cli_other_app" }},
		{"ambiguous mention key", func(m *lark.LarkMessage) {
			m.Mentions = append(m.Mentions, lark.LarkMessageMention{Key: "@_user_1", ID: "cli_other_app", IDType: "app_id"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newChatPRDFixture(t)
			draft := f.draft(t, prdFixtureContent())
			message := f.confirm(draft)
			tc.change(&message)
			f.client.messages[message.MessageID] = message
			testutil.Call(t, f.h.PublishChatPRD, f.publishRequest(draft, message.MessageID)).Want(http.StatusForbidden)
			if len(f.client.documents) != 0 {
				t.Fatal("invalid confirmation caused an external write")
			}
		})
	}
}

// Keep the provider-to-authorization contract executable without a database:
// Feishu returns bot mentions as app_id even when users were requested as open_id.
func TestChatPRDNativeMentionAuthorization(t *testing.T) {
	const appID = "cli_a9493eea23b8dcb0"
	const botOpenID = "ou_installed_bot"
	const botUnionID = "on_installed_bot"
	draft := ChatPRDDraft{
		ID: "native-draft", Version: 1, SourceMessageID: "om_root", InitiatorOpenID: "ou_original_human",
		VersionCreatedAt: time.UnixMilli(1000),
	}
	for _, tc := range []struct {
		name        string
		id          string
		idType      string
		key         string
		textMention string
		want        bool
	}{
		{"installed app", appID, "app_id", "@_user_1", "@_user_1", true},
		{"installed open ID", botOpenID, "open_id", "@_user_1", "@_user_1", true},
		{"known union ID", botUnionID, "union_id", "@_user_1", "@_user_1", true},
		{"omitted type uses requested open ID", botOpenID, "", "@_user_1", "@_user_1", true},
		{"another app with same name", "cli_another_app", "app_id", "@_user_1", "@_user_1", false},
		{"unrelated user", "ou_another_user", "open_id", "@_user_1", "@_user_1", false},
		{"another union ID", "on_another_bot", "union_id", "@_user_1", "@_user_1", false},
		{"app ID under open ID type", appID, "open_id", "@_user_1", "@_user_1", false},
		{"open ID under app ID type", botOpenID, "app_id", "@_user_1", "@_user_1", false},
		{"omitted type cannot guess app ID", appID, "", "@_user_1", "@_user_1", false},
		{"omitted type cannot guess union ID", botUnionID, "", "@_user_1", "@_user_1", false},
		{"unknown type", botOpenID, "future_id", "@_user_1", "@_user_1", false},
		{"unverified user ID namespace", botOpenID, "user_id", "@_user_1", "@_user_1", false},
		{"missing identity", "", "app_id", "@_user_1", "@_user_1", false},
		{"metadata without text mention", appID, "app_id", "@_user_1", "", false},
		{"missing mention key", appID, "app_id", "", "@_user_1", false},
		{"prefix is not the metadata key", appID, "app_id", "@_user_1", "@_user_10", false},
		{"forged key prefix", appID, "app_id", "@_user_", "@_user_1", false},
		{"typed display name", appID, "app_id", "@_user_1", "@Mika", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/open-apis/auth/v3/tenant_access_token/internal", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, map[string]any{"code": 0, "tenant_access_token": "native-test-token", "expire": 7200})
			})
			mux.HandleFunc("/open-apis/im/v1/messages/", func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("user_id_type") != "open_id" {
					t.Error("native request must retain the open_id user namespace")
				}
				id := strings.TrimPrefix(r.URL.Path, "/open-apis/im/v1/messages/")
				text := tc.textMention + " 请生成订单导出 PRD"
				item := map[string]any{
					"message_id": id, "chat_id": "oc_chat", "thread_id": "omt_topic", "msg_type": "text", "create_time": "500",
					"sender": map[string]any{"id": draft.InitiatorOpenID, "id_type": "open_id", "sender_type": "user"},
				}
				if id == "om_confirmation" {
					text = tc.textMention + " " + chatPRDConfirmation(draft.ID, draft.Version)
					item["root_id"], item["parent_id"], item["create_time"] = draft.SourceMessageID, "om_bot_draft", "2000"
				}
				content, _ := json.Marshal(map[string]string{"text": text})
				item["body"] = map[string]any{"content": string(content)}
				mention := map[string]any{"id": tc.id, "key": tc.key, "name": "Mika"}
				if tc.idType != "" {
					mention["id_type"] = tc.idType
				}
				item["mentions"] = []any{mention}
				writeJSON(w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": []any{item}}})
			})
			server := httptest.NewServer(mux)
			defer server.Close()
			h := Handler{LarkAPIClient: lark.NewHTTPAPIClient(lark.HTTPClientConfig{BaseURL: server.URL, HTTPClient: server.Client()})}
			scope := chatPRDScope{
				chatID: "oc_chat", threadID: "omt_topic", botOpenID: botOpenID, botUnionID: botUnionID,
				credentials: lark.InstallationCredentials{AppID: appID, AppSecret: "native-test-secret"},
			}
			source, err := h.chatPRDMessage(context.Background(), scope, draft.SourceMessageID)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateChatPRDSource(source, scope); (err == nil) != tc.want {
				t.Fatalf("source authorization = %v, want accepted=%v", err, tc.want)
			}
			confirmation, err := h.chatPRDMessage(context.Background(), scope, "om_confirmation")
			if err != nil {
				t.Fatal(err)
			}
			if err := validateChatPRDConfirmation(confirmation, draft, scope); (err == nil) != tc.want {
				t.Fatalf("confirmation authorization = %v, want accepted=%v", err, tc.want)
			}
		})
	}
}

func TestChatPRDPublishFrozenSnapshotAndDuplicateConfirmation(t *testing.T) {
	f := newChatPRDFixture(t)
	content := prdFixtureContent()
	draft := f.draft(t, content)
	confirmation := f.confirm(draft)
	var published, repeated ChatPRDDraft
	testutil.Call(t, f.h.PublishChatPRD, f.publishRequest(draft, confirmation.MessageID)).Want(http.StatusOK).JSON(&published)
	testutil.Call(t, f.h.PublishChatPRD, f.publishRequest(draft, confirmation.MessageID)).Want(http.StatusOK).JSON(&repeated)
	if published.Status != "published" || published.DocumentURL != repeated.DocumentURL || len(f.client.documents) != 1 {
		t.Fatalf("publication was not idempotent: first=%+v repeated=%+v docs=%d", published, repeated, len(f.client.documents))
	}
	document := f.client.documents[published.DocumentID]
	if document.owner != f.root.SenderID || !reflect.DeepEqual(document.content, content.Sections) {
		t.Fatalf("document did not reflect confirmed content/real requester: %+v", document)
	}
	content.Sections[0].Body = "attempted post-confirmation edit"
	testutil.Call(t, f.h.SaveChatPRDDraft, f.request(http.MethodPost, "/api/chat/prd/draft", map[string]any{
		"source_message_id": f.root.MessageID, "content": content,
	})).Want(http.StatusConflict)
	var current ChatPRDDraft
	testutil.Call(t, f.h.GetChatPRD, f.request(http.MethodGet, "/api/chat/prd", nil)).Want(http.StatusOK).JSON(&current)
	if current.Content.Sections[0].Body == content.Sections[0].Body {
		t.Fatal("confirmed snapshot was overwritten")
	}
}

func TestChatPRDPartialFailureResumesSameDocument(t *testing.T) {
	for _, phase := range []string{"fill", "owner", "verify", "copy-known"} {
		t.Run(phase, func(t *testing.T) {
			f := newChatPRDFixture(t)
			draft := f.draft(t, prdFixtureContent())
			confirmation := f.confirm(draft)
			f.client.failPhase = phase
			f.client.copyKnownError = phase == "copy-known"
			var failed, published ChatPRDDraft
			testutil.Call(t, f.h.PublishChatPRD, f.publishRequest(draft, confirmation.MessageID)).Want(http.StatusBadGateway).JSON(&failed)
			if failed.Status != "failed" || failed.DocumentID == "" {
				t.Fatalf("partial failure lost document identity: %+v", failed)
			}
			f.client.failPhase, f.client.copyKnownError = "", false
			testutil.Call(t, f.h.PublishChatPRD, f.publishRequest(draft, confirmation.MessageID)).Want(http.StatusOK).JSON(&published)
			if published.Status != "published" || published.DocumentURL == "" || published.DocumentID != failed.DocumentID || len(f.client.documents) != 1 {
				t.Fatalf("retry did not finalize the original document: failed=%+v published=%+v docs=%d", failed, published, len(f.client.documents))
			}
		})
	}
}

func TestChatPRDUnknownCopyNeverRecreates(t *testing.T) {
	f := newChatPRDFixture(t)
	draft := f.draft(t, prdFixtureContent())
	confirmation := f.confirm(draft)
	f.client.copyUnknown = true
	var failed ChatPRDDraft
	testutil.Call(t, f.h.PublishChatPRD, f.publishRequest(draft, confirmation.MessageID)).Want(http.StatusBadGateway).JSON(&failed)
	if failed.Status != "unknown" || failed.DocumentID != "" {
		t.Fatalf("unknown copy not recorded: %+v", failed)
	}
	f.client.copyUnknown = false
	testutil.Call(t, f.h.PublishChatPRD, f.publishRequest(draft, confirmation.MessageID)).Want(http.StatusConflict)
	if len(f.client.documents) != 1 {
		t.Fatal("unknown remote success created a duplicate")
	}
}

func TestChatPRDConcurrentPublishDoesNotPretendSuccess(t *testing.T) {
	f := newChatPRDFixture(t)
	draft := f.draft(t, prdFixtureContent())
	confirmation := f.confirm(draft)
	entered, release := make(chan struct{}), make(chan struct{})
	f.client.onCopy = func() { close(entered); <-release }
	finished := make(chan *testutil.Response, 1)
	go func() {
		finished <- testutil.Call(t, f.h.PublishChatPRD, f.publishRequest(draft, confirmation.MessageID))
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("first publisher did not reach copy")
	}
	second := testutil.Call(t, f.h.PublishChatPRD, f.publishRequest(draft, confirmation.MessageID))
	close(release)
	second.Want(http.StatusConflict)
	select {
	case first := <-finished:
		first.Want(http.StatusOK)
	case <-time.After(5 * time.Second):
		t.Fatal("first publisher did not finish")
	}
	if len(f.client.documents) != 1 {
		t.Fatal("concurrent publish created multiple documents")
	}
}

func TestChatPRDTemplateAndScopeCannotBeBypassed(t *testing.T) {
	f := newChatPRDFixture(t)
	draft := f.draft(t, prdFixtureContent())
	confirmation := f.confirm(draft)
	t.Setenv("MULTICA_PRD_TEMPLATE_WIKI_TOKEN", "")
	testutil.Call(t, f.h.PublishChatPRD, f.publishRequest(draft, confirmation.MessageID)).Want(http.StatusServiceUnavailable)
	request := f.publishRequest(draft, confirmation.MessageID)
	request.Header.Set("X-Workspace-ID", uuid.NewString())
	testutil.Call(t, f.h.PublishChatPRD, request).Want(http.StatusForbidden)
	request = f.publishRequest(draft, confirmation.MessageID)
	request.Header.Set("X-Actor-Source", "api_key")
	testutil.Call(t, f.h.PublishChatPRD, request).Want(http.StatusForbidden)
	dbfx.Exec(t, `UPDATE channel_chat_session_binding SET retired_at=now() WHERE id=$1`, f.bindingID)
	testutil.Call(t, f.h.PublishChatPRD, f.publishRequest(draft, confirmation.MessageID)).Want(http.StatusForbidden)
	if len(f.client.documents) != 0 {
		t.Fatal("missing config or invalid scope performed a document write")
	}
}

func TestChatPRDPublishSerializesWorkspaceDeletion(t *testing.T) {
	f := newChatPRDFixture(t)
	draft := f.draft(t, prdFixtureContent())
	confirmation := f.confirm(draft)
	ctx := context.Background()
	publisher, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer publisher.Rollback(ctx)
	// Keep the real publisher on a known backend so the lock assertion cannot
	// accidentally observe an unrelated waiter. Begin uses a nested savepoint.
	f.h.TxStarter = publisher
	publisherPID := int(publisher.Conn().PgConn().PID())
	deleted := make(chan *testutil.Response, 1)
	deleteDone := make(chan struct{})
	f.client.onCopy = func() {
		go func() {
			defer close(deleteDone)
			request := withURLParam(newRequest(http.MethodDelete, "/api/workspaces/"+f.workspaceID, nil), "id", f.workspaceID)
			deleted <- testutil.Call(t, testHandler.DeleteWorkspace, request)
		}()
		t.Cleanup(func() {
			waitContextLockSignal(t, deleteDone, "workspace deletion did not finish after publication released its locks")
		})
		if !waitForWaiterBlockedBy(t, publisherPID, 5*time.Second) {
			t.Fatal("workspace deletion did not wait for the publisher while the remote copy was in flight")
		}
		var status, phase string
		dbfx.QueryRow(t, `SELECT status, phase FROM chat_prd_draft WHERE id=$1`, draft.ID).Scan(&status, &phase)
		if status != "publishing" || phase != "copy" {
			t.Fatalf("copy intent was not independently committed: status=%s phase=%s", status, phase)
		}
	}
	f.client.onVerify = func() {
		// A separate connection must see the returned copy ID before the remote
		// verification completes; rolling back the interlock cannot erase it.
		var documentID, status, phase string
		dbfx.QueryRow(t, `SELECT document_id, status, phase FROM chat_prd_draft WHERE id=$1`, draft.ID).Scan(&documentID, &status, &phase)
		if documentID != "document1" || status != "publishing" || phase != "verify" {
			t.Fatalf("remote copy identity was not durably recorded: document=%s status=%s phase=%s", documentID, status, phase)
		}
		select {
		case <-deleteDone:
			t.Fatal("workspace deletion finished before document verification")
		default:
		}
	}
	var published ChatPRDDraft
	testutil.Call(t, f.h.PublishChatPRD, f.publishRequest(draft, confirmation.MessageID)).Want(http.StatusOK).JSON(&published)
	if published.Status != "published" || published.DocumentID != "document1" || len(f.client.documents) != 1 {
		t.Fatalf("publication did not finish before teardown: %+v", published)
	}
	document := f.client.documents[published.DocumentID]
	if document.owner != f.root.SenderID || !reflect.DeepEqual(document.content, draft.Content.Sections) {
		t.Fatalf("workspace teardown left an unfinished remote document: %+v", document)
	}
	waitContextLockSignal(t, deleteDone, "workspace deletion deadlocked with publication")
	(<-deleted).Want(http.StatusNoContent)
	if count := dbfx.Count(t, `SELECT count(*) FROM chat_prd_draft WHERE workspace_id=$1`, f.workspaceID); count != 0 {
		t.Fatalf("completed workspace teardown retained %d PRD drafts", count)
	}
}

func TestChatPRDPublishRejectsWorkspaceDeletionWinningFirst(t *testing.T) {
	f := newChatPRDFixture(t)
	draft := f.draft(t, prdFixtureContent())
	confirmation := f.confirm(draft)
	ctx := context.Background()
	deleter, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer deleter.Rollback(ctx)
	deleting := f.h
	deleting.TxStarter = deleter
	request := withURLParam(newRequest(http.MethodDelete, "/api/workspaces/"+f.workspaceID, nil), "id", f.workspaceID)
	// Execute the real teardown, but keep its enclosing transaction uncommitted
	// so the publisher's initial scope read still sees the old workspace.
	testutil.Call(t, deleting.DeleteWorkspace, request).Want(http.StatusNoContent)
	finished := make(chan *testutil.Response, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		finished <- testutil.Call(t, f.h.PublishChatPRD, f.publishRequest(draft, confirmation.MessageID))
	}()
	t.Cleanup(func() { waitContextLockSignal(t, done, "publisher did not finish after workspace teardown") })
	if !waitForWaiterBlockedBy(t, int(deleter.Conn().PgConn().PID()), 5*time.Second) {
		t.Fatal("publisher did not wait for the in-flight workspace teardown")
	}
	if err := deleter.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	waitContextLockSignal(t, done, "publisher did not reject the deleted workspace")
	(<-finished).Want(http.StatusConflict)
	if len(f.client.documents) != 0 {
		t.Fatal("publisher copied a remote document after workspace teardown won")
	}
}

func TestChatPRDPublishRevalidatesScopeAfterSessionLock(t *testing.T) {
	f := newChatPRDFixture(t)
	draft := f.draft(t, prdFixtureContent())
	confirmation := f.confirm(draft)
	ctx := context.Background()
	retiring, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer retiring.Rollback(ctx)
	if _, err := f.h.Queries.WithTx(retiring).LockChatSessionForDraftWrite(ctx, parseUUID(f.sessionID)); err != nil {
		t.Fatal(err)
	}
	if _, err := retiring.Exec(ctx, `UPDATE channel_chat_session_binding SET retired_at=now() WHERE id=$1`, f.bindingID); err != nil {
		t.Fatal(err)
	}
	finished := make(chan *testutil.Response, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		finished <- testutil.Call(t, f.h.PublishChatPRD, f.publishRequest(draft, confirmation.MessageID))
	}()
	t.Cleanup(func() { waitContextLockSignal(t, done, "publisher did not finish after binding retirement") })
	if !waitForWaiterBlockedBy(t, int(retiring.Conn().PgConn().PID()), 5*time.Second) {
		t.Fatal("publisher did not wait for the session mutation")
	}
	if err := retiring.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	waitContextLockSignal(t, done, "publisher did not revalidate its retired topic")
	(<-finished).Want(http.StatusForbidden)
	if len(f.client.documents) != 0 {
		t.Fatal("stale pre-lock scope authorized a remote document copy")
	}
}

func TestChatPRDDraftRevalidatesScopeAfterSessionLock(t *testing.T) {
	f := newChatPRDFixture(t)
	ctx := context.Background()
	retiring, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer retiring.Rollback(ctx)
	if _, err := f.h.Queries.WithTx(retiring).LockChatSessionForDraftWrite(ctx, parseUUID(f.sessionID)); err != nil {
		t.Fatal(err)
	}
	if _, err := retiring.Exec(ctx, `UPDATE channel_chat_session_binding SET retired_at=now() WHERE id=$1`, f.bindingID); err != nil {
		t.Fatal(err)
	}
	finished := make(chan *testutil.Response, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		finished <- testutil.Call(t, f.h.SaveChatPRDDraft, f.request(http.MethodPost, "/api/chat/prd/draft", map[string]any{
			"source_message_id": f.root.MessageID, "content": prdFixtureContent(),
		}))
	}()
	t.Cleanup(func() { waitContextLockSignal(t, done, "draft save did not finish after binding retirement") })
	if !waitForWaiterBlockedBy(t, int(retiring.Conn().PgConn().PID()), 5*time.Second) {
		t.Fatal("draft save did not wait for the session mutation")
	}
	if err := retiring.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	waitContextLockSignal(t, done, "draft save did not revalidate its retired topic")
	(<-finished).Want(http.StatusForbidden)
	if count := dbfx.Count(t, `SELECT count(*) FROM chat_prd_draft WHERE installation_id=$1`, f.installationID); count != 0 {
		t.Fatalf("stale pre-lock scope saved %d drafts", count)
	}
}

func TestChatPRDDraftAuthenticatesOriginalHumanTask(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*testing.T, *chatPRDFixture)
		status int
	}{
		{"unattributed task", func(t *testing.T, f *chatPRDFixture) {
			dbfx.Exec(t, `UPDATE agent_task_queue SET originator_user_id=NULL WHERE id=$1`, f.taskID)
		}, http.StatusForbidden},
		{"borrowed human binding", func(t *testing.T, f *chatPRDFixture) {
			other := dbfx.User(t, "Other requester", "prd-other-"+uuid.NewString()+"@example.test")
			dbfx.Exec(t, `UPDATE channel_user_binding SET multica_user_id=$2 WHERE installation_id=$1`, f.installationID, other)
		}, http.StatusForbidden},
		{"missing membership", func(t *testing.T, f *chatPRDFixture) {
			dbfx.Exec(t, `DELETE FROM member WHERE workspace_id=$1 AND user_id=$2`, f.workspaceID, testUserID)
		}, http.StatusForbidden},
		{"private agent admin bypass", func(t *testing.T, f *chatPRDFixture) {
			other := dbfx.User(t, "Private agent owner", "prd-owner-"+uuid.NewString()+"@example.test")
			dbfx.Exec(t, `UPDATE agent SET owner_id=$2 WHERE id=(SELECT agent_id FROM agent_task_queue WHERE id=$1)`, f.taskID, other)
		}, http.StatusForbidden},
		{"completed task replay", func(t *testing.T, f *chatPRDFixture) {
			dbfx.Exec(t, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, f.taskID)
		}, http.StatusForbidden},
		{"source outside trigger lineage", func(t *testing.T, f *chatPRDFixture) {
			trigger := f.root
			trigger.MessageID, trigger.RootID, trigger.ParentID = "om_unrelated_trigger", "om_other_root", "om_other_root"
			f.client.messages[trigger.MessageID] = trigger
			dbfx.Exec(t, `UPDATE channel_task_delivery SET channel_message_id=$2 WHERE task_id=$1`, f.taskID, trigger.MessageID)
		}, http.StatusForbidden},
		{"unknown context", func(t *testing.T, f *chatPRDFixture) {
			dbfx.Exec(t, `UPDATE agent_task_queue SET channel_context_revision=99 WHERE id=$1`, f.taskID)
		}, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newChatPRDFixture(t)
			tc.change(t, f)
			testutil.Call(t, f.h.SaveChatPRDDraft, f.request(http.MethodPost, "/api/chat/prd/draft", map[string]any{
				"source_message_id": f.root.MessageID, "content": prdFixtureContent(),
			})).Want(tc.status)
			if count := dbfx.Count(t, `SELECT count(*) FROM chat_prd_draft WHERE installation_id=$1`, f.installationID); count != 0 {
				t.Fatalf("unauthorized task saved %d drafts", count)
			}
		})
	}
}

func TestChatPRDTemplateGuidesDirectDraft(t *testing.T) {
	f := newChatPRDFixture(t)
	f.client.templateHeadings = []string{"上线范围"}
	var template struct {
		Headings []string `json:"template_headings"`
	}
	testutil.Call(t, f.h.GetChatPRDTemplate, f.request(http.MethodGet, "/api/chat/prd/template", nil)).Want(http.StatusOK).JSON(&template)
	if !reflect.DeepEqual(template.Headings, f.client.templateHeadings) {
		t.Fatalf("template headings do not match configured source: %+v", template)
	}
	testutil.Call(t, f.h.SaveChatPRDDraft, f.request(http.MethodPost, "/api/chat/prd/draft", map[string]any{
		"source_message_id": f.root.MessageID, "content": prdFixtureContent(),
	})).Want(http.StatusBadRequest)
	content := prdFixtureContent()
	content.Sections[0].Heading = template.Headings[0]
	draft := f.draft(t, content)
	if !reflect.DeepEqual(draft.Content, content) || draft.InitiatorOpenID != f.root.SenderID || draft.Confirmation == "" || len(f.client.documents) != 0 {
		t.Fatalf("direct drafting changed content, human authority or created a document: %+v", draft)
	}
	f.client.failPhase = "template"
	testutil.Call(t, f.h.SaveChatPRDDraft, f.request(http.MethodPost, "/api/chat/prd/draft", map[string]any{
		"source_message_id": f.root.MessageID, "content": content,
	})).Want(http.StatusBadGateway)
	request := f.request(http.MethodGet, "/api/chat/prd/template", nil)
	request.Header.Set("X-Actor-Source", "api_key")
	testutil.Call(t, f.h.GetChatPRDTemplate, request).Want(http.StatusForbidden)
}

func TestChatPRDDraftRejectsInvalidCallerContent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content any
	}{
		{"missing content", nil},
		{"empty title", ChatPRDContent{Sections: prdFixtureContent().Sections}},
		{"duplicate headings", ChatPRDContent{Title: "PRD", Sections: append(prdFixtureContent().Sections, prdFixtureContent().Sections...)}},
		{"too many paragraphs", ChatPRDContent{Title: "PRD", Sections: []lark.PRDSection{{Heading: "需求背景", Body: strings.Repeat("line\n", 50)}}}},
		{"unknown content field", map[string]any{"title": "PRD", "sections": prdFixtureContent().Sections, "owner": "ou_override"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newChatPRDFixture(t)
			testutil.Call(t, f.h.SaveChatPRDDraft, f.request(http.MethodPost, "/api/chat/prd/draft", map[string]any{
				"source_message_id": f.root.MessageID, "content": tc.content,
			})).Want(http.StatusBadRequest)
			testutil.Call(t, f.h.GetChatPRD, f.request(http.MethodGet, "/api/chat/prd", nil)).Want(http.StatusNotFound)
		})
	}
}
