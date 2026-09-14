package handler

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/designcore"
	"github.com/multica-ai/multica/server/internal/designdocument"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	designAssetRefPrefix   = "design_v1_"
	designAssetFramePrefix = "frame_v1_"
	designAssetRefLifetime = 24 * time.Hour
)

var errDesignAssetRefInvalid = errors.New("design asset reference is invalid")

type designAssetRefClaim struct {
	Kind          string `json:"k"`
	WorkspaceID   string `json:"w"`
	ProjectID     string `json:"p"`
	UserID        string `json:"u"`
	AssetID       string `json:"a"`
	RevisionID    string `json:"r"`
	ContentDigest string `json:"d"`
	ExpiresAt     int64  `json:"e"`
}

type designAssetFrameClaim struct {
	WorkspaceID   string `json:"w"`
	ProjectID     string `json:"p"`
	UserID        string `json:"u"`
	AssetID       string `json:"a"`
	RevisionID    string `json:"r"`
	ContentDigest string `json:"d"`
	SelectionKind string `json:"s"`
	SelectionID   string `json:"i"`
	ExpiresAt     int64  `json:"e"`
}

type DesignAssetFrameResponse struct {
	FrameRef                 string   `json:"frame_ref"`
	SelectionKey             string   `json:"selection_key"`
	Title                    string   `json:"title"`
	ThumbnailURL             string   `json:"thumbnail_url,omitempty"`
	Description              string   `json:"description,omitempty"`
	RestorePackGroupFrameIDs []string `json:"-"`
}

type DesignAssetFramesResponse struct {
	DesignRef     string                     `json:"design_ref"`
	RevisionID    string                     `json:"revision_id"`
	ContentDigest string                     `json:"content_digest"`
	Frames        []DesignAssetFrameResponse `json:"frames"`
}

func designAssetRefAEAD(domain string) (cipher.AEAD, error) {
	sum := sha256.Sum256(append([]byte(domain+":"), auth.JWTSecret()...))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func sealDesignAssetRef(prefix, domain string, claim any) (string, error) {
	plaintext, err := json.Marshal(claim)
	if err != nil {
		return "", err
	}
	aead, err := designAssetRefAEAD(domain)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nonce, nonce, plaintext, []byte(prefix))
	return prefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func openDesignAssetRef(raw, prefix, domain string, target any) error {
	if !strings.HasPrefix(raw, prefix) {
		return errDesignAssetRefInvalid
	}
	encoded := strings.TrimPrefix(raw, prefix)
	sealed, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || base64.RawURLEncoding.EncodeToString(sealed) != encoded {
		return errDesignAssetRefInvalid
	}
	aead, err := designAssetRefAEAD(domain)
	if err != nil || len(sealed) < aead.NonceSize() {
		return errDesignAssetRefInvalid
	}
	plaintext, err := aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], []byte(prefix))
	if err != nil || json.Unmarshal(plaintext, target) != nil {
		return errDesignAssetRefInvalid
	}
	return nil
}

func issueDesignAssetRef(claim designAssetRefClaim) (string, error) {
	return sealDesignAssetRef(designAssetRefPrefix, "design-asset-reference/v1", claim)
}

func parseDesignAssetRef(raw string, now time.Time) (designAssetRefClaim, error) {
	var claim designAssetRefClaim
	if err := openDesignAssetRef(raw, designAssetRefPrefix, "design-asset-reference/v1", &claim); err != nil ||
		(claim.Kind != "figma" && claim.Kind != "multica") || claim.WorkspaceID == "" || claim.ProjectID == "" ||
		claim.UserID == "" || claim.AssetID == "" || claim.RevisionID == "" || !validNativePackageDigest(claim.ContentDigest) ||
		claim.ExpiresAt <= now.Unix() {
		return designAssetRefClaim{}, errDesignAssetRefInvalid
	}
	return claim, nil
}

func issueDesignAssetFrameRef(design designAssetRefClaim, selectionKind, selectionID string) (string, error) {
	return sealDesignAssetRef(designAssetFramePrefix, "design-asset-frame-reference/v1", designAssetFrameClaim{
		WorkspaceID: design.WorkspaceID, ProjectID: design.ProjectID, UserID: design.UserID,
		AssetID: design.AssetID, RevisionID: design.RevisionID, ContentDigest: design.ContentDigest,
		SelectionKind: selectionKind, SelectionID: selectionID, ExpiresAt: design.ExpiresAt,
	})
}

func parseDesignAssetFrameRef(raw string, now time.Time) (designAssetFrameClaim, error) {
	var claim designAssetFrameClaim
	if err := openDesignAssetRef(raw, designAssetFramePrefix, "design-asset-frame-reference/v1", &claim); err != nil ||
		claim.WorkspaceID == "" || claim.ProjectID == "" || claim.UserID == "" || claim.AssetID == "" ||
		claim.RevisionID == "" || !validDesignAssetSelectionKind(claim.SelectionKind) || claim.SelectionID == "" ||
		!validNativePackageDigest(claim.ContentDigest) ||
		claim.ExpiresAt <= now.Unix() {
		return designAssetFrameClaim{}, errDesignAssetRefInvalid
	}
	return claim, nil
}

func validDesignAssetSelectionKind(kind string) bool {
	switch kind {
	case "frame", "figma_group", "page":
		return true
	default:
		return false
	}
}

func digestDesignAssetBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func newDesignAssetRef(kind string, workspaceID, projectID, userID, assetID, revisionID, digest string, now time.Time) (string, error) {
	return issueDesignAssetRef(designAssetRefClaim{
		Kind: kind, WorkspaceID: workspaceID, ProjectID: projectID, UserID: userID,
		AssetID: assetID, RevisionID: revisionID, ContentDigest: digest,
		ExpiresAt: now.Add(designAssetRefLifetime).Unix(),
	})
}

func attachFigmaDesignAssetRef(response *DesignFileResponse, file db.DesignFile, revision db.DesignRevision, userID string, now time.Time) error {
	if !file.ProjectID.Valid || revision.Status != "valid" || revision.FileID != file.ID || revision.ID != file.CurrentRevisionID {
		return nil
	}
	ref, err := newDesignAssetRef("figma", uuidToString(file.WorkspaceID), uuidToString(file.ProjectID), userID,
		uuidToString(file.ID), uuidToString(revision.ID), digestDesignAssetBytes(revision.NativeJson), now)
	if err != nil {
		return err
	}
	response.DesignRef = ref
	return nil
}

func (h *Handler) attachMulticaDesignAssetRef(ctx context.Context, response *DesignDocumentResponse, document db.DesignDocument, userID string, now time.Time) error {
	response.Source = "multica"
	if !document.SavedRevisionID.Valid {
		return nil
	}
	revision, err := h.Queries.GetDesignDocumentRevisionInWorkspace(ctx, db.GetDesignDocumentRevisionInWorkspaceParams{
		ID: document.SavedRevisionID, WorkspaceID: document.WorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if revision.DesignDocumentID != document.ID || revision.PackageSchema != designdocument.PackageSchemaV1 || !validNativePackageDigest(revision.ContentDigest) {
		return nil
	}
	ref, err := newDesignAssetRef("multica", uuidToString(document.WorkspaceID), uuidToString(document.ProjectID), userID,
		uuidToString(document.ID), uuidToString(revision.ID), revision.ContentDigest, now)
	if err != nil {
		return err
	}
	response.DesignRef = ref
	return nil
}

func (h *Handler) GetDesignAssetFrames(w http.ResponseWriter, r *http.Request) {
	workspaceID, requesterID, ok := h.projectDesignSystemRequestScope(w, r)
	if !ok {
		return
	}
	rawRef := chi.URLParam(r, "designRef")
	claim, err := parseDesignAssetRef(rawRef, time.Now())
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "design_ref_invalid", "design reference is invalid or expired; select the design again")
		return
	}
	if claim.WorkspaceID != uuidToString(workspaceID) || claim.UserID != uuidToString(requesterID) {
		writeProjectDesignSystemError(w, http.StatusForbidden, "forbidden", "design reference is not available to this user or workspace")
		return
	}

	var frames []DesignAssetFrameResponse
	switch claim.Kind {
	case "figma":
		frames, err = h.resolveFigmaDesignAssetFrames(r, claim)
	case "multica":
		frames, err = h.resolveMulticaDesignAssetFrames(r, claim)
	}
	if err != nil {
		writeDesignAssetResolveError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, DesignAssetFramesResponse{
		DesignRef: rawRef, RevisionID: claim.RevisionID, ContentDigest: claim.ContentDigest, Frames: frames,
	})
}

type designAssetResolveError struct {
	status  int
	code    string
	message string
}

func (e *designAssetResolveError) Error() string { return e.code }

func writeDesignAssetResolveError(w http.ResponseWriter, err error) {
	var resolveErr *designAssetResolveError
	if errors.As(err, &resolveErr) {
		writeProjectDesignSystemError(w, resolveErr.status, resolveErr.code, resolveErr.message)
		return
	}
	writeProjectDesignSystemError(w, http.StatusInternalServerError, "design_lookup_failed", "failed to load design frames; retry the request")
}

func designAssetResolveFailure(status int, code, message string) error {
	return &designAssetResolveError{status: status, code: code, message: message}
}

func (h *Handler) resolveFigmaDesignAssetFrames(r *http.Request, claim designAssetRefClaim) ([]DesignAssetFrameResponse, error) {
	workspaceID, err := parseDesignAssetClaimUUID(claim.WorkspaceID)
	if err != nil {
		return nil, err
	}
	assetID, err := parseDesignAssetClaimUUID(claim.AssetID)
	if err != nil {
		return nil, err
	}
	revisionID, err := parseDesignAssetClaimUUID(claim.RevisionID)
	if err != nil {
		return nil, err
	}
	file, err := h.Queries.GetDesignFileInWorkspace(r.Context(), db.GetDesignFileInWorkspaceParams{ID: assetID, WorkspaceID: workspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, designAssetResolveFailure(http.StatusNotFound, "design_not_found", "design is no longer available; select another design")
	}
	if err != nil {
		return nil, err
	}
	if uuidToString(file.ProjectID) != claim.ProjectID {
		return nil, designAssetResolveFailure(http.StatusConflict, "project_mismatch", "design no longer belongs to the selected project")
	}
	if !file.CurrentRevisionID.Valid || uuidToString(file.CurrentRevisionID) != claim.RevisionID {
		return nil, designAssetResolveFailure(http.StatusConflict, "revision_not_restorable", "design revision is stale; select the latest valid revision")
	}
	revision, err := h.Queries.GetDesignRevisionInWorkspace(r.Context(), db.GetDesignRevisionInWorkspaceParams{ID: revisionID, WorkspaceID: workspaceID})
	if err != nil || revision.FileID != file.ID || revision.Status != "valid" || digestDesignAssetBytes(revision.NativeJson) != claim.ContentDigest {
		return nil, designAssetResolveFailure(http.StatusConflict, "revision_not_restorable", "design revision is unavailable or no longer valid")
	}
	document, err := designcore.ParseNativeJSON(revision.NativeJson)
	if err != nil {
		return nil, designAssetResolveFailure(http.StatusConflict, "revision_not_restorable", "design revision content is invalid")
	}
	var rawDocument map[string]any
	if json.Unmarshal(revision.NativeJson, &rawDocument) != nil {
		return nil, designAssetResolveFailure(http.StatusConflict, "revision_not_restorable", "design revision content is invalid")
	}
	frames := make([]DesignAssetFrameResponse, 0, len(document.Frames))
	for _, frame := range document.Frames {
		frameRef, err := issueDesignAssetFrameRef(claim, "frame", frame.ID)
		if err != nil {
			return nil, err
		}
		frames = append(frames, DesignAssetFrameResponse{
			FrameRef: frameRef, SelectionKey: designAssetSelectionKey(claim, "frame", frame.ID), Title: frame.Name, ThumbnailURL: designAssetFrameThumbnail(document, frame),
		})
	}
	groups, err := figmaGroupDesignAssetFrames(document, rawDocument, claim)
	if err != nil {
		return nil, err
	}
	frames = append(frames, groups...)
	return frames, nil
}

func figmaGroupDesignAssetFrames(document designcore.NativeJSON, rawDocument map[string]any, claim designAssetRefClaim) ([]DesignAssetFrameResponse, error) {
	framesByID := make(map[string]designcore.Frame, len(document.Frames))
	for _, frame := range document.Frames {
		framesByID[frame.ID] = frame
	}
	discovered := discoverDesignRestorePackGroups(rawDocument)
	groups := make([]DesignAssetFrameResponse, 0, len(discovered))
	for _, group := range discovered {
		title := group.Name
		if title == "" {
			title = group.ID
		}
		thumbnail := ""
		for _, frameID := range group.FrameIDs {
			if frame, exists := framesByID[frameID]; exists {
				thumbnail = designAssetFrameThumbnail(document, frame)
				break
			}
		}
		frameRef, err := issueDesignAssetFrameRef(claim, "figma_group", group.ID)
		if err != nil {
			return nil, err
		}
		groups = append(groups, DesignAssetFrameResponse{FrameRef: frameRef, SelectionKey: designAssetSelectionKey(claim, "figma_group", group.ID), Title: title, ThumbnailURL: thumbnail, RestorePackGroupFrameIDs: append([]string(nil), group.FrameIDs...)})
	}
	return groups, nil
}

func designAssetFrameThumbnail(document designcore.NativeJSON, frame designcore.Frame) string {
	for _, assetID := range []string{frame.ThumbnailAssetID, frame.PreviewAssetID} {
		if asset, ok := document.Assets[assetID]; ok && asset.URL != "" {
			return asset.URL
		}
	}
	if frame.ThumbnailURL != "" {
		return frame.ThumbnailURL
	}
	return frame.ThumbnailDataURL
}

func (h *Handler) resolveMulticaDesignAssetFrames(r *http.Request, claim designAssetRefClaim) ([]DesignAssetFrameResponse, error) {
	workspaceID, err := parseDesignAssetClaimUUID(claim.WorkspaceID)
	if err != nil {
		return nil, err
	}
	assetID, err := parseDesignAssetClaimUUID(claim.AssetID)
	if err != nil {
		return nil, err
	}
	revisionID, err := parseDesignAssetClaimUUID(claim.RevisionID)
	if err != nil {
		return nil, err
	}
	document, err := h.Queries.GetDesignDocumentInWorkspace(r.Context(), db.GetDesignDocumentInWorkspaceParams{ID: assetID, WorkspaceID: workspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, designAssetResolveFailure(http.StatusNotFound, "design_not_found", "design is no longer available; select another design")
	}
	if err != nil {
		return nil, err
	}
	if uuidToString(document.ProjectID) != claim.ProjectID {
		return nil, designAssetResolveFailure(http.StatusConflict, "project_mismatch", "design no longer belongs to the selected project")
	}
	if !document.SavedRevisionID.Valid || uuidToString(document.SavedRevisionID) != claim.RevisionID {
		return nil, designAssetResolveFailure(http.StatusConflict, "revision_not_restorable", "only the current saved design revision can be restored")
	}
	revision, err := h.Queries.GetDesignDocumentRevisionInWorkspace(r.Context(), db.GetDesignDocumentRevisionInWorkspaceParams{ID: revisionID, WorkspaceID: workspaceID})
	if err != nil || revision.DesignDocumentID != document.ID || revision.PackageSchema != designdocument.PackageSchemaV1 || revision.ContentDigest != claim.ContentDigest {
		return nil, designAssetResolveFailure(http.StatusConflict, "revision_not_restorable", "saved design revision is unavailable or invalid")
	}
	var manifest designdocument.Manifest
	if json.Unmarshal(revision.Manifest, &manifest) != nil || manifest.SchemaVersion != designdocument.PackageSchemaV1 ||
		manifest.ContentDigest != claim.ContentDigest || manifest.Binding.WorkspaceID != claim.WorkspaceID ||
		manifest.Binding.ProjectID != claim.ProjectID || manifest.Binding.DesignDocumentID != claim.AssetID ||
		!revision.SourceTaskID.Valid || manifest.Binding.TaskID != uuidToString(revision.SourceTaskID) {
		return nil, designAssetResolveFailure(http.StatusConflict, "design_package_invalid", "saved design package is invalid; save a valid revision and retry")
	}
	frames := make([]DesignAssetFrameResponse, 0, len(manifest.Pages))
	for _, page := range manifest.Pages {
		frameRef, err := issueDesignAssetFrameRef(claim, "page", page.ID)
		if err != nil {
			return nil, err
		}
		frames = append(frames, DesignAssetFrameResponse{FrameRef: frameRef, SelectionKey: designAssetSelectionKey(claim, "page", page.ID), Title: page.Title})
	}
	return frames, nil
}

func designAssetSelectionKey(claim designAssetRefClaim, kind, id string) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{claim.AssetID, claim.RevisionID, claim.ContentDigest, kind, id}, "\x00")))
	return "selection_v1_" + hex.EncodeToString(digest[:])
}

func parseDesignAssetClaimUUID(raw string) (pgtype.UUID, error) {
	parsed, err := util.ParseUUID(raw)
	if err != nil {
		return pgtype.UUID{}, designAssetResolveFailure(http.StatusBadRequest, "design_ref_invalid", "design reference is invalid; select the design again")
	}
	return parsed, nil
}
