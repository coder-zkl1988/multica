# Design System Profile MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Design System Profile as a first-class design-center asset and pass the project's default profile into UI Agent draft tasks.

**Architecture:** Store design systems in a new workspace-scoped table linked to a source design file/revision. Backend creates and analyzes profiles from existing native design JSON, frontend lists project design systems in the design center, and UI Agent draft task context includes the default project profile when available. This is additive and does not replace template catalog or raw draft patch materialization yet.

**Tech Stack:** Go handlers + sqlc + PostgreSQL migrations, Next.js shared views in `packages/views`, React Query hooks in `packages/core`, Vitest/Go tests.

---

## Files And Responsibilities

- Create `server/migrations/247_design_system_profile.up.sql`: create `design_system_profile` table and default uniqueness index.
- Create `server/migrations/247_design_system_profile.down.sql`: drop design-system schema.
- Modify `server/pkg/db/queries/design.sql`: add sqlc queries for create/list/get/update/set-default.
- Regenerate `server/pkg/db/generated/design.sql.go` with `make sqlc`.
- Modify `server/internal/handler/design_file.go`: add request/response types, analyzer helper, handlers, and UI Agent context injection.
- Modify `server/internal/handler/design_file_test.go`: add backend tests.
- Modify `server/cmd/server/router.go`: register `/api/design-systems` routes.
- Modify `packages/core/types/design.ts`: add design-system types and API response/request types.
- Modify `packages/core/designs/keys.ts`: add design-system query keys.
- Modify `packages/core/designs/queries.ts`: add design-system query options.
- Modify `packages/core/api/client.ts`: add API client methods.
- Modify `packages/views/designs/designs-page.tsx`: add "设计系统" section/tab content in design center.
- Modify design detail view under `packages/views/designs/`: add "发布为设计系统" action. Locate exact file with `rg -n "DesignFilePage|DesignDetail|publishDesignRevisionAsTemplate" packages/views/designs`.

---

## Task 1: Database Schema And Queries

**Files:**
- Create: `server/migrations/247_design_system_profile.up.sql`
- Create: `server/migrations/247_design_system_profile.down.sql`
- Modify: `server/pkg/db/queries/design.sql`
- Generated: `server/pkg/db/generated/design.sql.go`

- [ ] **Step 1: Add failing sqlc query tests through handler tests first**

Add tests in Task 2 before running sqlc-dependent implementation. The failing tests should reference handler behavior, not generated methods directly.

- [ ] **Step 2: Create migration**

Create `server/migrations/247_design_system_profile.up.sql`:

```sql
CREATE TABLE design_system_profile (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id uuid REFERENCES project(id) ON DELETE SET NULL,
    source_file_id uuid NOT NULL REFERENCES design_file(id) ON DELETE CASCADE,
    source_revision_id uuid NOT NULL REFERENCES design_revision(id) ON DELETE CASCADE,
    name text NOT NULL,
    description text,
    status text NOT NULL DEFAULT 'analyzed',
    is_default boolean NOT NULL DEFAULT false,
    profile_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    analysis_errors jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_by uuid REFERENCES "user"(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT design_system_profile_status_check CHECK (status IN ('draft', 'analyzed', 'failed', 'archived'))
);

CREATE INDEX idx_design_system_profile_workspace_project
    ON design_system_profile (workspace_id, project_id, updated_at DESC);

CREATE INDEX idx_design_system_profile_source_file
    ON design_system_profile (source_file_id);

CREATE UNIQUE INDEX idx_design_system_profile_default_project
    ON design_system_profile (workspace_id, project_id)
    WHERE is_default = true
      AND project_id IS NOT NULL;
```

Create `server/migrations/247_design_system_profile.down.sql`:

```sql
DROP INDEX IF EXISTS idx_design_system_profile_default_project;
DROP INDEX IF EXISTS idx_design_system_profile_source_file;
DROP INDEX IF EXISTS idx_design_system_profile_workspace_project;
DROP TABLE IF EXISTS design_system_profile;
```

- [ ] **Step 3: Add sqlc queries**

Append to `server/pkg/db/queries/design.sql`:

```sql
-- Gallery Native design systems

-- name: ListDesignSystemProfiles :many
SELECT * FROM design_system_profile
WHERE workspace_id = $1
  AND (
    sqlc.narg('project_id')::uuid IS NULL
    OR project_id = sqlc.narg('project_id')::uuid
  )
  AND status <> 'archived'
ORDER BY is_default DESC, updated_at DESC, created_at DESC;

-- name: GetDesignSystemProfileInWorkspace :one
SELECT * FROM design_system_profile
WHERE id = $1
  AND workspace_id = $2
  AND status <> 'archived';

-- name: GetDefaultDesignSystemProfileForProject :one
SELECT * FROM design_system_profile
WHERE workspace_id = $1
  AND project_id = $2
  AND is_default = true
  AND status = 'analyzed'
ORDER BY updated_at DESC
LIMIT 1;

-- name: CreateDesignSystemProfile :one
INSERT INTO design_system_profile (
    workspace_id, project_id, source_file_id, source_revision_id, name,
    description, status, is_default, profile_json, analysis_errors, created_by
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9, $10, $11
)
RETURNING *;

-- name: ClearDefaultDesignSystemProfilesForProject :exec
UPDATE design_system_profile
SET is_default = false,
    updated_at = now()
WHERE workspace_id = $1
  AND project_id = $2
  AND is_default = true;

-- name: SetDesignSystemProfileDefault :one
UPDATE design_system_profile
SET is_default = true,
    updated_at = now()
WHERE id = $1
  AND workspace_id = $2
  AND project_id = $3
  AND status = 'analyzed'
RETURNING *;

-- name: UpdateDesignSystemProfileAnalysis :one
UPDATE design_system_profile
SET status = $3,
    profile_json = $4,
    analysis_errors = $5,
    updated_at = now()
WHERE id = $1
  AND workspace_id = $2
RETURNING *;
```

- [ ] **Step 4: Regenerate sqlc**

Run:

```bash
make sqlc
```

Expected: `server/pkg/db/generated/design.sql.go` includes `DesignSystemProfile` and the new query methods.

---

## Task 2: Backend API And Analyzer

**Files:**
- Modify: `server/internal/handler/design_file.go`
- Modify: `server/internal/handler/design_file_test.go`
- Modify: `server/cmd/server/router.go`

- [ ] **Step 1: Write failing handler tests**

Add tests to `server/internal/handler/design_file_test.go` near existing design draft/template tests:

```go
func TestCreateDesignSystemProfileFromDesignFile(t *testing.T) {
	ctx := context.Background()
	created := createTestDesignFile(t, "Design System Source")

	req := newRequest("POST", "/api/design-systems?workspace_id="+testWorkspaceID, map[string]any{
		"project_id":          testProjectID,
		"source_file_id":     created.File.ID,
		"source_revision_id": created.CurrentRevision.ID,
		"name":               "CRM 后台设计系统",
		"description":        "CRM admin base components",
		"is_default":         true,
	})
	w := httptest.NewRecorder()
	testHandler.CreateDesignSystemProfile(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("CreateDesignSystemProfile: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp DesignSystemProfileResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Name != "CRM 后台设计系统" {
		t.Fatalf("name = %q", resp.Name)
	}
	if !resp.IsDefault {
		t.Fatal("created design system should be default")
	}
	if len(resp.ProfileJSON) == 0 || string(resp.ProfileJSON) == "null" {
		t.Fatal("profile_json should be populated")
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM design_system_profile WHERE id = $1`, resp.ID)
	})
}

func TestListDesignSystemProfilesByProject(t *testing.T) {
	created := createTestDesignFile(t, "Design System List Source")
	createReq := newRequest("POST", "/api/design-systems?workspace_id="+testWorkspaceID, map[string]any{
		"project_id":          testProjectID,
		"source_file_id":     created.File.ID,
		"source_revision_id": created.CurrentRevision.ID,
		"name":               "Listable Design System",
	})
	createW := httptest.NewRecorder()
	testHandler.CreateDesignSystemProfile(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("create setup: %d %s", createW.Code, createW.Body.String())
	}

	listReq := newRequest("GET", "/api/design-systems?workspace_id="+testWorkspaceID+"&project_id="+testProjectID, nil)
	listW := httptest.NewRecorder()
	testHandler.ListDesignSystemProfiles(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("ListDesignSystemProfiles: expected 200, got %d: %s", listW.Code, listW.Body.String())
	}
	var list DesignSystemProfileListResponse
	if err := json.NewDecoder(listW.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.DesignSystems) == 0 {
		t.Fatal("expected at least one design system")
	}
}
```

Run:

```bash
cd server && go test ./internal/handler -run 'Test(CreateDesignSystemProfileFromDesignFile|ListDesignSystemProfilesByProject)' -count=1
```

Expected: FAIL because handlers/types/routes do not exist.

- [ ] **Step 2: Add backend request/response types**

Add near the other design response types in `server/internal/handler/design_file.go`:

```go
type DesignSystemProfileResponse struct {
	ID               string          `json:"id"`
	WorkspaceID      string          `json:"workspace_id"`
	ProjectID        *string         `json:"project_id,omitempty"`
	SourceFileID     string          `json:"source_file_id"`
	SourceRevisionID string          `json:"source_revision_id"`
	Name             string          `json:"name"`
	Description      *string         `json:"description,omitempty"`
	Status           string          `json:"status"`
	IsDefault        bool            `json:"is_default"`
	ProfileJSON      json.RawMessage `json:"profile_json"`
	AnalysisErrors   json.RawMessage `json:"analysis_errors"`
	CreatedBy        *string         `json:"created_by"`
	CreatedAt        string          `json:"created_at"`
	UpdatedAt        string          `json:"updated_at"`
}

type DesignSystemProfileListResponse struct {
	DesignSystems []DesignSystemProfileResponse `json:"design_systems"`
}

type CreateDesignSystemProfileRequest struct {
	ProjectID        string `json:"project_id"`
	SourceFileID     string `json:"source_file_id"`
	SourceRevisionID string `json:"source_revision_id"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	IsDefault        bool   `json:"is_default"`
}
```

- [ ] **Step 3: Add response mapper**

Add near existing mappers:

```go
func designSystemProfileToResponse(profile db.DesignSystemProfile) DesignSystemProfileResponse {
	return DesignSystemProfileResponse{
		ID:               uuidToString(profile.ID),
		WorkspaceID:      uuidToString(profile.WorkspaceID),
		ProjectID:        uuidToPtr(profile.ProjectID),
		SourceFileID:     uuidToString(profile.SourceFileID),
		SourceRevisionID: uuidToString(profile.SourceRevisionID),
		Name:             profile.Name,
		Description:      textToPtr(profile.Description),
		Status:           profile.Status,
		IsDefault:        profile.IsDefault,
		ProfileJSON:      json.RawMessage(profile.ProfileJson),
		AnalysisErrors:   json.RawMessage(profile.AnalysisErrors),
		CreatedBy:        uuidToPtr(profile.CreatedBy),
		CreatedAt:        timestampToString(profile.CreatedAt),
		UpdatedAt:        timestampToString(profile.UpdatedAt),
	}
}
```

If `textToPtr` is unavailable, add:

```go
func textToPtr(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}
```

- [ ] **Step 4: Add lightweight analyzer**

Add in `server/internal/handler/design_file.go`:

```go
func analyzeDesignSystemProfile(nativeJSON json.RawMessage, sourceFileID string, sourceRevisionID string) (json.RawMessage, json.RawMessage, string) {
	var doc map[string]any
	if err := json.Unmarshal(nativeJSON, &doc); err != nil {
		return []byte(`{}`), []byte(`[{"message":"native_json is invalid"}]`), "failed"
	}
	profile := map[string]any{
		"version": "1.0",
		"source": map[string]any{
			"file_id":     sourceFileID,
			"revision_id": sourceRevisionID,
		},
		"tokens": map[string]any{
			"colors":     extractDesignSystemColors(doc, 40),
			"typography": extractDesignSystemTypography(doc, 40),
			"spacing":    []any{},
			"radius":     []any{},
		},
		"components": extractDesignSystemComponents(doc, 80),
		"guidelines": []string{
			"Use extracted tokens and component examples as the project visual contract.",
			"Prefer project design system examples over template residue when generating UI drafts.",
		},
		"confidence": map[string]any{"overall": "low"},
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		return []byte(`{}`), []byte(`[{"message":"failed to marshal profile"}]`), "failed"
	}
	return raw, []byte(`[]`), "analyzed"
}
```

Implement helper functions below it with bounded traversal of `native_json["layers"]`.

Use this minimal extraction behavior:

```go
func extractDesignSystemColors(doc map[string]any, limit int) []map[string]any {
	// Traverse layer maps, collect distinct fill/stroke/color-like string values.
	// Return objects like {"value":"#1677ff","source_layer_id":"...","source_layer_name":"..."}.
	// Bound output to limit.
	return []map[string]any{}
}

func extractDesignSystemTypography(doc map[string]any, limit int) []map[string]any {
	// Traverse text layers, collect fontSize/fontFamily/fontWeight/color/text samples when present.
	// Return bounded examples. Empty output is acceptable in MVP.
	return []map[string]any{}
}

func extractDesignSystemComponents(doc map[string]any, limit int) map[string][]map[string]any {
	// Traverse layer names and classify names containing button/input/select/tag/table/modal/card
	// or 按钮/输入/选择/标签/表格/弹窗/卡片.
	return map[string][]map[string]any{
		"button": {},
		"input":  {},
		"select": {},
		"tag":    {},
		"table":  {},
		"modal":  {},
		"card":   {},
	}
}
```

Keep the helpers conservative. Do not attempt perfect Figma semantics in this task.

- [ ] **Step 5: Add handlers**

Add in `server/internal/handler/design_file.go`:

```go
func (h *Handler) ListDesignSystemProfiles(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	var projectUUID pgtype.UUID
	if raw := strings.TrimSpace(r.URL.Query().Get("project_id")); raw != "" {
		var parseOK bool
		projectUUID, parseOK = parseUUIDOrBadRequest(w, raw, "project_id")
		if !parseOK {
			return
		}
	}
	rows, err := h.Queries.ListDesignSystemProfiles(r.Context(), db.ListDesignSystemProfilesParams{WorkspaceID: wsUUID, ProjectID: projectUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list design systems")
		return
	}
	resp := make([]DesignSystemProfileResponse, len(rows))
	for i, row := range rows {
		resp[i] = designSystemProfileToResponse(row)
	}
	writeJSON(w, http.StatusOK, DesignSystemProfileListResponse{DesignSystems: resp})
}

func (h *Handler) CreateDesignSystemProfile(w http.ResponseWriter, r *http.Request) {
	var req CreateDesignSystemProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	sourceFileID, ok := parseUUIDOrBadRequest(w, req.SourceFileID, "source_file_id")
	if !ok {
		return
	}
	sourceRevisionID, ok := parseUUIDOrBadRequest(w, req.SourceRevisionID, "source_revision_id")
	if !ok {
		return
	}
	projectID, ok := parseUUIDOrBadRequest(w, req.ProjectID, "project_id")
	if !ok {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	file, err := h.Queries.GetDesignFileInWorkspace(r.Context(), db.GetDesignFileInWorkspaceParams{ID: sourceFileID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "source design file not found")
		return
	}
	if file.ProjectID.Valid && uuidToString(file.ProjectID) != uuidToString(projectID) {
		writeError(w, http.StatusBadRequest, "source design file does not belong to project")
		return
	}
	revision, err := h.Queries.GetDesignRevisionInWorkspace(r.Context(), db.GetDesignRevisionInWorkspaceParams{ID: sourceRevisionID, WorkspaceID: wsUUID})
	if err != nil || uuidToString(revision.FileID) != uuidToString(sourceFileID) {
		writeError(w, http.StatusNotFound, "source design revision not found")
		return
	}
	profileJSON, analysisErrors, status := analyzeDesignSystemProfile(revision.NativeJson, uuidToString(sourceFileID), uuidToString(sourceRevisionID))
	if req.IsDefault {
		if err := h.Queries.ClearDefaultDesignSystemProfilesForProject(r.Context(), db.ClearDefaultDesignSystemProfilesForProjectParams{WorkspaceID: wsUUID, ProjectID: projectID}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to clear default design system")
			return
		}
	}
	created, err := h.Queries.CreateDesignSystemProfile(r.Context(), db.CreateDesignSystemProfileParams{
		WorkspaceID:       wsUUID,
		ProjectID:         projectID,
		SourceFileID:      sourceFileID,
		SourceRevisionID:  sourceRevisionID,
		Name:              name,
		Description:       pgtype.Text{String: strings.TrimSpace(req.Description), Valid: strings.TrimSpace(req.Description) != ""},
		Status:            status,
		IsDefault:         req.IsDefault && status == "analyzed",
		ProfileJson:       profileJSON,
		AnalysisErrors:    analysisErrors,
		CreatedBy:         parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create design system")
		return
	}
	writeJSON(w, http.StatusCreated, designSystemProfileToResponse(created))
}
```

Then add `GetDesignSystemProfile` and `SetDesignSystemProfileDefault` in the same style.

- [ ] **Step 6: Register routes**

Modify `server/cmd/server/router.go` inside the authenticated design route block:

```go
r.Get("/api/design-systems", h.ListDesignSystemProfiles)
r.Post("/api/design-systems", h.CreateDesignSystemProfile)
r.Get("/api/design-systems/{id}", h.GetDesignSystemProfile)
r.Post("/api/design-systems/{id}/set-default", h.SetDesignSystemProfileDefault)
```

- [ ] **Step 7: Run backend tests**

Run:

```bash
cd server && go test ./internal/handler -run 'Test(CreateDesignSystemProfileFromDesignFile|ListDesignSystemProfilesByProject)' -count=1
```

Expected: PASS.

Run broader handler tests:

```bash
cd server && go test ./internal/handler -count=1
```

Expected: PASS.

---

## Task 3: UI Agent Context Uses Default Design System

**Files:**
- Modify: `server/internal/handler/design_file.go`
- Modify: `server/internal/handler/design_file_test.go`
- Modify: `server/internal/service/task.go` if context structs need typed fields.

- [ ] **Step 1: Write failing context test**

Add to `server/internal/handler/design_file_test.go`:

```go
func TestCreateDesignDraftAgentTaskIncludesDefaultDesignSystem(t *testing.T) {
	ctx := context.Background()
	created := createTestDesignFile(t, "Default Design System Source")
	profileJSON := []byte(`{"version":"1.0","tokens":{"colors":[{"value":"#1677ff"}]},"components":{"button":[{"name":"主按钮"}]}}`)
	var profileID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO design_system_profile (
			workspace_id, project_id, source_file_id, source_revision_id, name,
			status, is_default, profile_json, analysis_errors, created_by
		)
		VALUES ($1, $2, $3, $4, 'Default Design System', 'analyzed', true, $5, '[]', $6)
		RETURNING id
	`, testWorkspaceID, testProjectID, created.File.ID, created.CurrentRevision.ID, profileJSON, testUserID).Scan(&profileID); err != nil {
		t.Fatalf("insert design system: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM design_system_profile WHERE id = $1`, profileID) })

	req := newRequest("POST", "/api/design-drafts/agent-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"agent_id": agentIDWithRuntime(t),
		"issue_id": createUIDesignIssueForProject(t, testProjectID),
		"title": "UI设计 设计草稿",
	})
	w := httptest.NewRecorder()
	testHandler.CreateDesignDraftAgentTask(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("CreateDesignDraftAgentTask: expected 202, got %d: %s", w.Code, w.Body.String())
	}

	var taskContext []byte
	if err := testPool.QueryRow(ctx, `
		SELECT context
		FROM agent_task_queue
		ORDER BY created_at DESC
		LIMIT 1
	`).Scan(&taskContext); err != nil {
		t.Fatalf("read task context: %v", err)
	}
	if !strings.Contains(string(taskContext), `"design_system"`) {
		t.Fatalf("context missing design_system: %s", string(taskContext))
	}
	if !strings.Contains(string(taskContext), `Default Design System`) {
		t.Fatalf("context missing design system name: %s", string(taskContext))
	}
}
```

Use existing test helpers where available instead of inventing duplicates. If `agentIDWithRuntime` or `createUIDesignIssueForProject` do not exist, use the fixture patterns already present in nearby draft-agent tests.

Run:

```bash
cd server && go test ./internal/handler -run TestCreateDesignDraftAgentTaskIncludesDefaultDesignSystem -count=1
```

Expected: FAIL because context does not include design system.

- [ ] **Step 2: Resolve project default profile in handler**

In `CreateDesignDraftAgentTask`, after issue/project context is available, resolve project id:

```go
var projectIDForDesignSystem pgtype.UUID
if issue.ProjectID.Valid {
	projectIDForDesignSystem = issue.ProjectID
} else if hasPresetTemplate && template.ProjectID.Valid {
	projectIDForDesignSystem = template.ProjectID
}
```

Then add:

```go
if projectIDForDesignSystem.Valid {
	if profile, err := h.Queries.GetDefaultDesignSystemProfileForProject(r.Context(), db.GetDefaultDesignSystemProfileForProjectParams{WorkspaceID: wsUUID, ProjectID: projectIDForDesignSystem}); err == nil {
		contextPayload["design_system"] = map[string]any{
			"id":                 uuidToString(profile.ID),
			"name":               profile.Name,
			"status":             profile.Status,
			"source_file_id":     uuidToString(profile.SourceFileID),
			"source_revision_id": uuidToString(profile.SourceRevisionID),
			"profile":            json.RawMessage(profile.ProfileJson),
		}
	} else {
		contextPayload["design_system"] = map[string]any{
			"status": "missing",
		}
	}
}
```

- [ ] **Step 3: Strengthen default prompt**

In the default prompt text for UI draft tasks, add design-system language:

```go
prompt = "Read the embedded issue, use the project design_system as the visual contract when present, choose the best template candidate as a structure reference, then generate a UI draft with meaningful design changes."
```

Keep this scoped to UI draft generation only.

- [ ] **Step 4: Run tests**

Run:

```bash
cd server && go test ./internal/handler -run 'TestCreateDesignDraftAgentTask' -count=1
```

Expected: PASS.

---

## Task 4: Core API Types And Queries

**Files:**
- Modify: `packages/core/types/design.ts`
- Modify: `packages/core/designs/keys.ts`
- Modify: `packages/core/designs/queries.ts`
- Modify: `packages/core/api/client.ts`

- [ ] **Step 1: Add types**

Add to `packages/core/types/design.ts`:

```ts
export type DesignSystemProfileStatus = "draft" | "analyzed" | "failed" | "archived";

export interface DesignSystemProfile {
  id: string;
  workspace_id: string;
  project_id?: string | null;
  source_file_id: string;
  source_revision_id: string;
  name: string;
  description?: string | null;
  status: DesignSystemProfileStatus;
  is_default: boolean;
  profile_json: Record<string, unknown>;
  analysis_errors: unknown[];
  created_by?: string | null;
  created_at: string;
  updated_at: string;
}

export interface ListDesignSystemProfilesResponse {
  design_systems: DesignSystemProfile[];
}

export interface CreateDesignSystemProfileRequest {
  project_id: string;
  source_file_id: string;
  source_revision_id: string;
  name: string;
  description?: string;
  is_default?: boolean;
}
```

- [ ] **Step 2: Add query keys**

Add to `packages/core/designs/keys.ts`:

```ts
designSystems: (wsId: string, projectId?: string) => ["designs", wsId, "design-systems", projectId ?? "all"] as const,
designSystem: (wsId: string, id: string) => ["designs", wsId, "design-systems", id] as const,
```

- [ ] **Step 3: Add API client methods**

Add imports and methods in `packages/core/api/client.ts`:

```ts
async listDesignSystemProfiles(params: { project_id?: string } = {}): Promise<ListDesignSystemProfilesResponse> {
  const search = new URLSearchParams();
  if (params.project_id) search.set("project_id", params.project_id);
  const suffix = search.toString();
  return this.fetch(`/api/design-systems${suffix ? `?${suffix}` : ""}`);
}

async createDesignSystemProfile(data: CreateDesignSystemProfileRequest): Promise<DesignSystemProfile> {
  return this.fetch("/api/design-systems", {
    method: "POST",
    body: JSON.stringify(data),
  });
}

async getDesignSystemProfile(id: string): Promise<DesignSystemProfile> {
  return this.fetch(`/api/design-systems/${encodeURIComponent(id)}`);
}

async setDesignSystemProfileDefault(id: string): Promise<DesignSystemProfile> {
  return this.fetch(`/api/design-systems/${encodeURIComponent(id)}/set-default`, { method: "POST" });
}
```

- [ ] **Step 4: Add React Query options**

Add to `packages/core/designs/queries.ts`:

```ts
export function designSystemListOptions(wsId: string, projectId?: string) {
  return queryOptions({
    queryKey: designKeys.designSystems(wsId, projectId),
    queryFn: () => api.listDesignSystemProfiles(projectId ? { project_id: projectId } : {}),
    select: (data) => data.design_systems,
  });
}

export function designSystemDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: designKeys.designSystem(wsId, id),
    queryFn: () => api.getDesignSystemProfile(id),
  });
}
```

- [ ] **Step 5: Run TS focused check**

Run:

```bash
pnpm --filter @multica/core typecheck
```

Expected: PASS. If the package has no `typecheck` script, run root:

```bash
pnpm typecheck
```

Expected: no new type errors from core design-system types.

---

## Task 5: Design Center UI

**Files:**
- Modify: `packages/views/designs/designs-page.tsx`
- Modify or create tests near `packages/views/designs/*.test.tsx` if existing patterns exist.

- [ ] **Step 1: Add failing render test if design page tests exist**

Check:

```bash
find packages/views/designs -name '*test*' -maxdepth 2
```

If there is an existing designs page test, add:

```tsx
it("renders design system section", () => {
  // Mock @multica/core designSystemListOptions data with one profile.
  // Render <DesignsPage /> under existing view test wrapper.
  // Expect text "设计系统" and "CRM 后台设计系统".
});
```

If no suitable test harness exists, skip creating a broad test and rely on TypeScript plus manual browser verification in Task 7.

- [ ] **Step 2: Fetch design systems**

In `packages/views/designs/designs-page.tsx`, import `designSystemListOptions`:

```ts
import { designDraftListOptions, designFileListOptions, designFolderListOptions, designRestoreTaskListOptions, designSystemListOptions, designTemplateListOptions } from "@multica/core/designs/queries";
```

Add query after `selectedProjectId` state is available. Since hooks cannot be conditional, call with the current string:

```ts
const { data: designSystems = [], isLoading: designSystemsLoading } = useQuery(designSystemListOptions(wsId, selectedProjectId || undefined));
```

- [ ] **Step 3: Add card component**

Add near `TemplateCatalogCard`:

```tsx
function DesignSystemCard({ profile, sourceFile }: { profile: DesignSystemProfile; sourceFile?: DesignFile }) {
  return (
    <div className="flex min-w-[240px] flex-col gap-3 rounded-lg border bg-card p-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="truncate text-sm font-medium">{profile.name}</div>
          <div className="mt-1 truncate text-xs text-muted-foreground">{profile.description ?? sourceFile?.title ?? "项目设计系统"}</div>
        </div>
        {profile.is_default ? <Badge variant="secondary" className="shrink-0">默认</Badge> : <Badge variant="outline" className="shrink-0">{profile.status}</Badge>}
      </div>
      <div className="flex items-center justify-between gap-2 text-xs text-muted-foreground">
        <span className="truncate">{sourceFile?.title ?? profile.source_file_id.slice(0, 8)}</span>
        <span className="shrink-0">{formatDate(profile.updated_at)}</span>
      </div>
    </div>
  );
}
```

Add `DesignSystemProfile` to the type import.

- [ ] **Step 4: Render design system section**

In `DesignsPage`, compute:

```ts
const fileById = useMemo(() => new Map(files.map((file) => [file.id, file])), [files]);
```

Render a section near the template/draft drawer or main page controls:

```tsx
<section className="border-b px-5 py-4">
  <div className="mb-3 flex items-center justify-between gap-3">
    <div>
      <h2 className="text-sm font-medium">设计系统</h2>
      <p className="text-xs text-muted-foreground">项目视觉规范，供 UI Agent 生成设计稿时参考。</p>
    </div>
  </div>
  {designSystemsLoading ? (
    <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
      <Skeleton className="h-28 rounded-lg" />
      <Skeleton className="h-28 rounded-lg" />
      <Skeleton className="h-28 rounded-lg" />
    </div>
  ) : designSystems.length ? (
    <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
      {designSystems.map((profile) => (
        <DesignSystemCard key={profile.id} profile={profile} sourceFile={fileById.get(profile.source_file_id)} />
      ))}
    </div>
  ) : (
    <div className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">
      暂无设计系统。打开一份 UI 规范设计稿后，可以发布为项目设计系统。
    </div>
  )}
</section>
```

Keep it operational and compact. Do not add marketing copy or a token editor.

- [ ] **Step 5: Run views typecheck/test**

Run:

```bash
pnpm --filter @multica/views typecheck
```

If unavailable:

```bash
pnpm typecheck
```

Expected: PASS or no new design-system errors.

---

## Task 6: Publish From Design Detail

**Files:**
- Modify the design detail page under `packages/views/designs/`.
- Modify API/query invalidation calls from Task 4.

- [ ] **Step 1: Locate design detail component**

Run:

```bash
rg -n "publishDesignRevisionAsTemplate|设计详情|DesignFileDetail|DesignFilePage|current_revision" packages/views/designs
```

Use the component that already renders a design file detail.

- [ ] **Step 2: Add publish dialog state**

In the detail component, add:

```tsx
const [publishDesignSystemOpen, setPublishDesignSystemOpen] = useState(false);
const [designSystemName, setDesignSystemName] = useState("");
const [designSystemDescription, setDesignSystemDescription] = useState("");
const [designSystemDefault, setDesignSystemDefault] = useState(true);
```

- [ ] **Step 3: Add mutation**

Add:

```tsx
const createDesignSystem = useMutation({
  mutationFn: () => {
    if (!file?.file.project_id) throw new Error("设计系统需要关联项目");
    if (!file.current_revision?.id) throw new Error("当前设计稿没有可发布版本");
    const name = designSystemName.trim() || `${file.file.title} 设计系统`;
    return api.createDesignSystemProfile({
      project_id: file.file.project_id,
      source_file_id: file.file.id,
      source_revision_id: file.current_revision.id,
      name,
      description: designSystemDescription.trim(),
      is_default: designSystemDefault,
    });
  },
  onSuccess: async () => {
    setPublishDesignSystemOpen(false);
    await queryClient.invalidateQueries({ queryKey: designKeys.designSystems(wsId, file?.file.project_id ?? undefined) });
    toast.success("已发布为设计系统");
  },
  onError: (error) => toast.error(error instanceof Error ? error.message : "发布设计系统失败"),
});
```

Adjust local variable names to match the actual detail component.

- [ ] **Step 4: Add action button**

Add a compact button in the existing action toolbar:

```tsx
<Button type="button" variant="outline" size="sm" onClick={() => {
  setDesignSystemName(`${file.file.title} 设计系统`);
  setPublishDesignSystemOpen(true);
}}>
  发布为设计系统
</Button>
```

- [ ] **Step 5: Add dialog**

Add:

```tsx
<Dialog open={publishDesignSystemOpen} onOpenChange={setPublishDesignSystemOpen}>
  <DialogContent>
    <DialogHeader>
      <DialogTitle>发布为设计系统</DialogTitle>
      <DialogDescription>将当前设计稿版本分析为项目 UI 规范，供 UI Agent 生成设计稿时使用。</DialogDescription>
    </DialogHeader>
    <div className="space-y-3">
      <Input value={designSystemName} onChange={(event) => setDesignSystemName(event.target.value)} placeholder="CRM 后台设计系统" />
      <Textarea value={designSystemDescription} onChange={(event) => setDesignSystemDescription(event.target.value)} placeholder="说明这个设计系统适用的项目或场景" />
      <label className="flex items-center gap-2 text-sm">
        <input type="checkbox" checked={designSystemDefault} onChange={(event) => setDesignSystemDefault(event.target.checked)} />
        设为当前项目默认设计系统
      </label>
    </div>
    <DialogFooter>
      <Button type="button" variant="outline" onClick={() => setPublishDesignSystemOpen(false)}>取消</Button>
      <Button type="button" disabled={createDesignSystem.isPending} onClick={() => createDesignSystem.mutate()}>
        {createDesignSystem.isPending ? "发布中..." : "发布"}
      </Button>
    </DialogFooter>
  </DialogContent>
</Dialog>
```

Use existing checkbox component if the design detail file already imports one. Otherwise native checkbox is acceptable for MVP if the surrounding file already uses native controls.

- [ ] **Step 6: Verify manually**

With backend/frontend running:

1. Open an uploaded UI specification design file.
2. Click "发布为设计系统".
3. Fill name and publish as default.
4. Return to design center.
5. Confirm the design system card appears.

---

## Task 7: Verification And Restart

**Files:** no new source files unless tests require fixes.

- [ ] **Step 1: Run backend targeted tests**

Run:

```bash
cd server && go test ./internal/handler -count=1
```

Expected: PASS.

- [ ] **Step 2: Run frontend focused checks**

Run:

```bash
pnpm --filter @multica/core typecheck
pnpm --filter @multica/views typecheck
```

If filtered scripts are unavailable, run:

```bash
pnpm typecheck
```

Expected: no new type errors caused by design-system changes.

- [ ] **Step 3: Run diff checks**

Run:

```bash
git diff --check
```

Expected: no whitespace errors.

- [ ] **Step 4: Rebuild CLI only if daemon or CLI code changed**

This plan should not require daemon binary rebuild unless handler changes affect daemon task context only through server APIs. Rebuild only when CLI/daemon code changes:

```bash
cd server && go build -o /Users/fengyujie/Documents/soyoung/multica/server/bin/multica ./cmd/multica
```

- [ ] **Step 5: Restart services using the agreed commands**

Backend:

```bash
cd /Users/fengyujie/Documents/soyoung/multica/server
set -a && source ../.env && set +a
go run ./cmd/server
```

Frontend:

```bash
cd /Users/fengyujie/Documents/soyoung/multica/apps/web
set -a && source ../../.env && set +a
./node_modules/.bin/next dev --webpack --port "${FRONTEND_PORT:-3031}"
```

Do not use `make dev`, random `5173`, or unrelated startup commands.

- [ ] **Step 6: Manual smoke test**

1. Open `http://localhost:3031/amc/designs`.
2. Select the project that contains the uploaded UI specification.
3. Open the design file that represents the UI specification.
4. Publish it as "CRM 后台设计系统" and set default.
5. Return to design center and confirm it appears in "设计系统".
6. Trigger a UI Agent draft task for an issue in the same project.
7. Inspect latest `agent_task_queue.context` and confirm it contains `design_system`.

SQL check:

```bash
docker exec multica-postgres-dev psql -U multica -d multica -c "
select context->'design_system'
from agent_task_queue
where context->>'type' = 'ui_agent_draft_create'
order by created_at desc
limit 1;
"
```

Expected: JSON object with `id`, `name`, `status`, and `profile`.

---

## Self-Review

Spec coverage:

- Independent Design System asset: Task 1 and Task 2.
- Design center exposure: Task 5.
- Publish from uploaded design file: Task 6.
- Project default design system: Task 1 default index, Task 2 set-default handling, Task 3 context resolution.
- UI Agent reads default profile: Task 3.
- Figma plugin unchanged in first slice: plan uses existing design upload plus detail publish action.
- Full token editor / complete DSL excluded: no task adds those.

Known implementation risk:

- `design_file.go` is already large. This plan follows existing locality for design handlers to reduce routing churn, but after MVP we should consider splitting design-system handlers into a focused file in the same package.

Validation:

- Backend handler tests prove API and context behavior.
- Frontend typecheck and manual smoke test prove the UI path.
- SQL smoke test proves the Agent task actually receives the default profile.
