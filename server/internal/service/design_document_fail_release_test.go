package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// A design document task that dies mid-run must release the document.
//
// The regression this pins: FailTask — the path a daemon-reported failure
// takes — released the design system and the profile but not the document, so
// a run killed by a provider stream disconnect left active_task_id pointing at
// a task that would never finish. The document then read 生成中 forever, and
// every guard keyed on that pointer refused to save, discard, adjust or delete
// it. The sweepers call HandleFailedTasks, which does release it, but they only
// ever see tasks they failed themselves — an agent-reported failure never
// reaches them.
func newDesignDocumentFailPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dbURL := testDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database unreachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

type designDocumentFailFixture struct {
	pool        *pgxpool.Pool
	workspaceID string
	documentID  string
	taskID      string
}

// createDesignDocumentFailFixture seeds a running design document task exactly
// as the quick-create path does: no issue, no chat session, the binding
// carried entirely by the context JSONB that
// parseDesignDocumentTaskWorkspaceContext reads.
func createDesignDocumentFailFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) designDocumentFailFixture {
	t.Helper()

	suffix := time.Now().UnixNano()

	var userID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, "Design Doc Fail Test", fmt.Sprintf("design-doc-fail-%d@multica.ai", suffix)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}

	var workspaceID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, '', 'DDF') RETURNING id
	`, "Design Doc Fail Test", fmt.Sprintf("design-doc-fail-%d", suffix)).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
	`, workspaceID, userID); err != nil {
		t.Fatalf("create member: %v", err)
	}

	var runtimeID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider,
			status, device_info, metadata, last_seen_at, visibility, owner_id
		)
		VALUES ($1, NULL, 'Design Doc Fail Runtime', 'cloud', 'design_doc_fail_test',
		        'online', 'test runtime', '{}'::jsonb, now(), 'private', $2)
		RETURNING id
	`, workspaceID, userID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	var agentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'Design Doc Fail Agent', '', 'cloud', '{}'::jsonb, $2, 'private', 1, $3)
		RETURNING id
	`, workspaceID, runtimeID, userID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	var projectID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, description, created_by)
		VALUES ($1, 'Design Doc Fail Project', '', $2) RETURNING id
	`, workspaceID, userID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}

	taskContext, err := json.Marshal(map[string]any{
		"type":         "design_document_task",
		"workspace_id": workspaceID,
		"project_id":   projectID,
		"operation":    "generate",
	})
	if err != nil {
		t.Fatalf("marshal task context: %v", err)
	}

	var taskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, status, priority, context, runtime_id, started_at)
		VALUES ($1, 'running', 0, $2::jsonb, $3, now())
		RETURNING id
	`, agentID, string(taskContext), runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}

	var documentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO design_document (
			workspace_id, project_id, title, platform, recipe,
			current_agent_id, active_task_id, active_operation, input_snapshot, created_by
		)
		VALUES ($1, $2, 'Fail Release Doc', 'web', 'test-recipe', $3, $4, 'generate', '{}'::jsonb, $5)
		RETURNING id
	`, workspaceID, projectID, agentID, taskID, userID).Scan(&documentID); err != nil {
		t.Fatalf("create design document: %v", err)
	}

	t.Cleanup(func() {
		c := context.Background()
		pool.Exec(c, `DELETE FROM design_document WHERE project_id = $1`, projectID)
		pool.Exec(c, `DELETE FROM task_message WHERE task_id = $1`, taskID)
		pool.Exec(c, `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID)
		pool.Exec(c, `DELETE FROM project WHERE id = $1`, projectID)
		pool.Exec(c, `DELETE FROM agent WHERE id = $1`, agentID)
		pool.Exec(c, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
		pool.Exec(c, `DELETE FROM member WHERE workspace_id = $1`, workspaceID)
		pool.Exec(c, `DELETE FROM workspace WHERE id = $1`, workspaceID)
		pool.Exec(c, `DELETE FROM "user" WHERE id = $1`, userID)
	})

	return designDocumentFailFixture{
		pool:        pool,
		workspaceID: workspaceID,
		documentID:  documentID,
		taskID:      taskID,
	}
}

func (f designDocumentFailFixture) readDocument(t *testing.T, ctx context.Context) (activeTaskID *string, lastError []byte) {
	t.Helper()
	if err := f.pool.QueryRow(ctx, `
		SELECT active_task_id, last_error FROM design_document WHERE id = $1
	`, f.documentID).Scan(&activeTaskID, &lastError); err != nil {
		t.Fatalf("read design document: %v", err)
	}
	return activeTaskID, lastError
}

func TestFailTaskReleasesTheDesignDocumentItWasGenerating(t *testing.T) {
	ctx := context.Background()
	pool := newDesignDocumentFailPool(t)
	fixture := createDesignDocumentFailFixture(t, ctx, pool)

	svc := &TaskService{Queries: db.New(pool), TxStarter: pool, Bus: events.New()}
	taskUUID, err := util.ParseUUID(fixture.taskID)
	if err != nil {
		t.Fatalf("parse task id: %v", err)
	}

	// The exact failure that wedged the real document: the provider stream
	// dropped mid-run and the daemon reported it.
	if _, err := svc.FailTask(ctx, taskUUID, "stream disconnected before completion", "", "", "",
		"agent_error.provider_network", false, "", ""); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	activeTaskID, lastError := fixture.readDocument(t, ctx)
	if activeTaskID != nil {
		t.Fatalf("document still points at task %s after its run failed; it would read 生成中 forever", *activeTaskID)
	}
	// The pointer alone going away is not enough: without the recorded error
	// the document silently reads as an untouched draft and the user never
	// learns the run died.
	if len(lastError) == 0 || string(lastError) == "null" {
		t.Fatalf("failure was not recorded on the document: last_error = %q", string(lastError))
	}
	var recorded map[string]any
	if err := json.Unmarshal(lastError, &recorded); err != nil {
		t.Fatalf("last_error is not valid JSON: %v", err)
	}
	if recorded["code"] != "agent_error.provider_network" {
		t.Fatalf("last_error code = %v, want the task's own failure reason", recorded["code"])
	}
}

// A failed package audit must leave every broken rule on the document, not
// only the first line of the failure text. The rerun reads this back as its
// previous-failures feedback, so dropping the array makes the retry blind.
func TestFailTaskRecordsEveryFailureDiagnosticOnTheDocument(t *testing.T) {
	ctx := context.Background()
	pool := newDesignDocumentFailPool(t)
	fixture := createDesignDocumentFailFixture(t, ctx, pool)

	svc := &TaskService{Queries: db.New(pool), TxStarter: pool, Bus: events.New()}
	taskUUID, err := util.ParseUUID(fixture.taskID)
	if err != nil {
		t.Fatalf("parse task id: %v", err)
	}

	diagnostics := []DesignDocumentFailureDiagnostic{
		{Code: "prototype_inline_handler", Path: "prototype/orders.html", Message: "inline event handler is not allowed"},
		{Code: "prototype_script_forbidden_api", Path: "prototype/app.js", Message: "network access is not allowed"},
	}
	if _, _, err := svc.FailTaskWithTransition(ctx, taskUUID,
		"design document package failed static audit", "", "", "",
		"design_document_audit_failed", false, "", "", diagnostics); err != nil {
		t.Fatalf("FailTaskWithTransition: %v", err)
	}

	activeTaskID, lastError := fixture.readDocument(t, ctx)
	if activeTaskID != nil {
		t.Fatalf("document still points at the failed task %s", *activeTaskID)
	}
	var recorded struct {
		Code        string                            `json:"code"`
		Message     string                            `json:"message"`
		TaskID      string                            `json:"task_id"`
		Operation   string                            `json:"operation"`
		Diagnostics []DesignDocumentFailureDiagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(lastError, &recorded); err != nil {
		t.Fatalf("last_error is not valid JSON: %v (%s)", err, lastError)
	}
	// The pre-existing keys keep their contract: an older client still finds
	// the failure code and message.
	if recorded.Code != "design_document_audit_failed" || recorded.Message == "" ||
		recorded.TaskID != fixture.taskID || recorded.Operation != "generate" {
		t.Fatalf("last_error lost its original keys: %s", lastError)
	}
	if len(recorded.Diagnostics) != len(diagnostics) {
		t.Fatalf("last_error diagnostics = %+v, want %+v", recorded.Diagnostics, diagnostics)
	}
	for index, want := range diagnostics {
		if recorded.Diagnostics[index] != want {
			t.Fatalf("diagnostic %d = %+v, want %+v", index, recorded.Diagnostics[index], want)
		}
	}
}

// A failure with no structured findings keeps the old shape exactly: no empty
// diagnostics key that a reader could mistake for "the audit found nothing".
func TestFailTaskOmitsEmptyDiagnosticsFromLastError(t *testing.T) {
	ctx := context.Background()
	pool := newDesignDocumentFailPool(t)
	fixture := createDesignDocumentFailFixture(t, ctx, pool)

	svc := &TaskService{Queries: db.New(pool), TxStarter: pool, Bus: events.New()}
	taskUUID, err := util.ParseUUID(fixture.taskID)
	if err != nil {
		t.Fatalf("parse task id: %v", err)
	}
	if _, err := svc.FailTask(ctx, taskUUID, "stream disconnected before completion", "", "", "",
		"agent_error.provider_network", false, "", ""); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	_, lastError := fixture.readDocument(t, ctx)
	var recorded map[string]json.RawMessage
	if err := json.Unmarshal(lastError, &recorded); err != nil {
		t.Fatalf("last_error is not valid JSON: %v", err)
	}
	if _, present := recorded["diagnostics"]; present {
		t.Fatalf("last_error carries an empty diagnostics key: %s", lastError)
	}
}
