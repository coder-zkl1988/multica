package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/opendesign"
	"github.com/multica-ai/multica/server/internal/projectdesignsystem"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const nativePackagePreviewSchema = "multica.project-design-system-package-preview/v1"

type projectDesignSystemPackagePreviewResponse struct {
	Schema                  string                              `json:"schema"`
	Slot                    string                              `json:"slot"`
	ContentDigest           string                              `json:"content_digest"`
	ResourceAccessToken     string                              `json:"resource_access_token"`
	ResourceAccessExpiresAt string                              `json:"resource_access_expires_at"`
	Targets                 []projectdesignsystem.PreviewTarget `json:"targets"`
}

func (h *Handler) GetProjectDesignSystemPackagePreview(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := h.projectDesignSystemRequestScope(w, r)
	if !ok {
		return
	}
	system, ok := h.loadProjectDesignSystemForRequest(w, r, workspaceID)
	if !ok {
		return
	}
	selected, err := h.loadProjectDesignSystemPreviewPackage(r.Context(), system)
	if err != nil {
		writeProjectDesignSystemRequestError(w, err)
		return
	}

	if selected.PackageSchema == projectdesignsystem.PackageSchemaV2 {
		manifest, _, err := h.loadNativeProjectDesignSystemPackageArchive(r.Context(), system, selected)
		if err != nil {
			writeProjectDesignSystemError(w, http.StatusConflict, "native_package_preview_unavailable", "native design package preview is unavailable")
			return
		}
		accessToken, expiresAt := issueOpenDesignArchivePreviewAccessToken(uuidToString(system.WorkspaceID), uuidToString(system.ID), manifest.ContentDigest)
		writeJSON(w, http.StatusOK, projectDesignSystemPackagePreviewResponse{
			Schema: nativePackagePreviewSchema, Slot: selected.Slot, ContentDigest: manifest.ContentDigest,
			ResourceAccessToken: accessToken, ResourceAccessExpiresAt: expiresAt.Format(time.RFC3339), Targets: manifest.PreviewTargets,
		})
		return
	}
	if isOpenDesignProjectDesignSystemPackage(selected) {
		loaded, err := h.loadOpenDesignArchivePreviewPackage(r.Context(), h.Queries, system, selected)
		if err != nil {
			writeProjectDesignSystemRequestError(w, err)
			return
		}
		targets, err := opendesign.DiscoverPreviewTargets(loaded.Archive)
		if err != nil {
			writeProjectDesignSystemError(w, http.StatusConflict, "open_design_preview_evidence_conflict", "Open Design archive preview targets are invalid")
			return
		}
		converted := make([]projectdesignsystem.PreviewTarget, 0, len(targets))
		for _, target := range targets {
			converted = append(converted, projectdesignsystem.PreviewTarget{ID: target.ID, Kind: string(target.Kind), Path: target.Path})
		}
		accessToken, expiresAt := issueOpenDesignArchivePreviewAccessToken(uuidToString(system.WorkspaceID), uuidToString(system.ID), loaded.ContentDigest)
		writeJSON(w, http.StatusOK, projectDesignSystemPackagePreviewResponse{
			Schema: nativePackagePreviewSchema, Slot: loaded.Slot, ContentDigest: loaded.ContentDigest,
			ResourceAccessToken: accessToken, ResourceAccessExpiresAt: expiresAt.Format(time.RFC3339), Targets: converted,
		})
		return
	}
	writeJSON(w, http.StatusOK, projectDesignSystemPackagePreviewResponse{Schema: nativePackagePreviewSchema, Slot: selected.Slot, Targets: []projectdesignsystem.PreviewTarget{}})
}

func (h *Handler) GetProjectDesignSystemPackagePreviewFile(w http.ResponseWriter, r *http.Request) {
	workspaceID, system, ok := h.openDesignArchivePreviewResourceScope(w, r)
	if !ok {
		return
	}
	selected, err := h.loadProjectDesignSystemPreviewPackage(r.Context(), system)
	if err != nil {
		writeProjectDesignSystemRequestError(w, err)
		return
	}
	if selected.PackageSchema != projectdesignsystem.PackageSchemaV2 {
		h.GetProjectDesignSystemArchivePreviewFile(w, r)
		return
	}
	manifest, archive, err := h.loadNativeProjectDesignSystemPackageArchive(r.Context(), system, selected)
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusConflict, "native_package_preview_unavailable", "native design package preview is unavailable")
		return
	}
	if "sha256:"+chi.URLParam(r, "digest") != manifest.ContentDigest {
		writeProjectDesignSystemError(w, http.StatusConflict, "native_package_preview_digest_conflict", "native design package preview digest is stale")
		return
	}
	artifactPath := strings.TrimPrefix(chi.URLParam(r, "*"), "/")
	artifact, err := projectdesignsystem.ReadV2Artifact(archive, manifest.Files, artifactPath)
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusNotFound, "native_package_preview_file_not_found", "native design package preview file is unavailable")
		return
	}
	contentType, ok := nativePackagePreviewContentType(artifactPath)
	if !ok {
		writeProjectDesignSystemError(w, http.StatusUnsupportedMediaType, "native_package_preview_file_unsupported", "native design package preview file type is unsupported")
		return
	}
	if nativePackagePreviewTarget(manifest.PreviewTargets, artifactPath) {
		accessToken := chi.URLParam(r, "accessToken")
		artifact = injectNativePackagePreviewBridge(artifact, accessToken)
		w.Header().Set("Content-Security-Policy", nativePackagePreviewCSP(accessToken))
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(artifact)))
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("X-Multica-Design-Package-Digest", manifest.ContentDigest)
	w.Header().Set("X-Multica-Workspace-ID", uuidToString(workspaceID))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(artifact)
}

func (h *Handler) DownloadProjectDesignSystemBasePackage(w http.ResponseWriter, r *http.Request) {
	task, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, chi.URLParam(r, "taskId"))
	if !ok {
		return
	}
	var taskContext service.ProjectDesignSystemTaskContext
	if json.Unmarshal(task.Context, &taskContext) != nil || taskContext.Type != service.ProjectDesignSystemTaskContextType ||
		(taskContext.Operation != service.ProjectDesignSystemAdjust && taskContext.Operation != service.ProjectDesignSystemRegenerate) ||
		taskContext.WorkspaceID != workspaceID || taskContext.AgentID != uuidToString(task.AgentID) {
		writeNativeBasePackageUnavailable(w)
		return
	}
	var reference projectdesignsystem.BasePackageReference
	if json.Unmarshal(taskContext.BasePackage, &reference) != nil || projectdesignsystem.ValidateBasePackageReference(reference) != nil {
		if isOpenDesignBasePackage(taskContext.BasePackage) {
			h.DownloadOpenDesignBaseArchive(w, r)
			return
		}
		writeNativeBasePackageUnavailable(w)
		return
	}
	workspaceUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeNativeBasePackageUnavailable(w)
		return
	}
	systemID, err := util.ParseUUID(taskContext.ProjectDesignSystemID)
	if err != nil {
		writeNativeBasePackageUnavailable(w)
		return
	}
	system, err := h.Queries.GetProjectDesignSystemInWorkspace(r.Context(), db.GetProjectDesignSystemInWorkspaceParams{ID: systemID, WorkspaceID: workspaceUUID})
	if err != nil || taskContext.ProjectID != uuidToString(system.ProjectID) {
		writeNativeBasePackageUnavailable(w)
		return
	}
	selected, err := h.Queries.GetProjectDesignSystemPackageBySlot(r.Context(), db.GetProjectDesignSystemPackageBySlotParams{DesignSystemID: system.ID, Slot: reference.Slot, WorkspaceID: system.WorkspaceID})
	if err != nil || selected.PackageSchema != projectdesignsystem.PackageSchemaV2 || "sha256:"+selected.IntegritySha256 != reference.ContentDigest ||
		!selected.SourceTaskID.Valid || uuidToString(selected.SourceTaskID) != reference.SourceTaskID {
		writeNativeBasePackageUnavailable(w)
		return
	}
	manifest, archive, err := h.loadNativeProjectDesignSystemPackageArchive(r.Context(), system, selected)
	if err != nil || manifest.ContentDigest != reference.ContentDigest || manifest.Binding != reference.Binding {
		writeNativeBasePackageUnavailable(w)
		return
	}
	w.Header().Set("Content-Type", projectdesignsystem.BasePackageArchiveContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(archive)))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set(projectdesignsystem.BasePackageDigestHeader, manifest.ContentDigest)
	w.Header().Set(projectdesignsystem.BasePackageSlotHeader, reference.Slot)
	w.Header().Set(projectdesignsystem.BasePackageSourceTaskHeader, reference.SourceTaskID)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(archive)
}

func (h *Handler) loadProjectDesignSystemPreviewPackage(ctx context.Context, system db.ProjectDesignSystem) (db.ProjectDesignSystemPackage, error) {
	for _, slot := range []string{"draft", "saved"} {
		selected, err := h.Queries.GetProjectDesignSystemPackageBySlot(ctx, db.GetProjectDesignSystemPackageBySlotParams{DesignSystemID: system.ID, Slot: slot, WorkspaceID: system.WorkspaceID})
		if err == nil {
			if selected.RenderStatus != "passed" {
				return db.ProjectDesignSystemPackage{}, &projectDesignSystemRequestError{status: http.StatusConflict, code: "package_preview_unavailable", message: "design package preview has not passed validation"}
			}
			return selected, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return db.ProjectDesignSystemPackage{}, projectDesignSystemInternalError("package_preview_lookup_failed", "failed to load design package preview")
		}
	}
	return db.ProjectDesignSystemPackage{}, &projectDesignSystemRequestError{status: http.StatusNotFound, code: "package_preview_unavailable", message: "design package preview is unavailable"}
}

func (h *Handler) loadNativeProjectDesignSystemPackageManifest(ctx context.Context, system db.ProjectDesignSystem, selected db.ProjectDesignSystemPackage) (projectdesignsystem.ManifestV2, error) {
	expected, err := h.expectedNativeProjectDesignSystemPackageBinding(ctx, system, selected)
	if err != nil {
		return projectdesignsystem.ManifestV2{}, err
	}
	if selected.PackageSchema != projectdesignsystem.PackageSchemaV2 || !selected.ArchiveObjectKey.Valid || selected.ArchiveObjectKey.String == "" || !validNativePackageDigest("sha256:"+selected.IntegritySha256) {
		return projectdesignsystem.ManifestV2{}, errors.New("native package metadata is invalid")
	}
	var manifest projectdesignsystem.ManifestV2
	var index []projectdesignsystem.ArtifactIndexEntry
	if json.Unmarshal(selected.Manifest, &manifest) != nil || json.Unmarshal(selected.ArtifactIndex, &index) != nil ||
		manifest.SchemaVersion != projectdesignsystem.PackageSchemaV2 || manifest.ContentDigest != "sha256:"+selected.IntegritySha256 ||
		manifest.Binding != expected || !reflect.DeepEqual(manifest.Files, index) {
		return projectdesignsystem.ManifestV2{}, errors.New("native package manifest is invalid")
	}
	return manifest, nil
}

func (h *Handler) loadNativeProjectDesignSystemPackageArchive(ctx context.Context, system db.ProjectDesignSystem, selected db.ProjectDesignSystemPackage) (projectdesignsystem.ManifestV2, []byte, error) {
	expected, err := h.expectedNativeProjectDesignSystemPackageBinding(ctx, system, selected)
	if err != nil {
		return projectdesignsystem.ManifestV2{}, nil, err
	}
	manifest, err := h.loadNativeProjectDesignSystemPackageManifest(ctx, system, selected)
	if err != nil {
		return projectdesignsystem.ManifestV2{}, nil, err
	}
	if h.Storage == nil {
		return projectdesignsystem.ManifestV2{}, nil, errors.New("native package storage is unavailable")
	}
	archive, err := readNativeArchiveFromStorage(ctx, h.Storage, selected.ArchiveObjectKey.String)
	if err != nil {
		return projectdesignsystem.ManifestV2{}, nil, err
	}
	validated, err := projectdesignsystem.ValidateV2Archive(archive, expected)
	if err != nil || !reflect.DeepEqual(validated.Manifest, manifest) ||
		validated.Manifest.ContentDigest != "sha256:"+selected.IntegritySha256 ||
		!reflect.DeepEqual(validated.Manifest.Files, manifest.Files) {
		if err == nil {
			err = errors.New("native package archive evidence is invalid")
		}
		return projectdesignsystem.ManifestV2{}, nil, err
	}
	return manifest, archive, nil
}

// expectedNativeProjectDesignSystemPackageBinding binds persisted package
// evidence to its immutable source task. It deliberately does not recompute
// an input digest from the current system row: later edits must not make a
// historically valid package unreadable.
func (h *Handler) expectedNativeProjectDesignSystemPackageBinding(ctx context.Context, system db.ProjectDesignSystem, selected db.ProjectDesignSystemPackage) (projectdesignsystem.PackageBinding, error) {
	if selected.PackageSchema != projectdesignsystem.PackageSchemaV2 || !selected.SourceTaskID.Valid ||
		uuidToString(selected.DesignSystemID) != uuidToString(system.ID) || !selected.AgentID.Valid {
		return projectdesignsystem.PackageBinding{}, errors.New("native package ownership is invalid")
	}
	task, err := h.Queries.GetAgentTask(ctx, selected.SourceTaskID)
	if err != nil {
		return projectdesignsystem.PackageBinding{}, errors.New("native package source task is unavailable")
	}
	var taskContext service.ProjectDesignSystemTaskContext
	if json.Unmarshal(task.Context, &taskContext) != nil || taskContext.Type != service.ProjectDesignSystemTaskContextType ||
		taskContext.PackageSchema != projectdesignsystem.PackageSchemaV2 || !validNativePackageOperation(taskContext.Operation) {
		return projectdesignsystem.PackageBinding{}, errors.New("native package task context is invalid")
	}
	if taskContext.WorkspaceID != uuidToString(system.WorkspaceID) || taskContext.ProjectID != uuidToString(system.ProjectID) ||
		taskContext.ProjectDesignSystemID != uuidToString(system.ID) || taskContext.AgentID != uuidToString(task.AgentID) ||
		taskContext.AgentID != uuidToString(selected.AgentID) {
		return projectdesignsystem.PackageBinding{}, errors.New("native package task ownership is invalid")
	}
	if !validNativePackageDigest(taskContext.InputSnapshotSHA256) || !selected.InputSnapshotSha256.Valid ||
		selected.InputSnapshotSha256.String != taskContext.InputSnapshotSHA256 {
		return projectdesignsystem.PackageBinding{}, errors.New("native package source binding is invalid")
	}
	baseDigest := taskContext.BasePackageSHA256
	if taskContext.Operation == service.ProjectDesignSystemGenerate {
		if baseDigest != "" || (selected.BasePackageSha256.Valid && selected.BasePackageSha256.String != "") {
			return projectdesignsystem.PackageBinding{}, errors.New("native generate package has a base digest")
		}
		baseDigest = ""
	} else if !validNativePackageDigest(baseDigest) || !selected.BasePackageSha256.Valid || selected.BasePackageSha256.String != strings.TrimPrefix(baseDigest, "sha256:") {
		return projectdesignsystem.PackageBinding{}, errors.New("native package base binding is invalid")
	}
	return projectdesignsystem.PackageBinding{
		WorkspaceID:         taskContext.WorkspaceID,
		ProjectID:           taskContext.ProjectID,
		DesignSystemID:      taskContext.ProjectDesignSystemID,
		TaskID:              uuidToString(task.ID),
		AgentID:             taskContext.AgentID,
		Operation:           string(taskContext.Operation),
		InputSnapshotSHA256: taskContext.InputSnapshotSHA256,
		BasePackageSHA256:   baseDigest,
	}, nil
}

func nativePackagePreviewTarget(targets []projectdesignsystem.PreviewTarget, artifactPath string) bool {
	for _, target := range targets {
		if target.Path == artifactPath {
			return true
		}
	}
	return false
}

func nativePackagePreviewContentType(artifactPath string) (string, bool) {
	switch strings.ToLower(path.Ext(artifactPath)) {
	case ".html":
		return "text/html; charset=utf-8", true
	case ".css":
		return "text/css; charset=utf-8", true
	case ".svg":
		return "image/svg+xml", true
	case ".png":
		return "image/png", true
	case ".jpg", ".jpeg":
		return "image/jpeg", true
	case ".webp":
		return "image/webp", true
	case ".woff":
		return "font/woff", true
	case ".woff2":
		return "font/woff2", true
	case ".ttf":
		return "font/ttf", true
	case ".otf":
		return "font/otf", true
	default:
		return "", false
	}
}

func nativePackagePreviewBridgeScript(capability string) string {
	return projectdesignsystem.RuntimePreviewBridgeScript(capability)
}

func nativePackagePreviewCSP(capability string) string {
	return projectdesignsystem.RuntimePreviewCSP(nativePackagePreviewBridgeScript(capability))
}

func injectNativePackagePreviewBridge(html []byte, capability string) []byte {
	return projectdesignsystem.InjectRuntimePreviewHTML(html, nativePackagePreviewBridgeScript(capability))
}

func isOpenDesignBasePackage(raw json.RawMessage) bool {
	var envelope struct {
		Schema string `json:"schema"`
	}
	return json.Unmarshal(raw, &envelope) == nil && envelope.Schema == opendesign.BasePackageReferenceSchema
}

func writeNativeBasePackageUnavailable(w http.ResponseWriter) {
	writeProjectDesignSystemError(w, http.StatusConflict, "native_package_base_unavailable", "native design package base is unavailable")
}
