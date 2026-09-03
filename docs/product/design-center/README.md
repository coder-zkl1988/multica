# Multica 设计中心长期产品记忆

> 状态：持续维护
> 最后更新：2026-09-03
> 适用范围：设计中心、设计体系、UI 规范、设计任务、UI Agent、设计稿生成、设计还原、设计 MCP、Open Design 能力接入

## 1. 这份模块解决什么问题

这不是某一阶段的实现计划，而是 Multica 设计产品线的长期事实源。

它负责保存：

- 已经由产品讨论确认的方向；
- 有明确版本和源码依据的外部研究结论；
- 仍在讨论、尚未批准的提案；
- 已暂停、已否决或被替代的历史路线；
- 最终实现前必须解决的开放问题。

历史聊天摘要只能帮助恢复对话，不能替代本模块中的证据和决策状态。

## 2. 强制上下文恢复规则

任何 Agent 在以下情况继续设计产品线工作前，都必须重新读取本文件：

1. 新会话开始；
2. 会话发生上下文压缩；
3. 工作被其他任务打断后恢复；
4. 准备提出产品结论、技术方案或实现计划；
5. 准备修改设计中心、UI Agent、设计体系或 Open Design 接入相关代码。

随后按需要读取：

- [design-center-issue-product-overview.md](./design-center-issue-product-overview.md)：设计中心与 Issue 设计模块的完整时间线、双主线、当前能力、待办和产品收益总览；
- [decision-register.md](./decision-register.md)：确认当前哪些内容已经决定，哪些仍是提案；
- [open-design-evidence.md](./open-design-evidence.md)：凡涉及 Open Design 的判断，必须回到对应版本和源码证据；
- [open-design-engine-integration.md](./open-design-engine-integration.md)：已被替代的 Open Design 固定版本、headless worker 和 Runtime 接入实验，只用于保留技术证据；
- [2026-08-05-multica-native-design-engine-design.md](../../superpowers/specs/2026-08-05-multica-native-design-engine-design.md)：以 Open Design 为核心参照、但不依赖 Worker/Runtime 的 Multica 原生设计引擎方案；
- [2026-08-16-design-center-three-tab-migration-design.md](../../superpowers/specs/2026-08-16-design-center-three-tab-migration-design.md)：当前迁移范围（Open Design 首页 / 社区 / 设计体系三个 tab）、设计体系仓库化、tweaks 与 critique 边界，以及生产端契约对齐；
- [open-design-gap-2026-09-03.md](./open-design-gap-2026-09-03.md)：对照 Open Design 0.21.1 的迁移缺口盘点与建议顺序；
- [open-design-multica-mapping.md](./open-design-multica-mapping.md)：已被替代的早期云端实体映射，只用于理解历史；
- [project-design-system-validation.md](./project-design-system-validation.md)：项目设计体系第一阶段的真实链路、持久化与失败保护证据，以及尚未完成的验收项；
- [project-design-system-workspace-validation.md](./project-design-system-workspace-validation.md)：项目设计体系工作区的创建、渲染校验、保存、调整隔离和放弃恢复证据；
- 历史文档：只用于了解已有实现和失败经验，不自动继承为当前方案。

恢复后必须遵守：

- 不把聊天摘要中的推断提升为已确认决策；
- 不把旧实现的存在当成继续沿用它的理由；
- 不把某个 Open Design 版本的行为描述成永久事实；
- 不以任务完成、草稿通过或测试通过代替真实产物和视觉质量验证。

## 3. 记录等级

本模块只使用以下状态：

| 状态 | 含义 |
| --- | --- |
| `confirmed` | 用户已经明确确认，可以约束后续方案 |
| `evidence` | 已通过指定版本、源码、运行结果或持久化数据验证 |
| `proposal` | 值得讨论，但尚未获得确认 |
| `open` | 仍需研究或做产品选择 |
| `paused` | 当前停止推进，但可能保留局部价值 |
| `rejected` | 已明确否决，不得悄悄重新引入 |
| `superseded` | 曾经成立，后来被新决策替代 |

只有 `confirmed` 决策可以直接进入实现方针。`evidence` 负责支撑判断，但证据本身不等于 Multica 必须照搬。

## 4. 已确认的产品地基

### P-001 Multica 的目标

`confirmed`

Multica 以项目为切入点，通过 Issue 连接需求、设计、开发和最终落地，并结合云端 Agent 与本地 Agent 完成完整的软件交付流程。

### P-002 人与 Agent 的关系

`confirmed`

人和 Agent 必须共享同一份工作上下文。人可以随时观察、介入和接管，之后也可以把任务重新交还 Agent，不能因为接管导致上下文和产物链断裂。

### P-003 设计能力的产品位置

`confirmed`

设计稿上传、UI 规范、设计体系、设计稿生成、MCP 和设计稿还原都只是需求交付流程中的能力，不是脱离 Project 和 Issue 独立运行的最终产品。

### P-004 旧决策：选择性接入 Open Design

`superseded`

Multica 研究并选择性接入 Open Design 的部分能力，用于补充在线设计生产、设计体系和未来模板能力。目标不是复制 Open Design，也不是用它替换 Multica 的 Project、Issue 和 Agent 控制面。

替代原因：该决策曾被“直接采用 Open Design 上游引擎”替代；2026-08-05 又由 P-010 / DC-039 重新收敛为“以 Open Design 为核心参照、由 Multica 原生翻新”，因此本条继续只保留为历史。

### P-005 设计体系是源，UI 规范是派生产物

`confirmed`

项目设计体系是 Multica 管理的设计事实源。它至少同时包含 Agent 可读规则和机器可执行 Tokens，并根据真实来源逐步增加组件、状态与模式。Multica 根据设计体系生成在线 UI Kit，供人预览、调整和 Agent 使用。

Figma UI 规范不是建立设计体系的硬性要求。已有 Figma UI 规范可以作为可选导入证据；未来也可以由设计体系生成或同步原生 Figma UI Kit，但原生 Figma 写回不属于第一阶段。

没有来源材料、也没有用户主动创建意图的空项目必须保持未建立状态，不能由 Agent 自动猜测设计体系。用户明确发起从零创建时，可以根据产品定位、品牌材料或参考风格生成草稿，预览调整后保存为项目设计体系。

### P-006 第一版采用 Open Design 设计体系规则

`confirmed`

Multica 第一版参照 Open Design 的设计体系基础契约，不再另行设计统一 UI 规范表单或新的 Token 分类。最小正式包为 `manifest.json`、`DESIGN.md` 和 `tokens.css`，并按真实来源选择性增加组件、预览、UI Kit、来源证据、资产和字体。

Token 分层、草稿与保存、一个主体系加多个弱参考等规则参照 Open Design。Multica 将这些规则翻新为云端项目资产，并接入现有 Project、Issue 和 Agent；不复制 Open Design 的本地 Project，也不运行或重写其 daemon/worker。

### P-009 设计体系引擎直接采用 Open Design

`superseded`

Multica 不再围绕 Open Design 的概念自行设计一套生成、导入、组件识别、Token 推导、UI Kit 和包审计流程。来源采集与快照、确定性资产提取、分层设计体系包、Agent 工作空间深化、Package Audit、Preview/UI Kit 和模板资源协议均直接采用 Open Design 的上游实现语义。

Multica 只实现必要的薄适配：现有 Project、Issue、用户选择 Agent、任务桥接、云端存储、鉴权权限和设计中心展示。Open Design 的本地 Project 和桌面端产品外壳不进入 Multica，daemon 也不成为第二套控制面；已有 Multica 草稿/保存语言和不引入审核权限等产品决策继续有效。

现有“一次 Prompt 生成固定三文件”的实现属于阶段性验证，不再作为目标架构继续扩展。后续任何实现计划必须先固定 Open Design 版本，并明确哪些模块直接复用、哪些仅做云端适配、哪些现有自研代码应淘汰。

第一接入基线固定为官方稳定 Release `open-design-v0.16.1`（commit `276b4d8e970bc143d7ad060181a89a834e3d9caf`）。Multica 不复制或重写 Open Design daemon，而是在本地 daemon 或云端隔离 worker 中运行该固定版本的 headless daemon/engine；它只作为一次任务的执行引擎，不成为第二套业务控制面。详细边界见 [open-design-engine-integration.md](./open-design-engine-integration.md)。

替代原因：2026-08-05 用户明确不接受专用 Open Design Worker 或 Runtime 进入正式运行链路，但要求 Multica 的设计引擎继续以 Open Design 的核心产品流程、能力语义和分层资源包为依据。替代决策见 P-010 / DC-039。

### P-010 以 Open Design 为核心参照翻新 Multica 原生设计引擎

`confirmed`

Multica 使用现有 Project、Issue、Agent、daemon、任务队列、对象存储和设计中心，原生实现设计体系和后续在线设计能力，不运行、分发或托管 Open Design Worker、Daemon 和 Runtime。

Open Design 不是普通视觉参考，而是行为基线：统一设计任务入口、多来源取证、确定性提取与 Agent 深化、稳定内核加可选扩展的分层资源包、可持续调整工作空间、可执行模板、固定输入快照、Package Audit、Preview/UI Kit 和坏草稿隔离均需根据固定源码或真实实验翻新。

Agent 负责语义理解、设计判断、布局和组件组织；Multica 负责输入快照、任务编排、隔离工作区、产物协议、安全收集、对象存储、Audit、真实预览、草稿与保存。第一阶段只替换项目设计体系的生成和调整内核，不同时开展首页、设计稿生成和社区模板。

完整方案见 [2026-08-05-multica-native-design-engine-design.md](../../superpowers/specs/2026-08-05-multica-native-design-engine-design.md) 和 DC-039。

### P-011 Phase A 页面设计采用版本化 Design Document

`confirmed`

设计中心首页发起的页面设计 task 产出轻量、版本化的 `multica.design-document/v1`。一份 Design Document 可以包含主页面、相关子页面、状态、弹窗和关键流程；使用语义 `brief.json` 表达需求关系，以完全离线的可运行 prototype 表达布局和交互，以 `coverage.json` 表达需求覆盖，不恢复大型 PageSpec DSL。

项目允许多份 Design Document。每份文档通过不可变 revisions 演进，并维护当前 `draft` 与 `saved` 指针；用户只看到当前草稿和已保存状态。首次生成和每次调整均创建独立 task，在文档持续隔离工作空间中执行，固定输入快照和 base revision。Package 必须通过 Audit 与员工本地守护进程的现有 `designpreview` 强制门禁后才能进入 draft；浏览器不可用时任务失败，不增加跳过或降级路径。只有用户明确保存后，saved 才移动，下游智能体、MCP 和交付链只读取 saved。

页面 task 在执行中自动完成有界只读仓库 grounding；项目和智能体必选，任务（Issue）可选。保存设计稿只记录可追溯关联，不自动改变任务状态。完整方案见 [Native Design Phase A：页面 Design Document 产品与技术方案](../../superpowers/specs/2026-08-12-native-design-phase-a-design-document-design.md) 和 DC-042 至 DC-046。

### P-007 旧决策：先实现 Open Design 契约的云端映射

`superseded`

现有 `design_system_profile` 演进为设计体系稳定身份，设计内容进入可审核、可发布、可固定引用的 `design_system_revision`，Project 通过 `project_design_system_binding` 使用一个已发布主 revision 和零到多个弱参考 revision。

发布 revision 不可变，Project 不自动追随新版本。owner project 首次发布时可通过“发布并设为项目主体系”完成显式绑定。旧 Figma UI 规范、`profile_json` 和 `is_default` 只迁移为待审核来源与草稿，不能自动发布或自动绑定。

UI 设计和设计还原共用统一 Design Context Resolver，并在任务中固定 revision 与内容摘要。完整方案见 [open-design-multica-mapping.md](./open-design-multica-mapping.md)。

替代原因：这把底层资产治理和设计还原接入误当成了当前产品目标。相关模型可以作为后续技术研究，但不能先于用户可见的设计体系创建与管理闭环实施。

### P-008 第一阶段先完成项目设计体系的创建与管理闭环

`confirmed`

第一阶段的产品目标是在 Multica 的项目设计模块中，实现一套参考 Open Design 的设计体系创建与管理能力，而不是先替换旧 Profile、建设完整版本治理或接入设计还原。

用户主动为项目创建设计体系，并可提供项目定位、品牌资料、参考风格或已有设计资产。Agent 按 Open Design 的规则生成设计体系，Multica 在线展示设计规则、Tokens、组件和可视化 UI Kit，用户可以预览、调整并保存为项目长期资产。

第一阶段以用户能否生成、看懂并获得一套有价值的项目设计体系作为成功标准。Figma UI 规范可以作为输入，也可以在未来由设计体系派生，但不是前置条件；空项目在用户没有主动发起时不得自动生成。

本阶段暂不接入设计还原，也不迁移旧 `design_system_profile`。设计体系生成、包校验和 Preview 按 P-010 的原生引擎实施，继续沿用现有项目控制面、草稿隔离和用户保存语义。

已确认的第一阶段用户流程：

```mermaid
flowchart LR
    A["设计中心选择项目"] --> B["进入设计体系"]
    B --> C["未建立设计体系"]
    C --> D["创建设计体系"]
    D --> E["选择执行 Agent"]
    E --> F["填写项目与品牌信息"]
    F --> G["添加可选参考资料"]
    G --> H["Agent 生成设计体系草稿"]
    H --> I["在线 UI Kit 预览与调整"]
    I --> J["保存为项目设计体系"]
```

第一阶段不新增隐式或强制的前置仓库扫描 Agent。用户可以主动选择 Agent 发起只读仓库分析，并将结果作为本次设计体系输入；没有主动分析时仍可根据其他材料创建。现有 `design_repo_analysis` 只是设计还原链路中的浅层规则扫描，不作为本流程的依赖。

创建输入采用“自然语言为主、结构化信息为辅”。系统自动带入已有项目名称和描述，用户主要描述产品定位、目标用户、核心场景和期望风格；Logo、品牌色、截图、Figma UI 规范和参考设计等材料均为可选输入，不要求用户填写冗长的设计规范表单。

创建页必须由用户选择执行 Agent。目标平台是唯一必选的结构化设计信息，第一版提供 Web、移动端和跨端；平台会直接影响组件形态、交互模式和信息密度，其他设计信息继续由自然语言和可选参考资料表达。Agent 选择属于任务负责人，不计入设计信息表单。

第一阶段不设置任何审核状态或设计审核权限。预览与调整只是普通编辑过程，不产生“待审核、通过、驳回”等状态；拥有项目编辑权限的用户可以调整并保存设计体系。

Agent 每次直接生成一套内部一致的设计体系草稿，不先生成多套风格方向让用户选择。用户通过在线 UI Kit 预览实际效果，需要变化时直接调整当前草稿或重新生成。

设计体系产物遵循 Open Design 包契约：最小事实源为 `manifest.json`、`DESIGN.md` 和 `tokens.css`，并根据真实来源增加 `components.html`、组件 manifest、Design Tokens JSON、预览、来源证据、资产和字体。`components.html` 是派生的组件与组合展示，不等同于项目真实组件库。Multica 不再规定一套平行的固定三文件生成流程，也不建设自研画布或固定组件渲染器。

上述文件是系统与 Agent 的内部产物契约，不是用户界面的信息架构。设计体系详情必须像 Open Design 的主视图一样展示具体设计体系内容：将设计意图组织为可阅读的动态章节，将 Tokens 展示为色彩、字体、间距等视觉内容，并将组件与状态展示为在线 UI Kit。第一阶段不展示文件树、原始文件名或代码编辑入口。

草稿调整采用“组件或区块定位 + 自然语言指令”。用户可以对整个体系提出要求，也可以先在 UI Kit 中定位某个组件或区块再描述修改；Multica Agent 工作空间必须同步维护受影响的事实源、派生产物和预览，不能只改变展示表面。第一阶段不提供 Token 表单、代码编辑器或拖拽画布，调整失败时保留修改前草稿。

第一阶段每个项目只维护一套当前设计体系。Agent 生成和调整的内容自动保留为草稿，用户点击“保存为项目设计体系”后进入已保存状态；后续继续调整同一套体系。第一阶段不提供多体系选择、主体系/参考体系绑定或历史版本，彻底重做时必须明确提示将替换当前体系。

> 本段“每个项目只维护一套”已于 2026-08-16 由 DC-052 替代：设计体系改为按仓库划分，`project_design_system.project_resource_id` 为 `NULL` 表示项目级体系、非 `NULL` 表示仓库专属体系。草稿/已保存语义、保存即成为当前有效体系、彻底重做需提示替换等规则继续有效。P-008 的其余内容未被替代。

项目的“设计体系” Tab 直接承载完整内容主视图，不再使用摘要列表和“打开设计体系”的二级入口。页面参考 Open Design 的内容优先结构，但不复制其多体系列表：项目 Tab 已经确定当前项目，左侧只展示由真实体系内容生成的动态章节目录，中间连续展示设计规则、视觉 Tokens、组件状态和在线 UI Kit。智能体调整面板按需打开，并在用户定位组件或区块后自动带入调整范围。

未建立体系时，同一个 Tab 直接展示单屏创建工作台，不增加空状态中转、步骤条或多页向导。主区使用自然语言收集产品定位、目标用户、核心场景和期望风格，并接收可选参考资料；紧凑设置区只要求用户选择目标平台和执行智能体。系统自动带入项目名称与描述，用户不需要预先选择 Token 分类、组件范围或固定章节。提交后原地展示真实执行状态，完成后直接切换为草稿主视图，失败时保留全部输入。

草稿和已保存状态共用一套连续内容主视图，不再把规则、Tokens、组件和预览拆成多个内部 Tab。左侧动态章节目录负责滚动定位，中间以主要内容宽度连续展示体系身份、设计原则、视觉 Tokens、组件状态、页面模式和在线 UI Kit。设计规则转换为可阅读内容，Tokens 转换为色板、字体样例、间距和布局等视觉表达，组件以真实状态和组合场景呈现。

智能体调整面板默认关闭。用户可以从顶部发起全局调整，也可以从章节发起局部调整；打开时自动带入当前范围。草稿只突出“保存为项目设计体系”这一主操作，“放弃草稿”和“重新生成”进入更多菜单。保存后继续使用同一主视图并显示已保存状态，再次调整时回到草稿状态。第一阶段不支持任意 DOM 选择、框选、代码编辑或复杂差异对比。

设计体系生成是否成功不能由智能体 task 的 `completed` 判定。task 结束后必须通过 Open Design Package Audit，确认最小事实包完整一致，并验证当前包声明的 Preview/UI Kit 与资源可以正常渲染，随后才能产生可用草稿。生成页面只展示可以由真实执行事件证明的状态、智能体、运行时长和最后活动时间，不使用虚构百分比；长时间没有活动时必须明确提示。

系统内部隔离当前草稿与当前已保存设计体系。首次保存前，下游智能体不能把草稿作为项目强约束；已有体系正在调整时，下游继续读取最近一次已保存内容。首次草稿使用“保存为项目设计体系”，后续调整使用“保存调整”，保存操作必须原子替换当前有效内容。执行、校验、渲染或保存失败时保留输入、参考资料、草稿和最近一次有效体系，不允许把失败或不完整内容自动推进为已保存状态。

仓库分析成功后，创建工作台将本次使用的参考资料收起为只读摘要，后续生成自动沿用同一组输入。用户重新开放参考资料选择时，当前分析不能再直接用于生成，必须用新的参考资料重新完成仓库分析，避免分析来源与生成输入发生静默偏差。

## 5. 当前讨论范围

以下内容来自 2026-07-28 至 2026-08-16 的讨论：

1. **项目设计体系**，`confirmed`：项目拥有可生成、预览、调整和保存的云端设计体系；在线 UI Kit 是它的派生视图，Figma UI 规范是可选输入。
2. **设计体系规则基线**，`confirmed`：第一版参照采用 Open Design 的包结构、Token 分层和可选扩展，不再扩展一套平行模型；不照搬其修订审核工作流。
3. **第一阶段产品闭环**，`confirmed`：用户在项目设计模块主动创建或生成设计体系，在线查看设计规则、Tokens、组件与 UI Kit，预览调整后保存为项目长期资产。
4. **设计任务发起器**，`confirmed`：首页第一版是跨项目页面设计 task 发起器，用户输入自然语言页面需求并选择项目和智能体；任务创建成功后进入目标项目“设计草稿”，首页与项目 Tab 读取同一服务端 task/document 状态。项目和智能体必选；任务（Issue）默认同步创建、可在发起器中关闭（2026-08-24 由用户确认，取代原「不自动创建」）。创建出的 Issue 只是可追溯的伴生卡片，负责人是所选智能体；设计任务**不推进也不改变**它的状态（DC-045 不变）。已在发起器中指定 Issue 时不再新建，避免把追溯链拆成两条。
5. **页面 Design Document**，`confirmed`：正式产物为 `multica.design-document/v1`，由语义 brief、离线可运行 prototype、assets 和 coverage 组成。一份文档可包含完整页面流程；项目允许多份文档，文档以不可变 revisions 演进并维护 draft/saved 指针。每次调整创建独立 task，在持续工作空间中基于固定 base revision 输出完整 package。
6. **页面仓库 Grounding**，`confirmed`：页面设计 task 内由所选智能体自动执行有界只读仓库取证，固定 commit、相对来源、摘要、结构化事实和不确定性；不增加首页前置扫描步骤。该规则不改变设计体系创建中由用户主动发起仓库分析并冻结参考资料的 DC-018/DC-036。
7. **页面 Audit/Preview 门禁**，`confirmed`：Design Document 直接复用员工本地守护进程现有 `designpreview` 强制门禁，并叠加页面/状态/流程和关键交互检查。浏览器不可用时 task 失败，不跳过 Preview、不形成或覆盖 draft；能够渲染不等于视觉质量通过。
8. **设计任务模板**，`confirmed`：模板是页面设计 task 配方，不是设计体系；按官方模板、工作区成员发布、跨工作区社区模板分期推进，均不进入新 Phase A。
9. **项目工作区 Tab**，`confirmed`：设计中心固定保留不可关闭的“首页” Tab；具体项目以可打开、可切换、可关闭的 Tab 呈现。关闭最后一个项目后回到首页，首页内容留待下一阶段设计。
10. **项目内容 Tab**，`confirmed`：项目内的“设计稿 / 设计草稿 / 模版 / 设计体系”使用紧凑的二级内容 Tab，数量以小徽标呈现，不再使用说明型大卡片；内容面板不重复显示项目名和分类标题。
11. **设计体系内容主视图**，`confirmed`：设计体系 Tab 直接展示动态章节、视觉 Tokens、组件状态和在线 UI Kit，不再经过摘要列表或二级详情入口；左侧使用当前体系的章节目录，智能体调整面板按需打开。
12. **设计体系创建工作台**，`confirmed`：未建立体系时直接在设计体系 Tab 内展示自然语言为主的单屏创建工作台；目标平台和执行智能体必选，其他参考资料可选，提交后原地进入生成状态。
13. **设计体系连续内容画布**，`confirmed`：草稿和已保存状态共用动态章节目录与连续内容主视图；智能体调整面板默认关闭，保存是草稿的唯一主操作。
14. **设计体系成功与保存边界**，`confirmed`：task 完成后必须通过产物和 UI Kit 渲染验证才能形成草稿；草稿与已保存体系隔离，下游只读取已保存内容，保存采用原子替换。
15. **统一设计上下文解析**，`confirmed`：下游固定按“云端已保存项目设计体系 > 本地 `DESIGN.md` > 仓库现实”解析设计约束；Server 只读取通过校验的 `saved` 内容，不读取草稿或旧 Profile，无云端内容时由本地 Agent 继续完成本地回退。
16. **用户主动发起只读仓库分析**，`confirmed`：设计体系创建不增加隐式强制扫描；用户选择 Agent 和目标平台后可主动分析该 runtime 可访问的项目资源。分析期间内容区锁定且只保留停止操作，成功后回填结构化仓库事实与冲突，失败时保留原表单和上一次有效分析，Agent 不得修改仓库或生成本地 `DESIGN.md`。
17. **仓库分析后的参考资料快照**，`confirmed`：分析成功后只展示已使用资料摘要并自动沿用到生成；用户重新选择参考资料后必须重新分析，旧分析不能继续驱动生成。
18. **Open Design 核心参照边界**，`confirmed`：产品流程、能力语义、分层包、Agent 深化、Package Audit、Preview/UI Kit 和模板机制以 Open Design 为行为基线，但由 Multica 使用现有 Agent 与基础设施原生实现。
19. **不采用专用 Worker/Runtime**，`confirmed`：不运行、分发或托管 Open Design Worker、Daemon 和 Runtime；固定版本实验只保留为源码证据、行为对照和质量门禁依据。

2026-08-16 新增确认（详见 DC-047 至 DC-055）：

20. **Open Design 证据基线**，`confirmed`：基线从 `open-design-v0.16.1` 改为 `open-design-v0.19.2`；`OD-021` 至 `OD-044` 降级为经验来源，不再是行为基线。
21. **迁移范围收窄为三个 tab**，`confirmed`：只迁 Open Design 的首页、社区和设计体系；Studio 的替代物是 Multica 项目内的 Design Document 工作区。Brands 已在上游并入设计体系，Multica 不建独立品牌套件实体。
22. **首页场景 chip**，`confirmed`：复刻 Open Design 首页的视觉与信息架构，只复刻不搬代码；第一版放五个有真实产物支撑的 chip，发起 API 预留 `recipe` 字段。
23. **tweaks 与 critique 进入产品**，`confirmed`：落在 Design Document 工作区。tweaks 只进 `prototype/`，不进设计体系包；critique 分数不构成 draft 形成条件，`fallbackPolicy` 只取 `fail`。
24. **设计体系按仓库划分**，`confirmed`：替代 P-008 的“每个项目一套”，`project_resource_id` 为 `NULL` 表示项目级；不引入工作区默认体系。
25. **仓库可选**，`confirmed`：选中仓库才做有界只读取证，未选时跳过并在文档中显式标注。
26. **先窄后宽**，`confirmed`：工作区级体系目录与社区模板排在 Phase A 之后，A2 只留灰态位置。
27. **生产端契约先对齐**，`confirmed`：原生 V2 的 prompt 与包契约此前从未接通，必须先对齐并补跨边界测试，随后重算 Phase A 基线。

2026-08-19 新增确认（详见 DC-057）：

28. **Design Document 工作区页面**，`confirmed`：生成后的预览、版本、调整、保存与放弃在 `/{slug}/designs/documents/{id}` 完成，对应 Open Design Studio 的生成后页面；修订通过独立读取契约与能力令牌预览路由提供，服务端与守护进程的包绑定已对齐并有跨边界测试。
29. **三 tab 补齐与 tweaks / critique 落地**，`confirmed`（详见 DC-058）：官方设计体系详情以 Open Design 自带 showcase 为封面并逐章节展示 DESIGN.md，官方体系可作为项目体系创建的参考风格；社区卡片可打开实时示例与提示词的详情弹层；tweaks 以 prompt 约定加工作区预设指令落地，critique 以任务内五视角循环加 `critique.json` 报告与工作区“设计评审”面板落地，两者都不影响 draft 的形成条件。
30. **设计文档任务上下文可领取并声明取证模式**，`confirmed`（详见 DC-059）：服务端上下文写 `execution_ready` 与 `input.repository_grounding`，claim 只交付文档绑定的那一个仓库，prompt 按取证模式说明 checkout 与 `work/repository-grounding.json` 的写回义务；在此之前每个设计文档任务都在 claim 被拒，真实生成从未跑通。

2026-09-03 新增确认（详见 DC-063）：

31. **设计稿生成首次真实跑通**，`confirmed`：内置目录体系摘要改为 `sha256:<hex>`、模板残留审计不再扫描 `coverage.json` 自述、新增任务内预检命令 `multica design audit`（与守护进程门禁同一份收集 / 审计 / Chromium 校验）并注入 `MULTICA_CLI`；真实 codex 任务 `01a0653d…` 在 15 分钟内产出通过门禁的 v1 草稿。视觉与业务质量验收仍属 A6。与 Open Design 的剩余缺口见 [open-design-gap-2026-09-03.md](./open-design-gap-2026-09-03.md)。

当前尚未确认的细节只保留后续落地与独立切片问题：

- A1 实施计划中如何在不破坏历史 `semantic_design_draft` 的前提下落地 Design Document、revision 和指针持久化；
- 工作区共享设计体系 revision 的发布、剥离和权限模型；
- 官方模板与后续社区模板的资源、revision 和应用快照模型；
- 何时以及如何支持原生 Figma UI Kit 写回。

## 6. 历史资料的使用方式

下列文档记录了真实实现和阶段性决策，但它们不再自动代表当前产品方向：

- [../design-restore-memory.md](../design-restore-memory.md)：Figma 上传、查看、MCP 和设计还原的长期历史；
- [../project-design-contract-roadmap.md](../project-design-contract-roadmap.md)：早期云端设计契约路线；
- [../../superpowers/specs/2026-07-08-design-system-profile-mvp-design.md](../../superpowers/specs/2026-07-08-design-system-profile-mvp-design.md)：第一版 Design System Profile；
- [../../superpowers/specs/2026-07-21-semantic-ui-agent-design-generation-design.md](../../superpowers/specs/2026-07-21-semantic-ui-agent-design-generation-design.md)：基于 `PageSpec` 和编译器的 B 端结构化生成路线。

使用历史资料时必须先回答：

1. 它描述的是产品目标、实验方案还是已经存在的代码？
2. 它的日期是否早于当前决策？
3. 它是否已经被后续用户反馈暂停、否决或替代？
4. 它能提供什么经验，而不是要求我们继续维护什么包袱？

## 7. 更新协议

每轮讨论结束后，只做以下增量更新：

- 用户明确确认或否决的内容，写入决策台账；
- 新的外部研究结果，写入证据台账并标明版本、提交和日期；
- 新提案先记录为 `proposal`，不得提前改成 `confirmed`；
- 被替代的决策保留原文并改为 `superseded`，不得删除历史；
- 实现结果必须附真实验证证据，不能只记录“任务已完成”。

## 8. 当前下一议题

2026-08-12 用户取消独立、一次性的 Open Design V1 破坏性移除 Phase B。后续只沿 Native V2 产品功能切片推进：切片内部已经无调用、被完整替代的旧分支、fallback、配置和测试必须删除；跨切片、跨数据生命周期或仍有外部消费者的残留进入唯一退役账本，不得扩大当前切片。数据库行、对象、表和约束的不可逆退役最后单独提出、审批和验证。详细决策见 DC-040 和 [Native Design 产品切片演进与渐进清理方案](../../superpowers/specs/2026-08-12-native-design-slice-driven-evolution-design.md)。

新 Phase A 的产品方案已经确认。首页使用自然语言发起项目页面设计 task，项目和智能体必选、任务（Issue）可选；task 内自动完成有界只读仓库 grounding，并产出版本化 `multica.design-document/v1`。每个项目允许多份 Design Document，每份文档以不可变 revisions、draft/saved 指针和持续工作空间支持 Preview、调整、保存与放弃。Package 必须通过 Audit 和员工本地守护进程现有 `designpreview` 强制门禁，浏览器不可用时不降级。完整方案见 [Native Design Phase A：页面 Design Document 产品与技术方案](../../superpowers/specs/2026-08-12-native-design-phase-a-design-document-design.md) 和 DC-042 至 DC-046。

A1 至 A5 已按确认规格完成自动化实现：A1 提供 Design Document 协议、持久化和对象存储基础，A2 提供首页/项目任务入口，A3 提供 task-owned workspace、只读仓库 Grounding 与固定输入，A4 提供静态 Audit、本地 Chrome 强制 Preview、immutable archive、原子 first revision/draft 和项目内 sandbox Preview，A5 提供固定 base revision 的语义范围调整、单文档单写者、完整 package 重验、新 revision/draft 以及保存与放弃。证据见 [Phase A A5 阶段报告](./native-design-phase-a5-validation.md)。按正式规格权重严格加权，当前 Phase A 工程进度为 **90%**；A6 真实 Agent 产物、视觉和业务质量人工验收尚未完成。2026-09-03 起这条链路已有第一份真实端到端证据（DC-063：真实 codex 任务通过守护进程门禁并形成 v1 草稿），A6 从"从未跑通"进入"可以开始验收"。工作区共享设计体系、官方模板、工作区成员模板发布和跨工作区社区模板继续按 DC-041 作为 Slice B 至 E 独立推进，不计入 Phase A。`feature/fengchen-fixed-v2` 只保留为取消路线的隔离 checkpoint，不属于当前产品进度或后续实现基线。

2026-08-05 已确认的原生方向继续有效：Open Design Worker/Runtime 的直接接入不进入产品主线，Phase 0 和 Phase 2 已完成结果保留为真实执行、失败隔离、Audit、Preview 和草稿门禁证据。

Phase 0 Task 0.1 已证明固定提交上的 Open Design headless daemon 可以使用锁定依赖构建、在隔离数据目录启动并优雅停止，证据见 `OD-021`。Task 0.2 已使用现有 Multica Agent 和 CRM 来源材料完成一次真实 `orchestrator-scratch` 创建任务，并取得完整设计体系包、独立 Package Audit、Chrome 渲染以及源仓库零修改证据，详见 `OD-022`。Task 0.3 已分五个有界子阶段验证一次真实 scratch 调整、真实 Run 取消与回收、真实 Agent 失败隔离、固定上游 Audit 对坏候选的拒绝，以及 Audit 通过但页面不可见的 Preview 失败与回收，详见 `OD-023` 至 `OD-027`。`OD-028` 已将固定制品和 Agent preflight、Run、SSE 与 result package 接入 Multica 持久 Supervisor 骨架；`OD-029` 进一步接入上游 export manifest 与完整 archive 收集，生成并持久化脱敏 result package、逐文件 artifact index 和稳定 content digest；`OD-030` 已把经过 Server 二次完整性校验的 ZIP 上传统一对象存储，并在 result callback 前持久化确定性 `archive_object_key`；`OD-031` 已把 worker 启动前和执行中的 orphan task 与 `open_design_run` 原子收敛到持久失败终态，并在 scratch 交给 worker 前写入可回收 GC 身份；`OD-032` 已读取固定上游结构化 `audit.ok`，把 digest 绑定的 Audit 回执持久化，并让拒绝报告与 `audit_failed` 原子落盘；`OD-033` 已接入独立 Chromium Preview verifier，把固定引擎和 digest 绑定的渲染回执持久化；`OD-034` 已让 Server 重新读取对象存储 archive，并只在同一 Run 的 result、archive、Audit 和 Preview 完整一致时原子生成隔离 draft、完成 task 和清理 active task，saved 不受影响；`OD-035` 已让 SSE 从最后持久事件 ID 有界续接，并分别识别 `missing_cursor`、worker Run 丢失和持续流不可用，不会新建 Session 冒充恢复；`OD-036` 已为终态 Run 提供 workspace 隔离、原始 archive 重验和字节级确定性的统一证据 ZIP 下载；`OD-037` 已在正式 Supervisor 上真实跑通所选 Agent、Audit、Preview、隔离 draft 和确定性证据下载的正向闭环，并确认旧设计中心三文件兼容视图无法展示模块化 archive UI Kit；`OD-038` 已真实验证运行中的 worker Run 可被用户取消、不会产生或覆盖 draft，并可导出确定性取消证据包；`OD-039` 已真实验证运行中的专属 Agent 进程失败会独立收敛为 `agent_failed`、不产生或覆盖 draft，并可导出确定性失败证据包；`OD-040` 已真实验证 Agent 正常结束后，上游 Audit 可将唯一缺少 `DESIGN.md` 的坏候选拒绝为 `audit_failed`，短路 Preview、隔离 draft 并导出确定性证据包；`OD-041` 已真实验证 Audit 通过后，独立 Chrome 可将 DOM 存在但计算不可见的 UI Kit 拒绝为 `preview_failed`，同样不产生或覆盖 draft；`OD-042` 已真实验证 worker 硬重启后旧 Run 返回 404，Multica 可收敛为 `open_design_worker_run_missing` 并跨 worker 生命周期导出确定性证据；`OD-043` 最终使用 29 个 CRM 仓库来源文件和 28 条结构化事实跑通正式正向任务，证明仓库上下文、所选 Agent、原生 archive、零告警 Audit、7 个离线 Chrome 目标、隔离 draft、源仓库零修改和确定性 Evidence ZIP 在同一次 Run 中完整闭合。

Phase 0 的验收矩阵和 go/no-go 结论已汇总到 [open-design-engine-integration.md](./open-design-engine-integration.md#101-当前验收矩阵)。`OD-043` 补齐最后一条带仓库来源输入的正式正向证据后，固定引擎、真实 Agent、正式 Supervisor、完整失败矩阵、worker restart、源仓库只读、对象存储 archive、Package Audit、Preview、隔离 draft 和统一证据归档已经全部贯通，Phase 0 结论转为 **Go**。

Phase 0 Go 只表示固定 Open Design 引擎和 Multica production gate 可以进入下一阶段，不允许自动把 draft 保存为项目有效体系。Phase 2 的第一个有界切片已完成 archive-backed Preview/UI Kit 读取：设计中心可直接展示通过门禁的原生 archive，6 张 Preview 和 1 个 UI Kit 已在用户本机 Chrome 完成资源、视觉和切换验收，详见 `OD-044`；这不等同于调整或保存闭环已经完成。

不再继续固定 Worker 的部署托管、Runtime 分发和上游 Run 协议接入。下一阶段只允许先盘点现有代码，将可复用的输入快照、对象存储、Audit、Preview、取消和失败隔离迁移到 Multica 原生 Agent 链路；设计还原、旧 PageSpec 编译器和下游 Design Context Resolver 继续作为独立边界处理。
