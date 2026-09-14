package handler

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/projectdesignsystem"
	"github.com/multica-ai/multica/server/internal/service"
)

// DownloadDesignDocumentDesignSystem serves only the saved archive pinned in
// this task's server-generated context. The request names no object, system,
// or package; every identity and byte-level validation is checked against the
// frozen task context before storage is consulted.
func (h *Handler) DownloadDesignDocumentDesignSystem(w http.ResponseWriter, r *http.Request) {
	task, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, chi.URLParam(r, "taskId"))
	if !ok {
		return
	}
	var taskContext service.DesignDocumentTaskContext
	if json.Unmarshal(task.Context, &taskContext) != nil ||
		taskContext.Type != service.DesignDocumentTaskContextType ||
		taskContext.WorkspaceID != workspaceID ||
		taskContext.AgentID != uuidToString(task.AgentID) ||
		taskContext.Operation != service.DesignDocumentGenerate ||
		!taskContext.ExecutionReady ||
		taskContext.DesignContext == nil ||
		taskContext.Input.DesignSystem == nil ||
		!validNativePackageDigest(taskContext.DesignSystemDigest) ||
		taskContext.DesignSystemDigest != taskContext.Input.DesignSystem.ContentDigest {
		writeDesignDocumentDesignSystemUnavailable(w)
		return
	}
	var designContext service.ResolvedDesignContext
	if json.Unmarshal(taskContext.DesignContext, &designContext) != nil ||
		designContext.Package == nil || designContext.ProjectID == "" ||
		designContext.Package.DesignSystemID == "" || designContext.Package.SavedPackageID == "" ||
		designContext.Package.ArchiveObjectKey == "" || designContext.Digest != taskContext.DesignSystemDigest {
		writeDesignDocumentDesignSystemUnavailable(w)
		return
	}
	repositoryScope := designContext.Source == service.DesignContextSourceCloudSavedRepository && designContext.Package.Scope == service.DesignContextScopeRepository &&
		designContext.Package.ProjectID == taskContext.ProjectID && designContext.Package.ProjectResourceID == taskContext.ProjectResourceID && taskContext.ProjectResourceID != ""
	explicitScope := designContext.Source == service.DesignContextSourceCloudSavedWorkspace && designContext.Package.Scope == service.DesignContextScopeWorkspace
	if taskContext.ProjectID != designContext.ProjectID || (!repositoryScope && !explicitScope) {
		writeDesignDocumentDesignSystemUnavailable(w)
		return
	}
	if h.Storage == nil {
		writeProjectDesignSystemError(w, http.StatusServiceUnavailable, "design_system_storage_unavailable", "design system package storage is unavailable")
		return
	}
	archive, err := readNativeArchiveFromStorage(r.Context(), h.Storage, designContext.Package.ArchiveObjectKey)
	if err != nil {
		writeDesignDocumentDesignSystemChanged(w)
		return
	}
	var manifest projectdesignsystem.ManifestV2
	if designContext.Package.PackageSchema != projectdesignsystem.PackageSchemaV2 ||
		json.Unmarshal(designContext.Package.V2Manifest, &manifest) != nil ||
		manifest.SchemaVersion != projectdesignsystem.PackageSchemaV2 ||
		manifest.ContentDigest != designContext.Digest ||
		!reflect.DeepEqual(manifest.Files, designContext.Package.V2ArtifactIndex) {
		writeDesignDocumentDesignSystemChanged(w)
		return
	}
	// Validate the full archive against the frozen manifest binding and file
	// index. The current saved slot is deliberately not consulted: later saves
	// cannot retarget or invalidate an already queued task.
	if _, err := projectdesignsystem.ReadV2Artifact(archive, manifest.Files, "DESIGN.md"); err != nil {
		writeDesignDocumentDesignSystemChanged(w)
		return
	}

	w.Header().Set("Content-Type", nativePackageArchiveContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(archive)))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set(nativePackageDigestHeader, designContext.Digest)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(archive)
}

func writeDesignDocumentDesignSystemUnavailable(w http.ResponseWriter) {
	writeProjectDesignSystemError(w, http.StatusNotFound, "design_system_unavailable", "no design system package is pinned for this task")
}

func writeDesignDocumentDesignSystemChanged(w http.ResponseWriter) {
	writeProjectDesignSystemError(w, http.StatusConflict, "design_system_changed", "the pinned design system package is unavailable or changed")
}

func artifactIndexFromRaw(raw []byte) []projectdesignsystem.ArtifactIndexEntry {
	if len(raw) == 0 {
		return nil
	}
	var index []projectdesignsystem.ArtifactIndexEntry
	_ = json.Unmarshal(raw, &index)
	return index
}
