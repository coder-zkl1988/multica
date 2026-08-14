package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/designdocument"
	"github.com/multica-ai/multica/server/internal/designpreview"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	designDocumentPreviewSchema = "multica.design-document-preview/v1"
	designDocumentPreviewCSP    = "default-src 'self' data:; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'; sandbox allow-scripts"
)

type designDocumentSummary struct {
	ID              string `json:"id"`
	ProjectID       string `json:"project_id"`
	IssueID         string `json:"issue_id,omitempty"`
	Title           string `json:"title"`
	DraftRevisionID string `json:"draft_revision_id,omitempty"`
	SavedRevisionID string `json:"saved_revision_id,omitempty"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type designDocumentPreviewResponse struct {
	Schema                  string                         `json:"schema"`
	DocumentID              string                         `json:"document_id"`
	RevisionID              string                         `json:"revision_id"`
	ContentDigest           string                         `json:"content_digest"`
	ResourceBaseURL         string                         `json:"resource_base_url"`
	ResourceAccessToken     string                         `json:"resource_access_token"`
	ResourceAccessExpiresAt string                         `json:"resource_access_expires_at"`
	Targets                 []designdocument.PreviewTarget `json:"targets"`
	Preview                 designpreview.Receipt          `json:"preview"`
}

type loadedDesignDocumentPreview struct {
	Document db.DesignDocument
	Revision db.DesignDocumentRevision
	Snapshot db.DesignDocumentInputSnapshot
	Binding  designdocument.Binding
	Package  designdocument.ValidatedPackage
	Archive  []byte
	Receipt  DesignDocumentPackageReceipt
}

func (h *Handler) ListDesignDocuments(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := h.projectDesignSystemRequestScope(w, r)
	if !ok {
		return
	}
	projectID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(r.URL.Query().Get("project_id")), "project_id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: workspaceID}); err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	rows, err := h.Queries.ListDesignDocumentsInProject(r.Context(), db.ListDesignDocumentsInProjectParams{WorkspaceID: workspaceID, ProjectID: projectID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list Design Documents")
		return
	}
	documents := make([]designDocumentSummary, 0, len(rows))
	for _, row := range rows {
		documents = append(documents, designDocumentSummary{
			ID: uuidToString(row.ID), ProjectID: uuidToString(row.ProjectID), IssueID: uuidToString(row.IssueID), Title: row.Title,
			DraftRevisionID: uuidToString(row.DraftRevisionID), SavedRevisionID: uuidToString(row.SavedRevisionID),
			CreatedAt: row.CreatedAt.Time.Format(time.RFC3339), UpdatedAt: row.UpdatedAt.Time.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"documents": documents})
}

func (h *Handler) GetDesignDocumentPreview(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := h.projectDesignSystemRequestScope(w, r)
	if !ok {
		return
	}
	projectID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(r.URL.Query().Get("project_id")), "project_id")
	if !ok {
		return
	}
	documentID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "documentId"), "design_document_id")
	if !ok {
		return
	}
	loaded, err := h.loadDesignDocumentPreview(r.Context(), workspaceID, projectID, documentID)
	if err != nil {
		writeError(w, http.StatusConflict, "Design Document preview is unavailable")
		return
	}
	workspace, project, document, revision := uuidToString(workspaceID), uuidToString(projectID), uuidToString(documentID), uuidToString(loaded.Revision.ID)
	token, expiresAt := issueOpenDesignArchivePreviewAccessToken(workspace, document+"/"+revision, loaded.Revision.ContentDigest)
	digest := strings.TrimPrefix(loaded.Revision.ContentDigest, "sha256:")
	baseURL := "/api/design-document-previews/" + workspace + "/" + project + "/" + document + "/" + revision + "/" + digest + "/" + token + "/files/"
	writeJSON(w, http.StatusOK, designDocumentPreviewResponse{
		Schema: designDocumentPreviewSchema, DocumentID: document, RevisionID: revision, ContentDigest: loaded.Revision.ContentDigest,
		ResourceBaseURL: baseURL, ResourceAccessToken: token, ResourceAccessExpiresAt: expiresAt.Format(time.RFC3339), Targets: loaded.Package.Manifest.PreviewTargets,
		Preview: loaded.Receipt.Preview,
	})
}

func (h *Handler) GetDesignDocumentPreviewFile(w http.ResponseWriter, r *http.Request) {
	workspaceID, workspaceOK := parseUUIDValue(chi.URLParam(r, "workspaceId"))
	projectID, projectOK := parseUUIDValue(chi.URLParam(r, "projectId"))
	documentID, documentOK := parseUUIDValue(chi.URLParam(r, "documentId"))
	revisionID, revisionOK := parseUUIDValue(chi.URLParam(r, "revisionId"))
	digest := "sha256:" + chi.URLParam(r, "digest")
	resourceID := chi.URLParam(r, "documentId") + "/" + chi.URLParam(r, "revisionId")
	if workspaceOK != nil || projectOK != nil || documentOK != nil || revisionOK != nil || !validNativePackageDigest(digest) ||
		!validateOpenDesignArchivePreviewAccessToken(chi.URLParam(r, "accessToken"), chi.URLParam(r, "workspaceId"), resourceID, digest, time.Now()) {
		http.NotFound(w, r)
		return
	}
	loaded, err := h.loadDesignDocumentPreview(r.Context(), workspaceID, projectID, documentID)
	if err != nil || loaded.Revision.ID != revisionID || loaded.Revision.ContentDigest != digest {
		http.NotFound(w, r)
		return
	}
	artifactPath := strings.TrimPrefix(chi.URLParam(r, "*"), "/")
	body, entry, err := designdocument.ReadArchiveFile(loaded.Archive, loaded.Binding, artifactPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", entry.MediaType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", designDocumentPreviewCSP)
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (h *Handler) loadDesignDocumentPreview(ctx context.Context, workspaceID, projectID, documentID pgtype.UUID) (loadedDesignDocumentPreview, error) {
	if h.Storage == nil {
		return loadedDesignDocumentPreview{}, errors.New("Design Document storage is unavailable")
	}
	document, err := h.Queries.GetDesignDocumentInProject(ctx, db.GetDesignDocumentInProjectParams{ID: documentID, WorkspaceID: workspaceID, ProjectID: projectID})
	if err != nil || !document.DraftRevisionID.Valid {
		return loadedDesignDocumentPreview{}, errors.New("draft document not found")
	}
	revision, err := h.Queries.GetDesignDocumentRevisionInProject(ctx, db.GetDesignDocumentRevisionInProjectParams{ID: document.DraftRevisionID, WorkspaceID: workspaceID, ProjectID: projectID})
	if err != nil {
		return loadedDesignDocumentPreview{}, err
	}
	snapshot, err := h.Queries.GetDesignDocumentInputSnapshotInProject(ctx, db.GetDesignDocumentInputSnapshotInProjectParams{ID: revision.InputSnapshotID, WorkspaceID: workspaceID, ProjectID: projectID})
	if err != nil {
		return loadedDesignDocumentPreview{}, err
	}
	task, err := h.Queries.GetAgentTask(ctx, revision.SourceTaskID)
	if err != nil {
		return loadedDesignDocumentPreview{}, err
	}
	var result struct {
		Package *DesignDocumentPackageReceipt `json:"design_document_package"`
	}
	if json.Unmarshal(task.Result, &result) != nil || result.Package == nil {
		return loadedDesignDocumentPreview{}, errors.New("source task has no Design Document receipt")
	}
	receipt := result.Package
	groundingRaw, _ := json.Marshal(receipt.Grounding)
	_, groundedDigest, err := designdocument.SnapshotWithRepositoryGrounding(snapshot.Snapshot, groundingRaw)
	if err != nil || groundedDigest != snapshot.SnapshotSha256 || receipt.InputSnapshotSHA256 != snapshot.SnapshotSha256 {
		return loadedDesignDocumentPreview{}, errors.New("snapshot evidence conflict")
	}
	snapshotParams := designDocumentSnapshotParams{
		WorkspaceID: snapshot.WorkspaceID, ProjectID: snapshot.ProjectID, IssueID: snapshot.IssueID, TaskID: snapshot.TaskID, AgentID: snapshot.AgentID,
		TargetPlatform: snapshot.TargetPlatform, SchemaVersion: snapshot.SchemaVersion, Snapshot: snapshot.Snapshot,
		BaseRevisionID: snapshot.BaseRevisionID, BaseContentDigest: snapshot.BaseContentDigest.String,
		DesignSystemID: snapshot.DesignSystemID, DesignSystemSourceTaskID: snapshot.DesignSystemSourceTaskID, DesignSystemContentDigest: snapshot.DesignSystemContentDigest.String,
	}
	binding := designDocumentPersistenceBinding(designDocumentFirstRevisionParams{DocumentID: document.ID, RevisionID: revision.ID, Snapshot: snapshotParams}, snapshot.SnapshotSha256)
	archive, validated, err := designdocument.LoadArchive(ctx, h.Storage, revision.ArchiveObjectKey, binding)
	if err != nil || revision.ContentDigest != validated.Manifest.ContentDigest || receipt.ContentDigest != revision.ContentDigest || receipt.ObjectKey != revision.ArchiveObjectKey {
		return loadedDesignDocumentPreview{}, errors.New("archive evidence conflict")
	}
	manifest, _ := json.Marshal(validated.Manifest)
	index, _ := json.Marshal(validated.Manifest.Files)
	if !jsonValuesEqual(revision.Manifest, manifest) || !jsonValuesEqual(revision.ArtifactIndex, index) || receipt.DocumentID != uuidToString(document.ID) || receipt.RevisionID != uuidToString(revision.ID) {
		return loadedDesignDocumentPreview{}, errors.New("revision evidence conflict")
	}
	required := make(map[string]bool, len(validated.Coverage.Interactions))
	targets := make([]designpreview.Target, len(validated.Manifest.PreviewTargets))
	for _, interaction := range validated.Coverage.Interactions {
		required[interaction.TargetID] = true
	}
	for i, target := range validated.Manifest.PreviewTargets {
		targets[i] = designpreview.Target{Kind: "preview", ID: target.ID, Path: target.Path}
	}
	if !receipt.Audit.Passed || !reflect.DeepEqual(receipt.ArtifactIndex, validated.Manifest.Files) || !reflect.DeepEqual(receipt.Audit, validated.Audit) ||
		!receipt.Preview.Verification.Passed || designpreview.ValidateReceiptWithInteractions(receipt.Preview, revision.ContentDigest, targets, required) != nil {
		return loadedDesignDocumentPreview{}, errors.New("validation evidence conflict")
	}
	return loadedDesignDocumentPreview{Document: document, Revision: revision, Snapshot: snapshot, Binding: binding, Package: validated, Archive: archive, Receipt: *receipt}, nil
}
