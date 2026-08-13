# PMO 负责人邮箱自动映射与可读实体名称 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** PMO 同步预览和应用阶段用 workspace 成员邮箱精确映射负责人，并让预览“实体”列以需求/任务标题为主、稳定 external key 为次要信息。

**Architecture:** 保留 `task-d46ba80ebcc030c3` 一类 `task_id` / `external_key` 作为同步 identity、幂等 link key 和冲突选择 key，不改数据库身份模型；前端仅从现有 diff 的 `title` 字段派生可读名称。负责人解析集中在 service 层，复用现有 `ListMembersWithUser`，显式手动映射优先；仅对安全的裸账号补 `@soyoung.com`，随后与 workspace 成员邮箱做大小写不敏感的精确匹配，预览、手动 apply 和 scheduled auto-apply 共用同一规则。

**Tech Stack:** Go、pgx/sqlc（复用已有 query）、React/TypeScript、Vitest/Testing Library、GitNexus

---

## 执行边界与已确认根因

- **本计划由 DeepSeek 执行；Codex 负责执行后的独立验收。** 执行者不要自动提交 commit，除非用户另行明确要求。
- 当前工作树已有用户修改：`AGENTS.md`、`CLAUDE.md`。不得覆盖、回滚或格式化这两个文件。
- 不新增 dependency、SQL、migration、配置项或后台修复脚本。
- 不按中文姓名、display name、拼音或相似度匹配负责人；不把 `yanmeichenyanmeichen` 智能拆半。错误旧值无法精确命中时继续留在手动映射队列。
- `task-d46ba80ebcc030c3` 不是 PM 系统“冗余列”：它是 snapshot 中稳定的 task identity。数据库 `pmo_sync_link.external_key` / `external_task_id` 必须继续保留；只修展示层。
- `BuildPMOSyncPrompt` 的 GitNexus upstream impact 已评估为 **HIGH**（8 个受影响 symbols、6 个 direct、3 个 modules）。修改前必须再次执行 impact，并明确告知用户风险；修改后必须跑 service + daemon prompt 回归。
- 其他已评估 symbols：`PreparePMOSyncRunPreview` LOW、`storePMOSyncRunPreview` LOW、`applySnapshotInTx` LOW、`upsertAssigneeLinks` LOW、`parseDiffView` LOW、`PMOConfigDetailPage` LOW。若执行者改变函数边界或新建不同名称，先对实际将修改的 symbol 重新做 impact。

## 文件结构

| 文件 | 动作 | 职责 |
|---|---|---|
| `server/internal/service/pmo_assignee.go` | Create | 负责人邮箱标准化、snapshot owner 收集、workspace 邮箱精确解析 |
| `server/internal/service/pmo_assignee_test.go` | Create | 纯规则测试：裸账号、大小写、显式优先、非法值、坏旧值不猜测 |
| `server/internal/service/pmo.go` | Modify | prompt 约束；preview 接收已解析 mappings |
| `server/internal/service/pmo_test.go` | Modify | 锁定 owner `external_id` 邮箱 contract 与基础设施无关约束 |
| `server/internal/daemon/prompt_pmo_test.go` | Modify | 锁定 daemon 最终 prompt 仍包含邮箱规则且无 URL/凭据泄露 |
| `server/internal/handler/pmo_agent_task.go` | Modify | 首次 preview 查询 workspace members 并注入 mappings |
| `server/internal/handler/pmo_daemon_test.go` | Modify | 验证首次 preview 已自动映射、warnings/summary 正确 |
| `server/internal/service/pmo_apply.go` | Modify | apply 合并显式与自动 mappings；持久化自动 assignee link |
| `server/internal/service/pmo_apply_test.go` | Modify | 验证自动映射、显式映射优先、project/issue 写入正确 user ID |
| `server/internal/handler/pmo_schedule_test.go` | Modify | 验证 scheduled completion 通过共享 apply 路径自动映射 |
| `packages/views/pmo/pmo-diff.tsx` | Modify | 为扁平 diff row 派生 `entityName`，title 缺失时 fallback external key |
| `packages/views/pmo/pmo-config-detail-page.tsx` | Modify | 实体列主显示名称，次显示类型和 external key；冲突 identity 不变 |
| `packages/views/pmo/pmo-config-detail-page.test.tsx` | Modify | 验证名称主展示、ID 次展示、缺标题 fallback、ARIA key 不变 |

---

### Task 1: 修改前做 GitNexus impact 与基线测试

**Files:**
- Read only: 上述全部目标文件

- [ ] **Step 1: 确认工作树，不触碰用户已有修改**

Run:

```bash
git status --short
```

Expected: 至少看到：

```text
 M AGENTS.md
 M CLAUDE.md
```

允许存在执行者自己创建的 worktree/计划文件；不得 checkout 或 reset 上述两个文件。

- [ ] **Step 2: 对所有 existing symbols 做 upstream impact**

优先用 GitNexus MCP，逐个调用：

```json
{"target":"BuildPMOSyncPrompt","direction":"upstream"}
{"target":"PreparePMOSyncRunPreview","direction":"upstream"}
{"target":"storePMOSyncRunPreview","direction":"upstream"}
{"target":"applySnapshotInTx","direction":"upstream"}
{"target":"upsertAssigneeLinks","direction":"upstream"}
{"target":"parseDiffView","direction":"upstream"}
{"target":"PMOConfigDetailPage","direction":"upstream"}
```

Expected: `BuildPMOSyncPrompt` 仍为 HIGH；其余应与 LOW 基线大致一致。若任一结果变成 HIGH/CRITICAL，先停止该 symbol 的编辑并向用户报告新的 blast radius。

- [ ] **Step 3: 跑现有基线测试**

Run:

```bash
cd server && go test ./internal/service -run 'Test(BuildPMOSyncPrompt|ApplyPMORunUnresolvedAndMappedAssignees)' -count=1 -v
cd server && go test ./internal/handler -run 'TestComplete(PMOSyncTaskStoresPreview|ScheduledPMOSyncTaskAutoApplies)' -count=1 -v
cd server && go test ./internal/daemon -run 'TestBuildPromptPMOSyncStrictAndClean' -count=1 -v
pnpm -C packages/views exec vitest run pmo/pmo-config-detail-page.test.tsx
```

Expected: 全部 PASS。若基线失败，先记录失败，不能把既有失败冒充本次改动回归。

---

### Task 2: 用 TDD 锁定 prompt 的负责人邮箱 contract

**Files:**
- Modify: `server/internal/service/pmo_test.go:8-21`
- Modify: `server/internal/daemon/prompt_pmo_test.go:16-59`
- Modify: `server/internal/service/pmo.go:306-313`

- [ ] **Step 1: 先写失败的 service prompt 断言**

把 `TestBuildPMOSyncPromptIsStrictAndInfrastructureAgnostic` 的 required 内容扩充为：

```go
for _, required := range []string{
    "EXT-P-001",
    `"schema_version"`,
    `"snapshot_complete"`,
    "JSON only",
    "owner.external_id",
    "corporate email",
    "@soyoung.com",
    "do not concatenate",
} {
    if !strings.Contains(strings.ToLower(prompt), strings.ToLower(required)) {
        t.Fatalf("prompt missing %q: %s", required, prompt)
    }
}
```

保留原有 forbidden 断言：`://`、`skill`、`sub-agent`、`credential`。

- [ ] **Step 2: 先写失败的 daemon prompt 断言**

在 `TestBuildPromptPMOSyncStrictAndClean` 的 `want` 列表增加：

```go
"owner.external_id",
"@soyoung.com",
```

并保留其他 prompt builder 不得泄露进 PMO prompt 的断言。

- [ ] **Step 3: 运行测试，确认 RED**

Run:

```bash
cd server && go test ./internal/service -run 'TestBuildPMOSyncPromptIsStrictAndInfrastructureAgnostic' -count=1 -v
cd server && go test ./internal/daemon -run 'TestBuildPromptPMOSyncStrictAndClean' -count=1 -v
```

Expected: FAIL，原因分别是 prompt 缺少 `owner.external_id` / `@soyoung.com` 等新约束。

- [ ] **Step 4: 最小修改 `BuildPMOSyncPrompt`**

保留 JSON schema 和所有现有状态/date 规则，仅把 owner 说明替换为下列明确 contract：

```go
Each owner is null or {"external_id":"","display_name":""}.
Set owner.external_id to the owner's corporate email whenever it is available.
If the source exposes only a bare corporate account such as yanmeichen, return yanmeichen@soyoung.com.
Do not concatenate the account with the display name, and do not infer an email from a person's displayed name.
```

不要加入 PM URL、接口地址、cookie、credential、tool 名称或采集实现细节。

- [ ] **Step 5: 运行 prompt 回归，确认 GREEN**

Run:

```bash
cd server && go test ./internal/service -run 'TestBuildPMOSyncPromptIsStrictAndInfrastructureAgnostic' -count=1 -v
cd server && go test ./internal/daemon -run 'TestBuildPromptPMOSyncStrictAndClean' -count=1 -v
```

Expected: PASS。

---

### Task 3: 新增最小负责人邮箱解析器

**Files:**
- Create: `server/internal/service/pmo_assignee.go`
- Create: `server/internal/service/pmo_assignee_test.go`
- Reuse: `server/pkg/db/queries/member.sql:27-33` 的 `ListMembersWithUser`（不要修改 SQL、不要运行 sqlc）

- [ ] **Step 1: 先写纯规则失败测试**

创建 `pmo_assignee_test.go`，至少包含以下 table test：

```go
func TestNormalizePMOOwnerEmail(t *testing.T) {
    tests := []struct {
        name, externalID, want string
    }{
        {"bare account", "yanmeichen", "yanmeichen@soyoung.com"},
        {"trim and lowercase email", " YanMeiChen@Soyoung.com ", "yanmeichen@soyoung.com"},
        {"safe punctuation", "yan.mei_chen-1", "yan.mei_chen-1@soyoung.com"},
        {"empty", "   ", ""},
        {"display name is not guessed", "严美辰", ""},
        {"spaces are invalid", "yan mei chen", ""},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if got := normalizePMOOwnerEmail(tt.externalID); got != tt.want {
                t.Fatalf("normalizePMOOwnerEmail(%q) = %q, want %q", tt.externalID, got, tt.want)
            }
        })
    }
}
```

再写一个不依赖数据库的 merge/match test，输入成员 rows 和 existing mapping，锁定：

```go
func TestMatchPMOAssigneeMappingsKeepsExplicitAndUsesOriginalExternalIDKey(t *testing.T) {
    // members: yanmeichen@soyoung.com -> user-a; manual target -> user-b
    // owners: "YanMeiChen", "manual-owner", "yanmeichenyanmeichen", "严美辰"
    // existing: "manual-owner" -> user-b
    // assert:
    // mappings["YanMeiChen"] == user-a  // key 保留 snapshot 原始 ExternalID
    // mappings["manual-owner"] == user-b // 显式 mapping 不覆盖
    // duplicated bad value 与中文值均不存在于结果中
}
```

为方便纯测，只增加一个包内 helper：

```go
func matchPMOAssigneeMappings(
    owners map[string]*PMOExternalOwner,
    memberEmailToUserID map[string]string,
    existing map[string]string,
) map[string]string
```

- [ ] **Step 2: 运行测试，确认 RED**

Run:

```bash
cd server && go test ./internal/service -run 'Test(NormalizePMOOwnerEmail|MatchPMOAssigneeMappings)' -count=1 -v
```

Expected: 编译 FAIL，helper 尚未定义。

- [ ] **Step 3: 实现最小 helper**

`pmo_assignee.go` 使用 Go stdlib `strings`，不要引入 regex/package dependency。实现：

```go
const pmoCorporateEmailDomain = "@soyoung.com"

func normalizePMOOwnerEmail(externalID string) string {
    value := strings.ToLower(strings.TrimSpace(externalID))
    if value == "" {
        return ""
    }
    if strings.Contains(value, "@") {
        if strings.Count(value, "@") != 1 || strings.HasPrefix(value, "@") || strings.HasSuffix(value, "@") {
            return ""
        }
        return value
    }
    for _, r := range value {
        if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
            continue
        }
        return ""
    }
    return value + pmoCorporateEmailDomain
}
```

收集 owner 时只复用 snapshot 已有字段，不读取 display name：

```go
func pmoSnapshotOwners(snapshot PMOSnapshot) map[string]*PMOExternalOwner
```

遍历 `Parent.Owner`、每个 `Children[i].Owner`、child tasks、top-level tasks；忽略 nil/空 `ExternalID`。

`matchPMOAssigneeMappings`：

1. 复制 `existing` 中非空项到新 map，不修改调用者 map。
2. 遍历 owners；已有 mapping 直接 continue。
3. 调 `normalizePMOOwnerEmail(owner.ExternalID)`。
4. 用 lower-case email map 精确查找。
5. 命中时写 `result[owner.ExternalID] = userID`，保留原始 key。

数据库 wrapper 使用现有 query：

```go
func ResolvePMOAssigneeMappings(
    ctx context.Context,
    qtx *db.Queries,
    workspaceID pgtype.UUID,
    snapshot PMOSnapshot,
    existing map[string]string,
) (map[string]string, error) {
    members, err := qtx.ListMembersWithUser(ctx, workspaceID)
    if err != nil {
        return nil, fmt.Errorf("resolve pmo assignees: list workspace members: %w", err)
    }
    memberEmailToUserID := make(map[string]string, len(members))
    for _, member := range members {
        memberEmailToUserID[strings.ToLower(strings.TrimSpace(member.UserEmail))] = util.UUIDToString(member.UserID)
    }
    return matchPMOAssigneeMappings(pmoSnapshotOwners(snapshot), memberEmailToUserID, existing), nil
}
```

不需要处理“重复邮箱冲突”：`user.email` 已是 `UNIQUE NOT NULL`，且查询限定当前 workspace membership。

- [ ] **Step 4: 运行纯规则测试，确认 GREEN**

Run:

```bash
cd server && go test ./internal/service -run 'Test(NormalizePMOOwnerEmail|MatchPMOAssigneeMappings)' -count=1 -v
```

Expected: PASS。

---

### Task 4: 首次 preview 注入自动 mappings

**Files:**
- Modify: `server/internal/service/pmo.go:71-91`
- Modify: `server/internal/handler/pmo_agent_task.go:107-141`
- Modify: `server/internal/handler/pmo_daemon_test.go:60-85,198-235`

- [ ] **Step 1: 先写 handler preview 失败测试**

在 `pmo_daemon_test.go` 增加测试 helper，创建唯一测试用户并加入现有 `testWorkspaceID`：

```go
func createPMOEmailMemberForTest(t *testing.T, account string) string {
    t.Helper()
    ctx := context.Background()
    email := strings.ToLower(account) + "@soyoung.com"
    var userID string
    if err := testPool.QueryRow(ctx,
        `INSERT INTO "user" (name, email) VALUES ('PMO Email Member', $1) RETURNING id`,
        email,
    ).Scan(&userID); err != nil {
        t.Fatalf("create pmo email user: %v", err)
    }
    if _, err := testPool.Exec(ctx,
        `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
        testWorkspaceID, userID,
    ); err != nil {
        t.Fatalf("create pmo email member: %v", err)
    }
    t.Cleanup(func() {
        _, _ = testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, userID)
        _, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
    })
    return userID
}
```

增加一个 snapshot helper：把 parent 和 child owner 都设为 `{"external_id": account, "display_name": "PMO Email Member"}`。

扩展/新增 `TestCompletePMOSyncTaskStoresPreviewAutoMappedAssignee`：完成 task 后读取 `pmo_sync_run.diff` 与 `summary`，反序列化为 `service.PMODiff` / `service.PMODiffSummary`，断言：

```go
if diff.Summary.UnresolvedAssignees != 0 || len(diff.Warnings) != 0 {
    t.Fatalf("preview unresolved assignees = %d, warnings = %+v", diff.Summary.UnresolvedAssignees, diff.Warnings)
}
for _, entity := range diff.Entities {
    if field, ok := entity.Fields["lead_id"]; ok && field.External != userID {
        t.Fatalf("project lead external = %#v, want %s", field.External, userID)
    }
    if field, ok := entity.Fields["assignee_id"]; ok && field.External != userID {
        t.Fatalf("issue assignee external = %#v, want %s", field.External, userID)
    }
}
```

- [ ] **Step 2: 运行测试，确认 RED**

Run:

```bash
cd server && go test ./internal/handler -run 'TestCompletePMOSyncTaskStoresPreviewAutoMappedAssignee' -count=1 -v
```

Expected: FAIL；当前 preview 未传 `AssigneeMappings`，summary 仍含 unresolved warning。

- [ ] **Step 3: 修改 preview serializer 签名**

将：

```go
func PreparePMOSyncRunPreview(snapshot PMOSnapshot) (...)
```

改为：

```go
func PreparePMOSyncRunPreview(snapshot PMOSnapshot, assigneeMappings map[string]string) (...)
```

并把 diff 构造改为：

```go
diff := DiffPMOSnapshot(PMODiffInput{
    Snapshot:         snapshot,
    AssigneeMappings: assigneeMappings,
})
```

删除/更新“diff 只 against empty local state，所以负责人一定 unresolved”的过时注释；仍保留“preview 不写 project/issue”的说明。

- [ ] **Step 4: 修改 `storePMOSyncRunPreview`**

在 marshal preview 前先解析 workspace ID，再解析 mappings：

```go
workspaceID, err := util.ParseUUID(pmoCtx.WorkspaceID)
if err != nil {
    return err
}
assigneeMappings, err := service.ResolvePMOAssigneeMappings(ctx, qtx, workspaceID, snapshot, nil)
if err != nil {
    return err
}
sourceSnapshot, diffJSON, summaryJSON, err := service.PreparePMOSyncRunPreview(snapshot, assigneeMappings)
if err != nil {
    return err
}
```

`runID` 解析和 `StorePMOSyncRunPreview` guarded update/idempotency 逻辑保持不变。

- [ ] **Step 5: 运行 preview 测试，确认 GREEN**

Run:

```bash
cd server && go test ./internal/handler -run 'TestCompletePMOSyncTaskStoresPreview' -count=1 -v
```

Expected: 原测试和新增自动映射测试均 PASS。

---

### Task 5: Apply 合并自动 mapping 并持久化 link

**Files:**
- Modify: `server/internal/service/pmo_apply.go:184-221,331-339,877-920`
- Modify: `server/internal/service/pmo_apply_test.go:16-100,589-636`

- [ ] **Step 1: 先写自动映射 apply 失败测试**

在 fixture 中增加一个小 helper，用 fixture pool/workspace 创建 workspace member：

```go
func addPMOApplyMember(t *testing.T, f pmoApplyFixture, account string) pgtype.UUID
```

用户 email 为 `strings.ToLower(account) + "@soyoung.com"`；cleanup 删除 member/user。

新增：

```go
func TestApplyPMORunAutoMapsBareOwnerIDByWorkspaceEmail(t *testing.T)
```

测试数据：

```go
account := fmt.Sprintf("yanmeichen-%d", time.Now().UnixNano())
memberUserID := addPMOApplyMember(t, f, account)
owner := map[string]any{"external_id": account, "display_name": "严美辰"}
```

parent 和 child 都设置该 owner。apply 后断言：

```go
if applied.Status != "applied" {
    t.Fatalf("status = %q, want applied", applied.Status)
}
projectLink := pmoLinkByExternal(t, f, "requirement", "EXT-P-001")
project := projectByID(t, f.pool, projectLink.LocalID)
if project.LeadID != memberUserID {
    t.Fatalf("project lead = %v, want %v", project.LeadID, memberUserID)
}
childLink := pmoLinkByExternal(t, f, "requirement", "EXT-I-001")
issue := issueByID(t, f.pool, childLink.LocalID)
if issue.AssigneeID != memberUserID || issue.AssigneeType.String != "member" {
    t.Fatalf("issue assignee = %v/%q, want member %v", issue.AssigneeID, issue.AssigneeType.String, memberUserID)
}
assigneeLink := pmoLinkByExternal(t, f, "assignee", account)
if assigneeLink.LocalType.String != "member" || assigneeLink.LocalID != memberUserID {
    t.Fatalf("assignee link = %+v", assigneeLink)
}
```

若仓库没有 `projectByID`，在本测试文件内用一条只读 `SELECT lead_id FROM project WHERE id=$1` 完成断言，不新增产品 query。

- [ ] **Step 2: 先写显式 mapping 优先测试**

新增：

```go
func TestApplyPMORunKeepsExplicitAssigneeMappingOverEmailMatch(t *testing.T)
```

建立两个 workspace members：

- email 自动可匹配 member A；
- 先调用 `SetAssigneeMapping(... externalID, memberB)` 建立显式 mapping。

apply 后断言 project lead、issue assignee 和 assignee link 均为 member B，不是 member A。

- [ ] **Step 3: 运行测试，确认 RED**

Run:

```bash
cd server && go test ./internal/service -run 'TestApplyPMORun(AutoMapsBareOwnerIDByWorkspaceEmail|KeepsExplicitAssigneeMappingOverEmailMatch)' -count=1 -v
```

Expected: 自动映射测试 FAIL；显式优先测试可 PASS 或因新测试尚未接线 FAIL，但最终必须同时覆盖。

- [ ] **Step 4: 在 `applySnapshotInTx` 合并 mappings**

保留现有 link 扫描得到的显式 map：

```go
explicitAssigneeMappings := map[string]string{}
```

扫描 link 时继续只接受 `assignee` + valid `LocalID`。扫描完成后调用：

```go
assigneeMappings, err := ResolvePMOAssigneeMappings(
    ctx,
    qtx,
    workspaceID,
    snapshot,
    explicitAssigneeMappings,
)
if err != nil {
    return result, err
}
```

传入现有 diff：

```go
diff := DiffPMOSnapshot(PMODiffInput{
    Snapshot:         snapshot,
    Links:            linkStates,
    AssigneeMappings: assigneeMappings,
})
```

- [ ] **Step 5: 让 `upsertAssigneeLinks` 持久化 resolved mapping**

签名改为：

```go
func (s *PMOService) upsertAssigneeLinks(
    ctx context.Context,
    qtx *db.Queries,
    workspaceID, configID pgtype.UUID,
    snapshot PMOSnapshot,
    byIdentity map[string]db.PmoSyncLink,
    assigneeMappings map[string]string,
) error
```

复用 `pmoSnapshotOwners(snapshot)`，删除函数内重复 owner 遍历。每个 owner 的优先级：

1. existing link 已有 `LocalID`：原样保留，绝不覆盖；
2. 否则读取 `assigneeMappings[externalID]`，用 `util.ParseUUID` 解析；
3. 命中时写 `LocalType = member`、`LocalID = userID`、`BaselineLocal = {"member_id": userID}`；
4. 未命中保持 unresolved link。

核心分支：

```go
if existing.ID.Valid && existing.LocalID.Valid {
    params.LocalType = existing.LocalType
    params.LocalID = existing.LocalID
    localJSON, _ = json.Marshal(map[string]any{"member_id": util.UUIDToString(existing.LocalID)})
} else if localUserID := assigneeMappings[externalID]; localUserID != "" {
    parsed, err := util.ParseUUID(localUserID)
    if err != nil {
        return fmt.Errorf("upsert pmo assignee link %q: %w", externalID, err)
    }
    params.LocalType = pgtype.Text{String: pmoLocalTypeMember, Valid: true}
    params.LocalID = parsed
    localJSON, _ = json.Marshal(map[string]any{"member_id": localUserID})
}
params.BaselineLocal = localJSON
```

调用点改为传 `assigneeMappings`。更新注释为“never infer by display name; exact workspace email match or explicit mapping only”。

- [ ] **Step 6: 跑 apply 测试，确认 GREEN**

Run:

```bash
cd server && go test ./internal/service -run 'TestApplyPMORun(UnresolvedAndMappedAssignees|AutoMapsBareOwnerIDByWorkspaceEmail|KeepsExplicitAssigneeMappingOverEmailMatch)' -count=1 -v
```

Expected: 全部 PASS；原 `EXT-U-001` 因对应邮箱不存在，仍保持 unresolved，证明没有模糊猜测。

---

### Task 6: 锁定 scheduled auto-apply 走相同规则

**Files:**
- Modify: `server/internal/handler/pmo_schedule_test.go:24-55`
- Reuse: `server/internal/handler/pmo_daemon_test.go` 新增的测试 helper

- [ ] **Step 1: 增加 scheduled 自动映射测试**

新增：

```go
func TestCompleteScheduledPMOSyncTaskAutoMapsAssigneeByEmail(t *testing.T)
```

步骤：

1. 创建唯一裸账号 `pmo-scheduled-<n>` 和对应 `@soyoung.com` workspace member；
2. 创建 run，并 `makeRunScheduledForTest`；
3. 完成带该 owner 的 snapshot；
4. 读取 run status/summary；
5. 读取 `pmo_sync_link` 中 `external_type='assignee' AND external_key=$account`。

断言：

```go
if status != "applied" {
    t.Fatalf("scheduled run status = %q, want applied", status)
}
if summary.UnresolvedAssignees != 0 {
    t.Fatalf("scheduled unresolved assignees = %d", summary.UnresolvedAssignees)
}
if localType != "member" || localID != userID {
    t.Fatalf("scheduled assignee link = %q/%s, want member/%s", localType, localID, userID)
}
```

- [ ] **Step 2: 运行 scheduled 测试**

Run:

```bash
cd server && go test ./internal/handler -run 'TestCompleteScheduledPMOSyncTask(AutoApplies|AutoMapsAssigneeByEmail)' -count=1 -v
```

Expected: PASS。scheduled handler 不新增独立匹配实现；它必须通过 `PMOService.ApplyRun -> applySnapshotInTx` 共用 Task 5 的规则。

---

### Task 7: 实体列主显示 title，external key 作为次要 ID

**Files:**
- Modify: `packages/views/pmo/pmo-diff.tsx:30-40,72-103`
- Modify: `packages/views/pmo/pmo-config-detail-page.tsx:318-323`
- Modify: `packages/views/pmo/pmo-config-detail-page.test.tsx:23-85,391-401,444-461`

- [ ] **Step 1: 先写失败的 UI 测试**

修改 preview 测试，使它明确区分“主名称”和“次要 ID”。建议给实体名称和 ID 添加稳定 test id：

```tsx
expect(screen.getAllByTestId("pmo-entity-name").map((node) => node.textContent)).toEqual(
  expect.arrayContaining(["New external title", "New task title"]),
);
expect(screen.getAllByTestId("pmo-entity-key").map((node) => node.textContent)).toEqual(
  expect.arrayContaining(["EXT-P-001", "TASK-001"]),
);
```

新增 fallback case：构造一个 entity，其 `fields.title` 的 `external`、`local`、`baseline_external`、`baseline_local` 全为空，`external_key: "task-d46ba80ebcc030c3"`；断言 entity name 显示该 ID。

保留 apply 冲突测试：

```tsx
screen.getByRole("button", { name: "Use external EXT-P-001 title" })
```

证明 aria-label/conflict identity 仍使用稳定 ID。

- [ ] **Step 2: 运行 UI 测试，确认 RED**

Run:

```bash
pnpm -C packages/views exec vitest run pmo/pmo-config-detail-page.test.tsx
```

Expected: FAIL；当前 `DiffFieldRow` 没有 `entityName`，实体列主内容仍是 `entityKey`。

- [ ] **Step 3: 在 `parseDiffView` 派生实体名称**

给 row 增加：

```ts
export interface DiffFieldRow {
  entityName: string;
  entityKey: string;
  // existing fields unchanged
}
```

增加小 helper：

```ts
function entityTitle(fields: Record<string, unknown>, entityKey: string): string {
  const raw = fields.title;
  if (!raw || typeof raw !== "object") return entityKey;
  const title = raw as Record<string, unknown>;
  return asString(title.external)
    || asString(title.local)
    || asString(title.baseline_external)
    || asString(title.baseline_local)
    || entityKey;
}
```

在进入 fields loop 前只算一次：

```ts
const entityName = entityTitle(fields, entityKey);
```

每个 row 写入 `entityName`。不要改变 `conflicts` 的 `${external_type}:${entityKey}:${field}` 格式、`conflictId` 或 row key。

- [ ] **Step 4: 最小修改实体列 JSX**

替换当前主展示：

```tsx
<TruncatedValue value={row.entityKey} />
```

为：

```tsx
<div data-testid="pmo-entity-name">
  <TruncatedValue value={row.entityName} />
</div>
<span className="block text-micro text-muted-foreground">
  {entityTypeLabel} · <span data-testid="pmo-entity-key" className="font-mono">{row.entityKey}</span>
</span>
```

继续使用现有 requirement/task i18n label；不新增 tooltip、复制按钮、展开状态或新的翻译 key。

- [ ] **Step 5: 跑 UI 测试，确认 GREEN**

Run:

```bash
pnpm -C packages/views exec vitest run pmo/pmo-config-detail-page.test.tsx
```

Expected: PASS；名称为主、ID 仍存在、title 缺失 fallback、冲突 aria-label 未变。

---

### Task 8: 全量定向验证与变更范围审计

**Files:**
- No new product files

- [ ] **Step 1: Go 格式化，仅限本次修改文件**

Run:

```bash
gofmt -w \
  server/internal/service/pmo_assignee.go \
  server/internal/service/pmo_assignee_test.go \
  server/internal/service/pmo.go \
  server/internal/service/pmo_test.go \
  server/internal/service/pmo_apply.go \
  server/internal/service/pmo_apply_test.go \
  server/internal/handler/pmo_agent_task.go \
  server/internal/handler/pmo_daemon_test.go \
  server/internal/handler/pmo_schedule_test.go \
  server/internal/daemon/prompt_pmo_test.go
```

Expected: 命令成功；`AGENTS.md`、`CLAUDE.md` 不变。

- [ ] **Step 2: 后端定向测试**

Run:

```bash
cd server && go test ./internal/service -run 'Test(BuildPMOSyncPrompt|NormalizePMOOwnerEmail|MatchPMOAssigneeMappings|ApplyPMORun.*Assignee.*)' -count=1 -v
cd server && go test ./internal/handler -run 'TestComplete(PMOSyncTaskStoresPreview|ScheduledPMOSyncTask.*)' -count=1 -v
cd server && go test ./internal/daemon -run 'TestBuildPromptPMOSyncStrictAndClean' -count=1 -v
```

Expected: PASS。

- [ ] **Step 3: 前端定向验证**

Run:

```bash
pnpm -C packages/views exec vitest run pmo/pmo-config-detail-page.test.tsx
pnpm --filter @multica/views typecheck
pnpm --filter @multica/views lint
```

Expected: PASS / exit 0。

- [ ] **Step 4: 相关包回归**

Run:

```bash
cd server && go test ./internal/service ./internal/handler ./internal/daemon
pnpm --filter @multica/views test
```

Expected: PASS。若 handler integration 因本机无测试数据库而 SKIP，必须在交接中明确记录，不能写成 PASS。

- [ ] **Step 5: GitNexus detect changes**

优先用 GitNexus MCP：

```json
{"scope":"compare","base_ref":"main"}
```

Expected: 变更集中在 PMO prompt、assignee resolution/apply/preview、PMO diff UI 与对应测试；没有意外 execution flow。若只能使用 CLI，先执行：

```bash
rtk npx gitnexus detect-changes --help
```

再按当前版本帮助使用 compare `main` 的语法；不要猜参数。发现 HIGH/CRITICAL 意外影响时停止交付并报告。

- [ ] **Step 6: 检查最终 diff，不提交**

Run:

```bash
git status --short
git diff --check
git diff --stat
git diff -- \
  server/internal/service/pmo_assignee.go \
  server/internal/service/pmo_assignee_test.go \
  server/internal/service/pmo.go \
  server/internal/service/pmo_test.go \
  server/internal/service/pmo_apply.go \
  server/internal/service/pmo_apply_test.go \
  server/internal/handler/pmo_agent_task.go \
  server/internal/handler/pmo_daemon_test.go \
  server/internal/handler/pmo_schedule_test.go \
  server/internal/daemon/prompt_pmo_test.go \
  packages/views/pmo/pmo-diff.tsx \
  packages/views/pmo/pmo-config-detail-page.tsx \
  packages/views/pmo/pmo-config-detail-page.test.tsx
```

Expected: `git diff --check` 无输出；没有 migration、query、dependency 或无关文件变化。不要执行 `git commit`。

---

### Task 9: 部署后浏览器业务验收（由 Codex 独立执行）

**Targets:**
- iWorker PMO config: `https://iworker.soyoung.com/iworker/pmo/2ed00bd5-feab-4102-b9c4-a5c3b6b5f224`
- PM source issue: `https://pm.sy.soyoung.com/pages/project/main/list/index?issue_id=136076`

- [ ] **Step 1: 重新触发 issue `136076` 的同步预览**

Expected: run 进入 `preview_ready`；不直接点击“应用预览”，先验数据。

- [ ] **Step 2: 验收实体展示**

Expected:

- “实体”列主文案是需求/任务标题，与 PM 页面对应名称一致；
- `task-d46ba80ebcc030c3` 一类 key 只在次要灰色 monospace 信息中出现；
- 标题缺失的极端数据才用 key 作为主文案；
- diff 行数、字段内容和冲突选择没有因展示修改而变化。

- [ ] **Step 3: 验收负责人自动映射**

Expected:

- `yanmeichen` 精确命中 `yanmeichen@soyoung.com`；
- `heqing`、`liuxiaosong`、`fengyujie`、`leizongwen` 等合法裸账号按同规则匹配其 workspace member；
- `unresolved_assignees` 从原来的 6 降到实际无法精确匹配的数量，若 workspace 成员邮箱齐全则为 0；
- 旧 snapshot 若仍传 `yanmeichenyanmeichen`，不得拆半猜测，应保留 unresolved，待新 prompt 重新采集或手动映射；
- 已有手动 mapping 不被自动邮箱规则覆盖。

- [ ] **Step 4: 验收 apply**

在确认 diff 后应用预览。Expected:

- parent requirement 对应 project lead 写入正确 workspace member user ID；
- child requirement/task 对应 issue assignee 写入正确 workspace member user ID，type 为 `member`；
- assignee `pmo_sync_link.local_type='member'` 且 `local_id` 为正确 user ID；
- 重跑同步保持幂等，不因标题相同创建重复实体。

- [ ] **Step 5: 验收 scheduled path**

使用已启用 schedule 的配置或测试环境触发一次 scheduled run。Expected: 与手动 apply 使用相同邮箱精确匹配，不能重新出现“预览能匹配、定时不能匹配”的分叉。

---

## 最终验收清单

- [ ] `task-d46...` 被确认并保留为稳定同步 ID，不做数据库字段删除或 identity 替换。
- [ ] 实体列主显示 title，次显示 type + external key；缺 title 时 fallback ID。
- [ ] prompt 优先要求 owner `external_id` 为企业邮箱，裸账号补 `@soyoung.com`，禁止拼 display name。
- [ ] preview 在存储 diff 前完成 workspace email 精确映射。
- [ ] manual apply 与 scheduled auto-apply 共用同一 resolver。
- [ ] 手动显式 mapping 优先于自动 email mapping。
- [ ] 自动匹配结果持久化到 assignee link。
- [ ] 不按姓名/display name/拼音/模糊规则猜测，不拆错误重复账号。
- [ ] 未新增 SQL、migration、dependency、配置项或后台修复任务。
- [ ] 所有定向测试、相关包回归、typecheck、lint、`git diff --check` 和 GitNexus detect changes 已有 fresh evidence。
- [ ] `AGENTS.md`、`CLAUDE.md` 的用户修改未被覆盖。
- [ ] 未自动 commit。
