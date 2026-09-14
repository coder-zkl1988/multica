package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/lark"
)

// ChatPRDDraftHistoryItem is one row of the workspace PRD ledger. It is a
// read-only projection of chat_prd_draft (plus the initiator's Multica user
// binding); publication state, document links and failures stay in the
// authoritative table.
type ChatPRDDraftHistoryItem struct {
	ID                     string          `json:"id"`
	InstallationID         string          `json:"installation_id"`
	ChannelChatID          string          `json:"channel_chat_id"`
	ChannelThreadID        string          `json:"channel_thread_id"`
	SourceMessageID        string          `json:"source_message_id"`
	InitiatorOpenID        string          `json:"initiator_open_id"`
	InitiatorMulticaUserID *string         `json:"initiator_multica_user_id,omitempty"`
	Version                int             `json:"version"`
	Content                ChatPRDContent  `json:"content"`
	ConfirmedContent       *ChatPRDContent `json:"confirmed_content,omitempty"`
	ConfirmationMessageID  string          `json:"confirmation_message_id,omitempty"`
	Status                 string          `json:"status"`
	Phase                  string          `json:"phase,omitempty"`
	DocumentID             string          `json:"document_id,omitempty"`
	DocumentURL            string          `json:"document_url,omitempty"`
	Failure                string          `json:"failure,omitempty"`
	VersionCreatedAt       string          `json:"version_created_at"`
	CreatedAt              string          `json:"created_at"`
	UpdatedAt              string          `json:"updated_at"`
}

// ListChatPRDDraftHistory serves the workspace PRD ledger for back-office
// review and statistics: GET /api/chat/prd/history?status=&limit=. It is a
// member-readable projection of chat_prd_draft via the
// chat_prd_draft_history view (migration 923); write paths stay exclusively
// in the task-scoped draft/publish endpoints.
func (h *Handler) ListChatPRDDraftHistory(w http.ResponseWriter, r *http.Request) {
	// Workspace members only: a task token's originator-stamped X-User-ID
	// would otherwise let an agent enumerate the whole workspace's PRD
	// ledger from inside its sandbox. Machine credentials are refused here
	// regardless of the workspace middleware's membership resolution.
	if source := r.Header.Get("X-Actor-Source"); source == "task_token" || source == "service_account" {
		writeError(w, http.StatusForbidden, "workspace members only")
		return
	}
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	limit, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
	if err != nil || limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "" {
		switch status {
		case "draft", "publishing", "failed", "unknown", "published":
		default:
			writeError(w, http.StatusBadRequest, "status must be one of draft, publishing, failed, unknown, published")
			return
		}
	}
	rows, err := h.DB.Query(r.Context(), `SELECT id::text, installation_id::text, channel_chat_id, channel_thread_id,
source_message_id, initiator_open_id, initiator_multica_user_id::text, version, content, confirmed_content,
confirmation_message_id, status, phase, document_id, document_url, failure,
version_created_at::text, created_at::text, updated_at::text
FROM chat_prd_draft_history WHERE workspace_id=$1 AND ($2='' OR status=$2)
ORDER BY updated_at DESC LIMIT $3`, parseUUID(workspaceID), status, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read PRD history")
		return
	}
	defer rows.Close()
	items := make([]ChatPRDDraftHistoryItem, 0, limit)
	for rows.Next() {
		var item ChatPRDDraftHistoryItem
		var confirmed []byte
		var initiatorUserID *string
		if err := rows.Scan(&item.ID, &item.InstallationID, &item.ChannelChatID, &item.ChannelThreadID,
			&item.SourceMessageID, &item.InitiatorOpenID, &initiatorUserID, &item.Version,
			&item.Content, &confirmed, &item.ConfirmationMessageID, &item.Status, &item.Phase,
			&item.DocumentID, &item.DocumentURL, &item.Failure,
			&item.VersionCreatedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read PRD history")
			return
		}
		if initiatorUserID != nil && *initiatorUserID != "" {
			item.InitiatorMulticaUserID = initiatorUserID
		}
		if len(confirmed) != 0 {
			var parsed ChatPRDContent
			if err := json.Unmarshal(confirmed, &parsed); err == nil {
				item.ConfirmedContent = &parsed
			}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read PRD history")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

// Scan support: ChatPRDContent needs to read from pgx jsonb columns.
func (c *ChatPRDContent) Scan(value any) error {
	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, c)
	case string:
		return json.Unmarshal([]byte(v), c)
	}
	return nil
}

var _ = lark.PRDDocumentClient(nil) // keep the lark import for the type contract above
