# Project Design System Creation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user explicitly choose an Agent, generate one project design-system draft from natural-language and optional references, inspect and adjust real design content and an isolated UI Kit, then save it as the project's durable design system.

**Architecture:** Add a project-scoped identity plus two transactional package slots (`draft` and `saved`) without changing the legacy Figma-backed `design_system_profile`. A specialized non-Issue Agent task writes `DESIGN.md`, `tokens.css`, and `components.html` into its daemon-owned output directory; the daemon reads bounded regular files and sends them in the completion callback, and the Server validates and atomically replaces only the draft slot. Shared React views consume a parsed content response, render a CSP-locked UI Kit in a sandboxed iframe, and use a Multica-owned selection bridge for component-scoped natural-language adjustments.

**Tech Stack:** PostgreSQL 17 and sqlc, Go 1.26.1, `goldmark` v1.8.5, `tdewolff/parse/v2` v2.8.14, `golang.org/x/net` v0.57.0, Chi, existing `agent_task_queue` and daemon, React 19, TanStack Query, Zod, Base UI/shadcn, Vitest, local Chrome.

## Global Constraints

- Product authority is `docs/product/design-center/README.md`, decisions `P-008` and `DC-017` through `DC-027`, and `docs/superpowers/specs/2026-07-28-project-design-system-creation-design.md`.
- First-stage states are only `unestablished`, `generating`, `draft`, and `saved`. Do not add review, approval, rejection, publication, revision, or binding concepts.
- The user must explicitly select the executing Agent, even when only one Agent is available. The Server must re-check `service.AgentReadiness` immediately before enqueueing and must never auto-switch Agents.
- Target platform is required and is exactly `web`, `mobile`, or `cross_platform`. The final natural-language brief must be non-empty.
- Figma UI specifications, existing design files, uploads, brand colors, and links are optional reference inputs. Do not add a repository scan or another prerequisite Agent.
- Every successful Agent result contains non-empty `DESIGN.md`, `tokens.css`, and `components.html`; the Server generates `manifest.json` metadata. Agent task status alone is never success.
- The daemon transports file contents after the model exits. Do not place the three files in the model's final text and do not parse them from markdown fences.
- Artifact limits are: `DESIGN.md <= 256 KiB`, `tokens.css <= 256 KiB`, `components.html <= 1 MiB`, aggregate decoded content `<= 1.5 MiB`, and completion HTTP body `<= 2 MiB`.
- Agent HTML cannot contain scripts, event handlers, embedded browsing contexts, forms, imports, or unapproved network URLs. The preview may execute only the fixed Multica selection bridge whose exact source is allowed by a CSP hash.
- The iframe uses `sandbox="allow-scripts"` without `allow-same-origin`, `allow-forms`, `allow-top-navigation`, `allow-popups`, or downloads. Agent-authored scripts remain forbidden.
- The UI displays parsed principles, Tokens, components, states, and UI Kit content. It never exposes internal filenames, a file tree, source editors, or raw package JSON.
- Each project has at most one `project_design_system`. A failed generation or adjustment leaves the prior draft/saved package byte-for-byte intact.
- SQL added to `server/pkg/db/queries/design.sql` must contain no `JOIN` token; the GitLab pre-receive rule rejects generated `design.sql.go` files containing SQL join syntax.
- Migration `129` belongs to existing work. This plan starts at `130` and does not modify, rename, delete, or reset migration `129` or the user's database.
- The worktree is dirty. Never discard unrelated changes. Stage only files owned by the completed task, and use patch staging where a generated file already contains user changes.
- Before editing any existing function, method, or component, run GitNexus upstream impact and warn before HIGH or CRITICAL risk. Before each commit run GitNexus `detect_changes`, focused tests, and `rtk git diff --check`.
- Execute in reviewed batches of at most three tasks. A completion claim must include evidence for task input, Agent artifacts, Server validation, atomic persistence, and actual browser visuals.

## Non-Goals

- No legacy `design_system_profile` migration, deletion, default-profile rewrite, or Figma upload behavior change.
- No design restore, UI draft compiler, Design MCP, repository `DESIGN.md`, community resource catalog, or design-task template integration.
- No native Figma Components/Variants/Auto Layout output.
- No multi-system selection, historical versions, revision browser, design approval role, or pending-review queue.
- No drag canvas, Token form editor, HTML/CSS source editor, or fixed Multica component renderer.

---

## Locked Technical Decisions

### Artifact transport

The specialized task receives `MULTICA_OUTPUT_DIR=<envRoot>/output/project-design-system`. The Agent writes three exact regular files there. After the provider process has exited, the daemon rejects symlinks, missing files, non-regular files, and size violations, then sends a typed `project_design_system_artifacts` object in `/api/daemon/tasks/{id}/complete`. This removes model-output truncation from the artifact path.

### Persistence

PostgreSQL stores bounded text directly. `project_design_system_package` has one row per `slot` (`draft` or `saved`) and a unique `(design_system_id, slot)` constraint. A valid completion upserts only `draft`; save copies draft to saved and deletes draft in one transaction. This is deliberately not a user-visible revision system.

### Preview isolation

The Server validates the Agent HTML/CSS and builds `preview_html` on read. It injects a fixed selection script and a CSP containing the script's SHA-256 hash. The iframe is opaque-origin and only posts stable locator IDs to its parent; the parent accepts messages only from that iframe window and only for IDs present in the Server manifest.

### API namespace

Legacy `/api/design-systems` remains unchanged. The new flow uses `/api/project-design-systems` so installed clients and Figma-backed Profile callers keep their existing contract.

---

## Files And Responsibilities

- Modify `server/go.mod` and `server/go.sum`: add structured Markdown, CSS, and HTML parsers.
- Create `server/internal/projectdesignsystem/{types,markdown,tokens,html,validate,preview}.go`: bounded package validation, parsed display model, digest/manifest creation, and trusted preview shell.
- Create `server/internal/projectdesignsystem/*_test.go` plus `testdata/valid/*`: domain and security fixtures.
- Create `server/migrations/130_project_design_system.up.sql` and `.down.sql`: one project identity and draft/saved package slots.
- Modify `server/pkg/db/queries/design.sql` and regenerate `server/pkg/db/generated/{design.sql.go,models.go}`: identity, package-slot, active-task, save, and task-history queries without joins.
- Create `server/internal/handler/project_design_system.go`: lookup, create/generate, adjust, regenerate, save, reference snapshotting, response assembly, and domain events.
- Create `server/internal/handler/project_design_system_test.go`: authorization, validation, explicit Agent, project isolation, state, and transaction tests.
- Modify `server/cmd/server/router.go` and `server/cmd/server/integration_test.go`: authenticated routes and route coverage.
- Modify `server/internal/service/task.go`: specialized context model and failure/cancellation synchronization.
- Modify `server/internal/handler/daemon.go`: claim transport and artifact-aware completion mutation.
- Modify `server/internal/daemon/{types,prompt,daemon,client}.go`: task transport, prompt, output-directory injection, bounded artifact collection, and completion payload.
- Modify `server/internal/daemon/execenv/{execenv,context,runtime_config}.go`: output path and task/base-package context files.
- Modify corresponding daemon, execenv, service, and handler tests.
- Modify `server/pkg/protocol/events.go`, `packages/core/types/events.ts`, and `packages/core/realtime/use-realtime-sync.ts`: `project_design_system:changed` invalidation.
- Modify `packages/core/types/design.ts`, `packages/core/api/{client,schemas}.ts`, `packages/core/designs/{keys,queries}.ts`, and tests: typed, drift-tolerant client contract.
- Modify `packages/core/paths/{paths,paths.test}.ts`: shared detail path.
- Create `packages/views/designs/project-design-system-create.tsx`: explicit Agent, platform, brief, and optional references.
- Create `packages/views/designs/project-design-system-page.tsx`: content-first details, task state, adjustment, regenerate, and save.
- Create `packages/views/designs/project-design-system-preview.tsx`: isolated UI Kit and trusted locator selection.
- Create colocated view tests and modify `packages/views/designs/{designs-page,index}.tsx`.
- Create `apps/web/app/[workspaceSlug]/(dashboard)/designs/systems/[id]/page.tsx` and modify `apps/desktop/src/renderer/src/routes.tsx`.
- Create `docs/product/design-center/project-design-system-validation.md` only after live validation, containing IDs, digests, screenshots, failure injection, and remaining gaps.

---

### Task 1: Validate And Render A Project Design-System Package

**Files:**
- Modify: `server/go.mod`
- Modify: `server/go.sum`
- Create: `server/internal/projectdesignsystem/types.go`
- Create: `server/internal/projectdesignsystem/markdown.go`
- Create: `server/internal/projectdesignsystem/tokens.go`
- Create: `server/internal/projectdesignsystem/html.go`
- Create: `server/internal/projectdesignsystem/validate.go`
- Create: `server/internal/projectdesignsystem/preview.go`
- Create: `server/internal/projectdesignsystem/validate_test.go`
- Create: `server/internal/projectdesignsystem/preview_test.go`
- Create: `server/internal/projectdesignsystem/testdata/valid/DESIGN.md`
- Create: `server/internal/projectdesignsystem/testdata/valid/tokens.css`
- Create: `server/internal/projectdesignsystem/testdata/valid/components.html`

**Interfaces:**
- Consumes: exact three-file `ArtifactInput` and allowed CDN hosts.
- Produces:

```go
package projectdesignsystem

const SchemaVersion = "multica.project-design-system/v1"

type ArtifactInput struct {
    DesignMD      string `json:"design_md"`
    TokensCSS     string `json:"tokens_css"`
    ComponentsHTML string `json:"components_html"`
}

type Section struct {
    ID       string `json:"id"`
    Title    string `json:"title"`
    Markdown string `json:"markdown"`
}

type TokenValue struct {
    Name  string `json:"name"`
    Value string `json:"value"`
}

type TokenGroup struct {
    ID     string       `json:"id"`
    Label  string       `json:"label"`
    Tokens []TokenValue `json:"tokens"`
}

type Locator struct {
    ID    string `json:"id"`
    Kind  string `json:"kind"` // component or block
    Label string `json:"label"`
}

type FileManifest struct {
    SizeBytes int    `json:"size_bytes"`
    SHA256    string `json:"sha256"`
    MediaType string `json:"media_type"`
}

type Manifest struct {
    SchemaVersion string                  `json:"schema_version"`
    Digest        string                  `json:"digest"`
    Files         map[string]FileManifest `json:"files"`
    Sections      []Section               `json:"sections"`
    TokenGroups   []TokenGroup            `json:"token_groups"`
    Locators      []Locator               `json:"locators"`
}

type ValidationReport struct {
    Passed      bool         `json:"passed"`
    Diagnostics []Diagnostic `json:"diagnostics"`
}

type ValidatedPackage struct {
    Artifacts  ArtifactInput
    Manifest   Manifest
    Validation ValidationReport
}

func Validate(input ArtifactInput, allowedHosts []string) (ValidatedPackage, error)
func BuildPreviewHTML(pkg ValidatedPackage, allowedHosts []string) string
```

- [ ] **Step 1: Write failing package and security tests**

Add these exact tests:

```go
func TestValidateAcceptsCoherentPackage(t *testing.T)
func TestValidateRejectsMissingAndOversizedFiles(t *testing.T)
func TestValidateExtractsDynamicMarkdownSections(t *testing.T)
func TestValidateRejectsMalformedCSSAndUnknownVariables(t *testing.T)
func TestValidateGroupsOnlyDeclaredCustomProperties(t *testing.T)
func TestValidateRejectsScriptEventsImportsAndUnsafeURLs(t *testing.T)
func TestValidateRequiresUniqueStableLocators(t *testing.T)
func TestValidateRequiresVisibleUIKitContentAndTokenUsage(t *testing.T)
func TestBuildPreviewHTMLAllowsOnlyTrustedSelectionBridge(t *testing.T)
```

The valid HTML fixture must contain at least one `data-design-node-id`, one `component`, one `block`, visible text, and CSS variable usage:

```html
<main class="showcase" data-design-node-id="overview" data-design-node-kind="block" data-design-node-label="Overview">
  <button type="button" class="primary" data-design-node-id="button-primary" data-design-node-kind="component" data-design-node-label="Primary button">Create customer</button>
</main>
```

Reject `<script>`, any `on*` attribute, `<iframe>`, `<object>`, `<embed>`, `<base>`, `<link>`, `<meta>`, `<form>`, `srcdoc`, `javascript:`, CSS `@import`, undeclared `var(--token)`, duplicate locator IDs, non-fragment anchors, and remote hosts outside `allowedHosts`.

- [ ] **Step 2: Run RED tests**

Workdir: `server`

```bash
rtk go test ./internal/projectdesignsystem -count=1 -v
```

Expected: FAIL because the package does not exist.

- [ ] **Step 3: Add parser dependencies**

Workdir: `server`

```bash
rtk go get github.com/yuin/goldmark@v1.8.5
rtk go get github.com/tdewolff/parse/v2@v2.8.14
rtk go get golang.org/x/net@v0.57.0
```

Use Goldmark AST headings for Markdown, `tdewolff/parse/v2/css` tokens for declarations/references/URLs, and `golang.org/x/net/html` nodes for HTML. Regex may validate locator ID syntax only; it must not parse or sanitize HTML/CSS.

- [ ] **Step 4: Implement bounded validation and deterministic manifest generation**

Enforce the exact limits from Global Constraints before parsing. Normalize line endings only for digesting and storage; do not rewrite content. Compute the package digest from three length-prefixed byte sequences in filename order so concatenation cannot collide:

```go
func packageDigest(input ArtifactInput) string {
    h := sha256.New()
    writePart(h, "DESIGN.md", []byte(input.DesignMD))
    writePart(h, "tokens.css", []byte(input.TokensCSS))
    writePart(h, "components.html", []byte(input.ComponentsHTML))
    return hex.EncodeToString(h.Sum(nil))
}
```

`DESIGN.md` must have a title or at least one H2 section. `tokens.css` must declare semantic custom properties and every `var()` reference must resolve. `components.html` must use at least one declared Token and contain at least one visible locator.

- [ ] **Step 5: Build the trusted preview shell**

Inject Tokens as a trusted `<style>`, Agent HTML as validated body markup, and one constant selection bridge. The bridge must post only:

```js
parent.postMessage({type: "multica:project-design-system-select", id: node.dataset.designNodeId}, "*")
```

Generate `script-src` from the exact SHA-256 of that constant. CSP must use `default-src 'none'`, allow inline style, allow images/fonts only for approved CDN hosts and `data:` images, and block connect/frame/object/base/form navigation.

- [ ] **Step 6: Run GREEN and security tests**

Workdir: `server`

```bash
rtk gofmt -w internal/projectdesignsystem
rtk go test ./internal/projectdesignsystem -count=1 -v
rtk go test ./internal/middleware -run ContentSecurityPolicy -count=1
```

Expected: all pass; preview tests prove Agent script text never enters the allowed script hash.

- [ ] **Step 7: Commit the domain boundary**

```bash
rtk git add server/go.mod server/go.sum server/internal/projectdesignsystem
rtk git commit -m "feat(design): validate project design system packages"
```

---

### Task 2: Persist One Project Identity And Draft/Saved Slots

**Files:**
- Create: `server/migrations/130_project_design_system.up.sql`
- Create: `server/migrations/130_project_design_system.down.sql`
- Modify: `server/pkg/db/queries/design.sql`
- Regenerate: `server/pkg/db/generated/design.sql.go`
- Regenerate: `server/pkg/db/generated/models.go`
- Create: `server/internal/handler/project_design_system_persistence_test.go`

**Interfaces:**
- Consumes: validated package JSON and task/project UUIDs.
- Produces: sqlc methods named in this task; no query contains `JOIN`.

- [ ] **Step 1: Write failing persistence tests**

Add database tests proving:

```go
func TestProjectDesignSystemProjectUniqueness(t *testing.T)
func TestProjectDesignSystemDraftUpsertLeavesSavedUntouched(t *testing.T)
func TestSaveProjectDesignSystemCopiesDraftAndDeletesDraftAtomically(t *testing.T)
func TestProjectDesignSystemWorkspaceIsolation(t *testing.T)
func TestProjectDesignSystemPackageRejectsInvalidSlot(t *testing.T)
```

- [ ] **Step 2: Create migration 130**

Use this physical boundary:

```sql
CREATE TABLE project_design_system (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name text NOT NULL,
    platform text NOT NULL CHECK (platform IN ('web', 'mobile', 'cross_platform')),
    current_agent_id uuid REFERENCES agent(id) ON DELETE SET NULL,
    active_task_id uuid REFERENCES agent_task_queue(id) ON DELETE SET NULL,
    active_operation text CHECK (active_operation IS NULL OR active_operation IN ('generate', 'adjust', 'regenerate')),
    input_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
    last_error jsonb,
    created_by uuid REFERENCES "user"(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    saved_at timestamptz,
    UNIQUE (workspace_id, project_id)
);

CREATE TABLE project_design_system_package (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    design_system_id uuid NOT NULL REFERENCES project_design_system(id) ON DELETE CASCADE,
    slot text NOT NULL CHECK (slot IN ('draft', 'saved')),
    design_md text NOT NULL,
    tokens_css text NOT NULL,
    components_html text NOT NULL,
    manifest jsonb NOT NULL,
    validation jsonb NOT NULL,
    integrity_sha256 text NOT NULL,
    source_task_id uuid REFERENCES agent_task_queue(id) ON DELETE SET NULL,
    agent_id uuid REFERENCES agent(id) ON DELETE SET NULL,
    instruction text,
    scope jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (design_system_id, slot)
);
```

The down migration drops package first, then identity. Do not touch legacy tables.

- [ ] **Step 3: Add no-JOIN sqlc queries**

Add exact query responsibilities:

```sql
-- name: GetProjectDesignSystemByProject :one
-- name: GetProjectDesignSystemInWorkspace :one
-- name: CreateProjectDesignSystem :one
-- name: UpdateProjectDesignSystemInputAndTask :one
-- name: ClearProjectDesignSystemActiveTask :one
-- name: SetProjectDesignSystemFailure :one
-- name: GetProjectDesignSystemPackageBySlot :one
-- name: UpsertProjectDesignSystemPackage :one
-- name: SaveProjectDesignSystemDraft :one
-- name: DeleteProjectDesignSystemPackageSlot :exec
-- name: ListProjectDesignSystemTasks :many
```

`SaveProjectDesignSystemDraft` must be one `INSERT ... SELECT ... ON CONFLICT ... DO UPDATE ... RETURNING` statement. The caller deletes the draft and updates `saved_at` in the same Go transaction. `ListProjectDesignSystemTasks` filters `agent_task_queue.context->>'project_design_system_id'` and orders newest first; it does not join agents.

- [ ] **Step 4: Regenerate and prove the generated query is policy-compliant**

```bash
rtk make sqlc
rtk rg -n "JOIN" server/pkg/db/generated/design.sql.go
```

Expected: the second command produces no output attributable to new queries. Do not overwrite unrelated generated-file changes; inspect the diff before staging.

- [ ] **Step 5: Run migration and persistence tests against the test database**

Workdir: `server`

```bash
rtk go test ./internal/handler -run '^TestProjectDesignSystem(ProjectUniqueness|DraftUpsert|Save|WorkspaceIsolation|PackageRejects)' -count=1 -v
```

Expected: all pass without resetting the user's development database.

- [ ] **Step 6: Commit only schema-owned changes**

Run GitNexus `detect_changes` first. If generated files contain pre-existing unstaged hunks, use `rtk git add -p` and confirm the staged diff contains the complete generated definitions required by migration 130 and nothing else.

```bash
rtk git commit -m "feat(design): persist project design system drafts"
```

---

### Task 3: Add Project Design-System API And Explicit Task Dispatch

**Files:**
- Create: `server/internal/handler/project_design_system.go`
- Create: `server/internal/handler/project_design_system_test.go`
- Modify: `server/internal/service/task.go`
- Modify: `server/internal/service/task_test.go`
- Modify: `server/cmd/server/router.go`
- Modify: `server/cmd/server/integration_test.go`

**Interfaces:**
- Consumes: project membership, `service.AgentReadiness`, project metadata, selected references, and Task 2 queries.
- Produces:

```go
const ProjectDesignSystemTaskContextType = "project_design_system_task"

type ProjectDesignSystemOperation string

const (
    ProjectDesignSystemGenerate   ProjectDesignSystemOperation = "generate"
    ProjectDesignSystemAdjust     ProjectDesignSystemOperation = "adjust"
    ProjectDesignSystemRegenerate ProjectDesignSystemOperation = "regenerate"
)

type ProjectDesignSystemTaskContext struct {
    Type                  string                       `json:"type"`
    Operation             ProjectDesignSystemOperation `json:"operation"`
    RequesterID           string                       `json:"requester_id"`
    WorkspaceID           string                       `json:"workspace_id"`
    ProjectID             string                       `json:"project_id"`
    ProjectDesignSystemID string                       `json:"project_design_system_id"`
    AgentID               string                       `json:"agent_id"`
    Project               json.RawMessage              `json:"project"`
    Platform              string                       `json:"platform"`
    Brief                 string                       `json:"brief"`
    References            json.RawMessage              `json:"references"`
    BasePackage           json.RawMessage              `json:"base_package,omitempty"`
    Instruction           string                       `json:"instruction,omitempty"`
    Scope                 json.RawMessage              `json:"scope,omitempty"`
    OutputPolicy          json.RawMessage              `json:"output_policy"`
}
```

HTTP routes:

```text
GET  /api/project-design-systems?project_id={uuid}
GET  /api/project-design-systems/{id}
POST /api/project-design-systems
POST /api/project-design-systems/{id}/adjust
POST /api/project-design-systems/{id}/regenerate
POST /api/project-design-systems/{id}/save
```

- [ ] **Step 1: Write failing request and state tests**

Add exact coverage:

```go
func TestCreateProjectDesignSystemRequiresExplicitReadyAgent(t *testing.T)
func TestCreateProjectDesignSystemRequiresPlatformAndBrief(t *testing.T)
func TestCreateProjectDesignSystemSnapshotsExactInputs(t *testing.T)
func TestCreateProjectDesignSystemRejectsSecondSystem(t *testing.T)
func TestGetProjectDesignSystemReturnsUnestablishedAfterFailedFirstRun(t *testing.T)
func TestAdjustProjectDesignSystemValidatesScopeAgainstManifest(t *testing.T)
func TestRegenerateProjectDesignSystemPreservesCurrentPackage(t *testing.T)
func TestSaveProjectDesignSystemRequiresValidatedDraft(t *testing.T)
func TestProjectDesignSystemRoutesRejectForeignWorkspace(t *testing.T)
```

Assert that a workspace with one Agent still rejects a request with no `agent_id`. Assert an offline runtime returns `409` with a stable error code and does not alter `input_snapshot`.

- [ ] **Step 2: Define request and response contracts**

Use these request shapes:

```go
type ProjectDesignSystemReferenceInput struct {
    Kind                  string `json:"kind"` // attachment, brand_color, design_file, design_system_profile, link
    AttachmentID          string `json:"attachment_id,omitempty"`
    DesignFileID          string `json:"design_file_id,omitempty"`
    DesignSystemProfileID string `json:"design_system_profile_id,omitempty"`
    Value                 string `json:"value,omitempty"`
    Label                 string `json:"label,omitempty"`
}

type CreateProjectDesignSystemRequest struct {
    ProjectID  string                              `json:"project_id"`
    AgentID    string                              `json:"agent_id"`
    Platform   string                              `json:"platform"`
    Brief      string                              `json:"brief"`
    References []ProjectDesignSystemReferenceInput `json:"references"`
}

type ProjectDesignSystemScope struct {
    Kind string `json:"kind"` // all, section, token_group, component, block
    ID   string `json:"id,omitempty"`
}

type AdjustProjectDesignSystemRequest struct {
    AgentID     string                   `json:"agent_id"`
    Instruction string                   `json:"instruction"`
    Scope       ProjectDesignSystemScope `json:"scope"`
}
```

Return a content-first object with `status`, `active_task`, `input_snapshot`, `content.sections`, `content.token_groups`, `content.preview_html`, `content.locators`, `has_unsaved_changes`, `last_error`, and recent task-derived activity. Do not return raw filenames or a file map to frontend callers.

- [ ] **Step 3: Resolve and freeze references**

Resolve every ID inside the same workspace/project. Snapshot only bounded, Agent-useful facts:

- attachment: ID, filename, content type, CDN URL;
- brand color: normalized CSS color plus label;
- design file: ID, title, thumbnail URL, current revision ID, frame names and preview URLs;
- legacy UI specification: ID, name, source revision ID, analyzed `profile_json`;
- link: HTTPS URL plus label.

Reject unknown kinds, foreign resources, non-HTTPS links, more than 20 references, or a serialized snapshot over 512 KiB. User brief wins when references conflict.

- [ ] **Step 4: Enqueue atomically with explicit Agent readiness**

Use `service.AgentReadiness(ctx, h.Queries, agent)` immediately before `CreateQuickCreateTask`. Inside one transaction: lock project, create/update the identity, persist the exact input snapshot, create the Agent task, and set `active_task_id` plus operation. Call `TaskService.NotifyTaskEnqueued` only after commit.

Do not auto-select by Agent name, ID history, list position, or candidate count.

- [ ] **Step 5: Implement read, adjust, regenerate, and save semantics**

Derive status in this order:

```go
switch {
case system.ActiveTaskID.Valid:
    status = "generating"
case draft != nil:
    status = "draft"
case saved != nil:
    status = "saved"
default:
    status = "unestablished"
}
```

Adjustment context includes the complete current package (draft first, saved fallback), selected stable scope, and instruction. Regenerate uses current creation inputs plus user replacements and keeps current content visible. Save copies draft to saved and deletes draft transactionally; it does not create or complete an Agent task.

- [ ] **Step 6: Register routes and run handler/integration tests**

Workdir: `server`

```bash
rtk gofmt -w internal/handler/project_design_system.go internal/handler/project_design_system_test.go internal/service/task.go internal/service/task_test.go cmd/server/router.go cmd/server/integration_test.go
rtk go test ./internal/handler -run 'ProjectDesignSystem' -count=1 -v
rtk go test ./internal/service -run 'ProjectDesignSystem' -count=1 -v
rtk go test ./cmd/server -run 'ProjectDesignSystem' -count=1 -v
```

- [ ] **Step 7: Commit the synchronous API and dispatch boundary**

```bash
rtk git add server/internal/handler/project_design_system.go server/internal/handler/project_design_system_test.go server/internal/service/task.go server/internal/service/task_test.go server/cmd/server/router.go server/cmd/server/integration_test.go
rtk git commit -m "feat(design): dispatch project design system tasks"
```

---

### Task 4: Give The Agent A File-Based Design-System Workspace

**Files:**
- Modify: `server/internal/daemon/types.go`
- Modify: `server/internal/daemon/prompt.go`
- Modify: `server/internal/daemon/prompt_test.go`
- Modify: `server/internal/daemon/daemon.go`
- Modify: `server/internal/daemon/daemon_test.go`
- Modify: `server/internal/daemon/execenv/execenv.go`
- Modify: `server/internal/daemon/execenv/execenv_test.go`
- Modify: `server/internal/daemon/execenv/context.go`
- Modify: `server/internal/daemon/execenv/runtime_config.go`
- Modify: corresponding execenv tests.
- Modify: `server/internal/handler/daemon.go`
- Modify: `server/internal/handler/daemon_test.go`

**Interfaces:**
- Consumes: `ProjectDesignSystemTaskContext` in claim response.
- Produces: task workspace files and `MULTICA_OUTPUT_DIR`.

- [ ] **Step 1: Write failing claim, context, prompt, and environment tests**

Add:

```go
func TestClaimProjectDesignSystemTaskReturnsExactContext(t *testing.T)
func TestProjectDesignSystemContextWritesTaskAndBasePackageFiles(t *testing.T)
func TestBuildPromptProjectDesignSystemGenerate(t *testing.T)
func TestBuildPromptProjectDesignSystemAdjustRequiresCompleteReplacement(t *testing.T)
func TestRunTaskExportsProjectDesignSystemOutputDir(t *testing.T)
func TestGCMetaRecognizesProjectDesignSystemTask(t *testing.T)
```

The claim test must prove the chosen Agent ID, project ID, platform, brief, references, scope, and base package survive Server to daemon unchanged.

- [ ] **Step 2: Extend claim and daemon task types**

Add `ProjectDesignSystemContext json.RawMessage` to handler response and daemon `Task`. Detect the context discriminator in `ClaimTaskByRuntime`, set workspace/project/title, and do not inject project repositories or trigger a repository checkout for this task type.

Update workspace-isolation diagnostics and `gcMetaForTask`; use the existing task-ID GC path.

- [ ] **Step 3: Materialize task context without bloating the prompt**

Write:

```text
.agent_context/project_design_system/task.json
.agent_context/project_design_system/base/DESIGN.md
.agent_context/project_design_system/base/tokens.css
.agent_context/project_design_system/base/components.html
```

Base files exist only for adjust/regenerate. `task.json` omits the embedded base contents after extracting them so the model does not receive duplicate megabytes. Track all sidecars in the existing cleanup manifest.

- [ ] **Step 4: Expose a dedicated output path**

Add `OutputDir` to `execenv.Environment`, create `<envRoot>/output/project-design-system`, and inject its absolute path as `MULTICA_OUTPUT_DIR`. Block custom Agent environment variables from overriding this key.

- [ ] **Step 5: Build the specialized designer prompt**

The prompt must say, in substance and tests, all of the following:

```text
Read .agent_context/project_design_system/task.json first.
For adjust/regenerate, read all three files under base/ before designing.
Treat the user brief as the primary intent and references as evidence.
Use Open Design's stable Token layers; do not invent unsupported project facts merely to fill a catalog.
Create one coherent direction, not multiple alternatives or a demo switcher.
components.html is a real static UI Kit using tokens.css, with project-relevant components, states, and representative compositions.
Every selectable component/block has unique data-design-node-id, data-design-node-kind, and data-design-node-label attributes.
Never write scripts, event attributes, imports, forms, external embeds, or arbitrary remote resources.
For adjustment, return a complete mutually consistent replacement of all three files even when scope is local.
Write exact files to $MULTICA_OUTPUT_DIR/DESIGN.md, tokens.css, and components.html.
Do not paste file contents into the final response; report only a short completion summary.
```

The Agent must not modify a repository, call Figma, upload a design file, or mark success without writing all files.

- [ ] **Step 6: Run transport and prompt tests**

Workdir: `server`

```bash
rtk gofmt -w internal/handler/daemon.go internal/handler/daemon_test.go internal/daemon internal/daemon/execenv
rtk go test ./internal/handler -run 'ClaimProjectDesignSystem' -count=1 -v
rtk go test ./internal/daemon -run 'ProjectDesignSystem|GCMeta' -count=1 -v
rtk go test ./internal/daemon/execenv -run 'ProjectDesignSystem|OutputDir' -count=1 -v
```

- [ ] **Step 7: Commit task workspace transport**

```bash
rtk git add server/internal/handler/daemon.go server/internal/handler/daemon_test.go server/internal/daemon
rtk git commit -m "feat(design): prepare design system agent workspaces"
```

---

### Task 5: Collect Files And Atomically Complete Or Fail The Task

**Files:**
- Modify: `server/internal/daemon/types.go`
- Modify: `server/internal/daemon/client.go`
- Modify: `server/internal/daemon/client_test.go`
- Modify: `server/internal/daemon/daemon.go`
- Modify: `server/internal/daemon/daemon_test.go`
- Modify: `server/internal/handler/daemon.go`
- Modify: `server/internal/handler/daemon_test.go`
- Modify: `server/internal/service/task.go`
- Modify: `server/internal/service/task_test.go`

**Interfaces:**
- Consumes: three daemon output files and Task 1 validator.
- Produces:

```go
type ProjectDesignSystemArtifacts struct {
    DesignMD       string `json:"design_md"`
    TokensCSS      string `json:"tokens_css"`
    ComponentsHTML string `json:"components_html"`
}

type TaskCompleteRequest struct {
    PRURL                        string                        `json:"pr_url"`
    Output                       string                        `json:"output"`
    SessionID                    string                        `json:"session_id"`
    WorkDir                      string                        `json:"work_dir"`
    ProjectDesignSystemArtifacts *ProjectDesignSystemArtifacts `json:"project_design_system_artifacts,omitempty"`
}
```

- [ ] **Step 1: Write failing daemon file-safety tests**

```go
func TestReadProjectDesignSystemArtifactsAcceptsExactRegularFiles(t *testing.T)
func TestReadProjectDesignSystemArtifactsRejectsMissingFile(t *testing.T)
func TestReadProjectDesignSystemArtifactsRejectsSymlink(t *testing.T)
func TestReadProjectDesignSystemArtifactsRejectsNonRegularAndOversizedFile(t *testing.T)
func TestCompletedProjectDesignSystemWithoutArtifactsBecomesBlocked(t *testing.T)
```

Use `Lstat`, resolved-path containment, and bounded reads. The provider process is already stopped before collection.

- [ ] **Step 2: Attach artifacts only to specialized completions**

Add an internal artifact pointer to `TaskResult`. On a completed specialized run, collect files before `reportTaskResult`; collection failure changes the result to `blocked` with failure reason `project_design_system_artifacts_invalid`. Other task kinds keep the current payload exactly.

Extend `Client.CompleteTask` to serialize the optional typed field. Keep terminal retry behavior unchanged.

- [ ] **Step 3: Write failing Server completion tests**

```go
func TestCompleteProjectDesignSystemTaskCreatesValidatedDraft(t *testing.T)
func TestCompleteProjectDesignSystemTaskRejectsMissingArtifactPayload(t *testing.T)
func TestCompleteProjectDesignSystemTaskRejectsUnsafeHTMLWithoutReplacingDraft(t *testing.T)
func TestCompleteProjectDesignSystemAdjustmentReplacesAllFilesAtomically(t *testing.T)
func TestCompleteProjectDesignSystemTaskRejectsMismatchedActiveTask(t *testing.T)
func TestProjectDesignSystemFailureAndCancellationPreserveExistingPackage(t *testing.T)
```

Capture old draft/saved digests before invalid completion and assert they are identical afterward. A task row becoming `completed` without a package must be impossible.

- [ ] **Step 4: Validate before the completion transaction**

For specialized context only:

1. require artifact payload;
2. call `projectdesignsystem.Validate` with both configured storage CDN hosts;
3. verify context Agent/project/system IDs and the identity's `active_task_id` match this task;
4. marshal Server manifest and validation report;
5. call `CompleteTaskWithMutation`;
6. inside the mutation lock the project, upsert the draft slot, clear active task/error, and preserve saved;
7. publish `project_design_system:changed` only after commit.

If validation fails, call `FailTask` with `project_design_system_invalid_artifacts`; do not call `CompleteTask` first.

- [ ] **Step 5: Synchronize every failure and cancellation path**

Add `markProjectDesignSystemTaskFailed` beside existing specialized task synchronization. It clears active task only when `active_task_id == failed task ID`, writes a structured `last_error`, and never touches package rows. Call it from single failure, cancellation, and bulk sweeper failure paths.

- [ ] **Step 6: Limit completion bodies without breaking legacy tasks**

Wrap the completion request body with `http.MaxBytesReader` at 2 MiB. Add a regression test showing a normal existing task completion remains accepted and an oversized specialized payload receives `413` and leaves its task non-completed.

- [ ] **Step 7: Run completion, rollback, and regression tests**

Workdir: `server`

```bash
rtk gofmt -w internal/daemon internal/handler/daemon.go internal/handler/daemon_test.go internal/service/task.go internal/service/task_test.go
rtk go test ./internal/daemon -run 'ProjectDesignSystem|CompleteTask' -count=1 -v
rtk go test ./internal/handler -run 'CompleteProjectDesignSystem|ProjectDesignSystemFailure' -count=1 -v
rtk go test ./internal/service -run 'ProjectDesignSystem' -count=1 -v
```

- [ ] **Step 8: Commit artifact completion**

```bash
rtk git add server/internal/daemon server/internal/handler/daemon.go server/internal/handler/daemon_test.go server/internal/service/task.go server/internal/service/task_test.go
rtk git commit -m "feat(design): persist validated agent design systems"
```

---

### Task 6: Add Drift-Tolerant Frontend Contracts And Realtime Refresh

**Files:**
- Modify: `packages/core/types/design.ts`
- Modify: `packages/core/types/events.ts`
- Modify: `packages/core/api/schemas.ts`
- Modify: `packages/core/api/schemas.test.ts`
- Modify: `packages/core/api/client.ts`
- Modify: `packages/core/api/client.test.ts`
- Modify: `packages/core/designs/keys.ts`
- Modify: `packages/core/designs/keys.test.ts`
- Modify: `packages/core/designs/queries.ts`
- Modify: `packages/core/realtime/use-realtime-sync.ts`
- Modify: `packages/core/realtime/use-realtime-sync-ws-instance.test.tsx`
- Modify: `packages/core/paths/paths.ts`
- Modify: `packages/core/paths/paths.test.ts`
- Modify: `server/pkg/protocol/events.go`

**Interfaces:**
- Consumes: Task 3 API response and `project_design_system:changed` event.
- Produces: `projectDesignSystemByProjectOptions`, `projectDesignSystemDetailOptions`, mutation methods, and `paths.projectDesignSystemDetail(id)`.

- [ ] **Step 1: Write failing schema, key, path, and realtime tests**

Test malformed responses with missing arrays, wrong status strings, null content, and malformed locators. Fallback must render an empty/unestablished state instead of throwing. Test event invalidation of both project lookup and detail keys.

- [ ] **Step 2: Define frontend types**

```ts
export type ProjectDesignSystemStatus = "unestablished" | "generating" | "draft" | "saved";
export type ProjectDesignSystemPlatform = "web" | "mobile" | "cross_platform";
export type ProjectDesignSystemScope =
  | { kind: "all" }
  | { kind: "section" | "token_group" | "component" | "block"; id: string };

export interface ProjectDesignSystemContent {
  sections: Array<{ id: string; title: string; markdown: string }>;
  token_groups: Array<{ id: string; label: string; tokens: Array<{ name: string; value: string }> }>;
  locators: Array<{ id: string; kind: "component" | "block"; label: string }>;
  preview_html: string;
  integrity_sha256: string;
}
```

Use Zod `.loose()`, default arrays to `[]`, accept unknown status strings at the boundary, and map unknown status to `unestablished` in UI derivation.

- [ ] **Step 3: Add API methods and React Query keys**

```ts
api.getProjectDesignSystemForProject(projectId)
api.getProjectDesignSystem(id)
api.createProjectDesignSystem(input)
api.adjustProjectDesignSystem(id, input)
api.regenerateProjectDesignSystem(id, input)
api.saveProjectDesignSystem(id)
```

All responses cross `parseWithFallback`. Keys are workspace-scoped and include project/system ID.

- [ ] **Step 4: Add path and domain event**

Path:

```ts
projectDesignSystemDetail: (id: string) => `${ws}/designs/systems/${encode(id)}`
```

Event payload contains `project_design_system_id`, `project_id`, and `status`. Realtime sync invalidates the selected project's lookup and the specific system detail. Do not add polling or copy Server state into Zustand.

- [ ] **Step 5: Run core tests and typecheck**

```bash
rtk pnpm --filter @multica/core exec vitest run api/schemas.test.ts api/client.test.ts designs/keys.test.ts paths/paths.test.ts realtime/use-realtime-sync-ws-instance.test.tsx
rtk pnpm --filter @multica/core typecheck
```

- [ ] **Step 6: Commit the frontend contract**

```bash
rtk git add packages/core server/pkg/protocol/events.go
rtk git commit -m "feat(design): expose project design system client contract"
```

---

### Task 7: Replace The UI Specification Tab With The Project Design-System Entry

**Files:**
- Create: `packages/views/designs/project-design-system-create.tsx`
- Create: `packages/views/designs/project-design-system-create.test.tsx`
- Modify: `packages/views/designs/designs-page.tsx`
- Modify: `packages/views/designs/designs-page.test.tsx`

**Interfaces:**
- Consumes: selected project, project description, Agents/runtimes, existing design files, legacy Profiles, upload API, and Task 6 hooks.
- Produces: empty/create/generating/summary states for the `设计体系` tab.

- [ ] **Step 1: Write failing design-center workflow tests**

```ts
it("shows an honest unestablished state and does not auto-create")
it("never auto-selects the only agent")
it("requires platform, agent, and non-empty final brief")
it("preserves every field when the selected agent becomes unavailable")
it("submits exact project, agent, platform, brief, and references")
it("switching projects never reuses another project's form or system")
it("opens the existing project design system instead of creating a second one")
```

- [ ] **Step 2: Change only the third design-center asset entry**

The top-level entries become exactly:

```text
设计稿 | 模版 | 设计体系
```

The `systems` tab stops listing legacy Figma Profile cards. Legacy Profiles remain queryable only as optional creation references. Search copy becomes `搜索设计体系…`; do not display `默认 UI 规范`, Profile count, or Figma-upload instructions as the tab's main state.

- [ ] **Step 3: Build one unframed aggregate creation surface**

Use stable sections, not nested cards:

- explicit Agent picker with availability and no default selection;
- platform segmented control with Web, 移动端, 跨端;
- textarea seeded from the current project's description but editable before submit;
- optional reference controls for upload, brand-color swatch/input, HTTPS link, project design files, and legacy Figma UI specifications;
- primary command `生成设计体系`.

Use icons for add/remove/upload operations and tooltips for unfamiliar icons. Keep the user's form state keyed by project ID in component state; do not persist Server data in Zustand.

- [ ] **Step 4: Render factual generation and existing-system states**

During generation, show `准备上下文`, `智能体生成`, or `产物校验` only when returned by the API; no fake percentage. On failure, restore the creation surface with previous input and actionable `last_error`. On draft/saved, show one project system summary and open `paths.projectDesignSystemDetail(id)`.

- [ ] **Step 5: Run view tests and package typecheck**

```bash
rtk pnpm --filter @multica/views exec vitest run designs/project-design-system-create.test.tsx designs/designs-page.test.tsx
rtk pnpm --filter @multica/views typecheck
```

- [ ] **Step 6: Commit the design-center entry**

```bash
rtk git add packages/views/designs/project-design-system-create.tsx packages/views/designs/project-design-system-create.test.tsx packages/views/designs/designs-page.tsx packages/views/designs/designs-page.test.tsx
rtk git commit -m "feat(design): add project design system creation UI"
```

---

### Task 8: Render Content-First Details And A Selectable Isolated UI Kit

**Files:**
- Create: `packages/views/designs/project-design-system-page.tsx`
- Create: `packages/views/designs/project-design-system-page.test.tsx`
- Create: `packages/views/designs/project-design-system-preview.tsx`
- Create: `packages/views/designs/project-design-system-preview.test.tsx`
- Modify: `packages/views/designs/index.ts`
- Create: `apps/web/app/[workspaceSlug]/(dashboard)/designs/systems/[id]/page.tsx`
- Modify: `apps/desktop/src/renderer/src/routes.tsx`

**Interfaces:**
- Consumes: `ProjectDesignSystemContent`, selected scope, Agent availability, and Task 6 mutations.
- Produces: Web/Desktop shared detail route, locator selection, adjustment, regenerate, and save.

- [ ] **Step 1: Write failing content and security tests**

```ts
it("renders dynamic DESIGN sections and omits absent categories")
it("renders actual token groups without exposing source filenames")
it("uses sandbox allow-scripts without same-origin forms navigation or popups")
it("accepts locator messages only from its own iframe and known IDs")
it("shows a preview error instead of treating blank HTML as success")
it("submits global and local adjustment scopes with explicit agent")
it("keeps old content visible while adjusting and after failed adjustment")
it("enables save only for a validated draft")
it("requires confirmation before regenerate and preserves saved content until success")
```

- [ ] **Step 2: Implement the content-first layout**

Desktop grid:

```text
top: project / name / platform / state / save
left: actual section and Token-group navigation
center: Markdown principles, visual Tokens, real UI Kit
right: current Agent, selected scope, natural-language adjustment, task feedback
```

Use `minmax(0, 1fr)` and bounded side columns so text and iframe never overlap. On narrow viewports, keep content full width and put the adjustment controls in the existing Sheet/Drawer primitive. Do not put the page in a decorative card or show an internal file tree.

Render section markdown with the existing sanitized `ReadonlyContent`.

- [ ] **Step 3: Implement token visuals**

Render only groups supplied by the Server. Use color swatches for color values, type samples for font values, and compact name/value rows for spacing/radius/shadow/motion/unknown extension groups. Long Token names wrap; no viewport-scaled font sizes or negative letter spacing.

- [ ] **Step 4: Implement the iframe selection boundary**

Use:

```tsx
<iframe ref={frameRef} srcDoc={previewHtml} sandbox="allow-scripts" title="项目设计体系 UI Kit" />
```

The window-message handler must require:

```ts
event.source === frameRef.current?.contentWindow
event.data?.type === "multica:project-design-system-select"
knownLocatorIds.has(event.data.id)
```

Show the selected label beside the adjustment input and allow clearing back to `{kind: "all"}`. Do not trust a locator label from the message; resolve it from Server data.

- [ ] **Step 5: Implement adjustment, save, and regenerate**

The current Agent may be prefilled for continuity, but the user can explicitly change it before the next adjustment; Server readiness remains authoritative. Adjustment posts a complete scope plus non-empty instruction. Successful domain events refresh actual content. Failure shows the reason and keeps the old digest on screen.

Save changes state from draft to saved. Regenerate uses a secondary menu and confirmation that new output will become a draft and saved content remains until the user saves it.

- [ ] **Step 6: Wire routes in static-before-dynamic order**

Export `ProjectDesignSystemPage`. Add the Next route. In desktop routes, register `designs/systems/:id` before `designs/:id`, otherwise `systems` is consumed as a design-file ID.

- [ ] **Step 7: Run shared view, Web, and Desktop checks**

```bash
rtk pnpm --filter @multica/views exec vitest run designs/project-design-system-page.test.tsx designs/project-design-system-preview.test.tsx
rtk pnpm --filter @multica/views typecheck
rtk pnpm --filter @multica/web typecheck
rtk pnpm --filter @multica/desktop typecheck
```

- [ ] **Step 8: Commit the content and UI Kit experience**

```bash
rtk git add packages/views/designs apps/web/app/'[workspaceSlug]'/'(dashboard)'/designs/systems apps/desktop/src/renderer/src/routes.tsx
rtk git commit -m "feat(design): preview and adjust project design systems"
```

---

### Task 9: Prove The Complete Workflow In Local Chrome

**Files:**
- Create after evidence exists: `docs/product/design-center/project-design-system-validation.md`
- Modify only if acceptance exposes a defect: files owned by Tasks 1-8 plus focused regression tests.

**Interfaces:**
- Consumes: running backend on `8080`, frontend on `3031`, an online user-selected Agent, and a real project.
- Produces: evidence satisfying every first-stage completion criterion; no status-only claims.

- [ ] **Step 1: Run the focused automated suite**

```bash
rtk pnpm --filter @multica/core test
rtk pnpm --filter @multica/views test
rtk pnpm typecheck
```

Workdir `server`:

```bash
rtk go test ./internal/projectdesignsystem ./internal/handler ./internal/service ./internal/daemon ./internal/daemon/execenv -count=1
```

Classify unrelated baseline failures separately; do not weaken new tests to make the suite green.

- [ ] **Step 2: Start services only through the repository's recorded commands**

Use `rtk make stop` followed by `rtk make start` only if a restart is required. Confirm `http://localhost:8080/health` and `http://localhost:3031` before touching Agent state. Do not run a second frontend command, change ports, reset the DB, or stop unrelated daemons.

- [ ] **Step 3: Create through the real design-center UI in the user's local Chrome**

Using `chrome:control-chrome`, select one project with no project design system, open `设计体系`, explicitly select the Agent, select a platform, enter a distinctive brief, attach at least one optional reference, and click `生成设计体系`.

Capture:

- project/system/task IDs;
- exact selected Agent ID;
- exact platform, brief, and reference IDs from persisted task context;
- daemon output directory listing and three file byte sizes;
- package digest and validation report;
- screenshot of dynamic content and nonblank UI Kit.

- [ ] **Step 4: Verify visual content, not only records**

At desktop `1440x900` and mobile `390x844`, capture screenshots and verify:

- project direction and reference visibly influenced Tokens/UI Kit;
- no filenames or file tree are visible;
- text does not overlap or overflow controls;
- the iframe is nonblank and component states are distinguishable;
- clicking one component updates the adjustment scope label;
- narrow-screen adjustment Sheet does not cover the primary content when closed.

Inspect console and Network for CSP violations, unexpected external requests, iframe errors, API 4xx/5xx, and repeated refetch loops.

- [ ] **Step 5: Perform one real local adjustment and save**

Select a component, request a visibly obvious but specification-compliant change, and record old/new digests. Prove all three artifact hashes changed consistently where expected, UI visuals changed, no parallel system was created, and save survives page refresh and a new authenticated browser navigation.

- [ ] **Step 6: Inject failures**

Run controlled tests for:

1. selected Agent offline before submit: request blocked and form preserved;
2. missing `components.html`: task failed and no first draft created;
3. unsafe `<script>` or event attribute: task failed and prior package digest unchanged;
4. failed adjustment: prior visible package and saved package remain intact.

Do not manually patch DB state to make these pass.

- [ ] **Step 7: Write the evidence document**

Record exact commands, IDs, artifact hashes, screenshots, browser observations, failure evidence, and remaining differences. Never state completion based only on `task.status = completed`, package row existence, or UI status text.

- [ ] **Step 8: Run final structural checks and commit evidence/fixes**

```bash
rtk node .gitnexus/run.cjs detect_changes --scope compare --base-ref main
rtk git diff --check
rtk git status --short
```

Expected: affected flows are limited to project design-system creation, specialized Agent task transport/completion, Design Center, and route/realtime plumbing. Stage only the evidence document and any focused acceptance fixes, then commit with an accurate message.

---

## Self-Review Checklist

- [ ] Every requirement in spec sections 5-16 maps to at least one task and one test/evidence step.
- [ ] No task migrates or promotes legacy `design_system_profile`.
- [ ] No successful code path relies on model final-output length or task status alone.
- [ ] First generation and later adjustment share one validator and one atomic draft replacement path.
- [ ] Saved content remains readable during regenerate/adjust and after failure.
- [ ] Explicit Agent selection exists in UI and is revalidated on Server.
- [ ] Preview selection does not permit Agent JavaScript or same-origin iframe access.
- [ ] Web and Desktop share the same views and both route to `/designs/systems/:id`.
- [ ] API response parsing has malformed-response fallbacks.
- [ ] SQL and generated SQL comply with the repository's no-`JOIN` push rule.
- [ ] Live acceptance verifies prompt input, three files, Server validation, database digest, and actual visuals.
