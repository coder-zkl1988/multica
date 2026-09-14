package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/projectdesignsystem"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCreateProjectDesignSystemRequiresExplicitReadyAgent(t *testing.T) {
	projectID := createProjectForDesignTest(t, "Explicit agent project")

	missingAgent := performProjectDesignSystemRequest(t, testHandler.CreateProjectDesignSystem, http.MethodPost, "/api/project-design-systems", map[string]any{
		"project_id": projectID,
		"platform":   "web",
		"brief":      "A focused operational product.",
	})
	assertProjectDesignSystemErrorCode(t, missingAgent, http.StatusBadRequest, "agent_id_required")

	offlineAgentID, _ := createProjectDesignSystemAgent(t, "offline")
	offline := performProjectDesignSystemRequest(t, testHandler.CreateProjectDesignSystem, http.MethodPost, "/api/project-design-systems", map[string]any{
		"project_id": projectID,
		"agent_id":   offlineAgentID,
		"platform":   "web",
		"brief":      "A focused operational product.",
	})
	assertProjectDesignSystemErrorCode(t, offline, http.StatusConflict, "agent_unavailable")

	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM project_design_system WHERE project_id = $1`, projectID).Scan(&count); err != nil {
		t.Fatalf("count project design systems: %v", err)
	}
	if count != 0 {
		t.Fatalf("project design system count = %d, want 0 after rejected dispatches", count)
	}
}

func TestCreateProjectDesignSystemLegacyProgrammaticModeUsesSingleAgent(t *testing.T) {
	projectID := createProjectForDesignTest(t, "Repository Agent project")
	agentID, _ := createProjectDesignSystemAgent(t, "online")
	var repositoryID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project_resource (project_id, workspace_id, resource_type, resource_ref, position, label)
		VALUES ($1, $2, 'github_repo', '{"url":"https://github.com/example/product","ref":"release"}'::jsonb, 0, 'product')
		RETURNING id
	`, projectID, testWorkspaceID).Scan(&repositoryID); err != nil {
		t.Fatalf("create repository resource: %v", err)
	}

	response := performProjectDesignSystemRequest(t, testHandler.CreateProjectDesignSystem, http.MethodPost, "/api/project-design-systems", map[string]any{
		"project_id": projectID, "project_resource_id": repositoryID, "agent_id": agentID,
		"generation_mode": "programmatic_first", "platform": "web",
		"brief": "Extract the repository design system with the selected Agent.",
	})
	if response.Code != http.StatusAccepted {
		t.Fatalf("create status = %d, body = %s", response.Code, response.Body.String())
	}
	var got ProjectDesignSystemResponse
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ProjectResourceID != repositoryID || got.ActiveTask == nil || got.ActiveTask.ExecutionMode != "" {
		t.Fatalf("create response = %+v", got)
	}
	var input projectDesignSystemInputSnapshot
	if err := json.Unmarshal(got.InputSnapshot, &input); err != nil {
		t.Fatalf("decode input snapshot: %v", err)
	}
	if input.GenerationMode != "agent" {
		t.Fatalf("generation mode = %q", input.GenerationMode)
	}
	var taskContext service.ProjectDesignSystemTaskContext
	var rawContext []byte
	if err := testPool.QueryRow(context.Background(), `SELECT context FROM agent_task_queue WHERE id = $1`, got.ActiveTask.ID).Scan(&rawContext); err != nil {
		t.Fatalf("load task context: %v", err)
	}
	if err := json.Unmarshal(rawContext, &taskContext); err != nil {
		t.Fatalf("decode task context: %v", err)
	}
	if taskContext.ExecutionMode != "" || taskContext.ProjectResourceID != repositoryID || taskContext.PackageSchema != projectdesignsystem.PackageSchemaV2 {
		t.Fatalf("task context = %+v", taskContext)
	}

	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'running' WHERE id = $1`, got.ActiveTask.ID); err != nil {
		t.Fatalf("mark task running: %v", err)
	}
	testHandler.TaskService.ReconcileAgentStatus(ctx, parseUUID(agentID))
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent WHERE id = $1`, agentID).Scan(&status); err != nil {
		t.Fatalf("load agent status: %v", err)
	}
	if status != "working" {
		t.Fatalf("repository generation did not mark Agent working: status = %q", status)
	}
}

func TestDecodeProjectDesignSystemInputRemovesLegacyUIProfileAndMode(t *testing.T) {
	raw, err := json.Marshal(projectDesignSystemInputSnapshot{
		AgentID: "11111111-1111-1111-1111-111111111111", GenerationMode: service.ProjectDesignSystemExecutionModeProgrammaticFirst,
		Platform: "web", Brief: "Repository design system.",
		References: []projectDesignSystemReferenceSnapshot{
			{Kind: "design_system_profile", ProfileID: "profile-1", Profile: json.RawMessage(`{"token":"legacy"}`)},
			{Kind: "link", URL: "https://example.test/reference"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	input, normalized, err := decodeProjectDesignSystemInput(raw)
	if err != nil {
		t.Fatalf("decode input: %v", err)
	}
	if input.GenerationMode != "agent" || len(input.References) != 1 || input.References[0].Kind != "link" {
		t.Fatalf("normalized input = %+v", input)
	}
	if strings.Contains(string(normalized), "design_system_profile") || strings.Contains(string(normalized), "legacy") {
		t.Fatalf("normalized task input leaked legacy profile: %s", normalized)
	}
}

func TestCreateProjectDesignSystemLegacyModeNormalizesWithoutRepository(t *testing.T) {
	projectID := createProjectForDesignTest(t, "Legacy mode project")
	agentID, _ := createProjectDesignSystemAgent(t, "online")
	response := performProjectDesignSystemRequest(t, testHandler.CreateProjectDesignSystem, http.MethodPost, "/api/project-design-systems", map[string]any{
		"project_id": projectID, "agent_id": agentID, "generation_mode": "programmatic_first",
		"platform": "web", "brief": "Create with the selected Agent.",
	})
	if response.Code != http.StatusAccepted {
		t.Fatalf("create status = %d, body = %s", response.Code, response.Body.String())
	}
	var got ProjectDesignSystemResponse
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	var input projectDesignSystemInputSnapshot
	if err := json.Unmarshal(got.InputSnapshot, &input); err != nil {
		t.Fatal(err)
	}
	if input.GenerationMode != "agent" || got.ActiveTask == nil || got.ActiveTask.ExecutionMode != "" {
		t.Fatalf("legacy mode was not normalized: input=%+v task=%+v", input, got.ActiveTask)
	}
}

func TestCreateProjectDesignSystemRequiresPlatformAndBrief(t *testing.T) {
	projectID := createProjectForDesignTest(t, "Required input project")
	agentID, _ := createProjectDesignSystemAgent(t, "online")

	tests := []struct {
		name string
		body map[string]any
		code string
	}{
		{
			name: "platform",
			body: map[string]any{"project_id": projectID, "agent_id": agentID, "brief": "Operational product."},
			code: "platform_required",
		},
		{
			name: "brief",
			body: map[string]any{"project_id": projectID, "agent_id": agentID, "platform": "web", "brief": "  "},
			code: "brief_required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := performProjectDesignSystemRequest(t, testHandler.CreateProjectDesignSystem, http.MethodPost, "/api/project-design-systems", tt.body)
			assertProjectDesignSystemErrorCode(t, response, http.StatusBadRequest, tt.code)
		})
	}
}

func TestCreateProjectDesignSystemAlwaysEnqueuesNativeV2WhenOpenDesignFlagIsTrue(t *testing.T) {
	previousOpenDesign := testHandler.cfg.OpenDesignEnabled
	testHandler.cfg.OpenDesignEnabled = true
	t.Cleanup(func() { testHandler.cfg.OpenDesignEnabled = previousOpenDesign })
	projectID := createProjectForDesignTest(t, "Snapshot project")
	if _, err := testPool.Exec(context.Background(), `UPDATE project SET description = $2 WHERE id = $1`, projectID, "Current CRM for service teams"); err != nil {
		t.Fatalf("update project description: %v", err)
	}
	agentID, _ := createProjectDesignSystemAgent(t, "online")
	attachmentID, designFileID, _ := createProjectDesignSystemReferencesForTest(t, projectID)

	response := performProjectDesignSystemRequest(t, testHandler.CreateProjectDesignSystem, http.MethodPost, "/api/project-design-systems", map[string]any{
		"project_id": projectID,
		"agent_id":   agentID,
		"platform":   "web",
		"brief":      "  Calm CRM for repeated customer operations.  ",
		"references": []map[string]any{
			{"kind": "brand_color", "value": "#abc", "label": "Primary"},
			{"kind": "link", "value": "https://example.com/brand", "label": "Brand guide"},
			{"kind": "attachment", "attachment_id": attachmentID, "label": "Logo"},
			{"kind": "design_file", "design_file_id": designFileID, "label": "Current dashboard"},
		},
	})
	if response.Code != http.StatusAccepted {
		t.Fatalf("CreateProjectDesignSystem: status = %d, body = %s", response.Code, response.Body.String())
	}

	var created ProjectDesignSystemResponse
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.ID == "" || created.ActiveTask == nil || created.ActiveTask.ID == "" {
		t.Fatalf("create response missing system/task identity: %+v", created)
	}

	var inputJSON, taskContextJSON []byte
	if err := testPool.QueryRow(context.Background(), `
		SELECT pds.input_snapshot, task.context
		FROM project_design_system pds, agent_task_queue task
		WHERE pds.id = $1 AND task.id = pds.active_task_id
	`, created.ID).Scan(&inputJSON, &taskContextJSON); err != nil {
		t.Fatalf("load frozen input/task context: %v", err)
	}

	var input map[string]any
	if err := json.Unmarshal(inputJSON, &input); err != nil {
		t.Fatalf("decode input snapshot: %v", err)
	}
	if input["agent_id"] != agentID || input["platform"] != "web" || input["brief"] != "Calm CRM for repeated customer operations." {
		t.Fatalf("input snapshot lost exact selected values: %#v", input)
	}
	references, ok := input["references"].([]any)
	if !ok || len(references) != 4 {
		t.Fatalf("input references = %#v, want 4 frozen references", input["references"])
	}
	color := references[0].(map[string]any)
	if color["kind"] != "brand_color" || color["value"] != "#AABBCC" || color["label"] != "Primary" {
		t.Fatalf("brand color snapshot = %#v", color)
	}
	link := references[1].(map[string]any)
	if link["kind"] != "link" || link["url"] != "https://example.com/brand" || link["label"] != "Brand guide" {
		t.Fatalf("link snapshot = %#v", link)
	}
	attachment := references[2].(map[string]any)
	if attachment["attachment_id"] != attachmentID || attachment["filename"] != "atlas-logo.png" || attachment["content_type"] != "image/png" || attachment["url"] != "https://static.soyoung.com/atlas-logo.png" {
		t.Fatalf("attachment snapshot = %#v", attachment)
	}
	designFile := references[3].(map[string]any)
	if designFile["design_file_id"] != designFileID || designFile["title"] != "Atlas dashboard" || designFile["thumbnail_url"] != "https://static.soyoung.com/atlas-dashboard.png" {
		t.Fatalf("design file snapshot = %#v", designFile)
	}
	frames := designFile["frames"].([]any)
	if len(frames) != 1 || frames[0].(map[string]any)["name"] != "Dashboard" || frames[0].(map[string]any)["preview_url"] != "https://static.soyoung.com/atlas-dashboard.png" {
		t.Fatalf("design file frame snapshot = %#v", frames)
	}

	var taskContext map[string]any
	if err := json.Unmarshal(taskContextJSON, &taskContext); err != nil {
		t.Fatalf("decode task context: %v", err)
	}
	if taskContext["type"] != "project_design_system_task" || taskContext["operation"] != "generate" {
		t.Fatalf("task discriminator = %#v", taskContext)
	}
	if taskContext["package_schema"] != projectdesignsystem.PackageSchemaV2 || taskContext["open_design_run"] != nil {
		t.Fatalf("new task did not use the native V2 contract: %#v", taskContext)
	}
	if taskContext["agent_id"] != agentID || taskContext["project_id"] != projectID || taskContext["project_design_system_id"] != created.ID {
		t.Fatalf("task identity snapshot = %#v", taskContext)
	}
	project := taskContext["project"].(map[string]any)
	if project["name"] != "Snapshot project" || project["description"] != "Current CRM for service teams" {
		t.Fatalf("task project snapshot = %#v", project)
	}
}

func TestCreateProjectDesignSystemRejectsUIProfileReference(t *testing.T) {
	projectID := createProjectForDesignTest(t, "Repository-only evidence project")
	agentID, _ := createProjectDesignSystemAgent(t, "online")
	_, _, profileID := createProjectDesignSystemReferencesForTest(t, projectID)
	response := performProjectDesignSystemRequest(t, testHandler.CreateProjectDesignSystem, http.MethodPost, "/api/project-design-systems", map[string]any{
		"project_id": projectID, "agent_id": agentID, "platform": "web", "brief": "Use repository evidence.",
		"references": []map[string]any{{"kind": "design_system_profile", "design_system_profile_id": profileID}},
	})
	assertProjectDesignSystemErrorCode(t, response, http.StatusBadRequest, "reference_kind_invalid")
}

// A standalone system (empty project_id) belongs to the workspace itself:
// it needs a name, any number may coexist, and the task context it enqueues
// carries no project — the binding digest is computed over the same empty
// project_id everywhere, so integrity holds.
func TestCreateProjectDesignSystemStandalone(t *testing.T) {
	agentID, _ := createProjectDesignSystemAgent(t, "online")

	var existing int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM project_design_system WHERE project_id IS NULL`).Scan(&existing); err != nil {
		t.Fatalf("count standalone systems: %v", err)
	}

	noName := performProjectDesignSystemRequest(t, testHandler.CreateProjectDesignSystem, http.MethodPost, "/api/project-design-systems", map[string]any{
		"agent_id": agentID, "platform": "web", "brief": "A brand kit for the studio.",
	})
	assertProjectDesignSystemErrorCode(t, noName, http.StatusBadRequest, "name_required")

	// A project row must not be required, and a second standalone system must
	// not conflict with the first.
	standaloneIDs := map[string]string{}
	for i, name := range []string{"品牌 A", "品牌 B"} {
		response := performProjectDesignSystemRequest(t, testHandler.CreateProjectDesignSystem, http.MethodPost, "/api/project-design-systems", map[string]any{
			"name": name, "agent_id": agentID, "platform": "web",
			"brief":      "A brand kit for the studio.",
			"references": []map[string]any{{"kind": "link", "value": "https://example.com", "label": "官网"}},
		})
		if response.Code != http.StatusAccepted {
			t.Fatalf("standalone #%d status = %d body = %s", i, response.Code, response.Body.String())
		}
		var created struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			ProjectID string `json:"project_id"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
			t.Fatalf("decode standalone response: %v", err)
		}
		if created.ProjectID != "" || created.Name != name {
			t.Fatalf("standalone response = %+v, want no project and name %q", created, name)
		}
		standaloneIDs[name] = created.ID
	}

	// Earlier runs of this suite leave their rows in the shared test
	// database, so the assertion is on what this run added.
	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM project_design_system WHERE project_id IS NULL`).Scan(&count); err != nil {
		t.Fatalf("count standalone systems: %v", err)
	}
	if count != existing+2 {
		t.Fatalf("standalone systems = %d, want %d (this run adds two)", count, existing+2)
	}

	// Scoped to the system this run created. Ordering over every standalone
	// system and taking the first picks the oldest row in the database, which
	// on a shared dev database is somebody's real design system, not this
	// test's — the same reason the count above is relative to `existing`.
	var contextRaw []byte
	if err := testPool.QueryRow(context.Background(), `
		SELECT q.context FROM agent_task_queue q
		JOIN project_design_system s ON s.active_task_id = q.id
		WHERE s.id = $1
	`, standaloneIDs["品牌 A"]).Scan(&contextRaw); err != nil {
		t.Fatalf("load standalone task context: %v", err)
	}
	var taskContext struct {
		ProjectID string          `json:"project_id"`
		Project   json.RawMessage `json:"project"`
	}
	if err := json.Unmarshal(contextRaw, &taskContext); err != nil {
		t.Fatalf("decode task context: %v", err)
	}
	if taskContext.ProjectID != "" {
		t.Fatalf("task context project_id = %q, want empty", taskContext.ProjectID)
	}
	var project map[string]any
	if err := json.Unmarshal(taskContext.Project, &project); err != nil {
		t.Fatalf("decode embedded project: %v", err)
	}
	if project["name"] != "品牌 A" {
		t.Fatalf("embedded project name = %v, want the system name", project["name"])
	}
}

func TestCreateProjectDesignSystemForSettingsRepository(t *testing.T) {
	ctx := context.Background()
	repositoryID := uuid.NewString()
	var previous []byte
	if err := testPool.QueryRow(ctx, `SELECT repos FROM workspace WHERE id=$1`, testWorkspaceID).Scan(&previous); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE workspace SET repos=$1::jsonb WHERE id=$2`, `[{"id":"`+repositoryID+`","url":"https://github.com/example/settings-repo.git","description":"Settings repo","default_branch_hint":"main"}]`, testWorkspaceID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `UPDATE workspace SET repos=$1 WHERE id=$2`, previous, testWorkspaceID)
	})
	agentID, _ := createProjectDesignSystemAgent(t, "online")
	response := performProjectDesignSystemRequest(t, testHandler.CreateProjectDesignSystem, http.MethodPost, "/api/project-design-systems", map[string]any{
		"workspace_repository_id": repositoryID,
		"name":                    "Settings repo design system", "agent_id": agentID,
		"generation_mode": "programmatic_first", "platform": "web",
		"brief":      "Extract the repository design language.",
		"references": []map[string]any{{"kind": "link", "value": "https://github.com/example/settings-repo.git"}},
	})
	if response.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var created ProjectDesignSystemResponse
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ProjectID != "" || created.ProjectResourceID != "" || created.WorkspaceRepositoryID != repositoryID {
		t.Fatalf("settings repository scope = %+v", created)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id=(SELECT active_task_id FROM project_design_system WHERE id=$1)`, created.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project_design_system_package WHERE design_system_id=$1`, created.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project_design_system WHERE id=$1`, created.ID)
	})
	var taskContext service.ProjectDesignSystemTaskContext
	var raw []byte
	if err := testPool.QueryRow(ctx, `SELECT q.context FROM agent_task_queue q JOIN project_design_system s ON s.active_task_id=q.id WHERE s.id=$1`, created.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &taskContext); err != nil {
		t.Fatal(err)
	}
	if taskContext.WorkspaceRepositoryID != repositoryID || taskContext.WorkspaceRepositoryURL != "https://github.com/example/settings-repo.git" || taskContext.ExecutionMode != "" {
		t.Fatalf("task context = %+v", taskContext)
	}
	var input projectDesignSystemInputSnapshot
	if err := json.Unmarshal(created.InputSnapshot, &input); err != nil || input.GenerationMode != "agent" {
		t.Fatalf("input snapshot = %+v err=%v", input, err)
	}
}

func TestCreateProjectDesignSystemRejectsSecondSystem(t *testing.T) {
	projectID := createProjectForDesignTest(t, "Single system project")
	agentID, _ := createProjectDesignSystemAgent(t, "online")
	body := map[string]any{
		"project_id": projectID,
		"agent_id":   agentID,
		"platform":   "mobile",
		"brief":      "A concise field-service app.",
	}

	first := performProjectDesignSystemRequest(t, testHandler.CreateProjectDesignSystem, http.MethodPost, "/api/project-design-systems", body)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first create status = %d, body = %s", first.Code, first.Body.String())
	}
	second := performProjectDesignSystemRequest(t, testHandler.CreateProjectDesignSystem, http.MethodPost, "/api/project-design-systems", body)
	assertProjectDesignSystemErrorCode(t, second, http.StatusConflict, "project_design_system_exists")

	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM project_design_system WHERE project_id = $1`, projectID).Scan(&count); err != nil {
		t.Fatalf("count project design systems: %v", err)
	}
	if count != 1 {
		t.Fatalf("project design system count = %d, want 1", count)
	}
}

func TestCreateProjectDesignSystemRetriesFailedUnestablishedSystem(t *testing.T) {
	projectID := createProjectForDesignTest(t, "Retry failed generation project")
	agentID, _ := createProjectDesignSystemAgent(t, "online")
	system := createProjectDesignSystemIdentityForTest(t, projectID, agentID, projectDesignSystemInputSnapshot{
		AgentID:    agentID,
		Platform:   "web",
		Brief:      "First attempt",
		References: []projectDesignSystemReferenceSnapshot{},
	})
	if _, err := testPool.Exec(context.Background(), `
		UPDATE project_design_system
		SET last_error = '{"code":"agent_failed","message":"generation failed"}'::jsonb
		WHERE id = $1
	`, uuidToString(system.ID)); err != nil {
		t.Fatalf("record failed generation: %v", err)
	}

	response := performProjectDesignSystemRequest(t, testHandler.CreateProjectDesignSystem, http.MethodPost, "/api/project-design-systems", map[string]any{
		"project_id": projectID,
		"agent_id":   agentID,
		"platform":   "mobile",
		"brief":      "Retry with the preserved project identity.",
	})
	if response.Code != http.StatusAccepted {
		t.Fatalf("retry create status = %d, body = %s", response.Code, response.Body.String())
	}
	var got ProjectDesignSystemResponse
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("decode retry response: %v", err)
	}
	if got.ID != uuidToString(system.ID) {
		t.Fatalf("retry system id = %q, want %q", got.ID, uuidToString(system.ID))
	}
	if got.ActiveTask == nil || got.ActiveTask.Operation != "generate" || got.ActiveTask.Status != "queued" {
		t.Fatalf("retry active task = %+v", got.ActiveTask)
	}

	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM project_design_system WHERE project_id = $1`, projectID).Scan(&count); err != nil {
		t.Fatalf("count project design systems after retry: %v", err)
	}
	if count != 1 {
		t.Fatalf("project design system count after retry = %d, want 1", count)
	}
	var taskContext []byte
	if err := testPool.QueryRow(context.Background(), `SELECT context FROM agent_task_queue WHERE id = $1`, got.ActiveTask.ID).Scan(&taskContext); err != nil {
		t.Fatalf("load retry task context: %v", err)
	}
	var contextSnapshot map[string]any
	if err := json.Unmarshal(taskContext, &contextSnapshot); err != nil {
		t.Fatalf("decode retry task context: %v", err)
	}
	if contextSnapshot["operation"] != "generate" || contextSnapshot["platform"] != "mobile" || contextSnapshot["brief"] != "Retry with the preserved project identity." {
		t.Fatalf("retry task context = %#v", contextSnapshot)
	}
}

func TestCreateProjectDesignSystemRejectsUnsafeOrForeignReferences(t *testing.T) {
	projectID := createProjectForDesignTest(t, "Reference boundary project")
	foreignProjectID := createProjectForDesignTest(t, "Foreign reference project")
	_, foreignDesignFileID, _ := createProjectDesignSystemReferencesForTest(t, foreignProjectID)
	agentID, _ := createProjectDesignSystemAgent(t, "online")
	base := map[string]any{
		"project_id": projectID,
		"agent_id":   agentID,
		"platform":   "web",
		"brief":      "Reference validation.",
	}

	tests := []struct {
		name       string
		references any
		status     int
		code       string
	}{
		{name: "unknown kind", references: []map[string]any{{"kind": "moodboard"}}, status: http.StatusBadRequest, code: "reference_kind_invalid"},
		{name: "non HTTPS link", references: []map[string]any{{"kind": "link", "value": "http://example.com"}}, status: http.StatusBadRequest, code: "reference_invalid"},
		{name: "foreign design file", references: []map[string]any{{"kind": "design_file", "design_file_id": foreignDesignFileID}}, status: http.StatusNotFound, code: "reference_not_found"},
		{name: "oversized snapshot", references: []map[string]any{{"kind": "brand_color", "value": "#2463eb", "label": strings.Repeat("x", maxProjectDesignSystemSnapshotBytes)}}, status: http.StatusRequestEntityTooLarge, code: "input_snapshot_too_large"},
	}
	many := make([]map[string]any, maxProjectDesignSystemReferences+1)
	for index := range many {
		many[index] = map[string]any{"kind": "brand_color", "value": "#2463eb"}
	}
	tests = append(tests, struct {
		name       string
		references any
		status     int
		code       string
	}{name: "too many references", references: many, status: http.StatusBadRequest, code: "too_many_references"})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := make(map[string]any, len(base)+1)
			for key, value := range base {
				body[key] = value
			}
			body["references"] = tt.references
			response := performProjectDesignSystemRequest(t, testHandler.CreateProjectDesignSystem, http.MethodPost, "/api/project-design-systems", body)
			assertProjectDesignSystemErrorCode(t, response, tt.status, tt.code)
		})
	}
}

func TestGetProjectDesignSystemReturnsUnestablishedAfterFailedFirstRun(t *testing.T) {
	projectID := createProjectForDesignTest(t, "Failed first run project")
	agentID, _ := createProjectDesignSystemAgent(t, "online")
	input := projectDesignSystemInputSnapshot{
		AgentID:    agentID,
		Platform:   "web",
		Brief:      "Keep this input after failure.",
		References: []projectDesignSystemReferenceSnapshot{},
	}
	system := createProjectDesignSystemIdentityForTest(t, projectID, agentID, input)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE project_design_system
		SET active_task_id = NULL,
		    active_operation = NULL,
		    last_error = '{"code":"agent_failed","message":"generation failed"}'::jsonb
		WHERE id = $1
	`, uuidToString(system.ID)); err != nil {
		t.Fatalf("record failed first run: %v", err)
	}

	response := performProjectDesignSystemRequest(t, testHandler.GetProjectDesignSystemByProject, http.MethodGet, "/api/project-design-systems?project_id="+projectID, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GetProjectDesignSystemByProject: status = %d, body = %s", response.Code, response.Body.String())
	}
	var got ProjectDesignSystemResponse
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Status != "unestablished" || got.ActiveTask != nil || got.HasUnsavedChanges {
		t.Fatalf("failed first run response = %+v", got)
	}
	var snapshot map[string]any
	if err := json.Unmarshal(got.InputSnapshot, &snapshot); err != nil || snapshot["brief"] != input.Brief {
		t.Fatalf("input snapshot after failure = %#v, err = %v", snapshot, err)
	}
	var lastError map[string]any
	if err := json.Unmarshal(got.LastError, &lastError); err != nil || lastError["code"] != "agent_failed" {
		t.Fatalf("last error = %#v, err = %v", lastError, err)
	}
}

func insertRepositoryForProjectDesignSystemTest(t *testing.T, projectID string) string {
	t.Helper()
	resourceRef, err := json.Marshal(map[string]string{"url": "https://github.com/acme/crm-admin.git"})
	if err != nil {
		t.Fatalf("marshal repository ref: %v", err)
	}
	var resourceID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project_resource (project_id, workspace_id, resource_type, resource_ref, label, position, created_by)
		VALUES ($1, $2, 'github_repo', $3::jsonb, 'crm-admin', 0, $4)
		RETURNING id
	`, projectID, testWorkspaceID, resourceRef, testUserID).Scan(&resourceID); err != nil {
		t.Fatalf("insert project_resource: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project_design_system WHERE project_resource_id = $1`, resourceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project_resource WHERE id = $1`, resourceID)
	})
	return resourceID
}

func TestGetProjectDesignSystemDoesNotReturnProjectSystemForRepository(t *testing.T) {
	projectID := createProjectForDesignTest(t, "Exact repository lookup project")
	resourceID := insertRepositoryForProjectDesignSystemTest(t, projectID)
	agentID, _ := createProjectDesignSystemAgent(t, "online")
	input := projectDesignSystemInputSnapshot{
		AgentID:    agentID,
		Platform:   "web",
		Brief:      "The shared project system.",
		References: []projectDesignSystemReferenceSnapshot{},
	}
	projectSystem := createProjectDesignSystemIdentityForTest(t, projectID, agentID, input)
	pkg := validProjectDesignSystemPackageForTest(t)
	upsertValidatedProjectDesignSystemPackageForTest(t, projectSystem.ID, "saved", pkg)

	response := performProjectDesignSystemRequest(
		t,
		testHandler.GetProjectDesignSystemByProject,
		http.MethodGet,
		"/api/project-design-systems?project_id="+projectID+"&project_resource_id="+resourceID,
		nil,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("GetProjectDesignSystemByProject: status = %d, body = %s", response.Code, response.Body.String())
	}
	var got ProjectDesignSystemResponse
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("decode repository response: %v", err)
	}
	if got.ID != "" || got.ProjectResourceID != resourceID || got.Status != "unestablished" || got.ActiveTask != nil {
		t.Fatalf("repository response = %+v, want explicit unestablished state", got)
	}
}

func TestAdjustHistoricalV1PackageUsesLegacyReadOnlyBase(t *testing.T) {
	projectID := createProjectForDesignTest(t, "Scoped adjustment project")
	agentID, _ := createProjectDesignSystemAgent(t, "online")
	input := projectDesignSystemInputSnapshot{
		AgentID:    agentID,
		Platform:   "web",
		Brief:      "A calm CRM.",
		References: []projectDesignSystemReferenceSnapshot{},
	}
	system := createProjectDesignSystemIdentityForTest(t, projectID, agentID, input)
	pkg := validProjectDesignSystemPackageForTest(t)
	upsertValidatedProjectDesignSystemPackageForTest(t, system.ID, "draft", pkg)

	invalid := performProjectDesignSystemIDRequest(t, testHandler.AdjustProjectDesignSystem, http.MethodPost, "/api/project-design-systems/"+uuidToString(system.ID)+"/adjust", uuidToString(system.ID), map[string]any{
		"agent_id":    agentID,
		"instruction": "Make this section denser.",
		"scope":       map[string]any{"kind": "section", "id": "missing-section"},
	})
	assertProjectDesignSystemErrorCode(t, invalid, http.StatusBadRequest, "scope_not_found")

	valid := performProjectDesignSystemIDRequest(t, testHandler.AdjustProjectDesignSystem, http.MethodPost, "/api/project-design-systems/"+uuidToString(system.ID)+"/adjust", uuidToString(system.ID), map[string]any{
		"agent_id":    agentID,
		"instruction": "Make the primary button more decisive.",
		"scope":       map[string]any{"kind": "component", "id": "button-primary"},
	})
	if valid.Code != http.StatusAccepted {
		t.Fatalf("AdjustProjectDesignSystem: status = %d, body = %s", valid.Code, valid.Body.String())
	}
	var response ProjectDesignSystemResponse
	if err := json.NewDecoder(valid.Body).Decode(&response); err != nil {
		t.Fatalf("decode adjustment response: %v", err)
	}
	if response.ActiveTask == nil || response.Status != "generating" {
		t.Fatalf("adjustment response = %+v", response)
	}
	assertProjectDesignSystemResponseDigest(t, response.Content, pkg.Manifest.Digest)

	var contextJSON []byte
	if err := testPool.QueryRow(context.Background(), `SELECT context FROM agent_task_queue WHERE id = $1`, response.ActiveTask.ID).Scan(&contextJSON); err != nil {
		t.Fatalf("load adjustment context: %v", err)
	}
	var taskContext map[string]any
	if err := json.Unmarshal(contextJSON, &taskContext); err != nil {
		t.Fatalf("decode adjustment context: %v", err)
	}
	if taskContext["operation"] != "adjust" || taskContext["instruction"] != "Make the primary button more decisive." {
		t.Fatalf("adjustment task context = %#v", taskContext)
	}
	scope := taskContext["scope"].(map[string]any)
	if scope["kind"] != "component" || scope["id"] != "button-primary" {
		t.Fatalf("adjustment scope = %#v", scope)
	}
	base := taskContext["base_package"].(map[string]any)
	if !strings.Contains(base["design_md"].(string), "Atlas CRM") {
		t.Fatalf("base package was not frozen into task context: %#v", base)
	}
}

func TestRegenerateProjectDesignSystemBindsCurrentBaseDigest(t *testing.T) {
	projectID := createProjectForDesignTest(t, "Regeneration project")
	agentID, _ := createProjectDesignSystemAgent(t, "online")
	input := projectDesignSystemInputSnapshot{
		AgentID:  agentID,
		Platform: "web",
		Brief:    "Original direction.",
		References: []projectDesignSystemReferenceSnapshot{
			{Kind: "brand_color", Label: "Primary", Value: "#2463EB"},
		},
	}
	system := createProjectDesignSystemIdentityForTest(t, projectID, agentID, input)
	pkg := validProjectDesignSystemPackageForTest(t)
	upsertValidatedProjectDesignSystemPackageForTest(t, system.ID, "saved", pkg)

	response := performProjectDesignSystemIDRequest(t, testHandler.RegenerateProjectDesignSystem, http.MethodPost, "/api/project-design-systems/"+uuidToString(system.ID)+"/regenerate", uuidToString(system.ID), map[string]any{
		"agent_id": agentID,
		"platform": "mobile",
		"brief":    "A touch-first field operations system.",
	})
	if response.Code != http.StatusAccepted {
		t.Fatalf("RegenerateProjectDesignSystem: status = %d, body = %s", response.Code, response.Body.String())
	}

	queries := db.New(testPool)
	saved, err := queries.GetProjectDesignSystemPackageBySlot(context.Background(), db.GetProjectDesignSystemPackageBySlotParams{
		DesignSystemID: system.ID,
		Slot:           "saved",
		WorkspaceID:    parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load saved package after regenerate: %v", err)
	}
	if saved.IntegritySha256 != pkg.Manifest.Digest || saved.DesignMd != pkg.Artifacts.DesignMD {
		t.Fatalf("regenerate changed saved package: %+v", saved)
	}

	var got ProjectDesignSystemResponse
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("decode regenerate response: %v", err)
	}
	var contextJSON []byte
	if err := testPool.QueryRow(context.Background(), `SELECT context FROM agent_task_queue WHERE id = $1`, got.ActiveTask.ID).Scan(&contextJSON); err != nil {
		t.Fatalf("load regenerate context: %v", err)
	}
	var taskContext map[string]any
	if err := json.Unmarshal(contextJSON, &taskContext); err != nil {
		t.Fatalf("decode regenerate context: %v", err)
	}
	if taskContext["operation"] != "regenerate" || taskContext["platform"] != "mobile" || taskContext["brief"] != "A touch-first field operations system." {
		t.Fatalf("regenerate task context = %#v", taskContext)
	}
	if taskContext["base_package_sha256"] != "sha256:"+pkg.Manifest.Digest {
		t.Fatalf("regenerate base digest = %#v, want %q", taskContext["base_package_sha256"], "sha256:"+pkg.Manifest.Digest)
	}
	references := taskContext["references"].([]any)
	if len(references) != 1 || references[0].(map[string]any)["value"] != "#2463EB" {
		t.Fatalf("regenerate did not preserve omitted references: %#v", references)
	}
}

func TestSaveProjectDesignSystemRequiresValidatedDraft(t *testing.T) {
	projectID := createProjectForDesignTest(t, "Save validation project")
	agentID, _ := createProjectDesignSystemAgent(t, "online")
	input := projectDesignSystemInputSnapshot{AgentID: agentID, Platform: "web", Brief: "Save only valid work.", References: []projectDesignSystemReferenceSnapshot{}}
	system := createProjectDesignSystemIdentityForTest(t, projectID, agentID, input)
	systemID := uuidToString(system.ID)

	missing := performProjectDesignSystemIDRequest(t, testHandler.SaveProjectDesignSystem, http.MethodPost, "/api/project-design-systems/"+systemID+"/save", systemID, nil)
	assertProjectDesignSystemErrorCode(t, missing, http.StatusConflict, "draft_required")

	queries := db.New(testPool)
	if _, err := queries.UpsertProjectDesignSystemPackage(context.Background(), db.UpsertProjectDesignSystemPackageParams{
		DesignSystemID:  system.ID,
		Slot:            "draft",
		DesignMd:        "invalid",
		TokensCss:       "invalid",
		ComponentsHtml:  "invalid",
		Manifest:        []byte(`{}`),
		Validation:      []byte(`{"passed":false}`),
		IntegritySha256: strings.Repeat("f", 64),
		WorkspaceID:     parseUUID(testWorkspaceID),
	}); err != nil {
		t.Fatalf("insert invalid draft: %v", err)
	}
	invalid := performProjectDesignSystemIDRequest(t, testHandler.SaveProjectDesignSystem, http.MethodPost, "/api/project-design-systems/"+systemID+"/save", systemID, nil)
	assertProjectDesignSystemErrorCode(t, invalid, http.StatusUnprocessableEntity, "draft_invalid")

	pkg := validProjectDesignSystemPackageForTest(t)
	upsertValidatedProjectDesignSystemPackageForTest(t, system.ID, "draft", pkg)
	savedResponse := performProjectDesignSystemIDRequest(t, testHandler.SaveProjectDesignSystem, http.MethodPost, "/api/project-design-systems/"+systemID+"/save", systemID, nil)
	if savedResponse.Code != http.StatusOK {
		t.Fatalf("SaveProjectDesignSystem: status = %d, body = %s", savedResponse.Code, savedResponse.Body.String())
	}
	var response ProjectDesignSystemResponse
	if err := json.NewDecoder(savedResponse.Body).Decode(&response); err != nil {
		t.Fatalf("decode saved response: %v", err)
	}
	if response.Status != "saved" || response.HasUnsavedChanges {
		t.Fatalf("saved response = %+v", response)
	}
	assertProjectDesignSystemResponseDigest(t, response.Content, pkg.Manifest.Digest)
	if _, err := queries.GetProjectDesignSystemPackageBySlot(context.Background(), db.GetProjectDesignSystemPackageBySlotParams{DesignSystemID: system.ID, Slot: "draft", WorkspaceID: parseUUID(testWorkspaceID)}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("draft lookup error = %v, want pgx.ErrNoRows", err)
	}
	saved, err := queries.GetProjectDesignSystemPackageBySlot(context.Background(), db.GetProjectDesignSystemPackageBySlotParams{DesignSystemID: system.ID, Slot: "saved", WorkspaceID: parseUUID(testWorkspaceID)})
	if err != nil || saved.IntegritySha256 != pkg.Manifest.Digest {
		t.Fatalf("saved package = %+v, err = %v", saved, err)
	}
}

func assertProjectDesignSystemResponseDigest(t *testing.T, content ProjectDesignSystemContentResponse, want string) {
	t.Helper()
	raw, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("marshal project design system content: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode project design system content: %v", err)
	}
	if decoded["integrity_sha256"] != want {
		t.Fatalf("content integrity_sha256 = %v, want %s", decoded["integrity_sha256"], want)
	}
}

func TestProjectDesignSystemRoutesRejectForeignWorkspace(t *testing.T) {
	foreignWorkspaceID := createProjectDesignSystemWorkspace(t)
	foreignProjectID := createProjectDesignSystemProject(t, uuidToString(foreignWorkspaceID), "Foreign design system project")
	system := createProjectDesignSystemForTest(t, db.New(testPool), foreignWorkspaceID, foreignProjectID, "Foreign design system")
	systemID := uuidToString(system.ID)

	lookup := performProjectDesignSystemRequest(t, testHandler.GetProjectDesignSystemByProject, http.MethodGet, "/api/project-design-systems?project_id="+uuidToString(foreignProjectID), nil)
	assertProjectDesignSystemErrorCode(t, lookup, http.StatusNotFound, "project_not_found")

	tests := []struct {
		name    string
		handler http.HandlerFunc
		path    string
		body    any
	}{
		{name: "detail", handler: testHandler.GetProjectDesignSystem, path: "/api/project-design-systems/" + systemID},
		{name: "adjust", handler: testHandler.AdjustProjectDesignSystem, path: "/api/project-design-systems/" + systemID + "/adjust", body: map[string]any{"agent_id": testUserID, "instruction": "change", "scope": map[string]any{"kind": "all"}}},
		{name: "regenerate", handler: testHandler.RegenerateProjectDesignSystem, path: "/api/project-design-systems/" + systemID + "/regenerate", body: map[string]any{"agent_id": testUserID}},
		{name: "save", handler: testHandler.SaveProjectDesignSystem, path: "/api/project-design-systems/" + systemID + "/save"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := performProjectDesignSystemIDRequest(t, tt.handler, http.MethodPost, tt.path, systemID, tt.body)
			assertProjectDesignSystemErrorCode(t, response, http.StatusNotFound, "project_design_system_not_found")
		})
	}
}

func TestMarshalProjectDesignSystemTaskContextPinsV2SchemaAndDigests(t *testing.T) {
	systemID := parseUUID("11111111-1111-1111-1111-111111111111")
	workspaceID := parseUUID("22222222-2222-2222-2222-222222222222")
	projectID := parseUUID("33333333-3333-3333-3333-333333333333")
	agentID := parseUUID("44444444-4444-4444-4444-444444444444")
	requesterID := parseUUID("55555555-5555-5555-5555-555555555555")
	system := db.ProjectDesignSystem{ID: systemID, WorkspaceID: workspaceID, ProjectID: projectID}
	project := db.Project{ID: projectID, Title: "Native agent design system", Description: pgtype.Text{String: "Test", Valid: true}}
	input := projectDesignSystemInputSnapshot{
		AgentID:  agentID.String(),
		Platform: "web",
		Brief:    "Calm CRM",
		References: []projectDesignSystemReferenceSnapshot{
			{Kind: "brand_color", Label: "Primary", Value: "#123456"},
		},
	}
	canonicalInput, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input snapshot: %v", err)
	}
	expectedInputDigest, err := projectdesignsystem.SnapshotDigest(canonicalInput)
	if err != nil {
		t.Fatalf("digest input snapshot: %v", err)
	}
	basePackage := json.RawMessage(`{"design_md":"# base","tokens_css":":root{}","components_html":"<main>x</main>","integrity_sha256":"a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"}`)
	legacyOpenDesignRun := json.RawMessage(`{"schema":"open-design/v1","run":{"id":"run-legacy","status":"pending"}}`)

	generateJSON, err := marshalProjectDesignSystemTaskContext(
		system, &project, requesterID, agentID, input,
		service.ProjectDesignSystemGenerate, nil, "", nil, nil,
	)
	if err != nil {
		t.Fatalf("marshal generate context: %v", err)
	}
	var generated map[string]any
	if err := json.Unmarshal(generateJSON, &generated); err != nil {
		t.Fatalf("decode generate context: %v", err)
	}
	if generated["package_schema"] != projectdesignsystem.PackageSchemaV2 {
		t.Fatalf("generate package_schema = %v, want %s", generated["package_schema"], projectdesignsystem.PackageSchemaV2)
	}
	if got, _ := generated["input_snapshot_sha256"].(string); got != expectedInputDigest {
		t.Fatalf("generate input_snapshot_sha256 = %q, want %q", got, expectedInputDigest)
	}
	if _, present := generated["base_package_sha256"]; present {
		t.Fatalf("generate must not set base_package_sha256 without a base, got %v", generated["base_package_sha256"])
	}
	if generated["open_design_run"] != nil {
		t.Fatalf("generate must not synthesize open_design_run, got %v", generated["open_design_run"])
	}

	adjustJSON, err := marshalProjectDesignSystemTaskContext(
		system, &project, requesterID, agentID, input,
		service.ProjectDesignSystemAdjust, basePackage, "tighten the spacing", json.RawMessage(`{"kind":"all"}`), nil,
	)
	if err != nil {
		t.Fatalf("marshal adjust context: %v", err)
	}
	var adjusted map[string]any
	if err := json.Unmarshal(adjustJSON, &adjusted); err != nil {
		t.Fatalf("decode adjust context: %v", err)
	}
	if adjusted["package_schema"] != projectdesignsystem.PackageSchemaV2 {
		t.Fatalf("adjust package_schema = %v, want %s", adjusted["package_schema"], projectdesignsystem.PackageSchemaV2)
	}
	if got, _ := adjusted["input_snapshot_sha256"].(string); got != expectedInputDigest {
		t.Fatalf("adjust input_snapshot_sha256 = %q, want %q", got, expectedInputDigest)
	}
	if got, _ := adjusted["base_package_sha256"].(string); got != "sha256:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2" {
		t.Fatalf("adjust base_package_sha256 = %q, want sha256-prefixed integrity from base", got)
	}
	if _, present := adjusted["open_design_run"]; present {
		t.Fatalf("adjust must not synthesize open_design_run, got %v", adjusted["open_design_run"])
	}

	// Legacy Open Design tasks keep parsing: the open_design_run envelope
	// remains in the struct, but the V2 markers are not stamped onto the
	// Open Design path so the V2 contract is opt-in.
	legacyJSON, err := marshalProjectDesignSystemTaskContext(
		system, &project, requesterID, agentID, input,
		service.ProjectDesignSystemAdjust, basePackage, "tighten the spacing", json.RawMessage(`{"kind":"all"}`), legacyOpenDesignRun,
	)
	if err != nil {
		t.Fatalf("marshal legacy adjust context: %v", err)
	}
	var legacy map[string]any
	if err := json.Unmarshal(legacyJSON, &legacy); err != nil {
		t.Fatalf("decode legacy adjust context: %v", err)
	}
	if _, present := legacy["package_schema"]; present {
		t.Fatalf("legacy open-design adjust must not set package_schema, got %v", legacy["package_schema"])
	}
	if got, _ := legacy["open_design_run"].(map[string]any); got["run"].(map[string]any)["id"] != "run-legacy" {
		t.Fatalf("legacy adjust must preserve open_design_run, got %v", legacy["open_design_run"])
	}
}

func TestMarshalRepositoryAnalysisContextKeepsRepositoryContract(t *testing.T) {
	systemID := parseUUID("11111111-1111-1111-1111-111111111111")
	workspaceID := parseUUID("22222222-2222-2222-2222-222222222222")
	projectID := parseUUID("33333333-3333-3333-3333-333333333333")
	agentID := parseUUID("44444444-4444-4444-4444-444444444444")
	requesterID := parseUUID("55555555-5555-5555-5555-555555555555")
	system := db.ProjectDesignSystem{ID: systemID, WorkspaceID: workspaceID, ProjectID: projectID}
	project := db.Project{ID: projectID, Title: "Repo analysis", Description: pgtype.Text{String: "Test", Valid: true}}
	input := projectDesignSystemInputSnapshot{
		AgentID:  agentID.String(),
		Platform: "web",
		Brief:    "Analyse the existing repo",
		References: []projectDesignSystemReferenceSnapshot{
			{Kind: "brand_color", Label: "Primary", Value: "#abcdef"},
		},
	}

	contextJSON, err := marshalProjectDesignSystemTaskContext(
		system, &project, requesterID, agentID, input,
		service.ProjectDesignSystemRepositoryAnalysis, nil, "", nil, nil,
	)
	if err != nil {
		t.Fatalf("marshal repository analysis context: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(contextJSON, &decoded); err != nil {
		t.Fatalf("decode repository analysis context: %v", err)
	}
	if decoded["operation"] != string(service.ProjectDesignSystemRepositoryAnalysis) {
		t.Fatalf("repository analysis operation = %v, want %s", decoded["operation"], service.ProjectDesignSystemRepositoryAnalysis)
	}
	if decoded["type"] != service.ProjectDesignSystemTaskContextType {
		t.Fatalf("repository analysis type = %v, want %s", decoded["type"], service.ProjectDesignSystemTaskContextType)
	}
	if _, present := decoded["package_schema"]; present {
		t.Fatalf("repository analysis must not set package_schema, got %v", decoded["package_schema"])
	}
	if _, present := decoded["input_snapshot_sha256"]; present {
		t.Fatalf("repository analysis must not set input_snapshot_sha256, got %v", decoded["input_snapshot_sha256"])
	}
	if _, present := decoded["base_package_sha256"]; present {
		t.Fatalf("repository analysis must not set base_package_sha256, got %v", decoded["base_package_sha256"])
	}
	policyRaw, ok := decoded["output_policy"].(map[string]any)
	if !ok {
		t.Fatalf("repository analysis output_policy missing or wrong type: %v", decoded["output_policy"])
	}
	if policyRaw["result_marker"] != "REPOSITORY_DESIGN_CONTEXT_JSON:" {
		t.Fatalf("repository analysis output_policy.result_marker = %v, want REPOSITORY_DESIGN_CONTEXT_JSON:", policyRaw["result_marker"])
	}
	if policyRaw["read_only"] != true {
		t.Fatalf("repository analysis output_policy.read_only = %v, want true", policyRaw["read_only"])
	}
	if policyRaw["scripts_allowed"] != false {
		t.Fatalf("repository analysis output_policy.scripts_allowed = %v, want false", policyRaw["scripts_allowed"])
	}
}

// TestNativeProjectDesignSystemOperationsDoNotRequireOpenDesignEnvironment
// proves the Phase A boundary: the create / adjust / regenerate handlers enqueue
// a native V2 task with no Open Design environment present. Each subtest
// unsets every MULTICA_OPEN_DESIGN_* variable, drives one real handler, and
// asserts the enqueued task carries the V2 contract, its raw JSON has no
// open_design_run envelope, and no open_design_run row exists for the system.
// The V2 draft for adjust/regenerate is seeded through the completion fixture /
// fake receipt; the tested behavior stops at handler enqueue + task context
// and never starts a daemon finalizer or a user Agent CLI. These tests must
// NOT call t.Parallel: they mutate the process environment.
func TestNativeProjectDesignSystemOperationsDoNotRequireOpenDesignEnvironment(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		unsetOpenDesignEnvironmentForTest(t)
		projectID := createProjectForDesignTest(t, "Native environment isolation project")
		agentID, _ := createProjectDesignSystemAgent(t, "online")
		response := performProjectDesignSystemRequest(t, testHandler.CreateProjectDesignSystem, http.MethodPost, "/api/project-design-systems", map[string]any{
			"project_id": projectID,
			"agent_id":   agentID,
			"platform":   "web",
			"brief":      "Native environment isolation.",
		})
		if response.Code != http.StatusAccepted {
			t.Fatalf("CreateProjectDesignSystem: status = %d, body = %s", response.Code, response.Body.String())
		}
		var created ProjectDesignSystemResponse
		if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
			t.Fatalf("decode create response: %v", err)
		}
		if created.ID == "" || created.ActiveTask == nil || created.ActiveTask.ID == "" {
			t.Fatalf("create response missing system/task identity: %+v", created)
		}
		taskContext := assertNativeV2TaskWithoutOpenDesignRun(t, created.ActiveTask.ID, created.ID)
		if taskContext.Operation != service.ProjectDesignSystemGenerate {
			t.Fatalf("create operation = %q, want %q", taskContext.Operation, service.ProjectDesignSystemGenerate)
		}
		if taskContext.InputSnapshotSHA256 == "" {
			t.Fatalf("create input snapshot digest is empty")
		}
		if taskContext.BasePackageSHA256 != "" {
			t.Fatalf("create base package digest = %q, want empty", taskContext.BasePackageSHA256)
		}
		t.Logf("create task=%s system=%s operation=%s package_schema=%s input_snapshot_sha256=%s",
			created.ActiveTask.ID, created.ID, taskContext.Operation, taskContext.PackageSchema, taskContext.InputSnapshotSHA256)
	})

	t.Run("adjust", func(t *testing.T) {
		unsetOpenDesignEnvironmentForTest(t)
		fixture := newNativeV2CompletionFixture(t, service.ProjectDesignSystemGenerate)
		if response := fixture.completeTask(t, fixture.buildPackagePayload(t, nil)); response.Code != http.StatusOK {
			t.Fatalf("complete native package: status = %d, body = %s", response.Code, response.Body.String())
		}
		inputJSON, err := json.Marshal(map[string]any{
			"agent_id":   fixture.Completion.AgentID,
			"platform":   "web",
			"brief":      "Native adjustment isolation.",
			"references": []any{},
		})
		if err != nil {
			t.Fatalf("marshal adjustment input snapshot: %v", err)
		}
		if _, err := testPool.Exec(context.Background(), `
			UPDATE project_design_system
			SET input_snapshot = $1::jsonb
			WHERE id = $2
		`, inputJSON, fixture.Completion.System.ID); err != nil {
			t.Fatalf("seed adjustment input snapshot: %v", err)
		}
		systemID := uuidToString(fixture.Completion.System.ID)
		response := performProjectDesignSystemIDRequest(t, testHandler.AdjustProjectDesignSystem, http.MethodPost, "/api/project-design-systems/"+systemID+"/adjust", systemID, map[string]any{
			"agent_id":    fixture.Completion.AgentID,
			"instruction": "Tighten the primary action.",
			"scope":       map[string]any{"kind": "all"},
		})
		if response.Code != http.StatusAccepted {
			t.Fatalf("AdjustProjectDesignSystem: status = %d, body = %s", response.Code, response.Body.String())
		}
		var adjusted ProjectDesignSystemResponse
		if err := json.NewDecoder(response.Body).Decode(&adjusted); err != nil {
			t.Fatalf("decode adjustment response: %v", err)
		}
		if adjusted.ID == "" || adjusted.ActiveTask == nil || adjusted.ActiveTask.ID == "" {
			t.Fatalf("adjustment response missing system/task identity: %+v", adjusted)
		}
		taskContext := assertNativeV2TaskWithoutOpenDesignRun(t, adjusted.ActiveTask.ID, adjusted.ID)
		if taskContext.Operation != service.ProjectDesignSystemAdjust {
			t.Fatalf("adjust operation = %q, want %q", taskContext.Operation, service.ProjectDesignSystemAdjust)
		}
		if taskContext.BasePackageSHA256 != fixture.Collected.Manifest.ContentDigest {
			t.Fatalf("adjust base package digest = %q, want %q", taskContext.BasePackageSHA256, fixture.Collected.Manifest.ContentDigest)
		}
		t.Logf("adjust task=%s system=%s operation=%s package_schema=%s base_package_sha256=%s input_snapshot_sha256=%s",
			adjusted.ActiveTask.ID, adjusted.ID, taskContext.Operation, taskContext.PackageSchema, taskContext.BasePackageSHA256, taskContext.InputSnapshotSHA256)
	})

	t.Run("regenerate", func(t *testing.T) {
		unsetOpenDesignEnvironmentForTest(t)
		fixture := newNativeV2CompletionFixture(t, service.ProjectDesignSystemGenerate)
		if response := fixture.completeTask(t, fixture.buildPackagePayload(t, nil)); response.Code != http.StatusOK {
			t.Fatalf("complete native package: status = %d, body = %s", response.Code, response.Body.String())
		}
		inputJSON, err := json.Marshal(map[string]any{
			"agent_id":   fixture.Completion.AgentID,
			"platform":   "web",
			"brief":      "Native regeneration isolation.",
			"references": []any{},
		})
		if err != nil {
			t.Fatalf("marshal regeneration input snapshot: %v", err)
		}
		if _, err := testPool.Exec(context.Background(), `
			UPDATE project_design_system
			SET input_snapshot = $1::jsonb
			WHERE id = $2
		`, inputJSON, fixture.Completion.System.ID); err != nil {
			t.Fatalf("seed regeneration input snapshot: %v", err)
		}
		systemID := uuidToString(fixture.Completion.System.ID)
		response := performProjectDesignSystemIDRequest(t, testHandler.RegenerateProjectDesignSystem, http.MethodPost, "/api/project-design-systems/"+systemID+"/regenerate", systemID, map[string]any{
			"agent_id": fixture.Completion.AgentID,
		})
		if response.Code != http.StatusAccepted {
			t.Fatalf("RegenerateProjectDesignSystem: status = %d, body = %s", response.Code, response.Body.String())
		}
		var regenerated ProjectDesignSystemResponse
		if err := json.NewDecoder(response.Body).Decode(&regenerated); err != nil {
			t.Fatalf("decode regenerate response: %v", err)
		}
		if regenerated.ID == "" || regenerated.ActiveTask == nil || regenerated.ActiveTask.ID == "" {
			t.Fatalf("regenerate response missing system/task identity: %+v", regenerated)
		}
		taskContext := assertNativeV2TaskWithoutOpenDesignRun(t, regenerated.ActiveTask.ID, regenerated.ID)
		if taskContext.Operation != service.ProjectDesignSystemRegenerate {
			t.Fatalf("regenerate operation = %q, want %q", taskContext.Operation, service.ProjectDesignSystemRegenerate)
		}
		if taskContext.BasePackageSHA256 != fixture.Collected.Manifest.ContentDigest {
			t.Fatalf("regenerate base package digest = %q, want %q", taskContext.BasePackageSHA256, fixture.Collected.Manifest.ContentDigest)
		}
		t.Logf("regenerate task=%s system=%s operation=%s package_schema=%s base_package_sha256=%s input_snapshot_sha256=%s",
			regenerated.ActiveTask.ID, regenerated.ID, taskContext.Operation, taskContext.PackageSchema, taskContext.BasePackageSHA256, taskContext.InputSnapshotSHA256)
	})
}

func createProjectDesignSystemAgent(t *testing.T, runtimeStatus string) (string, string) {
	t.Helper()
	suffix := time.Now().UnixNano()
	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, last_seen_at, owner_id
		) VALUES ($1, NULL, $2, 'cloud', 'project_design_system_test', $3, '', '{}'::jsonb, now(), $4)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Project Design System Runtime %d", suffix), runtimeStatus, testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("create project design system runtime: %v", err)
	}

	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		) VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Project Design System Agent %d", suffix), runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create project design system agent: %v", err)
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return agentID, runtimeID
}

func performProjectDesignSystemRequest(
	t *testing.T,
	handler http.HandlerFunc,
	method string,
	path string,
	body any,
) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler(recorder, newRequest(method, path, body))
	return recorder
}

func performProjectDesignSystemIDRequest(
	t *testing.T,
	handler http.HandlerFunc,
	method string,
	path string,
	id string,
	body any,
) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := withURLParam(newRequest(method, path, body), "id", id)
	handler(recorder, request)
	return recorder
}

func createProjectDesignSystemIdentityForTest(
	t *testing.T,
	projectID string,
	agentID string,
	input projectDesignSystemInputSnapshot,
) db.ProjectDesignSystem {
	t.Helper()
	inputJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal project design system input: %v", err)
	}
	system, err := db.New(testPool).CreateProjectDesignSystem(context.Background(), db.CreateProjectDesignSystemParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		ProjectID:      parseUUID(projectID),
		Name:           "Test design system",
		Platform:       input.Platform,
		CurrentAgentID: parseUUID(agentID),
		InputSnapshot:  inputJSON,
		CreatedBy:      parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("create project design system identity: %v", err)
	}
	return system
}

func validProjectDesignSystemPackageForTest(t *testing.T) projectdesignsystem.ValidatedPackage {
	t.Helper()
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join("..", "projectdesignsystem", "testdata", "valid", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return string(data)
	}
	pkg, err := projectdesignsystem.Validate(projectdesignsystem.ArtifactInput{
		DesignMD:       read("DESIGN.md"),
		TokensCSS:      read("tokens.css"),
		ComponentsHTML: read("components.html"),
	}, nil)
	if err != nil {
		t.Fatalf("validate project design system fixture: %v", err)
	}
	return pkg
}

func upsertValidatedProjectDesignSystemPackageForTest(
	t *testing.T,
	designSystemID pgtype.UUID,
	slot string,
	pkg projectdesignsystem.ValidatedPackage,
) {
	t.Helper()
	manifestJSON, err := json.Marshal(pkg.Manifest)
	if err != nil {
		t.Fatalf("marshal package manifest: %v", err)
	}
	validationJSON, err := json.Marshal(pkg.Validation)
	if err != nil {
		t.Fatalf("marshal package validation: %v", err)
	}
	if _, err := db.New(testPool).UpsertProjectDesignSystemPackage(context.Background(), db.UpsertProjectDesignSystemPackageParams{
		DesignSystemID:  designSystemID,
		Slot:            slot,
		DesignMd:        pkg.Artifacts.DesignMD,
		TokensCss:       pkg.Artifacts.TokensCSS,
		ComponentsHtml:  pkg.Artifacts.ComponentsHTML,
		Manifest:        manifestJSON,
		Validation:      validationJSON,
		IntegritySha256: pkg.Manifest.Digest,
		WorkspaceID:     parseUUID(testWorkspaceID),
	}); err != nil {
		t.Fatalf("upsert validated %s package: %v", slot, err)
	}
}

func createProjectDesignSystemReferencesForTest(t *testing.T, projectID string) (string, string, string) {
	t.Helper()
	ctx := context.Background()
	var attachmentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO attachment (
			id, workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes
		) VALUES (gen_random_uuid(), $1, 'member', $2, 'atlas-logo.png', 'https://static.soyoung.com/atlas-logo.png', 'image/png', 2048)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&attachmentID); err != nil {
		t.Fatalf("create reference attachment: %v", err)
	}

	var designFileID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO design_file (workspace_id, project_id, title, description, source_type, source_ref, created_by)
		VALUES ($1, $2, 'Atlas dashboard', '', 'import', '{}'::jsonb, $3)
		RETURNING id
	`, testWorkspaceID, projectID, testUserID).Scan(&designFileID); err != nil {
		t.Fatalf("create reference design file: %v", err)
	}
	var revisionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO design_revision (
			file_id, workspace_id, revision_number, status, native_json, validation_errors, created_by
		) VALUES (
			$1, $2, 1, 'valid',
			'{"frames":[{"id":"frame-1","name":"Dashboard","previewAssetId":"asset-preview"}],"assets":{"asset-preview":{"url":"https://static.soyoung.com/atlas-dashboard.png"}},"layers":{}}'::jsonb,
			'[]'::jsonb, $3
		)
		RETURNING id
	`, designFileID, testWorkspaceID, testUserID).Scan(&revisionID); err != nil {
		t.Fatalf("create reference design revision: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE design_file SET current_revision_id = $2 WHERE id = $1`, designFileID, revisionID); err != nil {
		t.Fatalf("set current reference revision: %v", err)
	}

	var profileID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO design_system_profile (
			workspace_id, project_id, source_file_id, source_revision_id, name, description,
			status, is_default, profile_json, analysis_errors, created_by
		) VALUES (
			$1, $2, $3, $4, 'Atlas Figma UI specification', '',
			'analyzed', false, '{"density":"compact"}'::jsonb, '[]'::jsonb, $5
		)
		RETURNING id
	`, testWorkspaceID, projectID, designFileID, revisionID, testUserID).Scan(&profileID); err != nil {
		t.Fatalf("create reference UI specification: %v", err)
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM design_system_profile WHERE id = $1`, profileID)
		_, _ = testPool.Exec(ctx, `DELETE FROM design_revision WHERE id = $1`, revisionID)
		_, _ = testPool.Exec(ctx, `DELETE FROM design_file WHERE id = $1`, designFileID)
		_, _ = testPool.Exec(ctx, `DELETE FROM attachment WHERE id = $1`, attachmentID)
	})
	return attachmentID, designFileID, profileID
}

func getProjectDesignSystemPackageForTest(t *testing.T, systemID pgtype.UUID, slot string) db.ProjectDesignSystemPackage {
	t.Helper()
	pkg, err := db.New(testPool).GetProjectDesignSystemPackageBySlot(context.Background(), db.GetProjectDesignSystemPackageBySlotParams{
		DesignSystemID: systemID,
		Slot:           slot,
		WorkspaceID:    parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("get %s project design system package: %v", slot, err)
	}
	return pkg
}

func assertProjectDesignSystemErrorCode(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d: %s", response.Code, status, response.Body.String())
	}
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if payload["code"] != code {
		t.Fatalf("error code = %#v, want %q: %#v", payload["code"], code, payload)
	}
}

// unsetOpenDesignEnvironmentForTest removes every MULTICA_OPEN_DESIGN_*
// environment variable so the handler under test runs with no Open Design
// environment present. It is a real unset (not an empty-string set) and
// restores the prior value in cleanup. Callers must not use t.Parallel:
// this mutates the shared process environment.
func unsetOpenDesignEnvironmentForTest(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"MULTICA_OPEN_DESIGN_ENABLED",
		"MULTICA_OPEN_DESIGN_WORKER_URL",
		"MULTICA_OPEN_DESIGN_WORKER_TOKEN",
		"MULTICA_OPEN_DESIGN_ARTIFACT_ROOT",
		"MULTICA_OPEN_DESIGN_BROWSER_PATH",
	} {
		value, existed := os.LookupEnv(name)
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("unset %s: %v", name, err)
		}
		t.Cleanup(func() {
			if existed {
				_ = os.Setenv(name, value)
			} else {
				_ = os.Unsetenv(name)
			}
		})
	}
}

// assertNativeV2TaskWithoutOpenDesignRun proves the native V2 contract for an
// enqueued project design system task: the raw stored context JSON has no
// open_design_run key, it decodes to PackageSchemaV2, and the owning design
// system has zero open_design_run rows.
func assertNativeV2TaskWithoutOpenDesignRun(t *testing.T, taskID, systemID string) service.ProjectDesignSystemTaskContext {
	t.Helper()
	var raw []byte
	if err := testPool.QueryRow(context.Background(), `SELECT context FROM agent_task_queue WHERE id = $1`, taskID).Scan(&raw); err != nil {
		t.Fatalf("load task context: %v", err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("decode raw task context: %v", err)
	}
	if _, exists := object["open_design_run"]; exists {
		t.Fatalf("task context contains open_design_run: %s", raw)
	}
	var taskContext service.ProjectDesignSystemTaskContext
	if err := json.Unmarshal(raw, &taskContext); err != nil || taskContext.PackageSchema != projectdesignsystem.PackageSchemaV2 {
		t.Fatalf("native task context = %+v, err = %v", taskContext, err)
	}
	var runCount int
	if err := testPool.QueryRow(context.Background(), `SELECT COUNT(*) FROM open_design_run WHERE design_system_id = $1`, systemID).Scan(&runCount); err != nil {
		t.Fatalf("count open_design_run: %v", err)
	}
	if runCount != 0 {
		t.Fatalf("open_design_run count = %d, want 0", runCount)
	}
	return taskContext
}

func TestMarshalRepositoryProjectDesignSystemContextAlwaysUsesSingleAgent(t *testing.T) {
	systemID := parseUUID("11111111-1111-1111-1111-111111111111")
	workspaceID := parseUUID("22222222-2222-2222-2222-222222222222")
	projectID := parseUUID("33333333-3333-3333-3333-333333333333")
	repositoryID := parseUUID("66666666-6666-6666-6666-666666666666")
	agentID := parseUUID("44444444-4444-4444-4444-444444444444")
	requesterID := parseUUID("55555555-5555-5555-5555-555555555555")
	system := db.ProjectDesignSystem{ID: systemID, WorkspaceID: workspaceID, ProjectID: projectID, ProjectResourceID: repositoryID}
	project := db.Project{ID: projectID, Title: "Repository design system"}
	input := projectDesignSystemInputSnapshot{
		AgentID: agentID.String(), GenerationMode: service.ProjectDesignSystemExecutionModeProgrammaticFirst,
		Platform: "web", Brief: "Generate from repository evidence.", References: []projectDesignSystemReferenceSnapshot{},
	}

	for _, operation := range []service.ProjectDesignSystemOperation{
		service.ProjectDesignSystemGenerate, service.ProjectDesignSystemAdjust, service.ProjectDesignSystemRegenerate,
	} {
		raw, err := marshalProjectDesignSystemTaskContext(
			system, &project, requesterID, agentID, input, operation, nil, "", nil, nil,
		)
		if err != nil {
			t.Fatalf("marshal %s context: %v", operation, err)
		}
		var taskContext service.ProjectDesignSystemTaskContext
		if err := json.Unmarshal(raw, &taskContext); err != nil {
			t.Fatal(err)
		}
		if taskContext.ExecutionMode != "" || taskContext.ProjectResourceID != repositoryID.String() {
			t.Fatalf("%s context = %+v", operation, taskContext)
		}
		if taskContext.PackageSchema != projectdesignsystem.PackageSchemaV2 || taskContext.InputSnapshotSHA256 == "" {
			t.Fatalf("%s context lost V2 binding: %+v", operation, taskContext)
		}
		if strings.Contains(string(taskContext.OutputPolicy), "components.html") || !strings.Contains(string(taskContext.OutputPolicy), "ui-kit/index.html") {
			t.Fatalf("%s output policy = %s", operation, taskContext.OutputPolicy)
		}
	}
}

func TestProjectDesignSystemTaskResponsePreservesProgrammaticExecutionMode(t *testing.T) {
	contextJSON, err := json.Marshal(service.ProjectDesignSystemTaskContext{
		Type: service.ProjectDesignSystemTaskContextType, Operation: service.ProjectDesignSystemGenerate,
		ExecutionMode: service.ProjectDesignSystemExecutionModeProgrammaticFirst,
	})
	if err != nil {
		t.Fatal(err)
	}
	response := projectDesignSystemTaskResponse(db.AgentTaskQueue{Context: contextJSON})
	if response.Operation != string(service.ProjectDesignSystemGenerate) || response.ExecutionMode != service.ProjectDesignSystemExecutionModeProgrammaticFirst {
		t.Fatalf("task response = %+v", response)
	}
}
