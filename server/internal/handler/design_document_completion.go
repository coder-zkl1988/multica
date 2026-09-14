package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/designdocument"
	"github.com/multica-ai/multica/server/internal/designpreview"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Design document completion (DC-042 / DC-034).
//
// The daemon already collected, audited, previewed and uploaded the package
// before reporting. The server does NOT take that on trust: it re-reads the
// archive from object storage and re-derives everything independently. The
// daemon runs on an employee machine, and a draft it produced becomes the
// basis for what downstream agents and delivery will read once saved.
//
// A draft forms only when the archive, its digest, the audit receipt and the
// preview receipt all agree with the same task binding — and it forms
// atomically with the pointer move, so there is no window where a revision
// exists that nothing points at, or a pointer aims at a half-written row.

const designDocumentObjectKeyRoot = "design-documents"

const (
	designDocumentTaskContextType = service.DesignDocumentTaskContextType
	designDocumentTaskSchema      = designdocument.PackageSchemaV1
	designDocumentInputSchema     = "multica.design-document-input/v1"
)

type DesignDocumentPackageReceipt struct {
	SchemaVersion string                              `json:"schema_version"`
	ObjectKey     string                              `json:"object_key"`
	ContentDigest string                              `json:"content_digest"`
	ArtifactIndex []designdocument.ArtifactIndexEntry `json:"artifact_index"`
	Audit         designdocument.AuditReport          `json:"audit"`
	Preview       designpreview.Receipt               `json:"preview"`
}

type preparedDesignDocumentCompletion struct {
	TaskContext service.DesignDocumentTaskContext
	WorkspaceID pgtype.UUID
	DocumentID  pgtype.UUID
	AgentID     pgtype.UUID
	Validated   designdocument.ValidatedPackage
	Receipt     *DesignDocumentPackageReceipt
	// RepositoryGrounding is normalized bytes from a strictly validated daemon
	// receipt. Empty means no explicit receipt and, for pinned runs, inheritance
	// from BaseRevisionID during persistence.
	RepositoryGrounding json.RawMessage
	// Stored verbatim from the re-validated archive so the persisted rows are
	// the bytes the server verified, not a re-encoding of them.
	Brief    []byte
	Coverage []byte
}

func isDesignDocumentTaskContext(task db.AgentTaskQueue) bool {
	if len(task.Context) == 0 {
		return false
	}
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(task.Context, &envelope); err != nil {
		return false
	}
	return envelope.Type == service.DesignDocumentTaskContextType
}

func (h *Handler) prepareDesignDocumentCompletion(
	ctx context.Context,
	task db.AgentTaskQueue,
	workspaceID string,
	receipt *DesignDocumentPackageReceipt,
	repositoryGrounding json.RawMessage,
) (preparedDesignDocumentCompletion, error) {
	var taskContext service.DesignDocumentTaskContext
	if err := json.Unmarshal(task.Context, &taskContext); err != nil ||
		taskContext.Type != service.DesignDocumentTaskContextType {
		return preparedDesignDocumentCompletion{}, errors.New("invalid design document task context")
	}
	if receipt == nil {
		return preparedDesignDocumentCompletion{}, errors.New("design document package receipt is required")
	}
	if taskContext.WorkspaceID != workspaceID {
		return preparedDesignDocumentCompletion{}, errors.New("design document task context does not match daemon task ownership")
	}
	if taskContext.AgentID != uuidToString(task.AgentID) {
		return preparedDesignDocumentCompletion{}, errors.New("design document task context does not match the reporting agent")
	}
	if receipt.SchemaVersion != designdocument.PackageSchemaV1 {
		return preparedDesignDocumentCompletion{}, fmt.Errorf("design document package receipt schema %q does not match %q", receipt.SchemaVersion, designdocument.PackageSchemaV1)
	}
	if strings.TrimSpace(receipt.ObjectKey) == "" || strings.TrimSpace(receipt.ContentDigest) == "" {
		return preparedDesignDocumentCompletion{}, errors.New("design document package receipt is missing object key or digest")
	}

	workspaceUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return preparedDesignDocumentCompletion{}, errors.New("invalid design document workspace")
	}
	documentUUID, err := util.ParseUUID(taskContext.DesignDocumentID)
	if err != nil {
		return preparedDesignDocumentCompletion{}, errors.New("invalid design document id")
	}
	document, err := h.Queries.GetDesignDocumentInWorkspace(ctx, db.GetDesignDocumentInWorkspaceParams{
		ID: documentUUID, WorkspaceID: workspaceUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return preparedDesignDocumentCompletion{}, errors.New("design document not found")
	}
	if err != nil {
		return preparedDesignDocumentCompletion{}, fmt.Errorf("load design document: %w", err)
	}
	// The task must still be the document's active one. A stale task
	// reporting late must not overwrite a draft produced by a newer run.
	if uuidToString(document.ActiveTaskID) != uuidToString(task.ID) {
		return preparedDesignDocumentCompletion{}, errors.New("design document task is no longer the active task")
	}

	binding := designDocumentBindingFromContext(taskContext, task)
	// Deriving the expected key from the binding means a daemon cannot point
	// the server at some other workspace's archive.
	expectedKey := designDocumentObjectKey(binding, receipt.ContentDigest)
	if receipt.ObjectKey != expectedKey {
		return preparedDesignDocumentCompletion{}, fmt.Errorf("design document package receipt object key %q does not match the task binding %q", receipt.ObjectKey, expectedKey)
	}

	if h.Storage == nil {
		return preparedDesignDocumentCompletion{}, errors.New("design document package storage is unavailable")
	}
	archive, err := readNativeArchiveFromStorage(ctx, h.Storage, receipt.ObjectKey)
	if err != nil {
		return preparedDesignDocumentCompletion{}, fmt.Errorf("read design document package archive: %w", err)
	}

	// Full independent re-validation: re-derives every per-file digest, the
	// content digest, and re-runs the whole audit from archive bytes.
	validated, err := designdocument.ValidateArchive(archive, binding)
	if err != nil {
		return preparedDesignDocumentCompletion{}, fmt.Errorf("design document package archive failed revalidation: %w", err)
	}
	if validated.Manifest.ContentDigest != receipt.ContentDigest {
		return preparedDesignDocumentCompletion{}, fmt.Errorf("design document receipt digest %q does not match recomputed archive digest %q", receipt.ContentDigest, validated.Manifest.ContentDigest)
	}
	if !validated.Audit.Passed {
		return preparedDesignDocumentCompletion{}, errors.New("design document package failed server-side audit")
	}
	if receipt.Audit.SchemaVersion != designdocument.AuditSchemaV1 {
		return preparedDesignDocumentCompletion{}, fmt.Errorf("design document audit receipt schema %q does not match %q", receipt.Audit.SchemaVersion, designdocument.AuditSchemaV1)
	}

	// Preview is a hard gate: a package that was never rendered, or was
	// rendered from different bytes, cannot become a draft (spec §12.3).
	// ValidateReceipt also binds the browser policy and the exact target set,
	// so a receipt that skipped a page cannot pass for a complete one.
	expectedTargets, err := designDocumentPreviewTargets(validated.Manifest.PreviewTargets)
	if err != nil {
		return preparedDesignDocumentCompletion{}, err
	}
	if err := designpreview.ValidateReceipt(
		receipt.Preview,
		validated.Manifest.ContentDigest,
		expectedTargets,
	); err != nil {
		return preparedDesignDocumentCompletion{}, fmt.Errorf("design document preview receipt invalid: %w", err)
	}
	if !receipt.Preview.Verification.Passed {
		return preparedDesignDocumentCompletion{}, errors.New("design document package did not pass browser preview")
	}

	repositoryGrounding, err = validateDesignDocumentCompletionGrounding(
		taskContext.Input.RepositoryGrounding,
		repositoryGrounding,
	)
	if err != nil {
		return preparedDesignDocumentCompletion{}, err
	}

	brief, err := designdocument.ReadArtifact(archive, validated.Manifest.Files, "brief.json")
	if err != nil {
		return preparedDesignDocumentCompletion{}, fmt.Errorf("read design document brief: %w", err)
	}
	coverage, err := designdocument.ReadArtifact(archive, validated.Manifest.Files, "coverage.json")
	if err != nil {
		return preparedDesignDocumentCompletion{}, fmt.Errorf("read design document coverage: %w", err)
	}

	return preparedDesignDocumentCompletion{
		TaskContext:         taskContext,
		WorkspaceID:         workspaceUUID,
		DocumentID:          documentUUID,
		AgentID:             task.AgentID,
		Validated:           validated,
		Receipt:             receipt,
		Brief:               brief,
		RepositoryGrounding: repositoryGrounding,
		Coverage:            coverage,
	}, nil
}

// designDocumentPreviewTargets re-derives the target set the receipt is
// validated against. It MUST apply the same kind mapping the daemon used when
// building the receipt — passing the raw prototype_entry / prototype_page
// kinds through would make ValidateTargetSet reject every valid receipt.
func designDocumentPreviewTargets(targets []designdocument.PreviewTarget) ([]designpreview.Target, error) {
	out := make([]designpreview.Target, 0, len(targets))
	for _, target := range targets {
		kind, ok := designdocument.PreviewTargetKind(target.Kind)
		if !ok {
			return nil, fmt.Errorf("design document preview target %q has unsupported kind %q", target.ID, target.Kind)
		}
		out = append(out, designpreview.Target{Kind: kind, ID: target.ID, Path: target.Path})
	}
	return out, nil
}

// validateDesignDocumentCompletionGrounding normalizes the daemon receipt and
// applies the task's grounding mode. Pinned runs always discard a caller
// receipt; persistence copies the immutable base revision's evidence instead.
func validateDesignDocumentCompletionGrounding(mode string, raw json.RawMessage) (json.RawMessage, error) {
	if mode == service.DesignDocumentGroundingPinned {
		return nil, nil
	}
	if len(raw) == 0 {
		if mode == service.DesignDocumentGroundingPending {
			return nil, errors.New("design document repository grounding is required")
		}
		switch mode {
		case service.DesignDocumentGroundingUnavailable, service.DesignDocumentGroundingPinned:
			return nil, nil
		default:
			return nil, errors.New("unknown design document grounding mode")
		}
	}

	grounding, err := designdocument.ValidateRepositoryGrounding(raw)
	if err != nil {
		return nil, err
	}
	switch mode {
	case service.DesignDocumentGroundingPending:
		if grounding.Status != designdocument.GroundingAvailable {
			return nil, errors.New("pending repository grounding must be available")
		}
	case service.DesignDocumentGroundingUnavailable:
		if grounding.Status != designdocument.GroundingUnavailable {
			return nil, errors.New("unavailable repository grounding must remain unavailable")
		}
	default:
		return nil, errors.New("unknown design document grounding mode")
	}
	normalized, err := json.Marshal(grounding)
	if err != nil {
		return nil, err
	}
	return normalized, nil
}

// designDocumentBindingFromContext is the server side of the package binding
// contract; daemon.DecodeDesignDocumentTaskBinding is the other. Both must
// produce the identical value for the same task, and TestDesignDocumentBinding-
// MatchesTheDaemonBinding holds them together.
//
// RevisionID is the task id: the server does not pre-allocate revision ids and a
// task produces exactly one revision, so the task identity is the deterministic
// revision identity — the same rule the daemon applies when the context carries
// no explicit revision_id.
func designDocumentBindingFromContext(taskContext service.DesignDocumentTaskContext, task db.AgentTaskQueue) designdocument.PackageBinding {
	taskID := uuidToString(task.ID)
	revisionID := taskContext.RevisionID
	if revisionID == "" {
		revisionID = taskID
	}
	return designdocument.PackageBinding{
		WorkspaceID:         taskContext.WorkspaceID,
		ProjectID:           taskContext.ProjectID,
		ProjectResourceID:   taskContext.ProjectResourceID,
		IssueID:             taskContext.IssueID,
		DesignDocumentID:    taskContext.DesignDocumentID,
		RevisionID:          revisionID,
		TaskID:              taskID,
		AgentID:             taskContext.AgentID,
		Platform:            taskContext.Platform,
		InputSnapshotSHA256: taskContext.InputSnapshotSHA256,
		BaseRevisionSHA256:  taskContext.BaseContentDigest,
		DesignSystemSHA256:  taskContext.DesignSystemDigest,
	}
}

func designDocumentObjectKey(binding designdocument.PackageBinding, contentDigest string) string {
	return fmt.Sprintf(
		"%s/%s/%s/%s/%s.zip",
		designDocumentObjectKeyRoot,
		binding.WorkspaceID,
		binding.DesignDocumentID,
		binding.TaskID,
		strings.TrimPrefix(contentDigest, "sha256:"),
	)
}

// persistDesignDocumentCompletion writes the revision and moves the draft
// pointer inside the caller's terminal transaction, so the task reaching a
// completed state and the draft appearing are the same commit. Split, a crash
// could leave a revision nothing points at, a pointer aimed at a row that was
// never finished, or a task marked done with no draft to show for it.
//
// saved is untouched here. A new draft never changes what downstream reads;
// only an explicit user save moves that pointer (DC-034).
func persistDesignDocumentCompletion(
	ctx context.Context,
	queries *db.Queries,
	completedTask db.AgentTaskQueue,
	prepared preparedDesignDocumentCompletion,
) (db.DesignDocument, error) {
	// Lock the document so two tasks reporting at once cannot claim the same
	// revision number or interleave their pointer moves.
	if _, err := queries.GetDesignDocumentInWorkspaceForUpdate(ctx, db.GetDesignDocumentInWorkspaceForUpdateParams{
		ID: prepared.DocumentID, WorkspaceID: prepared.WorkspaceID,
	}); err != nil {
		return db.DesignDocument{}, fmt.Errorf("lock design document: %w", err)
	}

	nextNumber, err := queries.GetNextDesignDocumentRevisionNumber(ctx, prepared.DocumentID)
	if err != nil {
		return db.DesignDocument{}, fmt.Errorf("allocate design document revision number: %w", err)
	}

	manifestJSON, err := json.Marshal(prepared.Validated.Manifest)
	if err != nil {
		return db.DesignDocument{}, fmt.Errorf("encode design document manifest: %w", err)
	}
	auditJSON, err := json.Marshal(prepared.Validated.Audit)
	if err != nil {
		return db.DesignDocument{}, fmt.Errorf("encode design document audit: %w", err)
	}
	previewJSON, err := json.Marshal(prepared.Receipt.Preview)
	if err != nil {
		return db.DesignDocument{}, fmt.Errorf("encode design document preview receipt: %w", err)
	}
	indexJSON, err := json.Marshal(prepared.Validated.Manifest.Files)
	if err != nil {
		return db.DesignDocument{}, fmt.Errorf("encode design document artifact index: %w", err)
	}

	baseRevisionID := pgtype.UUID{}
	if prepared.TaskContext.BaseRevisionID != "" {
		parsed, err := util.ParseUUID(prepared.TaskContext.BaseRevisionID)
		if err != nil {
			return db.DesignDocument{}, errors.New("invalid design document base revision id")
		}
		baseRevisionID = parsed
	}

	repositoryGrounding := prepared.RepositoryGrounding
	if len(repositoryGrounding) == 0 && baseRevisionID.Valid {
		baseRevision, err := queries.GetDesignDocumentRevisionInWorkspace(ctx, db.GetDesignDocumentRevisionInWorkspaceParams{
			ID: baseRevisionID, WorkspaceID: prepared.WorkspaceID,
		})
		if err != nil {
			return db.DesignDocument{}, fmt.Errorf("load design document base revision grounding: %w", err)
		}
		if baseRevision.DesignDocumentID != prepared.DocumentID {
			return db.DesignDocument{}, errors.New("design document base revision does not belong to this document")
		}
		repositoryGrounding = baseRevision.RepositoryGrounding
	}

	revision, err := queries.CreateDesignDocumentRevision(ctx, db.CreateDesignDocumentRevisionParams{
		WorkspaceID:         prepared.WorkspaceID,
		DesignDocumentID:    prepared.DocumentID,
		RevisionNumber:      int32(nextNumber),
		PackageSchema:       designdocument.PackageSchemaV1,
		ContentDigest:       prepared.Validated.Manifest.ContentDigest,
		ArchiveObjectKey:    prepared.Receipt.ObjectKey,
		ArtifactIndex:       indexJSON,
		Manifest:            manifestJSON,
		Brief:               prepared.Brief,
		Coverage:            prepared.Coverage,
		Audit:               auditJSON,
		Preview:             previewJSON,
		InputSnapshotSha256: prepared.TaskContext.InputSnapshotSHA256,
		BaseRevisionID:      baseRevisionID,
		DesignSystemDigest:  pgtype.Text{String: prepared.TaskContext.DesignSystemDigest, Valid: prepared.TaskContext.DesignSystemDigest != ""},
		SourceTaskID:        completedTask.ID,
		AgentID:             prepared.AgentID,
		Instruction:         pgtype.Text{String: prepared.TaskContext.Instruction, Valid: prepared.TaskContext.Instruction != ""},
		Scope:               prepared.TaskContext.Scope,
		RepositoryGrounding: repositoryGrounding,
	})
	if err != nil {
		return db.DesignDocument{}, fmt.Errorf("create design document revision: %w", err)
	}

	document, err := queries.SetDesignDocumentDraftRevision(ctx, db.SetDesignDocumentDraftRevisionParams{
		ID:              prepared.DocumentID,
		WorkspaceID:     prepared.WorkspaceID,
		DraftRevisionID: revision.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// The query only matches when the revision belongs to this document,
		// so no rows means the two disagreed — never point at it.
		return db.DesignDocument{}, errors.New("design document draft revision does not belong to this document")
	}
	if err != nil {
		return db.DesignDocument{}, fmt.Errorf("move design document draft pointer: %w", err)
	}
	return document, nil
}
