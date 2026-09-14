package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/designdocument"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
)

// UploadDesignDocumentLivePreview stores observation-only output. It never
// advances a document revision or bypasses the final Audit/Preview gate.
func (h *Handler) UploadDesignDocumentLivePreview(w http.ResponseWriter, r *http.Request) {
	task, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, chi.URLParam(r, "taskId"))
	if !ok {
		return
	}
	if !task.RuntimeID.Valid {
		writeError(w, http.StatusConflict, "task runtime is unavailable")
		return
	}
	runtime, ok := h.requireDaemonRuntimeAccess(w, r, uuidToString(task.RuntimeID))
	if !ok {
		return
	}
	if daemonID := middleware.DaemonIDFromContext(r.Context()); daemonID != "" && (!runtime.DaemonID.Valid || runtime.DaemonID.String != daemonID) {
		writeError(w, http.StatusForbidden, "task belongs to another daemon")
		return
	}
	var binding service.DesignDocumentTaskContext
	if task.Status != "running" || json.Unmarshal(task.Context, &binding) != nil || binding.Type != service.DesignDocumentTaskContextType || binding.WorkspaceID != workspaceID || binding.AgentID != uuidToString(task.AgentID) {
		writeProjectDesignSystemError(w, http.StatusConflict, "live_preview_task_invalid", "live preview requires its running design task")
		return
	}
	documentID, ok := parseUUIDOrBadRequest(w, binding.DesignDocumentID, "design_document_id")
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 6<<20)
	var snapshot designdocument.LivePreview
	if json.NewDecoder(r.Body).Decode(&snapshot) != nil || snapshot.Validate() != nil {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "live_preview_invalid", "invalid bounded prototype snapshot")
		return
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode preview")
		return
	}
	digest, err := snapshot.Digest()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to digest preview")
		return
	}
	// Recheck the task state and document binding in the write statement so a
	// late publisher cannot replace the snapshot of another or completed run.
	written, err := h.DB.Exec(r.Context(), `
 WITH locked AS MATERIALIZED (
 SELECT q.id,a.workspace_id,d.id AS document_id
 FROM agent_task_queue q JOIN agent a ON a.id=q.agent_id AND a.workspace_id=$2
 JOIN design_document d ON d.id=$3 AND d.workspace_id=a.workspace_id
 WHERE q.id=$1 AND q.status='running' AND d.active_task_id=q.id
 AND lock_task_owner_rows(q.agent_id,q.issue_id,q.runtime_id)
 FOR UPDATE OF d,q
 )
 INSERT INTO design_document_live_preview (task_id,workspace_id,document_id,content_digest,snapshot,updated_at)
 SELECT id,workspace_id,document_id,$4,$5,now() FROM locked
 ON CONFLICT (task_id) DO UPDATE SET content_digest=EXCLUDED.content_digest,snapshot=EXCLUDED.snapshot,updated_at=EXCLUDED.updated_at`, task.ID, workspaceID, documentID, digest, data)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store preview")
		return
	}
	if written.RowsAffected() != 1 {
		writeProjectDesignSystemError(w, http.StatusConflict, "live_preview_task_invalid", "document is no longer running this task")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetDesignDocumentLivePreview(w http.ResponseWriter, r *http.Request) {
	document, workspaceID, ok := h.loadDesignDocumentForRequest(w, r)
	if !ok {
		return
	}
	taskID, ok := parseUUIDOrBadRequest(w, r.URL.Query().Get("task_id"), "task_id")
	if !ok {
		return
	}
	var data []byte
	var digest string
	var updated time.Time
	err := h.DB.QueryRow(r.Context(), `SELECT snapshot,content_digest,updated_at FROM design_document_live_preview WHERE task_id=$1 AND document_id=$2 AND workspace_id=$3`, taskID, document.ID, workspaceID).Scan(&data, &digest, &updated)
	w.Header().Set("Cache-Control", "no-store")
	if isNotFound(err) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read preview")
		return
	}
	var snapshot designdocument.LivePreview
	if json.Unmarshal(data, &snapshot) != nil || snapshot.Validate() != nil {
		writeError(w, http.StatusInternalServerError, "invalid stored preview")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		designdocument.LivePreview
		TaskID        string    `json:"task_id"`
		DocumentID    string    `json:"document_id"`
		ContentDigest string    `json:"content_digest"`
		UpdatedAt     time.Time `json:"updated_at"`
	}{snapshot, uuidToString(taskID), uuidToString(document.ID), digest, updated})
}
