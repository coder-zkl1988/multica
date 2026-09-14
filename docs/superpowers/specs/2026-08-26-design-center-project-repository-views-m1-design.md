# 设计中心项目/仓库双视角 M1 产品与技术方案

> 状态：产品方案已确认，待用户审阅书面 Spec
> 日期：2026-08-26
> 适用范围：设计中心 M1 信息架构、设计资产仓库关联、仓库设计体系、旧模板资产隐藏
> 当前基线：`main@a7606af71`
> 事实源：`docs/product/design-center/README.md`、本方案确认过程、当前 Electron 桌面应用和当前代码

## 1. 摘要

M1 将设计中心的资产浏览方式调整为两个工作区级视角：

1. **项目视角**：按项目聚合设计资产，只展示“设计稿”和“设计草稿”；
2. **仓库视角**：按代码仓库精确展示人工关联的设计资产，并增加仓库专属“设计体系”。

两个视角通过类似 macOS Finder 的图标分段控件切换，不增加文字 Tab。首页、已打开对象的工作区 Tab、首页“创作 / 社区 / 设计体系”、场景配方和示例提示词继续保留。

设计稿仍由项目拥有，代码仓库是可选、最多一个、可后续人工调整的关联。设计师通过 Figma 插件上传时只需要选择项目，不被迫理解开发仓库；项目负责人或开发人员随后可以从仓库视角批量关联，或从项目视角对单份资产关联。

仓库没有专属设计体系时，仓库“设计体系”面板直接原地展示统一创建表单；仓库已有体系时直接展示对应内容。现有“仓库体系不存在时自动回落到项目通用体系”的行为取消，避免用户看不到的体系成为隐形设计约束。

M1 只隐藏项目工作区中的旧“模板资产”及 Figma 插件对应上传选项。首页社区及其场景配方不是本次隐藏对象。

## 2. 当前实现事实

### 2.1 当前桌面端界面

2026-08-26 在 Electron 应用 `Multica Canary main-3031` 中确认：

- 设计首页包含“创作 / 社区 / 设计体系”；
- 项目工作区包含“设计稿 / 设计草稿 / 模板 / 设计体系”；
- 项目设计体系面板内部使用“项目通用 / 仓库 A / 仓库 B”范围切换器；
- 仓库没有专属体系时，当前查询会回落并展示项目通用体系；
- 项目“模板”面板展示由 Figma 插件以“模板资产”类型上传的旧资产；
- 首页“社区”与项目“模板资产”是两个不同概念。

当时本地服务状态：

- `GET http://localhost:8080/health` 返回 `200`；
- `GET http://localhost:8080/readyz` 返回 `200`；
- Electron renderer `http://localhost:5175/` 返回 `200`。

### 2.2 当前数据实体

#### `design_file`

`design_file` 是 Figma、上传或旧生成链形成的画板型设计资产：

- 稳定身份位于 `design_file`；
- 版本位于 `design_revision`；
- Frame、Layer、Native JSON、缩略图、切图和资源属于该链路；
- 当前只有 `project_id` 和 `folder_id`，没有仓库字段。

#### `design_document`

`design_document` 是首页“创作”形成的原生设计工作区：

- 保存 brief、项目、智能体、任务、配方和输入快照；
- 通过不可变 `design_document_revision` 演进；
- 维护 draft/saved 指针；
- 支持生成、调整、重新生成、保存和放弃；
- 已有可空 `project_resource_id`，当前含义是 Design Document 面向的可选目标仓库，并用于后续取证、设计体系解析、调整和重新生成。

#### 旧 `design_draft`

当前“设计草稿”Tab 主要读取旧模板、PageSpec 和语义编译链的 `design_draft`。它不是新版 Design Document 的 draft/saved 模型。

### 2.3 当前前端结构

`packages/views/designs/designs-page.tsx` 当前同时负责：

- 首页与项目工作区 Tab；
- 首页三个二级面板；
- 项目四个资产 Tab；
- Figma 文件与 Design Document 混合展示；
- 旧模板、旧草稿和项目设计体系；
- 项目范围内的仓库设计体系切换。

M1 不继续把所有新增职责堆回该组件，而是拆出清晰的工作区与列表边界。

## 3. 产品目标

M1 完成后，用户应能够：

1. 使用 Finder 式图标控件在项目和仓库两种浏览视角之间切换；
2. 从项目视角查看项目下全部设计稿和新版设计草稿；
3. 从仓库视角查看明确关联该仓库的设计稿、设计草稿和仓库专属设计体系；
4. 在不阻塞设计师上传的情况下，由人工后续建立设计稿与代码仓库的关系；
5. 在仓库没有设计体系时直接创建，在已有体系时直接使用；
6. 不再接触项目工作区旧“模板资产”概念；
7. 不因隐藏 UI 而删除历史数据或破坏旧客户端兼容。

## 4. 非目标

M1 明确不包含：

- 新生成稿与 Figma 设计稿的内容级合并；
- `design_file` 与 `design_document` 数据库实体合并；
- 一份设计稿关联多个仓库；
- 任务（Issue）反向发起创作；
- 需求 → 原型 → 设计稿 → 代码还原的自动化链路；
- MCP 自动选择或修改目标仓库；
- 首页社区改版；
- 场景配方或示例提示词下线；
- 历史模板资产、旧 `design_draft`、相关表或接口的删除；
- 不可逆的数据清理；
- 自动把已有设计稿推断或回填到仓库。

上述能力分别进入后续独立 Spec，不得顺手扩进 M1。

## 5. 产品术语

### 5.1 设计稿

用户可长期查看和使用的设计结果：

- Figma 插件成功上传或更新的 `design_file`；
- 已拥有 saved revision 的 `design_document`。

如果 Design Document 已保存且同时存在新调整：

- “设计稿”展示 saved 版本；
- 卡片标记“有未保存调整”；
- draft 版本同时出现在“设计草稿”。

### 5.2 设计草稿

新版 Design Document 中尚未完成保存确认的工作：

- 首次生成中；
- 首次生成失败、允许重试；
- 首次生成完成、等待保存；
- 已保存设计稿的新调整草稿；
- 调整失败但最近 saved 仍有效。

旧模板链 `design_draft` 不再定义新的“设计草稿”。

### 5.3 模板资产

本方案中的“模板资产”只指：

- 项目工作区当前“模板”Tab；
- Figma 插件当前“模板资产 / 作为模板上传 / 发布为模板”能力；
- 对应旧 `design_template`、catalog template 和旧草稿链。

首页“社区”、场景配方和示例提示词不属于该定义。

### 5.4 仓库关联

仓库关联是设计资产的可选元数据，不改变资产的项目所有权：

```text
项目拥有设计稿
仓库最多关联一个
未关联仓库是合法状态
```

仓库关联不等于：

- Agent 已读取仓库；
- 已经开始代码实现；
- 已经完成设计还原；
- 任务状态应当变化。

## 6. 信息架构

### 6.1 顶部工作区

现有首页和已打开对象 Tab 保留：

```text
首页 | 已打开项目或仓库 | +
```

在顶部工具区增加 Finder 式图标分段控件：

```text
[项目视角图标] [仓库视角图标]
```

要求：

- 不显示“按项目 / 按仓库”文字 Tab；
- 图标必须有 Tooltip；
- 图标必须有 `aria-label`；
- 支持键盘聚焦和切换；
- 当前选中态使用 Finder 风格的浅色表面、边框和轻阴影；
- 不使用颜色作为唯一选中信号。

### 6.2 工作区级双视角

#### 项目视角

“+”列出当前工作区可访问的项目。打开后形成可关闭的项目工作区。

项目内容只包含：

```text
设计稿 | 设计草稿
```

项目视角不显示：

- 模板资产；
- 设计体系；
- 设计体系仓库切换器；
- 隐藏但仍占空间的空 Tab。

#### 仓库视角

“+”列出当前工作区内、属于可访问项目且资源类型为 `github_repo` 的仓库。打开后形成可关闭的仓库工作区。

仓库内容包含：

```text
设计稿 | 设计草稿 | 设计体系
```

同名仓库必须始终同时显示仓库名和所属项目，并在 Tooltip 中提供完整远端仓库路径。

### 6.3 视角状态

项目视角和仓库视角分别维护：

- 已打开对象列表；
- 当前激活对象；
- 当前内容面板；
- 当前搜索词。

切换视角不能串用另一视角的当前对象。

M1 只保证在当前设计中心页面生命周期内记忆状态；应用重启后不恢复这两套打开项，也不为此引入新的服务端状态。

## 7. 设计资产展示规则

### 7.1 项目视角

项目视角按 `project_id` 聚合：

- 未关联仓库的 Figma 设计稿；
- 已关联仓库 A 的 Figma 设计稿；
- 已关联仓库 B 的 Figma 设计稿；
- 项目下全部 Design Document；
- 同一 Design Document 的 saved/draft 状态投影。

设计卡片必须显示：

- 当前关联仓库名称；
- “未关联仓库”；
- 来源类型；
- 当前状态；
- 是否有未保存调整。

### 7.2 仓库视角

仓库视角只按当前资产行的 `project_resource_id` 精确查询。

不得根据以下内容推断纳入：

- Design Document 历史输入快照中的仓库；
- 历史 task 的 grounding 仓库；
- 任务关联；
- 设计还原目标；
- 项目只有一个仓库；
- 文件名、标题或 URL；
- Agent 自己的判断。

未关联资产不出现在任一仓库视角。

### 7.3 saved/draft 双投影

| Design Document 状态 | 设计稿 | 设计草稿 |
| --- | --- | --- |
| 首次生成中 | 否 | 是 |
| 首次生成失败、可重试 | 否 | 是 |
| 首次生成完成、等待保存 | 否 | 是 |
| 已保存且无新调整 | 是 | 否 |
| 已保存且有新调整 | 是，显示 saved | 是，显示 draft |
| 调整失败且 saved 有效 | 是，显示 saved | 是，显示失败草稿 |

Figma 上传成功后直接进入“设计稿”，不进入 Design Document 草稿状态机。

## 8. 仓库人工关联

### 8.1 产品原则

- Figma 上传与首页创作中的仓库选择均为可选；
- 设计师可以只选择项目；
- 不隐式推断仓库；
- 一份设计稿最多关联一个仓库；
- 后续人工关联是正常流程，不是异常补救；
- 设计还原执行前仍需再次确认目标仓库。

### 8.2 双入口

#### 仓库视角主入口

仓库工作区提供“关联设计稿”：

- 从仓库所属项目中选择设计资产；
- 默认聚焦未关联资产；
- 支持搜索；
- 支持多选；
- 确认后批量关联到当前仓库。

#### 项目视角辅助入口

单份设计卡片的更多菜单提供：

- 关联仓库；
- 更换仓库；
- 取消关联。

两个入口调用同一个领域操作和校验逻辑。

### 8.3 人工确认

以下动作必须明确确认：

- 首次关联；
- 从仓库 A 更换到仓库 B；
- 取消关联；
- 批量关联多份资产。

### 8.4 运行中保护

Design Document 存在活动生成、调整或重新生成 task 时，禁止修改 `project_resource_id`。

原因：运行中的任务已经固定自己的输入和仓库工作目录；中途切换会导致页面当前状态与正在执行的上下文不一致。

## 9. 数据模型

### 9.1 `design_file`

新增可空字段：

```sql
project_resource_id UUID
```

语义：

- `NULL`：未关联仓库；
- 非 `NULL`：当前人工关联的唯一代码仓库。

要求：

- 不增加数据库外键；
- 应用层验证工作区、项目和资源类型；
- 为仓库视角查询增加合适索引；
- 索引使用 `CREATE INDEX CONCURRENTLY`；
- 每个并发索引独立 migration；
- 在当前 `main@a7606af71` 基线上使用 906 至 908；实施前只校验编号仍未被占用，不允许静默改用 upstream 序号。

### 9.2 `design_document`

复用已有：

```sql
project_resource_id UUID
```

不新增第二个 `linked_project_resource_id`。

该字段表示 Design Document 当前关联和后续执行面向的仓库：

- 首次创建时选择仓库则写入；
- 未选择则为空；
- 后续允许在无活动任务时人工调整；
- 后续调整和重新生成使用当前值；
- 历史 task、input snapshot 和 revision 证据不因修改而改变。

### 9.3 旧 `design_draft`

M1 不增加仓库字段，不做数据迁移，不进入新列表。

### 9.4 仓库解绑

项目资源解绑或删除时，在现有应用事务中：

- 将相关 `design_file.project_resource_id` 清空；
- 将相关 `design_document.project_resource_id` 清空；
- 保留设计文件、文档、revision、附件和预览；
- 历史任务与取证证据不变；
- 仓库专属设计体系按现有资源清理规则处理；
- 不使用级联删除。

## 10. 仓库取证语义

### 10.1 当前问题

当前响应逻辑把：

```text
project_resource_id 非空
```

直接当作：

```text
repository_grounded = true
```

人工关联上线后该逻辑不再成立。

### 10.2 修正语义

- `project_resource_id`：当前关联和后续任务目标仓库；
- `repository_grounded`：当前展示的 draft/saved revision 是否存在真实仓库取证证据。

实际证据来自：

1. revision 对应 task 的固定输入；
2. task context 的 grounding mode；
3. 已验证的 `repository-grounding.json`；
4. revision/task provenance。

刚完成人工关联但还未重新执行 Agent 时：

```text
project_resource_id = 仓库 A
repository_grounded = false
```

后续调整读取仓库 A 并成功形成新 revision 后，该 revision 才能显示：

```text
repository_grounded = true
```

## 11. 仓库设计体系

### 11.1 无专属体系

仓库“设计体系”面板直接原地展示统一创建表单，不增加空状态中转和独立路由跳转。

自动带入并锁定：

- 所属项目；
- 所属仓库；
- 仓库远端地址，只读展示。

自动预填但允许修改：

- 名称：默认“仓库名 + Design System”；
- 设计目标：项目名称、项目描述和仓库名称组成的初始草稿。

必须人工选择：

- 执行智能体；
- 平台：Web、移动端或跨端。

可选且默认不填：

- 品牌色；
- 参考链接；
- 附件；
- Figma UI 规范；
- 项目设计稿；
- 官方或已有设计体系参考。

仓库分析仍由用户主动发起，不隐式扫描。

生成失败时保留全部输入。生成和强制门禁通过后，当前面板原地切换为设计体系草稿/内容视图。

### 11.2 有专属体系

直接展示当前仓库的设计体系内容、状态、Preview/UI Kit、调整和保存操作。

### 11.3 取消自动回落

仓库没有专属体系时，不能自动返回或使用项目通用体系。

设计上下文解析调整为：

```text
显式选择的设计体系
    ↓ 没有
仓库专属 saved 体系
    ↓ 没有
本地 DESIGN.md
    ↓ 没有
仓库现实
    ↓ 没有
none
```

历史项目通用体系：

- 保留数据；
- 不在项目视角展示；
- 不自动作用于仓库；
- 可以被用户明确选择为复制来源或参考资料；
- 后续是否退役进入独立 Spec。

## 12. 模板资产隐藏

### 12.1 Design Center

项目和仓库工作区均不得显示：

- “模板”Tab；
- 模板计数；
- “搜索模板”；
- “当前项目暂无模板资产”；
- 模板草稿创建、审核和物化入口。

### 12.2 Figma 插件

新版插件不得显示：

- “模板资产”类型；
- “作为模板上传”；
- “发布为模板”；
- 模板名称；
- 模板分类；
- Slot schema 配置。

新版插件不再主动构造 `asset_type=template` 请求。

### 12.3 兼容边界

M1 不删除服务端旧模板接口，不删除历史模板数据。旧版本插件的历史请求继续按兼容策略处理。

首页以下能力不受影响：

- 社区；
- 场景配方；
- 示例提示词；
- `recipe` 字段。

## 13. 前端组件边界

```text
DesignsPage
├── DesignWorkspaceHeader
│   ├── DesignWorkspaceTabs
│   ├── FinderViewModeSwitcher
│   └── DesignSearch
│
├── ProjectDesignWorkspace
│   ├── DesignAssetsPanel
│   └── DesignDraftsPanel
│
└── RepositoryDesignWorkspace
    ├── RepositoryIdentity
    ├── DesignAssetsPanel
    ├── DesignDraftsPanel
    └── RepositoryDesignSystemWorkspace
```

新增或抽取的关键职责：

### `FinderViewModeSwitcher`

只负责项目/仓库视角切换和无障碍交互。

### `DesignAssetListItem`

Core 层统一列表投影：

```ts
type DesignAssetListItem = {
  id: string;
  kind: "figma_file" | "design_document";
  projectId: string;
  projectResourceId: string;
  title: string;
  thumbnailUrl?: string;
  sourceLabel: string;
  status: string;
  hasSavedVersion: boolean;
  hasDraftVersion: boolean;
  repositoryGrounded: boolean;
  updatedAt: string;
};
```

数据库不合并，列表、搜索和卡片统一。

### `RepositoryAssociationDialog`

统一处理单份和批量关联，失败后保留选择。

### `DesignSystemCreateForm`

从当前两套创建 UI 中抽取共享表单：

```text
standalone workspace mode
repository-bound mode
```

仓库模式通过上下文参数锁定项目和仓库，不复制校验、上传、Agent 选择和提交逻辑。

### `RepositoryDesignSystemWorkspace`

直接绑定一个仓库，不再渲染“项目通用 / 仓库”范围切换器。

## 14. API 契约

### 14.1 统一仓库关联操作

新增：

```http
PUT /api/design-assets/repository-association
```

请求：

```json
{
  "project_id": "project-id",
  "project_resource_id": "repository-id",
  "items": [
    { "kind": "design_file", "id": "file-id" },
    { "kind": "design_document", "id": "document-id" }
  ]
}
```

取消关联时 `project_resource_id` 传空字符串。

服务端必须：

1. 先校验全部资产；
2. 任一项失败则整体失败；
3. 全部通过后在同一事务内更新；
4. 返回关联结果；
5. 发布统一实时失效事件。

### 14.2 列表查询

项目视角：

```http
GET /api/design-files?project_id=...
GET /api/design-documents?project_id=...
```

仓库视角：

```http
GET /api/design-files?project_id=...&project_resource_id=...
GET /api/design-documents?project_id=...&project_resource_id=...
```

仓库参数是精确过滤，不触发 fallback。

### 14.3 设计体系精确查询

```http
GET /api/project-design-systems/by-project
  ?project_id=...
  &project_resource_id=...
```

返回只能表示：

- 当前仓库专属体系；
- 当前仓库未建立体系。

不能把项目通用体系包装成仓库结果。

### 14.4 API 边界

- 网络字段保持 `snake_case`；
- API client 在边界转换；
- Query key 必须包含 `workspace_id`、`project_id` 和 `project_resource_id`；
- UI 不直接解析 `Response`；
- 兼容已有 Figma plugin token 验证和 workspace/project/folder 校验。

## 15. 权限、并发与错误处理

### 15.1 权限

仓库关联沿用项目设计资产编辑权限。只读用户可以查看关联状态，不能修改。

### 15.2 原子性

批量关联必须全部成功或全部失败，不能部分提交。

### 15.3 服务端错误

至少定义并稳定返回：

- `design_asset_not_found`；
- `project_not_found`；
- `project_resource_not_found`；
- `project_resource_not_repository`；
- `project_resource_project_mismatch`；
- `design_document_task_active`；
- `forbidden`；
- `repository_association_failed`。

### 15.4 前端恢复

- mutation 失败后保留已选资产和目标仓库；
- 显示用户可行动的错误，不暴露内部 SQL/UUID 解析信息；
- mutation 成功后主动失效相关 Query；
- WebSocket 丢失不能让 UI 永久停留在旧列表；
- 仓库解绑后卡片回到“未关联仓库”。

## 16. 数据库迁移

M1 只做可逆、增量迁移。当前 `main@a7606af71` 基线预留：

1. `906_design_file_repository_scope`：为 `design_file` 增加可空 `project_resource_id`；
2. `907_idx_design_file_repository_scope`：单语句、并发创建 `(workspace_id, project_id, project_resource_id, updated_at DESC)` partial index；
3. `908_idx_design_document_repository_scope`：单语句、并发创建 `(workspace_id, project_id, project_resource_id, updated_at DESC)` partial index；
4. down migrations 按逆序删除索引和字段；
5. 不回填历史数据；
6. 不新增外键；
7. 不删除模板、项目通用体系或旧草稿数据。

现有 `design_document.project_resource_id` 不迁移，只补充查询、更新与证据语义测试。

## 17. 实施切片

M1 拆成四个有界切片，每个切片独立验证和回滚。

### Slice 1：数据与仓库关联契约

- `design_file.project_resource_id`；
- 精确查询；
- 统一批量关联 API；
- Design Document 活动任务保护；
- 仓库解绑清理；
- `repository_grounded` 证据语义修正。

### Slice 2：项目/仓库双视角

- Finder 控件；
- 两套打开项状态；
- 项目与仓库工作区；
- 统一资产投影；
- saved/draft 双投影；
- 双入口人工关联。

### Slice 3：仓库设计体系

- 精确仓库体系查询；
- 取消项目通用 fallback；
- 共享创建表单；
- 仓库上下文预填；
- 无体系/有体系两种原地状态。

### Slice 4：旧模板资产隐藏与真实验收

- 项目/仓库模板 Tab 下线；
- Figma 插件上传选项下线；
- 旧接口兼容；
- 完整 Electron 验收矩阵；
- 退役账本登记，不删除数据。

不得在 Slice 2 未通过真实资产矩阵前并行扩展任务自动化或设计稿融合。

## 18. 产品验收矩阵

准备：

```text
项目 CRM
├── 仓库 A：prime-saas-fe
├── 仓库 B：staffrnapp
├── Figma 稿 1：未关联
├── Figma 稿 2：关联仓库 A
├── Document 1：仓库 A，已保存
├── Document 2：仓库 A，首次生成失败
├── Document 3：仓库 B，已保存且有调整草稿
├── 历史模板资产
└── 历史项目通用设计体系
```

预期：

| 场景 | 预期 |
| --- | --- |
| 项目 CRM 的设计稿 | Figma 1、Figma 2、Document 1、Document 3 saved |
| 项目 CRM 的设计草稿 | Document 2、Document 3 draft |
| 仓库 A 的设计稿 | Figma 2、Document 1 |
| 仓库 A 的设计草稿 | Document 2 |
| 仓库 B 的设计稿 | Document 3 saved |
| 仓库 B 的设计草稿 | Document 3 draft |
| 未关联 Figma 1 | 不出现在任一仓库 |
| 仓库 A 无专属体系但有项目通用体系 | 显示创建表单，不显示 fallback |
| 仓库 B 有专属体系 | 直接显示仓库专属体系 |
| 历史模板资产 | 新项目/仓库工作区均不可见 |
| 首页社区 | 保持可见且可正常使用 |

## 19. 自动化门禁

### 19.1 后端聚焦测试

```bash
cd /Users/fengyujie/Documents/soyoung/multica/server

go test ./internal/handler \
  -run 'Design(File|Document|Plugin|ProjectResource|ProjectDesignSystem)' \
  -count=1

go test ./internal/service \
  -run 'Design(Context|Document|ProjectDesignSystem)' \
  -count=1
```

必须覆盖：

- `design_file.project_resource_id` 创建、查询和更新；
- `design_document.project_resource_id` 人工更新；
- 活动任务期间拒绝更新；
- 跨项目和非仓库资源拒绝；
- 批量关联原子回滚；
- 仓库解绑后资产保留、关联清空；
- 精确设计体系查询；
- Resolver 不再执行仓库到项目体系 fallback；
- `repository_grounded` 不再由当前字段直接推断；
- 旧 Figma 插件模板请求兼容。

### 19.2 前端聚焦测试

```bash
cd /Users/fengyujie/Documents/soyoung/multica

pnpm --filter @multica/views test
pnpm --filter @multica/core test
pnpm --filter @multica/views typecheck
pnpm --filter @multica/core typecheck
```

必须覆盖：

- Finder 控件键盘和无障碍行为；
- 项目/仓库打开项隔离；
- 统一资产投影；
- saved/draft 双投影；
- 单份和批量关联；
- 失败后保留选择；
- 仓库无体系/有体系；
- 项目模板 Tab 消失；
- 首页社区不受影响；
- Query key 和失效范围正确。

### 19.3 全量工程门禁

```bash
cd /Users/fengyujie/Documents/soyoung/multica

pnpm typecheck
pnpm test
make test
git diff --check
```

## 20. GitNexus 与改动范围门禁

实施阶段必须：

1. 修改任何函数、类或方法前执行 GitNexus `impact`；
2. HIGH 或 CRITICAL 风险先向用户报告；
3. 索引过期时先执行 `rtk node .gitnexus/run.cjs analyze`；
4. FTS 损坏时最多使用一次 `analyze --force` 恢复；
5. 提交前执行：

```bash
rtk node .gitnexus/run.cjs detect-changes \
  --scope compare \
  --base-ref main
```

6. 只接受与 M1 预期实体、组件和执行流一致的变化；
7. 不得把旧 Open Design Worker、任务自动化或设计还原重构混入。

## 21. 真实 Electron 验收门禁

自动化测试、HTTP 200、lint 和 typecheck 都不能替代真实桌面端验收。

必须在 Electron 应用中完成：

1. 打开设计模块；
2. 确认首页仍有“创作 / 社区 / 设计体系”；
3. 使用 Finder 控件切换项目/仓库视角；
4. 分别打开 CRM、仓库 A 和仓库 B；
5. 验证两套视角各自保存打开项；
6. 验证项目聚合与仓库精确筛选；
7. 从仓库视角批量关联；
8. 从项目卡片单份更换或取消关联；
9. 验证运行中的 Design Document 不能换仓库；
10. 验证仓库无体系时原地创建；
11. 验证仓库有体系时直接展示；
12. 验证项目通用体系不自动回落；
13. 验证项目和仓库均无模板资产 Tab；
14. 验证新版 Figma 插件没有模板资产类型；
15. 验证搜索、滚动、缩窄窗口、Tooltip 和键盘操作；
16. 检查控制台错误和失败请求。

验收证据至少包括：

- 项目视角截图；
- 仓库 A 视角截图；
- 仓库 B 视角截图；
- 仓库无体系截图；
- 仓库有体系截图；
- Figma 插件上传类型截图；
- 关键网络请求与响应摘要；
- 数据库或 API 关联结果摘要。

## 22. 不算完成的情况

以下任一情况存在，都不能宣布 M1 完成：

- 只完成 UI，没有真实仓库关联数据；
- 仓库列表只靠前端过滤，服务端没有权限与归属校验；
- 仓库无体系时仍隐式使用项目通用体系；
- Figma 插件仍显示模板资产；
- `repository_grounded` 因人工关联而错误变成 `true`；
- 批量关联部分成功；
- 设计运行中允许切换仓库；
- 只在普通浏览器验收，没有验证 Electron；
- 只验证请求成功，没有验证真实资产内容；
- 删除或迁移历史模板数据；
- 把首页社区一并隐藏；
- 顺手实现任务反向创作或统一设计稿还原与代码实现。

## 23. 回滚边界

M1 保持可独立回滚：

- `design_file.project_resource_id` 是可空增量字段；
- 历史行不回填；
- UI 回滚后新增字段可以暂时保留，不影响旧查询；
- 仓库关联操作只改关联字段，不改 revision 内容；
- 项目通用设计体系和历史模板数据未删除；
- Figma plugin UI 回滚不需要数据库恢复；
- 每个实施切片独立提交，避免功能和清理交叉。

## 24. 后续阶段接口

M1 完成后，后续 Spec 可以基于稳定的 `project_resource_id` 关系推进：

1. 统一设计稿还原与仓库代码实现，详见 `2026-08-26-unified-design-asset-implementation-design.md`；
2. 任务（Issue）反向发起创作，并固定项目、仓库和任务上下文；
3. 需求 → 原型 → 设计稿 → 代码还原的自动化链路；
4. 设计还原完成后的任务状态、人工接管和后续交付。

后续阶段不得绕过 M1 的人工关联、真实取证和 saved/draft 边界。

## 25. 已确认决策记录

本方案已由用户逐项确认：

- M1 先做信息架构、仓库设计体系和资产关联；
- Finder 式控件采用 A1；
- 仓库为工作区级一级对象 R1；
- 一份设计稿最多关联一个仓库；
- 仓库关联可选，由人工后续完成；
- 仓库视角批量关联 + 项目视角单份关联；
- 新“设计草稿”指未保存的 Design Document；
- 仓库无体系采用 S1 原地创建；
- 采用已确认的预填写边界；
- 取消仓库到项目通用体系自动回落；
- 只隐藏项目旧模板资产及 Figma 插件对应类型；
- `design_document` 复用已有 `project_resource_id`；
- `design_file` 增加同名可空字段；
- 组件、API、异常处理和验收门禁均已确认。
