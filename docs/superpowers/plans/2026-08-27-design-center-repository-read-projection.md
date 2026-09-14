# Design Center Repository Read Projection Implementation Plan (M1 Slice 1 Closure + Slice 2A)

**Goal:** Persist trustworthy Design Document repository-grounding evidence, expose exact project/repository list contracts for both design entities, and add a typed Core read projection that can support the later Finder/repository UI without implementing that UI in this slice.

**Architecture:** The immutable `design_document_revision` becomes the durable source of repository-grounding evidence; `design_document.project_resource_id` remains only the current human-managed repository link. Existing list routes gain optional project/repository scopes without breaking workspace-wide Design File compatibility. Core owns scope-aware query keys and the unified `DesignAssetListItem` projection; Views remain unchanged except for a regression test proving the current page still compiles against the Core surface.

**Tech Stack:** Go 1.26, PostgreSQL 17, sqlc v1.31.1, Chi, pgx v5, TypeScript, Zod, TanStack Query, Vitest.

**Spec:** `docs/superpowers/specs/2026-08-26-design-center-project-repository-views-m1-design.md`

**Implementation Baseline:** `origin/codex/design-file-repository-scope@80092ab8c`. Before Task 1, the controller must fetch that remote ref, verify the SHA, and create `codex/design-center-repository-read-projection` plus an isolated integration worktree from it. If the remote SHA has advanced, stop and regenerate impact/context before creating Task 1.

## Global Constraints

- `repository_grounded` is evidence, never an alias for `project_resource_id != NULL`.
- A manually associated repository with no validated grounding receipt returns `repository_grounded=false`.
- Grounding evidence belongs to an immutable Design Document revision; draft responses use the draft revision first, saved delivery uses the saved revision only.
- `pinned` adjustment/regeneration inherits the base revision's persisted grounding evidence; it does not claim that the repository was re-read.
- `design_file` and `design_document` remain separate database entities; only Core list projection is unified.
- Repository list filtering is exact on `workspace_id + project_id + project_resource_id`; it never falls back to project scope and never infers a repository.
- Existing `GET /api/design-files` workspace-wide behavior remains compatible when no project scope is supplied.
- `issue_id` and `project_resource_id` cannot be combined on `GET /api/design-documents`; reject the ambiguous request with `400 invalid_request`.
- Network fields remain `snake_case`; TypeScript query scopes and projections use `camelCase`.
- No database foreign keys, cascading actions, historical backfill, Finder UI, Repository Workspace UI, association dialog, template retirement, design-system fallback removal, or Electron visual work in this plan.
- Migration `909` is reserved for this plan; implementation must verify the stem is unoccupied before writing it.
- Run GitNexus `impact` before modifying every existing function/method and stop on HIGH/CRITICAL until the user is warned.
- Run GitNexus `detect-changes` before every task commit.
- Each Task uses a dedicated `codex/design-center-repository-read-projection-task-N` branch created from the latest integration branch, produces its own commit, receives DeepSeek review, and merges with `--no-ff` into `codex/design-center-repository-read-projection`.
- Every task ends with `git diff --check`; backend tasks run focused Go tests and `go build ./...`; Core tasks run focused Vitest and package typecheck.

---

### Task 1: Migration 909 — Persist Revision Grounding Evidence

**Files:**
- Create: `server/migrations/909_design_document_revision_repository_grounding.up.sql`
- Create: `server/migrations/909_design_document_revision_repository_grounding.down.sql`

**Interfaces:**
- Produces: nullable `design_document_revision.repository_grounding JSONB`.
- Consumed by: Task 2 completion persistence and Task 3 response semantics.

- [ ] **Step 1: Verify migration number and write the failing schema assertion**

Add a focused assertion in a temporary SQL verification script outside the worktree:

```sql
SELECT count(*)
FROM information_schema.columns
WHERE table_schema = 'public'
  AND table_name = 'design_document_revision'
  AND column_name = 'repository_grounding';
```

Run against a Task 1 worktree database migrated through 908.

Expected: `0`.

- [ ] **Step 2: Write the up migration**

```sql
-- A revision owns the validated repository-grounding receipt used to produce
-- that immutable design package. NULL means the revision has no durable
-- repository evidence; it does not mean the document has no current link.
ALTER TABLE design_document_revision
    ADD COLUMN repository_grounding JSONB;
```

- [ ] **Step 3: Write the down migration**

```sql
ALTER TABLE design_document_revision
    DROP COLUMN IF EXISTS repository_grounding;
```

- [ ] **Step 4: Verify reversible SQL without leaving unrecorded schema drift**

Use the isolated PostgreSQL container and pipe the migration files into `psql` inside an explicit transaction:

```bash
set -a
source .env.worktree
set +a
{
  echo 'BEGIN;'
  cat server/migrations/909_design_document_revision_repository_grounding.up.sql
  echo 'ROLLBACK;'
} | docker compose --env-file .env.worktree exec -T postgres \
      psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1
```

Expected: `ALTER TABLE`, then `ROLLBACK`; the column count remains `0` afterward.

- [ ] **Step 5: Run migration rules**

```bash
cd server
go test ./internal/migrations -count=1
```

Expected: PASS.

- [ ] **Step 6: Run checks and commit**

```bash
git diff --check
git add \
  server/migrations/909_design_document_revision_repository_grounding.up.sql \
  server/migrations/909_design_document_revision_repository_grounding.down.sql
git diff --cached --check
node .gitnexus/run.cjs detect-changes --scope staged --repo multica-design-center-repository-read-projection-task-1
git commit -m "feat(designs): persist revision repository grounding"
```

Expected: one commit containing only the 909 migration pair.

---

### Task 2: Carry Validated Grounding Through Completion and Persist It

**Files:**
- Modify: `server/internal/handler/daemon.go`
- Modify: `server/internal/handler/design_document_completion.go`
- Modify: `server/pkg/db/queries/design_document.sql`
- Modify generated: `server/pkg/db/generated/design_document.sql.go`
- Modify generated: `server/pkg/db/generated/models.go`
- Create: `server/internal/handler/design_document_grounding_persistence_test.go`

**Interfaces:**
- Consumes: Task 1 `design_document_revision.repository_grounding`.
- Produces:
  - `TaskCompleteRequest.DesignDocumentGrounding json.RawMessage`.
  - `preparedDesignDocumentCompletion.RepositoryGrounding json.RawMessage`.
  - persisted, server-validated grounding JSON on each created revision.
- Consumed by: Task 3 `repository_grounded` calculation.

- [ ] **Step 1: Write failing grounding-normalization unit tests**

Create `design_document_grounding_persistence_test.go` with a table covering the three task-context modes:

```go
func TestValidateDesignDocumentCompletionGrounding(t *testing.T) {
    available := json.RawMessage(`{
      "schema_version":"multica.design-document-grounding/v1",
      "status":"available",
      "repositories":[{
        "id":"repo-1",
        "checkout_path":"repo",
        "commit_sha":"0123456789012345678901234567890123456789",
        "status_sha256":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "tree_sha256":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
        "files":[]
      }],
      "facts":[],
      "conflicts":[],
      "missing":[],
      "warnings":[]
    }`)
    unavailable := json.RawMessage(`{
      "schema_version":"multica.design-document-grounding/v1",
      "status":"unavailable",
      "repositories":[],
      "facts":[],
      "conflicts":[],
      "missing":[],
      "warnings":["repository unavailable"]
    }`)

    tests := []struct {
        name    string
        mode    string
        raw     json.RawMessage
        wantSet bool
        wantErr bool
    }{
        {name: "pending requires available evidence", mode: service.DesignDocumentGroundingPending, raw: available, wantSet: true},
        {name: "pending rejects missing evidence", mode: service.DesignDocumentGroundingPending, wantErr: true},
        {name: "unavailable accepts explicit unavailable receipt", mode: service.DesignDocumentGroundingUnavailable, raw: unavailable, wantSet: true},
        {name: "pinned permits inheritance", mode: service.DesignDocumentGroundingPinned},
        {name: "pending rejects unavailable receipt", mode: service.DesignDocumentGroundingPending, raw: unavailable, wantErr: true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := validateDesignDocumentCompletionGrounding(tt.mode, tt.raw)
            if (err != nil) != tt.wantErr {
                t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
            }
            if (len(got) > 0) != tt.wantSet {
                t.Fatalf("grounding set = %v, want %v", len(got) > 0, tt.wantSet)
            }
        })
    }
}
```

Run:

```bash
cd server
go test ./internal/handler -run TestValidateDesignDocumentCompletionGrounding -count=1
```

Expected: FAIL because the helper does not exist.

- [ ] **Step 2: Add the completion wire field**

In `TaskCompleteRequest` add:

```go
DesignDocumentGrounding json.RawMessage `json:"design_document_grounding,omitempty"`
```

The daemon client already serializes the `design_document_grounding` JSON key and its existing client test remains the wire-source regression. Add a handler-side decode assertion in `design_document_grounding_persistence_test.go`:

```go
func TestTaskCompleteRequestDecodesDesignDocumentGrounding(t *testing.T) {
    var request TaskCompleteRequest
    body := []byte(`{"design_document_grounding":{"schema_version":"multica.design-document-grounding/v1","status":"unavailable","repositories":[],"facts":[],"conflicts":[],"missing":[],"warnings":["repository unavailable"]}}`)
    if err := json.Unmarshal(body, &request); err != nil {
        t.Fatal(err)
    }
    if len(request.DesignDocumentGrounding) == 0 {
        t.Fatal("design_document_grounding was discarded")
    }
}
```

Update the `sanitizeTaskCompleteRequest` comment so it claims exhaustiveness only for caller-supplied string fields; grounding JSON is handled by strict schema validation, not text sanitization.

- [ ] **Step 3: Add strict completion validation**

Add a pure helper in `design_document_completion.go`:

```go
func validateDesignDocumentCompletionGrounding(mode string, raw json.RawMessage) (json.RawMessage, error) {
    if len(raw) == 0 {
        if mode == service.DesignDocumentGroundingPending {
            return nil, errors.New("design document repository grounding is required")
        }
        return nil, nil
    }
    grounding, err := designdocument.ValidateRepositoryGrounding(raw)
    if err != nil {
        return nil, err
    }
    switch mode {
    case service.DesignDocumentGroundingPending:
        if grounding.Status != designdocument.GroundingAvailable {
            return nil, errors.New("pending repository grounding must be available")
        }
    case service.DesignDocumentGroundingUnavailable:
        if grounding.Status != designdocument.GroundingUnavailable {
            return nil, errors.New("unavailable repository grounding must remain unavailable")
        }
    case service.DesignDocumentGroundingPinned:
        return nil, nil
    default:
        return nil, errors.New("unknown design document grounding mode")
    }
    normalized, err := json.Marshal(grounding)
    if err != nil {
        return nil, err
    }
    return normalized, nil
}
```

Extend `preparedDesignDocumentCompletion`:

```go
RepositoryGrounding json.RawMessage
```

Pass `req.DesignDocumentGrounding` into `prepareDesignDocumentCompletion`, validate against `taskContext.RepositoryGrounding`, and store the normalized bytes in the prepared value.

- [ ] **Step 4: Persist explicit evidence or inherit base-revision evidence**

Extend `CreateDesignDocumentRevision`:

```sql
INSERT INTO design_document_revision (
    workspace_id,
    design_document_id,
    revision_number,
    package_schema,
    content_digest,
    archive_object_key,
    artifact_index,
    manifest,
    brief,
    coverage,
    audit,
    preview,
    input_snapshot_sha256,
    base_revision_id,
    design_system_digest,
    source_task_id,
    agent_id,
    instruction,
    scope,
    repository_grounding
) VALUES (
    sqlc.arg('workspace_id'),
    sqlc.arg('design_document_id'),
    sqlc.arg('revision_number'),
    sqlc.arg('package_schema'),
    sqlc.arg('content_digest'),
    sqlc.arg('archive_object_key'),
    sqlc.arg('artifact_index'),
    sqlc.arg('manifest'),
    sqlc.arg('brief'),
    sqlc.arg('coverage'),
    sqlc.arg('audit'),
    sqlc.narg('preview'),
    sqlc.arg('input_snapshot_sha256'),
    sqlc.narg('base_revision_id'),
    sqlc.narg('design_system_digest'),
    sqlc.narg('source_task_id'),
    sqlc.narg('agent_id'),
    sqlc.narg('instruction'),
    sqlc.narg('scope'),
    sqlc.narg('repository_grounding')
)
RETURNING *;
```

In `persistDesignDocumentCompletion`, resolve the bytes before creating the revision:

```go
repositoryGrounding := prepared.RepositoryGrounding
if len(repositoryGrounding) == 0 && prepared.TaskContext.BaseRevisionID != "" {
    baseRevisionID := parseUUID(prepared.TaskContext.BaseRevisionID)
    baseRevision, err := queries.GetDesignDocumentRevisionInWorkspace(ctx, db.GetDesignDocumentRevisionInWorkspaceParams{
        ID: baseRevisionID, WorkspaceID: prepared.WorkspaceID,
    })
    if err != nil {
        return db.DesignDocument{}, err
    }
    repositoryGrounding = baseRevision.RepositoryGrounding
}
```

Pass:

```go
RepositoryGrounding: repositoryGrounding,
```

Run root-level `make sqlc`. Expected generated changes are limited to `design_document.sql.go` and `models.go`.

- [ ] **Step 5: Add persistence and inheritance tests**

Add tests that create a base revision and assert:

```go
func TestDesignDocumentRevisionStoresValidatedRepositoryGrounding(t *testing.T)
func TestPinnedDesignDocumentRevisionInheritsBaseGrounding(t *testing.T)
func TestPendingDesignDocumentCompletionRejectsMissingGrounding(t *testing.T)
```

Each test must read the created revision with `GetDesignDocumentRevisionInWorkspace` and compare `RepositoryGrounding` JSON semantically after unmarshalling, not by byte order.

Run:

```bash
cd server
go test ./internal/handler -run 'Test(DesignDocumentRevisionStoresValidatedRepositoryGrounding|PinnedDesignDocumentRevisionInheritsBaseGrounding|PendingDesignDocumentCompletionRejectsMissingGrounding|ValidateDesignDocumentCompletionGrounding)' -count=1
```

Expected: PASS.

- [ ] **Step 6: Build, detect and commit**

```bash
cd server
go build ./...
cd ..
git diff --check
git add \
  server/internal/handler/daemon.go \
  server/internal/handler/design_document_completion.go \
  server/internal/handler/design_document_grounding_persistence_test.go \
  server/pkg/db/queries/design_document.sql \
  server/pkg/db/generated/design_document.sql.go \
  server/pkg/db/generated/models.go
git diff --cached --check
node .gitnexus/run.cjs detect-changes --scope staged --repo multica-design-center-repository-read-projection-task-2
git commit -m "feat(designs): persist validated repository grounding"
```

---

### Task 3: Derive `repository_grounded` From Revision Evidence

**Files:**
- Modify: `server/internal/handler/design_document.go`
- Modify: `server/internal/handler/design_document_revision.go`
- Modify: `server/internal/handler/design_document_manual_edit.go`
- Modify: `server/internal/handler/design_document_lifecycle.go`
- Modify: `server/internal/handler/design_document_adjust.go`
- Modify: `server/internal/handler/design_document_deliver.go`
- Modify: `server/internal/handler/design_document_regenerate.go`
- Modify: `server/internal/handler/design_document_delivery_context.go`
- Create: `server/internal/handler/design_document_repository_grounding_test.go`

**Interfaces:**
- Consumes: persisted revision `RepositoryGrounding` from Task 2.
- Produces: all Design Document responses and saved delivery contexts derive `repository_grounded` from the selected revision evidence.
- Consumed by: Task 7 unified projection.

- [ ] **Step 1: Write failing pure evidence tests**

```go
func TestRepositoryGroundingAvailable(t *testing.T) {
    available := []byte(`{"schema_version":"multica.design-document-grounding/v1","status":"available","repositories":[{"id":"repo-1","checkout_path":"repo","commit_sha":"0123456789012345678901234567890123456789","status_sha256":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","tree_sha256":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","files":[]}],"facts":[],"conflicts":[],"missing":[],"warnings":[]}`)
    unavailable := []byte(`{"schema_version":"multica.design-document-grounding/v1","status":"unavailable","repositories":[],"facts":[],"conflicts":[],"missing":[],"warnings":["repository unavailable"]}`)

    if !repositoryGroundingAvailable(available) {
        t.Fatal("available grounding should be true")
    }
    if repositoryGroundingAvailable(unavailable) || repositoryGroundingAvailable(nil) || repositoryGroundingAvailable([]byte(`{}`)) {
        t.Fatal("missing, invalid, and unavailable grounding should be false")
    }
}
```

Run:

```bash
cd server
go test ./internal/handler -run TestRepositoryGroundingAvailable -count=1
```

Expected: FAIL because the helper does not exist.

- [ ] **Step 2: Add selected-revision helpers**

```go
func repositoryGroundingAvailable(raw []byte) bool {
    grounding, err := designdocument.ValidateRepositoryGrounding(raw)
    return err == nil && grounding.Status == designdocument.GroundingAvailable
}

func designDocumentDisplayRevisionID(document db.DesignDocument) pgtype.UUID {
    if document.DraftRevisionID.Valid {
        return document.DraftRevisionID
    }
    return document.SavedRevisionID
}

func (h *Handler) designDocumentRepositoryGrounded(ctx context.Context, document db.DesignDocument, savedOnly bool) bool {
    revisionID := designDocumentDisplayRevisionID(document)
    if savedOnly {
        revisionID = document.SavedRevisionID
    }
    if !revisionID.Valid {
        return false
    }
    revision, err := h.Queries.GetDesignDocumentRevisionInWorkspace(ctx, db.GetDesignDocumentRevisionInWorkspaceParams{
        ID: revisionID, WorkspaceID: document.WorkspaceID,
    })
    return err == nil && repositoryGroundingAvailable(revision.RepositoryGrounding)
}
```

Keep `designDocumentResponse` as the pure struct mapper but add an explicit boolean argument:

```go
func designDocumentResponse(document db.DesignDocument, task *db.AgentTaskQueue, repositoryGrounded bool) DesignDocumentResponse
```

Set:

```go
RepositoryGrounded: repositoryGrounded,
```

- [ ] **Step 3: Update every response call site explicitly**

Update the existing call sites in:

```text
server/internal/handler/design_document.go
server/internal/handler/design_document_revision.go
server/internal/handler/design_document_manual_edit.go
server/internal/handler/design_document_lifecycle.go
server/internal/handler/design_document_adjust.go
server/internal/handler/design_document_deliver.go
server/internal/handler/design_document_regenerate.go
```

For normal responses call:

```go
grounded := h.designDocumentRepositoryGrounded(r.Context(), document, false)
writeJSON(w, status, designDocumentResponse(document, task, grounded))
```

For list responses compute the bool per document before appending the response. This plan accepts the additional bounded revision lookup because the list already performs active-task lookups; query aggregation is a later optimization, not a semantic shortcut.

- [ ] **Step 4: Fix saved delivery context**

In `design_document_delivery_context.go`, remove `row.ProjectResourceID.Valid`. The function already loads the saved revision; derive:

```go
RepositoryGrounded: repositoryGroundingAvailable(revision.RepositoryGrounding),
```

Only the saved revision can mark delivery context grounded.

- [ ] **Step 5: Add semantic regression tests**

```go
func TestDesignDocumentResponseManualRepositoryLinkIsNotGrounded(t *testing.T)
func TestDesignDocumentResponseUsesDraftRevisionGrounding(t *testing.T)
func TestDesignDocumentDeliveryContextUsesSavedRevisionGrounding(t *testing.T)
```

Required assertions:

```text
project_resource_id set + no revision evidence     -> false
saved ungrounded + draft grounded                  -> normal response true, delivery false
saved grounded + draft unavailable                 -> normal response false, delivery true
```

Run:

```bash
cd server
go test ./internal/handler -run 'Test(RepositoryGroundingAvailable|DesignDocumentResponse.*Grounded|DesignDocumentDeliveryContextUsesSavedRevisionGrounding)' -count=1
go test ./internal/service -run 'Design(Context|Document)' -count=1
```

Expected: PASS.

- [ ] **Step 6: Build, detect and commit**

```bash
cd server
go build ./...
cd ..
git diff --check
git add server/internal/handler/design_document*.go server/internal/handler/design_document_repository_grounding_test.go
git diff --cached --check
node .gitnexus/run.cjs detect-changes --scope staged --repo multica-design-center-repository-read-projection-task-3
git commit -m "fix(designs): derive grounding from revision evidence"
```

---

### Task 4: Repository-Scoped Backend List Contracts

**Files:**
- Modify: `server/pkg/db/queries/design_document.sql`
- Modify generated: `server/pkg/db/generated/design_document.sql.go`
- Modify: `server/internal/handler/design_file.go`
- Modify: `server/internal/handler/design_document.go`
- Modify: `server/internal/handler/design_file_test.go`
- Modify: `server/internal/handler/design_sql_query_test.go`
- Create: `server/internal/handler/design_document_repository_list_test.go`

**Interfaces:**
- Produces:
  - `ListDesignDocumentsByRepositoryParams{WorkspaceID, ProjectID, ProjectResourceID}`.
  - `GET /api/design-files?project_id=&project_resource_id=` exact repository filtering.
  - `GET /api/design-documents?project_id=&project_resource_id=` exact repository filtering.
  - `DesignFileResponse.project_resource_id`.
- Consumed by: Task 5 Core API client and Task 6 query options.

- [ ] **Step 1: Write failing repository list tests**

Create HTTP tests with two projects, repository A/B, linked and unlinked assets:

```go
func TestListDesignFilesByRepositoryIsExact(t *testing.T)
func TestListDesignDocumentsByRepositoryIsExact(t *testing.T)
func TestListDesignDocumentsRejectsIssueAndRepositoryScope(t *testing.T)
func TestListDesignFilesRejectsRepositoryWithoutProject(t *testing.T)
```

The exact-result assertions must prove:

```text
repository A returns only A-linked assets
repository B returns only B-linked assets
unlinked assets return in project scope but not repository scope
cross-project repository returns project_resource_project_mismatch
non-github resource returns project_resource_not_repository
```

Run:

```bash
cd server
go test ./internal/handler -run 'TestListDesign(File|Documents).*Repository' -count=1
```

Expected: FAIL because the handlers do not parse repository scope and the Document query does not exist.

- [ ] **Step 2: Add the Design Document query**

```sql
-- name: ListDesignDocumentsByRepository :many
SELECT * FROM design_document
WHERE workspace_id = sqlc.arg('workspace_id')
  AND project_id = sqlc.arg('project_id')
  AND project_resource_id = sqlc.arg('project_resource_id')
ORDER BY updated_at DESC;
```

Run root-level `make sqlc`.

Expected generated signature:

```go
type ListDesignDocumentsByRepositoryParams struct {
    WorkspaceID       pgtype.UUID `json:"workspace_id"`
    ProjectID         pgtype.UUID `json:"project_id"`
    ProjectResourceID pgtype.UUID `json:"project_resource_id"`
}

func (q *Queries) ListDesignDocumentsByRepository(ctx context.Context, arg ListDesignDocumentsByRepositoryParams) ([]DesignDocument, error)
```

Expected generated file change: `server/pkg/db/generated/design_document.sql.go` only.

- [ ] **Step 3: Add `project_resource_id` to Design File responses**

Extend `DesignFileResponse`:

```go
ProjectResourceID *string `json:"project_resource_id"`
```

Map it in `designFileToResponse`:

```go
ProjectResourceID: uuidPtr(file.ProjectResourceID),
```

If no shared `uuidPtr` exists, add a file-local helper:

```go
func optionalUUIDString(value pgtype.UUID) *string {
    if !value.Valid {
        return nil
    }
    result := uuidToString(value)
    return &result
}
```

- [ ] **Step 4: Scope `ListDesignFiles` without breaking workspace callers**

Implement this decision tree:

```text
no project_id, no project_resource_id -> existing workspace-wide ListDesignFiles
project_id only                       -> ListDesignFilesByProject
project_id + project_resource_id      -> validate repository, ListDesignFilesByRepository
project_resource_id without project   -> 400 invalid_request
```

Reuse existing project lookup and resource validation. Do not modify the shared validation helpers. For the project-only branch call the existing sqlc query with an invalid optional folder:

```go
files, err := h.Queries.ListDesignFilesByProject(r.Context(), db.ListDesignFilesByProjectParams{
    WorkspaceID: wsUUID,
    ProjectID: projectID,
    FolderID: pgtype.UUID{},
})
```

- [ ] **Step 5: Scope `ListDesignDocuments`**

Implement:

```text
issue_id only                         -> existing issue query
project_id only                       -> existing project query
project_id + project_resource_id      -> validate repository, repository query
issue_id + project_resource_id        -> 400 invalid_request
project_resource_id without project   -> 400 invalid_request
```

Continue populating active task and Task 3 evidence-based `repository_grounded` for each result.

- [ ] **Step 6: Run tests, build, detect and commit**

```bash
cd server
go test ./internal/handler -run 'TestListDesign(File|Documents).*Repository|TestListAndGetDesignFiles' -count=1
go build ./...
cd ..
git diff --check
git add \
  server/pkg/db/queries/design_document.sql \
  server/pkg/db/generated/design_document.sql.go \
  server/internal/handler/design_file.go \
  server/internal/handler/design_document.go \
  server/internal/handler/design_file_test.go \
  server/internal/handler/design_sql_query_test.go \
  server/internal/handler/design_document_repository_list_test.go
git diff --cached --check
node .gitnexus/run.cjs detect-changes --scope staged --repo multica-design-center-repository-read-projection-task-4
git commit -m "feat(designs): add repository-scoped asset lists"
```

---

### Task 5: Core Types, Schemas and API Client Contracts

**Files:**
- Modify: `packages/core/types/design.ts`
- Modify: `packages/core/api/schemas.ts`
- Modify: `packages/core/api/client.ts`
- Modify: `packages/core/designs/schema.test.ts`
- Create: `packages/core/designs/api-contract.test.ts`

**Interfaces:**
- Produces:
  - `DesignFile.project_resource_id?: string | null`.
  - scoped list client methods.
  - typed batch repository-association mutation.
- Consumed by: Task 6 query options and Task 7 projection.

- [ ] **Step 1: Write failing schema and client tests**

```ts
// @vitest-environment node
it("parses the nullable Design File repository id", () => {
  const parsed = DesignFileSchema.parse({
    ...EMPTY_DESIGN_FILE_DETAIL_RESPONSE.design_file,
    project_resource_id: "repo-1",
  });
  expect(parsed.project_resource_id).toBe("repo-1");
});

it("uses snake_case repository list query parameters", async () => {
  await api.listDesignFiles({ projectId: "project-1", projectResourceId: "repo-1" });
  expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining("project_id=project-1&project_resource_id=repo-1"), expect.anything());
});
```

Add equivalent tests for Design Documents and `PUT /api/design-assets/repository-association`.

Run:

```bash
pnpm --filter @multica/core exec vitest run designs/schema.test.ts designs/api-contract.test.ts
```

Expected: FAIL because the field and client methods do not exist.

- [ ] **Step 2: Extend types**

```ts
export interface DesignFile {
  id: string;
  workspace_id: string;
  project_id?: string | null;
  project_resource_id?: string | null;
  folder_id?: string | null;
  title: string;
  description: string | null;
  source_type: DesignSourceType;
  source_ref: Record<string, unknown>;
  thumbnail_url?: string | null;
  current_revision_id: string | null;
  created_by: string | null;
  created_at: string;
  updated_at: string;
}

export type DesignAssetAssociationKind = "design_file" | "design_document";

export interface SetDesignAssetRepositoryAssociationRequest {
  project_id: string;
  project_resource_id: string;
  items: Array<{ kind: DesignAssetAssociationKind; id: string }>;
}

export interface SetDesignAssetRepositoryAssociationResponse {
  project_id: string;
  project_resource_id: string;
  count: number;
}
```

- [ ] **Step 3: Extend Zod schemas and fallbacks**

Add to `DesignFileSchema`:

```ts
project_resource_id: z.string().nullable().catch(null).default(null),
```

Add the same field with `null` to `EMPTY_DESIGN_FILE_DETAIL_RESPONSE.design_file`.

Add a strict association response schema:

```ts
export const SetDesignAssetRepositoryAssociationResponseSchema = z.object({
  project_id: z.string(),
  project_resource_id: z.string(),
  count: z.number().int().nonnegative(),
});
```

- [ ] **Step 4: Add scoped API methods**

```ts
async listDesignFiles(params?: { projectId?: string; projectResourceId?: string }): Promise<ListDesignFilesResponse>

async listDesignDocuments(projectId: string, projectResourceId?: string): Promise<ListDesignDocumentsResponse>

async setDesignAssetRepositoryAssociation(
  data: SetDesignAssetRepositoryAssociationRequest,
): Promise<SetDesignAssetRepositoryAssociationResponse>
```

Build URL parameters with `URLSearchParams` and exact network names:

```ts
const search = new URLSearchParams();
if (params?.projectId) search.set("project_id", params.projectId);
if (params?.projectResourceId) search.set("project_resource_id", params.projectResourceId);
```

Parse the mutation response with `SetDesignAssetRepositoryAssociationResponseSchema`; malformed responses must reject rather than silently return a fallback.

- [ ] **Step 5: Run Core tests and commit**

```bash
pnpm --filter @multica/core exec vitest run designs/schema.test.ts designs/api-contract.test.ts
pnpm --filter @multica/core typecheck
git diff --check
git add \
  packages/core/types/design.ts \
  packages/core/api/schemas.ts \
  packages/core/api/client.ts \
  packages/core/designs/schema.test.ts \
  packages/core/designs/api-contract.test.ts
git diff --cached --check
node .gitnexus/run.cjs detect-changes --scope staged --repo multica-design-center-repository-read-projection-task-5
git commit -m "feat(core): add repository design asset contracts"
```

---

### Task 6: Scope-Aware Query Keys and Options

**Files:**
- Modify: `packages/core/types/design.ts`
- Modify: `packages/core/designs/keys.ts`
- Modify: `packages/core/designs/keys.test.ts`
- Modify: `packages/core/designs/queries.ts`
- Create: `packages/core/designs/repository-queries.test.ts`

**Interfaces:**
- Consumes: Task 5 scoped API methods.
- Produces: project/repository list options with non-colliding cache keys.
- Consumed by: Task 7 combined projection and later Slice 2B Views.

- [ ] **Step 1: Define the scope type and failing key tests**

```ts
export type DesignAssetScope =
  | { kind: "project"; projectId: string }
  | { kind: "repository"; projectId: string; projectResourceId: string };
```

Tests:

```ts
it("separates project and repository Design File caches", () => {
  expect(designKeys.files("ws-1", { kind: "project", projectId: "p-1" })).not.toEqual(
    designKeys.files("ws-1", { kind: "repository", projectId: "p-1", projectResourceId: "r-1" }),
  );
});

it("keeps the workspace file key as the invalidation prefix", () => {
  expect(designKeys.files("ws-1", { kind: "project", projectId: "p-1" }).slice(0, 3)).toEqual(
    designKeys.files("ws-1"),
  );
});
```

Run:

```bash
pnpm --filter @multica/core exec vitest run designs/keys.test.ts
```

Expected: FAIL because scoped keys do not exist.

- [ ] **Step 2: Implement keys**

```ts
files: (wsId: string, scope?: DesignAssetScope) =>
  scope
    ? [
        "designs",
        wsId,
        "files",
        scope.kind,
        scope.projectId,
        scope.kind === "repository" ? scope.projectResourceId : "",
      ] as const
    : ["designs", wsId, "files"] as const,

documentsByRepository: (wsId: string, projectId: string, projectResourceId: string) =>
  ["designs", wsId, "documents", "repository", projectId, projectResourceId] as const,

assetsByRepository: (wsId: string, projectId: string, projectResourceId: string) =>
  ["designs", wsId, "assets", "repository", projectId, projectResourceId] as const,
```

- [ ] **Step 3: Implement server-backed query options**

```ts
export function designFileListOptions(wsId: string, scope?: DesignAssetScope) {
  return queryOptions({
    queryKey: designKeys.files(wsId, scope),
    queryFn: () =>
      api.listDesignFiles(
        scope
          ? {
              projectId: scope.projectId,
              projectResourceId: scope.kind === "repository" ? scope.projectResourceId : undefined,
            }
          : undefined,
      ),
    select: (data) => data.design_files,
  });
}

export function designDocumentListByRepositoryOptions(
  wsId: string,
  projectId: string,
  projectResourceId: string,
) {
  return queryOptions({
    queryKey: designKeys.documentsByRepository(wsId, projectId, projectResourceId),
    queryFn: () => api.listDesignDocuments(projectId, projectResourceId),
    select: (data) => data.documents,
    enabled: Boolean(wsId && projectId && projectResourceId),
  });
}
```

Do not client-filter workspace-wide responses; the repository queries must call the exact backend contract from Task 4.

- [ ] **Step 4: Add query option tests**

Mock the API and assert:

```text
project scope sends only project_id
repository scope sends project_id + project_resource_id
project and repository keys do not collide
workspace prefix invalidates every file scope
repository document option is disabled when any ID is empty
```

Run:

```bash
pnpm --filter @multica/core exec vitest run designs/keys.test.ts designs/repository-queries.test.ts
pnpm --filter @multica/core typecheck
```

Expected: PASS.

- [ ] **Step 5: Detect and commit**

```bash
git diff --check
git add \
  packages/core/types/design.ts \
  packages/core/designs/keys.ts \
  packages/core/designs/keys.test.ts \
  packages/core/designs/queries.ts \
  packages/core/designs/repository-queries.test.ts
git diff --cached --check
node .gitnexus/run.cjs detect-changes --scope staged --repo multica-design-center-repository-read-projection-task-6
git commit -m "feat(core): scope design queries by repository"
```

---

### Task 7: Unified `DesignAssetListItem` Projection

**Files:**
- Create: `packages/core/designs/asset-projection.ts`
- Create: `packages/core/designs/asset-projection.test.ts`
- Modify: `packages/core/designs/index.ts`
- Modify: `packages/core/designs/queries.ts`

**Interfaces:**
- Consumes: Task 3 evidence-correct `DesignDocument.repository_grounded`, Task 5 entity types, Task 6 scoped queries.
- Produces:
  - `DesignAssetListItem`.
  - pure file/document projection functions.
  - combined repository read option.
- Consumed by: later Slice 2B Finder and Repository Workspace UI.

- [ ] **Step 1: Write failing projection tests**

```ts
// @vitest-environment node
it("projects an uploaded Figma file as saved-only", () => {
  const item = designFileToAssetItem(fileFixture);
  expect(item).toMatchObject({
    kind: "figma_file",
    hasSavedVersion: true,
    hasDraftVersion: false,
    repositoryGrounded: false,
  });
});

it("projects a document with saved and newer draft into both axes", () => {
  const item = designDocumentToAssetItem({
    ...documentFixture,
    status: "draft_ahead_of_saved",
    saved_revision_id: "saved-1",
    draft_revision_id: "draft-2",
    repository_grounded: true,
  });
  expect(item).toMatchObject({
    kind: "design_document",
    hasSavedVersion: true,
    hasDraftVersion: true,
    repositoryGrounded: true,
  });
});
```

Add cases for first generation running, first failure, draft waiting save, saved-only, and mixed descending sort.

- [ ] **Step 2: Implement the exact projection type**

```ts
export interface DesignAssetListItem {
  id: string;
  kind: "figma_file" | "design_document";
  projectId: string;
  projectResourceId: string | null;
  title: string;
  thumbnailUrl?: string;
  sourceLabel: string;
  status: string;
  hasSavedVersion: boolean;
  hasDraftVersion: boolean;
  repositoryGrounded: boolean;
  updatedAt: string;
}
```

- [ ] **Step 3: Implement pure mappings**

```ts
export function designFileToAssetItem(file: DesignFile): DesignAssetListItem {
  return {
    id: file.id,
    kind: "figma_file",
    projectId: file.project_id ?? "",
    projectResourceId: file.project_resource_id ?? null,
    title: file.title,
    thumbnailUrl: file.thumbnail_url ?? undefined,
    sourceLabel: "Figma",
    status: "saved",
    hasSavedVersion: true,
    hasDraftVersion: false,
    repositoryGrounded: false,
    updatedAt: file.updated_at,
  };
}

export function designDocumentToAssetItem(document: DesignDocument): DesignAssetListItem {
  const hasSavedVersion = document.saved_revision_id !== "";
  const hasDraftVersion = ["running", "failed", "draft", "draft_ahead_of_saved"].includes(document.status);
  return {
    id: document.id,
    kind: "design_document",
    projectId: document.project_id,
    projectResourceId: document.project_resource_id || null,
    title: document.title,
    sourceLabel: "Multica Design",
    status: document.status,
    hasSavedVersion,
    hasDraftVersion,
    repositoryGrounded: document.repository_grounded,
    updatedAt: document.updated_at,
  };
}

export function toDesignAssetItems(files: DesignFile[], documents: DesignDocument[]): DesignAssetListItem[] {
  return [...files.map(designFileToAssetItem), ...documents.map(designDocumentToAssetItem)].sort(
    (a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt),
  );
}
```

- [ ] **Step 4: Add the combined repository read option**

```ts
export function repositoryDesignAssetListOptions(
  wsId: string,
  projectId: string,
  projectResourceId: string,
) {
  return queryOptions({
    queryKey: designKeys.assetsByRepository(wsId, projectId, projectResourceId),
    queryFn: async () => {
      const [files, documents] = await Promise.all([
        api.listDesignFiles({ projectId, projectResourceId }),
        api.listDesignDocuments(projectId, projectResourceId),
      ]);
      return toDesignAssetItems(files.design_files, documents.documents);
    },
    enabled: Boolean(wsId && projectId && projectResourceId),
  });
}
```

Export the projection from `packages/core/designs/index.ts`.

- [ ] **Step 5: Run projection tests and commit**

```bash
pnpm --filter @multica/core exec vitest run designs/asset-projection.test.ts
pnpm --filter @multica/core typecheck
git diff --check
git add \
  packages/core/designs/asset-projection.ts \
  packages/core/designs/asset-projection.test.ts \
  packages/core/designs/index.ts \
  packages/core/designs/queries.ts
git diff --cached --check
node .gitnexus/run.cjs detect-changes --scope staged --repo multica-design-center-repository-read-projection-task-7
git commit -m "feat(core): project unified repository design assets"
```

---

### Task 8: Read-Only Repository Projection Gate and Slice Report

**Files:**
- Create: `server/internal/handler/design_repository_read_matrix_test.go`
- Create: `packages/core/designs/repository-read-matrix.test.ts`
- Modify: `packages/views/designs/designs-page.test.tsx` (regression-only assertion; no rendering change)
- Create: `docs/product/design-center/m1-slice-2a-validation.md`

**Interfaces:**
- Consumes: Tasks 1–7.
- Produces: evidence that project/repository scopes return and project the correct saved/draft assets without adding Finder UI.
- Handoff: later `M1 Slice 2B Finder + Repository Workspace + Association UI` plan.

- [ ] **Step 1: Add the backend real-data matrix**

Create one DB-backed test matrix containing:

```text
Project CRM
Repository A
Repository B
unlinked Design File
Repository A Design File
Repository B Design File
unlinked Design Document saved-only
Repository A Design Document draft-only
Repository A Design Document saved + newer draft
Repository B grounded Design Document
```

Assert project scope returns every project asset, repository A/B scopes are exact, and no unlinked asset appears in repository scope.

Run:

```bash
cd server
go test ./internal/handler -run TestDesignRepositoryReadMatrix -count=1
```

Expected: PASS.

- [ ] **Step 2: Add the Core projection matrix**

Mock the Task 4 APIs with the same fixture and assert:

```text
repository A projection contains only A-linked items
saved + newer draft has both flags true
manual association without evidence keeps repositoryGrounded false
available persisted revision evidence returns true
results sort by updatedAt descending
```

Run:

```bash
pnpm --filter @multica/core exec vitest run designs/repository-read-matrix.test.ts
```

Expected: PASS.

- [ ] **Step 3: Protect current Views from accidental migration**

Add a regression assertion to `designs-page.test.tsx` that the existing page still renders project content with the unchanged default query path. Do not introduce Finder controls or repository UI in this plan.

Run:

```bash
pnpm --filter @multica/views exec vitest run designs/designs-page.test.tsx
```

Expected: PASS.

- [ ] **Step 4: Run the full focused gate**

```bash
make sqlc
git status --porcelain
cd server
go test ./internal/handler -run 'Design(File|Document|Plugin|ProjectResource|ProjectDesignSystem|RepositoryReadMatrix)' -count=1
go test ./internal/service -run 'Design(Context|Document|ProjectDesignSystem)' -count=1
go build ./...
cd ..
pnpm --filter @multica/core typecheck
pnpm --filter @multica/core test
pnpm --filter @multica/views exec vitest run designs/designs-page.test.tsx
git diff --check
```

Expected:

```text
make sqlc leaves no tracked diff
all focused Go tests pass
Core typecheck/tests pass
Views regression passes
worktree contains only the validation report before commit
```

Run the repository-wide gate and keep the log outside the worktree:

```bash
make check-worktree > /tmp/design-center-repository-read-projection-check.log 2>&1
```

If the known unrelated E2E baseline failures remain, record them separately in the validation report and prove that no failing file is in this plan's diff. Any new migration, Go, Core, or focused repository-read failure blocks the task.

- [ ] **Step 5: Write the validation report**

`m1-slice-2a-validation.md` must record:

```text
branch and commit range
migration 909 evidence
repository_grounded truth table
backend repository matrix
Core projection matrix
focused command results
known N+1 revision lookup limitation
explicitly not implemented: Finder/UI/association dialog/fallback removal/template retirement/Electron
```

- [ ] **Step 6: Final GitNexus compare, commit and stop**

```bash
node .gitnexus/run.cjs analyze . --index-only --name multica-design-center-repository-read-projection-final
node .gitnexus/run.cjs detect-changes --scope compare --base-ref main --repo multica-design-center-repository-read-projection-final
git add \
  server/internal/handler/design_repository_read_matrix_test.go \
  packages/core/designs/repository-read-matrix.test.ts \
  packages/views/designs/designs-page.test.tsx \
  docs/product/design-center/m1-slice-2a-validation.md
git diff --cached --check
git commit -m "test(designs): verify repository read projection"
```

Stop after Task 8. Do not start Finder UI, association UI, design-system fallback removal, template retirement, or Electron work without a separately approved plan.

---

## Self-Review

- **Spec coverage:** Tasks 1–3 close the `repository_grounded` evidence semantics that the prior Slice 1 plan deferred. Task 4 adds exact server repository reads for both entities. Tasks 5–7 add typed Core contracts, scope-safe caching, and the unified list projection. Task 8 proves the read-only matrix and explicitly stops before Slice 2B UI.
- **Placeholder scan:** No unresolved markers, no cross-task shorthand, and no unspecified implementation step. Every GitNexus repository alias is fixed per task.
- **Type consistency:** `DesignAssetScope`, `DesignAssetListItem`, scoped API method names, query keys, and repository list signatures are defined before later tasks consume them.
- **Boundary check:** The plan does not implement Finder controls, repository tabs/workspaces, association dialog/menu, query invalidation events, repository design-system fallback removal, template retirement, or Electron acceptance.
- **Known cost:** Task 3 introduces one selected-revision lookup per Design Document response. This is accepted for semantic correctness in Slice 2A and must be measured before Slice 2B; do not replace it with `project_resource_id` inference.
