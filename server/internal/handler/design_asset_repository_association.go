package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type designAssetRepositoryAssociationRequest struct {
	ProjectID         string                                 `json:"project_id"`
	ProjectResourceID string                                 `json:"project_resource_id"`
	Items             []designAssetRepositoryAssociationItem `json:"items"`
}

type designAssetRepositoryAssociationItem struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

const (
	designAssetKindDesignFile     = "design_file"
	designAssetKindDesignDocument = "design_document"
)

func (h *Handler) SetDesignAssetRepositoryAssociation(w http.ResponseWriter, r *http.Request) {
	var req designAssetRepositoryAssociationRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	workspaceID, _, ok := h.projectDesignSystemRequestScope(w, r)
	if !ok {
		return
	}
	projectID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.ProjectID), "project_id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: projectID, WorkspaceID: workspaceID,
	}); errors.Is(err, pgx.ErrNoRows) {
		writeProjectDesignSystemError(w, http.StatusNotFound, "project_not_found", "project not found")
		return
	} else if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "repository_association_failed", "failed to load project")
		return
	}
	if len(req.Items) == 0 {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "items_required", "at least one design asset is required")
		return
	}

	resourceID := pgtype.UUID{}
	if strings.TrimSpace(req.ProjectResourceID) != "" {
		resourceID, ok = parseUUIDOrBadRequest(w, strings.TrimSpace(req.ProjectResourceID), "project_resource_id")
		if !ok {
			return
		}
		resource, err := h.Queries.GetProjectResourceInWorkspace(r.Context(), db.GetProjectResourceInWorkspaceParams{
			ID: resourceID, WorkspaceID: workspaceID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			writeProjectDesignSystemError(w, http.StatusNotFound, "project_resource_not_found", "repository not found")
			return
		}
		if err != nil {
			writeProjectDesignSystemError(w, http.StatusInternalServerError, "repository_association_failed", "failed to load repository")
			return
		}
		if resource.ProjectID != projectID {
			writeProjectDesignSystemError(w, http.StatusConflict, "project_resource_project_mismatch", "repository does not belong to project")
			return
		}
		if resource.ResourceType != projectResourceTypeGitHubRepo {
			writeProjectDesignSystemError(w, http.StatusBadRequest, "project_resource_not_repository", "resource is not a code repository")
			return
		}
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "transaction_failed", "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	queries := h.Queries.WithTx(tx)

	for _, item := range req.Items {
		switch item.Kind {
		case designAssetKindDesignFile:
			fileID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(item.ID), "id")
			if !ok {
				return
			}
			file, err := queries.GetDesignFileInWorkspace(r.Context(), db.GetDesignFileInWorkspaceParams{
				ID: fileID, WorkspaceID: workspaceID,
			})
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && file.ProjectID != projectID) {
				writeProjectDesignSystemError(w, http.StatusNotFound, "design_asset_not_found", "design file not found")
				return
			}
			if err != nil {
				writeProjectDesignSystemError(w, http.StatusInternalServerError, "repository_association_failed", "failed to load design file")
				return
			}
			if _, err := queries.SetDesignFileRepository(r.Context(), db.SetDesignFileRepositoryParams{
				ID: fileID, WorkspaceID: workspaceID, ProjectResourceID: resourceID,
			}); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					writeProjectDesignSystemError(w, http.StatusNotFound, "design_asset_not_found", "design file not found")
					return
				}
				writeProjectDesignSystemError(w, http.StatusInternalServerError, "repository_association_failed", "failed to update design file")
				return
			}
		case designAssetKindDesignDocument:
			documentID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(item.ID), "id")
			if !ok {
				return
			}
			document, err := queries.GetDesignDocumentInWorkspace(r.Context(), db.GetDesignDocumentInWorkspaceParams{
				ID: documentID, WorkspaceID: workspaceID,
			})
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && document.ProjectID != projectID) {
				writeProjectDesignSystemError(w, http.StatusNotFound, "design_asset_not_found", "design document not found")
				return
			}
			if err != nil {
				writeProjectDesignSystemError(w, http.StatusInternalServerError, "repository_association_failed", "failed to load design document")
				return
			}
			if document.ActiveTaskID.Valid {
				writeProjectDesignSystemError(w, http.StatusConflict, "design_document_task_active", "design document has an active task")
				return
			}
			if _, err := queries.SetDesignDocumentRepository(r.Context(), db.SetDesignDocumentRepositoryParams{
				ID: documentID, WorkspaceID: workspaceID, ProjectResourceID: resourceID,
			}); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					writeProjectDesignSystemError(w, http.StatusConflict, "design_document_task_active", "design document has an active task")
					return
				}
				writeProjectDesignSystemError(w, http.StatusInternalServerError, "repository_association_failed", "failed to update design document")
				return
			}
		default:
			writeProjectDesignSystemError(w, http.StatusBadRequest, "design_asset_kind_invalid", "unknown design asset kind")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "transaction_failed", "failed to commit transaction")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"project_id":          uuidToString(projectID),
		"project_resource_id": uuidToString(resourceID),
		"count":               len(req.Items),
	})
}
