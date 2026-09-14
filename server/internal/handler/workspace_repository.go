package handler

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var errWorkspaceRepositoryNotFound = errors.New("workspace repository not found")

func loadWorkspaceRepository(ctx context.Context, queries *db.Queries, workspaceID pgtype.UUID, repositoryID pgtype.UUID) (workspaceRepoRef, error) {
	workspace, err := queries.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return workspaceRepoRef{}, err
	}
	var repos []workspaceRepoRef
	if len(workspace.Repos) > 0 {
		if err := json.Unmarshal(workspace.Repos, &repos); err != nil {
			return workspaceRepoRef{}, err
		}
	}
	wanted := uuidToString(repositoryID)
	for _, repo := range repos {
		if strings.TrimSpace(repo.ID) == wanted && isValidGitRepoURL(strings.TrimSpace(repo.URL)) {
			repo.ID = wanted
			repo.URL = strings.TrimSpace(repo.URL)
			repo.Description = strings.TrimSpace(repo.Description)
			repo.DefaultBranchHint = strings.TrimSpace(repo.DefaultBranchHint)
			return repo, nil
		}
	}
	return workspaceRepoRef{}, errWorkspaceRepositoryNotFound
}

func ensureWorkspaceRepositoryRemovalsSafe(ctx context.Context, queries *db.Queries, workspaceID pgtype.UUID, previousJSON, nextJSON []byte) error {
	var previous, next []workspaceRepoRef
	if len(previousJSON) > 0 {
		if err := json.Unmarshal(previousJSON, &previous); err != nil {
			return err
		}
	}
	if len(nextJSON) > 0 {
		if err := json.Unmarshal(nextJSON, &next); err != nil {
			return err
		}
	}
	kept := make(map[string]struct{}, len(next))
	for _, repo := range next {
		kept[strings.TrimSpace(repo.ID)] = struct{}{}
	}
	for _, repo := range previous {
		id := strings.TrimSpace(repo.ID)
		if id == "" {
			continue
		}
		if _, ok := kept[id]; ok {
			continue
		}
		repositoryID, err := parseWorkspaceRepositoryUUID(id)
		if err != nil {
			continue
		}
		systems, err := queries.CountProjectDesignSystemsByWorkspaceRepository(ctx, db.CountProjectDesignSystemsByWorkspaceRepositoryParams{WorkspaceID: workspaceID, WorkspaceRepositoryID: repositoryID})
		if err != nil {
			return err
		}
		files, err := queries.CountDesignFilesByWorkspaceRepository(ctx, db.CountDesignFilesByWorkspaceRepositoryParams{WorkspaceID: workspaceID, WorkspaceRepositoryID: repositoryID})
		if err != nil {
			return err
		}
		documents, err := queries.CountDesignDocumentsByWorkspaceRepository(ctx, db.CountDesignDocumentsByWorkspaceRepositoryParams{WorkspaceID: workspaceID, WorkspaceRepositoryID: repositoryID})
		if err != nil {
			return err
		}
		if systems+files+documents > 0 {
			return errWorkspaceRepositoryInUse
		}
	}
	return nil
}

var errWorkspaceRepositoryInUse = errors.New("workspace repository is used by Design Center")

func parseWorkspaceRepositoryUUID(value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(value); err != nil || !id.Valid {
		return pgtype.UUID{}, errWorkspaceRepositoryNotFound
	}
	return id, nil
}
