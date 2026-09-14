package handler

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/google/uuid"
)

type DesignRepositoryResponse struct {
	ID                string `json:"id"`
	ProjectID         string `json:"project_id"`
	ProjectTitle      string `json:"project_title"`
	Label             string `json:"label"`
	Description       string `json:"description,omitempty"`
	RepositoryURL     string `json:"repository_url"`
	DefaultBranchHint string `json:"default_branch_hint"`
}

// ListDesignRepositories returns Settings > Repositories as the sole source of
// Design Center's repository view. Project resources remain independent and do
// not appear here merely because a project attached a repository URL.
func (h *Handler) ListDesignRepositories(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	workspace, err := h.Queries.GetWorkspace(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load design repositories")
		return
	}
	var repos []workspaceRepoRef
	if len(workspace.Repos) > 0 && json.Unmarshal(workspace.Repos, &repos) != nil {
		writeError(w, http.StatusInternalServerError, "failed to decode design repositories")
		return
	}
	result := make([]DesignRepositoryResponse, 0, len(repos))
	for _, repo := range repos {
		repo.URL = strings.TrimSpace(repo.URL)
		if !isValidGitRepoURL(repo.URL) {
			continue
		}
		id := strings.TrimSpace(repo.ID)
		if _, err := uuid.Parse(id); err != nil {
			// Compatibility for a workspace not yet backfilled by migration 911.
			id = uuid.NewSHA1(uuid.NameSpaceURL, []byte(uuidToString(workspaceID)+"\x00"+repo.URL)).String()
		}
		result = append(result, DesignRepositoryResponse{
			ID:                id,
			Label:             workspaceRepositoryDisplayName(repo),
			Description:       strings.TrimSpace(repo.Description),
			RepositoryURL:     repo.URL,
			DefaultBranchHint: strings.TrimSpace(repo.DefaultBranchHint),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"repositories": result})
}

func workspaceRepositoryDisplayName(repo workspaceRepoRef) string {
	if value := strings.TrimSpace(repo.Description); value != "" {
		return value
	}
	value := strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(repo.URL), "/"), ".git")
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		if base := path.Base(strings.TrimRight(parsed.Path, "/")); base != "." && base != "/" && base != "" {
			return base
		}
	}
	if index := strings.LastIndexAny(value, "/:"); index >= 0 && index+1 < len(value) {
		return value[index+1:]
	}
	return value
}
