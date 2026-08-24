package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestResolveTaskWorkspaceIDFromInvestigationContext(t *testing.T) {
	raw, err := json.Marshal(InvestigationTaskContext{
		Type:            InvestigationTaskContextType,
		InvestigationID: "00000000-0000-4000-8000-000000000001",
		WorkspaceID:     "00000000-0000-4000-8000-000000000002",
		Environment:     "production",
	})
	if err != nil {
		t.Fatal(err)
	}
	task := db.AgentTaskQueue{
		InvestigationID: util.MustParseUUID("00000000-0000-4000-8000-000000000001"),
		Context:         raw,
	}

	got := (&TaskService{}).ResolveTaskWorkspaceID(context.Background(), task)
	if got != "00000000-0000-4000-8000-000000000002" {
		t.Fatalf("workspace = %q", got)
	}
}

func TestParseInvestigationContextRejectsWrongType(t *testing.T) {
	task := db.AgentTaskQueue{Context: []byte(`{"type":"quick_create","workspace_id":"wrong"}`)}
	if _, ok := parseInvestigationTaskContext(task); ok {
		t.Fatal("non-investigation context was accepted")
	}
}

func TestEnqueueInvestigationTaskIsSingleFlight(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)
	var investigationID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO investigation (
			workspace_id, title, description, environment, agent_id,
			diagnostic_capability, diagnostic_version, created_by
		) VALUES ($1, 'Timeout', 'Checkout times out', 'production', $2, 'automatic_diagnosis', 'v1', $3)
		RETURNING id
	`, workspaceID, agentID, userID).Scan(&investigationID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE investigation_id = $1`, investigationID)
		pool.Exec(context.Background(), `DELETE FROM investigation WHERE id = $1`, investigationID)
	})

	q := db.New(pool)
	investigation, err := q.GetInvestigationInWorkspace(ctx, db.GetInvestigationInWorkspaceParams{
		ID: util.MustParseUUID(investigationID), WorkspaceID: util.MustParseUUID(workspaceID),
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	actorID := util.MustParseUUID(userID)

	task, err := svc.EnqueueInvestigationTask(ctx, investigation, actorID, nil, nil)
	if err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	if task.InvestigationID != investigation.ID || task.IssueID.Valid {
		t.Fatalf("task ownership = investigation %v issue %v", task.InvestigationID.Valid, task.IssueID.Valid)
	}
	contextValue, ok := parseInvestigationTaskContext(task)
	if !ok || contextValue.Environment != "production" || contextValue.CapabilityVersion != "v1" {
		t.Fatalf("task context = %+v, ok=%v", contextValue, ok)
	}

	_, err = svc.EnqueueInvestigationTask(ctx, investigation, actorID, nil, nil)
	if !errors.Is(err, ErrInvestigationTaskActive) {
		t.Fatalf("second enqueue error = %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE investigation_id = $1`, investigationID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("task count = %d", count)
	}
}
