package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/projectdesignsystem"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCompleteProjectDesignSystemTaskCreatesValidatedDraft(t *testing.T) {
	fixture := createProjectDesignSystemCompletionFixture(t, service.ProjectDesignSystemGenerate)
	artifacts := validProjectDesignSystemCompletionArtifacts()

	w := completeProjectDesignSystemTaskForTest(t, fixture.TaskID, map[string]any{
		"output":                          "Created the project design system.",
		"project_design_system_artifacts": artifacts,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("CompleteTask status = %d, want 200: %s", w.Code, w.Body.String())
	}

	queries := db.New(testPool)
	task, err := queries.GetAgentTask(context.Background(), parseUUID(fixture.TaskID))
	if err != nil {
		t.Fatalf("get completed task: %v", err)
	}
	if task.Status != "completed" {
		t.Fatalf("task status = %q, want completed", task.Status)
	}

	draft, err := queries.GetProjectDesignSystemPackageBySlot(context.Background(), db.GetProjectDesignSystemPackageBySlotParams{
		DesignSystemID: fixture.System.ID,
		Slot:           "draft",
		WorkspaceID:    parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("get generated draft: %v", err)
	}
	if draft.DesignMd != artifacts.DesignMD || draft.TokensCss != artifacts.TokensCSS || draft.ComponentsHtml != artifacts.ComponentsHTML {
		t.Fatalf("draft artifacts were not persisted together: %+v", draft)
	}
	if uuidToString(draft.SourceTaskID) != fixture.TaskID {
		t.Fatalf("source task = %q, want %q", uuidToString(draft.SourceTaskID), fixture.TaskID)
	}
	if uuidToString(draft.AgentID) != fixture.AgentID {
		t.Fatalf("draft agent = %q, want %q", uuidToString(draft.AgentID), fixture.AgentID)
	}
	if !json.Valid(draft.Manifest) || !json.Valid(draft.Validation) {
		t.Fatalf("draft manifest or validation is invalid JSON")
	}
	validated, err := projectdesignsystem.Validate(artifacts, nil)
	if err != nil {
		t.Fatalf("fixture artifacts are invalid: %v", err)
	}
	if draft.IntegritySha256 != validated.Manifest.Digest {
		t.Fatalf("draft digest = %q, want %q", draft.IntegritySha256, validated.Manifest.Digest)
	}

	system, err := queries.GetProjectDesignSystemInWorkspace(context.Background(), db.GetProjectDesignSystemInWorkspaceParams{
		ID:          fixture.System.ID,
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("get completed design system: %v", err)
	}
	if system.ActiveTaskID.Valid || system.ActiveOperation.Valid || len(system.LastError) != 0 {
		t.Fatalf("active task state was not cleared: %+v", system)
	}
	if _, err := queries.GetProjectDesignSystemPackageBySlot(context.Background(), db.GetProjectDesignSystemPackageBySlotParams{
		DesignSystemID: fixture.System.ID,
		Slot:           "saved",
		WorkspaceID:    parseUUID(testWorkspaceID),
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("saved package error = %v, want pgx.ErrNoRows", err)
	}
}

func TestCompleteProjectDesignSystemTaskRejectsMissingArtifactPayload(t *testing.T) {
	fixture := createProjectDesignSystemCompletionFixture(t, service.ProjectDesignSystemGenerate)

	w := completeProjectDesignSystemTaskForTest(t, fixture.TaskID, map[string]any{
		"output": "Agent reported success without files.",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CompleteTask status = %d, want 400: %s", w.Code, w.Body.String())
	}

	assertProjectDesignSystemTaskFailed(t, fixture.TaskID, "project_design_system_invalid_artifacts")
	assertProjectDesignSystemFailureState(t, fixture.System.ID, fixture.TaskID, "project_design_system_invalid_artifacts")
	if _, err := db.New(testPool).GetProjectDesignSystemPackageBySlot(context.Background(), db.GetProjectDesignSystemPackageBySlotParams{
		DesignSystemID: fixture.System.ID,
		Slot:           "draft",
		WorkspaceID:    parseUUID(testWorkspaceID),
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("draft package error = %v, want pgx.ErrNoRows", err)
	}
}

func TestCompleteProjectDesignSystemTaskRejectsUnsafeHTMLWithoutReplacingDraft(t *testing.T) {
	fixture := createProjectDesignSystemCompletionFixture(t, service.ProjectDesignSystemRegenerate)
	queries := db.New(testPool)
	oldDraftDigest := strings.Repeat("d", 64)
	oldSavedDigest := strings.Repeat("s", 64)
	upsertProjectDesignSystemPackageForTest(t, queries, fixture.System.ID, "draft", "draft-before-invalid-completion", oldDraftDigest)
	upsertProjectDesignSystemPackageForTest(t, queries, fixture.System.ID, "saved", "saved-before-invalid-completion", oldSavedDigest)

	artifacts := validProjectDesignSystemCompletionArtifacts()
	artifacts.ComponentsHTML = `<main data-design-node-id="overview" data-design-node-kind="block" data-design-node-label="Overview">Visible<script>alert(1)</script></main>`
	w := completeProjectDesignSystemTaskForTest(t, fixture.TaskID, map[string]any{
		"output":                          "Generated unsafe HTML.",
		"project_design_system_artifacts": artifacts,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CompleteTask status = %d, want 400: %s", w.Code, w.Body.String())
	}

	assertProjectDesignSystemTaskFailed(t, fixture.TaskID, "project_design_system_invalid_artifacts")
	assertProjectDesignSystemFailureState(t, fixture.System.ID, fixture.TaskID, "project_design_system_invalid_artifacts")
	assertProjectDesignSystemPackageDigest(t, queries, fixture.System.ID, "draft", oldDraftDigest)
	assertProjectDesignSystemPackageDigest(t, queries, fixture.System.ID, "saved", oldSavedDigest)
}

func TestCompleteProjectDesignSystemAdjustmentReplacesAllFilesAtomically(t *testing.T) {
	fixture := createProjectDesignSystemCompletionFixture(t, service.ProjectDesignSystemAdjust)
	queries := db.New(testPool)
	oldSavedDigest := strings.Repeat("a", 64)
	upsertProjectDesignSystemPackageForTest(t, queries, fixture.System.ID, "saved", "saved-must-remain", oldSavedDigest)
	upsertProjectDesignSystemPackageForTest(t, queries, fixture.System.ID, "draft", "old-design-md", strings.Repeat("b", 64))

	artifacts := validProjectDesignSystemCompletionArtifacts()
	artifacts.DesignMD = strings.Replace(artifacts.DesignMD, "calm and direct", "focused and precise", 1)
	artifacts.TokensCSS = strings.ReplaceAll(artifacts.TokensCSS, "#2463eb", "#0f766e")
	artifacts.ComponentsHTML = strings.Replace(artifacts.ComponentsHTML, "Create customer", "Add account", 1)
	w := completeProjectDesignSystemTaskForTest(t, fixture.TaskID, map[string]any{
		"output":                          "Adjusted the selected design system scope.",
		"project_design_system_artifacts": artifacts,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("CompleteTask status = %d, want 200: %s", w.Code, w.Body.String())
	}

	draft, err := queries.GetProjectDesignSystemPackageBySlot(context.Background(), db.GetProjectDesignSystemPackageBySlotParams{
		DesignSystemID: fixture.System.ID,
		Slot:           "draft",
		WorkspaceID:    parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("get adjusted draft: %v", err)
	}
	if draft.DesignMd != artifacts.DesignMD || draft.TokensCss != artifacts.TokensCSS || draft.ComponentsHtml != artifacts.ComponentsHTML {
		t.Fatalf("adjustment did not replace all artifacts together: %+v", draft)
	}
	if !draft.Instruction.Valid || draft.Instruction.String != "Keep controls compact." {
		t.Fatalf("draft instruction = %+v", draft.Instruction)
	}
	var scope struct {
		Kind string `json:"kind"`
	}
	if json.Unmarshal(draft.Scope, &scope) != nil || scope.Kind != "all" {
		t.Fatalf("draft scope = %s, want all scope", draft.Scope)
	}
	assertProjectDesignSystemPackageDigest(t, queries, fixture.System.ID, "saved", oldSavedDigest)
}

func TestCompleteProjectDesignSystemTaskRejectsMismatchedActiveTask(t *testing.T) {
	fixture := createProjectDesignSystemCompletionFixture(t, service.ProjectDesignSystemGenerate)
	replacementTaskID := createReplacementProjectDesignSystemTask(t, fixture)
	if _, err := db.New(testPool).UpdateProjectDesignSystemInputAndTask(context.Background(), db.UpdateProjectDesignSystemInputAndTaskParams{
		Platform:        "web",
		CurrentAgentID:  parseUUID(fixture.AgentID),
		ActiveTaskID:    parseUUID(replacementTaskID),
		ActiveOperation: pgtype.Text{String: string(service.ProjectDesignSystemRegenerate), Valid: true},
		InputSnapshot:   []byte(`{"brief":"replacement task"}`),
		ID:              fixture.System.ID,
		WorkspaceID:     parseUUID(testWorkspaceID),
	}); err != nil {
		t.Fatalf("replace active task: %v", err)
	}

	w := completeProjectDesignSystemTaskForTest(t, fixture.TaskID, map[string]any{
		"output":                          "Stale task attempted completion.",
		"project_design_system_artifacts": validProjectDesignSystemCompletionArtifacts(),
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CompleteTask status = %d, want 400: %s", w.Code, w.Body.String())
	}
	assertProjectDesignSystemTaskFailed(t, fixture.TaskID, "project_design_system_invalid_artifacts")

	system, err := db.New(testPool).GetProjectDesignSystemInWorkspace(context.Background(), db.GetProjectDesignSystemInWorkspaceParams{
		ID:          fixture.System.ID,
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("get mismatched design system: %v", err)
	}
	if uuidToString(system.ActiveTaskID) != replacementTaskID {
		t.Fatalf("active task = %q, want replacement %q", uuidToString(system.ActiveTaskID), replacementTaskID)
	}
	if _, err := db.New(testPool).GetProjectDesignSystemPackageBySlot(context.Background(), db.GetProjectDesignSystemPackageBySlotParams{
		DesignSystemID: fixture.System.ID,
		Slot:           "draft",
		WorkspaceID:    parseUUID(testWorkspaceID),
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("draft package error = %v, want pgx.ErrNoRows", err)
	}
}

func TestProjectDesignSystemFailureAndCancellationPreserveExistingPackage(t *testing.T) {
	tests := []struct {
		name string
		act  func(*testing.T, projectDesignSystemCompletionFixture)
		code string
	}{
		{
			name: "daemon failure",
			code: "provider_error",
			act: func(t *testing.T, fixture projectDesignSystemCompletionFixture) {
				w := httptest.NewRecorder()
				req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+fixture.TaskID+"/fail", map[string]any{
					"error":          "provider failed while generating the design system",
					"failure_reason": "provider_error",
				}, testWorkspaceID, "project-design-system-test")
				req = withURLParam(req, "taskId", fixture.TaskID)
				testHandler.FailTask(w, req)
				if w.Code != http.StatusOK {
					t.Fatalf("FailTask status = %d, want 200: %s", w.Code, w.Body.String())
				}
			},
		},
		{
			name: "single cancellation",
			code: "project_design_system_cancelled",
			act: func(t *testing.T, fixture projectDesignSystemCompletionFixture) {
				if _, err := testHandler.TaskService.CancelTask(context.Background(), parseUUID(fixture.TaskID)); err != nil {
					t.Fatalf("cancel design system task: %v", err)
				}
			},
		},
		{
			name: "agent bulk cancellation",
			code: "project_design_system_cancelled",
			act: func(t *testing.T, fixture projectDesignSystemCompletionFixture) {
				if _, err := testHandler.TaskService.CancelTasksForAgent(context.Background(), parseUUID(fixture.AgentID)); err != nil {
					t.Fatalf("bulk cancel design system task: %v", err)
				}
			},
		},
		{
			name: "sweeper failure transaction",
			code: "timeout",
			act: func(t *testing.T, fixture projectDesignSystemCompletionFixture) {
				rows, err := testHandler.TaskService.FailTasksWithProfileSync(context.Background(), func(queries *db.Queries) ([]db.AgentTaskQueue, error) {
					failed, failErr := queries.FailAgentTask(context.Background(), db.FailAgentTaskParams{
						ID:            parseUUID(fixture.TaskID),
						Error:         pgtype.Text{String: "design system task timed out", Valid: true},
						FailureReason: pgtype.Text{String: "timeout", Valid: true},
					})
					if failErr != nil {
						return nil, failErr
					}
					return []db.AgentTaskQueue{failed}, nil
				})
				if err != nil || len(rows) != 1 {
					t.Fatalf("sweeper failure rows/error = %d/%v, want 1/nil", len(rows), err)
				}
			},
		},
		{
			name: "post-transaction failure handling",
			code: "timeout",
			act: func(t *testing.T, fixture projectDesignSystemCompletionFixture) {
				failed, err := testHandler.Queries.FailAgentTask(context.Background(), db.FailAgentTaskParams{
					ID:            parseUUID(fixture.TaskID),
					Error:         pgtype.Text{String: "design system worker disappeared", Valid: true},
					FailureReason: pgtype.Text{String: "timeout", Valid: true},
				})
				if err != nil {
					t.Fatalf("fail task before shared handler: %v", err)
				}
				testHandler.TaskService.HandleFailedTasks(context.Background(), []db.AgentTaskQueue{failed})
			},
		},
		{
			name: "post-transaction cancellation broadcast",
			code: "project_design_system_cancelled",
			act: func(t *testing.T, fixture projectDesignSystemCompletionFixture) {
				cancelled, err := testHandler.Queries.CancelAgentTask(context.Background(), parseUUID(fixture.TaskID))
				if err != nil {
					t.Fatalf("cancel task before shared broadcast: %v", err)
				}
				testHandler.TaskService.BroadcastCancelledTasks(context.Background(), testWorkspaceID, []db.AgentTaskQueue{cancelled})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := createProjectDesignSystemCompletionFixture(t, service.ProjectDesignSystemGenerate)
			queries := db.New(testPool)
			draftDigest := strings.Repeat("c", 64)
			savedDigest := strings.Repeat("e", 64)
			upsertProjectDesignSystemPackageForTest(t, queries, fixture.System.ID, "draft", "draft-before-terminal-task", draftDigest)
			upsertProjectDesignSystemPackageForTest(t, queries, fixture.System.ID, "saved", "saved-before-terminal-task", savedDigest)

			tt.act(t, fixture)

			assertProjectDesignSystemFailureState(t, fixture.System.ID, fixture.TaskID, tt.code)
			assertProjectDesignSystemPackageDigest(t, queries, fixture.System.ID, "draft", draftDigest)
			assertProjectDesignSystemPackageDigest(t, queries, fixture.System.ID, "saved", savedDigest)
		})
	}
}

func TestCancelStandaloneProjectDesignSystemTaskClearsActiveState(t *testing.T) {
	fixture := createProjectDesignSystemCompletionFixture(t, service.ProjectDesignSystemGenerate)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE project_design_system
		SET project_id = NULL
		WHERE id = $1
	`, fixture.System.ID); err != nil {
		t.Fatalf("make design system standalone: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent_task_queue
		SET context = jsonb_set(context, '{project_id}', '""'::jsonb)
		WHERE id = $1
	`, fixture.TaskID); err != nil {
		t.Fatalf("clear standalone task project id: %v", err)
	}

	if _, err := testHandler.TaskService.CancelTaskByUser(context.Background(), parseUUID(fixture.TaskID)); err != nil {
		t.Fatalf("cancel standalone design system task: %v", err)
	}

	var status string
	if err := testPool.QueryRow(context.Background(), `
		SELECT status FROM agent_task_queue WHERE id = $1
	`, fixture.TaskID).Scan(&status); err != nil {
		t.Fatalf("load cancelled standalone task: %v", err)
	}
	if status != "cancelled" {
		t.Fatalf("task status = %q, want cancelled", status)
	}
	assertProjectDesignSystemFailureState(t, fixture.System.ID, fixture.TaskID, "project_design_system_cancelled")
}

func TestCompleteProjectDesignSystemBodyLimitPreservesOrdinaryCompletion(t *testing.T) {
	t.Run("ordinary task remains compatible", func(t *testing.T) {
		taskID := createOrdinaryCompletionTask(t)
		w := completeProjectDesignSystemTaskForTest(t, taskID, map[string]any{
			"output": "Ordinary task completed normally.",
		})
		if w.Code != http.StatusOK {
			t.Fatalf("ordinary CompleteTask status = %d, want 200: %s", w.Code, w.Body.String())
		}
		task, err := db.New(testPool).GetAgentTask(context.Background(), parseUUID(taskID))
		if err != nil {
			t.Fatalf("get ordinary completed task: %v", err)
		}
		if task.Status != "completed" {
			t.Fatalf("ordinary task status = %q, want completed", task.Status)
		}
	})

	t.Run("oversized specialized task stays running", func(t *testing.T) {
		fixture := createProjectDesignSystemCompletionFixture(t, service.ProjectDesignSystemGenerate)
		w := completeProjectDesignSystemTaskForTest(t, fixture.TaskID, map[string]any{
			"output":                          strings.Repeat("x", (2<<20)+1),
			"project_design_system_artifacts": validProjectDesignSystemCompletionArtifacts(),
		})
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("oversized CompleteTask status = %d, want 413: %s", w.Code, w.Body.String())
		}

		task, err := db.New(testPool).GetAgentTask(context.Background(), parseUUID(fixture.TaskID))
		if err != nil {
			t.Fatalf("get oversized task: %v", err)
		}
		if task.Status != "running" {
			t.Fatalf("oversized task status = %q, want running", task.Status)
		}
		system, err := db.New(testPool).GetProjectDesignSystemInWorkspace(context.Background(), db.GetProjectDesignSystemInWorkspaceParams{
			ID:          fixture.System.ID,
			WorkspaceID: parseUUID(testWorkspaceID),
		})
		if err != nil {
			t.Fatalf("get oversized design system: %v", err)
		}
		if uuidToString(system.ActiveTaskID) != fixture.TaskID {
			t.Fatalf("active task = %q, want %q", uuidToString(system.ActiveTaskID), fixture.TaskID)
		}
		if _, err := db.New(testPool).GetProjectDesignSystemPackageBySlot(context.Background(), db.GetProjectDesignSystemPackageBySlotParams{
			DesignSystemID: fixture.System.ID,
			Slot:           "draft",
			WorkspaceID:    parseUUID(testWorkspaceID),
		}); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("oversized draft error = %v, want pgx.ErrNoRows", err)
		}
	})
}

type projectDesignSystemCompletionFixture struct {
	System  db.ProjectDesignSystem
	TaskID  string
	AgentID string
}

func createProjectDesignSystemCompletionFixture(t *testing.T, operation service.ProjectDesignSystemOperation) projectDesignSystemCompletionFixture {
	t.Helper()
	ctx := context.Background()
	queries := db.New(testPool)
	projectID := createProjectDesignSystemProject(t, testWorkspaceID, "Completion")
	system := createProjectDesignSystemForTest(t, queries, parseUUID(testWorkspaceID), projectID, "Completion system")

	runtimeID := handlerTestRuntimeID(t)
	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Project design system completion %d", time.Now().UnixNano()), runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create completion agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	contextJSON, err := json.Marshal(service.ProjectDesignSystemTaskContext{
		Type:                  service.ProjectDesignSystemTaskContextType,
		Operation:             operation,
		RequesterID:           testUserID,
		WorkspaceID:           testWorkspaceID,
		ProjectID:             uuidToString(projectID),
		ProjectDesignSystemID: uuidToString(system.ID),
		AgentID:               agentID,
		Platform:              "web",
		Brief:                 "Create a calm operational design system.",
		Instruction:           "Keep controls compact.",
		Scope:                 json.RawMessage(`{"kind":"all"}`),
		OutputPolicy:          json.RawMessage(`{"scripts_allowed":false}`),
	})
	if err != nil {
		t.Fatalf("marshal completion context: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, context, started_at)
		VALUES ($1, $2, 'running', 0, $3, now())
		RETURNING id
	`, agentID, runtimeID, contextJSON).Scan(&taskID); err != nil {
		t.Fatalf("create completion task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	system, err = queries.UpdateProjectDesignSystemInputAndTask(ctx, db.UpdateProjectDesignSystemInputAndTaskParams{
		Platform:        "web",
		CurrentAgentID:  parseUUID(agentID),
		ActiveTaskID:    parseUUID(taskID),
		ActiveOperation: pgtype.Text{String: string(operation), Valid: true},
		InputSnapshot:   []byte(`{"brief":"completion test"}`),
		ID:              system.ID,
		WorkspaceID:     parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("activate completion task: %v", err)
	}
	return projectDesignSystemCompletionFixture{System: system, TaskID: taskID, AgentID: agentID}
}

func completeProjectDesignSystemTaskForTest(t *testing.T, taskID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/complete", body, testWorkspaceID, "project-design-system-test")
	req = withURLParam(req, "taskId", taskID)
	testHandler.CompleteTask(w, req)
	return w
}

func createReplacementProjectDesignSystemTask(t *testing.T, fixture projectDesignSystemCompletionFixture) string {
	t.Helper()
	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `SELECT runtime_id FROM agent WHERE id = $1`, fixture.AgentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load replacement runtime: %v", err)
	}
	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, context, started_at)
		VALUES ($1, $2, 'running', 0, '{}'::jsonb, now())
		RETURNING id
	`, fixture.AgentID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("create replacement task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID
}

func createOrdinaryCompletionTask(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, fmt.Sprintf("Ordinary completion runtime %d", time.Now().UnixNano()))
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, fmt.Sprintf("Ordinary completion %d", time.Now().UnixNano()))
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at)
		VALUES ($1, $2, $3, 'running', 0, now())
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("create ordinary completion task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID
}

func assertProjectDesignSystemTaskFailed(t *testing.T, taskID, failureReason string) {
	t.Helper()
	var status string
	var storedReason pgtype.Text
	if err := testPool.QueryRow(context.Background(), `
		SELECT status, failure_reason
		FROM agent_task_queue
		WHERE id = $1
	`, taskID).Scan(&status, &storedReason); err != nil {
		t.Fatalf("load failed project design system task: %v", err)
	}
	if status != "failed" {
		t.Fatalf("task status = %q, want failed", status)
	}
	if !storedReason.Valid || storedReason.String != failureReason {
		t.Fatalf("failure reason = %+v, want %q", storedReason, failureReason)
	}
}

func assertProjectDesignSystemFailureState(t *testing.T, systemID pgtype.UUID, taskID, code string) {
	t.Helper()
	system, err := db.New(testPool).GetProjectDesignSystemInWorkspace(context.Background(), db.GetProjectDesignSystemInWorkspaceParams{
		ID:          systemID,
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("get failed design system: %v", err)
	}
	if system.ActiveTaskID.Valid || system.ActiveOperation.Valid {
		t.Fatalf("failed design system still has active task: %+v", system)
	}
	var lastError struct {
		Code   string `json:"code"`
		TaskID string `json:"task_id"`
	}
	if len(system.LastError) == 0 || json.Unmarshal(system.LastError, &lastError) != nil {
		t.Fatalf("last_error = %s, want structured error", system.LastError)
	}
	if lastError.Code != code || lastError.TaskID != taskID {
		t.Fatalf("last_error = %+v, want code %q task %q", lastError, code, taskID)
	}
}

func validProjectDesignSystemCompletionArtifacts() projectdesignsystem.ArtifactInput {
	return projectdesignsystem.ArtifactInput{
		DesignMD: "# Atlas CRM Design System\n\nAtlas CRM is calm and direct.\n\n## Principles\n\nUse clear hierarchy and compact controls.\n",
		TokensCSS: `:root {
  --color-surface: #ffffff;
  --color-text: #17212b;
  --color-action: #2463eb;
  --radius-control: 6px;
}
.showcase { color: var(--color-text); background: var(--color-surface); }
.primary { color: var(--color-surface); background: var(--color-action); border-radius: var(--radius-control); }
`,
		ComponentsHTML: `<main class="showcase" data-design-node-id="overview" data-design-node-kind="block" data-design-node-label="Overview">
  <h1>Customer workspace</h1>
  <button type="button" class="primary" data-design-node-id="button-primary" data-design-node-kind="component" data-design-node-label="Primary button">Create customer</button>
</main>`,
	}
}
