package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/lark"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const maxChatPRDBytes = 128 * 1024

type ChatPRDContent struct {
	Title    string            `json:"title"`
	Sections []lark.PRDSection `json:"sections"`
}

type ChatPRDDraft struct {
	ID                    string         `json:"draft_id"`
	Version               int            `json:"version"`
	Content               ChatPRDContent `json:"content"`
	SourceMessageID       string         `json:"source_message_id"`
	InitiatorOpenID       string         `json:"initiator_open_id"`
	VersionCreatedAt      time.Time      `json:"version_created_at"`
	Confirmation          string         `json:"confirmation"`
	ConfirmationMessageID string         `json:"confirmation_message_id,omitempty"`
	Status                string         `json:"status"`
	Phase                 string         `json:"phase,omitempty"`
	DocumentID            string         `json:"document_id,omitempty"`
	DocumentURL           string         `json:"document_url,omitempty"`
	Failure               string         `json:"failure,omitempty"`
	confirmedContent      []byte
	claimToken            pgtype.UUID
}

type chatPRDScope struct {
	workspaceID    pgtype.UUID
	installationID pgtype.UUID
	sessionID      pgtype.UUID
	chatID         string
	threadID       string
	triggerID      string
	botOpenID      string
	botUnionID     string
	credentials    lark.InstallationCredentials
}

func (s chatPRDScope) args() []any {
	return []any{s.workspaceID, s.installationID, s.chatID, s.threadID}
}

func (s chatPRDScope) lockKey() string {
	return "chat-prd:" + uuidToString(s.workspaceID) + ":" + uuidToString(s.installationID) + ":" + s.chatID + ":" + s.threadID
}

// Scope is derived exclusively from the authenticated task and its frozen
// channel delivery. Mutable context initiators never authorize document writes.
func (h *Handler) chatPRDScope(w http.ResponseWriter, r *http.Request) (chatPRDScope, bool) {
	history, ok := h.chatHistorySession(w, r)
	if !ok {
		return chatPRDScope{}, false
	}
	fail := func(status int, message string) (chatPRDScope, bool) {
		writeError(w, status, message)
		return chatPRDScope{}, false
	}
	if h.DB == nil || h.TxStarter == nil || h.LarkInstallations == nil || h.LarkAPIClient == nil {
		return fail(http.StatusServiceUnavailable, "PRD integration is not configured")
	}
	session, err := h.Queries.GetChatSession(r.Context(), history.sessionID)
	if err != nil {
		return fail(http.StatusNotFound, "chat session not found")
	}
	if r.Header.Get("X-Workspace-ID") == "" || r.Header.Get("X-Workspace-ID") != uuidToString(session.WorkspaceID) {
		return fail(http.StatusForbidden, "PRD requires an authenticated task workspace")
	}
	taskID := parseUUID(r.Header.Get("X-Task-ID")) // chatHistorySession validated this boundary.
	task, err := h.Queries.GetAgentTask(r.Context(), taskID)
	if err != nil || task.AgentID != session.AgentID {
		return fail(http.StatusForbidden, "task does not own this chat session")
	}
	binding, err := h.Queries.GetChannelChatSessionBindingBySessionAny(r.Context(), session.ID)
	if err != nil {
		return fail(http.StatusForbidden, "PRD requires a channel-bound chat task")
	}
	delivery, err := h.Queries.GetChannelTaskDelivery(r.Context(), taskID)
	if err != nil {
		return fail(http.StatusForbidden, "PRD requires immutable task channel delivery")
	}
	if session.Status != "active" || binding.RetiredAt.Valid || binding.PendingFresh ||
		delivery.BindingID != binding.ID || delivery.InstallationID != binding.InstallationID ||
		delivery.RouteRevision != binding.RouteRevision || delivery.ChannelChatID != binding.ChannelChatID ||
		delivery.ChannelType != "feishu" || binding.ChannelType != "feishu" || delivery.ChatType != "group" ||
		binding.ChatType != "group" || delivery.ChannelThreadID.String == "" || delivery.ChannelMessageID.String == "" {
		return fail(http.StatusForbidden, "PRD is only available in the current Feishu group topic")
	}
	if task.ChannelContextRevision.Valid && task.ChannelContextRevision.Int64 != binding.ContextRevision {
		return fail(http.StatusConflict, "this chat task belongs to an older context")
	}
	var route struct {
		ChatID string `json:"chat_id"`
	}
	if json.Unmarshal(delivery.Config, &route) != nil || route.ChatID == "" ||
		delivery.ChannelChatID != route.ChatID+":"+delivery.ChannelThreadID.String {
		return fail(http.StatusForbidden, "invalid Feishu topic routing")
	}
	inst, err := h.LarkInstallations.GetInWorkspace(r.Context(), delivery.InstallationID, session.WorkspaceID)
	if err != nil || inst.Status != string(lark.InstallationActive) || inst.AgentID != session.AgentID {
		return fail(http.StatusForbidden, "PRD installation is not active for this agent")
	}
	secret, err := h.LarkInstallations.DecryptAppSecret(inst)
	if err != nil {
		return fail(http.StatusServiceUnavailable, "PRD installation credentials are unavailable")
	}
	return chatPRDScope{
		workspaceID: session.WorkspaceID, installationID: inst.ID, sessionID: session.ID,
		chatID: route.ChatID, threadID: delivery.ChannelThreadID.String, triggerID: delivery.ChannelMessageID.String,
		botOpenID:   inst.BotOpenID,
		botUnionID:  inst.BotUnionID.String,
		credentials: lark.InstallationCredentials{AppID: inst.AppID, AppSecret: secret, TenantKey: inst.TenantKey.String, Region: lark.RegionOrDefault(inst.Region)},
	}, true
}

const chatPRDScopeWhere = `workspace_id=$1 AND installation_id=$2 AND channel_chat_id=$3 AND channel_thread_id=$4`
const chatPRDColumns = `id::text, version, content, source_message_id, initiator_open_id, version_created_at,
 confirmation_message_id, status, phase, document_id, document_url, failure, confirmed_content, claim_token`

func readChatPRD(ctx context.Context, db dbExecutor, scope chatPRDScope) (ChatPRDDraft, error) {
	return scanChatPRD(db.QueryRow(ctx, `SELECT `+chatPRDColumns+` FROM chat_prd_draft WHERE `+chatPRDScopeWhere, scope.args()...))
}

func scanChatPRD(row pgx.Row) (ChatPRDDraft, error) {
	var draft ChatPRDDraft
	var content []byte
	err := row.Scan(&draft.ID, &draft.Version, &content, &draft.SourceMessageID, &draft.InitiatorOpenID,
		&draft.VersionCreatedAt, &draft.ConfirmationMessageID, &draft.Status, &draft.Phase,
		&draft.DocumentID, &draft.DocumentURL, &draft.Failure, &draft.confirmedContent, &draft.claimToken)
	if err != nil {
		return draft, err
	}
	if err := json.Unmarshal(content, &draft.Content); err != nil {
		return draft, err
	}
	if draft.Status == "draft" {
		draft.Confirmation = chatPRDConfirmation(draft.ID, draft.Version)
	}
	return draft, nil
}

func chatPRDConfirmation(id string, version int) string {
	return fmt.Sprintf("确认创建 %s v%d", id, version)
}

func (h *Handler) GetChatPRD(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.chatPRDScope(w, r)
	if !ok {
		return
	}
	// Same advisory early refusal as the template read: an unauthorized
	// topic gets the actionable rejection immediately (before any drafting
	// work), while the authoritative verdict stays in draft/publish.
	if rejection := h.chatPRDTopicRejection(r, scope); rejection != nil {
		writeChatPRDRejection(w, http.StatusForbidden, *rejection)
		return
	}
	draft, err := readChatPRD(r.Context(), h.DB, scope)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "no PRD draft exists in this topic")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read PRD draft")
		return
	}
	writeJSON(w, http.StatusOK, draft)
}

func (h *Handler) chatPRDTemplateHeadings(w http.ResponseWriter, r *http.Request, scope chatPRDScope) ([]string, bool) {
	template, ok := h.LarkAPIClient.(lark.PRDTemplateReader)
	token := strings.TrimSpace(os.Getenv("MULTICA_PRD_TEMPLATE_WIKI_TOKEN"))
	if !ok || token == "" {
		writeError(w, http.StatusServiceUnavailable, "PRD template reading is not configured")
		return nil, false
	}
	headings, err := template.ReadPRDTemplateHeadings(r.Context(), scope.credentials, token)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to read unambiguous PRD template headings: "+err.Error())
		return nil, false
	}
	return headings, true
}

func (h *Handler) GetChatPRDTemplate(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.chatPRDScope(w, r)
	if !ok {
		return
	}
	// Fast, actionable refusal: when this topic's ROOT message cannot
	// authorize a PRD, say so here with the structured rejection instead of
	// letting the agent spend a full drafting run discovering it at save
	// time. Advisory only — the save path re-validates authoritatively, and
	// the root (trigger.RootID, else the trigger) is what it validates, so
	// ordinary reply triggers under a valid root still read the template.
	if rejection := h.chatPRDTopicRejection(r, scope); rejection != nil {
		writeChatPRDRejection(w, http.StatusForbidden, *rejection)
		return
	}
	headings, ok := h.chatPRDTemplateHeadings(w, r, scope)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"template_headings": headings})
}

func decodeChatPRDRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxChatPRDBytes))
	if err != nil || !utf8.Valid(raw) {
		writeError(w, http.StatusBadRequest, "PRD request must be UTF-8 JSON within 128 KiB")
		return false
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid PRD request")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, http.StatusBadRequest, "PRD request must contain one JSON object")
		return false
	}
	return true
}

func validateChatPRDContent(content ChatPRDContent) error {
	if strings.TrimSpace(content.Title) == "" || len(content.Title) > 256 || len(content.Sections) == 0 || len(content.Sections) > 50 {
		return errors.New("PRD requires a title (up to 256 bytes) and 1-50 sections")
	}
	seen := make(map[string]bool, len(content.Sections))
	for _, section := range content.Sections {
		heading := strings.TrimSpace(section.Heading)
		if heading == "" || len(heading) > 500 || strings.TrimSpace(section.Body) == "" || len(section.Body) > 20000 || seen[heading] {
			return errors.New("PRD requires unique headings and nonempty bodies of at most 20000 bytes; mark unknowns explicitly")
		}
		seen[heading] = true
		paragraphs := 0
		for _, line := range strings.Split(strings.ReplaceAll(section.Body, "\r\n", "\n"), "\n") {
			paragraphs += max(1, (utf8.RuneCountInString(line)+999)/1000)
		}
		if paragraphs > 50 {
			return errors.New("PRD section exceeds 50 native text blocks")
		}
	}
	return nil
}

func (h *Handler) chatPRDMessage(ctx context.Context, scope chatPRDScope, id string) (lark.LarkMessage, error) {
	messages, err := h.LarkAPIClient.GetMessage(ctx, scope.credentials, id)
	if err != nil {
		return lark.LarkMessage{}, errors.New("failed to verify message with Feishu")
	}
	if len(messages) != 1 {
		return lark.LarkMessage{}, errors.New("message is not available")
	}
	message := messages[0]
	if message.MessageID != id || message.ChatID != scope.chatID || message.ThreadID != scope.threadID || message.Deleted || message.UpperMessageID != "" {
		return lark.LarkMessage{}, errors.New("message is not a live message in the current topic")
	}
	return message, nil
}

var chatPRDRequestVerb = regexp.MustCompile(`(?i)(写|创建|生成|整理|产出|做|输出|create|draft|write|generate)`)
var chatPRDMentionKey = regexp.MustCompile(`@_user_[0-9]+`)

func chatPRDPlainText(message lark.LarkMessage, scope chatPRDScope) (string, bool, error) {
	if message.MessageType != "text" {
		return "", false, errors.New("PRD authorization requires a plain text message")
	}
	var content struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(message.Content), &content); err != nil {
		return "", false, errors.New("invalid message content")
	}
	mentioned := false
	text := chatPRDMentionKey.ReplaceAllStringFunc(content.Text, func(key string) string {
		// Match whole native tokens, not prefixes or arbitrary metadata keys.
		// A metadata-only or ambiguously assigned mention cannot authorize.
		var matched *lark.LarkMessageMention
		for i := range message.Mentions {
			mention := &message.Mentions[i]
			if mention.Key != key {
				continue
			}
			if matched != nil {
				return key
			}
			matched = mention
		}
		if matched == nil || matched.ID == "" {
			return key
		}
		if matched.IsBotMention(scope.credentials.AppID, scope.botOpenID, scope.botUnionID) {
			mentioned = true
		}
		return ""
	})
	return strings.TrimSpace(text), mentioned, nil
}

func validateChatPRDSource(source lark.LarkMessage, scope chatPRDScope) error {
	if source.SenderType != "user" || !strings.HasPrefix(source.SenderID, "ou_") || source.ParentID != "" ||
		(source.RootID != "" && source.RootID != source.MessageID) {
		return errors.New("PRD source must be the topic's original human request, not a reply or bot relay")
	}
	text, mentioned, err := chatPRDPlainText(source, scope)
	if err != nil {
		return err
	}
	if (!strings.Contains(strings.ToLower(text), "prd") && !strings.Contains(text, "需求文档")) || !chatPRDRequestVerb.MatchString(text) {
		return errors.New("topic root must explicitly request creating a PRD or requirement document")
	}
	if !mentioned {
		return errors.New("the original PRD request must mention this app")
	}
	return nil
}

func validateChatPRDConfirmation(message lark.LarkMessage, draft ChatPRDDraft, scope chatPRDScope) error {
	if message.SenderType != "user" || message.SenderID != draft.InitiatorOpenID || message.Deleted ||
		message.UpperMessageID != "" || message.RootID != draft.SourceMessageID {
		return errors.New("only the original human requester can confirm in the original topic")
	}
	created, err := strconv.ParseInt(message.CreateTime, 10, 64)
	if err != nil || !time.UnixMilli(created).After(draft.VersionCreatedAt) {
		return errors.New("confirmation must be newer than this draft version")
	}
	text, mentioned, err := chatPRDPlainText(message, scope)
	if err != nil {
		return err
	}
	if !mentioned {
		return errors.New("confirmation must genuinely mention this app")
	}
	if text != chatPRDConfirmation(draft.ID, draft.Version) {
		return errors.New("confirmation must exactly match the current draft's confirmation phrase")
	}
	return nil
}

func (h *Handler) SaveChatPRDDraft(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.chatPRDScope(w, r)
	if !ok {
		return
	}
	var request struct {
		SourceMessageID string         `json:"source_message_id"`
		Content         ChatPRDContent `json:"content"`
	}
	if !decodeChatPRDRequest(w, r, &request) {
		return
	}
	if request.SourceMessageID == "" {
		writeError(w, http.StatusBadRequest, "source_message_id is required")
		return
	}
	if err := validateChatPRDContent(request.Content); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	source, err := h.chatPRDMessage(r.Context(), scope, request.SourceMessageID)
	if err != nil {
		writeChatPRDRejection(w, http.StatusForbidden, chatPRDRejection{
			Error:    err.Error(),
			Code:     prdRejectNotTopicRoot,
			Guidance: chatPRDRejectionGuidance(),
		})
		return
	}
	if err := validateChatPRDSource(source, scope); err != nil {
		writeChatPRDRejection(w, http.StatusForbidden, h.chatPRDRejectionForSource(r, scope, source))
		return
	}
	trigger, err := h.chatPRDMessage(r.Context(), scope, scope.triggerID)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	if trigger.MessageID != source.MessageID && trigger.RootID != source.MessageID {
		writeError(w, http.StatusForbidden, "source is not the root of this task's triggering topic")
		return
	}
	headings, ok := h.chatPRDTemplateHeadings(w, r, scope)
	if !ok {
		return
	}
	for _, section := range request.Content.Sections {
		found := false
		for _, heading := range headings {
			if section.Heading == heading {
				found = true
				break
			}
		}
		if !found {
			writeError(w, http.StatusBadRequest, "PRD heading is not in the current template; read multica chat prd template before drafting")
			return
		}
	}
	content, _ := json.Marshal(request.Content)
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save PRD draft")
		return
	}
	defer tx.Rollback(context.Background())
	// The session lock participates in the existing delete/archive protocol.
	locked, err := h.Queries.WithTx(tx).LockChatSessionForDraftWrite(r.Context(), scope.sessionID)
	if err != nil || locked.Status != "active" {
		writeError(w, http.StatusConflict, "chat session is no longer active")
		return
	}
	currentScope, ok := h.chatPRDScope(w, r)
	if !ok {
		return
	}
	if currentScope.sessionID != scope.sessionID || currentScope.lockKey() != scope.lockKey() {
		writeError(w, http.StatusConflict, "PRD task scope changed while waiting to save")
		return
	}
	// The direct writer must still act for the original root author. Neither
	// the current operator nor mutable topic context can lend human authority.
	qtx := h.Queries.WithTx(tx)
	task, err := qtx.GetAgentTask(r.Context(), parseUUID(r.Header.Get("X-Task-ID")))
	if err != nil || isTerminalTaskStatus(task.Status) {
		writeError(w, http.StatusForbidden, "PRD drafting requires an active task")
		return
	}
	identity, err := qtx.GetChannelUserBindingByUserID(r.Context(), db.GetChannelUserBindingByUserIDParams{
		InstallationID: scope.installationID, ChannelUserID: source.SenderID,
	})
	if err != nil || identity.WorkspaceID != scope.workspaceID || !task.OriginatorUserID.Valid || identity.MulticaUserID != task.OriginatorUserID {
		writeError(w, http.StatusForbidden, "PRD drafting requires the original requester's verified human task identity; no account fallback is allowed")
		return
	}
	if _, err := h.getWorkspaceMember(r.Context(), uuidToString(identity.MulticaUserID), uuidToString(scope.workspaceID)); err != nil {
		writeError(w, http.StatusForbidden, "the original requester is not a workspace member")
		return
	}
	agent, err := qtx.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: task.AgentID, WorkspaceID: scope.workspaceID})
	if err != nil || agent.ArchivedAt.Valid || !h.canInvokeAgent(r.Context(), agent, "member", uuidToString(identity.MulticaUserID), "", uuidToString(scope.workspaceID)) {
		h.writeDispatchBlocked(w, http.StatusForbidden, ReasonInvocationNotAllowed)
		return
	}
	args := append(scope.args(), source.MessageID, source.SenderID, content)
	draft, err := scanChatPRD(tx.QueryRow(r.Context(), `INSERT INTO chat_prd_draft
 (workspace_id, installation_id, channel_chat_id, channel_thread_id, source_message_id, initiator_open_id, content)
 VALUES ($1,$2,$3,$4,$5,$6,$7)
 ON CONFLICT (workspace_id, installation_id, channel_chat_id, channel_thread_id) DO UPDATE SET
 content=EXCLUDED.content,
 version=chat_prd_draft.version + CASE WHEN chat_prd_draft.content=EXCLUDED.content THEN 0 ELSE 1 END,
 version_created_at=CASE WHEN chat_prd_draft.content=EXCLUDED.content THEN chat_prd_draft.version_created_at ELSE clock_timestamp() END,
 confirmation_message_id='', updated_at=clock_timestamp()
 WHERE chat_prd_draft.status='draft' AND chat_prd_draft.confirmed_content IS NULL
 AND chat_prd_draft.source_message_id=EXCLUDED.source_message_id AND chat_prd_draft.initiator_open_id=EXCLUDED.initiator_open_id
 RETURNING `+chatPRDColumns, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		existing, readErr := readChatPRD(r.Context(), tx, scope)
		if readErr == nil {
			same, _ := json.Marshal(existing.Content)
			if string(same) == string(content) && existing.SourceMessageID == source.MessageID && existing.InitiatorOpenID == source.SenderID {
				writeJSON(w, http.StatusOK, existing)
				return
			}
		}
		writeError(w, http.StatusConflict, "confirmed PRD snapshots cannot be changed or assigned to another source")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save PRD draft")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit PRD draft")
		return
	}
	writeJSON(w, http.StatusOK, draft)
}

// persistChatPRDPhase survives caller cancellation. Every write is fenced by
// the claim and topic so an interrupted older publisher cannot overwrite a run.
func (h *Handler) persistChatPRDPhase(scope chatPRDScope, draft *ChatPRDDraft, status, phase, failure string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	args := append(scope.args(), draft.ID, draft.Version, draft.claimToken, status, phase, failure, draft.DocumentID, draft.DocumentURL)
	tag, err := h.DB.Exec(ctx, `UPDATE chat_prd_draft SET status=$8, phase=$9, failure=$10,
 document_id=$11, document_url=$12, updated_at=clock_timestamp()
 WHERE `+chatPRDScopeWhere+` AND id=$5 AND version=$6 AND claim_token=$7`, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("PRD claim was lost")
	}
	draft.Status, draft.Phase, draft.Failure = status, phase, failure
	return nil
}

func (h *Handler) PublishChatPRD(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.chatPRDScope(w, r)
	if !ok {
		return
	}
	var request struct {
		DraftID               string `json:"draft_id"`
		Version               int    `json:"version"`
		ConfirmationMessageID string `json:"confirmation_message_id"`
	}
	if !decodeChatPRDRequest(w, r, &request) {
		return
	}
	if _, ok := parseUUIDOrBadRequest(w, request.DraftID, "draft_id"); !ok {
		return
	}
	if request.Version < 1 || request.ConfirmationMessageID == "" {
		writeError(w, http.StatusBadRequest, "version and confirmation_message_id are required")
		return
	}
	client, ok := h.LarkAPIClient.(lark.PRDDocumentClient)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "PRD document client is not configured")
		return
	}
	// Keep the topic mutex and teardown interlocks separate from phase writes:
	// rolling back these locks must never roll back knowledge of a remote copy.
	mutex, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock PRD publishing")
		return
	}
	defer mutex.Rollback(context.Background())
	var acquired bool
	if err := mutex.QueryRow(r.Context(), `SELECT pg_try_advisory_xact_lock(hashtextextended($1,0))`, scope.lockKey()).Scan(&acquired); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock PRD publishing")
		return
	}
	if !acquired {
		writeError(w, http.StatusConflict, "PRD publish is already in progress")
		return
	}
	// Take workspace before session, matching workspace teardown. The topic
	// try-lock stays first so another publisher fails promptly instead of waiting
	// for these row locks. Never lock the draft here: its phase ledger commits
	// independently while these interlocks keep deletion from sweeping it.
	qtx := h.Queries.WithTx(mutex)
	if _, err := qtx.LockWorkspaceForChatSessionCreate(r.Context(), scope.workspaceID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "workspace is no longer available")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to lock PRD workspace")
		}
		return
	}
	locked, err := qtx.LockChatSessionForDraftWrite(r.Context(), scope.sessionID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && locked.Status != "active") {
		writeError(w, http.StatusConflict, "chat session is no longer active")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock PRD chat session")
		return
	}
	// The initial scope may predate a wait for teardown, archive, or rebinding.
	// Revalidate all task/channel authorization before any remote document call.
	currentScope, ok := h.chatPRDScope(w, r)
	if !ok {
		return
	}
	if currentScope.sessionID != scope.sessionID || currentScope.lockKey() != scope.lockKey() {
		writeError(w, http.StatusConflict, "PRD task scope changed while waiting to publish")
		return
	}
	scope = currentScope
	draft, err := readChatPRD(r.Context(), h.DB, scope)
	if err != nil {
		writeError(w, http.StatusNotFound, "PRD draft not found")
		return
	}
	if draft.ID != request.DraftID || draft.Version != request.Version {
		writeError(w, http.StatusConflict, "draft or version is not current; read the topic draft again")
		return
	}
	source, err := h.chatPRDMessage(r.Context(), scope, draft.SourceMessageID)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	if source.SenderID != draft.InitiatorOpenID || validateChatPRDSource(source, scope) != nil {
		writeError(w, http.StatusForbidden, "original human PRD request is no longer valid")
		return
	}
	confirmation, err := h.chatPRDMessage(r.Context(), scope, request.ConfirmationMessageID)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	if err := validateChatPRDConfirmation(confirmation, draft, scope); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	if draft.Status == "published" {
		writeJSON(w, http.StatusOK, draft)
		return
	}
	if draft.ConfirmationMessageID != "" && draft.ConfirmationMessageID != request.ConfirmationMessageID {
		writeError(w, http.StatusConflict, "this snapshot is already bound to another confirmation; reuse its message ID")
		return
	}
	if draft.DocumentID == "" && (draft.Status == "unknown" || (draft.Status == "publishing" && draft.Phase == "copy")) {
		if err := h.persistChatPRDPhase(scope, &draft, "unknown", "copy", "copy outcome is unknown; operator reconciliation required, never create another copy"); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record unknown PRD copy outcome")
			return
		}
		writeJSON(w, http.StatusConflict, draft)
		return
	}
	template := strings.TrimSpace(os.Getenv("MULTICA_PRD_TEMPLATE_WIKI_TOKEN"))
	if draft.DocumentID == "" && template == "" {
		writeError(w, http.StatusServiceUnavailable, "MULTICA_PRD_TEMPLATE_WIKI_TOKEN is not configured; no document was created")
		return
	}
	claim := parseUUID(uuid.NewString())
	args := append(scope.args(), draft.ID, draft.Version, request.ConfirmationMessageID, claim)
	tag, err := h.DB.Exec(r.Context(), `UPDATE chat_prd_draft SET status='publishing', claim_token=$8,
 confirmed_content=COALESCE(confirmed_content,content), confirmation_message_id=$7, failure='', updated_at=clock_timestamp()
 WHERE `+chatPRDScopeWhere+` AND id=$5 AND version=$6 AND status IN ('draft','failed','publishing','unknown')`, args...)
	if err != nil || tag.RowsAffected() != 1 {
		writeError(w, http.StatusConflict, "PRD publish claim failed; no document action was started")
		return
	}
	draft.claimToken, draft.Status, draft.ConfirmationMessageID = claim, "publishing", request.ConfirmationMessageID
	draft.Confirmation = ""
	// Reload the atomically frozen snapshot rather than caller-supplied content.
	draft, err = readChatPRD(r.Context(), h.DB, scope)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load claimed PRD snapshot")
		return
	}
	var snapshot ChatPRDContent
	if err := json.Unmarshal(draft.confirmedContent, &snapshot); err != nil {
		writeError(w, http.StatusInternalServerError, "invalid confirmed PRD snapshot")
		return
	}
	fail := func(status, phase, message string) {
		if err := h.persistChatPRDPhase(scope, &draft, status, phase, message); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to persist PRD publish result; do not create another document")
			return
		}
		writeJSON(w, http.StatusBadGateway, draft)
	}
	phase := func(name string) bool {
		if err := h.persistChatPRDPhase(scope, &draft, "publishing", name, ""); err != nil {
			writeError(w, http.StatusInternalServerError, "PRD claim persistence failed; document action stopped")
			return false
		}
		return true
	}
	if draft.DocumentID == "" {
		if !phase("resolve") {
			return
		}
		sourceID, err := client.ResolvePRDTemplate(r.Context(), scope.credentials, template)
		if err != nil {
			fail("failed", "resolve", "source template could not be resolved; no copy attempted")
			return
		}
		if !phase("copy") {
			return
		}
		document, copyErr := client.CopyPRDTemplate(r.Context(), scope.credentials, sourceID, snapshot.Title)
		draft.DocumentID, draft.DocumentURL = document.ID, document.URL
		if copyErr != nil {
			status := "unknown"
			var copyFailure *lark.PRDCopyError
			if document.ID != "" || (errors.As(copyErr, &copyFailure) && !copyFailure.OutcomeUnknown) {
				status = "failed"
			}
			fail(status, "copy", "template copy failed; known document will be reused, unknown outcome requires operator reconciliation")
			return
		}
		if document.ID == "" {
			fail("unknown", "copy", "copy returned no document ID; operator reconciliation required")
			return
		}
		if !phase("fill") {
			return
		}
	}
	// Resume from the durable phase. Fill is idempotent within this exact
	// confirmed snapshot; ownership and verification never need another copy.
	if draft.Phase != "owner" && draft.Phase != "verify" {
		if !phase("fill") {
			return
		}
		if err := client.FillPRDDocument(r.Context(), scope.credentials, draft.DocumentID, snapshot.Sections); err != nil {
			fail("failed", "fill", "document fill failed; retry resumes the same document")
			return
		}
		if !phase("owner") {
			return
		}
	}
	if draft.Phase != "verify" {
		if !phase("owner") {
			return
		}
		if err := client.FinalizePRDOwner(r.Context(), scope.credentials, draft.DocumentID, draft.InitiatorOpenID); err != nil {
			fail("failed", "owner", "owner transfer failed; retry resumes the same document")
			return
		}
		if !phase("verify") {
			return
		}
	}
	if err := client.VerifyPRDDocument(r.Context(), scope.credentials, draft.DocumentID, snapshot.Sections, draft.InitiatorOpenID); err != nil {
		fail("failed", "verify", "document or owner verification failed; publishing is not complete")
		return
	}
	if draft.DocumentURL == "" {
		url, err := client.GetPRDDocumentURL(r.Context(), scope.credentials, draft.DocumentID)
		if err != nil || url == "" {
			fail("failed", "verify", "document URL lookup failed; retry resumes the same verified document")
			return
		}
		draft.DocumentURL = url
	}
	if err := h.persistChatPRDPhase(scope, &draft, "published", "verify", ""); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist verified PRD; retry with the same confirmation")
		return
	}
	writeJSON(w, http.StatusOK, draft)
}
