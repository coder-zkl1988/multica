package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/designdocument"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Durable share links for a saved design document revision (DC-062 item 5).
//
// The link and the bytes travel on separate trust paths. The link itself is a
// raw random token that never expires; only revocation kills it. The public
// exchange hands each visitor a fresh, short-lived preview capability, so a
// leaked token still only ever buys one visit's worth of archive access. The
// exchange answers every dead link — revoked, unknown, orphaned by a deleted
// document, or pointing at a revision whose manifest no longer parses — with
// the same 404, so a visitor learns nothing about which case they hit.

// DesignDocumentShareResponse is one live share as the document's own members
// see it. Token is returned raw (never hashed) because creating is
// idempotent: re-copying the link must show the value the creator already
// holds.
type DesignDocumentShareResponse struct {
	ShareID       string  `json:"share_id"`
	Token         string  `json:"token"`
	URL           string  `json:"url"`
	RevisionID    string  `json:"revision_id"`
	DocumentID    string  `json:"document_id"`
	DocumentTitle string  `json:"document_title"`
	CreatedAt     string  `json:"created_at"`
	RevokedAt     *string `json:"revoked_at"`
}

type ListDesignDocumentSharesResponse struct {
	Shares []DesignDocumentShareResponse `json:"shares"`
}

// DesignDocumentShareExchangeResponse is the public face of a link. It stays
// minimal on purpose: a title for the tab, the page index and entry the frame
// needs to boot, and a capability scoped to this visit.
type DesignDocumentShareExchangeResponse struct {
	DocumentTitle string `json:"document_title"`
	// Pages is the manifest page projection; Slides is the deck projection and
	// stays empty for a non-deck package.
	Pages                   []designdocument.PageIndexEntry  `json:"pages"`
	Slides                  []designdocument.SlideIndexEntry `json:"slides"`
	SlideCount              int                              `json:"slide_count"`
	PrototypeEntry          string                           `json:"prototype_entry"`
	ResourceBasePath        string                           `json:"resource_base_path"`
	ResourceAccessToken     string                           `json:"resource_access_token"`
	ResourceAccessExpiresAt string                           `json:"resource_access_expires_at"`
}

func designDocumentShareResponse(document db.DesignDocument, share db.DesignDocumentShare) DesignDocumentShareResponse {
	return DesignDocumentShareResponse{
		ShareID:       uuidToString(share.ID),
		Token:         share.Token,
		URL:           pluginAuthorizeBaseURL() + "/shares/" + share.Token,
		RevisionID:    uuidToString(share.RevisionID),
		DocumentID:    uuidToString(share.DesignDocumentID),
		DocumentTitle: document.Title,
		CreatedAt:     timestampToString(share.CreatedAt),
		RevokedAt:     nil,
	}
}

// loadShareableRevision resolves the revision in the path and applies the two
// gates every shareable revision must pass: it belongs to the document in the
// path, and it is the revision the document has saved. A draft is not a promise
// (P-011 / DC-034), so it can never be handed to an outside visitor.
func (h *Handler) loadShareableRevision(w http.ResponseWriter, r *http.Request, document db.DesignDocument, workspaceUUID pgtype.UUID) (db.DesignDocumentRevision, bool) {
	revisionUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "revisionId"), "revision_id")
	if !ok {
		return db.DesignDocumentRevision{}, false
	}
	revision, err := h.Queries.GetDesignDocumentRevisionInWorkspace(r.Context(), db.GetDesignDocumentRevisionInWorkspaceParams{
		ID: revisionUUID, WorkspaceID: workspaceUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && revision.DesignDocumentID != document.ID) {
		writeProjectDesignSystemError(w, http.StatusNotFound, "revision_not_found", "design document revision not found")
		return db.DesignDocumentRevision{}, false
	}
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "revision_lookup_failed", "failed to load the design document revision")
		return db.DesignDocumentRevision{}, false
	}
	// The question is whether this revision was saved, not whether it is the
	// current draft — those come apart the moment an unsaved draft is adjusted.
	// The adjustment becomes the draft, the old revision drops into history
	// with saved_revision_id still null, and asking "is this the draft?" then
	// answers no and hands unpublished work a permanent anonymous link.
	//
	// Saving keeps the pointer (the draft becomes the saved revision), and
	// nothing records which revisions were saved in the past, so
	// saved_revision_id is the whole answer: the document promises exactly one
	// revision at a time, and that is the one a visitor may see.
	if !document.SavedRevisionID.Valid || document.SavedRevisionID != revisionUUID {
		writeProjectDesignSystemError(w, http.StatusConflict, "share_draft_revision", "only a saved revision can be shared; save it first")
		return db.DesignDocumentRevision{}, false
	}
	var manifest designdocument.Manifest
	if err := json.Unmarshal(revision.Manifest, &manifest); err != nil || manifest.SchemaVersion != designdocument.PackageSchemaV1 {
		writeProjectDesignSystemError(w, http.StatusConflict, "revision_manifest_invalid", "design document revision manifest is invalid")
		return db.DesignDocumentRevision{}, false
	}
	return revision, true
}

// CreateDesignDocumentRevisionShare mints the document's live link for one
// revision — or returns the one that already exists. A revision keeps at most
// one live link, so creating twice hands back the same URL instead of piling
// up duplicates.
func (h *Handler) CreateDesignDocumentRevisionShare(w http.ResponseWriter, r *http.Request) {
	document, workspaceUUID, requesterUUID, ok := h.loadDesignDocumentForRequester(w, r)
	if !ok {
		return
	}
	revision, ok := h.loadShareableRevision(w, r, document, workspaceUUID)
	if !ok {
		return
	}
	existing, err := h.Queries.GetLiveDesignDocumentShareByRevision(r.Context(), db.GetLiveDesignDocumentShareByRevisionParams{
		WorkspaceID: workspaceUUID, DesignDocumentID: document.ID, RevisionID: revision.ID,
	})
	if err == nil {
		writeJSON(w, http.StatusOK, designDocumentShareResponse(document, existing))
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "share_lookup_failed", "failed to look up the design document share")
		return
	}
	token, err := randomHexToken("mds_", 32)
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "share_token_failed", "failed to generate the share token")
		return
	}
	created, err := h.Queries.CreateDesignDocumentShare(r.Context(), db.CreateDesignDocumentShareParams{
		WorkspaceID: workspaceUUID, DesignDocumentID: document.ID,
		RevisionID: revision.ID, Token: token, CreatedBy: requesterUUID,
	})
	if err != nil {
		// Two members racing to mint the same revision's link: the partial
		// unique index rejects the second insert, which resolves to the first
		// winner's link.
		if isUniqueViolation(err) {
			if winner, readErr := h.Queries.GetLiveDesignDocumentShareByRevision(r.Context(), db.GetLiveDesignDocumentShareByRevisionParams{
				WorkspaceID: workspaceUUID, DesignDocumentID: document.ID, RevisionID: revision.ID,
			}); readErr == nil {
				writeJSON(w, http.StatusOK, designDocumentShareResponse(document, winner))
				return
			}
		}
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "share_create_failed", "failed to create the design document share")
		return
	}
	writeJSON(w, http.StatusCreated, designDocumentShareResponse(document, created))
}

// ListDesignDocumentShares returns the document's live links, newest first.
// Revoked links are absent, not marked: revocation is a delete the list
// simply reflects.
func (h *Handler) ListDesignDocumentShares(w http.ResponseWriter, r *http.Request) {
	document, workspaceUUID, ok := h.loadDesignDocumentForRequest(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListDesignDocumentShares(r.Context(), db.ListDesignDocumentSharesParams{
		WorkspaceID: workspaceUUID, DesignDocumentID: document.ID,
	})
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "share_list_failed", "failed to list design document shares")
		return
	}
	shares := make([]DesignDocumentShareResponse, 0, len(rows))
	for _, row := range rows {
		shares = append(shares, designDocumentShareResponse(document, row))
	}
	writeJSON(w, http.StatusOK, ListDesignDocumentSharesResponse{Shares: shares})
}

// RevokeDesignDocumentShare is the only way a link dies. Revoking twice, or
// revoking another document's share id, reads as not found.
func (h *Handler) RevokeDesignDocumentShare(w http.ResponseWriter, r *http.Request) {
	document, workspaceUUID, ok := h.loadDesignDocumentForRequest(w, r)
	if !ok {
		return
	}
	shareUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "shareId"), "share_id")
	if !ok {
		return
	}
	_, err := h.Queries.RevokeDesignDocumentShare(r.Context(), db.RevokeDesignDocumentShareParams{
		ID: shareUUID, WorkspaceID: workspaceUUID, DesignDocumentID: document.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeProjectDesignSystemError(w, http.StatusNotFound, "share_not_found", "design document share not found")
		return
	}
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "share_revoke_failed", "failed to revoke the design document share")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetDesignDocumentShareExchange is the public face of a link: no session, no
// workspace context, the token alone decides. Every dead link — unknown,
// revoked, orphaned by a deleted document, or carrying an unparsable
// manifest — answers with the same 404 so the visitor cannot tell which.
func (h *Handler) GetDesignDocumentShareExchange(w http.ResponseWriter, r *http.Request) {
	writeShareExchangeError := func() {
		writeProjectDesignSystemError(w, http.StatusNotFound, "share_not_found", "design document share not found")
	}
	share, err := h.Queries.GetLiveDesignDocumentShareByToken(r.Context(), chi.URLParam(r, "token"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeShareExchangeError()
		return
	}
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "share_lookup_failed", "failed to load the design document share")
		return
	}
	document, err := h.Queries.GetDesignDocumentInWorkspace(r.Context(), db.GetDesignDocumentInWorkspaceParams{
		ID: share.DesignDocumentID, WorkspaceID: share.WorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeShareExchangeError()
		return
	}
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "share_lookup_failed", "failed to load the shared design document")
		return
	}
	revision, err := h.Queries.GetDesignDocumentRevisionInWorkspace(r.Context(), db.GetDesignDocumentRevisionInWorkspaceParams{
		ID: share.RevisionID, WorkspaceID: share.WorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && revision.DesignDocumentID != document.ID) {
		writeShareExchangeError()
		return
	}
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "share_lookup_failed", "failed to load the shared revision")
		return
	}
	var manifest designdocument.Manifest
	if err := json.Unmarshal(revision.Manifest, &manifest); err != nil || manifest.SchemaVersion != designdocument.PackageSchemaV1 {
		writeShareExchangeError()
		return
	}
	accessToken, expiresAt := issueDesignDocumentPreviewAccessToken(
		uuidToString(share.WorkspaceID), uuidToString(revision.ID), revision.ContentDigest,
	)
	pages := manifest.Pages
	if pages == nil {
		pages = []designdocument.PageIndexEntry{}
	}
	slides := manifest.Slides
	if slides == nil {
		slides = []designdocument.SlideIndexEntry{}
	}
	// The capability is minted per visit and expires on its own; a cached
	// exchange body would hand later visitors a stale, already-expired token.
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, DesignDocumentShareExchangeResponse{
		DocumentTitle:  document.Title,
		Pages:          pages,
		Slides:         slides,
		SlideCount:     len(slides),
		PrototypeEntry: manifest.PrototypeEntry,
		ResourceBasePath: designDocumentPreviewResourceBasePath(
			uuidToString(share.WorkspaceID), uuidToString(revision.ID), revision.ContentDigest, accessToken,
		),
		ResourceAccessToken:     accessToken,
		ResourceAccessExpiresAt: expiresAt.Format(time.RFC3339),
	})
}
