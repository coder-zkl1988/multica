package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestListDesignRepositoriesUsesOnlySettingsRepositories(t *testing.T) {
	firstID := uuid.NewString()
	secondID := uuid.NewString()
	var previous []byte
	dbfx.QueryRow(t, `SELECT repos FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&previous)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `UPDATE workspace SET repos=$1 WHERE id=$2`, previous, testWorkspaceID)
	})
	dbfx.Exec(t, `UPDATE workspace SET repos=$1::jsonb WHERE id=$2`, `[
		{"id":"`+firstID+`","url":"https://github.com/example/web.git","description":"Web","default_branch_hint":"main"},
		{"id":"`+secondID+`","url":"git@github.com:example/api.git","description":"API"}
	]`, testWorkspaceID)

	project := dbfx.Project(t, "Ignored project repository")
	finderRepository(t, project, "ignored", `{"url":"https://github.com/example/project-only.git"}`)

	req := testutil.JSONRequest(http.MethodGet, "/api/design-repositories", nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	resp := testutil.Call(t, testHandler.ListDesignRepositories, req)
	resp.Want(http.StatusOK)
	rows := resp.Map()["repositories"].([]any)
	if len(rows) != 2 {
		t.Fatalf("repository rows = %#v", rows)
	}
	first := rows[0].(map[string]any)
	second := rows[1].(map[string]any)
	if first["id"] != firstID || first["label"] != "Web" || first["repository_url"] != "https://github.com/example/web.git" || first["default_branch_hint"] != "main" {
		t.Fatalf("first settings repository = %#v", first)
	}
	if first["project_id"] != "" || first["project_title"] != "" {
		t.Fatalf("settings repository unexpectedly inherited project scope: %#v", first)
	}
	if second["id"] != secondID || second["label"] != "API" {
		t.Fatalf("second settings repository = %#v", second)
	}
}

func TestListDesignRepositoriesHidesMalformedSettingsRepositories(t *testing.T) {
	validID := uuid.NewString()
	var previous []byte
	dbfx.QueryRow(t, `SELECT repos FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&previous)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `UPDATE workspace SET repos=$1 WHERE id=$2`, previous, testWorkspaceID)
	})
	dbfx.Exec(t, `UPDATE workspace SET repos=$1::jsonb WHERE id=$2`, `[
		{"id":"`+validID+`","url":"https://github.com/example/valid"},
		{"id":"`+uuid.NewString()+`","url":"not-a-repository"}
	]`, testWorkspaceID)

	req := testutil.JSONRequest(http.MethodGet, "/api/design-repositories", nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	resp := testutil.Call(t, testHandler.ListDesignRepositories, req)
	resp.Want(http.StatusOK)
	rows := resp.Map()["repositories"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["id"] != validID {
		t.Fatalf("catalogue = %#v", rows)
	}
}

func finderRepository(t *testing.T, projectID, label, ref string) string {
	t.Helper()
	return dbfx.Insert(t, "project_resource", testutil.Cols{
		"project_id": projectID, "workspace_id": testWorkspaceID, "resource_type": "github_repo",
		"resource_ref": testutil.Raw(`'` + ref + `'::jsonb`), "label": label, "created_by": testUserID,
	})
}
