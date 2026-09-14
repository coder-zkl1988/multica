# Design File Repository Scope 实施计划（M1 Slice 1）

**Goal:** 为 `design_file` 增加可空 `project_resource_id` 仓库关联，为 `design_document` 复用该字段补充更新能力，实现统一批量仓库关联 API 与仓库解绑清理。

**Architecture:** 纯数据层与后端契约，不改 UI。`design_file` 新增字段并加并发索引；`design_document` 复用已有字段，补一个带活动任务保护的更新 query；新增一个领域 handler 在事务内原子更新两类资产，并接入现有项目资源删除清理路径。

**Tech Stack:** Go、sqlc、PostgreSQL/pgx、net/http/httptest。

**Spec:** `docs/superpowers/specs/2026-08-26-design-center-project-repository-views-m1-design.md`

## Global Constraints

- Migration 编号固定为 `906`、`907`、`908`，均属于 fork 800+ 保留区间；实施前只校验编号未被占用。
- 不创建数据库 foreign key，不级联删除；关系校验与清理在应用层事务内完成。
- 所有新增索引必须 `CREATE INDEX CONCURRENTLY`，每个并发索引单独一个 migration 文件。
- 迁移文件必须双向（`.up.sql` + `.down.sql`）。
- 网络字段 `snake_case`，Go 结构体 JSON tag 用 `snake_case`；TS 内部 camelCase（本 Slice 不涉及 TS）。
- 每个 task 结束时 `git diff --check` 无输出，`go test ./internal/handler -run 'Design' -count=1` 通过后再提交。
- 仓库关联是可空、最多一个、人工维护；一个资产最多关联一个仓库。
- 仓库必须属于资产所在项目，且 `resource_type='github_repo'`。

---

### Task 1: Migration 906 — design_file 增加 project_resource_id

**Files:**
- Create: `server/migrations/906_design_file_repository_scope.up.sql`
- Create: `server/migrations/906_design_file_repository_scope.down.sql`

**Interfaces:**
- Produces: `design_file.project_resource_id UUID NULL`，后续 query 与 handler 依赖该列存在。

- [ ] **Step 1: 写 up migration**

```sql
-- design_file gains an optional repository association (DC-052 supersedes the
-- project-only model). NULL means the design is not yet linked to a
-- repository; a non-NULL value links it to exactly one github_repo under the
-- same project. No foreign key per repository policy: clearing on
-- project_resource delete is done in an application transaction.
ALTER TABLE design_file
    ADD COLUMN project_resource_id UUID;
```

- [ ] **Step 2: 写 down migration**

```sql
ALTER TABLE design_file
    DROP COLUMN IF EXISTS project_resource_id;
```

- [ ] **Step 3: 校验 SQL 可执行**

Run:
```bash
cd /Users/fengyujie/Documents/soyoung/multica/server
psql "$DATABASE_URL" -f migrations/906_design_file_repository_scope.up.sql -v ON_ERROR_STOP=1
```
Expected: 退出码 0，无错误。

- [ ] **Step 4: 提交**

```bash
cd /Users/fengyujie/Documents/soyoung/multica
git add server/migrations/906_design_file_repository_scope.up.sql server/migrations/906_design_file_repository_scope.down.sql
git commit -m "feat(designs): add design_file project_resource_id column"
```

---

### Task 2: Migration 907/908 — 仓库范围并发索引

**Files:**
- Create: `server/migrations/907_idx_design_file_repository_scope.up.sql`
- Create: `server/migrations/907_idx_design_file_repository_scope.down.sql`
- Create: `server/migrations/908_idx_design_document_repository_scope.up.sql`
- Create: `server/migrations/908_idx_design_document_repository_scope.down.sql`

**Interfaces:**
- Consumes: Task 1 的 `design_file.project_resource_id`；`design_document.project_resource_id` 已存在（迁移 880）。
- Produces: 两个 partial index，供仓库精确查询使用。

- [ ] **Step 1: 写 907 up**

```sql
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_design_file_repository_scope
    ON design_file (workspace_id, project_id, project_resource_id, updated_at DESC)
    WHERE project_resource_id IS NOT NULL;
```

- [ ] **Step 2: 写 907 down**

```sql
DROP INDEX CONCURRENTLY IF EXISTS idx_design_file_repository_scope;
```

- [ ] **Step 3: 写 908 up**

```sql
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_design_document_repository_scope
    ON design_document (workspace_id, project_id, project_resource_id, updated_at DESC)
    WHERE project_resource_id IS NOT NULL;
```

- [ ] **Step 4: 写 908 down**

```sql
DROP INDEX CONCURRENTLY IF EXISTS idx_design_document_repository_scope;
```

- [ ] **Step 5: 校验并发索引可执行**

Run:
```bash
cd /Users/fengyujie/Documents/soyoung/multica/server
psql "$DATABASE_URL" -f migrations/907_idx_design_file_repository_scope.up.sql -v ON_ERROR_STOP=1
psql "$DATABASE_URL" -f migrations/908_idx_design_document_repository_scope.up.sql -v ON_ERROR_STOP=1
```
Expected: 退出码 0，无错误。

- [ ] **Step 6: 提交**

```bash
cd /Users/fengyujie/Documents/soyoung/multica
git add server/migrations/907_idx_design_file_repository_scope.up.sql server/migrations/907_idx_design_file_repository_scope.down.sql server/migrations/908_idx_design_document_repository_scope.up.sql server/migrations/908_idx_design_document_repository_scope.down.sql
git commit -m "feat(designs): add repository scope indexes"
```

---

### Task 3: design_file 查询支持 project_resource_id

**Files:**
- Modify: `server/pkg/db/queries/design.sql:108-123`（CreateDesignFile / UpdateDesignFile）
- Create: 新增 `SetDesignFileRepository` 与 `ListDesignFilesByRepository` 两个 query

**Interfaces:**
- Produces:
  - `db.SetDesignFileRepositoryParams{ID, WorkspaceID, ProjectResourceID pgtype.UUID}` → `db.DesignFile`
  - `db.ListDesignFilesByRepositoryParams{WorkspaceID, ProjectID, ProjectResourceID}` → `[]db.DesignFile`
- 后续 Task 5 的 handler 调用这两个 query。

- [ ] **Step 1: 在 design.sql 追加两个 query**

在 `-- name: UpdateDesignFile :one` 块之后追加：

```sql
-- name: SetDesignFileRepository :one
UPDATE design_file SET
    project_resource_id = sqlc.narg('project_resource_id'),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: ListDesignFilesByRepository :many
SELECT df.* FROM design_file df
WHERE df.workspace_id = $1
  AND df.project_id = $2
  AND df.project_resource_id = $3
  AND COALESCE(df.source_ref->>'asset_type', '') NOT IN ('template', 'design_system')
  AND NOT EXISTS (
    SELECT 1 FROM design_system_profile dsp
    WHERE dsp.source_file_id = df.id AND dsp.status <> 'archived'
  )
  AND NOT EXISTS (
    SELECT 1 FROM design_template_revision dtr
    WHERE EXISTS (
      SELECT 1 FROM design_revision dr
      WHERE dr.id = dtr.design_revision_id AND dr.file_id = df.id
    )
  )
ORDER BY updated_at DESC, created_at DESC;
```

- [ ] **Step 2: 重新生成 sqlc**

Run:
```bash
cd /Users/fengyujie/Documents/soyoung/multica/server
make sqlc
```
Expected: 生成 `pkg/db/generated/design.sql.go` 更新，包含 `SetDesignFileRepository` 与 `ListDesignFilesByRepository`。

- [ ] **Step 3: 校验编译**

Run:
```bash
cd /Users/fengyujie/Documents/soyoung/multica/server
go build ./...
```
Expected: 退出码 0。

- [ ] **Step 4: 提交**

```bash
cd /Users/fengyujie/Documents/soyoung/multica
git add server/pkg/db/queries/design.sql server/pkg/db/generated/design.sql.go
git commit -m "feat(designs): add design_file repository queries"
```

---

### Task 4: design_document 仓库更新 query（活动任务保护）

**Files:**
- Modify: `server/pkg/db/queries/design_document.sql`（追加 `SetDesignDocumentRepository`）

**Interfaces:**
- Produces: `db.SetDesignDocumentRepositoryParams{ID, WorkspaceID, ProjectResourceID pgtype.UUID}` → `db.DesignDocument`
- 约束：`active_task_id IS NULL` 才允许更新，返回 0 行表示有活动任务。

- [ ] **Step 1: 在 design_document.sql 追加 query**

```sql
-- Repository scope is an intentional, human-managed link (DC-052). It may
-- change only while no generation/adjust/regenerate task is running: a live
-- run has already pinned its own repository input.
-- name: SetDesignDocumentRepository :one
UPDATE design_document SET
    project_resource_id = sqlc.narg('project_resource_id'),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND active_task_id IS NULL
RETURNING *;
```

- [ ] **Step 2: 重新生成 sqlc**

Run:
```bash
cd /Users/fengyujie/Documents/soyoung/multica/server
make sqlc
```
Expected: `pkg/db/generated/design_document.sql.go` 包含 `SetDesignDocumentRepository`。

- [ ] **Step 3: 校验编译**

Run:
```bash
cd /Users/fengyujie/Documents/soyoung/multica/server
go build ./...
```
Expected: 退出码 0。

- [ ] **Step 4: 提交**

```bash
cd /Users/fengyujie/Documents/soyoung/multica
git add server/pkg/db/queries/design_document.sql server/pkg/db/generated/design_document.sql.go
git commit -m "feat(designs): add guarded design_document repository query"
```

---

### Task 5: 统一批量仓库关联 handler

**Files:**
- Create: `server/internal/handler/design_asset_repository_association.go`
- Create: `server/internal/handler/design_asset_repository_association_test.go`
- Modify: `server/cmd/server/router.go`（注册路由）

**Interfaces:**
- Consumes: Task 3 的 `SetDesignFileRepository`/`ListDesignFilesByRepository`，Task 4 的 `SetDesignDocumentRepository`，以及已有 `projectResourceBelongsToProject`、`parseUUIDOrBadRequest`、`writeProjectDesignSystemError`、`h.TxStarter`。
- Produces: `PUT /api/design-assets/repository-association`，请求体 `{project_id, project_resource_id, items:[{kind,id}]}`。

- [ ] **Step 1: 写失败测试**

创建 `design_asset_repository_association_test.go`，写入一个测试骨架并先跑一个“未注册路由”用例：

```go
package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSetDesignAssetRepositoryAssociationRouteExists(t *testing.T) {
	req := httptest.NewRequest(http.MethodPut,
		"/api/design-assets/repository-association",
		strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	testRouter.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Fatalf("route not registered: %d", rec.Code)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run:
```bash
cd /Users/fengyujie/Documents/soyoung/multica/server
go test ./internal/handler -run TestSetDesignAssetRepositoryAssociationRouteExists -count=1
```
Expected: FAIL，提示 route 未注册或 handler 不存在。

- [ ] **Step 3: 实现 handler**

创建 `design_asset_repository_association.go`：

```go
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type designAssetRepositoryAssociationRequest struct {
	ProjectID         string                                  `json:"project_id"`
	ProjectResourceID string                                  `json:"project_resource_id"`
	Items             []designAssetRepositoryAssociationItem  `json:"items"`
}

type designAssetRepositoryAssociationItem struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

const (
	designAssetKindDesignFile     = "design_file"
	designAssetKindDesignDocument = "design_document"
)

func (h *Handler) SetDesignAssetRepositoryAssociation(w http.ResponseWriter, r *http.Request) {
	var req designAssetRepositoryAssociationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	workspaceID, requesterID, ok := h.projectDesignSystemRequestScope(w, r)
	if !ok {
		return
	}
	projectID, ok := parseUUIDOrBadRequest(w, req.ProjectID, "project_id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: projectID, WorkspaceID: workspaceID,
	}); err != nil {
		writeProjectDesignSystemError(w, http.StatusNotFound, "project_not_found", "project not found")
		return
	}
	resourceID := pgtype.UUID{}
	if strings.TrimSpace(req.ProjectResourceID) != "" {
		parsed, ok := parseUUIDOrBadRequest(w, req.ProjectResourceID, "project_resource_id")
		if !ok {
			return
		}
		resourceID = parsed
	}
	if resourceID.Valid && !h.projectResourceBelongsToProject(r.Context(), w, workspaceID, projectID, resourceID) {
		return
	}
	if resourceID.Valid {
		resource, err := h.Queries.GetProjectResourceInWorkspace(r.Context(), db.GetProjectResourceInWorkspaceParams{
			ID: resourceID, WorkspaceID: workspaceID,
		})
		if err == nil && resource.ResourceType != "github_repo" {
			writeProjectDesignSystemError(w, http.StatusBadRequest, "project_resource_not_repository", "resource is not a code repository")
			return
		}
	}
	if len(req.Items) == 0 {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "items_required", "at least one design asset is required")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "transaction_failed", "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	queries := h.Queries.WithTx(tx)
	for _, item := range req.Items {
		switch item.Kind {
		case designAssetKindDesignFile:
			fileID, ok := parseUUIDOrBadRequest(w, item.ID, "id")
			if !ok {
				return
			}
			if _, err := queries.SetDesignFileRepository(r.Context(), db.SetDesignFileRepositoryParams{
				ID: fileID, WorkspaceID: workspaceID, ProjectResourceID: resourceID,
			}); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					writeProjectDesignSystemError(w, http.StatusNotFound, "design_asset_not_found", "design file not found")
					return
				}
				writeProjectDesignSystemError(w, http.StatusInternalServerError, "repository_association_failed", "failed to update design file")
				return
			}
		case designAssetKindDesignDocument:
			docID, ok := parseUUIDOrBadRequest(w, item.ID, "id")
			if !ok {
				return
			}
			if _, err := queries.SetDesignDocumentRepository(r.Context(), db.SetDesignDocumentRepositoryParams{
				ID: docID, WorkspaceID: workspaceID, ProjectResourceID: resourceID,
			}); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					writeProjectDesignSystemError(w, http.StatusConflict, "design_document_task_active", "design document has an active task")
					return
				}
				writeProjectDesignSystemError(w, http.StatusInternalServerError, "repository_association_failed", "failed to update design document")
				return
			}
		default:
			writeProjectDesignSystemError(w, http.StatusBadRequest, "design_asset_kind_invalid", "unknown design asset kind")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "transaction_failed", "failed to commit transaction")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"project_id":         uuidToString(projectID),
		"project_resource_id": uuidToString(resourceID),
		"count":              len(req.Items),
	})
}
```

- [ ] **Step 4: 注册路由**

在 `router.go` 中设计相关 Route 内注册：

```go
r.Put("/api/design-assets/repository-association", h.SetDesignAssetRepositoryAssociation)
```

- [ ] **Step 5: 跑测试确认通过**

Run:
```bash
cd /Users/fengyujie/Documents/soyoung/multica/server
go test ./internal/handler -run TestSetDesignAssetRepositoryAssociationRouteExists -count=1
```
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
cd /Users/fengyujie/Documents/soyoung/multica
git add server/internal/handler/design_asset_repository_association.go server/internal/handler/design_asset_repository_association_test.go server/cmd/server/router.go
git commit -m "feat(designs): add batch repository association endpoint"
```

---

### Task 6: 仓库解绑清理

**Files:**
- Modify: `server/internal/handler/project_resource_cleanup.go`（或实际清理文件，按现有 `project_resource_design_cleanup_test.go` 指向的实现文件）
- Modify: `server/internal/handler/project_resource_design_cleanup_test.go`

**Interfaces:**
- Consumes: Task 3/4 的字段；现有清理事务函数。
- Produces: 删除 `project_resource` 时，在同一事务内将关联 `design_file.project_resource_id` 与 `design_document.project_resource_id` 置空。

- [ ] **Step 1: 在清理事务内追加两条清理 SQL**

在现有 `project_resource` 删除清理路径（`projectResourceBelongsToProject` 删除处的应用事务）内追加：

```go
_, err := queries.DetachDesignFilesFromProjectResource(ctx, db.DetachDesignFilesFromProjectResourceParams{
    WorkspaceID: workspaceID, ProjectResourceID: resourceID,
})
if err != nil {
    return err
}
_, err = queries.DetachDesignDocumentsFromProjectResource(ctx, db.DetachDesignDocumentsFromProjectResourceParams{
    WorkspaceID: workspaceID, ProjectResourceID: resourceID,
})
if err != nil {
    return err
}
```

- [ ] **Step 2: 在 design.sql / design_document.sql 追加 detach query**

```sql
-- name: DetachDesignFilesFromProjectResource :exec
UPDATE design_file SET project_resource_id = NULL
WHERE workspace_id = $1 AND project_resource_id = $2;

-- name: DetachDesignDocumentsFromProjectResource :exec
UPDATE design_document SET project_resource_id = NULL
WHERE workspace_id = $1 AND project_resource_id = $2;
```

- [ ] **Step 3: 重新生成 sqlc 并编译**

Run:
```bash
cd /Users/fengyujie/Documents/soyoung/multica/server
make sqlc
go build ./...
```
Expected: 退出码 0。

- [ ] **Step 4: 扩展清理测试断言**

在 `project_resource_design_cleanup_test.go` 中新增一个用例，插入 `design_file` 与 `design_document`（`project_resource_id` 指向目标仓库），调用删除资源 handler，断言两行的 `project_resource_id` 均变为 NULL、且行未被删除。

- [ ] **Step 5: 跑清理测试**

Run:
```bash
cd /Users/fengyujie/Documents/soyoung/multica/server
go test ./internal/handler -run 'TestDeleteProjectResource|TestDetachDesign' -count=1
```
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
cd /Users/fengyujie/Documents/soyoung/multica
git add server/pkg/db/queries/design.sql server/pkg/db/queries/design_document.sql server/pkg/db/generated server/internal/handler/project_resource_cleanup.go server/internal/handler/project_resource_design_cleanup_test.go
git commit -m "feat(designs): detach design repository links on resource delete"
```

---

## Self-Review

- **Spec coverage:** 覆盖 M1 Spec 第 16 节（906/907/908 migration）与第 9 节（design_file 加字段、design_document 复用、仓库解绑清理）以及第 14.1（统一批量关联 API）。`repository_grounded` 语义修正在统一还原子 Spec 中实现，不属于本 Slice。
- **Placeholder scan:** 无 TBD/TODO；每个 query 与 handler 均给出实际代码。
- **Type consistency:** `SetDesignFileRepository` / `SetDesignDocumentRepository` / `DetachDesignFilesFromProjectResource` / `DetachDesignDocumentsFromProjectResource` 在 query 与 handler 中命名一致。

## Test Coverage（Task 5/6 执行时以 TDD 先写）

Task 5 需补齐以下用例，均使用现有 `testPool`/`testRouter`/`testWorkspaceID`/`testUserID` fixture，参照 `project_resource_design_cleanup_test.go` 的插数/清理模式：

- `TestSetDesignAssetRepositoryAssociationRejectsCrossProject`
- `TestSetDesignAssetRepositoryAssociationRejectsNonRepository`
- `TestSetDesignAssetRepositoryAssociationRollsBackOnSecondItemFailure`
- `TestSetDesignAssetRepositoryAssociationClearsOnEmptyResource`

Task 6 需补齐：

- `TestDeleteProjectResourceDetachesDesignFileRepository`
- `TestDeleteProjectResourceDetachesDesignDocumentRepository`
- `TestDeleteProjectResourceKeepsDesignRows`

每个用例遵循：插数 → 调用 handler/清理函数 → 断言结果 → `t.Cleanup` 删除插入行。
