package handler

import (
	"net/http"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/lark"
)

// chatPRDRejection is the structured, actionable body the PRD endpoints
// return when the topic's source authorization fails. `error` keeps the
// legacy single-field contract for old clients; `code` lets the CLI opt
// into stable refusal handling; the open_id fields name the real humans so
// the agent can resolve their display names from `multica chat thread` and
// construct a genuine mention when relaying the refusal.
type chatPRDRejection struct {
	Error string `json:"error"`
	Code  string `json:"code"`

	// RootAuthorOpenID is the topic-root message's sender — the human whose
	// new dedicated topic can authorize a PRD. Empty when the root could not
	// be read. Display names are deliberately NOT included: Feishu's REST
	// item only carries names for the people who were @-mentioned, so any
	// name derivable here could be the bot's own; the agent resolves the
	// real names from chat thread instead.
	RootAuthorOpenID string `json:"root_author_open_id,omitempty"`
	// RequesterOpenID is the sender of the message currently asking for the
	// PRD when that differs from the root author.
	RequesterOpenID string `json:"requester_open_id,omitempty"`
	// Guidance is a ready-to-relay sentence (Chinese, matching the feature's
	// user language) describing what the root author must do. It references
	// the root author by role, not by an unverified name.
	Guidance string `json:"guidance"`
}

// prdRejectionCode values are stable machine codes; never reorder or reuse.
const (
	prdRejectNotTopicRoot  = "prd_source_not_topic_root"
	prdRejectNoMention     = "prd_source_missing_mention"
	prdRejectNotPRDRequest = "prd_source_not_prd_request"
)

func writeChatPRDRejection(w http.ResponseWriter, status int, rejection chatPRDRejection) {
	if rejection.Error == "" {
		rejection.Error = "PRD source authorization failed"
	}
	writeJSON(w, status, rejection)
}

// chatPRDRejectionGuidance builds the actionable sentence. The root author
// is referenced by role; the agent overlays the display name it resolved
// from chat thread when relaying.
func chatPRDRejectionGuidance() string {
	return "当前 PRD 请求未通过来源授权：PRD 必须由话题首条消息的原始发起人在专用新话题首条消息中真实 @本应用并发送“请创建 PRD 草稿”。请话题首条消息作者另开专用新话题发起；回复中的请求、转发或机器人转述不能替代原始发起人。"
}

// chatPRDClassifySource re-derives the validateChatPRDSource verdict so the
// message the agent relays can never disagree with enforcement, mirroring
// its exact check order: human root position → PRD intent → genuine mention.
func chatPRDClassifySource(source lark.LarkMessage, scope chatPRDScope) chatPRDRejection {
	rejection := chatPRDRejection{RequesterOpenID: source.SenderID}
	if source.SenderType != "user" || !strings.HasPrefix(source.SenderID, "ou_") || source.ParentID != "" ||
		(source.RootID != "" && source.RootID != source.MessageID) {
		rejection.Code = prdRejectNotTopicRoot
		rejection.Error = "PRD source must be the topic's original human request, not a reply or bot relay"
		return rejection
	}
	text, _, err := chatPRDPlainText(source, scope)
	if err != nil {
		rejection.Code = prdRejectNotPRDRequest
		rejection.Error = "PRD authorization requires a plain text message"
		return rejection
	}
	if (!strings.Contains(strings.ToLower(text), "prd") && !strings.Contains(text, "需求文档")) || !chatPRDRequestVerb.MatchString(text) {
		rejection.Code = prdRejectNotPRDRequest
		rejection.Error = "topic root must explicitly request creating a PRD or requirement document"
		return rejection
	}
	rejection.Code = prdRejectNoMention
	rejection.Error = "the original PRD request must genuinely mention this app"
	return rejection
}

// chatPRDRejectionForSource classifies the save path's failing source and
// enriches it with the topic-root author when the source is a reply rooted
// at one, so the guidance can point at the human who must open the new
// topic. Lookups are best-effort: unreadable messages leave the optional
// fields empty, never block the refusal itself.
func (h *Handler) chatPRDRejectionForSource(r *http.Request, scope chatPRDScope, source lark.LarkMessage) chatPRDRejection {
	rejection := chatPRDClassifySource(source, scope)
	rejection.Guidance = chatPRDRejectionGuidance()
	rootID := source.RootID
	if rootID == "" {
		if trigger, err := h.chatPRDMessage(r.Context(), scope, scope.triggerID); err == nil && trigger.RootID != "" {
			rootID = trigger.RootID
		}
	}
	if rootID != "" && rootID != source.MessageID {
		if root, err := h.chatPRDMessage(r.Context(), scope, rootID); err == nil {
			rejection.RootAuthorOpenID = root.SenderID
		}
	}
	return rejection
}

// chatPRDTopicRejection returns nil when the topic's ROOT message can
// authorize a PRD — the same verdict SaveChatPRDDraft will reach — else a
// structured rejection. The root is trigger.RootID when the task was
// triggered by a reply (reply-triggered topics), else the trigger itself;
// validating the trigger directly would wrongly refuse legitimate revision
// flows whose triggers are ordinary replies under a valid PRD root.
func (h *Handler) chatPRDTopicRejection(r *http.Request, scope chatPRDScope) *chatPRDRejection {
	trigger, err := h.chatPRDMessage(r.Context(), scope, scope.triggerID)
	if err != nil {
		return nil // unreadable trigger: leave classification to the save path
	}
	rootID := trigger.RootID
	if rootID == "" {
		rootID = trigger.MessageID
	}
	root, err := h.chatPRDMessage(r.Context(), scope, rootID)
	if err != nil || validateChatPRDSource(root, scope) == nil {
		return nil
	}
	rejection := chatPRDClassifySource(root, scope)
	rejection.RootAuthorOpenID = root.SenderID
	rejection.Guidance = chatPRDRejectionGuidance()
	if trigger.SenderID != root.SenderID {
		rejection.RequesterOpenID = trigger.SenderID
	}
	return &rejection
}
