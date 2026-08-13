package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/designdocument"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type preparedDesignDocumentGrounding struct {
	Grounding json.RawMessage
}

func isDesignDocumentTaskContext(raw json.RawMessage) bool {
	var value struct {
		Type string `json:"type"`
	}
	return json.Unmarshal(raw, &value) == nil && value.Type == designDocumentTaskContextType
}

func prepareDesignDocumentGroundingCompletion(task db.AgentTaskQueue, receipt *designdocument.RepositoryGrounding) (preparedDesignDocumentGrounding, error) {
	if receipt == nil {
		return preparedDesignDocumentGrounding{}, errors.New("Design Document grounding receipt is required")
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return preparedDesignDocumentGrounding{}, err
	}
	validated, err := designdocument.ValidateRepositoryGrounding(raw)
	if err != nil {
		return preparedDesignDocumentGrounding{}, err
	}
	var taskContext struct {
		Type           string          `json:"type"`
		TaskProtocol   string          `json:"task_protocol"`
		Operation      string          `json:"operation"`
		ExecutionReady bool            `json:"execution_ready"`
		Input          json.RawMessage `json:"input"`
		WorkspaceID    string          `json:"workspace_id"`
		ProjectID      string          `json:"project_id"`
		IssueID        string          `json:"issue_id"`
		AgentID        string          `json:"agent_id"`
		TargetPlatform string          `json:"target_platform"`
	}
	if json.Unmarshal(task.Context, &taskContext) != nil || taskContext.Type != designDocumentTaskContextType || taskContext.TaskProtocol != designDocumentTaskSchema || taskContext.Operation != "first_generation" || !taskContext.ExecutionReady {
		return preparedDesignDocumentGrounding{}, errors.New("task is not an execution-ready Design Document task")
	}
	if taskContext.AgentID != uuidToString(task.AgentID) || taskContext.ProjectID == "" || taskContext.WorkspaceID == "" {
		return preparedDesignDocumentGrounding{}, errors.New("Design Document task identity is invalid")
	}
	_, workspaceErr := parseUUIDValue(taskContext.WorkspaceID)
	_, projectErr := parseUUIDValue(taskContext.ProjectID)
	agentID, agentErr := parseUUIDValue(taskContext.AgentID)
	if workspaceErr != nil || projectErr != nil || agentErr != nil || agentID != task.AgentID {
		return preparedDesignDocumentGrounding{}, errors.New("Design Document task identity is invalid")
	}
	var issueID pgtype.UUID
	if taskContext.IssueID != "" {
		var issueErr error
		issueID, issueErr = parseUUIDValue(taskContext.IssueID)
		if issueErr != nil {
			return preparedDesignDocumentGrounding{}, errors.New("Design Document task issue identity is invalid")
		}
	}
	if issueID != task.IssueID {
		return preparedDesignDocumentGrounding{}, errors.New("Design Document task issue identity is invalid")
	}
	var input designDocumentTaskSnapshot
	if json.Unmarshal(taskContext.Input, &input) != nil || input.SchemaVersion != designDocumentInputSchema {
		return preparedDesignDocumentGrounding{}, errors.New("Design Document task input is invalid")
	}
	if err := validateDesignDocumentTaskInputIdentity(input, taskContext.WorkspaceID, taskContext.ProjectID, taskContext.AgentID, issueID); err != nil {
		return preparedDesignDocumentGrounding{}, err
	}
	if input.TargetPlatform != taskContext.TargetPlatform {
		return preparedDesignDocumentGrounding{}, errors.New("Design Document task input identity is invalid")
	}
	if input.AttachmentSourceTaskID != "" {
		sourceID, err := parseUUIDValue(input.AttachmentSourceTaskID)
		if err != nil || !task.RerunOfTaskID.Valid || sourceID != task.RerunOfTaskID {
			return preparedDesignDocumentGrounding{}, errors.New("Design Document task attachment provenance is invalid")
		}
	} else if task.RerunOfTaskID.Valid {
		return preparedDesignDocumentGrounding{}, errors.New("Design Document task attachment provenance is invalid")
	}
	if input.RepositoryGrounding != "pending" && input.RepositoryGrounding != designdocument.GroundingUnavailable {
		return preparedDesignDocumentGrounding{}, errors.New("Design Document repository grounding mode is invalid")
	}
	if (input.RepositoryGrounding == "unavailable") != (validated.Status == designdocument.GroundingUnavailable) {
		return preparedDesignDocumentGrounding{}, errors.New("repository grounding receipt does not match the explicit task mode")
	}
	validatedRaw, _ := json.Marshal(validated)
	return preparedDesignDocumentGrounding{Grounding: validatedRaw}, nil
}

func persistDesignDocumentGroundingCompletion(ctx context.Context, queries *db.Queries, task db.AgentTaskQueue, prepared preparedDesignDocumentGrounding) error {
	var taskContext struct {
		Input       json.RawMessage `json:"input"`
		WorkspaceID string          `json:"workspace_id"`
		ProjectID   string          `json:"project_id"`
		IssueID     string          `json:"issue_id"`
		AgentID     string          `json:"agent_id"`
	}
	if err := json.Unmarshal(task.Context, &taskContext); err != nil {
		return err
	}
	snapshotJSON, snapshotDigest, err := designdocument.SnapshotWithRepositoryGrounding(taskContext.Input, prepared.Grounding)
	if err != nil {
		return err
	}
	var input struct {
		SchemaVersion  string `json:"schema_version"`
		TargetPlatform string `json:"target_platform"`
		DesignSystem   *struct {
			ID            string `json:"id"`
			SourceTaskID  string `json:"source_task_id"`
			ContentDigest string `json:"content_digest"`
		} `json:"design_system"`
	}
	if err := json.Unmarshal(snapshotJSON, &input); err != nil {
		return err
	}
	workspaceID, _ := parseUUIDValue(taskContext.WorkspaceID)
	projectID, _ := parseUUIDValue(taskContext.ProjectID)
	var issueID pgtype.UUID
	if taskContext.IssueID != "" {
		issueID, _ = parseUUIDValue(taskContext.IssueID)
	}
	params := designDocumentSnapshotParams{
		WorkspaceID: workspaceID, ProjectID: projectID, IssueID: issueID,
		TaskID: task.ID, AgentID: task.AgentID, TargetPlatform: pgtype.Text{String: input.TargetPlatform, Valid: input.TargetPlatform != ""},
		SchemaVersion: input.SchemaVersion, Snapshot: snapshotJSON,
	}
	if input.DesignSystem != nil {
		params.DesignSystemID, _ = parseUUIDValue(input.DesignSystem.ID)
		params.DesignSystemSourceTaskID, _ = parseUUIDValue(input.DesignSystem.SourceTaskID)
		params.DesignSystemContentDigest = input.DesignSystem.ContentDigest
	}
	snapshot, err := createDesignDocumentInputSnapshot(ctx, queries, params)
	if err != nil {
		return fmt.Errorf("create Design Document input snapshot: %w", err)
	}
	if snapshot.SnapshotSha256 != snapshotDigest {
		return errors.New("Design Document input snapshot digest mismatch")
	}
	updated, err := queries.SetDesignDocumentTaskInputSnapshot(ctx, db.SetDesignDocumentTaskInputSnapshotParams{
		Input: snapshotJSON, InputSnapshotID: uuidToString(snapshot.ID), InputSnapshotSha256: snapshotDigest, ID: task.ID, AgentID: task.AgentID,
	})
	if err != nil {
		return err
	}
	if updated != 1 {
		return errors.New("Design Document task snapshot binding changed")
	}
	return nil
}
