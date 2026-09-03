package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type firstCommitHookStarter struct {
	pool *pgxpool.Pool
	once sync.Once
	hook func()
}

func (s *firstCommitHookStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &firstCommitHookTx{Tx: tx, afterCommit: func() { s.once.Do(s.hook) }}, nil
}

type firstCommitHookTx struct {
	pgx.Tx
	afterCommit func()
}

type rollbackOnCommitStarter struct {
	pool *pgxpool.Pool
}

func (s *rollbackOnCommitStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &rollbackOnCommitTx{Tx: tx}, nil
}

func (tx *firstCommitHookTx) Commit(ctx context.Context) error {
	if err := tx.Tx.Commit(ctx); err != nil {
		return err
	}
	tx.afterCommit()
	return nil
}

func TestAutopilotCreateFirstCompletionPublishesRunStartBeforeRunDone(t *testing.T) {
	ctx := context.Background()
	title := fmt.Sprintf("Autopilot create-first completion %d", time.Now().UnixNano())
	var autopilotID, projectID, issueID string
	t.Cleanup(func() {
		if issueID != "" {
			_, _ = testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
		}
		if autopilotID != "" {
			_, _ = testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		}
		if projectID != "" {
			_, _ = testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectID)
		}
	})
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, created_by)
		VALUES ($1, $2, (SELECT id FROM "user" LIMIT 1)) RETURNING id::text
	`, testWorkspaceID, "Autopilot create-first project").Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	agentID := createHandlerTestAgent(t, "Autopilot create-first agent", nil)
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Create-first completion autopilot", "assignee_id": agentID,
		"execution_mode": "create_issue", "issue_title_template": title, "project_id": projectID,
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: %d %s", w.Code, w.Body.String())
	}
	var created AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode autopilot: %v", err)
	}
	autopilotID = created.ID
	queries := db.New(testPool)
	ap, err := queries.GetAutopilot(ctx, parseUUID(autopilotID))
	if err != nil {
		t.Fatalf("GetAutopilot: %v", err)
	}

	var eventTypes []string
	createdStatus := ""
	testHandler.Bus.SubscribeAll(func(event events.Event) {
		if event.WorkspaceID != testWorkspaceID {
			return
		}
		switch event.Type {
		case protocol.EventAutopilotRunStart, protocol.EventAutopilotRunDone:
			payload, _ := event.Payload.(map[string]any)
			if payload["autopilot_id"] != autopilotID {
				return
			}
			eventTypes = append(eventTypes, event.Type)
		case protocol.EventIssueCreated:
			payload, _ := event.Payload.(map[string]any)
			issue, _ := payload["issue"].(map[string]any)
			if issue["title"] == title {
				createdStatus, _ = issue["status"].(string)
			}
		}
	})
	originalStarter := testHandler.AutopilotService.TxStarter
	testHandler.AutopilotService.TxStarter = &firstCommitHookStarter{pool: testPool, hook: func() {
		if _, err := testPool.Exec(ctx, `UPDATE project SET status = 'completed' WHERE id = $1`, projectID); err != nil {
			t.Errorf("complete project: %v", err)
			return
		}
		if err := testPool.QueryRow(ctx, `
			UPDATE issue SET status = 'done' WHERE workspace_id = $1 AND title = $2
			RETURNING id::text
		`, testWorkspaceID, title).Scan(&issueID); err != nil {
			t.Errorf("complete created issue: %v", err)
			return
		}
		issue, err := queries.GetIssue(ctx, parseUUID(issueID))
		if err != nil {
			t.Errorf("load completed issue: %v", err)
			return
		}
		testHandler.Bus.Publish(events.Event{
			Type: protocol.EventIssueUpdated, WorkspaceID: testWorkspaceID,
			ActorType: "member", ActorID: testUserID,
			Payload: map[string]any{
				"issue": issueToResponse(issue, ""), "status_changed": true, "prev_status": "todo",
			},
		})
	}}
	t.Cleanup(func() { testHandler.AutopilotService.TxStarter = originalStarter })

	// Since MUL-6951 a dispatch with no actor resolves its principal from the
	// trigger; these tests pass no trigger, so they must exercise the manual
	// entry point with an explicit actor — the shape the UI actually produces.
	run, _, err := testHandler.AutopilotService.DispatchAutopilotManual(ctx, ap, pgtype.UUID{}, nil, parseUUID(testUserID))
	if err != nil {
		t.Fatalf("DispatchAutopilot: %v", err)
	}
	if run == nil || run.Status != "completed" {
		t.Fatalf("run = %+v, want completed", run)
	}
	if createdStatus != "done" {
		t.Fatalf("issue:created status = %q, want done", createdStatus)
	}
	runStartIndex, runDoneIndex := -1, -1
	runStartCount, runDoneCount := 0, 0
	for i, eventType := range eventTypes {
		if eventType == protocol.EventAutopilotRunStart {
			runStartCount++
			if runStartIndex == -1 {
				runStartIndex = i
			}
		}
		if eventType == protocol.EventAutopilotRunDone {
			runDoneCount++
			if runDoneIndex == -1 {
				runDoneIndex = i
			}
		}
	}
	if runStartCount != 1 || runDoneCount != 1 || runStartIndex >= runDoneIndex {
		t.Fatalf("run lifecycle order = %v, want one start before done", eventTypes)
	}
	var queued int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issueID).Scan(&queued); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if queued != 0 {
		t.Fatalf("queued tasks for terminal issue = %d, want 0", queued)
	}
}

func TestAutopilotCreateCommitFailurePublishesMatchingRunDone(t *testing.T) {
	ctx := context.Background()
	var autopilotID string
	t.Cleanup(func() {
		if autopilotID != "" {
			_, _ = testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		}
	})
	agentID := createHandlerTestAgent(t, "Autopilot commit-failure agent", nil)
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Commit failure autopilot", "assignee_id": agentID,
		"execution_mode": "create_issue", "issue_title_template": "must roll back",
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: %d %s", w.Code, w.Body.String())
	}
	var created AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode autopilot: %v", err)
	}
	autopilotID = created.ID
	queries := db.New(testPool)
	ap, err := queries.GetAutopilot(ctx, parseUUID(autopilotID))
	if err != nil {
		t.Fatalf("GetAutopilot: %v", err)
	}

	var eventTypes []string
	testHandler.Bus.SubscribeAll(func(event events.Event) {
		if event.WorkspaceID != testWorkspaceID {
			return
		}
		payload, _ := event.Payload.(map[string]any)
		if payload["autopilot_id"] != autopilotID {
			return
		}
		if event.Type == protocol.EventAutopilotRunStart || event.Type == protocol.EventAutopilotRunDone {
			eventTypes = append(eventTypes, event.Type)
		}
	})
	originalStarter := testHandler.AutopilotService.TxStarter
	testHandler.AutopilotService.TxStarter = &rollbackOnCommitStarter{pool: testPool}
	t.Cleanup(func() { testHandler.AutopilotService.TxStarter = originalStarter })

	run, _, err := testHandler.AutopilotService.DispatchAutopilotManual(ctx, ap, pgtype.UUID{}, nil, parseUUID(testUserID))
	if err == nil {
		t.Fatal("DispatchAutopilot succeeded, want commit failure")
	}
	if run == nil || run.Status != "failed" {
		t.Fatalf("run = %+v, want failed", run)
	}
	wantEvents := []string{protocol.EventAutopilotRunStart, protocol.EventAutopilotRunDone}
	if fmt.Sprint(eventTypes) != fmt.Sprint(wantEvents) {
		t.Fatalf("run lifecycle = %v, want %v", eventTypes, wantEvents)
	}
	var issues int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE origin_type = 'autopilot' AND origin_id = $1`, autopilotID).Scan(&issues); err != nil {
		t.Fatalf("count rolled-back issues: %v", err)
	}
	if issues != 0 {
		t.Fatalf("persisted issues = %d, want 0", issues)
	}
}
