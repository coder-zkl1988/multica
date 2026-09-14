package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/designimplementation"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// CommentDesignRequest is an explicit delivery action, not an issue assignment.
type CommentDesignRequest struct {
	RequestID         string   `json:"request_id"`
	Operation         string   `json:"operation"`
	AgentID           string   `json:"agent_id"`
	ProjectResourceID string   `json:"project_resource_id"`
	DesignSystemID    string   `json:"design_system_id,omitempty"`
	DesignRef         string   `json:"design_ref,omitempty"`
	RevisionID        string   `json:"revision_id,omitempty"`
	FrameRefs         []string `json:"frame_refs,omitempty"`
}

type CommentDesignDeliveryResponse struct {
	Operation         string `json:"operation"`
	TaskID            string `json:"task_id"`
	DocumentID        string `json:"document_id,omitempty"`
	AgentID           string `json:"agent_id"`
	ProjectResourceID string `json:"project_resource_id"`
}

// The private snapshot survives body edits. Only the response projection leaves
// the server; signed refs and the original requirement are not extra public fields.
type commentDesignDelivery struct {
	CommentDesignDeliveryResponse
	RequestID      string `json:"request_id"`
	Fingerprint    string `json:"fingerprint"`
	RequestContent string `json:"request_content"`
}

func commentDesignDeliveryResponse(raw []byte) *CommentDesignDeliveryResponse {
	if len(raw) == 0 {
		return nil
	}
	var delivery CommentDesignDeliveryResponse
	if json.Unmarshal(raw, &delivery) != nil || delivery.TaskID == "" {
		return nil
	}
	return &delivery
}

func commentDesignDeliveryContent(comment db.Comment) string {
	var delivery commentDesignDelivery
	if len(comment.DesignDelivery) > 0 && json.Unmarshal(comment.DesignDelivery, &delivery) == nil && delivery.RequestContent != "" {
		return delivery.RequestContent
	}
	return comment.Content
}

func (h *Handler) createCommentDesignDelivery(w http.ResponseWriter, r *http.Request, issue db.Issue, req CreateCommentRequest, parentID pgtype.UUID, root *db.Comment, actorType, actorID string) {
	if actorType != "member" {
		writeError(w, http.StatusForbidden, "design delivery must be explicitly requested by a member")
		return
	}
	request := *req.DesignRequest
	request.RequestID = strings.TrimSpace(request.RequestID)
	if request.RequestID == "" || len(request.RequestID) > 128 || strings.ContainsRune(request.RequestID, 0) {
		writeError(w, http.StatusBadRequest, "request_id must contain between 1 and 128 bytes")
		return
	}
	if (request.Operation != "design" && request.Operation != "implement") || req.Type != "comment" {
		writeError(w, http.StatusBadRequest, "invalid design delivery operation or comment type")
		return
	}
	if !issue.ProjectID.Valid || strings.TrimSpace(req.Content) == "" || len(req.Content) > designDocumentMaxBriefBytes {
		writeError(w, http.StatusBadRequest, "design delivery requires a project and non-empty requirements within the size limit")
		return
	}
	if len(req.SuppressAgentIDs) > 0 {
		writeError(w, http.StatusBadRequest, "design delivery uses only the explicitly selected agent")
		return
	}
	if request.Operation == "design" && (request.DesignRef != "" || request.RevisionID != "" || len(request.FrameRefs) != 0) {
		writeError(w, http.StatusBadRequest, "design generation does not accept implementation references")
		return
	}
	if request.Operation == "implement" && request.DesignSystemID != "" {
		writeError(w, http.StatusBadRequest, "implementation uses the frozen design reference, not a replacement design system")
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, request.AgentID, "agent_id")
	if !ok {
		return
	}
	resourceID, ok := parseUUIDOrBadRequest(w, request.ProjectResourceID, "project_resource_id")
	if !ok {
		return
	}
	request.AgentID, request.ProjectResourceID = uuidToString(agentID), uuidToString(resourceID)
	if request.DesignSystemID != "" {
		systemID, ok := parseUUIDOrBadRequest(w, request.DesignSystemID, "design_system_id")
		if !ok {
			return
		}
		request.DesignSystemID = uuidToString(systemID)
	}
	requesterID := parseUUID(actorID)
	canonical, err := json.Marshal(struct {
		Request     CommentDesignRequest
		Content     string
		ParentID    pgtype.UUID
		Attachments []string
	}{request, req.Content, parentID, req.AttachmentIDs})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid design request")
		return
	}
	digest := sha256.Sum256(canonical)
	fingerprint := hex.EncodeToString(digest[:])
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin design delivery")
		return
	}
	defer tx.Rollback(r.Context())
	queries := h.Queries.WithTx(tx)
	// Serialize retries before creating any rows. The unique index also guards
	// alternate writers; the entire comment/document/task commits together.
	lockKey := uuidToString(issue.WorkspaceID) + ":" + actorID + ":" + request.RequestID
	if _, err := tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", lockKey); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reserve design delivery")
		return
	}
	existing, err := queries.GetCommentByDesignRequest(r.Context(), db.GetCommentByDesignRequestParams{
		WorkspaceID: issue.WorkspaceID, AuthorType: actorType, AuthorID: requesterID, RequestID: request.RequestID,
	})
	if err == nil {
		var delivery commentDesignDelivery
		if json.Unmarshal(existing.DesignDelivery, &delivery) != nil || existing.IssueID != issue.ID || delivery.Fingerprint != fingerprint {
			writeError(w, http.StatusConflict, "request_id already belongs to a different design delivery")
			return
		}
		attachments := h.groupAttachments(r, []pgtype.UUID{existing.ID})
		writeJSON(w, http.StatusOK, commentToResponse(existing, nil, attachments[uuidToString(existing.ID)]))
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to load design delivery")
		return
	}
	if !h.projectResourceBelongsToProject(r.Context(), w, issue.WorkspaceID, issue.ProjectID, resourceID) {
		return
	}
	agent, err := queries.GetAgent(r.Context(), agentID)
	if err != nil || agent.WorkspaceID != issue.WorkspaceID || !h.canInvokeAgent(r.Context(), agent, actorType, actorID, actorID, uuidToString(issue.WorkspaceID)) {
		writeError(w, http.StatusForbidden, "selected agent cannot be invoked")
		return
	}
	lookup := h.runtimeLookup(obsmetrics.RuntimeLookupSourceDesign)
	lookup.Queries = queries
	verdict, err := service.AgentReadiness(r.Context(), lookup, agent)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check agent readiness")
		return
	}
	if !verdict.Ready() {
		writeError(w, http.StatusConflict, verdict.Detail)
		return
	}

	var input designDocumentInputSnapshot
	var snapshots []designDocumentAttachmentSnapshot
	var inputJSON []byte
	if request.Operation == "design" {
		input, snapshots, err = h.commentDesignDocumentInput(r, issue, request, req.Content, req.AttachmentIDs)
		if err != nil {
			writeProjectDesignSystemRequestError(w, err)
			return
		}
		inputJSON, err = json.Marshal(input)
		if err != nil || len(inputJSON) > designDocumentMaxSnapshotBytes {
			writeError(w, http.StatusRequestEntityTooLarge, "design inputs exceed the size limit")
			return
		}
	} else {
		var valid bool
		_, valid = h.resolveCommentDesignImplementation(w, r, issue, request, req.Content)
		if !valid {
			return
		}
		// Validate uploaded input ownership before the atomic write, just as the
		// generation path does. Attachment linking itself uses the same query.
		rawAttachments := commentDesignAttachmentInput(req.AttachmentIDs)
		if _, err := h.resolveDesignDocumentAttachments(r.Context(), r, issue.WorkspaceID, rawAttachments); err != nil {
			writeProjectDesignSystemRequestError(w, err)
			return
		}
	}
	created, err := queries.CreateComment(r.Context(), db.CreateCommentParams{
		ID: dbid.NewV7(), IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
		AuthorType: actorType, AuthorID: requesterID, Content: req.Content, Type: req.Type, ParentID: parentID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create delivery comment")
		return
	}
	comment := created.Comment()
	var task db.AgentTaskQueue
	delivery := commentDesignDelivery{
		CommentDesignDeliveryResponse: CommentDesignDeliveryResponse{Operation: request.Operation, AgentID: uuidToString(agentID), ProjectResourceID: uuidToString(resourceID)},
		RequestID:                     request.RequestID, Fingerprint: fingerprint, RequestContent: req.Content,
	}
	if request.Operation == "design" {
		var document db.DesignDocument
		document, task, err = h.enqueueDesignDocumentTask(r.Context(), queries, issue.WorkspaceID, requesterID, issue.ProjectID,
			projectDesignSystemScope{ProjectResourceID: resourceID}, issue.ID, agent.ID,
			designDocumentTitleFromBrief(req.Content, issue.Title), input, inputJSON, snapshots)
		delivery.DocumentID = uuidToString(document.ID)
	} else {
		task, err = queries.CreateAgentTask(r.Context(), db.CreateAgentTaskParams{
			ID: dbid.NewV7(), AgentID: agent.ID, RuntimeID: agent.RuntimeID, IssueID: issue.ID,
			TriggerCommentID: comment.ID, TriggerSummary: pgtype.Text{String: designimplementation.TaskTrigger, Valid: true},
			ForceFreshSession: pgtype.Bool{Bool: true, Valid: true},
			OriginatorUserID:  requesterID, AccountableUserID: requesterID,
			OriginatorSource:    pgtype.Text{String: "direct_human", Valid: true},
			TriggerEvidenceKind: pgtype.Text{String: "comment", Valid: true}, TriggerEvidenceRefID: comment.ID,
		})
		claim, _ := parseDesignAssetRef(request.DesignRef, time.Now())
		if claim.Kind == "multica" {
			delivery.DocumentID = claim.AssetID
		}
	}
	if err != nil {
		writeProjectDesignSystemRequestError(w, err)
		return
	}
	triggerSummary := strings.SplitN(req.Content, "\n", 2)[0]
	if request.Operation == "implement" {
		triggerSummary = designimplementation.TaskTrigger
	}
	task, err = queries.BindDesignDeliveryTask(r.Context(), db.BindDesignDeliveryTaskParams{
		ID: task.ID, IssueID: issue.ID, CommentID: comment.ID, RequesterID: requesterID,
		TriggerSummary: pgtype.Text{String: triggerSummary, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to bind delivery task")
		return
	}
	delivery.TaskID = uuidToString(task.ID)
	rawDelivery, err := json.Marshal(delivery)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode design delivery")
		return
	}
	comment, err = queries.SetCommentDesignDelivery(r.Context(), db.SetCommentDesignDeliveryParams{ID: comment.ID, WorkspaceID: issue.WorkspaceID, DesignDelivery: rawDelivery})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to bind delivery comment")
		return
	}
	if len(req.AttachmentIDs) > 0 {
		ids, ok := parseUUIDSliceOrBadRequest(w, req.AttachmentIDs, "attachment_ids")
		if !ok {
			return
		}
		if _, err := queries.ReplaceCommentAttachments(r.Context(), db.ReplaceCommentAttachmentsParams{CommentID: comment.ID, IssueID: issue.ID, AttachmentIds: ids}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to link delivery attachments")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit design delivery")
		return
	}
	attachments := h.groupAttachments(r, []pgtype.UUID{comment.ID})
	response := commentToResponse(comment, nil, attachments[uuidToString(comment.ID)])
	response.IssueRevision = created.IssueRevision
	h.publish(protocol.EventCommentCreated, uuidToString(issue.WorkspaceID), actorType, actorID, map[string]any{
		"comment": response, "issue_title": issue.Title, "issue_assignee_type": textToPtr(issue.AssigneeType),
		"issue_assignee_id": uuidToPtr(issue.AssigneeID), "issue_status": issue.Status, "issue_revision": created.IssueRevision,
	})
	h.TaskService.AutoUnresolveThreadOnReply(r.Context(), root, uuidToString(issue.WorkspaceID), actorType, actorID)
	h.TaskService.NotifyTaskEnqueued(r.Context(), task)
	writeJSON(w, http.StatusCreated, response)
}

func (h *Handler) commentDesignDocumentInput(r *http.Request, issue db.Issue, request CommentDesignRequest, content string, attachmentIDs []string) (designDocumentInputSnapshot, []designDocumentAttachmentSnapshot, error) {
	input := designDocumentInputSnapshot{
		AgentID: request.AgentID, ProjectResourceID: request.ProjectResourceID, IssueID: uuidToString(issue.ID),
		Platform: "cross_platform", Recipe: "ui-mockup", Brief: content,
		DesignSystemID: request.DesignSystemID, DesignSystemChoice: "none",
		ResolvedDesignContext: &service.ResolvedDesignContext{
			Version: service.DesignContextVersion, ProjectID: uuidToString(issue.ProjectID),
			Source: service.DesignContextSourceNone, Priority: []service.DesignContextSource{service.DesignContextSourceRepositoryReality},
		},
	}
	if request.DesignSystemID != "" {
		id, err := parseDesignAssetClaimUUID(request.DesignSystemID)
		if err != nil {
			return input, nil, err
		}
		resolved, err := (service.ProjectDesignContextResolver{Store: h.Queries, AllowedHosts: h.projectDesignSystemAllowedHosts()}).Resolve(r.Context(), service.ResolveProjectDesignContextParams{
			WorkspaceID: issue.WorkspaceID, ProjectID: issue.ProjectID, DesignSystemID: id,
		})
		if err != nil {
			return input, nil, &projectDesignSystemRequestError{status: http.StatusUnprocessableEntity, code: "design_context_invalid", message: "selected saved design system is unavailable"}
		}
		input.DesignSystemChoice, input.ResolvedDesignContext = "selected", &resolved
	}
	repository, err := h.Queries.GetProjectResourceInWorkspace(r.Context(), db.GetProjectResourceInWorkspaceParams{ID: parseUUID(request.ProjectResourceID), WorkspaceID: issue.WorkspaceID})
	if err != nil {
		return input, nil, err
	}
	workspace, err := h.Queries.GetWorkspace(r.Context(), issue.WorkspaceID)
	if err != nil {
		return input, nil, err
	}
	var source struct {
		URL string `json:"url"`
	}
	var repositories []workspaceRepoRef
	if json.Unmarshal(repository.ResourceRef, &source) != nil || (len(workspace.Repos) > 0 && json.Unmarshal(workspace.Repos, &repositories) != nil) {
		return input, nil, projectDesignSystemInternalError("repository_invalid", "repository settings could not be read")
	}
	for _, candidate := range repositories {
		if strings.TrimSpace(source.URL) == strings.TrimSpace(candidate.URL) {
			if id, err := parseWorkspaceRepositoryUUID(candidate.ID); err == nil {
				input.WorkspaceRepositoryID = uuidToString(id)
				break
			}
		}
	}
	rawAttachments := commentDesignAttachmentInput(attachmentIDs)
	snapshots, attachmentErr := h.resolveDesignDocumentAttachments(r.Context(), r, issue.WorkspaceID, rawAttachments)
	if attachmentErr != nil {
		return input, nil, attachmentErr
	}
	input.Attachments, err = json.Marshal(snapshots)
	return input, snapshots, err
}

func (h *Handler) resolveCommentDesignImplementation(w http.ResponseWriter, r *http.Request, issue db.Issue, request CommentDesignRequest, content string) (DesignImplementationContextResponse, bool) {
	body, _ := json.Marshal(DesignImplementationRequest{RevisionID: request.RevisionID, FrameRefs: request.FrameRefs, ProjectResourceID: request.ProjectResourceID, IssueID: uuidToString(issue.ID)})
	clone := r.Clone(r.Context())
	clone.Body = io.NopCloser(bytes.NewReader(body))
	route := chi.NewRouteContext()
	route.URLParams.Add("designRef", request.DesignRef)
	clone = clone.WithContext(context.WithValue(clone.Context(), chi.RouteCtxKey, route))
	resolved, _, _, ok := h.resolveDesignImplementationRequest(w, clone)
	if !ok {
		return DesignImplementationContextResponse{}, false
	}
	identity, valid := designimplementation.ParseTaskIdentity(content)
	claim, err := parseDesignAssetRef(request.DesignRef, time.Now())
	if !valid || !designimplementation.IsTask(content) || err != nil || !designImplementationRequestMatchesTaskIdentity(request.DesignRef, claim,
		DesignImplementationRequest{RevisionID: request.RevisionID, FrameRefs: request.FrameRefs, ProjectResourceID: request.ProjectResourceID}, identity) {
		writeError(w, http.StatusConflict, "implementation prompt does not match the selected design, revision, frame, and repository")
		return DesignImplementationContextResponse{}, false
	}
	return resolved, true
}

func commentDesignAttachmentInput(ids []string) json.RawMessage {
	inputs := make([]designDocumentAttachmentInput, len(ids))
	for i, id := range ids {
		inputs[i].AttachmentID = id
	}
	raw, _ := json.Marshal(inputs)
	return raw
}
