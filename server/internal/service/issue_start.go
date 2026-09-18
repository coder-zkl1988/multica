package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var (
	ErrInvalidIssueStartContract = errors.New("invalid issue start contract")
	ErrStaleIssueStartClaim      = errors.New("stale issue start claim generation")
)

type IssueStart struct {
	Version         int
	ClaimGeneration int64
	BaseRevision    int64
	BaseStatus      string
}

type IssueStartState struct {
	Applied             bool
	BaselineAccepted    bool
	Issue               db.Issue
	EmptyCommentHistory *protocol.EmptyIssueCommentHistory `json:"empty_comment_history,omitempty"`
}

func applyIssueStart(ctx context.Context, qtx *db.Queries, task db.AgentTaskQueue, start IssueStart) (IssueStartState, error) {
	var state IssueStartState
	if start.Version != 1 || !ordinaryIssueCompletionSupportedTask(task) || start.ClaimGeneration <= 0 {
		return state, ErrInvalidIssueStartContract
	}
	if !task.DispatchedAt.Valid || task.DispatchedAt.Time.UnixMicro() != start.ClaimGeneration {
		return state, ErrStaleIssueStartClaim
	}
	baseIssue, err := qtx.GetIssue(ctx, task.IssueID)
	if err != nil {
		return state, err
	}
	issue, err := qtx.LockIssueForDescriptionUpdate(ctx, db.LockIssueForDescriptionUpdateParams{
		ID: task.IssueID, WorkspaceID: baseIssue.WorkspaceID,
	})
	if err != nil {
		return state, err
	}
	state.Issue = issue
	if issue.Revision != start.BaseRevision || issue.Status != start.BaseStatus || !issueTaskAssignmentMatches(issue, task) {
		return state, nil
	}
	status, err := issuestatus.Resolve(ctx, qtx, issue.WorkspaceID, issue.Status)
	if errors.Is(err, issuestatus.ErrUnknownStatus) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if status.Category == issuestatus.CategoryStarted {
		state.BaselineAccepted = true
		return captureIssueStartCommentHistory(ctx, qtx, task, start.ClaimGeneration, state)
	}
	if status.Category != issuestatus.CategoryUnstarted {
		return state, nil
	}
	updated, err := qtx.UpdateIssue(ctx, db.UpdateIssueParams{
		ID:               task.IssueID,
		ExpectedRevision: pgtype.Int8{Int64: start.BaseRevision, Valid: true},
		Status:           pgtype.Text{String: "in_progress", Valid: true},
		AssigneeType:     issue.AssigneeType,
		AssigneeID:       issue.AssigneeID,
		StartDate:        issue.StartDate,
		DueDate:          issue.DueDate,
		ParentIssueID:    issue.ParentIssueID,
		ProjectID:        issue.ProjectID,
		Stage:            issue.Stage,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	state.Applied = true
	state.BaselineAccepted = true
	state.Issue = updated
	return captureIssueStartCommentHistory(ctx, qtx, task, start.ClaimGeneration, state)
}

// Both accepted branches retain the issue row lock through this query and commit.
func captureIssueStartCommentHistory(ctx context.Context, qtx *db.Queries, task db.AgentTaskQueue, generation int64, state IssueStartState) (IssueStartState, error) {
	comments, err := qtx.ListCommentsForIssue(ctx, db.ListCommentsForIssueParams{
		IssueID: state.Issue.ID, WorkspaceID: state.Issue.WorkspaceID, Limit: 1,
	})
	if err != nil {
		return state, err
	}
	if len(comments) == 0 {
		now := time.Now().UTC()
		state.EmptyCommentHistory = &protocol.EmptyIssueCommentHistory{
			Version:         1,
			TaskID:          util.UUIDToString(task.ID),
			WorkspaceID:     util.UUIDToString(state.Issue.WorkspaceID),
			IssueID:         util.UUIDToString(state.Issue.ID),
			ClaimGeneration: generation,
			Revision:        state.Issue.Revision,
			CapturedAt:      now.Format(time.RFC3339Nano),
			ExpiresAt:       now.Add(5 * time.Minute).Format(time.RFC3339Nano),
		}
	}
	return state, nil
}

func ordinaryIssueCompletionSupportedTask(task db.AgentTaskQueue) bool {
	if !task.IssueID.Valid || task.ChatSessionID.Valid || task.AutopilotRunID.Valid {
		return false
	}
	var typed struct {
		Type string `json:"type"`
	}
	return len(task.Context) == 0 || json.Unmarshal(task.Context, &typed) != nil || typed.Type == ""
}

func issueTaskAssignmentMatches(issue db.Issue, task db.AgentTaskQueue) bool {
	if issue.AssigneeType.Valid && issue.AssigneeType.String == "agent" && issue.AssigneeID.Valid {
		return issue.AssigneeID == task.AgentID
	}
	return task.IsLeaderTask && task.SquadID.Valid && issue.AssigneeType.Valid && issue.AssigneeType.String == "squad" && issue.AssigneeID == task.SquadID
}
