package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

const designAssetRepositoryAssociationPath = "/api/design-assets/repository-association"

func TestSetDesignAssetRepositoryAssociationRejectsMalformedJSON(t *testing.T) {
	resp := callAssociation(t, `{"project_id":`)
	assertProjectDesignSystemErrorCode(t, resp.ResponseRecorder, http.StatusBadRequest, "invalid_request")
}

func TestSetDesignAssetRepositoryAssociationRejectsTrailingJSON(t *testing.T) {
	resp := callAssociation(t, `{"project_id":"`+uuid.NewString()+`"} trailing`)
	assertProjectDesignSystemErrorCode(t, resp.ResponseRecorder, http.StatusBadRequest, "invalid_request")
}

func TestSetDesignAssetRepositoryAssociationRejectsEmptyItems(t *testing.T) {
	projectID := dbfx.Project(t, "association-empty-items")
	resp := callAssociation(t, associationBody(projectID, "", nil))
	assertProjectDesignSystemErrorCode(t, resp.ResponseRecorder, http.StatusBadRequest, "items_required")
}

func TestSetDesignAssetRepositoryAssociationRejectsUnknownKind(t *testing.T) {
	projectID := dbfx.Project(t, "association-unknown-kind")
	resp := callAssociation(t, associationBody(projectID, "", []associationItem{{Kind: "image", ID: uuid.NewString()}}))
	assertProjectDesignSystemErrorCode(t, resp.ResponseRecorder, http.StatusBadRequest, "design_asset_kind_invalid")
}

func TestSetDesignAssetRepositoryAssociationRejectsMissingProject(t *testing.T) {
	resp := callAssociation(t, associationBody(uuid.NewString(), "", []associationItem{{Kind: designAssetKindDesignFile, ID: uuid.NewString()}}))
	assertProjectDesignSystemErrorCode(t, resp.ResponseRecorder, http.StatusNotFound, "project_not_found")
}

func TestSetDesignAssetRepositoryAssociationRejectsMissingRepository(t *testing.T) {
	projectID := dbfx.Project(t, "association-missing-resource")
	resp := callAssociation(t, associationBody(projectID, uuid.NewString(), []associationItem{{Kind: designAssetKindDesignFile, ID: uuid.NewString()}}))
	assertProjectDesignSystemErrorCode(t, resp.ResponseRecorder, http.StatusNotFound, "project_resource_not_found")
}

func TestSetDesignAssetRepositoryAssociationRejectsRepositoryFromAnotherProject(t *testing.T) {
	projectID := dbfx.Project(t, "association-resource-project")
	otherProjectID := dbfx.Project(t, "association-resource-other-project")
	resourceID := associationResource(t, otherProjectID, "github_repo")
	resp := callAssociation(t, associationBody(projectID, resourceID, []associationItem{{Kind: designAssetKindDesignFile, ID: uuid.NewString()}}))
	assertProjectDesignSystemErrorCode(t, resp.ResponseRecorder, http.StatusConflict, "project_resource_project_mismatch")
}

func TestSetDesignAssetRepositoryAssociationRejectsNonRepository(t *testing.T) {
	projectID := dbfx.Project(t, "association-non-repository")
	resourceID := associationResource(t, projectID, "notion_page")
	resp := callAssociation(t, associationBody(projectID, resourceID, []associationItem{{Kind: designAssetKindDesignFile, ID: uuid.NewString()}}))
	assertProjectDesignSystemErrorCode(t, resp.ResponseRecorder, http.StatusBadRequest, "project_resource_not_repository")
}

func TestSetDesignAssetRepositoryAssociationRejectsCrossProjectAssets(t *testing.T) {
	projectID := dbfx.Project(t, "association-asset-project")
	otherProjectID := dbfx.Project(t, "association-asset-other-project")
	resourceID := associationResource(t, projectID, "github_repo")
	fileID := associationDesignFile(t, otherProjectID, nil)
	documentID := associationDesignDocument(t, otherProjectID, nil)

	for _, item := range []associationItem{
		{Kind: designAssetKindDesignFile, ID: fileID},
		{Kind: designAssetKindDesignDocument, ID: documentID},
	} {
		resp := callAssociation(t, associationBody(projectID, resourceID, []associationItem{item}))
		assertProjectDesignSystemErrorCode(t, resp.ResponseRecorder, http.StatusNotFound, "design_asset_not_found")
	}
}

func TestSetDesignAssetRepositoryAssociationRejectsMissingAssets(t *testing.T) {
	projectID := dbfx.Project(t, "association-missing-assets")
	resourceID := associationResource(t, projectID, "github_repo")
	for _, item := range []associationItem{
		{Kind: designAssetKindDesignFile, ID: uuid.NewString()},
		{Kind: designAssetKindDesignDocument, ID: uuid.NewString()},
	} {
		resp := callAssociation(t, associationBody(projectID, resourceID, []associationItem{item}))
		assertProjectDesignSystemErrorCode(t, resp.ResponseRecorder, http.StatusNotFound, "design_asset_not_found")
	}
}

func TestSetDesignAssetRepositoryAssociationRejectsActiveDesignDocument(t *testing.T) {
	projectID := dbfx.Project(t, "association-active-document")
	resourceID := associationResource(t, projectID, "github_repo")
	documentID := associationDesignDocument(t, projectID, uuid.NewString())
	resp := callAssociation(t, associationBody(projectID, resourceID, []associationItem{{Kind: designAssetKindDesignDocument, ID: documentID}}))
	assertProjectDesignSystemErrorCode(t, resp.ResponseRecorder, http.StatusConflict, "design_document_task_active")
}

func TestSetDesignAssetRepositoryAssociationUpdatesMixedAssets(t *testing.T) {
	projectID := dbfx.Project(t, "association-mixed-success")
	resourceID := associationResource(t, projectID, "github_repo")
	fileID := associationDesignFile(t, projectID, nil)
	documentID := associationDesignDocument(t, projectID, nil)
	resp := callAssociation(t, associationBody(projectID, resourceID, []associationItem{
		{Kind: designAssetKindDesignFile, ID: fileID},
		{Kind: designAssetKindDesignDocument, ID: documentID},
	}))
	resp.Want(http.StatusOK)
	payload := resp.Map()
	if payload["project_id"] != projectID || payload["project_resource_id"] != resourceID || payload["count"] != float64(2) {
		t.Fatalf("success payload = %#v", payload)
	}
	assertAssociationResource(t, "design_file", fileID, resourceID)
	assertAssociationResource(t, "design_document", documentID, resourceID)
}

func TestSetDesignAssetRepositoryAssociationClearsOnEmptyResource(t *testing.T) {
	projectID := dbfx.Project(t, "association-clear")
	resourceID := associationResource(t, projectID, "github_repo")
	fileID := associationDesignFile(t, projectID, resourceID)
	documentID := associationDesignDocument(t, projectID, nil)
	dbfx.Exec(t, `UPDATE design_document SET project_resource_id = $1 WHERE id = $2`, resourceID, documentID)

	resp := callAssociation(t, associationBody(projectID, "   ", []associationItem{
		{Kind: designAssetKindDesignFile, ID: fileID},
		{Kind: designAssetKindDesignDocument, ID: documentID},
	}))
	resp.Want(http.StatusOK)
	payload := resp.Map()
	if payload["project_resource_id"] != "" {
		t.Fatalf("clear payload resource = %#v", payload["project_resource_id"])
	}
	assertAssociationResource(t, "design_file", fileID, "")
	assertAssociationResource(t, "design_document", documentID, "")
}

func TestSetDesignAssetRepositoryAssociationRollsBackOnSecondItemFailure(t *testing.T) {
	projectID := dbfx.Project(t, "association-rollback")
	resourceID := associationResource(t, projectID, "github_repo")
	fileID := associationDesignFile(t, projectID, nil)
	resp := callAssociation(t, associationBody(projectID, resourceID, []associationItem{
		{Kind: designAssetKindDesignFile, ID: fileID},
		{Kind: designAssetKindDesignDocument, ID: uuid.NewString()},
	}))
	assertProjectDesignSystemErrorCode(t, resp.ResponseRecorder, http.StatusNotFound, "design_asset_not_found")
	assertAssociationResource(t, "design_file", fileID, "")
}

type associationItem struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func associationBody(projectID, resourceID string, items []associationItem) map[string]any {
	return map[string]any{
		"project_id":          projectID,
		"project_resource_id": resourceID,
		"items":               items,
	}
}

func associationRequest(body any) *http.Request {
	req := testutil.JSONRequest(http.MethodPut, designAssetRepositoryAssociationPath, body)
	return testutil.WithHeaders(req, "X-User-ID", testUserID, "X-Workspace-ID", testWorkspaceID)
}

func callAssociation(t *testing.T, body any) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.SetDesignAssetRepositoryAssociation, associationRequest(body))
}

func associationResource(t *testing.T, projectID, resourceType string) string {
	t.Helper()
	return dbfx.Insert(t, "project_resource", testutil.Cols{
		"project_id":    projectID,
		"workspace_id":  testWorkspaceID,
		"resource_type": resourceType,
		"resource_ref":  testutil.Raw(`'{}'::jsonb`),
		"created_by":    testUserID,
	})
}

func associationDesignFile(t *testing.T, projectID string, resourceID any) string {
	t.Helper()
	return dbfx.Insert(t, "design_file", testutil.Cols{
		"workspace_id":        testWorkspaceID,
		"project_id":          projectID,
		"title":               "association design file",
		"source_type":         "upload",
		"source_ref":          testutil.Raw(`'{}'::jsonb`),
		"project_resource_id": resourceID,
		"created_by":          testUserID,
	})
}

func associationDesignDocument(t *testing.T, projectID string, activeTaskID any) string {
	t.Helper()
	return dbfx.Insert(t, "design_document", testutil.Cols{
		"workspace_id":   testWorkspaceID,
		"project_id":     projectID,
		"title":          "association design document",
		"platform":       "web",
		"recipe":         "default",
		"active_task_id": activeTaskID,
		"created_by":     testUserID,
	})
}

func assertAssociationResource(t *testing.T, kind, id, want string) {
	t.Helper()
	var got *string
	table := kind
	if err := testPool.QueryRow(context.Background(), "SELECT project_resource_id::text FROM "+table+" WHERE id = $1", id).Scan(&got); err != nil {
		t.Fatalf("read %s %s: %v", kind, id, err)
	}
	if (got == nil && want != "") || (got != nil && *got != want) {
		t.Fatalf("%s %s resource = %v, want %q", kind, id, got, want)
	}
}
