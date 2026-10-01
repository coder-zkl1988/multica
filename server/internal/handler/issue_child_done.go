package handler

import (
	"context"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func (h *Handler) notifyParentOfChildDone(ctx context.Context, prev, issue db.Issue) {
	if issue.ParentIssueID.Valid && prev.Status != issue.Status {
		category, err := issuestatus.CategoryWithError(ctx, h.Queries, issue.WorkspaceID, issue.Status)
		if err == nil && category == "done" {
			parent, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: issue.ParentIssueID, WorkspaceID: issue.WorkspaceID})
			if err == nil {
				h.promoteFrontendSiblingsAfterDesignDone(ctx, issue, parent)
			}
		}
	}
	h.processChildEvents(ctx, issue.ParentIssueID, prev.ParentIssueID)
}

func (h *Handler) notifyParentsOfBatchChildDone(ctx context.Context, children []db.Issue) {
	parents := make([]pgtype.UUID, 0, len(children))
	for _, child := range children {
		parents = append(parents, child.ParentIssueID)
		if !child.ParentIssueID.Valid {
			continue
		}
		parent, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: child.ParentIssueID, WorkspaceID: child.WorkspaceID})
		if err == nil {
			h.promoteFrontendSiblingsAfterDesignDone(ctx, child, parent)
		}
	}
	h.processChildEvents(ctx, parents...)
}

func (h *Handler) promoteFrontendSiblingsAfterDesignDone(ctx context.Context, completedChild, parent db.Issue) {
	if !isUIDesignIssue(completedChild) || !h.uiDesignDelivered(ctx, completedChild) {
		return
	}
	children, err := h.Queries.ListChildIssues(ctx, db.ListChildIssuesParams{
		WorkspaceID:   parent.WorkspaceID,
		ParentIssueID: parent.ID,
	})
	if err != nil {
		return
	}
	for _, child := range children {
		if child.ID == completedChild.ID || !isFrontendDevIssue(child) || child.Status != "backlog" {
			continue
		}
		if err := h.updateIssueStatusAndPublish(ctx, child.ID, child.WorkspaceID, "todo", "system", ""); err != nil {
			slog.Warn("design done: failed to promote frontend sibling", "issue_id", uuidToString(child.ID), "error", err)
			continue
		}
		h.createDesignRestoreIssueSystemComment(ctx, child.ID, "UI 设计交付已完成，前端开发已进入待办。")
	}
}

const (
	issueDesignRoleMetadataKey = "design_role"
	issueDesignRoleUI          = "ui_design"
	issueDesignRoleFrontend    = "frontend_dev"
)

func isUIDesignIssue(issue db.Issue) bool {
	return issueDesignRole(issue) == issueDesignRoleUI
}

func isFrontendDevIssue(issue db.Issue) bool {
	return issueDesignRole(issue) == issueDesignRoleFrontend
}

func issueDesignRole(issue db.Issue) string {
	if explicitRole := explicitIssueDesignRole(issue.Metadata); explicitRole != "" {
		return explicitRole
	}
	return inferIssueDesignRoleFromTitle(issue.Title)
}

func explicitIssueDesignRole(metadata []byte) string {
	rawRole, ok := parseIssueMetadata(metadata)[issueDesignRoleMetadataKey]
	if !ok {
		return ""
	}
	role, ok := rawRole.(string)
	if !ok {
		return ""
	}
	if role == issueDesignRoleUI || role == issueDesignRoleFrontend {
		return role
	}
	return ""
}

func inferIssueDesignRoleFromTitle(title string) string {
	if looksLikeUIDesignIssue(title) {
		return issueDesignRoleUI
	}
	if looksLikeFrontendDevIssue(title) {
		return issueDesignRoleFrontend
	}
	return ""
}

func looksLikeUIDesignIssue(title string) bool {
	title = strings.ToLower(strings.TrimSpace(title))
	return strings.Contains(title, "ui") || strings.Contains(title, "设计")
}

func looksLikeFrontendDevIssue(title string) bool {
	title = strings.ToLower(strings.TrimSpace(title))
	return strings.Contains(title, "前端") || strings.Contains(title, "frontend")
}
