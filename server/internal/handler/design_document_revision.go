package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/designdocument"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Reading a design document's revisions and previewing their prototypes.
//
// Revisions are immutable and every one of them stays readable, so a user can
// walk back through what each run produced. The prototype itself is served
// file by file from the stored archive through an unauthenticated route: the
// sandboxed iframe that shows it cannot carry the Bearer header, so instead
// the protected revision endpoint issues a short-lived capability bound to
// exactly one workspace, revision and content digest, and the file route only
// answers when that capability checks out.

const (
	designDocumentPreviewSchema              = "multica.design-document-preview/v1"
	designDocumentPreviewAccessTokenVersion  = "v1"
	designDocumentPreviewAccessTokenLifetime = 30 * time.Minute
	designDocumentPreviewRoutePrefix         = "/api/design-document-previews"
)

// DesignDocumentRevisionSummaryResponse is one row of the revision timeline.
type DesignDocumentRevisionSummaryResponse struct {
	ID             string          `json:"id"`
	RevisionNumber int32           `json:"revision_number"`
	ContentDigest  string          `json:"content_digest"`
	BaseRevisionID string          `json:"base_revision_id,omitempty"`
	SourceTaskID   string          `json:"source_task_id"`
	AgentID        string          `json:"agent_id"`
	Instruction    string          `json:"instruction"`
	Scope          json.RawMessage `json:"scope"`
	IsDraft        bool            `json:"is_draft"`
	IsSaved        bool            `json:"is_saved"`
	PageCount      int             `json:"page_count"`
	FlowCount      int             `json:"flow_count"`
	CreatedAt      string          `json:"created_at"`
}

// DesignDocumentRevisionResponse is one revision in full: the agent's brief and
// coverage, the platform's audit and preview receipts, the page/flow index the
// manifest carries, and a capability the client uses to frame the prototype.
type DesignDocumentRevisionResponse struct {
	DesignDocumentRevisionSummaryResponse
	Brief          json.RawMessage `json:"brief"`
	Coverage       json.RawMessage `json:"coverage"`
	Audit          json.RawMessage `json:"audit"`
	PreviewReceipt json.RawMessage `json:"preview_receipt"`
	// Critique is the agent's review loop report (DC-050) when the package
	// carries one, else null. Read from the archive: it is the agent's own
	// document, stored nowhere else, and never a gate.
	Critique       json.RawMessage `json:"critique"`
	PrototypeEntry string          `json:"prototype_entry"`
	// Manifest projections. Never nil so a client can index into them.
	Pages          []designdocument.PageIndexEntry `json:"pages"`
	Flows          []designdocument.FlowIndexEntry `json:"flows"`
	PreviewTargets []designdocument.PreviewTarget  `json:"preview_targets"`
	// Files is the package's artifact index, so the workspace can offer a
	// source view and per-file download over the same capability route the
	// preview frame already uses.
	Files []designdocument.ArtifactIndexEntry `json:"files"`
	// ResourceBasePath is the server-relative prefix under which the archive
	// files are served; append a package path such as prototype/index.html.
	ResourceBasePath        string `json:"resource_base_path"`
	ResourceAccessToken     string `json:"resource_access_token"`
	ResourceAccessExpiresAt string `json:"resource_access_expires_at"`
}

type ListDesignDocumentRevisionsResponse struct {
	Revisions []DesignDocumentRevisionSummaryResponse `json:"revisions"`
}

// ListDesignDocumentRevisions returns every revision of a document, newest
// first, marking which one the draft and saved pointers currently name.
func (h *Handler) ListDesignDocumentRevisions(w http.ResponseWriter, r *http.Request) {
	document, workspaceUUID, ok := h.loadDesignDocumentForRequest(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListDesignDocumentRevisions(r.Context(), db.ListDesignDocumentRevisionsParams{
		WorkspaceID:      workspaceUUID,
		DesignDocumentID: document.ID,
	})
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "revision_list_failed", "failed to list design document revisions")
		return
	}
	revisions := make([]DesignDocumentRevisionSummaryResponse, 0, len(rows))
	for _, row := range rows {
		revisions = append(revisions, designDocumentRevisionSummary(document, row))
	}
	writeJSON(w, http.StatusOK, ListDesignDocumentRevisionsResponse{Revisions: revisions})
}

// GetDesignDocumentRevision returns one revision with a fresh preview
// capability. The revision must belong to the document in the path: a
// revision id from another document is "not found", never a cross-document
// read.
func (h *Handler) GetDesignDocumentRevision(w http.ResponseWriter, r *http.Request) {
	document, workspaceUUID, ok := h.loadDesignDocumentForRequest(w, r)
	if !ok {
		return
	}
	revisionUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "revisionId"), "revision_id")
	if !ok {
		return
	}
	revision, err := h.Queries.GetDesignDocumentRevisionInWorkspace(r.Context(), db.GetDesignDocumentRevisionInWorkspaceParams{
		ID: revisionUUID, WorkspaceID: workspaceUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && revision.DesignDocumentID != document.ID) {
		writeProjectDesignSystemError(w, http.StatusNotFound, "revision_not_found", "design document revision not found")
		return
	}
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "revision_lookup_failed", "failed to load the design document revision")
		return
	}
	var manifest designdocument.Manifest
	if err := json.Unmarshal(revision.Manifest, &manifest); err != nil || manifest.SchemaVersion != designdocument.PackageSchemaV1 {
		writeProjectDesignSystemError(w, http.StatusConflict, "revision_manifest_invalid", "design document revision manifest is invalid")
		return
	}
	accessToken, expiresAt := issueDesignDocumentPreviewAccessToken(
		uuidToString(workspaceUUID), uuidToString(revision.ID), revision.ContentDigest,
	)
	response := DesignDocumentRevisionResponse{
		DesignDocumentRevisionSummaryResponse: designDocumentRevisionSummary(document, revision),
		Brief:                                 jsonOrDefault(revision.Brief, `{}`),
		Coverage:                              jsonOrDefault(revision.Coverage, `{}`),
		Audit:                                 jsonOrDefault(revision.Audit, `{}`),
		PreviewReceipt:                        jsonOrDefault(revision.Preview, `{}`),
		Critique:                              h.loadDesignDocumentRevisionCritique(r.Context(), revision),
		PrototypeEntry:                        manifest.PrototypeEntry,
		Pages:                                 manifest.Pages,
		Flows:                                 manifest.Flows,
		PreviewTargets:                        manifest.PreviewTargets,
		Files:                                 manifest.Files,
		ResourceBasePath: designDocumentPreviewResourceBasePath(
			uuidToString(workspaceUUID), uuidToString(revision.ID), revision.ContentDigest, accessToken,
		),
		ResourceAccessToken:     accessToken,
		ResourceAccessExpiresAt: expiresAt.Format(time.RFC3339),
	}
	if response.Pages == nil {
		response.Pages = []designdocument.PageIndexEntry{}
	}
	if response.Flows == nil {
		response.Flows = []designdocument.FlowIndexEntry{}
	}
	if response.PreviewTargets == nil {
		response.PreviewTargets = []designdocument.PreviewTarget{}
	}
	if response.Files == nil {
		response.Files = []designdocument.ArtifactIndexEntry{}
	}
	writeJSON(w, http.StatusOK, response)
}

// GetDesignDocumentPreviewFile serves one file of a revision's archive to the
// sandboxed preview frame. It is mounted outside the auth group and trusts
// only the capability in the path, which is bound to the workspace, revision
// and content digest it was issued for and expires on its own.
func (h *Handler) GetDesignDocumentPreviewFile(w http.ResponseWriter, r *http.Request) {
	workspaceUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "workspaceId"), "workspace_id")
	if !ok {
		return
	}
	revisionUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "revisionId"), "revision_id")
	if !ok {
		return
	}
	contentDigest := "sha256:" + chi.URLParam(r, "digest")
	if !validNativePackageDigest(contentDigest) || !validateDesignDocumentPreviewAccessToken(
		chi.URLParam(r, "accessToken"), uuidToString(workspaceUUID), uuidToString(revisionUUID), contentDigest, time.Now(),
	) {
		writeDesignDocumentPreviewFileNotFound(w)
		return
	}
	revision, err := h.Queries.GetDesignDocumentRevisionInWorkspace(r.Context(), db.GetDesignDocumentRevisionInWorkspaceParams{
		ID: revisionUUID, WorkspaceID: workspaceUUID,
	})
	if err != nil || revision.ContentDigest != contentDigest || revision.PackageSchema != designdocument.PackageSchemaV1 {
		writeDesignDocumentPreviewFileNotFound(w)
		return
	}
	artifactPath := strings.TrimPrefix(chi.URLParam(r, "*"), "/")
	entry, ok := designDocumentIndexEntry(revision.ArtifactIndex, artifactPath)
	if !ok {
		writeDesignDocumentPreviewFileNotFound(w)
		return
	}
	if h.Storage == nil {
		writeProjectDesignSystemError(w, http.StatusServiceUnavailable, "design_document_preview_storage_unavailable", "design document package storage is unavailable")
		return
	}
	archive, err := readNativeArchiveFromStorage(r.Context(), h.Storage, revision.ArchiveObjectKey)
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "design_document_preview_read_failed", "failed to read the design document package")
		return
	}
	var index []designdocument.ArtifactIndexEntry
	if err := json.Unmarshal(revision.ArtifactIndex, &index); err != nil {
		writeDesignDocumentPreviewFileNotFound(w)
		return
	}
	// ReadArtifact re-validates the whole archive against the index the
	// revision recorded, so a swapped object in storage cannot be served.
	artifact, err := designdocument.ReadArtifact(archive, index, entry.Path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			writeDesignDocumentPreviewFileNotFound(w)
			return
		}
		writeProjectDesignSystemError(w, http.StatusConflict, "design_document_preview_archive_invalid", "design document package archive failed revalidation")
		return
	}
	if strings.HasPrefix(entry.MediaType, "text/html") {
		w.Header().Set("Content-Security-Policy", designDocumentPreviewCSP())
	}
	w.Header().Set("Content-Type", entry.MediaType)
	w.Header().Set("Content-Length", strconv.Itoa(len(artifact)))
	w.Header().Set("Content-Disposition", "inline")
	// The capability already expires; the bytes behind a digest never change,
	// so the frame may cache them for the token's lifetime.
	w.Header().Set("Cache-Control", "private, max-age=1800")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Multica-Design-Document-Digest", revision.ContentDigest)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(artifact)
}

// loadDesignDocumentRevisionCritique returns the package's critique.json when
// the revision's index lists one, re-validating the archive on the way out
// like every other read. Anything missing or unreadable is null: the detail
// must not fail because an optional report could not be loaded.
func (h *Handler) loadDesignDocumentRevisionCritique(ctx context.Context, revision db.DesignDocumentRevision) json.RawMessage {
	const critiquePath = "critique.json"
	entry, ok := designDocumentIndexEntry(revision.ArtifactIndex, critiquePath)
	if !ok || h.Storage == nil {
		return json.RawMessage(`null`)
	}
	var index []designdocument.ArtifactIndexEntry
	if err := json.Unmarshal(revision.ArtifactIndex, &index); err != nil {
		return json.RawMessage(`null`)
	}
	archive, err := readNativeArchiveFromStorage(ctx, h.Storage, revision.ArchiveObjectKey)
	if err != nil {
		return json.RawMessage(`null`)
	}
	raw, err := designdocument.ReadArtifact(archive, index, entry.Path)
	if err != nil || !json.Valid(raw) {
		return json.RawMessage(`null`)
	}
	return json.RawMessage(raw)
}

func designDocumentRevisionSummary(document db.DesignDocument, revision db.DesignDocumentRevision) DesignDocumentRevisionSummaryResponse {
	summary := DesignDocumentRevisionSummaryResponse{
		ID:             uuidToString(revision.ID),
		RevisionNumber: revision.RevisionNumber,
		ContentDigest:  revision.ContentDigest,
		BaseRevisionID: uuidToString(revision.BaseRevisionID),
		SourceTaskID:   uuidToString(revision.SourceTaskID),
		AgentID:        uuidToString(revision.AgentID),
		Instruction:    revision.Instruction.String,
		Scope:          jsonOrDefault(revision.Scope, `null`),
		IsDraft:        document.DraftRevisionID.Valid && document.DraftRevisionID == revision.ID,
		IsSaved:        document.SavedRevisionID.Valid && document.SavedRevisionID == revision.ID,
	}
	var manifest designdocument.Manifest
	if json.Unmarshal(revision.Manifest, &manifest) == nil {
		summary.PageCount = len(manifest.Pages)
		summary.FlowCount = len(manifest.Flows)
	}
	if revision.CreatedAt.Valid {
		summary.CreatedAt = revision.CreatedAt.Time.UTC().Format(time.RFC3339Nano)
	}
	return summary
}

// designDocumentIndexEntry finds the requested path in the revision's own
// artifact index. Only paths the index names are ever read, so the archive is
// never probed for a path the package contract did not accept.
func designDocumentIndexEntry(rawIndex []byte, artifactPath string) (designdocument.ArtifactIndexEntry, bool) {
	if artifactPath == "" || strings.Contains(artifactPath, "..") || strings.HasPrefix(artifactPath, "/") {
		return designdocument.ArtifactIndexEntry{}, false
	}
	var index []designdocument.ArtifactIndexEntry
	if err := json.Unmarshal(rawIndex, &index); err != nil {
		return designdocument.ArtifactIndexEntry{}, false
	}
	for _, entry := range index {
		if entry.Path == artifactPath && entry.MediaType != "" {
			return entry, true
		}
	}
	return designdocument.ArtifactIndexEntry{}, false
}

// designDocumentPreviewCSP is the daemon gate's prototype policy (see
// buildDesignDocumentPreviewCSP) plus the framing rules the browser preview
// needs. 'self' covers package files served beside the document, script
// 'unsafe-inline' covers the inline blocks the package contract allows, and
// connect-src 'none' is what actually enforces "the prototype runs with the
// network off". The sandbox keeps the prototype's scripts on an opaque origin
// even if an embedder forgets the iframe attribute.
//
// frame-ancestors is open for the same reason the recipe covers leave it open:
// the frame is served from the API origin and embedded by the web app, the
// desktop app (a per-worktree dev port, file: when installed) and possibly the
// Figma plugin panel — embedders this server cannot enumerate. What framing
// could take is already fenced by the capability in the URL: only a client
// that fetched the revision holds it, it expires, and the document carries no
// session or action to hijack.
func designDocumentPreviewCSP() string {
	return strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' 'unsafe-inline'",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self'",
		"font-src 'self'",
		"connect-src 'none'",
		"worker-src 'none'",
		"object-src 'none'",
		"frame-src 'none'",
		"form-action 'none'",
		"base-uri 'none'",
		"frame-ancestors *",
		"sandbox allow-scripts",
	}, "; ")
}

func designDocumentPreviewResourceBasePath(workspaceID, revisionID, contentDigest, accessToken string) string {
	return strings.Join([]string{
		designDocumentPreviewRoutePrefix, workspaceID, revisionID,
		strings.TrimPrefix(contentDigest, "sha256:"), accessToken, "files",
	}, "/")
}

func issueDesignDocumentPreviewAccessToken(workspaceID, revisionID, contentDigest string) (string, time.Time) {
	expiresAt := time.Now().UTC().Add(designDocumentPreviewAccessTokenLifetime).Truncate(time.Second)
	expiresUnix := strconv.FormatInt(expiresAt.Unix(), 10)
	signature := signDesignDocumentPreviewAccessToken(workspaceID, revisionID, contentDigest, expiresUnix)
	return strings.Join([]string{designDocumentPreviewAccessTokenVersion, expiresUnix, signature}, "."), expiresAt
}

func validateDesignDocumentPreviewAccessToken(token, workspaceID, revisionID, contentDigest string, now time.Time) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != designDocumentPreviewAccessTokenVersion {
		return false
	}
	expiresUnix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || now.Unix() > expiresUnix {
		return false
	}
	provided, err := hex.DecodeString(parts[2])
	if err != nil || len(provided) != sha256.Size {
		return false
	}
	expected, err := hex.DecodeString(signDesignDocumentPreviewAccessToken(workspaceID, revisionID, contentDigest, parts[1]))
	return err == nil && hmac.Equal(provided, expected)
}

func signDesignDocumentPreviewAccessToken(workspaceID, revisionID, contentDigest, expiresUnix string) string {
	mac := hmac.New(sha256.New, auth.JWTSecret())
	_, _ = mac.Write([]byte(strings.Join([]string{
		designDocumentPreviewSchema, workspaceID, revisionID, contentDigest, expiresUnix,
	}, "\x00")))
	return hex.EncodeToString(mac.Sum(nil))
}

func writeDesignDocumentPreviewFileNotFound(w http.ResponseWriter) {
	writeProjectDesignSystemError(w, http.StatusNotFound, "design_document_preview_file_not_found", "design document preview file is unavailable")
}

// RestoreDesignDocumentRevision makes an earlier revision the draft again. It
// is a pointer move like save and discard: the revision row is immutable and
// already passed Audit and Preview when it was created, so pointing the draft
// back at it never forms a draft that skipped the gate (DC-034). saved does not
// move — the user still has to save the restored draft explicitly.
func (h *Handler) RestoreDesignDocumentRevision(w http.ResponseWriter, r *http.Request) {
	document, workspaceUUID, ok := h.loadDesignDocumentForRequest(w, r)
	if !ok {
		return
	}
	revisionUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "revisionId"), "revision_id")
	if !ok {
		return
	}
	// A running task is about to move the draft itself; restoring underneath
	// it would make the run land on a base the user no longer looks at.
	if h.designDocumentRunIsLive(r.Context(), h.Queries, document) {
		writeProjectDesignSystemError(w, http.StatusConflict, "operation_in_progress", "a design task is still running for this document")
		return
	}
	revision, err := h.Queries.GetDesignDocumentRevisionInWorkspace(r.Context(), db.GetDesignDocumentRevisionInWorkspaceParams{
		ID: revisionUUID, WorkspaceID: workspaceUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && revision.DesignDocumentID != document.ID) {
		writeProjectDesignSystemError(w, http.StatusNotFound, "revision_not_found", "design document revision not found")
		return
	}
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "revision_lookup_failed", "failed to load the design document revision")
		return
	}
	if document.DraftRevisionID.Valid && document.DraftRevisionID == revision.ID {
		writeJSON(w, http.StatusOK, designDocumentResponse(document, nil, h.designDocumentRepositoryGrounded(r.Context(), document)))
		return
	}
	restored, err := h.Queries.SetDesignDocumentDraftRevision(r.Context(), db.SetDesignDocumentDraftRevisionParams{
		ID: document.ID, WorkspaceID: workspaceUUID, DraftRevisionID: revision.ID,
	})
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "restore_failed", "failed to restore the design document revision")
		return
	}
	writeJSON(w, http.StatusOK, designDocumentResponse(restored, nil, h.designDocumentRepositoryGrounded(r.Context(), restored)))
}

// DownloadDesignDocumentRevisionArchive hands the user a revision's package as
// the ZIP the daemon uploaded — Open Design's "download as .zip". The archive
// is re-validated against the binding its own manifest records before a byte
// goes out, so a swapped object in storage is refused rather than served.
func (h *Handler) DownloadDesignDocumentRevisionArchive(w http.ResponseWriter, r *http.Request) {
	document, workspaceUUID, ok := h.loadDesignDocumentForRequest(w, r)
	if !ok {
		return
	}
	revisionUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "revisionId"), "revision_id")
	if !ok {
		return
	}
	revision, err := h.Queries.GetDesignDocumentRevisionInWorkspace(r.Context(), db.GetDesignDocumentRevisionInWorkspaceParams{
		ID: revisionUUID, WorkspaceID: workspaceUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && revision.DesignDocumentID != document.ID) {
		writeProjectDesignSystemError(w, http.StatusNotFound, "revision_not_found", "design document revision not found")
		return
	}
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "revision_lookup_failed", "failed to load the design document revision")
		return
	}
	if h.Storage == nil {
		writeProjectDesignSystemError(w, http.StatusServiceUnavailable, "design_document_archive_storage_unavailable", "design document package storage is unavailable")
		return
	}
	archive, err := readNativeArchiveFromStorage(r.Context(), h.Storage, revision.ArchiveObjectKey)
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "design_document_archive_read_failed", "failed to read the design document package")
		return
	}
	var manifest designdocument.Manifest
	if json.Unmarshal(revision.Manifest, &manifest) != nil {
		writeProjectDesignSystemError(w, http.StatusConflict, "revision_manifest_invalid", "design document revision manifest is invalid")
		return
	}
	validated, err := designdocument.ValidateArchive(archive, manifest.Binding)
	if err != nil || validated.Manifest.ContentDigest != revision.ContentDigest {
		writeProjectDesignSystemError(w, http.StatusConflict, "design_document_archive_invalid", "design document package archive failed revalidation")
		return
	}
	filename := designDocumentArchiveFilename(document.Title, revision.RevisionNumber)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Length", strconv.Itoa(len(archive)))
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"; filename*=UTF-8''`+url.PathEscape(filename))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Multica-Design-Document-Digest", revision.ContentDigest)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(archive)
}

// designDocumentArchiveFilename names the download after the document and the
// revision, with everything a filename should not carry stripped out.
func designDocumentArchiveFilename(title string, revisionNumber int32) string {
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r < 0x20, r == '/', r == '\\', r == ':', r == '*', r == '?', r == '"', r == '<', r == '>', r == '|':
			return -1
		default:
			return r
		}
	}, strings.TrimSpace(title))
	if cleaned == "" {
		cleaned = "design-document"
	}
	if len(cleaned) > 80 {
		cleaned = cleaned[:80]
	}
	return fmt.Sprintf("%s-v%d.zip", cleaned, revisionNumber)
}
