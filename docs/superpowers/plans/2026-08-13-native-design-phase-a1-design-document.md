# Native Design Phase A1：Design Document 核心协议与持久化实施计划

> **For Codex:** 获得用户批准后，按任务顺序逐项实施；每项先写失败测试，再写最小实现。A1 完成并提交阶段报告后停止，不进入 A2。

**Goal:** 在不破坏现有 `design_draft`、项目设计体系或 Open Design 历史数据的前提下，落地 `multica.design-document/v1` 的安全收集与重验、Design Document 稳定身份、不可变 revision、文档级 input snapshot、draft/saved 指针及对象存储基础。

**Architecture:** 新建 `designdocument` 领域包和三张独立表，不扩展可变的 `design_draft`，也不把 `project_design_system_package` 的覆盖式 slot 当成 revision。先从 `projectdesignsystem` 抽取包格式无关的 JSON digest、文件收集、ZIP 重验和确定性摘要 helper，保持 V2 外部行为不变，再让 Design Document 协议复用这些 helper。A1 只提供内部 Go/SQL primitives；A2 接 task 创建，A4 接 daemon/completion/Audit/Preview，A5 接保存与放弃。

**Tech Stack:** Go 1.24、PostgreSQL、sqlc、标准库 `archive/zip` / `crypto/sha256` / `encoding/json`、已安装的 `github.com/tdewolff/parse/v2` lexer、现有 `server/internal/storage`、现有 Go 测试和 GitNexus。

---

## 0. 已确认基线与边界

### 0.1 Git 基线

- 计划基线：`origin/main`，commit `97c6069c3`。
- 当前专用分支：`agent/codex-fe/8c579eaa`，相对 `origin/main` 为 ahead 0 / behind 0。
- `main` 已包含 `535e70f3f Feature/fengchen (#22)`；不再依赖不存在的远端 `feature/fengchen`。
- 制定计划前暂存区和工作区均为空；本文件是唯一计划产物。
- 不使用、合并或 cherry-pick `feature/fengchen-fixed-v2`。

### 0.2 代码盘点结论

| 现有能力 | 证据 | A1 决策 |
| --- | --- | --- |
| 历史页面草稿 | `server/migrations/234_gallery_native_designs.up.sql:102`、`server/migrations/874_semantic_design_draft.up.sql:18`、`server/pkg/db/queries/design.sql:398` | 保留全部表、查询、API 和消费者；A1 不停写、不迁移旧行。 |
| 项目设计体系 slot | `server/migrations/864_project_design_system.up.sql:22`、`server/pkg/db/queries/design.sql:1212` | 只复用事务和租户校验经验；其 upsert/覆盖语义不能承载不可变 revision。 |
| 安全 package 收集 | `server/internal/projectdesignsystem/v2_archive.go:52` | 抽取格式无关 helper 后由 V2 和新协议共同调用，避免复制 ZIP/path/digest 安全代码。 |
| input snapshot digest | `server/internal/projectdesignsystem/v2_archive.go:35` | 抽取为共享 canonical JSON digest；V2 结果必须逐字节兼容。 |
| 对象存储 | `server/internal/storage/storage.go:9`、`server/internal/handler/project_design_system_package_upload.go:86` | 直接复用 `storage.Storage.Upload/GetReader` 和稳定、按 digest 定址的 object key 规则；不新增存储接口。 |
| Audit 框架 | `server/internal/projectdesignsystem/v2_types.go:77`、`server/internal/projectdesignsystem/v2_audit.go:33` | 复用诊断形状和安全检查思路；A1 新增 Design Document 的结构/静态安全 Audit，不调用旧 Worker。 |
| 浏览器 Preview | `server/internal/designpreview/policy.go:25` | A1 不调用也不修改；A4 扩展交互检查并持久化 Preview receipt。 |
| completion/失败隔离 | `server/internal/handler/project_design_system_completion.go:171` | A1 只准备可原子调用的 SQL primitives；A4 才接 task completion，失败不得创建 document/revision 或移动指针。 |

### 0.3 A1 范围

**包含：**

- `multica.design-document/v1` 的 manifest/brief/coverage Go schema；
- 目录收集、确定性 ZIP、文件索引、逐文件 digest、content digest、服务端重验；
- 静态 Audit 基础：schema、结构、路径、大小、媒体类型、外部引用/网络/Service Worker/凭据/真实 API 风险和跨文件引用一致性；
- `design_document`、`design_document_input_snapshot`、`design_document_revision`；
- document 的 draft/saved 指针和首个 revision 原子插入 primitive；
- workspace/project/可选 Issue/task、base revision/digest 和 saved 设计体系引用不变量；
- 对象存储 upload/load primitive；
- project/workspace delete 与 Issue detach 的应用层关系维护。

**不包含：**

- 首页表单、新页面 task HTTP API、进行中 task 区（A2）；
- task 内仓库 Grounding 和持续 workspace（A3）；
- daemon finalize、真实 completion 接线、浏览器 Preview、receipt 持久化、首个用户 draft（A4）；
- 调整 API、base conflict、保存/放弃和文档 UI（A5）；
- 真实 Agent、真实仓库 Grounding、User Chrome、人工视觉验收（A6）；
- 共享设计体系或模板 Slice B-E；
- 任何旧 Worker/Runtime/SSE/Supervisor 恢复；
- 历史行、对象、表或约束删除。

### 0.4 API 与前端范围

- 新增公网/daemon/MCP API：**无**。
- 修改 `packages/core` 或 `packages/views`：**无**。
- 因此 A1 不新增 zod schema、`parseWithFallback` 或 malformed response 测试；这些在 A2/A5 首次新增相应 API 时成为硬门禁。

---

## 1. 数据模型与不变量

### 1.1 为什么新建表

`design_draft` 是带 `updated_at` 的可变行，并承载 legacy patch 与 `semantic_pagespec`；`project_design_system_package` 以 `(design_system_id, slot)` 唯一并通过 upsert 原地替换。二者都无法同时满足多文档、不可变 revision、draft/saved 指针和完整 package provenance。A1 因此新增独立模型，不修改两条历史链路的 schema 或读写行为。

### 1.2 表结构

#### `design_document`

```sql
id                  UUID NOT NULL DEFAULT gen_random_uuid()
workspace_id        UUID NOT NULL
project_id          UUID NOT NULL
issue_id            UUID NULL
title               TEXT NOT NULL
draft_revision_id   UUID NULL
saved_revision_id   UUID NULL
created_by          UUID NULL
created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
```

- `id` 是稳定资产身份；同一项目允许多行。
- Issue 只做可选关联；删除 Issue 时置空，不删除文档。
- A1 不新增 `status`、`active_task_id` 或 workspace 路径字段：前两者分别属于 A2/A5，持续 workspace 属于 A3。
- 指针只能由受控事务查询更新，并要求目标 revision 同 workspace/project/document；A1 不提供通用 `UPDATE revision`。

#### `design_document_input_snapshot`

```sql
id                    UUID NOT NULL DEFAULT gen_random_uuid()
workspace_id          UUID NOT NULL
project_id            UUID NOT NULL
issue_id              UUID NULL
task_id               UUID NOT NULL
agent_id              UUID NOT NULL
target_platform       TEXT NULL
schema_version        TEXT NOT NULL
snapshot              JSONB NOT NULL
snapshot_sha256       TEXT NOT NULL
base_revision_id      UUID NULL
base_content_digest   TEXT NULL
design_system_id              UUID NULL
design_system_source_task_id  UUID NULL
design_system_content_digest  TEXT NULL
created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
```

- 每个 task 固定一份快照；只提供 insert/get，不提供 update。
- `snapshot_sha256` 必须是对 canonical JSON 的 `sha256:` 摘要，由 Go 重算后写入。
- `base_revision_id` 与 `base_content_digest` 必须同时为空或同时存在；有 base 时必须属于同一 document（在插入 revision 的事务中校验）。
- 当前 `project_design_system_package` 是可覆盖 saved slot，没有可诚实引用的 immutable revision ID；A1 不伪造该概念，而以 `design_system_id + design_system_source_task_id + design_system_content_digest` 三元组固定实际使用的 saved package。三者必须同空同存，后续若项目设计体系获得正式 revision，再通过新协议版本演进。
- A3 再给 `snapshot` 增加仓库 Grounding 内容，不改 A1 表结构。

#### `design_document_revision`

```sql
id                    UUID NOT NULL
document_id           UUID NOT NULL
workspace_id          UUID NOT NULL
project_id            UUID NOT NULL
input_snapshot_id     UUID NOT NULL
source_task_id        UUID NOT NULL
base_revision_id      UUID NULL
schema_version        TEXT NOT NULL
manifest              JSONB NOT NULL
artifact_index        JSONB NOT NULL
archive_object_key    TEXT NOT NULL
content_digest        TEXT NOT NULL
created_by_agent_id   UUID NOT NULL
created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
-- uniqueness is attached by migration 889 using a prebuilt concurrent index
```

- revision 行追加且不可变：sqlc 只生成 create/get/list 查询，没有业务 update/delete 查询；数据库触发器拒绝 snapshot/revision 的 `UPDATE`，关系清理只使用 `DELETE`。
- `id` 在收集前生成并写入 manifest，服务端重验后原样持久化。
- `manifest`、`artifact_index`、object key 和 digest 必须来自同一份经重验 archive。
- 同一文档允许不同 task/revision 产生相同业务 content digest；重新取证即使没有视觉变化也仍需保留新的 input snapshot 和 revision provenance。
- Audit/Preview receipts 不进入 revision 或 package，避免 digest 循环；A4 单独建证据表并绑定 `revision_id + content_digest`。

### 1.3 关系规则

- 新迁移不创建 foreign key、`REFERENCES` 或 cascade；所有关系由 workspace-scoped SQL `EXISTS` 和事务锁校验。
- project 删除：删除该项目的 document revisions、snapshots、documents；A1 不删除对象存储对象。
- workspace 删除：在 design teardown 阶段按 revision → snapshot → document 顺序删除。
- Issue 删除：仅将可变的 `design_document.issue_id` 置空；immutable input snapshot 的 `issue_id`、snapshot JSON 和 revision manifest 都保留创建时 provenance。
- task 删除：历史 revision/snapshot 保留 `source_task_id` 值作为软引用，不级联。
- 下游在 A5 接入前没有 Design Document 读取入口；接入后只能读取 `saved_revision_id`。

### 1.4 迁移编号

现有最高前缀为 876。为了明确避开已废弃计划中的 `877`，A1 从 **878** 起使用新前缀，并保留 877 不执行：

- `878_design_document`：三张表、CHECK 约束和 snapshot/revision immutable-update triggers；所有待约束的 `id` 均为 `NOT NULL`，不含 PRIMARY KEY、UNIQUE 或其他会隐式建索引的约束；
- `879_idx_design_document_project`、`880_idx_design_document_issue`、`881_idx_design_document_revision_document`、`882_idx_design_document_snapshot_project`：四条查询索引，各自单条 `CREATE INDEX CONCURRENTLY`；
- `883_idx_design_document_id`、`884_idx_design_document_input_snapshot_id`、`885_idx_design_document_revision_id`、`886_idx_design_document_input_snapshot_task_id`、`887_idx_design_document_revision_source_task_id`、`888_idx_design_document_revision_input_snapshot_id`：六条唯一索引，各自单条 `CREATE UNIQUE INDEX CONCURRENTLY`；
- `889_design_document_constraints`：使用上述六个唯一索引通过 `USING INDEX` 附加三条 PRIMARY KEY 和三条 UNIQUE 约束，不建索引。

每个 migration 都有配对 down 文件；879–888 的 up/down 各自只有一条 concurrent index 语句。889 通过 `USING INDEX` 附加约束后，PostgreSQL 会使六个唯一索引由约束拥有并重命名；889 down 的 `DROP CONSTRAINT` 会删除这六个 backing indexes，故完整回滚中 883–888 的 `DROP INDEX CONCURRENTLY IF EXISTS` 是有意的 no-op，只有 879–882 down 实际删除四个仍独立的查询索引。回滚顺序仍为 889 → 888 → … → 879 → 878，最后由 878 删除表和 trigger function。down 只用于尚未承载用户数据的 A1 独立回滚，生产产生数据后不得用 down 作为历史数据清理手段。

---

## 2. Package 协议

### 2.1 `manifest.json`

```json
{
  "schema_version": "multica.design-document/v1",
  "document_id": "uuid",
  "revision_id": "uuid",
  "workspace_id": "uuid",
  "project_id": "uuid",
  "issue_id": "uuid or omitted",
  "task_id": "uuid",
  "agent_id": "uuid",
  "target_platform": "web|mobile|cross_platform or omitted",
  "input_snapshot_sha256": "sha256:...",
  "base_revision_id": "uuid or omitted",
  "base_content_digest": "sha256:... or omitted",
  "design_system_id": "uuid or omitted",
  "design_system_source_task_id": "uuid or omitted",
  "design_system_content_digest": "sha256:... or omitted",
  "files": [{"path":"brief.json","role":"brief","media_type":"application/json","size_bytes":1,"sha256":"..."}],
  "content_digest": "sha256:...",
  "prototype_entry": "prototype/index.html",
  "preview_targets": [{"id":"main","kind":"page","path":"prototype/index.html"}]
}
```

- `manifest.json` 不进入 `files` 和 content digest；digest 只覆盖排序后的业务文件索引，避免自引用。
- digest 算法沿用 Native V2 的 length-prefixed `path + media_type + size + sha256`，保持确定性并避免分隔符歧义。
- 目录至少包含 `brief.json`、`prototype/index.html`、`prototype/styles.css`、`prototype/app.js`、`coverage.json`；`assets/` 可为空，其他 `prototype/` 内文件可由 manifest 声明。
- 所有路径必须是正斜杠相对路径；拒绝绝对路径、反斜杠、`.`/`..`、重复路径、符号链接、硬链接和非普通文件。

### 2.2 `brief.json` 与 `coverage.json`

- 用严格 JSON decoder（未知字段报错、单一 JSON 值、大小有界）。
- brief 固定文档目标、pages/states/overlays/flows/scenarios/requirements/accessibility/non-goals，元素使用稳定非空 ID。
- coverage 固定 requirement/page/state/overlay/flow/design-system/interaction/template-residue coverage 和 uncovered reasons。
- A1 只验证结构、引用完整性和可确定的静态安全项，不替 Agent 自评背书，不把静态 Audit 描述成视觉通过。

### 2.3 Audit 与 Preview 边界

- A1 `AuditReport` 复用现有 `code/severity/path/message` 诊断形状，并绑定 `content_digest`。
- A1 静态检查 HTML/CSS/JS 文本中的外部 URL、CDN/remote font、`fetch`/XHR/WebSocket/EventSource、Service Worker、动态 import、外部脚本/样式、表单真实 action、凭据样式、绝对/家目录路径和 package 外资源。
- A1 不执行 JavaScript、不启动浏览器、不生成 Preview receipt；完整 DOM/网络/Console/交互门禁归 A4，继续复用 `server/internal/designpreview`。

---

## 3. 实施任务

### Task 1：抽取 package 通用安全 helper，保持 Native V2 行为不变

**Files:**

- Create: `server/internal/designpackage/archive.go`
- Create: `server/internal/designpackage/archive_test.go`
- Modify: `server/internal/projectdesignsystem/v2_archive.go:35-49,52-227,256-447,503-508,562-637`
- Test: `server/internal/projectdesignsystem/v2_archive_test.go`

**Step 1: 写失败测试**

- 在 `designpackage/archive_test.go` 覆盖 canonical JSON digest、排序文件索引、length-prefixed content digest、确定性 ZIP、ZIP bomb/重复路径/路径穿越/绝对路径、symlink/hardlink 和总大小限制。
- 在 V2 现有测试中增加 golden 断言：同 fixture 的 manifest、content digest、archive bytes 和错误码在抽取前后不变。

**Step 2: 运行 RED**

```bash
cd server && go test ./internal/designpackage ./internal/projectdesignsystem -run 'Test(CanonicalJSONDigest|CollectFiles|BuildDeterministicArchive|ReadArchive|CollectV2Directory|ValidateV2Archive)' -count=1 -v
```

预期：`designpackage` 尚不存在或新 API 尚未实现，测试失败。

**Step 3: 写最小实现**

- 仅抽取包格式无关的 `CanonicalJSONDigest`、安全目录遍历、ZIP 读写、path guard、文件上限、逐文件 SHA-256 和 content digest。
- 通过参数传入文件分类/大小策略；不为未来格式增加注册表、factory 或新依赖。
- V2 保留 `PackageBinding`、manifest、Audit、错误码和对外函数；其 wrapper 调用共享 helper。

**Step 4: 运行 GREEN 与 V2 回归**

```bash
cd server && go test ./internal/designpackage ./internal/projectdesignsystem -count=1 -v
cd server && go test ./internal/daemon ./internal/handler -run 'ProjectDesignSystem|NativePackage|PackagePreview' -count=1 -v
```

预期：新 helper 通过；V2 digest、upload、preview 与 completion 流程不变。

### Task 2：实现 `multica.design-document/v1` schema、收集、重验和静态 Audit

**Files:**

- Create: `server/internal/designdocument/types.go`
- Create: `server/internal/designdocument/schema.go`
- Create: `server/internal/designdocument/archive.go`
- Create: `server/internal/designdocument/audit.go`
- Create: `server/internal/designdocument/testdata/v1-valid/brief.json`
- Create: `server/internal/designdocument/testdata/v1-valid/prototype/index.html`
- Create: `server/internal/designdocument/testdata/v1-valid/prototype/styles.css`
- Create: `server/internal/designdocument/testdata/v1-valid/prototype/app.js`
- Create: `server/internal/designdocument/testdata/v1-valid/coverage.json`
- Create: `server/internal/designdocument/archive_test.go`
- Create: `server/internal/designdocument/audit_test.go`

**Step 1: 写失败测试**

- 正向：同一 fixture 两次收集产生相同 archive/manifest/content digest；重验后 binding、index、entry、preview targets 不变。
- 绑定负向：workspace/project/document/revision/task/agent/input/base/design-system system/source-task/digest 任一字段不匹配即拒绝。
- 结构负向：必需文件缺失、manifest 多余/未知字段、brief/coverage ID 重复或悬空引用、preview target 未声明。
- 安全负向：未知顶层文件、package 外资源、绝对/家目录路径、符号/硬链接、路径穿越、远程 URL/font/script/style、network API、WebSocket、Service Worker、真实 API 或凭据样式。
- 大小负向：压缩包、解压总量、单文件和文件数量超限均 fail closed。

**Step 2: 运行 RED**

```bash
cd server && go test ./internal/designdocument -count=1 -v
```

预期：新 package 尚未实现，测试失败。

**Step 3: 写最小实现**

- 定义 `SchemaVersion = "multica.design-document/v1"`、binding、manifest、brief、coverage、artifact index、preview target、diagnostic 和 audit report。
- `CollectDirectory` 先校验固定 binding 和业务 JSON，再使用 `designpackage` 收集、算 digest、生成 manifest 和确定性 ZIP，最后调用 `ValidateArchive` 自重验。
- `ValidateArchive` 从原始 bytes 重建索引和 digest，严格对比 manifest/binding，并重新执行 Audit；绝不信任 Agent 提供的 digest 或自评。
- HTML/CSS/JS 使用仓库已安装的结构化 lexer 做 token 级 allow/deny 校验；不以正则或子串扫描代替 trust-boundary 解析，也不新增 JavaScript 执行器或依赖。

**Step 4: 运行 GREEN**

```bash
cd server && go test ./internal/designdocument ./internal/designpackage -count=1 -v
```

预期：协议正向与所有静态失败门禁通过。

### Task 3：添加三张表与并发索引 migration

**Files:**

- Create: `server/migrations/878_design_document.up.sql`
- Create: `server/migrations/878_design_document.down.sql`
- Create: `server/migrations/879_idx_design_document_project.up.sql`
- Create: `server/migrations/879_idx_design_document_project.down.sql`
- Create: `server/migrations/880_idx_design_document_issue.up.sql`
- Create: `server/migrations/880_idx_design_document_issue.down.sql`
- Create: `server/migrations/881_idx_design_document_revision_document.up.sql`
- Create: `server/migrations/881_idx_design_document_revision_document.down.sql`
- Create: `server/migrations/882_idx_design_document_snapshot_project.up.sql`
- Create: `server/migrations/882_idx_design_document_snapshot_project.down.sql`
- Create: `server/migrations/883_idx_design_document_id.up.sql`
- Create: `server/migrations/883_idx_design_document_id.down.sql`
- Create: `server/migrations/884_idx_design_document_input_snapshot_id.up.sql`
- Create: `server/migrations/884_idx_design_document_input_snapshot_id.down.sql`
- Create: `server/migrations/885_idx_design_document_revision_id.up.sql`
- Create: `server/migrations/885_idx_design_document_revision_id.down.sql`
- Create: `server/migrations/886_idx_design_document_input_snapshot_task_id.up.sql`
- Create: `server/migrations/886_idx_design_document_input_snapshot_task_id.down.sql`
- Create: `server/migrations/887_idx_design_document_revision_source_task_id.up.sql`
- Create: `server/migrations/887_idx_design_document_revision_source_task_id.down.sql`
- Create: `server/migrations/888_idx_design_document_revision_input_snapshot_id.up.sql`
- Create: `server/migrations/888_idx_design_document_revision_input_snapshot_id.down.sql`
- Create: `server/migrations/889_design_document_constraints.up.sql`
- Create: `server/migrations/889_design_document_constraints.down.sql`
- Create: `server/internal/migrations/design_document_migration_test.go`

**Step 1: 写失败测试**

- 新增 A1 schema 测试，断言三张表、immutable triggers 和 paired-field CHECK 存在，878 不含 PRIMARY KEY/UNIQUE/FK/cascade，十个索引 migration 每份单语句且使用 `CONCURRENTLY`。
- 断言 889 使用预建唯一索引附加三条 PRIMARY KEY 和三条 UNIQUE，877 未新增、878-889 前缀唯一且 up/down 配对，并断言十个 cleanup mapping/hook。

**Step 2: 运行 RED**

```bash
cd server && env DATABASE_URL= go test ./internal/migrations -run 'Migration|DesignDocument|ForeignKeys|Indexes' -count=1 -v
```

预期：A1 migration 尚不存在，测试失败。

**Step 3: 写 migration**

- 878 只创建表、CHECK 和一个共用的 reject-update trigger function；待约束 `id` 使用 `NOT NULL`，不包含 PRIMARY KEY、UNIQUE 或 `CREATE INDEX`。
- SHA-256 字段用 CHECK `^sha256:[a-f0-9]{64}$`；base 两字段、design-system 三字段分别用 CHECK 保证同空同存；snapshot/revision 的 `BEFORE UPDATE` trigger 一律 fail closed。
- 879-888 每个 up/down 各一条 concurrent index statement；889 用 `USING INDEX` 附加约束，其 down 只删除约束。

**Step 4: 运行 GREEN**

```bash
cd server && env DATABASE_URL= go test ./internal/migrations -count=1 -v
```

预期：编号、无 FK 和 concurrent index 门禁全部通过。

### Task 4：添加 sqlc 持久化 primitives 和事务不变量测试

**Files:**

- Create: `server/pkg/db/queries/design_document.sql`
- Regenerate: `server/pkg/db/generated/design_document.sql.go`
- Regenerate: `server/pkg/db/generated/models.go`
- Regenerate: `server/pkg/db/generated/querier.go`
- Create: `server/internal/handler/design_document_persistence_test.go`

**Step 1: 写失败测试**

- `CreateDesignDocumentInputSnapshot`：project 必须属于 workspace；Issue 可空，有值时必须属于同 workspace/project；task/agent 与参数一致；digest/bases 成对。
- `CreateDesignDocumentWithFirstRevision`：单事务创建 document + immutable revision + draft pointer；archive binding 和 snapshot/document/project/workspace 必须一致。
- 失败隔离：任一不变量或第二步注入失败，三张表均不留下行；saved 指针保持 NULL。
- snapshot/revision 不可变：sqlc 表面无 update，直接 SQL `UPDATE` 也被 trigger 拒绝；重复 source task/input snapshot 被拒绝，但新 task 允许产生相同 content digest 的新 revision。
- `GetDesignDocumentRevisionInWorkspace` 和 list 查询不能跨 workspace/project。

**Step 2: 运行 RED**

```bash
make sqlc
cd server && go test ./internal/handler -run '^TestDesignDocumentPersistence' -count=1 -v
```

预期：新查询/模型尚未实现或断言失败。

**Step 3: 写最小查询**

- 只生成 snapshot/document/revision 的 create/get/list 和“首个 document + revision + draft”原子 primitive；不预建 A5 的调整、保存、放弃或通用 pointer update 查询，也不生成 snapshot/revision update/delete。
- 首个 document/revision/draft 用单条 CTE 或一个显式事务 helper 完成；SQL 通过 `EXISTS` 校验 workspace/project/issue/snapshot/revision 关系。
- pointer update 同时比较 document、workspace 和目标 revision；未来 A5 再叠加 base conflict/用户动作。

**Step 4: 生成代码并运行 GREEN**

```bash
make sqlc
git diff --check -- server/pkg/db/generated
cd server && go test ./internal/handler -run '^TestDesignDocumentPersistence' -count=1 -v
```

预期：sqlc 生成稳定；持久化正向、租户隔离和原子失败测试通过。若本地数据库不可用，测试必须显示 `SKIP`，不得把 package 的 `ok` 当 PASS。

### Task 5：复用对象存储，添加 Design Document archive store

**Files:**

- Create: `server/internal/designdocument/store.go`
- Create: `server/internal/designdocument/store_test.go`

**Step 1: 写失败测试**

- 使用最小 fake `storage.Storage` 断言 object key 为 `design-documents/{workspace}/{project}/{document}/{revision}/{digest_hex}.zip`，对象路径不包含 `sha256:` 前缀中的冒号。
- 上传前重验 archive；digest/binding 不匹配、storage nil 或上传失败均不返回可持久化 reference。
- 读取后重新 `ValidateArchive`；存储中的 bytes 被篡改必须拒绝。
- 同一 revision/digest 重试得到相同 key；A1 不删除旧对象。

**Step 2: 运行 RED**

```bash
cd server && go test ./internal/designdocument -run 'TestArchiveStore' -count=1 -v
```

预期：store 尚未实现，测试失败。

**Step 3: 写最小实现**

- `UploadArchive(ctx, storage.Storage, archive, expectedBinding)` 调用 `ValidateArchive`，再复用 `Upload`。
- `LoadArchive` 使用 `GetReader`、有界读取、重验并返回 validated package。
- 不新增接口层；现有 `storage.Storage` 已覆盖所需能力。

**Step 4: 运行 GREEN**

```bash
cd server && go test ./internal/designdocument -run 'TestArchiveStore' -count=1 -v
```

预期：存储正向、篡改和失败隔离通过。

### Task 6：接入 project/workspace/Issue 关系清理，保护历史 provenance

**Files:**

- Modify: `server/pkg/db/queries/project.sql:63-130`
- Modify: `server/pkg/db/queries/issue.sql:225-260`
- Modify: `server/pkg/db/queries/workspace_delete.sql:629-729`
- Create: `server/internal/handler/design_document_cleanup_test.go`
- Regenerate: `server/pkg/db/generated/project.sql.go`
- Regenerate: `server/pkg/db/generated/issue.sql.go`
- Regenerate: `server/pkg/db/generated/workspace_delete.sql.go`

**Step 1: 写失败测试**

- project delete 删除三张 A1 表中目标 project 数据，不影响邻接 workspace/project；对象存储删除标记保持 0。
- workspace delete 删除目标 workspace 的 revision/snapshot/document，不影响邻接 workspace。
- Issue delete 后仅 document 当前关联置空；snapshot 的 `issue_id`/JSON 与 revision manifest 保留原始 issue provenance，document/snapshot/revision 都不被删除。

**Step 2: 运行 RED**

```bash
cd server && go test ./internal/handler -run 'TestDelete(Project|Workspace|Issue).*Design' -count=1 -v
```

预期：A1 行尚未进入清理图，测试失败。

**Step 3: 写最小关系维护**

- 在现有 set-based CTE/teardown 阶段按 revision → snapshot → document 顺序清理。
- Issue delete 仅更新可变关联列；不重写 archive、revision manifest 或 snapshot JSON。
- 不修改 handler HTTP 行为，不新增对象删除调用。

**Step 4: 生成代码并运行 GREEN**

```bash
make sqlc
cd server && go test ./internal/handler -run 'TestDelete(Project|Workspace|Issue).*Design' -count=1 -v
```

预期：目标数据清理、邻接租户隔离和不可变 provenance 断言通过。

### Task 7：完整验证、GitNexus 复核与阶段证据

**Files:**

- Create: `docs/product/design-center/native-design-phase-a1-validation.md`
- Modify only if evidence warrants: `docs/product/design-center/native-v2-retirement-register.md`

**Step 1: 格式化和生成物检查**

```bash
gofmt -w server/internal/designpackage/*.go server/internal/designdocument/*.go server/internal/projectdesignsystem/v2_archive.go server/internal/handler/design_document*_test.go server/internal/migrations/design_document*_test.go
make sqlc
git diff --check
git status --short --branch
git diff --stat
git diff --cached --stat
```

预期：格式和 whitespace 通过；生成物没有二次漂移；无意外 staged 文件。

**Step 2: 分层测试**

```bash
cd server && env DATABASE_URL= go test ./internal/migrations -count=1 -v
cd server && go test ./internal/designpackage ./internal/designdocument ./internal/projectdesignsystem -count=1 -v
cd server && go test ./internal/handler -run 'DesignDocument|Delete(Project|Workspace|Issue).*Design|ProjectDesignSystem|NativePackage' -count=1 -v
cd server && go build ./...
cd server && go vet ./...
```

预期：全部可运行测试 PASS。数据库测试必须报告实际 `--- PASS`/`--- SKIP` 数；无本地 DB 时记录 SKIP 并阻塞 A1 完成结论，不能伪报通过。

**Step 3: GitNexus `detect_changes`**

```bash
gitnexus analyze . --index-only --name multica-sy46 --force
gitnexus detect-changes --scope all --repo multica-sy46
```

- 复核所有 affected processes，特别是项目设计体系 package preview/upload/completion、Issue batch delete 和 workspace teardown。
- 发现未计划的 HIGH/CRITICAL 或跨 A2-A5 流程时停止，缩小 diff 或回到用户复核。

**Step 4: DC-040 局部清理门禁**

```bash
rg -n 'CreateDesignDraftAgentTask|semantic_pagespec|open_design_run|project_design_system_package' server packages/core packages/views
```

- 该命令只验证旧消费者仍在，不从名称推导删除范围。
- A1 没有完整替代任何旧入口，因此不停止旧写入、不删代码、不改退役状态。
- 只有实际实现证明某个 A1 内部临时 helper 已无调用时才删除该 helper；跨切片残留保持原样。

**Step 5: 写阶段证据并停止**

- `native-design-phase-a1-validation.md` 按 issue 要求记录 Git、文件/符号/API/DB/前端、复用项、测试命令与数量、持久化断言、失败隔离、GitNexus、风险、进度重算和回滚。
- `native-v2-retirement-register.md` 默认写“无状态变化”；只有可复核的新证据满足 DC-040 才增量登记，不能预先盘点。
- Real Agent、Real repository grounding、User Chrome、Human visual review 均写 `NOT RUN（A1 非现场验收切片）`。
- 提交 A1 阶段报告后停止，等待用户确认，不进入 A2。

---

## 4. GitNexus 预变更影响报告

索引：本分支 `97c6069c3`，63,451 nodes / 200,033 edges。以下是本计划可能修改的既有函数/方法；新文件中的新符号没有 pre-change 上游可分析。

| 既有符号 | 风险 | 直接/总影响 | 受影响流程/模块 | 计划中的控制措施 |
| --- | --- | ---: | --- | --- |
| `CollectV2Directory` | HIGH | 25 / 57 | projectdesignsystem、daemon、handler、Open Design 间接测试 | 保留外部签名与 error contract；V2 golden + daemon/handler 回归。 |
| `ValidateV2Archive` | HIGH | 12 / 56 | 11 个 package preview 相关流程 | 只改为共享 helper wrapper；逐 bytes/digest 重验回归。 |
| `SnapshotDigest` | HIGH | 7 / 39 | regenerate/analyze/adjust project design system | 保持 canonical JSON 输出；固定 digest golden。 |
| `digestV2ArtifactIndex` | HIGH | 2 / 56 | package preview、handler/daemon 间接路径 | 保持 length-prefixed 算法；同 fixture digest 不变。 |
| `buildDeterministicV2Archive` | HIGH | 1 / 43 | V2 收集链的跨模块间接影响 | 固定排序、时间、mode 和 bytes golden。 |
| `readAndIndexV2Archive` | HIGH | 2 / 42 | package preview file/preview | 保持 ZIP guard、分类和错误码；恶意 archive 回归。 |
| `validateV2ArchivePath` | HIGH | 4 / 45 | V2 collector/reader/preview target | 共享 path guard 必须是行为超集；所有 V2 path 测试回归。 |
| `validateV2DirectoryPath` | HIGH | 1 / 43 | V2 collect 间接链 | 同上。 |
| `readBoundedFile` | HIGH | 1 / 43 | V2 collect 间接链 | 保留 size/short-read 语义。 |
| `hasMultipleHardlinks` | HIGH | 1 / 43 | V2 collect 间接链 | 保留平台分支和现有 hardlink 测试。 |
| `writeV2DigestField` | HIGH | 1 / 37 | package preview file 间接流程 | 固定 binary length-prefix 编码。 |
| `preflightV2ArchiveEOCD` | LOW | 1 / 17 | package preview file | 保持现有实现与测试；无必要不抽取。 |
| `Handler.DeleteProject` | LOW | 2 / 2 | handler tests | 不改方法体；只扩展其 SQL 查询和清理 fixture。 |
| `Handler.DeleteIssue` | HIGH | 16 / 17 | `BatchDeleteIssues` | 不改 HTTP 方法；扩展同一 `DeleteIssue` CTE，运行单删/批删回归。 |
| `deleteIssueAndCollectAttachmentURLs` | LOW | 2 / 17 | `BatchDeleteIssues` | 保持事务和附件对象行为；只让 SQL detach A1 关联。 |
| `Handler.DeleteWorkspace` | LOW | 0 / 0 | 无上游流程 | 不改方法体；复用现有 design teardown SQL stage。 |

GitNexus 没有索引 sqlc 生成的 query methods；因此对 SQL 行为变化使用其真实 handler 入口做补充影响分析。HIGH 符号的生产直接调用者为：`CollectV2Directory ← finalizeProjectDesignSystemResult`；`ValidateV2Archive ← CollectV2Directory / ReadV2Artifact / prepareNativeProjectDesignSystemCompletion / loadNativeProjectDesignSystemPackageArchive / UploadProjectDesignSystemPackage`；`SnapshotDigest ← marshalProjectDesignSystemTaskContext / nativePackageBindingForTaskContext / nativePackageBindingForTask`；`digestV2ArtifactIndex ← CollectV2Directory / ValidateV2Archive`；`readAndIndexV2Archive ← ValidateV2Archive / ReadV2Artifact`；`validateV2ArchivePath ← readAndIndexV2Archive / classifyV2Artifact / validateV2DirectoryPath / DiscoverV2PreviewTargets`。其余直接调用者为现有 tests。

**HIGH 结论：**主要风险不是新协议，而是抽取共享 ZIP/digest helper 对已上线项目设计体系链路的回归；因此 Task 1 必须先冻结 V2 bytes/digest/error 行为。`DeleteIssue` 的 HIGH 来自大量 handler 测试与批量删除流程，实施时不改 handler 符号，只在既有 SQL CTE 增加 workspace-scoped detach，并跑单删与批删矩阵。

---

## 5. 验收矩阵

| 类别 | 必须证明 |
| --- | --- |
| V2 正向 | 现有项目设计体系 V2 收集、上传、completion、preview 的 bytes/digest/错误行为不变；新 Design Document fixture 可确定性收集和重验。 |
| 失败隔离 | schema/digest/binding/storage/SQL 任一步失败都不创建 document/revision、不移动 draft/saved；对象上传失败不返回可持久化引用。 |
| 旧路径负向 | A1 不调用 `CreateDesignDraftAgentTask`、不写 `semantic_pagespec`、不新增 Open Design Worker/Run，不关闭旧 API。 |
| 范围外回归 | 项目设计体系、Native package preview、Issue 单删/批删、project/workspace teardown 通过；前端无 diff。 |
| 持久化 | workspace/project/可选 Issue/task/agent 隔离；snapshot/revision 追加且 DB 拒绝更新；base 两字段与设计体系三元组同空同存；manifest/index/object/digest 一致。 |
| 安全 | 拒绝 link/traversal/absolute path/package 外资源/远程网络/Service Worker/凭据/真实 API；所有输入和 archive 有界。 |
| 退役 | `design_draft` 与历史 package 数据保持原状；账本无状态变化；不删历史对象、行、表或约束。 |

---

## 6. 独立回滚与停止条件

### 6.1 回滚

- Task 1-2 可独立回滚新领域包和 V2 wrapper，V2 schema/数据不变。
- Task 3-4 在未承载真实数据前可按 889 → 888 → … → 879 → 878 回滚：889 的 `DROP CONSTRAINT` 删除由约束拥有并重命名的六个 backing indexes，883–888 的 `DROP INDEX CONCURRENTLY IF EXISTS` 因此有意成为 no-op，只有 879–882 实际删除四个仍独立的查询索引，878 最后删除表和 trigger function；一旦有用户数据，只能停止新写并另提数据迁移审批，不执行破坏性 down。
- Task 5 只新增按 digest 定址的对象；普通 A1 回滚不删除对象。
- Task 6 回滚只移除关系清理分支；不会恢复已由用户显式删除的 project/workspace。

### 6.2 立即停止并重新请求审批

- 需要新增/修改公网 API、daemon route、MCP 或前端；
- 需要接入 task lifecycle、daemon finalize、browser Preview 或 receipt；
- 需要停止 `design_draft`/PageSpec 写入或删除历史数据/对象/schema；
- 需要恢复 Open Design Worker/Runtime/SSE/Supervisor；
- shared helper 无法保持 Native V2 bytes/digest/error contract；
- GitNexus `detect_changes` 命中未计划的 A2-A5 流程或出现新的 HIGH/CRITICAL；
- 迁移必须使用 877、foreign key/cascade 或非并发多语句索引；
- 数据库测试无法在本地真实执行。

---

## 7. 本计划的明确简化

A1 不预建证据表、文档 workspace、活动 task、完整历史 API 或前端模型；这些字段现在没有消费者，提前建模会把 A2-A5 的产品决策带入 A1。当前最小方案只把协议、安全收集、对象引用和不可变持久化做成可验证的内部基础。
