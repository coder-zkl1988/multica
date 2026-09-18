package service

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/redact"
)

const (
	IssueCompletionOutcomeDelivered   = "delivered"
	IssueCompletionOutcomeReviewReady = "review_ready"
	IssueCompletionOutcomeBlocked     = "blocked"
	issueCompletionMaxPendingInputs   = 32
)

var ErrStaleIssueCompletionClaim = errors.New("stale claim generation")

type IssueCompletion struct {
	Outcome         string
	Comment         string
	BaseRevision    int64
	BaseStatus      string
	ClaimGeneration int64
}

type issueCompletionCommit struct {
	created        []db.CreateCommentRow
	roots          map[string]*db.Comment
	statusChanged  *db.Issue
	previousStatus string
}

type completionReplyTarget struct {
	parent    pgtype.UUID
	root      *db.Comment
	createdAt pgtype.Timestamptz
}

func (s *TaskService) applyIssueCompletion(ctx context.Context, qtx *db.Queries, task db.AgentTaskQueue, completion IssueCompletion) (issueCompletionCommit, error) {
	var committed issueCompletionCommit
	baseIssue, err := qtx.GetIssue(ctx, task.IssueID)
	if err != nil {
		return committed, err
	}
	// CreateComment updates this owner row before inserting. Taking the same
	// lock before the exact-final lookup serializes completion with an in-flight
	// CLI comment that began first, so its committed final is reused.
	issue, err := qtx.LockIssueForDescriptionUpdate(ctx, db.LockIssueForDescriptionUpdateParams{
		ID: task.IssueID, WorkspaceID: baseIssue.WorkspaceID,
	})
	if err != nil {
		return committed, err
	}
	statusGuardMatches := false
	if completion.Outcome != IssueCompletionOutcomeDelivered {
		statusGuardMatches, err = issueCompletionStatusGuardMatches(ctx, qtx, issue, task, completion)
		if err != nil {
			return committed, err
		}
	}
	if statusGuardMatches {
		nextStatus := "in_review"
		if completion.Outcome == IssueCompletionOutcomeBlocked {
			nextStatus = "blocked"
		}
		updated, updateErr := qtx.UpdateIssue(ctx, db.UpdateIssueParams{
			ID:               task.IssueID,
			ExpectedRevision: pgtype.Int8{Int64: issue.Revision, Valid: true},
			Status:           pgtype.Text{String: nextStatus, Valid: true},
			AssigneeType:     issue.AssigneeType,
			AssigneeID:       issue.AssigneeID,
			StartDate:        issue.StartDate,
			DueDate:          issue.DueDate,
			ParentIssueID:    issue.ParentIssueID,
			ProjectID:        issue.ProjectID,
			Stage:            issue.Stage,
		})
		if updateErr == nil {
			committed.previousStatus = issue.Status
			committed.statusChanged = &updated
			issue = updated
		} else if !errors.Is(updateErr, pgx.ErrNoRows) {
			return committed, updateErr
		}
	}

	targets, err := completionReplyTargets(ctx, qtx, task, issue.WorkspaceID)
	if err != nil {
		return committed, err
	}
	content := CanonicalIssueCompletionComment(completion.Comment)
	if content == "" {
		return committed, errors.New("issue completion comment is empty after sanitization")
	}
	committed.roots = make(map[string]*db.Comment, len(targets))
	for _, target := range targets {
		_, lookupErr := qtx.GetExactAgentTaskFinalComment(ctx, db.GetExactAgentTaskFinalCommentParams{
			IssueID: task.IssueID, WorkspaceID: issue.WorkspaceID, AuthorID: task.AgentID,
			SourceTaskID: task.ID, ParentID: target.parent, Content: content,
		})
		if lookupErr == nil {
			continue
		}
		if !errors.Is(lookupErr, pgx.ErrNoRows) {
			return committed, lookupErr
		}
		candidates, candidateErr := qtx.ListAgentTaskFinalCommentsForParent(ctx, db.ListAgentTaskFinalCommentsForParentParams{
			IssueID: task.IssueID, WorkspaceID: issue.WorkspaceID, AuthorID: task.AgentID,
			SourceTaskID: task.ID, ParentID: target.parent,
		})
		if candidateErr != nil {
			return committed, candidateErr
		}
		matched := false
		for _, candidate := range candidates {
			if CanonicalIssueCompletionComment(candidate.Content) == content {
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		created, createErr := qtx.CreateComment(ctx, db.CreateCommentParams{
			ID: dbid.NewV7(), IssueID: task.IssueID, WorkspaceID: issue.WorkspaceID,
			AuthorType: "agent", AuthorID: task.AgentID, Content: content, Type: "comment",
			ParentID: target.parent, SourceTaskID: task.ID,
		})
		if createErr != nil {
			return committed, createErr
		}
		committed.created = append(committed.created, created)
		if target.root != nil {
			committed.roots[util.UUIDToString(created.ID)] = target.root
		}
	}
	return committed, nil
}

// CanonicalIssueCompletionComment is the exact transform applied only at the
// managed final-delivery boundary. Ordinary CLI comments retain their authored
// Markdown; dedupe compares their transformed value without rewriting the row.
func CanonicalIssueCompletionComment(comment string) string {
	return truncateFallbackCommentBody(redact.Text(util.UnescapeBackslashEscapes(strings.TrimSpace(comment))), maxSynthesizedFallbackCommentRunes)
}

func issueCompletionStatusGuardMatches(ctx context.Context, qtx *db.Queries, issue db.Issue, task db.AgentTaskQueue, completion IssueCompletion) (bool, error) {
	if issue.Revision < completion.BaseRevision || issue.Status != completion.BaseStatus || !issueTaskAssignmentMatches(issue, task) {
		return false, nil
	}
	if issue.Revision > completion.BaseRevision {
		revisions, err := qtx.ListAckedTaskPendingInputIssueRevisionsForClaim(ctx, db.ListAckedTaskPendingInputIssueRevisionsForClaimParams{
			WorkspaceID: issue.WorkspaceID, IssueID: issue.ID, TaskID: task.ID,
			AgentID: task.AgentID, RuntimeID: task.RuntimeID, ClaimGeneration: completion.ClaimGeneration,
		})
		if err != nil {
			return false, err
		}
		if len(revisions) == 0 || len(revisions) > issueCompletionMaxPendingInputs {
			return false, nil
		}
		attested := make([]int64, 0, len(revisions)*2)
		for _, revision := range revisions {
			if !revision.QuestionIssueRevision.Valid || !revision.AnswerIssueRevision.Valid ||
				revision.QuestionIssueRevision.Int64 >= revision.AnswerIssueRevision.Int64 {
				return false, nil
			}
			attested = append(attested, revision.QuestionIssueRevision.Int64, revision.AnswerIssueRevision.Int64)
		}
		sort.Slice(attested, func(i, j int) bool { return attested[i] < attested[j] })
		for offset, revision := range attested {
			if revision != completion.BaseRevision+int64(offset)+1 {
				return false, nil
			}
		}
		if completion.BaseRevision+int64(len(attested)) != issue.Revision {
			return false, nil
		}
	}
	status, err := issuestatus.Resolve(ctx, qtx, issue.WorkspaceID, issue.Status)
	if errors.Is(err, issuestatus.ErrUnknownStatus) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return status.Category == issuestatus.CategoryUnstarted || status.Category == issuestatus.CategoryStarted, nil
}

func completionReplyTargets(ctx context.Context, qtx *db.Queries, task db.AgentTaskQueue, workspaceID pgtype.UUID) ([]completionReplyTarget, error) {
	ids := append([]pgtype.UUID{}, task.CoalescedCommentIds...)
	if task.TriggerCommentID.Valid {
		ids = append(ids, task.TriggerCommentID)
	}
	if len(ids) == 0 {
		return []completionReplyTarget{{}}, nil
	}
	byRoot := make(map[string]completionReplyTarget, len(ids))
	for _, id := range ids {
		comment, err := qtx.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{ID: id, WorkspaceID: workspaceID})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if comment.IssueID != task.IssueID {
			continue
		}
		root, err := qtx.GetThreadRoot(ctx, db.GetThreadRootParams{CommentID: id, WorkspaceID: workspaceID})
		if err != nil {
			return nil, err
		}
		key := util.UUIDToString(root.ID)
		candidate := completionReplyTarget{parent: id, root: &root, createdAt: comment.CreatedAt}
		current, exists := byRoot[key]
		if !exists || comment.CreatedAt.Time.After(current.createdAt.Time) ||
			(comment.CreatedAt.Time.Equal(current.createdAt.Time) && util.UUIDToString(id) > util.UUIDToString(current.parent)) {
			byRoot[key] = candidate
		}
	}
	if len(byRoot) == 0 {
		return []completionReplyTarget{{}}, nil
	}
	keys := make([]string, 0, len(byRoot))
	for key := range byRoot {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	targets := make([]completionReplyTarget, 0, len(keys))
	for _, key := range keys {
		targets = append(targets, byRoot[key])
	}
	return targets, nil
}

func (s *TaskService) publishIssueCompletion(ctx context.Context, task db.AgentTaskQueue, committed issueCompletionCommit) {
	if committed.statusChanged != nil {
		s.broadcastIssueUpdated(ctx, *committed.statusChanged, committed.previousStatus)
	}
	if len(committed.created) == 0 {
		return
	}
	issue, err := s.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		return
	}
	for _, row := range committed.created {
		comment := row.Comment()
		fields := commentEventFields(comment)
		fields["revision"] = comment.Revision
		s.Bus.Publish(events.Event{Type: protocol.EventCommentCreated, WorkspaceID: util.UUIDToString(issue.WorkspaceID), ActorType: "agent", ActorID: util.UUIDToString(task.AgentID), Payload: map[string]any{
			"comment": fields, "issue_title": issue.Title, "issue_status": issue.Status, "issue_revision": row.IssueRevision,
		}})
		s.AutoUnresolveThreadOnReply(ctx, committed.roots[util.UUIDToString(comment.ID)], util.UUIDToString(issue.WorkspaceID), "agent", util.UUIDToString(task.AgentID))
	}
}
