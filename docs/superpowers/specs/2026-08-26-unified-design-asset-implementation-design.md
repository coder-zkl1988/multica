# 统一设计稿还原与仓库代码实现方案

> 状态：产品与技术方案已确认，待用户审阅书面 Spec
> 日期：2026-08-26
> 适用范围：统一设计稿入口、Frame 选择、任务评论 Prompt、MCP/API、Figma 还原兼容、Design Document HTML 代码转换、仓库架构取证、验证与结果映射
> 当前基线：`main@a7606af71`
> 前置方案：`2026-08-26-design-center-project-repository-views-m1-design.md`

## 1. 摘要

Multica 当前存在两种内部设计产物：

- Figma 插件上传形成的 `design_file` / `design_revision`；
- 首页“创作”生成并保存的 `design_document` / `design_document_revision`。

用户在设计稿保存或上传完成后，不需要知道这两种内部来源。两者在项目设计资产中平级展示，并使用统一的设计稿、Frame、评论 Prompt 和 MCP/API 还原入口。

统一并不意味着合并数据库实体，也不意味着自动匹配或合并相同内容。每一份用户选择的设计稿只有一个内部来源：

- Figma 设计稿由现有 Restore Pack、Frame、Layer 和 Asset 提供实现上下文；
- Design Document 由 saved revision 中的 HTML Prototype、Brief、Coverage、状态、交互和 Assets 提供实现上下文。

任务详情右侧栏是最高频入口。用户选择设计稿、Frame 和目标仓库后点击“生成还原提示”，Server 固定设计稿版本和范围，生成 Prompt 并预填写到评论输入框。该动作不自动发送评论、不启动 Agent、不修改任务状态。用户发送评论后，Agent 调用统一 MCP 获取 Implementation Context，并在目标仓库中实现代码。

Design Document 的实现不是简单 HTML 字符串转译。平台确定性解析 Prototype 和设计包，实际 Agent 读取目标仓库框架、组件库、路由、状态管理和样式体系，再将设计实现为符合仓库架构的 Vue、React 或其他目标代码。

## 2. 当前实现事实

### 2.1 Figma 还原链

当前已存在：

- `multica_design_get_restore_pack`；
- 设计文件、Frame、Group 和 Selection Context MCP；
- Native JSON、Asset、Slice 和缩略图；
- 任务右侧栏的设计稿和 Frame 选择；
- “生成还原提示”并注入任务评论草稿；
- 前端要求 Agent 先读仓库、复用组件并禁止整图替代。

该链路继续保留，不在本方案中重写。

### 2.2 Design Document 交付链

当前已存在：

- saved-only 的 Design Document 交付；
- Design Document 与同项目任务的关联；
- `multica.design-delivery/v1`；
- saved revision ID、content digest、Prototype 入口和页面清单；
- 任务 Agent claim 时携带 Design Delivery Context；
- 守护进程下载、校验并只读展开 Design Document package；
- Agent 可以读取 `brief.json`、`coverage.json`、`prototype/` 和 `assets/`；
- 现有提示已说明 Prototype 是规格，不是可以原样复制进产品的源代码。

### 2.3 当前缺口

仍缺少：

- 用户/API/MCP 层统一的 `design_ref`；
- 两种来源统一的 Frame 列表；
- Server 生成的统一评论 Prompt；
- 通用 `multica_design_get_implementation_context`；
- Design Document Source Adapter；
- HTML Prototype 到目标仓库技术栈的明确转换契约；
- 本地 MCP 与任务 Agent 共用的 Context 目录；
- 统一 Frame → 代码文件映射和验证结果；
- Design Document 来源的运行时与视觉验收矩阵。

## 3. 产品原则

### 3.1 用户不选择内部来源

用户只看到：

```text
设计稿
设计稿版本
Frame
目标仓库
生成还原提示
```

用户不需要选择：

```text
design_file
design_document
Figma Restore
HTML Conversion
Native JSON Restore
```

来源可以保留在调试和审计信息中，但不成为用户主流程的分叉。

### 3.2 用户自行选择范围

用户自行选择：

- 要还原的设计稿；
- 一个或多个 Frame；
- 目标仓库；
- 发送给 Agent 的最终评论内容。

系统不替用户判断应选择哪份设计稿、哪个 Frame 或哪个仓库，也不做同内容设计稿的自动匹配和冲突融合。

### 3.3 内容不自动融合

本方案中的“生成稿与 Figma 上传稿融合”只表示：

- 在项目设计资产中平级展示；
- 使用统一设计稿和 Frame 选择体验；
- 使用统一 `design_ref` / `frame_ref`；
- 使用统一 Prompt、MCP、结果和门禁。

本方案明确不做：

- Design Document Page 与 Figma Frame 自动匹配；
- 标题、结构或截图相似度匹配；
- 同一逻辑 Frame 同时持有两种 Representation；
- Figma 与 Design Document 内容冲突合并；
- 用户上传相同设计稿后自动去重。

### 3.4 saved/valid 版本门禁

- Design Document 只允许使用 saved revision；
- Draft revision 不得进入代码还原；
- Figma 设计稿只允许使用 Server 判定可用的固定 design revision；
- Prompt、MCP Context 和结果必须绑定 revision ID 与 content digest；
- 设计稿出现新版本不能静默改变已经生成的 Prompt 或正在执行的 Agent task。

## 4. 统一设计资产引用

### 4.1 `design_ref`

Server 向客户端提供版本化、不透明的 `design_ref`。调用方必须将其视为普通字符串，不解析内部来源。

第一版不建立新的统一资产表。Server 使用版本化编码解析真实实体；编码格式不得进入 UI 文案或客户端业务判断。

统一投影示意：

```json
{
  "design_ref": "opaque-design-reference",
  "title": "客户列表页",
  "revision_id": "saved-or-valid-revision-id",
  "content_digest": "sha256:...",
  "thumbnail_url": "...",
  "frame_count": 3,
  "updated_at": "2026-08-26T12:00:00Z"
}
```

### 4.2 `frame_ref`

用户侧最小还原单位统一称为 Frame。

内部映射：

| 内部来源 | Frame 对应对象 |
| --- | --- |
| `design_file` | Figma Frame 或 Group |
| `design_document` | Manifest Page |

`frame_ref` 同样由 Server 生成，客户端不解析其来源。

Design Document Page 内的状态、弹窗和页面内交互自动包含在该 Frame Context 中，用户不逐个勾选状态。

跨页面流程只有在用户选择相关全部 Frame 时才要求完整实现；未选页面写入外部依赖说明。

### 4.3 统一 Frame API

```http
GET /api/design-assets/{design_ref}/frames
```

返回：

```json
{
  "design_ref": "design_xxx",
  "revision_id": "revision_xxx",
  "content_digest": "sha256:...",
  "frames": [
    {
      "frame_ref": "frame_customer_list",
      "title": "客户列表",
      "thumbnail_url": "...",
      "description": "客户筛选、列表和批量操作"
    }
  ]
}
```

前端不需要获得 `source_kind`、`design_file_id`、`design_document_id`、`page_id` 或 Prototype 路径。

## 5. 三种统一入口

### 5.1 任务右侧栏

最高频路径：

```text
打开任务
→ 右侧栏选择设计稿
→ 选择 Frame
→ 选择目标仓库
→ 点击生成还原提示
→ Prompt 预填写到评论输入框
→ 用户检查、修改并发送
→ Agent 响应评论并调用 MCP
```

点击“生成还原提示”时：

- 不自动发送评论；
- 不直接启动 Agent；
- 不创建第二条评论；
- 不改变任务状态；
- 不自动改变设计稿仓库关联；
- 不替用户选择仓库。

### 5.2 创建 task 交给 Agent

创建 task 时可以带入：

- `design_ref`；
- revision ID；
- Frame refs；
- 目标仓库。

Agent 执行时调用同一个 Implementation Context，不建立第二套设计协议。

该入口的完整任务产品行为在后续“任务双向关联与 Agent 自动化”Spec 中展开，本方案只固定设计上下文契约。

### 5.3 本地 Agent MCP

用户可以直接要求本地 Agent：

```text
使用这份设计稿的所选 Frame，在当前仓库中完成实现。
```

本地 Agent 调用统一 MCP，不要求先创建 task。

## 6. Server 生成评论 Prompt

### 6.1 API

```http
POST /api/design-assets/{design_ref}/implementation-prompt
```

请求：

```json
{
  "revision_id": "revision_xxx",
  "frame_refs": [
    "frame_customer_list"
  ],
  "project_resource_id": "repository_xxx",
  "issue_id": "issue_xxx"
}
```

### 6.2 Server 校验

Server 必须：

1. 解析 `design_ref`；
2. 验证 revision 属于该设计稿；
3. 验证 Design Document revision 为 saved；
4. 验证 Figma revision 可用于还原；
5. 验证全部 `frame_ref` 属于该 revision；
6. 验证任务、设计稿和目标仓库属于同一项目；
7. 验证用户权限；
8. 固定 content digest；
9. 生成统一 MCP 调用参数；
10. 返回可以注入评论输入框的完整 Prompt。

任一项失败时不得返回看似可执行的 Prompt。

### 6.3 前端职责

前端只负责：

```text
调用 API
→ 获取 prompt
→ injectDraft()
→ 用户编辑和发送
```

前端不得：

- 判断内部来源；
- 自己拼 Figma 或 Design Document 指令；
- 信任 UI 缓存中的旧 revision；
- 自动发送评论；
- 直接派发 Agent。

### 6.4 Prompt 结构

统一 Prompt 至少包含：

```text
【任务】
根据关联设计稿实现当前任务，优先复用目标仓库已有组件和页面结构。

【设计稿】
设计稿标题
固定版本
所选 Frame
目标仓库

【执行步骤】
1. 调用 multica_design_get_implementation_context。
2. 读取目标仓库路由、组件、状态管理和样式规范。
3. 根据 Implementation Context 完成实现。
4. 运行约定验证。
5. 输出 Frame 到代码文件映射。

【约束】
禁止整图替代；禁止直接复制 Prototype；保留无关 dirty worktree。

【输出】
修改文件、复用组件、新增组件、检查结果和阻塞项。
```

来源专属差异由 MCP Context 表达，不要求 Prompt 向用户解释内部实体类型。

## 7. 统一 MCP

### 7.1 新工具

新增：

```text
multica_design_get_implementation_context
```

参数：

```json
{
  "designRef": "design_xxx",
  "revisionId": "revision_xxx",
  "frameRefs": [
    "frame_customer_list"
  ],
  "targetRepositoryId": "repository_xxx"
}
```

### 7.2 兼容边界

现有 MCP 继续保留：

- `multica_design_get_restore_pack`；
- `multica_design_list_files`；
- `multica_design_list_frames`；
- `multica_design_list_groups`；
- `multica_design_get_selection_context`；
- `multica_design_get_ui_restore_artifact`。

旧 Figma Prompt 和客户端不因新工具失效。新统一入口内部可以复用旧 Restore Pack，不要求一次性迁移全部调用方。

### 7.3 MCP 输出

MCP 返回轻量 Manifest 和本地相对路径，不内联大包：

```json
{
  "schema_version": "multica.design-implementation-context/v1",
  "context_path": ".agent_context/design_implementation/context.json",
  "design_package_path": ".agent_context/design_implementation/design/package",
  "scope_path": ".agent_context/design_implementation/design/scope.json",
  "repository_context_path": ".agent_context/design_implementation/repository",
  "source_capabilities": {
    "has_layers": false,
    "has_prototype": true,
    "has_assets": true,
    "has_interactions": true
  }
}
```

不得返回：

- 对象存储 key；
- 用户机器绝对路径；
- 任意未经验证的磁盘路径；
- 未固定 revision 的动态内容。

## 8. Source Adapter

### 8.1 通用接口

两种来源实现相同的内部 Adapter 契约：

```text
ResolveDesign
ResolveRevision
ListFrames
ValidateScope
MaterializePackage
BuildSourceInstructions
BuildVerificationTargets
```

Adapter 输出进入统一 Implementation Context。

### 8.2 Figma Source Adapter

复用现有能力：

- Design File 和固定 Design Revision；
- Frame、Group、Layer；
- Native JSON；
- Asset、Slice、缩略图；
- Restore Pack；
- Selection Context。

来源专属指令：

- 禁止整帧截图替代；
- 可见的文本、容器、表单、表格、按钮和交互控件必须结构化，只有原始图片内容可以保持 raster；
- 文本不能烘焙进图片；
- 多状态归入同一业务组件；
- 使用真实 Asset 和 Slice；
- 无法结构化时明确报告。

### 8.3 Design Document Source Adapter

输入：

- Design Document；
- 固定 saved revision；
- Manifest；
- `brief.json`；
- `coverage.json`；
- `prototype/`；
- CSS、JavaScript、Assets；
- Page、State、Flow；
- pinned design system digest；
- Audit 和 Preview receipts。

来源专属指令：

- Prototype 是结构、状态和交互规格；
- 禁止原样复制整份 HTML/CSS；
- 禁止 iframe 或 `dangerouslySetInnerHTML` 承载整页；
- 必须转换到目标仓库技术栈；
- 必须覆盖所选 Page 内状态、弹窗和交互；
- Brief/Coverage 要求但 Prototype 缺失时报告包不完整。

## 9. 统一本地 Context 目录

### 9.1 v2 目录

```text
.agent_context/design_implementation/
├── context.json
├── design/
│   ├── manifest.json
│   ├── package/
│   └── scope.json
├── repository/
│   ├── checkout.json
│   ├── architecture.json
│   └── component-candidates.json
└── result/
    └── implementation-result.json
```

现有 `.agent_context/design_delivery/package/` 作为 v1 兼容路径保留，不破坏在途和历史任务。

### 9.2 `context.json`

```json
{
  "schema_version": "multica.design-implementation-context/v1",
  "design_ref": "design_xxx",
  "revision_id": "revision_xxx",
  "content_digest": "sha256:...",
  "frame_refs": [
    "frame_customer_list"
  ],
  "project_id": "project_xxx",
  "project_resource_id": "repository_xxx",
  "created_at": "2026-08-26T12:00:00Z"
}
```

### 9.3 `design/package/`

Figma：

```text
restore-pack.json
native.json
assets/
thumbnails/
```

Design Document：

```text
brief.json
coverage.json
manifest.json
prototype/
assets/
```

设计包只读，Agent 不得修改。

### 9.4 本地 Agent 与任务 Agent

任务 Agent：

```text
Agent claim
→ 守护进程下载并校验设计包
→ 写入统一 Context 目录
→ Agent 实现
```

本地 MCP：

```text
Agent 调用 MCP
→ CLI 获取 Manifest
→ 下载固定 revision
→ 校验 digest
→ 写入统一 Context 目录
→ 返回相对路径
```

两者不建立第二套文件布局。

## 10. 目标仓库架构取证

### 10.1 取证位置

Server 验证项目与仓库关系。实际代码架构分析在目标 checkout 中完成：

- 本地 Agent 读取用户当前 checkout；
- 任务 Agent 读取守护进程固定 checkout。

不使用过期 Server 全局扫描结果冒充当前仓库现实。

### 10.2 `checkout.json`

```json
{
  "remote": "repository-remote",
  "branch": "feature/customer-list",
  "commit": "git-commit-sha",
  "dirty": true
}
```

不得上传绝对路径、未提交源码或密钥。

### 10.3 `architecture.json`

至少记录：

```json
{
  "framework": "vue",
  "framework_version": "3",
  "ui_library": "element-plus",
  "style_language": "scss",
  "routing": "vue-router",
  "state_management": "pinia",
  "source_roots": ["src"],
  "commands": {
    "typecheck": "pnpm typecheck",
    "test": "pnpm test",
    "build": "pnpm build"
  }
}
```

### 10.4 `component-candidates.json`

记录可复用候选：

```json
{
  "components": [
    {
      "name": "CustomerTable",
      "path": "src/components/customer/customer-table.vue",
      "reason": "现有客户列表、分页和批量选择能力"
    }
  ]
}
```

候选不是结论。Agent 必须读取源码后决定复用。

## 11. Design Document 到目标代码转换

### 11.1 混合架构

转换采用：

```text
确定性解析 Design Document
+
实际 checkout 仓库分析
+
Agent 语义实现
+
强制验证
```

不开发纯字符串级 HTML → Vue/React 转译器。

### 11.2 Vue + Element Plus 示例

```text
HTML Prototype
→ 解析页面结构、状态和交互
→ 搜索现有页面和业务组件
→ 优先复用现有封装
→ 不足部分映射 Element Plus
→ 生成 Vue SFC
→ 接入 Vue Router
→ 接入现有 Pinia/API 层
→ 转换到仓库 SCSS/Token 体系
→ 运行测试和真实页面验收
```

映射示例：

| Prototype | 目标实现 |
| --- | --- |
| `<button>` | 现有 Button；否则 `ElButton` |
| HTML table | 现有业务表格；否则 `ElTable` |
| 手写 modal | 现有弹窗；否则 `ElDialog` |
| 本地状态 | 项目现有 Composition API / Pinia |
| 页面切换 | 当前 Vue Router 结构 |
| 固定颜色 | 仓库 Token、CSS Variable 或 SCSS Variable |
| 假数据 | 项目 Mock/API 边界，不混入生产请求 |

### 11.3 禁止事项

- 复制整份 Prototype HTML；
- 复制整块 Prototype CSS；
- iframe 嵌入；
- `dangerouslySetInnerHTML` 或等价整页注入；
- 为仓库已有能力创建重复组件；
- 绕过路由、状态管理和 API client；
- 未读仓库直接套用通用模板。

## 12. Agent 执行顺序

Agent 必须按顺序：

1. 读取 `context.json`；
2. 读取 Frame scope；
3. 读取设计包；
4. 检查当前分支和 dirty worktree；
5. 生成仓库架构事实；
6. 搜索现有路由、页面和组件；
7. 形成实现计划；
8. 实现代码；
9. 执行验证；
10. 写 `implementation-result.json`；
11. 在任务评论中给出面向人的摘要。

Agent 不得：

- 覆盖无关 dirty changes；
- 修改用户未选仓库；
- 扩散到无关模块；
- 自动提交、推送或建 PR；
- 把 `.agent_context` 加入提交；
- 将本地绝对路径写回 Server。

## 13. 统一结果契约

### 13.1 文件位置

```text
.agent_context/design_implementation/result/implementation-result.json
```

### 13.2 Schema

```json
{
  "schema_version": "multica.design-implementation-result/v1",
  "design_ref": "design_xxx",
  "revision_id": "revision_xxx",
  "repository_commit_before": "commit-sha",
  "status": "completed",
  "mappings": [
    {
      "frame_ref": "frame_customer_list",
      "target_files": [
        "src/views/customer/list.vue",
        "src/views/customer/list.scss"
      ],
      "target_components": [
        "CustomerList"
      ],
      "reused_components": [
        "CustomerTable",
        "SearchForm"
      ]
    }
  ],
  "checks": [
    {
      "name": "typecheck",
      "status": "passed"
    }
  ],
  "blockers": []
}
```

### 13.3 状态

```text
completed
partial
blocked
failed
cancelled
```

`completed`：全部 Frame 实现、必要检查和视觉验收通过。

`partial`：已有可用代码，但部分 Frame、状态或检查未完成。

`blocked`：需要用户、设计或仓库契约决策。

`failed`：Context、执行、代码或结果解析失败。

`cancelled`：用户主动停止，保留已完成结果和摘要。

自然语言评论不是事实源；结构化结果是事实源。

## 14. 输入门禁

### 14.1 通用

- Design Ref 有效；
- revision 属于设计稿；
- Frame 属于 revision；
- 目标仓库与项目一致；
- 用户有权限；
- content digest 一致；
- 选择范围非空；
- 设计包完整可读。

### 14.2 Figma

- Design Revision 有效；
- Native JSON 通过校验；
- Frame/Group 存在；
- Asset 可读取；
- Restore Pack 能生成。

### 14.3 Design Document

- 存在 saved revision；
- Package schema 可识别；
- Audit 通过；
- Preview 通过；
- Prototype 入口存在；
- Manifest Page 存在；
- Brief、Coverage 和 Assets 可读。

## 15. 来源专属实现门禁

### 15.1 Figma

- 禁止整图；
- 可见的文本、容器、表单、表格、按钮和交互控件必须结构化，只有原始图片内容可以保持 raster；
- 文本不烘焙进图片；
- 使用真实 Asset；
- 多状态归入同一业务组件；
- Frame → 文件映射完整。

### 15.2 Design Document

- 不复制 Prototype；
- 转换到目标技术栈；
- 覆盖 Frame 内默认、加载、空、错误状态；
- 覆盖页面内弹窗和交互；
- Coverage 属于该 Page 的要求得到验证；
- Brief/Coverage 与 Prototype 不一致时报告设计包缺口。

## 16. 目标仓库门禁

### 16.1 复用优先

新建组件前必须：

1. 搜索目标路由；
2. 搜索业务模块；
3. 搜索已有页面；
4. 搜索 UI 组件；
5. 搜索表格、表单和弹窗封装；
6. 阅读候选实现；
7. 在结果中说明复用或不能复用的原因。

### 16.2 架构边界

必须遵守目标仓库：

- 包依赖方向；
- 路由；
- 状态管理；
- API client；
- UI 组件边界；
- i18n；
- 命名；
- 测试放置；
- 构建和 lint 规则。

### 16.3 改动边界

- 检查分支和 dirty worktree；
- 保留无关修改；
- 只修改目标仓库；
- 不重构无关代码；
- 不提交、推送或建 PR，除非用户明确要求。

## 17. 自动化验证门禁

### 17.1 代码检查

根据目标仓库 `architecture.json` 执行：

- typecheck；
- lint；
- 相关测试；
- build；
- `git diff --check`。

不得写死 Multica 仓库命令。

### 17.2 运行时检查

至少验证：

- 目标路由可打开；
- 页面非空；
- 无新增控制台错误；
- 无失败资源；
- 关键交互可操作；
- 状态和弹窗可进入；
- 未使用整图或 Prototype iframe。

### 17.3 视觉检查

每个选中 Frame 都需要设计目标与真实页面截图对照，覆盖：

- 页面结构；
- 信息层级；
- 尺寸与间距；
- 颜色和字体；
- 表格、表单、弹窗；
- 关键响应式断点。

HTTP 200、构建成功、DOM 非空不能代替视觉验收。

## 18. 失败保护

失败、部分完成、阻塞、取消时必须：

- 保留用户原有 dirty worktree；
- 不删除已有文件；
- 不覆盖无关修改；
- 输出已修改文件；
- 输出已完成和未完成 Frame；
- 输出最后成功检查；
- 输出恢复建议；
- 仍写合法的 `implementation-result.json`。

## 19. 任务评论回写

Agent 在原任务评论中摘要：

```text
完成的 Frame
修改的文件
复用的组件
新增的组件
执行的检查
视觉验收结果
剩余阻塞
```

评论线程继续作为人和 Agent 的协作面；不新建其他前端任务承接同一次工作，除非用户明确要求。

本方案不自动改变任务状态。

## 20. API 与 MCP 错误

至少稳定定义：

- `design_ref_invalid`；
- `design_not_found`；
- `revision_not_found`；
- `revision_not_restorable`；
- `frame_ref_invalid`；
- `project_mismatch`；
- `repository_not_found`；
- `repository_project_mismatch`；
- `forbidden`；
- `design_package_invalid`；
- `context_materialization_failed`；
- `implementation_result_invalid`。

错误文案必须说明用户可以采取的下一步，不暴露对象存储、SQL、绝对路径或密钥。

## 21. 实施切片

### Slice 1：统一 Ref 与 Frame API

- DesignAsset projection；
- `design_ref`；
- `frame_ref`；
- 统一 Frame 列表；
- Figma 和 Design Document Adapter 接口；
- saved/valid revision 校验。

### Slice 2：Server Prompt 与统一 MCP

- `implementation-prompt` API；
- 评论草稿注入；
- `multica_design_get_implementation_context`；
- Figma Restore Pack 兼容；
- Design Document Source Adapter。

### Slice 3：统一 Context 目录

- v2 materialization；
- 任务 Agent；
- 本地 MCP；
- digest 校验；
- v1 delivery 路径兼容；
- 清理和失败恢复。

### Slice 4：仓库分析与 HTML 代码转换

- checkout 事实；
- architecture；
- component candidates；
- Vue/Element Plus/SCSS 首个真实目标；
- Frame mapping；
- 结构化结果。

### Slice 5：完整门禁与真实闭环

- 代码检查；
- 运行时验证；
- 视觉对照；
- partial/blocked/failure/cancelled；
- 任务评论回写；
- 本地 Agent 和任务 Agent 双路径验收。

## 22. 验收矩阵

至少覆盖：

### Figma 路径

- 单 Frame；
- Group 多 Frame；
- Asset 和 Slice；
- 禁止整图；
- 现有 Prompt/MCP 兼容；
- 统一 MCP 输出。

### Design Document 路径

- 单 Page；
- 多 Page 选择其中一项；
- Frame 内多状态；
- 弹窗和页面内交互；
- Vue 3 + Element Plus + SCSS；
- 复用已有组件；
- saved-only；
- Draft 拒绝；
- Package Audit/Preview 拒绝。

### 入口

- 任务右侧栏生成 Prompt；
- Prompt 仅注入、不自动发送；
- 用户修改后发送；
- 任务 Agent 调用 MCP；
- 创建 task 携带统一引用；
- 本地 Agent 直接调用 MCP。

### 仓库状态

- 干净 worktree；
- 存在无关 dirty changes；
- 错误分支；
- 找到现有组件；
- 找不到可复用组件；
- typecheck 基线失败；
- 页面运行失败；
- 用户取消。

## 23. 不算完成

以下任一情况存在，都不能宣布方案完成：

- 只统一列表，没有统一 MCP；
- 只生成 Prompt，没有真实 Agent 执行；
- 前端仍判断 Figma/Design Document 并拼两套 Prompt；
- Design Document Draft 可以还原；
- MCP 返回未固定 revision；
- Design Document 直接复制 HTML；
- Figma 使用整图；
- 未读仓库就生成目标框架代码；
- 只返回自然语言，没有 Frame mapping；
- typecheck 通过但运行或视觉失败；
- partial 被标记 completed；
- 覆盖用户无关修改；
- 本地 MCP 和任务 Agent 使用不同 Context 契约；
- 破坏现有 Figma Restore Pack 兼容。

## 24. 明确非目标

本方案不包含：

- Open Design 与 Multica Design 结果对比；
- Design Document 与 Figma 相同内容自动匹配；
- 同一逻辑设计稿的多 Representation 合并；
- 设计稿相似度去重；
- 自动选择目标仓库；
- 自动发送任务评论；
- 自动改变任务状态；
- Agent 自动提交、推送或建 PR；
- 任务反向创作的完整产品流程；
- 从需求到设计和代码的完整自动化状态机。

上述能力进入后续独立讨论。Open Design 对比最后进行。

## 25. 已确认决策

用户已确认：

- Figma 与 Design Document 在用户侧统一为设计稿；
- 内部实体继续分开；
- 不做相同内容自动匹配或融合；
- 用户自行选择设计稿、Frame 和仓库；
- Figma Frame/Group 与 Design Document Page 在 UI 中统一称为 Frame；
- Design Document Page 内状态和页面内交互自动包含；
- 任务右侧栏生成 Prompt 并注入评论输入框；
- 不自动发送评论或启动 Agent；
- Server 根据来源选择 Adapter；
- 新增统一 `design_ref`、`frame_ref` 和 Implementation Context；
- Design Document 使用确定性解析 + 仓库分析 + Agent 代码转换；
- 本地 Agent 与任务 Agent 使用统一 Context 目录；
- 结构化结果、仓库复用、运行时和视觉门禁均已确认。
