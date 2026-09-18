package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type afterCommitTxStarter struct {
	pool        *pgxpool.Pool
	afterCommit func()
	once        sync.Once
}

func (s *afterCommitTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &afterCommitTx{Tx: tx, afterCommit: func() { s.once.Do(s.afterCommit) }}, nil
}

type afterCommitTx struct {
	pgx.Tx
	afterCommit func()
}

func (t *afterCommitTx) Commit(ctx context.Context) error {
	if err := t.Tx.Commit(ctx); err != nil {
		return err
	}
	if t.afterCommit != nil {
		t.afterCommit()
		t.afterCommit = nil
	}
	return nil
}

func TestPublishAttachmentsChangedCarriesIssueScope(t *testing.T) {
	bus := events.New()
	svc := &IssueService{Bus: bus}
	workspaceID := util.MustParseUUID("11111111-1111-4111-8111-111111111111")
	issueID := util.MustParseUUID("22222222-2222-4222-8222-222222222222")
	actorID := util.MustParseUUID("33333333-3333-4333-8333-333333333333")
	var got events.Event
	bus.Subscribe(protocol.EventIssueAttachmentsChanged, func(e events.Event) { got = e })

	svc.PublishAttachmentsChanged(context.Background(), db.Issue{ID: issueID, WorkspaceID: workspaceID}, actorID)

	if got.Type != protocol.EventIssueAttachmentsChanged || got.WorkspaceID != util.UUIDToString(workspaceID) || got.ActorType != "member" || got.ActorID != util.UUIDToString(actorID) {
		t.Fatalf("event envelope = %+v", got)
	}
	payload, ok := got.Payload.(map[string]any)
	if !ok || payload["issue_id"] != util.UUIDToString(issueID) {
		t.Fatalf("event payload = %#v", got.Payload)
	}
}

func TestPublishAttachmentsChangedAlsoBroadcastsUpdatedDescription(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, _, issueID := seedAttributionFixture(t, pool)
	workspaceUUID := util.MustParseUUID(workspaceID)
	issueUUID := util.MustParseUUID(issueID)
	actorID := util.MustParseUUID(userID)
	const description = "![](/api/attachments/22222222-2222-4222-8222-222222222222/download)"
	if _, err := pool.Exec(ctx, `UPDATE issue SET description = $1 WHERE id = $2`, description, issueUUID); err != nil {
		t.Fatalf("update issue description: %v", err)
	}
	issue, err := q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID: issueUUID, WorkspaceID: workspaceUUID,
	})
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}

	bus := events.New()
	svc := &IssueService{Bus: bus, Queries: q}
	var updated events.Event
	var ordered []events.Event
	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) { updated = e })
	bus.SubscribeAll(func(e events.Event) {
		if e.Type == protocol.EventIssueUpdated || e.Type == protocol.EventIssueAttachmentsChanged {
			ordered = append(ordered, e)
		}
	})

	svc.PublishAttachmentsChanged(ctx, issue, actorID)

	if updated.Type != protocol.EventIssueUpdated || updated.WorkspaceID != workspaceID || updated.ActorType != "member" || updated.ActorID != userID {
		t.Fatalf("issue update envelope = %+v", updated)
	}
	payload, ok := updated.Payload.(map[string]any)
	if !ok {
		t.Fatalf("issue update payload = %#v", updated.Payload)
	}
	issuePayload, ok := payload["issue"].(map[string]any)
	if !ok {
		t.Fatalf("issue update body = %#v", payload["issue"])
	}
	gotDescription, ok := issuePayload["description"].(*string)
	if !ok || gotDescription == nil || *gotDescription != description {
		t.Fatalf("broadcast description = %#v, want %q", issuePayload["description"], description)
	}
	for _, key := range []string{"assignee_changed", "status_changed", "project_changed"} {
		if changed, ok := payload[key].(bool); !ok || changed {
			t.Fatalf("%s = %#v, want false", key, payload[key])
		}
	}
	if len(ordered) != 2 || ordered[0].Type != protocol.EventIssueUpdated || ordered[1].Type != protocol.EventIssueAttachmentsChanged {
		t.Fatalf("event order = %#v, want issue:updated then issue_attachments:changed", ordered)
	}
	attachmentPayload, ok := ordered[1].Payload.(map[string]any)
	if !ok || attachmentPayload["issue_revision"] != issue.Revision {
		t.Fatalf("attachment event payload = %#v, want revision %d", ordered[1].Payload, issue.Revision)
	}
}

func TestCreateReloadsProjectIssueBeforePostCommitEffects(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)
	workspaceUUID := util.MustParseUUID(workspaceID)
	userUUID := util.MustParseUUID(userID)
	agentUUID := util.MustParseUUID(agentID)
	var projectID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, status, created_by)
		VALUES ($1, 'post-commit project', 'planned', $2) RETURNING id
	`, workspaceUUID, userUUID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	config := pool.Config()
	config.MaxConns = 1
	servicePool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("create single-connection pool: %v", err)
	}
	t.Cleanup(servicePool.Close)
	q := db.New(servicePool)

	bus := events.New()
	taskService := &TaskService{Queries: q, TxStarter: servicePool, Bus: bus}
	txStarter := &afterCommitTxStarter{pool: servicePool, afterCommit: func() {
		if _, err := pool.Exec(ctx, `UPDATE project SET status = 'completed' WHERE id = $1`, projectID); err != nil {
			t.Errorf("complete project after issue commit: %v", err)
			return
		}
		if _, err := pool.Exec(ctx, `UPDATE issue SET status = 'done' WHERE project_id = $1`, projectID); err != nil {
			t.Errorf("complete issue after issue commit: %v", err)
		}
	}}
	issueService := NewIssueService(q, txStarter, bus, nil, taskService)
	createdStatus := ""
	bus.Subscribe(protocol.EventIssueCreated, func(event events.Event) {
		payload, _ := event.Payload.(map[string]any)
		issue, _ := payload["issue"].(map[string]any)
		createdStatus, _ = issue["status"].(string)
		issueID, _ := issue["id"].(string)
		if _, err := q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: util.MustParseUUID(issueID), WorkspaceID: workspaceUUID}); err != nil {
			t.Errorf("created listener database query: %v", err)
		}
	})

	res, err := issueService.Create(ctx, IssueCreateParams{
		WorkspaceID: workspaceUUID,
		Title:       "post-commit status refresh",
		Status:      "todo",
		Priority:    "none",
		AssigneeType: pgtype.Text{
			String: "agent", Valid: true,
		},
		AssigneeID:  agentUUID,
		CreatorType: "member",
		CreatorID:   userUUID,
		ProjectID:   projectID,
	}, IssueCreateOpts{
		AssignedAgentRunFireAt: time.Now().Add(time.Minute),
		BroadcastPayload: func(issue db.Issue, _ []db.Attachment, _ []db.IssueLabel) map[string]any {
			return map[string]any{"issue": IssueToMap(issue, "TST")}
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if res.Issue.Status != "done" || createdStatus != "done" {
		t.Fatalf("post-commit state = result %q event %q, want done/done", res.Issue.Status, createdStatus)
	}
	if res.AssignedTaskID.Valid {
		t.Fatalf("terminal create returned deferred task %s", util.UUIDToString(res.AssignedTaskID))
	}
	var active, cancelled int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE status IN ('deferred', 'queued', 'dispatched', 'running')),
		       count(*) FILTER (WHERE status = 'cancelled')
		FROM agent_task_queue WHERE issue_id = $1`, res.Issue.ID).Scan(&active, &cancelled); err != nil {
		t.Fatalf("count deferred task states: %v", err)
	}
	if active != 0 || cancelled != 1 {
		t.Fatalf("terminal deferred tasks = active %d cancelled %d, want 0/1", active, cancelled)
	}
}

func TestCreateReconcilesProjectMoveDuringCreatedEvent(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)
	workspaceUUID := util.MustParseUUID(workspaceID)
	userUUID := util.MustParseUUID(userID)
	agentUUID := util.MustParseUUID(agentID)
	var openProjectID, completedProjectID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, status, created_by)
		VALUES ($1, 'created-event source', 'planned', $2) RETURNING id
	`, workspaceUUID, userUUID).Scan(&openProjectID); err != nil {
		t.Fatalf("create source project: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, status, created_by)
		VALUES ($1, 'created-event target', 'completed', $2) RETURNING id
	`, workspaceUUID, userUUID).Scan(&completedProjectID); err != nil {
		t.Fatalf("create target project: %v", err)
	}

	bus := events.New()
	createdStatus := ""
	reconciledStatus := ""
	reconciledPriority := ""
	reconciliations := 0
	bus.Subscribe(protocol.EventIssueCreated, func(event events.Event) {
		payload, _ := event.Payload.(map[string]any)
		issue, _ := payload["issue"].(map[string]any)
		createdStatus, _ = issue["status"].(string)
		issueID, _ := issue["id"].(string)
		if _, err := pool.Exec(ctx, `
			UPDATE issue SET project_id = $1, status = 'done', updated_at = now()
			WHERE id = $2 AND workspace_id = $3
		`, completedProjectID, util.MustParseUUID(issueID), workspaceUUID); err != nil {
			t.Errorf("move issue during created event: %v", err)
		}
	})
	bus.Subscribe(protocol.EventIssueUpdated, func(event events.Event) {
		payload, _ := event.Payload.(map[string]any)
		if realtimeOnly, _ := payload["realtime_only"].(bool); !realtimeOnly {
			t.Errorf("reconciliation missing realtime_only marker: %#v", payload)
		}
		issue, _ := payload["issue"].(map[string]any)
		reconciledStatus, _ = issue["status"].(string)
		reconciledPriority, _ = issue["priority"].(string)
		reconciliations++
		if reconciliations == 1 {
			issueID, _ := issue["id"].(string)
			if _, err := pool.Exec(ctx, `UPDATE issue SET priority = 'high', updated_at = now() WHERE id = $1`, util.MustParseUUID(issueID)); err != nil {
				t.Errorf("second update during reconciliation: %v", err)
			}
		}
	})
	taskService := &TaskService{Queries: q, TxStarter: pool, Bus: bus}
	issueService := NewIssueService(q, pool, bus, nil, taskService)
	res, err := issueService.Create(ctx, IssueCreateParams{
		WorkspaceID:  workspaceUUID,
		Title:        "move during created event",
		Status:       "todo",
		Priority:     "none",
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   agentUUID,
		CreatorType:  "member",
		CreatorID:    userUUID,
		ProjectID:    openProjectID,
	}, IssueCreateOpts{BroadcastPayload: func(issue db.Issue, _ []db.Attachment, _ []db.IssueLabel) map[string]any {
		return map[string]any{"issue": IssueToMap(issue, "TST")}
	}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if createdStatus != "todo" || reconciledStatus != "done" || reconciledPriority != "high" || reconciliations != 2 {
		t.Fatalf("events = created %q reconciled %q/%q count %d, want todo/done/high/2",
			createdStatus, reconciledStatus, reconciledPriority, reconciliations)
	}
	if res.Issue.Status != "done" || res.Issue.Priority != "high" || res.Issue.ProjectID != completedProjectID {
		t.Fatalf("final issue = status %q priority %q project %s, want done/high/%s",
			res.Issue.Status, res.Issue.Priority, util.UUIDToString(res.Issue.ProjectID), util.UUIDToString(completedProjectID))
	}
	var tasks int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, res.Issue.ID).Scan(&tasks); err != nil {
		t.Fatalf("count guarded tasks: %v", err)
	}
	if tasks != 0 {
		t.Fatalf("create-time runnable guard inserted %d tasks for terminal issue", tasks)
	}
}

func TestCreateLocksWorkspaceBeforeProject(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, _, _ := seedAttributionFixture(t, pool)
	workspaceUUID := util.MustParseUUID(workspaceID)
	userUUID := util.MustParseUUID(userID)
	var projectID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, status, created_by)
		VALUES ($1, 'lock-order project', 'planned', $2) RETURNING id
	`, workspaceUUID, userUUID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}

	workspaceTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin workspace lock: %v", err)
	}
	if _, err := workspaceTx.Exec(ctx, `SELECT id FROM workspace WHERE id = $1 FOR UPDATE`, workspaceUUID); err != nil {
		t.Fatalf("lock workspace: %v", err)
	}
	issueService := NewIssueService(q, pool, events.New(), nil, nil)
	type createResult struct {
		result IssueCreateResult
		err    error
	}
	created := make(chan createResult, 1)
	go func() {
		result, err := issueService.Create(ctx, IssueCreateParams{
			WorkspaceID: workspaceUUID, Title: "lock-order issue", Status: "todo", Priority: "none",
			CreatorType: "member", CreatorID: userUUID, ProjectID: projectID,
		}, IssueCreateOpts{})
		created <- createResult{result: result, err: err}
	}()
	select {
	case result := <-created:
		t.Fatalf("create did not wait for workspace lock: %v", result.err)
	case <-time.After(100 * time.Millisecond):
	}

	probe, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin project probe: %v", err)
	}
	_, projectLockErr := probe.Exec(ctx, `SELECT id FROM project WHERE id = $1 FOR UPDATE NOWAIT`, projectID)
	_ = probe.Rollback(ctx)
	_ = workspaceTx.Rollback(ctx)
	var result createResult
	select {
	case result = <-created:
	case <-time.After(3 * time.Second):
		t.Fatal("create remained blocked after workspace lock release")
	}
	if projectLockErr != nil {
		t.Fatalf("create locked project before workspace: %v", projectLockErr)
	}
	if result.err != nil {
		t.Fatalf("Create: %v", result.err)
	}
}

func TestCreateMediaGatedIssueCommitsDeferredTaskAtomicallyBeforeCreatedEvent(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)
	workspaceUUID := util.MustParseUUID(workspaceID)
	userUUID := util.MustParseUUID(userID)
	agentUUID := util.MustParseUUID(agentID)

	bus := events.New()
	wakeup := &stubWakeup{}
	createdOverlay := json.RawMessage(`{"mcpServers":{"creator":{"url":"https://creator.example"}}}`)
	taskService := &TaskService{
		Queries:      q,
		TxStarter:    pool,
		Bus:          bus,
		Composio:     &stubOverlayBuilder{resp: createdOverlay},
		FeatureFlags: composioMCPAppsTestFlags(true),
		Wakeup:       wakeup,
	}
	var competingTaskID pgtype.UUID
	var competingErr error
	txStarter := &afterCommitTxStarter{pool: pool, afterCommit: func() {
		// This callback runs after the issue transaction is visible but before
		// IssueService resumes. The old implementation committed only the issue,
		// so this ordinary queued insert won the unique slot. The fixed path has
		// already committed the deferred task in the same transaction.
		var issueID, runtimeID pgtype.UUID
		if err := pool.QueryRow(ctx, `
			SELECT i.id, a.runtime_id
			FROM issue i
			JOIN agent a ON a.id = i.assignee_id
			WHERE i.workspace_id = $1 AND i.title = 'Media-gated issue'`, workspaceUUID).
			Scan(&issueID, &runtimeID); err != nil {
			competingErr = fmt.Errorf("discover committed issue: %w", err)
			return
		}
		competingErr = pool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
			VALUES ($1, $2, $3, 'queued', 1)
			RETURNING id`, agentUUID, runtimeID, issueID).Scan(&competingTaskID)
	}}
	issueService := NewIssueService(q, txStarter, bus, nil, taskService)

	var callbackErr error
	var mergedCommentID pgtype.UUID
	bus.Subscribe(protocol.EventIssueCreated, func(event events.Event) {
		payload, ok := event.Payload.(map[string]any)
		if !ok {
			callbackErr = fmt.Errorf("issue:created payload = %#v", event.Payload)
			return
		}
		issueID, ok := payload["issue_id"].(string)
		if !ok {
			callbackErr = fmt.Errorf("issue:created issue_id = %#v", payload["issue_id"])
			return
		}
		issueUUID := util.MustParseUUID(issueID)

		var status string
		var mediaPending bool
		var runtimeOverlay []byte
		if err := pool.QueryRow(ctx, `
			SELECT status, context->>'channel_issue_media_pending' = 'true', runtime_mcp_overlay
			FROM agent_task_queue
			WHERE issue_id = $1 AND agent_id = $2`, issueUUID, agentUUID).Scan(&status, &mediaPending, &runtimeOverlay); err != nil {
			callbackErr = fmt.Errorf("load task during issue:created: %w", err)
			return
		}
		if status != "deferred" || !mediaPending {
			callbackErr = fmt.Errorf("task during issue:created = status %q media_pending %v", status, mediaPending)
			return
		}
		var runtimeOverlayValue, createdOverlayValue any
		if err := json.Unmarshal(runtimeOverlay, &runtimeOverlayValue); err != nil {
			callbackErr = fmt.Errorf("decode task overlay during issue:created: %w", err)
			return
		}
		if err := json.Unmarshal(createdOverlay, &createdOverlayValue); err != nil || !reflect.DeepEqual(runtimeOverlayValue, createdOverlayValue) {
			callbackErr = fmt.Errorf("task overlay during issue:created = %s, want %s", runtimeOverlay, createdOverlay)
			return
		}

		if err := pool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
			VALUES ($1, $2, 'member', $3, 'Immediate follow-up')
			RETURNING id`, issueUUID, workspaceUUID, userUUID).Scan(&mergedCommentID); err != nil {
			callbackErr = fmt.Errorf("insert immediate comment: %w", err)
			return
		}
		if _, err := q.MergeCommentIntoPendingTask(ctx, db.MergeCommentIntoPendingTaskParams{
			IssueID:                 issueUUID,
			AgentID:                 agentUUID,
			NewTriggerCommentID:     mergedCommentID,
			NewOriginatorUserID:     userUUID,
			NewAccountableUserID:    userUUID,
			NewOriginatorSource:     pgtype.Text{String: "direct_human", Valid: true},
			NewTriggerEvidenceKind:  pgtype.Text{String: "comment", Valid: true},
			NewTriggerEvidenceRefID: mergedCommentID,
		}); !errors.Is(err, pgx.ErrNoRows) {
			callbackErr = fmt.Errorf("new comment thread must not merge into the assignment: %v", err)
		}
	})

	result, err := issueService.Create(ctx, IssueCreateParams{
		WorkspaceID:  workspaceUUID,
		Title:        "Media-gated issue",
		Status:       "todo",
		Priority:     "medium",
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   agentUUID,
		CreatorType:  "member",
		CreatorID:    userUUID,
	}, IssueCreateOpts{AssignedAgentRunFireAt: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if callbackErr != nil {
		t.Fatal(callbackErr)
	}
	if !result.AssignedTaskID.Valid {
		t.Fatal("media-gated issue did not return its deferred task")
	}
	if len(wakeup.calls) != 1 || wakeup.calls[0].taskID != "" {
		t.Fatalf("post-commit schedule wakeups = %+v, want one wakeup without a ready task id", wakeup.calls)
	}
	if !isDuplicatePendingTaskErr(competingErr) {
		t.Fatalf("post-commit competing queued insert error = %v, want duplicate pending task", competingErr)
	}
	if competingTaskID.Valid {
		t.Fatalf("post-commit competing task unexpectedly won: %s", util.UUIDToString(competingTaskID))
	}

	var taskID, triggerCommentID, runtimeID pgtype.UUID
	var taskCount int
	if err := pool.QueryRow(ctx, `
		SELECT id, trigger_comment_id, runtime_id, count(*) OVER ()
		FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2
		  AND status IN ('queued', 'dispatched', 'deferred')
		ORDER BY created_at
		LIMIT 1`, result.Issue.ID, agentUUID).
		Scan(&taskID, &triggerCommentID, &runtimeID, &taskCount); err != nil {
		t.Fatalf("load final pending task: %v", err)
	}
	if taskCount != 1 || taskID != result.AssignedTaskID || triggerCommentID.Valid {
		t.Fatalf("pending task = count %d id %v trigger %v, want one assignment task %v without a comment trigger", taskCount, taskID, triggerCommentID, result.AssignedTaskID)
	}
	if wakeup.calls[0].runtimeID != util.UUIDToString(runtimeID) {
		t.Fatalf("schedule wakeup runtime = %q, want %q", wakeup.calls[0].runtimeID, util.UUIDToString(runtimeID))
	}
}

func TestHydrateDeferredChannelIssueTaskOverlayDoesNotOverwriteMergedCommentPlan(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	userUUID := util.MustParseUUID(userID)

	plainService := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	task, err := plainService.EnqueueDeferredChannelIssueTask(ctx, db.Issue{
		ID:           util.MustParseUUID(issueID),
		WorkspaceID:  util.MustParseUUID(workspaceID),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   util.MustParseUUID(agentID),
		CreatorType:  "member",
		CreatorID:    userUUID,
		Priority:     "medium",
	}, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("EnqueueDeferredChannelIssueTask: %v", err)
	}

	var commentID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'New effective trigger')
		RETURNING id`, task.IssueID, workspaceID, userID).Scan(&commentID); err != nil {
		t.Fatalf("seed merged comment: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET trigger_comment_id=$2 WHERE id=$1`, task.ID, commentID); err != nil {
		t.Fatal(err)
	}
	mergedOverlay := json.RawMessage(`{"mcpServers":{"merged":{"url":"https://merged.example"}}}`)
	if _, err := q.MergeCommentIntoPendingTask(ctx, db.MergeCommentIntoPendingTaskParams{
		IssueID:                 task.IssueID,
		AgentID:                 task.AgentID,
		NewTriggerCommentID:     commentID,
		NewOriginatorUserID:     userUUID,
		NewAccountableUserID:    userUUID,
		NewRuntimeMcpOverlay:    mergedOverlay,
		NewOriginatorSource:     pgtype.Text{String: "direct_human", Valid: true},
		NewTriggerEvidenceKind:  pgtype.Text{String: "comment", Valid: true},
		NewTriggerEvidenceRefID: commentID,
	}); err != nil {
		t.Fatalf("MergeCommentIntoPendingTask: %v", err)
	}

	hydrationBuilder := &stubOverlayBuilder{
		resp: json.RawMessage(`{"mcpServers":{"stale":{"url":"https://stale.example"}}}`),
	}
	hydratingService := &TaskService{
		Queries:      q,
		Composio:     hydrationBuilder,
		FeatureFlags: composioMCPAppsTestFlags(true),
	}
	if err := hydratingService.hydrateDeferredChannelIssueTaskOverlay(ctx, task); err != nil {
		t.Fatalf("hydrateDeferredChannelIssueTaskOverlay: %v", err)
	}

	var triggerID pgtype.UUID
	var storedOverlay []byte
	if err := pool.QueryRow(ctx, `
		SELECT trigger_comment_id, runtime_mcp_overlay
		FROM agent_task_queue WHERE id = $1`, task.ID).Scan(&triggerID, &storedOverlay); err != nil {
		t.Fatalf("load hydrated task: %v", err)
	}
	if triggerID != commentID {
		t.Fatalf("trigger_comment_id = %s, want %s", util.UUIDToString(triggerID), util.UUIDToString(commentID))
	}
	var storedValue, mergedValue any
	if err := json.Unmarshal(storedOverlay, &storedValue); err != nil {
		t.Fatalf("decode stored runtime_mcp_overlay: %v", err)
	}
	if err := json.Unmarshal(mergedOverlay, &mergedValue); err != nil {
		t.Fatalf("decode expected runtime_mcp_overlay: %v", err)
	}
	if !reflect.DeepEqual(storedValue, mergedValue) {
		t.Fatalf("runtime_mcp_overlay = %s, want merged overlay %s", storedOverlay, mergedOverlay)
	}
}
