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
	binding := designDocumentPersistenceBinding(input, snapshotDigest)
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

func designDocumentPersistenceBinding(input designDocumentFirstRevisionParams, snapshotDigest string) designdocument.Binding {
	return designdocument.Binding{
		DocumentID: uuidToString(input.DocumentID), RevisionID: uuidToString(input.RevisionID),
		WorkspaceID: uuidToString(input.Snapshot.WorkspaceID), ProjectID: uuidToString(input.Snapshot.ProjectID),
		IssueID: uuidToString(input.Snapshot.IssueID), TaskID: uuidToString(input.Snapshot.TaskID),
		AgentID: uuidToString(input.Snapshot.AgentID), TargetPlatform: input.Snapshot.TargetPlatform.String,
		InputSnapshotSHA256: snapshotDigest, DesignSystemID: uuidToString(input.Snapshot.DesignSystemID),
		DesignSystemSourceTaskID:  uuidToString(input.Snapshot.DesignSystemSourceTaskID),
		DesignSystemContentDigest: input.Snapshot.DesignSystemContentDigest,
	}
}

func optionalText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}
