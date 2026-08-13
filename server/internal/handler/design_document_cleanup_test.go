package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestDeleteProjectCleansDesignDocumentsAndPreservesNeighbors(t *testing.T) {
	target := seedDesignDocumentCleanupFixture(t, "project-target", true)
	sameWorkspaceNeighbor := seedDesignDocumentCleanupProject(t, target.workspaceID, "project-neighbor")
	otherWorkspaceNeighbor := seedDesignDocumentCleanupFixture(t, "workspace-neighbor", false)
	store := designDocumentCleanupStore(t, target.objectKey, sameWorkspaceNeighbor.objectKey, otherWorkspaceNeighbor.objectKey)

	w := httptest.NewRecorder()
	req := newRequest(http.MethodDelete, "/api/projects/"+target.projectID, nil)
	req.Header.Set("X-Workspace-ID", target.workspaceID)
	req = withURLParam(req, "id", target.projectID)
	testHandler.DeleteProject(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteProject: got %d: %s, want 204", w.Code, w.Body.String())
	}

	assertDesignDocumentCleanupCounts(t, target, 0, 0, 0)
	assertDesignDocumentCleanupCounts(t, sameWorkspaceNeighbor, 1, 1, 1)
	assertDesignDocumentCleanupCounts(t, otherWorkspaceNeighbor, 1, 1, 1)
	assertDesignDocumentCleanupObject(t, store, target.objectKey)
}

func TestDeleteProjectDoesNotCleanForeignWorkspaceDesignDocuments(t *testing.T) {
	target := seedDesignDocumentCleanupFixture(t, "project-tenant-target", false)
	callerWorkspace := seedDesignDocumentCleanupFixture(t, "project-tenant-caller", false)
	store := designDocumentCleanupStore(t, target.objectKey)

	if err := testHandler.Queries.DeleteProject(context.Background(), db.DeleteProjectParams{
		ID:          parseUUID(target.projectID),
		WorkspaceID: parseUUID(callerWorkspace.workspaceID),
	}); err != nil {
		t.Fatalf("DeleteProject with wrong workspace: %v", err)
	}

	var projectCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM project WHERE id = $1`, target.projectID).Scan(&projectCount); err != nil {
		t.Fatalf("count protected project: %v", err)
	}
	if projectCount != 1 {
		t.Fatalf("protected project count = %d, want 1", projectCount)
	}
	assertDesignDocumentCleanupCounts(t, target, 1, 1, 1)
	assertDesignDocumentCleanupObject(t, store, target.objectKey)
}

func TestDeleteIssuePreservesDesignDocumentHistory(t *testing.T) {
	for _, tt := range []struct {
		name   string
		delete func(*testing.T, designDocumentCleanupFixture)
	}{
		{
			name: "single",
			delete: func(t *testing.T, fixture designDocumentCleanupFixture) {
				w := httptest.NewRecorder()
				req := newRequest(http.MethodDelete, "/api/issues/"+fixture.issueID, nil)
				req.Header.Set("X-Workspace-ID", fixture.workspaceID)
				req = withURLParam(req, "id", fixture.issueID)
				testHandler.DeleteIssue(w, req)
				if w.Code != http.StatusNoContent {
					t.Fatalf("DeleteIssue: got %d: %s, want 204", w.Code, w.Body.String())
				}
			},
		},
		{
			name: "batch",
			delete: func(t *testing.T, fixture designDocumentCleanupFixture) {
				w := httptest.NewRecorder()
				req := newRequest(http.MethodPost, "/api/issues/batch-delete", map[string]any{"issue_ids": []string{fixture.issueID}})
				req.Header.Set("X-Workspace-ID", fixture.workspaceID)
				testHandler.BatchDeleteIssues(w, req)
				if w.Code != http.StatusOK {
					t.Fatalf("BatchDeleteIssues: got %d: %s, want 200", w.Code, w.Body.String())
				}
				var response struct {
					Deleted int `json:"deleted"`
				}
				if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
					t.Fatalf("decode BatchDeleteIssues response: %v", err)
				}
				if response.Deleted != 1 {
					t.Fatalf("BatchDeleteIssues deleted = %d, want 1", response.Deleted)
				}
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			target := seedDesignDocumentCleanupFixture(t, "issue-"+tt.name, true)
			neighbor := seedDesignDocumentCleanupFixture(t, "issue-neighbor-"+tt.name, false)
			store := designDocumentCleanupStore(t, target.objectKey, neighbor.objectKey)
			snapshotBefore, manifestBefore := designDocumentCleanupImmutableState(t, target)

			tt.delete(t, target)

			var issueCount int
			if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM issue WHERE id = $1`, target.issueID).Scan(&issueCount); err != nil {
				t.Fatalf("count deleted issue: %v", err)
			}
			if issueCount != 0 {
				t.Fatalf("deleted issue count = %d, want 0", issueCount)
			}
			assertDesignDocumentCleanupCounts(t, target, 1, 1, 1)
			assertDesignDocumentCleanupCounts(t, neighbor, 1, 1, 1)
			var documentIssueID *string
			if err := testPool.QueryRow(context.Background(), `SELECT issue_id::text FROM design_document WHERE id = $1`, target.documentID).Scan(&documentIssueID); err != nil {
				t.Fatalf("read document issue binding: %v", err)
			}
			if documentIssueID != nil {
				t.Fatalf("document issue_id = %q, want NULL", *documentIssueID)
			}
			snapshotAfter, manifestAfter := designDocumentCleanupImmutableState(t, target)
			if !bytes.Equal(snapshotAfter, snapshotBefore) {
				t.Fatalf("input snapshot changed after issue delete: before=%s after=%s", snapshotBefore, snapshotAfter)
			}
			if !bytes.Equal(manifestAfter, manifestBefore) {
				t.Fatalf("revision manifest changed after issue delete: before=%s after=%s", manifestBefore, manifestAfter)
			}
			assertDesignDocumentCleanupObject(t, store, target.objectKey)
		})
	}
}

func TestDeleteIssueDoesNotDetachForeignWorkspaceDesignDocument(t *testing.T) {
	target := seedDesignDocumentCleanupFixture(t, "issue-tenant-target", false)
	callerWorkspace := seedDesignDocumentCleanupFixture(t, "issue-tenant-caller", false)
	store := designDocumentCleanupStore(t, target.objectKey)

	if err := testHandler.Queries.DeleteIssue(context.Background(), db.DeleteIssueParams{
		ID:          parseUUID(target.issueID),
		WorkspaceID: parseUUID(callerWorkspace.workspaceID),
	}); err != nil {
		t.Fatalf("DeleteIssue with wrong workspace: %v", err)
	}

	var issueCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM issue WHERE id = $1`, target.issueID).Scan(&issueCount); err != nil {
		t.Fatalf("count protected issue: %v", err)
	}
	if issueCount != 1 {
		t.Fatalf("protected issue count = %d, want 1", issueCount)
	}
	assertDesignDocumentCleanupCounts(t, target, 1, 1, 1)
	var documentIssueID string
	if err := testPool.QueryRow(context.Background(), `SELECT issue_id::text FROM design_document WHERE id = $1`, target.documentID).Scan(&documentIssueID); err != nil {
		t.Fatalf("read protected document issue binding: %v", err)
	}
	if documentIssueID != target.issueID {
		t.Fatalf("protected document issue_id = %s, want %s", documentIssueID, target.issueID)
	}
	assertDesignDocumentCleanupObject(t, store, target.objectKey)
}

func TestDeleteWorkspaceCleansDesignDocumentsAndPreservesNeighbor(t *testing.T) {
	target := seedDesignDocumentCleanupFixture(t, "workspace-target", true)
	neighbor := seedDesignDocumentCleanupFixture(t, "workspace-survivor", false)
	store := designDocumentCleanupStore(t, target.objectKey, neighbor.objectKey)

	w := httptest.NewRecorder()
	req := newRequest(http.MethodDelete, "/api/workspaces/"+target.workspaceID, nil)
	req = withURLParam(req, "id", target.workspaceID)
	testHandler.DeleteWorkspace(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteWorkspace: got %d: %s, want 204", w.Code, w.Body.String())
	}

	assertDesignDocumentCleanupCounts(t, target, 0, 0, 0)
	assertDesignDocumentCleanupCounts(t, neighbor, 1, 1, 1)
	assertDesignDocumentCleanupObject(t, store, target.objectKey)
}

type designDocumentCleanupFixture struct {
	workspaceID string
	projectID   string
	issueID     string
	documentID  string
	snapshotID  string
	revisionID  string
	objectKey   string
}

func seedDesignDocumentCleanupFixture(t *testing.T, label string, owner bool) designDocumentCleanupFixture {
	t.Helper()
	workspaceID := uuid.NewString()
	slug := "design-document-cleanup-" + strings.ToLower(strings.ReplaceAll(label, "_", "-")) + "-" + uuid.NewString()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO workspace (id, name, slug, issue_prefix) VALUES ($1, $2, $3, 'DDC')
	`, workspaceID, "Design document cleanup "+label, slug); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if owner {
		if _, err := testPool.Exec(context.Background(), `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, workspaceID, testUserID); err != nil {
			t.Fatalf("insert workspace owner: %v", err)
		}
	}
	t.Cleanup(func() { cleanupDesignDocumentCleanupWorkspace(context.Background(), workspaceID) })
	return seedDesignDocumentCleanupProject(t, workspaceID, label)
}

func seedDesignDocumentCleanupProject(t *testing.T, workspaceID, label string) designDocumentCleanupFixture {
	t.Helper()
	fixture := designDocumentCleanupFixture{
		workspaceID: workspaceID,
		projectID:   uuid.NewString(),
		issueID:     uuid.NewString(),
		documentID:  uuid.NewString(),
		snapshotID:  uuid.NewString(),
		revisionID:  uuid.NewString(),
	}
	fixture.objectKey = fmt.Sprintf("design-documents/%s/%s/%s/%s/archive.zip", fixture.workspaceID, fixture.projectID, fixture.documentID, fixture.revisionID)
	taskID := uuid.NewString()
	agentID := uuid.NewString()
	if _, err := testPool.Exec(context.Background(), `
		WITH project_row AS (
			INSERT INTO project (id, workspace_id, title) VALUES ($1, $2, $3)
			RETURNING id, workspace_id
		), issue_row AS (
			INSERT INTO issue (id, workspace_id, title, creator_type, creator_id, number, project_id)
			SELECT $4, workspace_id, $5, 'member', $6,
			       (SELECT COALESCE(max(number), 0) + 1 FROM issue WHERE workspace_id = $2), id
			FROM project_row
			RETURNING id, workspace_id, project_id
		), snapshot_row AS (
			INSERT INTO design_document_input_snapshot (
				id, workspace_id, project_id, issue_id, task_id, agent_id,
				target_platform, schema_version, snapshot, snapshot_sha256
			)
			SELECT $7, workspace_id, project_id, id, $8, $9, 'web',
			       'multica.design-document-input/v1', $10::jsonb, $11
			FROM issue_row
			RETURNING id, workspace_id, project_id, issue_id, task_id, agent_id
		), revision_row AS (
			INSERT INTO design_document_revision (
				id, document_id, workspace_id, project_id, input_snapshot_id,
				source_task_id, schema_version, manifest, artifact_index,
				archive_object_key, content_digest, created_by_agent_id
			)
			SELECT $12, $13, workspace_id, project_id, id, task_id,
			       'multica.design-document/v1', $14::jsonb, '[]'::jsonb, $15, $16, agent_id
			FROM snapshot_row
			RETURNING id, document_id, workspace_id, project_id
		), document_row AS (
			INSERT INTO design_document (
				id, workspace_id, project_id, issue_id, title, draft_revision_id, saved_revision_id, created_by
			)
			SELECT revision_row.document_id, revision_row.workspace_id, revision_row.project_id,
			       issue_row.id, $17, revision_row.id, revision_row.id, $6
			FROM revision_row CROSS JOIN issue_row
			RETURNING id
		)
		SELECT id FROM document_row
	`, fixture.projectID, fixture.workspaceID, "Cleanup "+label, fixture.issueID, "Cleanup issue "+label, testUserID,
		fixture.snapshotID, taskID, agentID, `{"goal":"retain history","stable_id":"brief-1"}`, "sha256:"+strings.Repeat("a", 64),
		fixture.revisionID, fixture.documentID, `{"schema_version":"multica.design-document/v1","stable_id":"revision-1"}`, fixture.objectKey,
		"sha256:"+strings.Repeat("b", 64), "Cleanup document "+label); err != nil {
		t.Fatalf("seed design document cleanup project: %v", err)
	}
	return fixture
}

func cleanupDesignDocumentCleanupWorkspace(ctx context.Context, workspaceID string) {
	_, _ = testPool.Exec(ctx, `DELETE FROM design_document_revision WHERE workspace_id = $1`, workspaceID)
	_, _ = testPool.Exec(ctx, `DELETE FROM design_document_input_snapshot WHERE workspace_id = $1`, workspaceID)
	_, _ = testPool.Exec(ctx, `DELETE FROM design_document WHERE workspace_id = $1`, workspaceID)
	_, _ = testPool.Exec(ctx, `DELETE FROM issue WHERE workspace_id = $1`, workspaceID)
	_, _ = testPool.Exec(ctx, `DELETE FROM project WHERE workspace_id = $1`, workspaceID)
	_, _ = testPool.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1`, workspaceID)
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, workspaceID)
}

func designDocumentCleanupStore(t *testing.T, keys ...string) *mockStorage {
	t.Helper()
	store := &mockStorage{files: make(map[string][]byte, len(keys))}
	for _, key := range keys {
		store.files[key] = []byte("immutable design archive")
	}
	previous := testHandler.Storage
	testHandler.Storage = store
	t.Cleanup(func() { testHandler.Storage = previous })
	return store
}

func assertDesignDocumentCleanupCounts(t *testing.T, fixture designDocumentCleanupFixture, documents, snapshots, revisions int) {
	t.Helper()
	for _, check := range []struct {
		name string
		want int
	}{
		{"design_document", documents},
		{"design_document_input_snapshot", snapshots},
		{"design_document_revision", revisions},
	} {
		var got int
		query := fmt.Sprintf("SELECT count(*) FROM %s WHERE workspace_id = $1 AND project_id = $2", check.name)
		if err := testPool.QueryRow(context.Background(), query, fixture.workspaceID, fixture.projectID).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", check.name, err)
		}
		if got != check.want {
			t.Errorf("%s count for workspace %s project %s = %d, want %d", check.name, fixture.workspaceID, fixture.projectID, got, check.want)
		}
	}
}

func designDocumentCleanupImmutableState(t *testing.T, fixture designDocumentCleanupFixture) ([]byte, []byte) {
	t.Helper()
	var snapshot, manifest []byte
	var snapshotIssueID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT snapshot, issue_id::text FROM design_document_input_snapshot WHERE id = $1
	`, fixture.snapshotID).Scan(&snapshot, &snapshotIssueID); err != nil {
		t.Fatalf("read input snapshot: %v", err)
	}
	if snapshotIssueID != fixture.issueID {
		t.Fatalf("immutable snapshot issue_id = %s, want %s", snapshotIssueID, fixture.issueID)
	}
	if err := testPool.QueryRow(context.Background(), `SELECT manifest FROM design_document_revision WHERE id = $1`, fixture.revisionID).Scan(&manifest); err != nil {
		t.Fatalf("read revision manifest: %v", err)
	}
	return snapshot, manifest
}

func assertDesignDocumentCleanupObject(t *testing.T, store *mockStorage, key string) {
	t.Helper()
	reader, err := store.GetReader(context.Background(), key)
	if err != nil {
		t.Fatalf("design document object %q was deleted: %v", key, err)
	}
	defer reader.Close()
	contents, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read design document object %q: %v", key, err)
	}
	if string(contents) != "immutable design archive" {
		t.Fatalf("design document object %q changed: %q", key, contents)
	}
}
