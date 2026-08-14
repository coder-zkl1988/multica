package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/designdocument"
	"github.com/multica-ai/multica/server/internal/designpackage"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type designDocumentSnapshotParams struct {
	WorkspaceID               pgtype.UUID
	ProjectID                 pgtype.UUID
	IssueID                   pgtype.UUID
	TaskID                    pgtype.UUID
	AgentID                   pgtype.UUID
	TargetPlatform            pgtype.Text
	SchemaVersion             string
	Snapshot                  json.RawMessage
	BaseRevisionID            pgtype.UUID
	BaseContentDigest         string
	DesignSystemID            pgtype.UUID
	DesignSystemSourceTaskID  pgtype.UUID
	DesignSystemContentDigest string
}

type designDocumentFirstRevisionParams struct {
	DocumentID pgtype.UUID
	RevisionID pgtype.UUID
	Title      string
	CreatedBy  pgtype.UUID
	Snapshot   designDocumentSnapshotParams
	Archive    []byte
}

type designDocumentAdjustmentRevisionParams struct {
	DocumentID pgtype.UUID
	RevisionID pgtype.UUID
	Snapshot   designDocumentSnapshotParams
	Archive    []byte
}

type designDocumentPointerParams struct {
	WorkspaceID                pgtype.UUID
	ProjectID                  pgtype.UUID
	DocumentID                 pgtype.UUID
	ExpectedDraftRevisionID    pgtype.UUID
	ExpectedDraftContentDigest string
}

func createDesignDocumentInputSnapshot(ctx context.Context, queries *db.Queries, input designDocumentSnapshotParams) (db.DesignDocumentInputSnapshot, error) {
	if input.BaseRevisionID.Valid || input.BaseContentDigest != "" {
		return db.DesignDocumentInputSnapshot{}, errors.New("standalone design document snapshot cannot have base provenance")
	}
	digest, err := designpackage.CanonicalJSONDigest(input.Snapshot, "design document input snapshot")
	if err != nil {
		return db.DesignDocumentInputSnapshot{}, err
	}
	return queries.CreateDesignDocumentInputSnapshot(ctx, db.CreateDesignDocumentInputSnapshotParams{
		WorkspaceID: input.WorkspaceID, ProjectID: input.ProjectID, IssueID: input.IssueID,
		TaskID: input.TaskID, AgentID: input.AgentID, TargetPlatform: input.TargetPlatform,
		SchemaVersion: input.SchemaVersion, Snapshot: input.Snapshot, SnapshotSha256: digest,
		BaseRevisionID:    input.BaseRevisionID,
		BaseContentDigest: optionalText(input.BaseContentDigest), DesignSystemID: input.DesignSystemID,
		DesignSystemSourceTaskID:  input.DesignSystemSourceTaskID,
		DesignSystemContentDigest: optionalText(input.DesignSystemContentDigest),
	})
}

func createDesignDocumentWithFirstRevision(ctx context.Context, queries *db.Queries, input designDocumentFirstRevisionParams) (db.CreateDesignDocumentWithInputSnapshotAndFirstRevisionRow, error) {
	if input.Snapshot.BaseRevisionID.Valid || input.Snapshot.BaseContentDigest != "" {
		return db.CreateDesignDocumentWithInputSnapshotAndFirstRevisionRow{}, errors.New("first design document revision cannot have base provenance")
	}
	snapshotDigest, err := designpackage.CanonicalJSONDigest(input.Snapshot.Snapshot, "design document input snapshot")
	if err != nil {
		return db.CreateDesignDocumentWithInputSnapshotAndFirstRevisionRow{}, err
	}
	binding := designDocumentPersistenceBinding(input.DocumentID, input.RevisionID, input.Snapshot, snapshotDigest)
	validated, err := designdocument.ValidateArchive(input.Archive, binding)
	if err != nil {
		return db.CreateDesignDocumentWithInputSnapshotAndFirstRevisionRow{}, err
	}
	manifest, err := json.Marshal(validated.Manifest)
	if err != nil {
		return db.CreateDesignDocumentWithInputSnapshotAndFirstRevisionRow{}, fmt.Errorf("encode design document manifest: %w", err)
	}
	artifactIndex, err := json.Marshal(validated.Manifest.Files)
	if err != nil {
		return db.CreateDesignDocumentWithInputSnapshotAndFirstRevisionRow{}, fmt.Errorf("encode design document artifact index: %w", err)
	}
	objectKey, err := designdocument.ArchiveObjectKey(binding, validated.Manifest.ContentDigest)
	if err != nil {
		return db.CreateDesignDocumentWithInputSnapshotAndFirstRevisionRow{}, err
	}
	return queries.CreateDesignDocumentWithInputSnapshotAndFirstRevision(ctx, db.CreateDesignDocumentWithInputSnapshotAndFirstRevisionParams{
		WorkspaceID: input.Snapshot.WorkspaceID, ProjectID: input.Snapshot.ProjectID,
		IssueID: input.Snapshot.IssueID, TaskID: input.Snapshot.TaskID, AgentID: input.Snapshot.AgentID,
		TargetPlatform: input.Snapshot.TargetPlatform, SnapshotSchemaVersion: input.Snapshot.SchemaVersion,
		Snapshot: input.Snapshot.Snapshot, SnapshotSha256: snapshotDigest,
		DesignSystemID:            input.Snapshot.DesignSystemID,
		DesignSystemSourceTaskID:  input.Snapshot.DesignSystemSourceTaskID,
		DesignSystemContentDigest: optionalText(input.Snapshot.DesignSystemContentDigest),
		RevisionID:                input.RevisionID, DocumentID: input.DocumentID,
		RevisionSchemaVersion: designdocument.SchemaVersion, Manifest: manifest,
		ArtifactIndex: artifactIndex, ArchiveObjectKey: objectKey,
		ContentDigest: validated.Manifest.ContentDigest,
		Title:         input.Title, CreatedBy: input.CreatedBy,
	})
}

func createDesignDocumentAdjustmentRevision(ctx context.Context, queries *db.Queries, input designDocumentAdjustmentRevisionParams) (db.CreateDesignDocumentAdjustmentRevisionRow, error) {
	if !input.Snapshot.BaseRevisionID.Valid || input.Snapshot.BaseContentDigest == "" {
		return db.CreateDesignDocumentAdjustmentRevisionRow{}, errors.New("design document adjustment requires base provenance")
	}
	snapshotDigest, err := designpackage.CanonicalJSONDigest(input.Snapshot.Snapshot, "design document input snapshot")
	if err != nil {
		return db.CreateDesignDocumentAdjustmentRevisionRow{}, err
	}
	binding := designDocumentPersistenceBinding(input.DocumentID, input.RevisionID, input.Snapshot, snapshotDigest)
	validated, err := designdocument.ValidateArchive(input.Archive, binding)
	if err != nil {
		return db.CreateDesignDocumentAdjustmentRevisionRow{}, err
	}
	manifest, err := json.Marshal(validated.Manifest)
	if err != nil {
		return db.CreateDesignDocumentAdjustmentRevisionRow{}, fmt.Errorf("encode design document manifest: %w", err)
	}
	artifactIndex, err := json.Marshal(validated.Manifest.Files)
	if err != nil {
		return db.CreateDesignDocumentAdjustmentRevisionRow{}, fmt.Errorf("encode design document artifact index: %w", err)
	}
	objectKey, err := designdocument.ArchiveObjectKey(binding, validated.Manifest.ContentDigest)
	if err != nil {
		return db.CreateDesignDocumentAdjustmentRevisionRow{}, err
	}
	return queries.CreateDesignDocumentAdjustmentRevision(ctx, db.CreateDesignDocumentAdjustmentRevisionParams{
		DocumentID: input.DocumentID, WorkspaceID: input.Snapshot.WorkspaceID,
		ProjectID: input.Snapshot.ProjectID, BaseRevisionID: input.Snapshot.BaseRevisionID,
		BaseContentDigest: input.Snapshot.BaseContentDigest, TaskID: input.Snapshot.TaskID,
		AgentID: input.Snapshot.AgentID, TargetPlatform: input.Snapshot.TargetPlatform,
		SnapshotSchemaVersion: input.Snapshot.SchemaVersion, Snapshot: input.Snapshot.Snapshot,
		SnapshotSha256: snapshotDigest, DesignSystemID: input.Snapshot.DesignSystemID,
		DesignSystemSourceTaskID:  input.Snapshot.DesignSystemSourceTaskID,
		DesignSystemContentDigest: optionalText(input.Snapshot.DesignSystemContentDigest),
		Manifest:                  manifest, RevisionSchemaVersion: designdocument.SchemaVersion,
		RevisionID: input.RevisionID, ContentDigest: validated.Manifest.ContentDigest,
		ArtifactIndex: artifactIndex, ArchiveObjectKey: objectKey,
	})
}

func saveDesignDocumentDraft(ctx context.Context, queries *db.Queries, input designDocumentPointerParams) (db.DesignDocument, error) {
	return queries.SaveDesignDocumentDraft(ctx, db.SaveDesignDocumentDraftParams{
		DocumentID: input.DocumentID, WorkspaceID: input.WorkspaceID, ProjectID: input.ProjectID,
		ExpectedDraftRevisionID:    input.ExpectedDraftRevisionID,
		ExpectedDraftContentDigest: input.ExpectedDraftContentDigest,
	})
}

func discardDesignDocumentDraft(ctx context.Context, queries *db.Queries, input designDocumentPointerParams) (db.DesignDocument, error) {
	return queries.DiscardDesignDocumentDraft(ctx, db.DiscardDesignDocumentDraftParams{
		DocumentID: input.DocumentID, WorkspaceID: input.WorkspaceID, ProjectID: input.ProjectID,
		ExpectedDraftRevisionID:    input.ExpectedDraftRevisionID,
		ExpectedDraftContentDigest: input.ExpectedDraftContentDigest,
	})
}

func designDocumentPersistenceBinding(documentID, revisionID pgtype.UUID, snapshot designDocumentSnapshotParams, snapshotDigest string) designdocument.Binding {
	return designdocument.Binding{
		DocumentID: uuidToString(documentID), RevisionID: uuidToString(revisionID),
		WorkspaceID: uuidToString(snapshot.WorkspaceID), ProjectID: uuidToString(snapshot.ProjectID),
		IssueID: uuidToString(snapshot.IssueID), TaskID: uuidToString(snapshot.TaskID),
		AgentID: uuidToString(snapshot.AgentID), TargetPlatform: snapshot.TargetPlatform.String,
		InputSnapshotSHA256: snapshotDigest, BaseRevisionID: uuidToString(snapshot.BaseRevisionID),
		BaseContentDigest: snapshot.BaseContentDigest, DesignSystemID: uuidToString(snapshot.DesignSystemID),
		DesignSystemSourceTaskID:  uuidToString(snapshot.DesignSystemSourceTaskID),
		DesignSystemContentDigest: snapshot.DesignSystemContentDigest,
	}
}

func optionalText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}
