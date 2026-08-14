package handler

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/designdocument"
	"github.com/multica-ai/multica/server/internal/designpreview"
	"github.com/multica-ai/multica/server/internal/storage"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type preparedDesignDocumentPackage struct {
	DocumentID     pgtype.UUID
	RevisionID     pgtype.UUID
	Title          string
	Snapshot       designDocumentSnapshotParams
	SnapshotDigest string
	Archive        []byte
}

func prepareDesignDocumentPackageCompletion(ctx context.Context, task db.AgentTaskQueue, receipt *DesignDocumentPackageReceipt, store storage.Storage) (preparedDesignDocumentPackage, error) {
	if receipt == nil || receipt.SchemaVersion != designdocument.SchemaVersion || store == nil {
		return preparedDesignDocumentPackage{}, errors.New("Design Document package receipt is required")
	}
	documentID, documentErr := parseUUIDValue(receipt.DocumentID)
	revisionID, revisionErr := parseUUIDValue(receipt.RevisionID)
	if documentErr != nil || revisionErr != nil {
		return preparedDesignDocumentPackage{}, errors.New("Design Document package identity is invalid")
	}
	grounding, err := prepareDesignDocumentGroundingCompletion(task, &receipt.Grounding)
	if err != nil {
		return preparedDesignDocumentPackage{}, err
	}
	snapshot, snapshotDigest, title, err := designDocumentGroundedSnapshot(task, grounding.Grounding)
	if err != nil || receipt.InputSnapshotSHA256 != snapshotDigest {
		return preparedDesignDocumentPackage{}, errors.New("Design Document package snapshot binding is invalid")
	}
	binding := designDocumentPersistenceBinding(designDocumentFirstRevisionParams{DocumentID: documentID, RevisionID: revisionID, Snapshot: snapshot}, snapshotDigest)
	wantKey, err := designdocument.ArchiveObjectKey(binding, receipt.ContentDigest)
	if err != nil || receipt.ObjectKey != wantKey {
		return preparedDesignDocumentPackage{}, errors.New("Design Document package object binding is invalid")
	}
	archive, validated, err := designdocument.LoadArchive(ctx, store, receipt.ObjectKey, binding)
	if err != nil || validated.Manifest.ContentDigest != receipt.ContentDigest || !validated.Audit.Passed {
		return preparedDesignDocumentPackage{}, errors.New("Design Document package failed independent validation")
	}
	if !reflect.DeepEqual(receipt.ArtifactIndex, validated.Manifest.Files) || !reflect.DeepEqual(receipt.Audit, validated.Audit) {
		return preparedDesignDocumentPackage{}, errors.New("Design Document package Audit or artifact index does not match")
	}
	required := make(map[string]bool, len(validated.Coverage.Interactions))
	for _, interaction := range validated.Coverage.Interactions {
		required[interaction.TargetID] = true
	}
	targets := make([]designpreview.Target, len(validated.Manifest.PreviewTargets))
	for i, target := range validated.Manifest.PreviewTargets {
		targets[i] = designpreview.Target{Kind: "preview", ID: target.ID, Path: target.Path}
	}
	if !receipt.Preview.Verification.Passed || designpreview.ValidateReceiptWithInteractions(receipt.Preview, receipt.ContentDigest, targets, required) != nil {
		return preparedDesignDocumentPackage{}, errors.New("Design Document Preview receipt is invalid")
	}
	return preparedDesignDocumentPackage{DocumentID: documentID, RevisionID: revisionID, Title: title, Snapshot: snapshot, SnapshotDigest: snapshotDigest, Archive: archive}, nil
}

func persistDesignDocumentPackageCompletion(ctx context.Context, queries *db.Queries, task db.AgentTaskQueue, prepared preparedDesignDocumentPackage) error {
	created, err := createDesignDocumentWithFirstRevision(ctx, queries, designDocumentFirstRevisionParams{
		DocumentID: prepared.DocumentID, RevisionID: prepared.RevisionID, Title: prepared.Title,
		CreatedBy: task.OriginatorUserID, Snapshot: prepared.Snapshot, Archive: prepared.Archive,
	})
	if err != nil {
		return err
	}
	updated, err := queries.SetDesignDocumentTaskInputSnapshot(ctx, db.SetDesignDocumentTaskInputSnapshotParams{
		Input: prepared.Snapshot.Snapshot, InputSnapshotID: uuidToString(created.InputSnapshotID), InputSnapshotSha256: prepared.SnapshotDigest,
		ID: task.ID, AgentID: task.AgentID,
	})
	if err != nil {
		return err
	}
	if updated != 1 {
		return errors.New("Design Document task snapshot binding changed")
	}
	return nil
}

func designDocumentGroundedSnapshot(task db.AgentTaskQueue, grounding json.RawMessage) (designDocumentSnapshotParams, string, string, error) {
	var taskContext struct {
		Input       json.RawMessage `json:"input"`
		WorkspaceID string          `json:"workspace_id"`
		ProjectID   string          `json:"project_id"`
		IssueID     string          `json:"issue_id"`
	}
	if err := json.Unmarshal(task.Context, &taskContext); err != nil {
		return designDocumentSnapshotParams{}, "", "", err
	}
	snapshotJSON, snapshotDigest, err := designdocument.SnapshotWithRepositoryGrounding(taskContext.Input, grounding)
	if err != nil {
		return designDocumentSnapshotParams{}, "", "", err
	}
	var input struct {
		SchemaVersion, TargetPlatform, Requirement string
		DesignSystem                               *struct {
			ID, SourceTaskID, ContentDigest string
		} `json:"design_system"`
	}
	if err := json.Unmarshal(snapshotJSON, &input); err != nil {
		return designDocumentSnapshotParams{}, "", "", err
	}
	workspaceID, _ := parseUUIDValue(taskContext.WorkspaceID)
	projectID, _ := parseUUIDValue(taskContext.ProjectID)
	var issueID pgtype.UUID
	if taskContext.IssueID != "" {
		issueID, _ = parseUUIDValue(taskContext.IssueID)
	}
	params := designDocumentSnapshotParams{
		WorkspaceID: workspaceID, ProjectID: projectID, IssueID: issueID, TaskID: task.ID, AgentID: task.AgentID,
		TargetPlatform: pgtype.Text{String: input.TargetPlatform, Valid: input.TargetPlatform != ""}, SchemaVersion: input.SchemaVersion, Snapshot: snapshotJSON,
	}
	if input.DesignSystem != nil {
		params.DesignSystemID, _ = parseUUIDValue(input.DesignSystem.ID)
		params.DesignSystemSourceTaskID, _ = parseUUIDValue(input.DesignSystem.SourceTaskID)
		params.DesignSystemContentDigest = input.DesignSystem.ContentDigest
	}
	title := strings.TrimSpace(input.Requirement)
	if utf8.RuneCountInString(title) > 160 {
		title = string([]rune(title)[:160])
	}
	return params, snapshotDigest, title, nil
}
