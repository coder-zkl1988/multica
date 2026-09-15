package handler

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/lark"
)

// GetChatDocument reads a docx link from the current Feishu task without
// exposing installation credentials to the agent. The task/session/topic and
// installation are pinned by chatPRDScope; the supplied URL contributes only
// a validated docx token.
func (h *Handler) GetChatDocument(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.chatPRDScope(w, r)
	if !ok {
		return
	}
	reader, ok := h.LarkAPIClient.(lark.TaskDocumentReader)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "Feishu document reading is not configured")
		return
	}
	docID, ok := chatDocxID(r.URL.Query().Get("url"))
	if !ok {
		writeError(w, http.StatusBadRequest, "url must be an HTTPS Feishu docx link")
		return
	}
	document, err := reader.ReadDocxText(r.Context(), scope.credentials, docID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to read Feishu document: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, document)
}

func chatDocxID(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Port() != "" || u.RawQuery != "" {
		return "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "docx" || parts[1] == "" || len(parts[1]) > 256 {
		return "", false
	}
	for _, r := range parts[1] {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return "", false
		}
	}
	return parts[1], true
}
