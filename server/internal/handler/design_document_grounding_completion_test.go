package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/designdocument"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestDesignDocumentGroundingCompletionCreatesOneAtomicSnapshot(t *testing.T) {
	task := createRunningDesignDocumentGroundingTask(t, "unavailable")
	receipt := unavailableGroundingReceipt()
	prepared, err := prepareDesignDocumentGroundingCompletion(task, &receipt)
	if err != nil {
		t.Fatalf("prepare completion: %v", err)
	}
	completed, err := testHandler.TaskService.CompleteTaskWithMutationAndSessionState(context.Background(), task.ID, []byte(`{"output":"done"}`), "", "", "", false, "", func(queries *db.Queries, completed db.AgentTaskQueue) error {
		return persistDesignDocumentGroundingCompletion(context.Background(), queries, completed, prepared)
	})
	if err != nil || completed == nil || completed.Status != "completed" {
		t.Fatalf("complete task = %+v, err=%v", completed, err)
	}
	var snapshots int
	var snapshotID, snapshotDigest, status string
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*), max(id::text), max(snapshot_sha256), max(snapshot->>'repository_grounding')
		FROM design_document_input_snapshot WHERE task_id = $1
	`, task.ID).Scan(&snapshots, &snapshotID, &snapshotDigest, &status); err != nil {
		t.Fatal(err)
	}
	if snapshots != 1 || snapshotID == "" || !strings.HasPrefix(snapshotDigest, "sha256:") || status != designdocument.GroundingUnavailable {
		t.Fatalf("snapshot = count=%d id=%q digest=%q status=%q", snapshots, snapshotID, snapshotDigest, status)
	}
	var contextSnapshotID, contextDigest string
	if err := testPool.QueryRow(context.Background(), `SELECT context->>'input_snapshot_id', context->>'input_snapshot_sha256' FROM agent_task_queue WHERE id = $1`, task.ID).Scan(&contextSnapshotID, &contextDigest); err != nil {
		t.Fatal(err)
	}
	if contextSnapshotID != snapshotID || contextDigest != snapshotDigest {
		t.Fatalf("task snapshot binding = %s/%s, want %s/%s", contextSnapshotID, contextDigest, snapshotID, snapshotDigest)
	}
	var taskContext struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(task.Context, &taskContext); err != nil {
		t.Fatal(err)
	}
	var documents, revisions int
	if err := testPool.QueryRow(context.Background(), `
		SELECT
			(SELECT count(*) FROM design_document WHERE project_id = $1),
			(SELECT count(*) FROM design_document_revision WHERE source_task_id = $2)
	`, taskContext.ProjectID, task.ID).Scan(&documents, &revisions); err != nil {
		t.Fatal(err)
	}
	if documents != 0 || revisions != 0 {
		t.Fatalf("A3 persisted documents=%d revisions=%d, want 0/0", documents, revisions)
	}
}

func TestDesignDocumentGroundingCompletionRollsBackTerminalTaskAndSnapshot(t *testing.T) {
	task := createRunningDesignDocumentGroundingTask(t, "unavailable")
	receipt := unavailableGroundingReceipt()
	prepared, err := prepareDesignDocumentGroundingCompletion(task, &receipt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_task_queue SET context = jsonb_set(context, '{operation}', '"changed"'::jsonb) WHERE id = $1`, task.ID); err != nil {
		t.Fatal(err)
	}
	_, err = testHandler.TaskService.CompleteTaskWithMutationAndSessionState(context.Background(), task.ID, []byte(`{"output":"done"}`), "", "", "", false, "", func(queries *db.Queries, completed db.AgentTaskQueue) error {
		return persistDesignDocumentGroundingCompletion(context.Background(), queries, completed, prepared)
	})
	if err == nil || !strings.Contains(err.Error(), "snapshot binding changed") {
		t.Fatalf("completion error = %v", err)
	}
	var status string
	var snapshots int
	if err := testPool.QueryRow(context.Background(), `SELECT status, (SELECT count(*) FROM design_document_input_snapshot WHERE task_id = agent_task_queue.id) FROM agent_task_queue WHERE id = $1`, task.ID).Scan(&status, &snapshots); err != nil {
		t.Fatal(err)
	}
	if status != "running" || snapshots != 0 {
		t.Fatalf("rolled-back state = status=%s snapshots=%d", status, snapshots)
	}
}

func TestDesignDocumentGroundingCompletionRejectsMismatchedNestedIdentity(t *testing.T) {
	task := createRunningDesignDocumentGroundingTask(t, "unavailable")
	var contextValue map[string]any
	if err := json.Unmarshal(task.Context, &contextValue); err != nil {
		t.Fatal(err)
	}
	input := contextValue["input"].(map[string]any)
	project := input["project"].(map[string]any)
	project["id"] = "00000000-0000-0000-0000-000000000001"
	task.Context, _ = json.Marshal(contextValue)
	if _, err := prepareDesignDocumentGroundingCompletion(task, ptr(unavailableGroundingReceipt())); err == nil || !strings.Contains(err.Error(), "input identity") {
		t.Fatalf("prepare mismatched identity error = %v", err)
	}
}

func createRunningDesignDocumentGroundingTask(t *testing.T, mode string) db.AgentTaskQueue {
	t.Helper()
	projectID := createProjectForDesignTest(t, "A3 grounding completion")
	agentID := handlerTestAgentID(t)
	w := httptest.NewRecorder()
	testHandler.CreateDesignDocumentAgentTask(w, newRequest("POST", "/api/design-documents/agent-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"project_id": projectID, "agent_id": agentID, "requirement": "Ground a customer page.", "repository_grounding_mode": mode,
	}))
	if w.Code != http.StatusAccepted {
		t.Fatalf("create task status=%d body=%s", w.Code, w.Body.String())
	}
	var created DesignDocumentAgentTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_document_input_snapshot WHERE task_id = $1`, created.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, created.ID)
	})
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`, created.ID); err != nil {
		t.Fatal(err)
	}
	task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(created.ID))
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func unavailableGroundingReceipt() designdocument.RepositoryGrounding {
	return designdocument.RepositoryGrounding{
		SchemaVersion: designdocument.GroundingSchemaVersion, Status: designdocument.GroundingUnavailable,
		Repositories: []designdocument.GroundedRepository{}, Facts: []designdocument.GroundingFact{},
		Conflicts: []designdocument.GroundingObservation{}, Missing: []designdocument.GroundingObservation{},
		Warnings: []string{"Repository access was explicitly unavailable."},
	}
}
