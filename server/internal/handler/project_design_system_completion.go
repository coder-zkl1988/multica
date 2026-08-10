package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/designpreview"
	"github.com/multica-ai/multica/server/internal/projectdesignsystem"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// nativePackageArchiveReadLimit caps how many bytes the server will read
// from object storage when revalidating an uploaded V2 archive. The upload
// endpoint caps at the same value; reading any more than that means the
// archive either exceeds the contract or the storage layer is corrupted.
const nativePackageArchiveReadLimit = 64 << 20

// storageGetter is the subset of the storage.Storage interface the
// completion path uses. Pulled into a local alias so we can pass a mock
// during tests without taking a dependency on the qiniu/local packages.
type storageGetter interface {
	GetReader(ctx context.Context, key string) (io.ReadCloser, error)
}

type ProjectDesignSystemArtifacts struct {
	DesignMD       string `json:"design_md"`
	TokensCSS      string `json:"tokens_css"`
	ComponentsHTML string `json:"components_html"`
}

type preparedProjectDesignSystemCompletion struct {
	TaskContext service.ProjectDesignSystemTaskContext
	WorkspaceID pgtype.UUID
	ProjectID   pgtype.UUID
	SystemID    pgtype.UUID
	AgentID     pgtype.UUID
	// Package holds the v1 inline artifact validation. It is set only when
	// the task context is missing PackageSchema (the v1 path).
	Package projectdesignsystem.ValidatedPackage
	// NativePackage holds the server-revalidated V2 archive, audit, and
	// preview receipt. Set only for V2 tasks (PackageSchema == V2).
	NativePackage *preparedNativePackage
}

// preparedNativePackage is the server-revalidated V2 result ready to be
// persisted in the same transaction as the task completion.
type preparedNativePackage struct {
	Binding       projectdesignsystem.PackageBinding
	Validated     projectdesignsystem.ValidatedV2Package
	Archive       []byte
	Receipt       designpreview.Receipt
	ObjectKey     string
	ContentDigest string
}

func isProjectDesignSystemTaskContext(task db.AgentTaskQueue) bool {
	if task.IssueID.Valid || task.ChatSessionID.Valid || task.AutopilotRunID.Valid || len(task.Context) == 0 {
		return false
	}
	var taskContext service.ProjectDesignSystemTaskContext
	return json.Unmarshal(task.Context, &taskContext) == nil && taskContext.Type == service.ProjectDesignSystemTaskContextType
}

func (h *Handler) prepareProjectDesignSystemCompletion(
	ctx context.Context,
	task db.AgentTaskQueue,
	resolvedWorkspaceID string,
	artifacts *ProjectDesignSystemArtifacts,
	receipt *ProjectDesignSystemPackageReceipt,
) (preparedProjectDesignSystemCompletion, error) {
	var taskContext service.ProjectDesignSystemTaskContext
	if err := json.Unmarshal(task.Context, &taskContext); err != nil || taskContext.Type != service.ProjectDesignSystemTaskContextType {
		return preparedProjectDesignSystemCompletion{}, errors.New("invalid project design system task context")
	}
	workspaceID, err := util.ParseUUID(taskContext.WorkspaceID)
	if err != nil || taskContext.WorkspaceID != resolvedWorkspaceID {
		return preparedProjectDesignSystemCompletion{}, errors.New("project design system workspace does not match task")
	}
	projectID, err := util.ParseUUID(taskContext.ProjectID)
	if err != nil {
		return preparedProjectDesignSystemCompletion{}, errors.New("invalid project design system project id")
	}
	systemID, err := util.ParseUUID(taskContext.ProjectDesignSystemID)
	if err != nil {
		return preparedProjectDesignSystemCompletion{}, errors.New("invalid project design system id")
	}
	agentID, err := util.ParseUUID(taskContext.AgentID)
	if err != nil || uuidToString(agentID) != uuidToString(task.AgentID) {
		return preparedProjectDesignSystemCompletion{}, errors.New("project design system agent does not match task")
	}

	system, err := h.Queries.GetProjectDesignSystemInWorkspace(ctx, db.GetProjectDesignSystemInWorkspaceParams{
		ID:          systemID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return preparedProjectDesignSystemCompletion{}, errors.New("project design system not found")
	}
	if uuidToString(system.ProjectID) != uuidToString(projectID) ||
		!system.CurrentAgentID.Valid || uuidToString(system.CurrentAgentID) != uuidToString(agentID) ||
		!system.ActiveTaskID.Valid || uuidToString(system.ActiveTaskID) != uuidToString(task.ID) {
		return preparedProjectDesignSystemCompletion{}, errors.New("project design system active task does not match completion")
	}

	prepared := preparedProjectDesignSystemCompletion{
		TaskContext: taskContext,
		WorkspaceID: workspaceID,
		ProjectID:   projectID,
		SystemID:    systemID,
		AgentID:     agentID,
	}

	// V2 tasks pin PackageSchema on the context. The daemon must send a
	// project_design_system_package receipt; inline artifacts are rejected
	// on this path. V1 tasks (no PackageSchema and no open_design_run) keep
	// the legacy inline-artifact flow.
	switch {
	case taskContext.PackageSchema == projectdesignsystem.PackageSchemaV2:
		if receipt == nil {
			return preparedProjectDesignSystemCompletion{}, errors.New("native project design system package receipt is required")
		}
		if artifacts != nil {
			return preparedProjectDesignSystemCompletion{}, errors.New("native project design system tasks must not include inline artifacts")
		}
		native, err := h.prepareNativeProjectDesignSystemCompletion(ctx, receipt, task, taskContext, systemID, workspaceID)
		if err != nil {
			return preparedProjectDesignSystemCompletion{}, err
		}
		prepared.NativePackage = native
	case taskContext.PackageSchema != "":
		return preparedProjectDesignSystemCompletion{}, fmt.Errorf("unsupported project design system package schema %q", taskContext.PackageSchema)
	case len(taskContext.OpenDesignRun) > 0:
		// Open Design supervisor callbacks flow through their own completion
		// path. The generic completion handler must not accept them.
		return preparedProjectDesignSystemCompletion{}, errors.New("open design project design system completions must use the supervisor callback")
	default:
		// Legacy v1 inline-artifact path. The receipt must be absent.
		if receipt != nil {
			return preparedProjectDesignSystemCompletion{}, errors.New("legacy project design system tasks must not include a package receipt")
		}
		if artifacts == nil {
			return preparedProjectDesignSystemCompletion{}, errors.New("project design system artifacts are required")
		}
		validated, err := projectdesignsystem.Validate(projectdesignsystem.ArtifactInput{
			DesignMD:       artifacts.DesignMD,
			TokensCSS:      artifacts.TokensCSS,
			ComponentsHTML: artifacts.ComponentsHTML,
		}, h.projectDesignSystemAllowedHosts())
		if err != nil {
			return preparedProjectDesignSystemCompletion{}, fmt.Errorf("invalid project design system artifacts: %w", err)
		}
		prepared.Package = validated
	}

	return prepared, nil
}

// prepareNativeProjectDesignSystemCompletion independently re-validates
// every field of the daemon's V2 receipt before persisting it. It downloads
// the archive from object storage (using the derived key the upload
// endpoint committed), recomputes the digest and artifact index, runs the
// static Package Audit, and checks the Preview receipt's schema, target
// set, and passing status. It does NOT trust any field of the receipt.
func (h *Handler) prepareNativeProjectDesignSystemCompletion(
	ctx context.Context,
	receipt *ProjectDesignSystemPackageReceipt,
	task db.AgentTaskQueue,
	taskContext service.ProjectDesignSystemTaskContext,
	systemID pgtype.UUID,
	workspaceID pgtype.UUID,
) (*preparedNativePackage, error) {
	if receipt.SchemaVersion != projectdesignsystem.PackageSchemaV2 {
		return nil, fmt.Errorf("native project design system package receipt schema %q does not match %q", receipt.SchemaVersion, projectdesignsystem.PackageSchemaV2)
	}
	if receipt.ObjectKey == "" || receipt.ContentDigest == "" {
		return nil, errors.New("native project design system package receipt is missing object key or digest")
	}

	binding, err := h.nativePackageBindingForTaskContext(ctx, task, taskContext, systemID, workspaceID)
	if err != nil {
		return nil, err
	}

	// The receipt's digest must equal the binding's input/base digests
	// combined with the archive contents. We derive the expected object
	// key from the binding + digest and compare with the receipt.
	digestHex := strings.TrimPrefix(receipt.ContentDigest, "sha256:")
	expectedKey := fmt.Sprintf("%s/%s/%s/%s/%s.zip",
		nativePackageObjectKeyRoot,
		binding.WorkspaceID,
		binding.DesignSystemID,
		binding.TaskID,
		digestHex,
	)
	if receipt.ObjectKey != expectedKey {
		return nil, fmt.Errorf("native project design system package receipt object key %q does not match the task binding %q", receipt.ObjectKey, expectedKey)
	}

	if h.Storage == nil {
		return nil, errors.New("native design package storage is unavailable")
	}
	archive, err := readNativeArchiveFromStorage(ctx, h.Storage, receipt.ObjectKey)
	if err != nil {
		return nil, fmt.Errorf("read native project design system package archive: %w", err)
	}

	validated, err := projectdesignsystem.ValidateV2Archive(archive, binding)
	if err != nil {
		return nil, fmt.Errorf("native project design system package archive failed revalidation: %w", err)
	}
	if validated.Manifest.ContentDigest != receipt.ContentDigest {
		return nil, fmt.Errorf("native project design system package receipt digest %q does not match recomputed archive digest %q", receipt.ContentDigest, validated.Manifest.ContentDigest)
	}

	// The audit report is server-recomputed inside ValidateV2Archive; the
	// receipt's audit must match both the schema version and the digest.
	if receipt.Audit.SchemaVersion != projectdesignsystem.AuditSchemaV1 {
		return nil, fmt.Errorf("native project design system package receipt audit schema %q does not match %q", receipt.Audit.SchemaVersion, projectdesignsystem.AuditSchemaV1)
	}
	if !validated.Audit.Passed || !receipt.Audit.Passed {
		return nil, fmt.Errorf("native project design system package audit did not pass: receipt=%v recomputed=%v", receipt.Audit.Passed, validated.Audit.Passed)
	}

	// The receipt's artifact index must match what ValidateV2Archive
	// recomputed from the archive contents.
	if !artifactIndexMatches(validated.Manifest.Files, receipt.ArtifactIndex) {
		return nil, errors.New("native project design system package receipt artifact index does not match archive contents")
	}

	// Preview receipt: schema, digest binding to the archive, target set,
	// and overall passing state.
	if err := designpreview.ValidateReceipt(receipt.Preview, receipt.ContentDigest, manifestPreviewTargets(validated.Manifest.PreviewTargets)); err != nil {
		return nil, fmt.Errorf("native project design system preview receipt invalid: %w", err)
	}
	if !receipt.Preview.Verification.Passed {
		return nil, errors.New("native project design system preview receipt did not pass")
	}

	return &preparedNativePackage{
		Binding:       binding,
		Validated:     validated,
		Archive:       archive,
		Receipt:       receipt.Preview,
		ObjectKey:     receipt.ObjectKey,
		ContentDigest: receipt.ContentDigest,
	}, nil
}

// nativePackageBindingForTaskContext reproduces the upload-endpoint's
// binding derivation but reads everything from already-parsed values
// (avoids re-unmarshalling the task context or re-querying the system).
func (h *Handler) nativePackageBindingForTaskContext(
	ctx context.Context,
	task db.AgentTaskQueue,
	taskContext service.ProjectDesignSystemTaskContext,
	systemID pgtype.UUID,
	workspaceID pgtype.UUID,
) (projectdesignsystem.PackageBinding, error) {
	system, err := h.Queries.GetProjectDesignSystemInWorkspace(ctx, db.GetProjectDesignSystemInWorkspaceParams{
		ID:          systemID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return projectdesignsystem.PackageBinding{}, errors.New("project design system not found")
	}
	inputDigest, err := projectdesignsystem.SnapshotDigest(system.InputSnapshot)
	if err != nil {
		return projectdesignsystem.PackageBinding{}, errors.New("invalid project design system input snapshot")
	}
	pinnedInputDigest, pinnedBaseDigest := nativePackagePinnedDigests(task.Context, taskContext.BasePackage)
	if pinnedInputDigest != "" && pinnedInputDigest != inputDigest {
		return projectdesignsystem.PackageBinding{}, errors.New("project design system input snapshot binding changed")
	}
	if taskContext.Operation == service.ProjectDesignSystemGenerate {
		pinnedBaseDigest = ""
	}
	return projectdesignsystem.PackageBinding{
		WorkspaceID:         uuidToString(workspaceID),
		ProjectID:           uuidToString(system.ProjectID),
		DesignSystemID:      uuidToString(systemID),
		TaskID:              uuidToString(task.ID),
		AgentID:             taskContext.AgentID,
		Operation:           string(taskContext.Operation),
		InputSnapshotSHA256: inputDigest,
		BasePackageSHA256:   pinnedBaseDigest,
	}, nil
}

func readNativeArchiveFromStorage(ctx context.Context, store storageGetter, key string) ([]byte, error) {
	reader, err := store.GetReader(ctx, key)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	limited := io.LimitReader(reader, nativePackageArchiveReadLimit+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(data) > nativePackageArchiveReadLimit {
		return nil, errors.New("native project design system package archive exceeds the read limit")
	}
	return data, nil
}

// artifactIndexMatches compares two artifact indices regardless of the
// JSON tag ordering the daemon may have used. They must have the same set
// of paths with identical content.
func artifactIndexMatches(recomputed []projectdesignsystem.ArtifactIndexEntry, receipt []projectdesignsystem.ArtifactIndexEntry) bool {
	if len(recomputed) != len(receipt) {
		return false
	}
	recomputedByPath := make(map[string]projectdesignsystem.ArtifactIndexEntry, len(recomputed))
	for _, entry := range recomputed {
		recomputedByPath[entry.Path] = entry
	}
	for _, entry := range receipt {
		want, ok := recomputedByPath[entry.Path]
		if !ok {
			return false
		}
		if want.MediaType != entry.MediaType || want.SizeBytes != entry.SizeBytes || want.SHA256 != entry.SHA256 || want.Role != entry.Role {
			return false
		}
	}
	return true
}

func manifestPreviewTargets(targets []projectdesignsystem.PreviewTarget) []designpreview.Target {
	out := make([]designpreview.Target, 0, len(targets))
	for _, target := range targets {
		out = append(out, designpreview.Target{
			Kind: target.Kind,
			ID:   target.ID,
			Path: target.Path,
		})
	}
	return out
}

func persistProjectDesignSystemCompletion(
	ctx context.Context,
	queries *db.Queries,
	completedTask db.AgentTaskQueue,
	prepared preparedProjectDesignSystemCompletion,
) (db.ProjectDesignSystem, error) {
	if _, err := queries.LockProjectInWorkspaceForUpdate(ctx, db.LockProjectInWorkspaceForUpdateParams{
		ID:          prepared.ProjectID,
		WorkspaceID: prepared.WorkspaceID,
	}); err != nil {
		return db.ProjectDesignSystem{}, err
	}
	system, err := queries.GetProjectDesignSystemInWorkspace(ctx, db.GetProjectDesignSystemInWorkspaceParams{
		ID:          prepared.SystemID,
		WorkspaceID: prepared.WorkspaceID,
	})
	if err != nil {
		return db.ProjectDesignSystem{}, err
	}
	if uuidToString(system.ProjectID) != uuidToString(prepared.ProjectID) ||
		!system.CurrentAgentID.Valid || uuidToString(system.CurrentAgentID) != uuidToString(prepared.AgentID) ||
		!system.ActiveTaskID.Valid || uuidToString(system.ActiveTaskID) != uuidToString(completedTask.ID) {
		return db.ProjectDesignSystem{}, errors.New("project design system active task changed before completion")
	}

	if prepared.NativePackage != nil {
		return persistNativeProjectDesignSystemCompletion(ctx, queries, completedTask, prepared, system)
	}

	manifestJSON, err := json.Marshal(prepared.Package.Manifest)
	if err != nil {
		return db.ProjectDesignSystem{}, fmt.Errorf("marshal project design system manifest: %w", err)
	}
	validationJSON, err := json.Marshal(prepared.Package.Validation)
	if err != nil {
		return db.ProjectDesignSystem{}, fmt.Errorf("marshal project design system validation: %w", err)
	}
	if _, err := queries.UpsertProjectDesignSystemPackage(ctx, db.UpsertProjectDesignSystemPackageParams{
		DesignSystemID:  system.ID,
		Slot:            "draft",
		DesignMd:        prepared.Package.Artifacts.DesignMD,
		TokensCss:       prepared.Package.Artifacts.TokensCSS,
		ComponentsHtml:  prepared.Package.Artifacts.ComponentsHTML,
		Manifest:        manifestJSON,
		Validation:      validationJSON,
		IntegritySha256: prepared.Package.Manifest.Digest,
		SourceTaskID:    completedTask.ID,
		AgentID:         prepared.AgentID,
		Instruction:     pgtype.Text{String: prepared.TaskContext.Instruction, Valid: strings.TrimSpace(prepared.TaskContext.Instruction) != ""},
		Scope:           prepared.TaskContext.Scope,
		WorkspaceID:     prepared.WorkspaceID,
	}); err != nil {
		return db.ProjectDesignSystem{}, err
	}

	return queries.ClearProjectDesignSystemActiveTask(ctx, db.ClearProjectDesignSystemActiveTaskParams{
		ID:           system.ID,
		WorkspaceID:  prepared.WorkspaceID,
		ActiveTaskID: completedTask.ID,
	})
}

// persistNativeProjectDesignSystemCompletion writes the V2 package as the
// new draft, stamps the audit/preview/render_status evidence, and clears
// the active task — all inside the same DB transaction as the task
// completion so a partial failure rolls back atomically.
func persistNativeProjectDesignSystemCompletion(
	ctx context.Context,
	queries *db.Queries,
	completedTask db.AgentTaskQueue,
	prepared preparedProjectDesignSystemCompletion,
	system db.ProjectDesignSystem,
) (db.ProjectDesignSystem, error) {
	native := prepared.NativePackage
	designMD, tokensCSS, componentsHTML, err := readNativeDraftArtifacts(native)
	if err != nil {
		return db.ProjectDesignSystem{}, err
	}

	manifestJSON, err := json.Marshal(native.Validated.Manifest)
	if err != nil {
		return db.ProjectDesignSystem{}, fmt.Errorf("marshal native project design system manifest: %w", err)
	}
	auditJSON, err := json.Marshal(native.Validated.Audit)
	if err != nil {
		return db.ProjectDesignSystem{}, fmt.Errorf("marshal native project design system audit: %w", err)
	}
	artifactIndexJSON, err := json.Marshal(native.Validated.Manifest.Files)
	if err != nil {
		return db.ProjectDesignSystem{}, fmt.Errorf("marshal native project design system artifact index: %w", err)
	}

	// integrity_sha256 stores the lowercase hex digest WITHOUT the "sha256:"
	// prefix (projectdesignsystem.ManifestV2.ContentDigest keeps it).
	integrityDigest := strings.TrimPrefix(native.ContentDigest, "sha256:")

	upserted, err := queries.UpsertProjectDesignSystemPackage(ctx, db.UpsertProjectDesignSystemPackageParams{
		DesignSystemID:      system.ID,
		Slot:                "draft",
		DesignMd:            designMD,
		TokensCss:           tokensCSS,
		ComponentsHtml:      componentsHTML,
		Manifest:            manifestJSON,
		Validation:          auditJSON,
		IntegritySha256:     integrityDigest,
		SourceTaskID:        completedTask.ID,
		AgentID:             prepared.AgentID,
		Instruction:         pgtype.Text{String: prepared.TaskContext.Instruction, Valid: strings.TrimSpace(prepared.TaskContext.Instruction) != ""},
		Scope:               prepared.TaskContext.Scope,
		PackageSchema:       projectdesignsystem.PackageSchemaV2,
		ArchiveObjectKey:    pgtype.Text{String: native.ObjectKey, Valid: true},
		ArtifactIndex:       artifactIndexJSON,
		InputSnapshotSha256: pgtype.Text{String: native.Binding.InputSnapshotSHA256, Valid: native.Binding.InputSnapshotSHA256 != ""},
		BasePackageSha256:   pgtype.Text{String: strings.TrimPrefix(native.Binding.BasePackageSHA256, "sha256:"), Valid: native.Binding.BasePackageSHA256 != ""},
		WorkspaceID:         prepared.WorkspaceID,
	})
	if err != nil {
		return db.ProjectDesignSystem{}, err
	}

	renderReport, err := nativeReceiptReport(native.Receipt)
	if err != nil {
		return db.ProjectDesignSystem{}, fmt.Errorf("marshal native project design system render report: %w", err)
	}
	if _, err := queries.UpdateProjectDesignSystemPackageRenderValidation(ctx, db.UpdateProjectDesignSystemPackageRenderValidationParams{
		RenderStatus:    "passed",
		RenderReport:    renderReport,
		DesignSystemID:  system.ID,
		IntegritySha256: upserted.IntegritySha256,
		WorkspaceID:     prepared.WorkspaceID,
	}); err != nil {
		return db.ProjectDesignSystem{}, fmt.Errorf("update native project design system render validation: %w", err)
	}

	return queries.ClearProjectDesignSystemActiveTask(ctx, db.ClearProjectDesignSystemActiveTaskParams{
		ID:           system.ID,
		WorkspaceID:  prepared.WorkspaceID,
		ActiveTaskID: completedTask.ID,
	})
}

// readNativeDraftArtifacts pulls the bounded markdown/css/html artifacts
// from the validated archive so the draft columns the UI relies on remain
// populated even after the legacy Package artifacts are no longer the
// canonical source. V2 packages ship DESIGN.md + tokens.css as required
// artifacts; components.html is a v1 legacy column and the v2 contract
// uses preview/*.html + ui-kit/index.html instead, so the column is
// left empty for native drafts.
func readNativeDraftArtifacts(native *preparedNativePackage) (string, string, string, error) {
	designMD, err := projectdesignsystem.ReadV2Artifact(native.Archive, native.Validated.Manifest.Files, "DESIGN.md")
	if err != nil {
		return "", "", "", fmt.Errorf("read native DESIGN.md: %w", err)
	}
	tokensCSS, err := projectdesignsystem.ReadV2Artifact(native.Archive, native.Validated.Manifest.Files, "tokens.css")
	if err != nil {
		return "", "", "", fmt.Errorf("read native tokens.css: %w", err)
	}
	return string(designMD), string(tokensCSS), "", nil
}

// nativeReceiptReport bundles the audit + preview evidence into the
// shape the project's existing render_report JSON column expects. The
// stored value is consumed by the project design system preview-verification
// endpoint and the saved/draft response renderer.
func nativeReceiptReport(receipt designpreview.Receipt) ([]byte, error) {
	type report struct {
		Source  string                `json:"source"`
		Receipt designpreview.Receipt `json:"receipt"`
	}
	return json.Marshal(report{Source: "native_v2_completion", Receipt: receipt})
}

func (h *Handler) failInvalidProjectDesignSystemCompletion(ctx context.Context, task db.AgentTaskQueue, req TaskCompleteRequest, cause error) {
	failedTask, err := h.TaskService.FailTask(ctx, task.ID, cause.Error(), req.SessionID, req.WorkDir, "project_design_system_invalid_artifacts", req.SessionRolloutMissing, req.RetiredSessionID)
	if err != nil || failedTask == nil {
		return
	}
	_ = h.Queries.DeleteTaskTokensByTask(ctx, failedTask.ID)
}
