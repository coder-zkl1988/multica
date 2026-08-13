package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const pmoCorporateEmailDomain = "@soyoung.com"

// normalizePMOOwnerEmail converts a PM snapshot owner external_id into the
// workspace email key used for exact matching. Bare corporate accounts get the
// canonical @soyoung.com domain; anything that is not a single, safe account or
// email (display names, spaces, repeated prefixes) resolves to empty so it stays
// in the manual mapping queue instead of being guessed.
func normalizePMOOwnerEmail(externalID string) string {
	value := strings.ToLower(strings.TrimSpace(externalID))
	if value == "" {
		return ""
	}
	if strings.Contains(value, "@") {
		if strings.Count(value, "@") != 1 || strings.HasPrefix(value, "@") || strings.HasSuffix(value, "@") {
			return ""
		}
		return value
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return ""
	}
	return value + pmoCorporateEmailDomain
}

// pmoSnapshotOwners returns every distinct owner named by the snapshot, keyed by
// the original external_id so mapping keys stay stable for diff/link persistence.
func pmoSnapshotOwners(snapshot PMOSnapshot) map[string]*PMOExternalOwner {
	owners := map[string]*PMOExternalOwner{}
	addOwner := func(o *PMOExternalOwner) {
		if o != nil && o.ExternalID != "" {
			owners[o.ExternalID] = o
		}
	}
	addOwner(snapshot.Parent.Owner)
	for _, child := range snapshot.Children {
		addOwner(child.Owner)
		for i := range child.Tasks {
			addOwner(child.Tasks[i].Owner)
		}
	}
	for i := range snapshot.Tasks {
		addOwner(snapshot.Tasks[i].Owner)
	}
	return owners
}

// matchPMOAssigneeMappings merges explicit mappings with automatic workspace
// email matches. Explicit mappings always win; automatic matching is exact
// (case-insensitive) against the provided member email -> user id map and never
// guesses from display names or malformed old values.
func matchPMOAssigneeMappings(owners map[string]*PMOExternalOwner, memberEmailToUserID map[string]string, existing map[string]string) map[string]string {
	result := make(map[string]string, len(existing)+len(owners))
	for externalID, userID := range existing {
		if externalID != "" && userID != "" {
			result[externalID] = userID
		}
	}
	for externalID := range owners {
		if _, ok := result[externalID]; ok {
			continue
		}
		email := normalizePMOOwnerEmail(externalID)
		if email == "" {
			continue
		}
		if userID, ok := memberEmailToUserID[strings.ToLower(email)]; ok {
			result[externalID] = userID
		}
	}
	return result
}

// ResolvePMOAssigneeMappings combines the existing explicit mappings with exact
// workspace-member email matches, reusing ListMembersWithUser (no new SQL).
func ResolvePMOAssigneeMappings(
	ctx context.Context,
	qtx *db.Queries,
	workspaceID pgtype.UUID,
	snapshot PMOSnapshot,
	existing map[string]string,
) (map[string]string, error) {
	members, err := qtx.ListMembersWithUser(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("resolve pmo assignees: list workspace members: %w", err)
	}
	memberEmailToUserID := make(map[string]string, len(members))
	for _, member := range members {
		memberEmailToUserID[strings.ToLower(strings.TrimSpace(member.UserEmail))] = util.UUIDToString(member.UserID)
	}
	return matchPMOAssigneeMappings(pmoSnapshotOwners(snapshot), memberEmailToUserID, existing), nil
}
