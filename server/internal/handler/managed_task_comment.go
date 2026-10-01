package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var errManagedTaskAlreadyCompleted = errors.New("managed task already completed")

type managedTaskCommentResult struct {
	comment       db.Comment
	issueRevision int64
	reused        bool
}

func (h *Handler) createManagedTaskComment(ctx context.Context, params db.CreateCommentParams, hasAttachments bool) (managedTaskCommentResult, error) {
	tx, err := h.beginWakeupWrite(ctx)
	if err != nil {
		return managedTaskCommentResult{}, fmt.Errorf("begin managed task comment: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	task, err := qtx.LockAgentTaskForManagedComment(ctx, params.SourceTaskID)
	if err != nil {
		return managedTaskCommentResult{}, fmt.Errorf("lock source task: %w", err)
	}
	if task.AgentID != params.AuthorID {
		return managedTaskCommentResult{}, errors.New("source task agent does not match comment author")
	}

	if completedTaskHasManagedIssueCompletion(task, params.IssueID) {
		if hasAttachments {
			return managedTaskCommentResult{}, errManagedTaskAlreadyCompleted
		}
		existing, lookupErr := findCanonicalManagedTaskFinal(ctx, qtx, params)
		if errors.Is(lookupErr, pgx.ErrNoRows) {
			return managedTaskCommentResult{}, errManagedTaskAlreadyCompleted
		}
		if lookupErr != nil {
			return managedTaskCommentResult{}, fmt.Errorf("find managed task final comment: %w", lookupErr)
		}
		issue, issueErr := qtx.GetIssue(ctx, params.IssueID)
		if issueErr != nil {
			return managedTaskCommentResult{}, fmt.Errorf("load managed task issue: %w", issueErr)
		}
		if err := tx.Commit(ctx); err != nil {
			return managedTaskCommentResult{}, fmt.Errorf("commit managed task comment lookup: %w", err)
		}
		return managedTaskCommentResult{comment: existing, issueRevision: issue.Revision, reused: true}, nil
	}

	created, err := qtx.CreateComment(ctx, params)
	if err != nil {
		return managedTaskCommentResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return managedTaskCommentResult{}, fmt.Errorf("commit managed task comment: %w", err)
	}
	return managedTaskCommentResult{comment: created.Comment(), issueRevision: created.IssueRevision}, nil
}

func findCanonicalManagedTaskFinal(ctx context.Context, qtx *db.Queries, params db.CreateCommentParams) (db.Comment, error) {
	canonical := service.CanonicalIssueCompletionComment(params.Content)
	existing, err := qtx.GetExactAgentTaskFinalComment(ctx, db.GetExactAgentTaskFinalCommentParams{
		IssueID:      params.IssueID,
		WorkspaceID:  params.WorkspaceID,
		AuthorID:     params.AuthorID,
		SourceTaskID: params.SourceTaskID,
		ParentID:     params.ParentID,
		Content:      canonical,
	})
	if err == nil || !errors.Is(err, pgx.ErrNoRows) {
		return existing, err
	}
	candidates, err := qtx.ListAgentTaskFinalCommentsForParent(ctx, db.ListAgentTaskFinalCommentsForParentParams{
		IssueID:      params.IssueID,
		WorkspaceID:  params.WorkspaceID,
		AuthorID:     params.AuthorID,
		SourceTaskID: params.SourceTaskID,
		ParentID:     params.ParentID,
	})
	if err != nil {
		return db.Comment{}, err
	}
	for _, candidate := range candidates {
		if service.CanonicalIssueCompletionComment(candidate.Content) == canonical {
			return candidate, nil
		}
	}
	return db.Comment{}, pgx.ErrNoRows
}

func completedTaskHasManagedIssueCompletion(task db.AgentTaskQueue, issueID pgtype.UUID) bool {
	if task.Status != "completed" || !task.IssueID.Valid || task.IssueID != issueID || len(task.Result) == 0 {
		return false
	}
	var result struct {
		IssueCompletion *IssueCompletionRequest `json:"issue_completion"`
	}
	return json.Unmarshal(task.Result, &result) == nil && result.IssueCompletion != nil && result.IssueCompletion.Version == issueCompletionContractVersion
}
