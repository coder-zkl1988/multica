package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateInvestigationQueuesDiagnosticTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "investigation-agent", nil)
	w := httptest.NewRecorder()
	testHandler.CreateInvestigation(w, newRequest(http.MethodPost, "/api/investigations?workspace_id="+testWorkspaceID, map[string]any{
		"environment": "production",
		"description": "Checkout requests time out after 30 seconds",
		"agent_id":    agentID,
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var response struct {
		ID       string `json:"id"`
		Status   string `json:"status"`
		TaskID   string `json:"current_task_id"`
		Internal string `json:"diagnostic_capability"`
	}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Status != "investigating" || response.TaskID == "" {
		t.Fatalf("response = %+v", response)
	}
	if response.Internal != "" {
		t.Fatal("internal capability name leaked through product API")
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE investigation_id = $1`, response.ID)
		testPool.Exec(context.Background(), `DELETE FROM investigation WHERE id = $1`, response.ID)
	})

	var taskCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_task_queue WHERE investigation_id = $1`, response.ID).Scan(&taskCount); err != nil {
		t.Fatal(err)
	}
	if taskCount != 1 {
		t.Fatalf("task count = %d", taskCount)
	}
}
