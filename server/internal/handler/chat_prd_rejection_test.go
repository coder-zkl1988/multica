package handler

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/lark"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// A reply-triggered topic whose root is a plain requirement must get the
// structured, actionable refusal from the read paths BEFORE any drafting
// work, naming the root author as the human who must open the new dedicated
// topic. This is the 锦鲤→海棠 production scenario.
func TestChatPRDReplyTriggeredTopicGetsActionableRejection(t *testing.T) {
	f := newChatPRDFixture(t)
	// Root: a plain requirement by another human — no PRD verb, no mention.
	root := lark.LarkMessage{MessageID: "om_req_root", ChatID: f.root.ChatID, ThreadID: f.root.ThreadID,
		MessageType: "text", Content: `{"text":"在院务系统顶部加一个按钮"}`, SenderType: "user",
		SenderID: "ou_root_author", CreateTime: strconv.FormatInt(time.Now().Add(-2*time.Minute).UnixMilli(), 10)}
	// Reply: the PRD request genuinely mentioning the app, by a later participant.
	reply := lark.LarkMessage{MessageID: "om_prd_reply", ChatID: f.root.ChatID, ThreadID: f.root.ThreadID,
		RootID: root.MessageID, ParentID: root.MessageID, MessageType: "text",
		Content: `{"text":"@_user_1 上面会话总结下 写个PRD"}`, SenderType: "user", SenderID: "ou_later_participant",
		CreateTime: strconv.FormatInt(time.Now().Add(-time.Minute).UnixMilli(), 10),
		Mentions:   []lark.LarkMessageMention{{Key: "@_user_1", ID: f.root.Mentions[0].ID, IDType: f.root.Mentions[0].IDType, Name: "米卡"}}}
	f.client.messages[root.MessageID], f.client.messages[reply.MessageID] = root, reply
	// The task was triggered by the reply, not the root. The binding keeps
	// the same topic so chatPRDScope still resolves; only the trigger moves.
	dbfx := testutil.New(testPool, f.workspaceID, testUserID)
	dbfx.Exec(t, `UPDATE channel_task_delivery SET channel_message_id=$1 WHERE task_id=$2`, reply.MessageID, f.taskID)

	var getRejection chatPRDRejection
	testutil.Call(t, f.h.GetChatPRD, f.request(http.MethodGet, "/api/chat/prd", nil)).Want(http.StatusForbidden).JSON(&getRejection)
	var templateRejection chatPRDRejection
	testutil.Call(t, f.h.GetChatPRDTemplate, f.request(http.MethodGet, "/api/chat/prd/template", nil)).Want(http.StatusForbidden).JSON(&templateRejection)

	for name, rejection := range map[string]chatPRDRejection{"get": getRejection, "template": templateRejection} {
		// The root is readable, human and root-positioned; it fails the same
		// first check validateChatPRDSource applies — no PRD intent.
		if rejection.Code != prdRejectNotPRDRequest {
			t.Fatalf("%s: code=%s want %s", name, rejection.Code, prdRejectNotPRDRequest)
		}
		if rejection.RequesterOpenID != "ou_later_participant" {
			t.Fatalf("%s: requester=%s want ou_later_participant", name, rejection.RequesterOpenID)
		}
		if rejection.RootAuthorOpenID != "ou_root_author" {
			t.Fatalf("%s: root author=%s want ou_root_author", name, rejection.RootAuthorOpenID)
		}
		if !strings.Contains(rejection.Guidance, "另开专用新话题") {
			t.Fatalf("%s: guidance not actionable: %s", name, rejection.Guidance)
		}
	}
	if len(f.client.documents) != 0 {
		t.Fatal("advisory rejection created a document")
	}
}
