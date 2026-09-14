package handler

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/designdocument"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Resolving "does this issue have a design to build from?" at claim time
// (DC-062).
//
// The answer is derived entirely server-side from the document's own saved
// pointer. A task never names a revision, so an agent cannot reach a draft,
// another document's package, or another workspace's anything.

// designDeliveryContextForIssue returns the delivered design for an issue, or
// nil when there is none. A lookup failure is logged and treated as "none":
// an implementation task must still be claimable when the design lookup is
// having a bad day, and the agent is told what it has rather than what it
// might have had.
func (h *Handler) designDeliveryContextForIssue(
	ctx context.Context,
	workspaceID pgtype.UUID,
	issueID pgtype.UUID,
) *service.DesignDeliveryContext {
	if !workspaceID.Valid || !issueID.Valid {
		return nil
	}
	rows, err := h.Queries.ListDeliveredDesignDocumentsByIssue(ctx, db.ListDeliveredDesignDocumentsByIssueParams{
		WorkspaceID: workspaceID,
		IssueID:     issueID,
	})
	if err != nil {
		slog.Warn("design delivery lookup failed",
			"workspace_id", uuidToString(workspaceID),
			"issue_id", uuidToString(issueID),
			"error", err,
		)
		return nil
	}
	for _, row := range rows {
		if row.SavedPackageSchema != designdocument.PackageSchemaV1 {
			// A package this server no longer knows how to read is not a
			// delivery; saying nothing beats handing over bytes the daemon
			// would reject anyway.
			continue
		}
		delivery := &service.DesignDeliveryContext{
			SchemaVersion:      service.DesignDeliverySchema,
			DesignDocumentID:   uuidToString(row.ID),
			RevisionID:         uuidToString(row.SavedRevisionUuid),
			RevisionNumber:     row.SavedRevisionNumber,
			ContentDigest:      row.SavedContentDigest,
			Title:              row.Title,
			Platform:           row.Platform,
			RepositoryGrounded: false,
			Pages:              []service.DesignDeliveryPage{},
		}
		// Page titles come from the revision's manifest, which is already
		// stored beside the archive — no extra read of object storage just to
		// tell the agent what the design contains.
		if revision, err := h.Queries.GetDesignDocumentRevisionInWorkspace(ctx, db.GetDesignDocumentRevisionInWorkspaceParams{
			ID: row.SavedRevisionUuid, WorkspaceID: workspaceID,
		}); err == nil {
			delivery.RepositoryGrounded = repositoryGroundingAvailable(revision.RepositoryGrounding)
			var manifest designdocument.Manifest
			if json.Unmarshal(revision.Manifest, &manifest) == nil {
				delivery.PrototypeEntry = manifest.PrototypeEntry
				for _, page := range manifest.Pages {
					delivery.Pages = append(delivery.Pages, service.DesignDeliveryPage{
						ID: page.ID, Title: page.Title, Entry: page.Entry,
					})
				}
			}
		}
		return delivery
	}
	return nil
}
