# Multica 设计中心决策台账

> 最后更新：2026-09-03
> 规则：保留历史，通过状态变化表达推翻或替代，不删除旧决策

## 状态说明

- `confirmed`：用户已经明确确认，约束后续方案；
- `proposal`：讨论中的建议，尚不能进入实现；
- `open`：必须继续研究或选择；
- `paused`：当前停止投入，但尚未永久否决；
- `rejected`：明确不采用；
- `superseded`：被后续决策替代。

## 已确认决策

### DC-001 Multica 是人和 Agent 协作的软件交付平台

- 状态：`confirmed`
- 日期：2026-07-28
- 决策：以 Project 为切入点，以 Issue 作为需求连接与派发载体，结合云端和本地 Agent 完成需求、设计、开发与落地。
- 影响：任何设计能力都必须回到 Project 和 Issue 主线，不能形成无法交接的孤立工具。

### DC-002 人和 Agent 必须可以双向接管

- 状态：`confirmed`
- 日期：2026-07-28
- 决策：人可随时进入 Agent 工作，Agent 也可继续接管人的工作；双方共享上下文、状态和产物。
- 影响：任务状态之外，还必须保留过程上下文、版本化产物和接管依据。

### DC-003 设计能力只是完整流水线中的一环

- 状态：`confirmed`
- 日期：2026-07-28
- 决策：设计稿上传、MCP、设计还原、UI 设计和 Open Design 能力都服务于完整需求交付，而不是 Multica 的独立中心。
- 影响：不能以局部设计工具的最佳体验破坏全局 Issue 协作模型。

### DC-004 旧决策：选择性接入 Open Design 能力

- 状态：`superseded`
- 日期：2026-07-28
- 决策：研究 Open Design 的主页与模板、设计体系和本地 Project，但只接入适合 Multica 的部分能力。
- 证据：[open-design-evidence.md](./open-design-evidence.md)
- 影响：不复制其 daemon，不新增与 Multica Project 竞争的第二套 Project 模型。
- 替代原因：该表述曾在 2026-07-31 被“直接采用 Open Design 引擎”替代；2026-08-05 又收敛为以 Open Design 为核心参照、由 Multica 原生翻新，见 DC-039。

### DC-005 产品结论必须有可复核依据

- 状态：`confirmed`
- 日期：2026-07-28
- 决策：讨论事实需要绑定源码、版本、运行观察或既有产品决策。上下文压缩或恢复后必须重新读取本模块。
- 影响：提案、证据和决策必须分开记录；不能依赖聊天摘要直接进入实现。

### DC-006 项目拥有云端设计体系

- 状态：`confirmed`
- 日期：2026-07-28
- 决策：项目可以拥有由 Multica 云端管理的设计体系，并在设计模块中与 UI 规范相关能力统一管理。
- 影响：设计体系是项目级长期资产，不是某次设计任务的临时 Prompt。
- 待决定：输入来源、来源冲突规则、保存方式和版本生命周期。

### DC-012 设计体系是源，UI 规范是派生视图和可选输入

- 状态：`confirmed`
- 日期：2026-07-28
- 决策：项目设计体系是设计事实源。Figma UI 规范不是硬性前置条件，它既可以作为已有项目的可选导入证据，也可以在未来成为设计体系的派生产物。
- 第一阶段产物：Multica 根据设计体系生成可视化、可调整的在线 UI Kit，不要求生成原生 Figma 文件。
- 后续能力：原生 Figma Components、Variants 和 Auto Layout 的生成或同步作为独立阶段研究。
- 依据：Open Design 的分层设计体系包、preview/showcase 和草稿生成能力，见 `OD-004`、`OD-005`。
- 影响：早期 `Figma UI specification -> design_system_profile` 主路线被替代；已有上传能力可以保留为导入渠道，但不能继续充当唯一真相源。

### DC-013 空项目不得自动猜测设计体系

- 状态：`confirmed`
- 日期：2026-07-28
- 决策：没有来源材料、也没有用户主动创建意图时，项目保持“未建立设计体系”，Agent 不得自动生成看似完整的体系。
- 从零创建：用户明确发起并提供产品定位、品牌材料或参考风格后，Agent 可以创建设计体系草稿；用户可通过在线 UI Kit 预览调整并保存。
- 影响：项目创建与设计体系生成解耦；“无体系”是合法状态，不应触发隐藏的自动生成任务。

### DC-015 第一版采用 Open Design 的设计体系规则基线

- 状态：`confirmed`
- 日期：2026-07-28
- 决策：Multica 第一版不再另行发明设计体系分类、固定 UI 规范表单或新的 Token 分层，参照采用 Open Design 已验证的基础规则作为设计体系契约。
- 最小正式包：`manifest.json`、`DESIGN.md`、`tokens.css`。
- 可选丰富内容：`USAGE.md`、组件 fixture 与 manifest、Design Tokens JSON、Tailwind 映射、预览、来源证据、资产和字体；项目没有真实证据时不强制伪造这些内容。
- Token 规则：沿用 `A1-identity`、`A1-structure`、`A2`、`B-slot` 和 `C-extension` 的职责与 fallback/alias 规则。
- 内容规则：`DESIGN.md` 保持自由章节结构，不要求用户填写统一 UI 规范表单；体系按来源成熟度逐步丰富。
- 使用规则：Project 可以不绑定设计体系；只允许已发布体系作为主强约束；额外设计体系只能作为弱参考，不覆盖主体系 Tokens。
- 生命周期：第一阶段只采用未建立、生成中、草稿和已保存的必要状态，不采用 Open Design 的 pending revision 接受/拒绝工作流；具体见 DC-021。
- 边界：采用的是设计体系契约和产品规则，不复制 Open Design 的本地 Project、文件系统注册表，也不运行或重写其 daemon/worker。Multica 使用现有云端 Project、Issue、Agent 和 daemon 原生实现。
- 依据：`OD-009`、`OD-011`、`OD-012`、`OD-013`、`OD-014`。
- 影响：后续不再讨论是否重新设计一套通用设计体系模型；生成与资产流程按 DC-039 以 Open Design 为行为基线原生翻新。

### DC-016 旧决策：先实现 Open Design 契约的最小云端映射

- 状态：`superseded`
- 日期：2026-07-28
- 决策：将现有 `design_system_profile` 演进为稳定的设计体系身份，新增 Open Design 包 revision 和 Project 固定版本 binding；不永久维护两套设计体系主模型。
- 核心规则：最小事实包原子保存；丰富文件进入对象存储；发布 revision 不可变；Project 只能绑定已发布固定 revision；UI 设计与设计还原共用统一 Design Context Resolver。
- 发布与绑定：发布新 revision 后 Project 必须由用户明确确认升级，不自动追随最新版；owner project 首次发布的主操作为“发布并设为项目主体系”。
- 迁移原则：旧 Figma UI 规范及 `profile_json` 只作为导入证据和待审核草稿；旧 `is_default` 数据不自动发布，也不自动转换为 Project primary。
- 暂不新增：设计体系专用 Design Run、社区模板、完整在线编辑器和旧语义编译器重写。
- 方案全文：[open-design-multica-mapping.md](./open-design-multica-mapping.md)
- 替代原因：该方案把底层资产版本治理和设计还原消费当成了第一阶段目标，没有先解决用户如何创建、查看、审核和保存项目设计体系。
- 后续处理：保留为未来技术研究，不得直接驱动当前实现。

### DC-017 第一阶段先完成项目设计体系创建与管理闭环

- 状态：`confirmed`
- 日期：2026-07-28，更新于 2026-07-31
- 决策：在项目设计模块中，让用户主动创建或生成设计体系；用户可以提供项目定位、品牌资料、参考风格或已有设计资产，Agent 按 Open Design 规则生成设计规则、Tokens、组件和在线 UI Kit，用户预览、调整后保存为项目长期资产。
- 成功标准：用户实际获得一套可理解、可视化且对项目有价值的设计体系，不能用数据库模型完整、Agent Task completed 或文件包生成代替产品成功。
- 输入边界：Figma UI 规范是可选输入，也可以在未来由设计体系派生；没有用户主动发起时，空项目不得自动生成。
- 阶段边界：暂不接设计还原，也不迁移旧 `design_system_profile`。生成、包校验和 Preview 使用 DC-039 的 Multica 原生引擎，保持现有草稿和保存语义。
- 影响：必须先完成并确认入口、输入、生成产物、在线预览、调整和保存流程的产品设计，再编写实施计划。

### DC-018 第一阶段不新增隐式前置仓库扫描 Agent

- 状态：`confirmed`
- 日期：2026-07-28，更新于 2026-07-30
- 决策：项目设计体系创建流程不依赖隐式或强制执行的仓库扫描；用户可以在创建工作台中主动选择 Agent 和目标平台，发起一次只读项目仓库分析，并将结构化仓库背景作为设计体系输入。
- 现状依据：已有 `design_repo_analysis` 是设计还原链路的本地规则扫描，只识别有限的框架、目录、路由、样式和候选文件，不是通用项目工程理解能力。
- 执行边界：分析 Agent 只能读取其 runtime 实际可访问的项目资源；本地目录必须与 runtime 的 `daemon_id` 匹配。分析不得修改仓库，不生成或更新本地 `DESIGN.md`，也不得返回绝对路径、源码或密钥。
- 交互边界：分析期间设计体系内容区完全锁定，只展示真实任务状态、运行时长和最后活动，并只保留“停止分析”；不展示虚构百分比。成功后回填摘要、事实、相对来源、建议目标和冲突，已有用户目标不自动覆盖。
- 失败边界：失败、停止或超时后保留原表单和上一次有效分析；没有可访问资源时在派发前明确拒绝。第一阶段不新增独立仓库分析资产、审核状态或隐式前置任务。

### DC-019 设计体系创建以自然语言输入为主

- 状态：`confirmed`
- 日期：2026-07-28
- 决策：创建页以一段自然语言项目与品牌描述作为主要输入，已有项目名称和描述由系统自动带入；结构化选项只补充机器必须明确的信息。
- 可选资料：Logo、品牌色、截图、Figma UI 规范和参考设计等，不构成创建前置条件。
- 体验边界：不使用冗长的设计规范表单，也不要求用户逐项定义 Token 或组件。

### DC-020 目标平台是创建页唯一必选结构化字段

- 状态：`confirmed`
- 日期：2026-07-28
- 决策：目标平台是唯一必选的结构化设计信息，第一版必须选择 Web、移动端或跨端；其余设计输入仍由自然语言和可选资料表达。执行 Agent 仍必须由用户选择，但它属于任务负责人，不属于设计信息字段。
- 原因：目标平台会实质影响组件形态、交互模式和信息密度，不能完全依赖 Agent 猜测。
- 影响：创建页不得继续扩展产品类型、行业、风格、组件范围等必填表单项。

### DC-021 第一阶段不设置设计审核状态和权限

- 状态：`confirmed`
- 日期：2026-07-28
- 决策：Multica 不设置 `pending_review`，也不提供设计审核人、通过或驳回等工作流。Open Design 的 pending revision 接受/拒绝机制不进入第一阶段。
- 产品语言：统一使用生成、草稿、预览、调整和保存，避免使用“审核、待审核、审核通过”等字眼。
- 权限边界：沿用项目现有编辑权限，不新建设计审核角色或审批权限。
- 影响：在线 UI Kit 的反馈和调整属于普通编辑过程，不产生额外状态；第一阶段的数据模型不得为审批流程预埋复杂度。

### DC-022 每次生成一套统一的设计体系草稿

- 状态：`confirmed`
- 日期：2026-07-28
- 决策：Agent 根据当前输入直接生成一套内部一致的设计体系草稿，不增加多套风格方向的前置选择步骤。
- 调整方式：用户在在线 UI Kit 中查看实际效果，对当前草稿直接调整或重新生成。
- 影响：第一阶段不实现多方案并行生成、方案对比或方案合并。

### DC-023 旧决策：在线 UI Kit 固定使用 Agent 生成的静态 HTML/CSS

- 状态：`superseded`
- 日期：2026-07-28
- 决策：Agent 草稿固定生成 `DESIGN.md`、`tokens.css` 和 `components.html`；`components.html` 使用 Tokens 展示真实组件、状态和组合效果。
- 展示边界：Multica 在隔离环境中渲染静态 HTML/CSS，不执行任意脚本。
- 依据：Open Design 的分层包、`components.html` 和 preview/showcase 机制，见 `OD-004`、`OD-005`、`OD-009`。
- 排除项：第一阶段不使用只能展示固定组件的 Multica Token 渲染器，也不建设 Figma 类画布和结构化编辑模型。
- 替代原因：固定三文件的一次性 Agent 生成只是 Multica 阶段性实验，不能把静态样例等同于真实组件库。后续 UI Kit、Preview、组件 manifest 和丰富产物参照 Open Design 分层包与工作空间语义，由 Multica 原生实现，见 DC-039。

### DC-024 使用组件定位和自然语言调整草稿

- 状态：`confirmed`
- 日期：2026-07-28，更新于 2026-07-31
- 决策：用户可以对整个体系输入自然语言调整要求，也可以先在在线 UI Kit 中定位某个组件或区块，再输入局部调整要求。
- Agent 契约：一次调整必须通过 Multica Agent 工作空间同步维护受影响的事实源、派生产物和预览，不能只改变 UI Kit 表面；完成后执行对应 Package Audit 并刷新 Preview/UI Kit。
- 失败边界：调整失败时保留调整前的草稿，不产生半更新文件。
- 排除项：第一阶段不提供 Token 表单、代码编辑器、拖拽画布或 pending revision 工作流。

### DC-025 旧决策：第一阶段一个项目只维护一套设计体系

- 状态：`superseded`
- 日期：2026-07-28
- 替代日期：2026-08-16
- 替代原因：一个项目可能同时包含 C 端 H5、App 和后台管理系统等多个仓库，它们的设计语言不能被迫共用一套体系。
- 替代决策：DC-052。
- 保留说明：以下原文保留为历史。草稿/已保存语义、保存即成为当前有效体系、彻底重做需提示替换等规则继续有效，只有“每个项目一套”这一条被替代。
- 决策：每个项目只维护一套当前设计体系，状态仅为未建立、草稿和已保存。
- 保存语义：Agent 生成和调整的内容自动保留为草稿；用户点击“保存为项目设计体系”后成为该项目当前体系，后续继续调整同一套体系。
- 替换边界：彻底重做时必须明确提示用户将替换当前体系。
- 排除项：第一阶段不提供多体系选择、主体系/参考体系绑定或历史版本。

### DC-026 设计体系详情以内容为中心而不是文件为中心

- 状态：`confirmed`
- 日期：2026-07-28
- 决策：`DESIGN.md`、`tokens.css` 和 `components.html` 只作为系统与 Agent 的内部产物契约，普通用户不看到文件树、原始文件名或代码编辑入口。
- 展示方式：参考 Open Design 主视图，将设计意图解析为动态内容章节，将 Tokens 转换为色彩、字体、间距等视觉内容，并以在线 UI Kit 展示组件、状态和组合效果。
- 结构边界：设计内容章节由真实体系内容决定，不强制所有项目填写一套固定目录。
- 排除项：第一阶段不复制 Open Design 的 Files 工作区。

### DC-027 用户必须选择生成设计体系的 Agent

- 状态：`confirmed`
- 日期：2026-07-28
- 决策：创建设计体系时必须由用户明确选择执行 Agent，系统不能隐藏选择器，也不能根据固定名称自动选择 `Local UI Restore Agent`。
- 产品含义：Agent 是本次设计工作的明确负责人，选择 Agent 不属于冗长的设计信息表单。
- 失败边界：已选择 Agent 不可执行任务时必须在生成前明确提示并保留全部输入，不能静默换用其他 Agent。

### DC-028 设计中心固定首页并允许关闭所有项目 Tab

- 状态：`confirmed`
- 日期：2026-07-29
- 决策：设计中心项目工作区固定保留不可关闭的“首页” Tab；项目通过 `+` 打开为独立 Tab，所有项目 Tab 均可关闭，不再根据当前项目 Tab 数量限制关闭能力。
- 回退规则：关闭当前项目后切换到相邻项目；没有其他项目时回到首页。
- 阶段边界：首页只作为下一阶段设计的固定入口，本阶段不提前定义其内容结构。

### DC-029 项目资产分类使用紧凑内容 Tab

- 状态：`confirmed`
- 日期：2026-07-29
- 决策：每个项目下固定使用“设计稿 / 模版 / 设计体系”三项二级内容 Tab，不再以三张说明型大卡片充当导航。
- 数量规则：各类资产数量紧邻标签，以小徽标展示；详细说明和操作继续留在对应内容面板内。
- 层级规则：项目 Tab 负责项目上下文，内容 Tab 负责当前项目内的资产分类，两层导航使用不同视觉形态。

### DC-030 设计草稿独立展示且不引入审核语义

- 状态：`confirmed`
- 日期：2026-07-29
- 决策：项目内容导航调整为“设计稿 / 设计草稿 / 模版 / 设计体系”四项；设计草稿不再混排在设计稿内容中。
- 产品语言：草稿阶段统一使用打开、修改、保存和放弃，不出现待审核、审核、批准或驳回等审批语言。
- 内容层级：内容 Tab 已表达当前分类，各面板不再重复显示项目名、分类标题和说明；仅保留新增分组等必要操作，以及真实文件夹和分组名称。
- 影响：本决策补充 DC-029，并将其中三项二级内容 Tab 更新为四项。

### DC-031 设计体系 Tab 直接承载内容主视图

- 状态：`confirmed`
- 日期：2026-07-29
- 决策：项目的“设计体系” Tab 直接展示当前项目的完整设计体系，不再先展示摘要列表，也不再要求用户通过“打开设计体系”进入二级详情页。
- 页面结构：参考 Open Design 的内容优先主视图，以体系身份、动态章节、视觉 Tokens、组件状态和在线 UI Kit 为主体；项目上下文已经由项目 Tab 确定，因此不复制 Open Design 的多体系列表，左侧仅提供当前体系的动态章节目录。
- 调整方式：默认优先展示设计体系内容。用户定位具体组件或区块后，按需打开智能体调整面板，并自动带入当前定位；顶部只保留草稿或已保存状态、更新时间、调整和保存等必要操作。
- 空状态：项目尚未建立设计体系时的具体入口由 DC-032 细化；创建成功后在同一个 Tab 内切换为内容主视图。
- 排除项：不重复显示项目名或“设计体系”分类标题，不展示文件树，不使用摘要卡片中转，不引入审核、批准或发布操作。

### DC-032 未建立态直接展示设计体系创建工作台

- 状态：`confirmed`
- 日期：2026-07-29
- 决策：项目尚未建立设计体系时，“设计体系” Tab 直接展示创建工作台，不先展示单独空状态，也不要求用户再次点击“创建设计体系”进入表单。
- 布局：采用单屏双区布局。主区承载自然语言设计背景和可选参考资料，紧凑设置区承载目标平台、执行智能体和生成操作；不使用步骤条、多页向导或弹窗表单。
- 输入规则：系统自动把项目名称和描述带入智能体上下文。用户以自然语言描述产品定位、目标用户、核心场景和期望风格；目标平台和执行智能体为必选，Logo、品牌色、截图、Figma UI 规范和其他参考资料均为可选。
- 智能体职责：用户不需要选择 Token 分类、组件范围或固定章节，智能体应根据项目背景和证据组织完整设计体系。
- 状态衔接：提交后在当前 Tab 原地展示真实执行状态；生成成功后直接切换为设计体系草稿主视图，失败时保留全部输入。
- 排除项：不增加空白中转页、冗长结构化表单、风格方案选择或生成内容清单。

### DC-033 设计体系草稿采用连续内容画布

- 状态：`confirmed`
- 日期：2026-07-29
- 决策：设计体系草稿和已保存内容共用一套连续内容主视图，不再把设计规则、Tokens、组件和预览拆成多个内部 Tab，也不使用常驻三栏工作台。
- 页面结构：左侧为根据真实内容生成的章节目录，中间连续展示体系身份、设计原则、视觉 Tokens、组件状态、页面模式和在线 UI Kit；各章节使用内容分隔而不是页面级卡片。
- 可视化规则：设计规则必须组织为可阅读内容，Tokens 必须转换为色板、字体样例、间距和布局等视觉表达，在线 UI Kit 使用主要内容宽度展示真实组件状态和组合场景。
- 调整方式：智能体调整面板默认关闭。用户可以从顶部发起全局调整，也可以从章节发起局部调整；打开面板时自动带入当前章节或组件范围。第一阶段不支持任意 DOM 选择、框选、代码编辑或复杂差异对比。
- 操作层级：草稿的唯一主操作是“保存为项目设计体系”，“放弃草稿”和“重新生成”进入更多菜单。保存后继续使用同一主视图并切换为已保存状态；再次调整时回到草稿状态。
- 产品原则：设计体系内容是页面主角，智能体只作为按需出现的修改工具，不能长期挤压在线 UI Kit 的展示空间。

### DC-034 设计体系以真实产物验证和原子保存判定成功

- 状态：`confirmed`
- 日期：2026-07-29，更新于 2026-07-31
- 成功标准：智能体 task 的 `completed` 只表示执行结束，不代表设计体系生成成功。只有 Open Design 最小事实包通过 Package Audit，且当前包声明的 Preview/UI Kit 与资源能够正常渲染后，系统才能产生可用草稿；不得强制把可选的 `components.html` 伪装成真实组件库。
- 过程反馈：生成页面只展示真实执行事件、当前智能体、开始时间、运行时长和最后活动时间，不使用无法由后端事件证明的百分比或虚构阶段。长时间没有活动时必须明确提示，不得无限保持模糊的生成中状态。
- 失败保护：智能体未接单、执行失败、产物校验失败、UI Kit 渲染失败和保存失败必须区分处理。失败时保留创建输入、参考资料和最近一次有效内容，并提供重新生成或更换智能体入口；技术详情按需展开。
- 草稿隔离：系统内部区分当前草稿和当前已保存设计体系。首次保存前，下游智能体不能把草稿当作项目强约束；已有设计体系产生新草稿时，下游继续读取最近一次已保存内容。
- 保存语义：首次草稿使用“保存为项目设计体系”，后续调整使用“保存调整”。保存必须原子替换当前有效内容；保存失败时旧体系继续有效且草稿不丢失。放弃首次草稿后回到创建工作台，放弃调整草稿后恢复最近一次已保存内容。
- 排除项：不以 task 状态、文件存在、数据库记录创建或页面跳转代替真实产物和视觉可用性验证，也不自动把失败或不完整草稿保存为项目设计体系。

### DC-035 下游统一按已保存设计体系解析设计上下文

- 状态：`confirmed`
- 日期：2026-07-30
- 决策：UI Agent 设计生成和后续设计还原必须共用统一的只读 Design Context Resolver，固定优先级为“云端已保存项目设计体系 > 本地 `DESIGN.md` > 仓库现实”。
- 云端边界：Server 只解析当前项目 `saved` 槽位中已通过产物校验、隔离渲染校验和摘要一致性校验的设计体系；草稿与旧 `design_system_profile` 永远不能成为下游强约束。
- 本地边界：云端 Server 不能直接读取用户本机仓库。没有有效云端已保存体系时，Resolver 返回 `source=none` 和完整优先级，由本地 Agent 继续读取本地 `DESIGN.md`，再以仓库中的真实组件、样式和工程结构作为最终回退依据。
- 可追溯性：解析结果必须包含稳定契约版本、实际来源和内容摘要；使用云端体系时同时提供体系身份、保存时间、来源任务及完整有效产物，供下游任务记录其真实设计约束。
- 当前范围：本阶段只建立 Resolver 与固定输出契约，不接入 UI Agent、设计还原或完整版本治理；下游链路必须在单独阶段显式接入并验证。
- 接入进度：后续独立阶段已将 Resolver 接入 UI Agent 设计草稿的首次生成和再次调整任务。任务创建时固化 `source`、`priority`、`digest` 和完整有效产物，daemon Prompt 只把该 `design_context` 作为设计规范输入，不再向 Agent 暴露旧 Profile JSON。
- 兼容边界：旧 `design_system_profile_id` 暂时仍作为现有 PageSpec 编译器定位 RecipeSet 的内部键，不属于 Agent 设计上下文。设计还原尚未接入 Resolver，真实 Agent 生成与视觉结果仍需单独验收。

### DC-036 仓库分析后冻结并收起参考资料

- 状态：`confirmed`
- 日期：2026-07-31
- 决策：仓库分析成功后，创建工作台不再继续展示完整参考资料表单，只展示本次分析已经使用的资料摘要；生成设计体系自动沿用同一组参考资料，不要求用户重复配置。
- 修改规则：用户主动重新选择参考资料时，当前仓库分析立即视为不再适用于新的输入，设计体系生成操作必须暂停，直到用户使用新资料重新完成仓库分析。
- 影响：参考资料、仓库分析和后续生成形成同一份输入快照，避免页面既展示分析结果又允许用户无提示地改变其来源。

### DC-037 设计体系引擎与资产流程直接采用 Open Design

- 状态：`superseded`
- 日期：2026-07-31
- 决策：Multica 不再参考 Open Design 后自行设计一套设计体系生成流程，而是直接采用 Open Design 的设计体系引擎、来源采集、确定性提取、分层资源包、Agent 工作空间深化、Package Audit、Preview/UI Kit 和模板资源协议作为上游实现标准。
- 停止项：停止继续扩展“一次 Prompt 直接生成固定三文件”、自定义组件识别、自定义 Token 推导和与 Open Design 平行的 UI Kit 生成流程；现有实现只作为阶段性验证和迁移输入，不再定义目标架构。
- 适配边界：Multica 只负责 Project、Issue、用户选择 Agent、任务桥接、云端存储、鉴权权限和设计中心展示，并将 Open Design 的本地执行与资产语义适配到 Multica 云端；不复制或重写其 daemon，也不复制本地 Project 和桌面端产品外壳。固定版本的 headless daemon/engine 可以作为隔离执行引擎运行，但不能成为第二套业务控制面。
- 产品边界：Multica 已确认的项目入口、草稿/保存语言和不引入审核权限等产品决策继续有效；采用 Open Design 的引擎不等于复制其完整界面与审批工作流。
- 证据边界：版本和模块复用必须绑定 Open Design 的固定提交与源码证据；遇到云端适配差异时优先增加薄适配层，不得先发明替代流程。
- 依据：`OD-004`、`OD-008`、`OD-009`、`OD-010`、`OD-011`、`OD-012`。
- 方案：[open-design-engine-integration.md](./open-design-engine-integration.md)。
- 替代原因：2026-08-05 用户确认不接受专用 Open Design Worker 或 Runtime 作为 Multica 正式运行依赖，但要求产品流程、能力语义和分层资源包继续以 Open Design 为核心参照。替代决策见 DC-039。

### DC-038 固定 Open Design v0.16.1 并采用外部编排 workspace

- 状态：`superseded`
- 日期：2026-07-31
- 版本：第一接入基线固定为官方稳定 Release `open-design-v0.16.1`，commit `276b4d8e970bc143d7ad060181a89a834e3d9caf`；不跟随 `main` 或 `latest` 静默升级。
- 执行：本地 Multica daemon 和云端隔离 worker 运行同源 Open Design headless 制品，并由 Multica 将用户选择的 Agent 显式映射到上游 adapter；不支持时派发前失败，不能静默换 Agent。
- workspace：统一使用 Open Design 官方 `orchestrator-scratch` provenance。Open Design 可读写 scratch 并产出结果包，源码 checkout、凭据、分支、PR、部署和写回继续由 Multica 或外部编排器负责。
- 成功边界：必须同时取得 `open-design.run-result-package.v1`、完整设计体系包、上游 Package Audit 和可渲染 Preview/UI Kit；run 或 task 终态不能代替这些证据。
- 迁移顺序：先完成不改主流程的 Phase 0 集成 spike，再依次接入运行时/云端包、切换创建与调整、清理自研引擎；详细方案见 [open-design-engine-integration.md](./open-design-engine-integration.md)。
- 依据：`OD-016`、`OD-017`、`OD-018`、`OD-019`、`OD-020`。
- 替代原因：固定版本实验已经提供真实 Agent、隔离工作空间、Audit、Preview、失败隔离和草稿门禁证据，但 Runtime 分发、Worker 生命周期和上游协议兼容不进入目标架构。替代决策见 DC-039。

### DC-039 以 Open Design 为核心参照翻新 Multica 原生设计引擎

- 状态：`confirmed`
- 日期：2026-08-05
- 决策：Multica 不运行、分发或托管 Open Design Worker、Daemon 和 Runtime；使用现有 Project、Issue、Agent、daemon、任务队列、对象存储和设计中心，原生实现设计体系与后续在线设计能力。
- 核心参照：统一设计任务入口、多来源取证、确定性提取与 Agent 深化、稳定内核加可选扩展的分层资源包、可持续调整工作空间、可执行模板、固定输入快照、Package Audit、Preview/UI Kit 和坏草稿隔离均以 Open Design 的固定版本源码与实验事实为行为基线。
- 执行边界：设计语义、布局和组件判断由用户明确选择的 Agent 完成；Multica 负责输入快照、任务编排、隔离工作区、产物协议、安全收集、对象存储、Audit、真实浏览器预览、draft/saved 生命周期和权限。不存在专用 Design Worker。
- 产物边界：项目设计体系采用 Multica 自有版本化资源包，保留 `manifest.json`、`DESIGN.md`、`tokens.css`、来源证据和可选组件/UI Kit 的分层语义；后续在线设计稿采用语义 brief 加可运行原型，不要求 Agent 直接生成巨大 Figma/Native JSON，也不恢复 PageSpec 通用 DSL。
- 产品边界：继续使用项目设计体系强约束、模板弱参考、用户选择 Agent、草稿/保存、不引入审核、内容主视图和 Issue 协作等既有决策。
- 迁移边界：保留 Worker Phase 0 的实验和质量证据；停止扩展 `open_design_run`、Runtime resolver、archive installer、adapter 和 Worker supervisor。现有数据和代码先隔离，原生链路稳定后再单独清理，不执行破坏性迁移。
- 阶段更新：2026-08-05 原第一阶段只替换项目设计体系生成和调整内核；2026-08-12 的 DC-041 至 DC-046 已将首页与页面 Design Document 闭环纳入新的 Phase A。共享设计体系和模板仍按 Slice B 至 E 后续推进，设计还原继续作为独立边界。
- 方案：[2026-08-05-multica-native-design-engine-design.md](../../superpowers/specs/2026-08-05-multica-native-design-engine-design.md)。
- 依据：`OD-001` 至 `OD-013` 的产品与包语义证据，以及 `OD-021` 至 `OD-044` 的执行、失败和质量门禁实验。

### DC-040 Native V2 产品切片内渐进清理旧残留

- 状态：`confirmed`
- 日期：2026-08-12
- 决策：取消独立、一次性的 Open Design V1 破坏性移除 Phase B。后续只沿 Native V2 产品能力推进，不再从全仓库旧实现反推一次性删除范围。
- 完成定义：采用两级清理规则。每个 Native V2 功能切片在证明完整替代关系后，必须删除切片内部已经无调用的 V1/Worker 分支、fallback、旧配置和旧测试，并以此阻塞切片完成；跨切片、跨数据生命周期或仍有外部消费者的残留不得迫使当前切片扩大范围，必须进入唯一退役账本。
- 删除门禁：计划批准前限定当前切片的 API、daemon、handler/service、数据、Web/Desktop、文件和符号范围，先取得 V2 正向与失败隔离证据，并确认切片内活动消费方已经迁移；旧入口必须形成局部拒绝或不可达合同，通用 Agent 生命周期不得受影响。实现中新发现的范围默认不自动纳入。
- 退役账本：`native-v2-retirement-register.md` 是旧能力状态的唯一事实源，只在功能切片真实触达旧能力时增量更新，不预先盘点全仓库。状态只使用 `active`、`write-retired`、`unreferenced`、`retired` 和 `data-pending`。
- 数据边界：普通功能切片只停止新的旧链路读写并删除死代码，不删除历史数据库行、对象、表或约束。`open_design_run`、non-V2 package rows、历史 archive/evidence/Preview 对象和 schema constraint 的不可逆退役只能在全部活动代码退出后单独提出、审批和验证。
- 历史关系：DC-039 保持有效，其“现有数据和代码先隔离，原生链路稳定后再单独清理，不执行破坏性迁移”重新成为迁移边界；本决策补充局部替代后的强制清理门禁。
- 取消路线：`2026-08-12-native-design-phase-1-closure-and-legacy-removal-design.md` 的独立 Phase B，以及 `2026-08-12-open-design-v1-destructive-removal.md` 的 Task 3–12，均改为 `superseded`。其中已有 Phase A 自动化证据继续有效。
- 分支边界：`feature/fengchen-fixed-v2` 只保留为取消路线的隔离 checkpoint，不合入 `feature/fengchen`，不作为后续实现基线，也不计入产品进度。
- 当前下一步：新 Phase A 产品方案已由 DC-041 至 DC-046 确认；用户复核书面规格后，只为 A1 至 A6 编写实施计划。在计划获批前不修改产品代码，也不推进旧代码或数据清理。
- 方案：[2026-08-12-native-design-slice-driven-evolution-design.md](../../superpowers/specs/2026-08-12-native-design-slice-driven-evolution-design.md)。

### DC-041 设计中心首页、工作区共享设计体系与社区模板分期

- 状态：`confirmed`
- 日期：2026-08-12
- 总体顺序：新 Phase A 建设首页页面设计任务入口；后续依次建设工作区共享设计体系、官方模板、工作区成员模板发布和跨工作区社区模板。五个切片共享任务上下文与不可变引用原则，但不得合并成一个实施阶段。
- 首页边界：首页是跨项目页面设计任务发起器，不创建第二套 Project。第一版只生成项目内页面设计稿或可运行原型，项目和 Agent 必选；创建成功后打开目标项目的“设计草稿”，首页与项目 Tab 读取同一服务端 task/draft。设计体系创建继续留在项目“设计体系”Tab。
- 约束优先级：用户明确需求与项目 saved 体系构成当前任务的主要约束；工作区共享设计体系和模板绑定体系只作为弱参考，不能覆盖项目 Tokens 或隐式建立项目体系。
- 共享体系：第一阶段仅工作区成员可发现和使用。公开对象不是项目体系本身，而是从 saved Native V2 package 经重新校验和安全剥离生成的不可变 revision；项目后续调整不影响已发布版本，历史任务固定 revision 和 digest。
- 模板边界：模板是页面设计任务配方，不是设计体系。每次应用模板都固定 template revision、manifest digest、用户需求、项目、Agent、设计体系引用和附件快照。模板升级或下架不改变历史任务。
- 模板分期：先建设官方模板目录以验证任务效率和生成质量，再开放工作区成员发布，最后才建设跨工作区社区发现、许可、举报、审核与推荐。
- 安全与历史：共享体系和模板发布都必须剥离项目、仓库、任务和附件私有信息并重新执行 Audit/Preview；下架只阻止新引用，历史任务继续读取固定 revision。来源项目删除不自动删除已发布资源。
- 现有模型：现有 workspace-scoped template library/catalog/revision 只作为迁移输入与部分基础，不能直接等同于新的共享资源和社区治理模型；先定义资源、revision 和应用快照契约，再决定数据库实现。
- Phase A 边界：只有首页页面设计入口和 DC-042 至 DC-046 确认的 Design Document 闭环进入新 Phase A；共享体系和模板 Slice B–E 不计入 Phase A 完成条件。Phase A 产品设计已确认，实施仍未开始；先前约 48% 仅是确认前的讨论基线，不再作为当前进度。
- 方案：[2026-08-12-design-home-public-systems-community-templates-design.md](../../superpowers/specs/2026-08-12-design-home-public-systems-community-templates-design.md)。

## 已确认的新增决策

### DC-042 页面设计采用版本化 Design Document

- 状态：`confirmed`
- 日期：2026-08-12
- 产物：页面设计 task 输出 `multica.design-document/v1`，由 `manifest.json`、语义 `brief.json`、完全离线的可运行 prototype、assets 和 `coverage.json` 组成。Audit/Preview receipt 与 package digest 绑定并独立持久化，避免回执进入自身摘要形成循环。
- 资产粒度：一份 Design Document 可以包含主页面、相关子页面、状态、弹窗和关键流程；一个项目允许多份文档。文档使用不可变 revisions 演进并维护当前 draft/saved 指针，第一版用户只看到当前草稿和已保存状态。
- 调整：支持文档、页面、状态、弹窗和命名区块的自然语言 scope；每次调整创建独立 task，固定 base revision，并输出完整新 package。第一版不支持任意 DOM 点选、分支合并和完整版本时间线。
- 保存：只有用户明确保存后 saved 才移动；下游智能体、MCP 和交付链只读取 saved。首次失败不创建空文档，调整失败不移动 draft，保存失败不移动 saved。
- 非目标：不恢复大型 PageSpec DSL、通用 Scene Graph、真实 API 联调、自动前端交付或 Figma/Native Design JSON。

### DC-043 页面 Design Document 使用 task 内自动只读仓库 Grounding

- 状态：`confirmed`
- 日期：2026-08-12
- 决策：首页不增加独立仓库分析前置步骤。所选智能体在页面设计 task 内，对其运行时可访问的项目仓库执行有界只读取证，固定 checkout/commit、相对来源、摘要、组件/路由/样式事实、冲突与不确定性。
- 安全：不修改源仓库，不长期保存绝对路径、凭据、无关业务数据或未授权完整源码。普通调整沿用固定事实；用户主动同步最新仓库时才创建新的 grounding 和 input snapshot。
- 不可访问：必须明确提示，并由用户选择仅按项目描述、关联任务、附件和 saved 体系继续，或停止并更换智能体/运行时；不得静默描述为已 grounding。
- 历史关系：本决策只约束页面 Design Document task，不推翻 DC-018/DC-036。项目设计体系创建仍由用户主动发起独立仓库分析，并在成功后冻结参考资料。

### DC-044 Design Document 复用现有本地浏览器强制门禁

- 状态：`confirmed`
- 日期：2026-08-12
- 决策：页面 Design Document 直接复用员工本地守护进程现有 `server/internal/designpreview`，由本地运行时自动解析 Chrome/Chromium，在 Audit 通过后执行 Preview。Phase A 不引入中心化 Chromium 服务。
- 强制边界：浏览器不可用时 task 失败，不跳过 Preview，不新增待验证候选、前端补验证或无浏览器保存例外。未通过 Audit/Preview 的 package 不得进入或覆盖 draft，旧 revision receipt 不得批准新 package。
- 原型边界：允许 package 内离线 HTML/CSS/JavaScript 和本地状态交互，但禁止网络、真实 API、凭据、外部脚本、Service Worker 和宿主同源权限。页面/状态/流程和关键交互检查是现有门禁的叠加，不得放松安全与 digest 绑定。
- 质量边界：浏览器通过只证明原型能够安全运行，不代表视觉质量通过；严格验收仍需用户 Chrome、Network、Console 和人工业务/视觉判断。

### DC-045 页面 Design Document 与任务（Issue）采用可选关联

- 状态：`confirmed`
- 日期：2026-08-12
- 决策：首页创建页面设计时项目和智能体必选，任务（Issue）与目标平台可选。无关联任务时仍可创建探索性 Design Document，系统不自动创建任务。
- 快照：有关联时固定当时的 `issue_id` 和可读取需求；无关联时 coverage 映射用户自然语言需求。后续补充关联不改写历史 revision，只从后续 task/revision 生效。
- 联动：Design Document 和 task 可以在任务时间线中显示可追溯事件或链接；保存设计稿不自动改变任务状态、负责人、优先级或完成状态。

### DC-046 Phase A 按 A1 至 A6 内部子切片实施

- 状态：`confirmed`
- 日期：2026-08-12
- 顺序：A1 Design Document 核心协议与持久化；A2 首页入口和项目 task 状态；A3 仓库 Grounding 与持续工作空间；A4 Audit、Preview 与首个 draft；A5 调整、保存与放弃；A6 真实 CRM 严格验收。
- 命名边界：A1 至 A6 是 DC-041 Slice A 的内部子切片，与后续 Slice B 至 E 正交。A1 至 A5 自动化通过不能替代 A6。
- 实施边界：每个子切片必须携带 DC-040 的局部清理门禁、退役账本变化、V2 正向、失败隔离、旧路径负向、范围外回归、持久化不变量、实际命令、GitNexus `detect_changes` 和独立回滚边界。
- 基础与进度：Phase A 不从零开始，现有任务、本地运行时、Native V2 package、对象存储、digest、Audit、`designpreview`、draft/saved、仓库事实和设计中心工作区是 A1 至 A5 的复用基础。产品设计确认度为 100%；按当前剩余工程工作保守估算，Phase A 工程基线约 40%–45%，暂记约 42%。每个阶段报告必须按实际结果重算，不得按阶段数量机械累加。
- 方案：[2026-08-12-native-design-phase-a-design-document-design.md](../../superpowers/specs/2026-08-12-native-design-phase-a-design-document-design.md)。

### DC-047 Open Design 证据基线改为 v0.19.2

- 状态：`confirmed`
- 日期：2026-08-16
- 决策：接入参照基线从 `open-design-v0.16.1`（commit `276b4d8e`）改为 `open-design-v0.19.2`。此后凡引用 Open Design 行为作为依据的判断，均以 0.19.2 为准。
- 历史处理：`OD-021` 至 `OD-044` 记录的是固定 worker 路径在 0.16.1 上的行为。该路径已由 DC-039 否决，这批证据降级为失败隔离与门禁设计的经验来源，不再是行为基线。
- 影响：`open-design-evidence.md` 与 `open-design-engine-integration.md` 中绑定旧版本的结论需标注为历史证据。

### DC-048 迁移范围收窄为首页、社区、设计体系三个 tab

- 状态：`confirmed`
- 日期：2026-08-16
- 依据：用户实际使用 0.19.2 桌面端后确认，Multica 只需要这三个 tab 内部的能力。
- 范围内：首页设计任务发起器、社区模板画廊、设计体系目录与创建流程。
- 范围外：Studio 外壳、自动化、集成、看板、成员、插件市场管理面、功能 skill 目录、本地 daemon 与 CLI 适配器、Electron 外壳、BYOK 代理、clipper 扩展、HTML/PDF/PPTX/MP4 导出，以及 deck / image / video / audio 产物形态。
- Studio 的替代物：Multica 项目内的 Design Document 工作区，不再另建。
- 相关事实：0.19.2 已在 v0.13.0（commit `29b138f7a`，#4691）把 Brands（中文标签“设计系统”）并入设计体系，品牌提取降级为创建向导的一个来源。`BrandsTab.tsx`、`/brands` 路由和 `entry.navBrands` 已无导航入口。Multica 照搬合并后的模型，不建独立品牌套件实体。

### DC-049 首页采用场景 chip 并复刻 Open Design 首页信息架构

- 状态：`confirmed`
- 日期：2026-08-16
- 决策：设计中心首页复刻 Open Design 首页的视觉与信息架构（输入卡、场景 chip 轨、场景插画、选择器区），品牌替换为 Multica Design。
- 复刻边界：只复刻视觉与信息架构，不搬运代码。Open Design 使用 Next.js 16 + React 18 + CSS Modules，Multica 必须按仓库 UI 规范用 shadcn/Base UI、语义 token 和 role-named `--text-*` 字号重写。`HomeHero.tsx` 与 `HomeView.tsx` 中的 AMR 余额、DeepSeek Harness 设置、Vela 计费、宠物、campaign 和 onboarding 均不迁。
- chip 范围：第一版只放有真实产物支撑的五个——UI Mockup、网站复刻、线框图、移动应用、来自 Figma；“来自模板”和“创建品牌套件”灰态留位；其余八个按 DC-048 不放。
- 契约预留：发起 API 从第一版就带 `recipe` 字段，第一版只接受上述五个值，模板切片建成后扩展为模板 id，不改 API 契约。
- 依据：Open Design 12 个创建 chip 中有 7 个的 `projectKind` 同为 `prototype`，chip 的真实维度是场景配方而非产物类型。

### DC-050 tweaks 与 critique 进入产品

- 状态：`confirmed`
- 日期：2026-08-16
- 决策：Open Design 的 tweaks 面板与 critique 评审进入 Multica 产品。两者在 Open Design 属于 Studio 而不属于 DC-048 的三个 tab，在 Multica 落在项目内的 Design Document 工作区。
- tweaks 的形态：它是一个 skill，不是平台 UI——由智能体把产物重构为读取 CSS 自定义属性（`--accent`、`--scale`、`--density`、`--mode`、`--motion`），并附带包内 vanilla-JS 侧栏，改动持久化到 `localStorage`。
- tweaks 的边界：只允许进入 Design Document 的 `prototype/`（Phase A 第 7.4 节允许包内 JS 与本地状态）。不得进入设计体系包——V2 Audit 与设计体系 prompt 均禁止 script、事件属性、表单和外部引用。
- critique 的形态：`apps/daemon/src/critique/` 共 18 个文件、3458 行，包含 designer/critic/brand/a11y/copy 五个带权重的评审角色、`maxRounds` 阈值循环、超时、run registry、scoreboard、transcript 和 SSE 流式回放。`ratchet.ts` 是灰度推进建议，属运维工具，不迁。
- critique 的边界：critique 是产物成型前的迭代改进循环，Audit/Preview 是产物成型后的系统门禁。DC-034 不松动——**critique 分数达标不构成 draft 形成条件**，draft 仍只能在 Audit 与 Preview 通过后原子形成。
- 配置取舍：`fallbackPolicy: fail` 与 Multica 的 fail-closed 语义一致，采用；`ship_best` 与 `ship_last` 与“不允许把失败或不完整内容自动推进为已保存状态”冲突，不采用。

### DC-051 不把设计体系素材授权作为约束项

- 状态：`confirmed`
- 日期：2026-08-16
- 背景：Open Design 附带 154 个设计体系包，其中含 Apple、Airbnb、Stripe、BMW 等真实品牌的设计语言。评估迁移时曾提出其在商业产品内再分发与开源仓库自身分发不是同一问题。
- 决策：用户判断 Open Design 作为开源项目公开展示即表明无法律风险，本条不作为迁移约束，从待决策列表关闭。
- 记录用途：保留决策依据本身，便于日后需要时复核，而不是重新讨论。

### DC-052 设计体系按仓库划分

- 状态：`confirmed`
- 日期：2026-08-16
- 替代：DC-025。
- 决策：同一项目下的不同仓库各自维护一套设计体系。一个项目可能同时包含 C 端 H5、App 和后台管理系统，它们的设计语言不能被迫共用一套。
- 模型：`project_design_system` 增加可空的 `project_resource_id`。`NULL` 表示项目级体系（跨仓库通用，也是不选仓库时使用的那套），非 `NULL` 表示该仓库专属体系。现有行天然是项目级，零数据迁移。
- 解析链：选了仓库先取该仓库体系，没有则回落项目级；没选仓库直接取项目级；仍然没有则按 DC-035 继续回落到本地 `DESIGN.md` 和仓库现实。
- 唯一性：必须拆成两个 partial unique index。PostgreSQL 将 `NULL` 视为互不相等，单一复合唯一键会放行多条项目级体系。
- 连带修正：`project_design_system.platform` 原为项目级且 `NOT NULL`，一个项目只能声明一个平台，与多形态项目对不上。按仓库划分后其语义变为“该仓库是什么形态”。
- 不引入工作区默认体系：目录只做“挑选 → 复制成项目体系”。要统一视觉时使用“从现有体系复制”，项目内明确留下自己的一份，保持可追溯。这与 P-005“空项目必须保持未建立状态”一致。
- 2026-08-19 修订（用户决定）：允许**独立设计体系**——`project_design_system.project_id` 可空（迁移 899），NULL 表示属于工作区本身、不绑定项目的体系，可以有任意多个；设计体系库的「新建设计体系」进入独立的创建页（复刻 Open Design 创建流程的版式：名称、来源链接、品牌描述、官方体系参考、平台与智能体），生成后落在 `/designs/systems/{id}`。这是对“不引入工作区默认体系”的补充而非推翻：独立体系仍不是任何项目的默认，项目要统一视觉仍须显式复制。
- 界面影响：DC-031 需澄清而非推翻。设计体系 Tab 内新增仓库切换器属于 scope 切换，内容主视图仍然直接渲染；不得据此退回摘要列表加二级入口的形态。

### DC-053 仓库可选，选中才做仓库取证

- 状态：`confirmed`
- 日期：2026-08-16
- 决策：设计任务发起时，项目与智能体必选，仓库与任务（Issue）可选。
- 选中仓库：带出该仓库的设计体系，并在 task 内对该仓库执行有界只读取证。这同时把 DC-043 原本含糊的“有界”定义为“对这一个仓库取证”。
- 未选仓库：跳过整个 grounding 阶段直接生成，使用项目级设计体系，并在文档中显式标注本次未做仓库取证，不得让用户误以为智能体读过代码。
- 依据：与 P-008“不增加隐式或强制的前置仓库扫描”一致。
- 2026-08-21 界面修订（用户要求）：删除首页输入框下方的常驻状态提示行（「未选择仓库…」「设计体系未指定…」及其选中态变体）。「不得让用户误以为读过代码」的保证改由提交成功 toast（「已创建页面设计任务，本次未做仓库取证」）与服务端 `repository_grounded` 回执承担，不再以常驻文案形式出现在输入框下方。

### DC-054 共享设计体系与社区模板采用先窄后宽

- 状态：`confirmed`
- 日期：2026-08-16
- 修订：DC-041 的分期口径。
- 决策：工作区级设计体系目录与社区模板画廊排在 Phase A 之后，不与 A1 至 A6 交错实施。
- 首页处理：A2 只为设计体系选择器与模板选择器留出灰态位置，等对应切片建成后点亮。
- 依据：原生 V2 链路的生产端与包契约此前从未接通（见 DC-055），先证明一条端到端链路能跑通，优于同时铺开三条。

### DC-055 原生 V2 生产端与包契约必须先对齐

- 状态：`confirmed`
- 日期：2026-08-16
- 问题：`handler/project_design_system.go` 为所有 generate / adjust / regenerate 任务标记 `PackageSchema = multica.project-design-system/v2`，但智能体收到的产物指令仍是 V1 三文件契约（`DESIGN.md`、`tokens.css`、`components.html`）。`components.html` 不在 `classifyV2Artifact` 的接受列表内，任务在 Audit 之前即以 `archive_path_undeclared` 失败。
- 第二处缺陷：预览服务注入的 `<link rel="stylesheet" href="tokens.css">` 是相对路径，而所有预览目标都位于下一级目录（`ui-kit/index.html`、`preview/*.html`），解析后 404。Audit 的 Token 检查是静态文本检查、Preview 只检查可见性，两者都会通过，于是系统对一个从未应用设计 Token 的页面出具了通过回执和截图。
- 根因：prompt 与包契约由两侧独立规定，没有任何测试跨越这条边界。
- 决策：在任何新切片之前先对齐两侧，并补跨边界测试——按 prompt 声明的文件集构造 package，必须通过真实的 `CollectV2Directory` 与 `auditV2Package`。
- 进度口径影响：DC-046 记录的 Phase A 工程基线约 42%，其中 A4 的 60% 建立在这条从未被原生链路走通的管道上。对齐并取得真实证据后必须重算。

## 当前提案

### DC-007 旧提案：从现有设计中心发起设计任务

- 状态：`superseded`
- 日期：2026-07-28
- 提案：现有设计中心作为设计任务入口，复用当前项目切换能力形成项目范围内的设计工作空间。
- 原待决定：从设计中心发起时如何关联 Issue；任务、会话、Run 和最终设计稿之间的数据关系。
- 替代原因：DC-041 已确认首页第一版是跨项目页面设计 task 发起器，项目和智能体必选，task 成功创建后进入目标项目“设计草稿”；DC-042 至 DC-045 已进一步确认页面产物协议、任务内仓库 Grounding、浏览器强制门禁和任务（Issue）可选关联，不再由本旧提案承载。

### DC-008 旧提案：设计任务模板延后

- 状态：`superseded`
- 日期：2026-07-28
- 提案：先落地设计体系和设计任务发起器，再研究和接入 Open Design 式模板。
- 原待决定：前两阶段是否只预留模板引用协议，还是完全不建模。
- 替代原因：DC-041 已确认模板分期为官方模板、工作区成员发布、跨工作区社区模板；模板不进入新 Phase A，只在后续独立切片中实现。

### DC-010 用 Design Run 表达一次设计执行

- 状态：`proposal`
- 日期：2026-07-28
- 提案：不引入第二套 Project，把 Open Design 的本地 project 能力映射为 Multica Issue 下的一次设计执行或工作空间。
- 依据：见 `OD-001`、`OD-006`。
- 待决定：是否需要显式产品名称，以及独立从设计中心开始时怎样自动关联或创建 Issue。

## 已替代路线

### DC-014 旧决策：所有设计体系必须覆盖完整组件和页面模式

- 状态：`superseded`
- 日期：2026-07-28
- 原决策：设计体系必须同时可供机器和 Agent 执行，并强制覆盖设计原则、基础 Tokens、组件、状态、页面模式、可访问性、禁用规则、来源和版本。
- 保留原则：设计体系不能只是一篇 AI 说明文档，必须同时具有 Agent 可读规则和机器可执行契约。
- 替代原因：完整组件、状态和页面模式不适用于所有成熟度的项目；强制补齐会制造没有来源依据的伪规范。
- 替代决策：DC-015 的 Open Design 最小包与按证据扩展规则。

### DC-009 旧提案：把 UI 规范作为设计体系事实来源

- 状态：`superseded`
- 日期：2026-07-28
- 原提案：把 Figma UI 规范视为主要事实来源，再由 Agent 分析为项目设计体系。
- 替代原因：这会让设计体系依赖外部 Figma 文件，无法支持真正从零创建、在线维护和向多个消费端派生。
- 替代决策：DC-012。

## 暂停路线

### DC-011 不让旧语义编译器路线约束新方向

- 状态：`paused`
- 日期：2026-07-28
- 背景：此前 `PageSpec -> Native JSON compiler` 路线针对 B 端结构化页面进行了大量实现，但真实生成质量没有达到产品目标，用户已要求暂告一段落并重新研究 Open Design。
- 保留价值：需求覆盖检查、模板残留检测、版本化草稿和真实视觉验证经验仍可复用。
- 限制：在重新确认在线设计产物和 Agent 工作方式前，不得默认它仍是通用 UI 设计主引擎。

### DC-056 Open Design 预置资源落地方式

`confirmed`

Open Design 的两类预置资源按各自性质分别落地，不共用一套存储：

- **社区模板**（291 条官方插件）进入 `design_scenario_recipe`，随迁移 892 作为 `origin='builtin'` 种子数据。其中 281 条可映射：6 条模式为 `utility`/`template`/`design-system`，不是内容模板；4 条与迁移 889 手写的中文配方 slug 冲突，当时手写版本优先；2026-08-19 起反过来：889 的 10 条手写配方由迁移 897 退役（它们是 Open Design 目录导入前的过渡内容，没有封面也没有上游对应），这 4 条上游模板随迁移 898 补入，社区 tab 的内置内容自此全部来自 Open Design。非 `prototype` 模式一并种入并可浏览，但创建接口按既有规则拒绝，画廊卡片已有对应说明；这样目录如实反映 Open Design 提供什么、本阶段能产出什么，而不是把差异藏起来。
- **官方设计体系**（152 个品牌包）不入库，由 `server/internal/designsystemcatalogue` 以 `go:embed` 随二进制分发，经 `GET /api/design-systems/builtin[/{slug}]` 只读提供。理由：它们对每个工作区完全相同、没有归属、无人编辑，与 `project_design_system`（项目自己生成并保存的体系，DC-052）不是同一类东西。分开存放使内置体系不可能被误认成已保存体系，新增一个内置体系是带 diff 的代码变更而非数据迁移。

设计体系库的「官方」范围因此不再为空，但仍是只读参考：要在项目中使用，仍须在项目的「设计体系」内创建并以其为参考风格，不引入工作区默认体系（DC-052 未被替代）。

### DC-057 Design Document 工作区页面与修订读取契约

- 状态：`confirmed`
- 日期：2026-08-19
- 依据：用户 2026-08-18 明确目标——“在 multica 里面使用 OD 完整的首页、社区、设计体系功能，包括 OD 生成设计稿之后的一系列的页面（预览、编辑等）”。此前 A1–A5 只有服务端与卡片，一份 Design Document 在前端没有任何详情、预览、调整、保存入口，卡片注释写着“There is no document detail route yet”。
- 决策：新增文档工作区路由 `/{slug}/designs/documents/{id}`（web 与 desktop 同时接入），对应 Open Design Studio 生成后的预览与迭代：左栏为需求描述、任务活动、版本时间线与自然语言调整发起器（整份文档或仅当前页面、执行智能体），右栏为原型 iframe（页面切换、适应 / 桌面 / 移动视口、重新加载、新标签页打开、全屏），顶栏承载保存与放弃草稿。首页发起、社区「直接创建」和所有文档卡片都进入该页面。
- 契约：`GET /api/design-documents/{id}/revisions`、`GET …/revisions/{revisionId}`（返回 brief / coverage / audit / preview 回执、manifest 的页面 / 流程 / 预览目标，以及 30 分钟有效的预览能力令牌与 `resource_base_path`）、`POST …/revisions/{revisionId}/restore`（草稿指针回退到历史修订，saved 不动，运行中拒绝）。未鉴权文件路由 `/api/design-document-previews/{workspaceId}/{revisionId}/{digest}/{accessToken}/files/*` 沿用设计体系预览的能力令牌模式；HTML 响应的 CSP 与守护进程 Preview 门禁一致（`script-src 'self' 'unsafe-inline'`、`connect-src 'none'`、`sandbox allow-scripts`），`frame-ancestors *` 与社区封面同一理由。`POST …/adjust` 新增可选 `base_revision_id` 守卫，基线已变时返回 409 `base_revision_changed`。
- 修正：服务端 `designDocumentBindingFromContext` 从未设置 `RevisionID`，而守护进程以任务 id 作为修订身份，两侧绑定不一致导致每一个真实包在 `POST /tasks/{taskId}/design-document/package` 上传时即以 `binding_invalid` 被拒，走不到 Audit 与 Preview——这正是 DC-055 描述的“两侧独立规定、无测试跨越边界”。现服务端与守护进程一致以任务 id 作为修订身份，`TestDesignDocumentBindingMatchesTheDaemonBinding` 跨包断言两侧相等；包契约平台白名单补 `cross_platform`（此前跨端文档在收集阶段即失败）。
- 边界：回退只移动草稿指针，不重新形成 draft（DC-034 不松动）；tweaks 与 critique（DC-050）尚未接入工作区；文档删除、重命名与归档导出仍未提供。
- 证据：`server/internal/handler/design_document_revision_test.go`、`design_document_binding_test.go`、`cmd/server/router_design_document_test.go`、`packages/views/designs/design-document-page.test.tsx`、`packages/core/api/client.test.ts`、`packages/core/api/schemas.test.ts`、`pnpm typecheck` 通过；真实智能体端到端仍待 A6 人工验收。

### DC-058 三 tab 对齐 Open Design 的补齐项与 tweaks / critique 落地方式

- 状态：`confirmed`
- 日期：2026-08-19
- 依据：用户目标“在 multica 里面使用 OD 完整的首页、社区、设计体系功能”，以及 DC-050（tweaks 与 critique 进入产品）。
- 设计体系 tab：官方体系详情以 Open Design 自带的 `system/kit.html`（含深色变体）为封面展示，随二进制嵌入并以 bundle 摘要版本化的未鉴权路由 `/api/design-systems/builtin/{slug}/showcase/{digest}/{variant}` 提供（CSP 不允许脚本与网络，`frame-ancestors *` 与社区封面同理）。
- 2026-08-20 用户验收修正：详情不再逐章节展示 DESIGN.md，而是复刻 Open Design DesignKitView 的模块结构与顺序——品牌标识（定位语）、Logo（内置包无 Logo 资产，如实显示「暂无 Logo」）、字体排版（Typography 章节的三个字族 + 权重刻度 + 以 DESIGN.md H1 为样例行）、调色板（色彩章节的命名色卡：标签、OD 推断的角色 chip、hex、用途文案，按 hex 去重、上限 12）、图像与布局（布局准则要点）、设计系统（kit 框 + 浅色/深色 + `system/kit.html` 标注 + `system/tokens.default.json` 契约 chips）、设计系统素材（六个派生页实时装框，`system/artifacts/*` 已随二进制打包并作为 `artifact-{id}` 变体经同一摘要路由提供）。列表行色条同步改为 OD 的四槽规则：从 DESIGN.md 自身的命名色中按 [背景, 辅助, 前景, 强调] 提示词挑选（front matter 色表填满四槽时优先），不再取 design-tokens.json 的前几个声明色（那会让所有卡片同为深色）。解析对我们保留中文 DESIGN.md 的包接受 CJK 标签与全角标点，这是相对上游的有意偏差。
- 2026-08-20 二次验收修正：字体排版与调色板的解析改为逐条移植上游 `parseTypography` / `parseColors` / `inferRole`——共享字重取自第一条含权重的行；字族先看 Families 别名链，再看按角色关键词（display/head/title、body/text、mono/code）匹配的加粗要点，要点正文截到第一个破折号/括号/反引号为止（Claude 的 "Line-heights of 1.10" 这类怪值与上游渲染一致，属如实复刻）；调色板角色无语义关键词时回落为小写标签本身（Parchment → parchment），表格行按 role|name|hex|usage 取列。卡片 UI 采用 OD 形态：hex 印在色块上（按亮度取深浅文字）、名称、角色行、完整用途说明。Claude 包 12 张卡与参照图逐字一致，已用 Go 测试钉住。
- 官方体系作为参考风格：项目设计体系创建工作台的参考资料新增「官方设计体系 · 参考风格」，最多 3 个，服务端以 `builtin_design_system` 引用把包内 DESIGN.md 与 tokens.css 内联进冻结的输入快照，prompt 要求智能体采纳其方向、产出项目自己的体系而不复制品牌身份。这是 DC-056“挑选 → 以其为参考风格”的落地，不是复制。
- 社区 tab：点击卡片封面或标题打开详情弹层——示例全尺寸实时渲染（可交互、可全屏、可新标签页打开）、提示词（可复制）、分面，以及「直接创建 / 填入首页」，对应 Open Design 社区的 PluginDetailsModal。
- tweaks：按 DC-050 作为约定而非平台 UI——设计文档 prompt 给出 `--accent/--scale/--density/--mode/--motion` 与 `prototype/tweaks.{css,js}` 的包内侧栏约定（只在需求或调整要求时执行，storage 访问必须 try/catch，不得触达 `parent/top`），工作区调整发起器提供「添加调整面板」预设指令，走正常的调整 → Audit → Preview 路径。
- critique：按 DC-050 作为产物成型前的迭代改进循环——prompt 在写 coverage 之前运行 designer / critic / brand / a11y / copy 五个视角的评审循环（阈值 8，至多 3 轮），记录到可选的 `critique.json`（`multica.design-document-critique/v1`）；包契约接受并严格校验该文件但分数不影响 Audit 结论；修订详情读取该报告，工作区以“设计评审”面板展示末轮分数、结果与未关闭的发现，并标注“这是智能体自己的评审记录，不决定草稿是否成立”。
- 首页参考文件：发起器可随提示词暂存截图 / PDF / 文本（最多 8 个、每个 16 MB），服务端按附件 id 解析、读取存储字节并把大小与 SHA-256 钉入冻结输入，守护进程经任务级路由 `GET /api/daemon/tasks/{taskId}/design-document/attachments/{attachmentId}` 只能取上下文列出且字节未变的附件，并落到 `reference/attachments/<id>` 供智能体阅读；prompt 说明位置与“复用图片须复制进 assets/”。
- 工作区导出：`GET /api/design-documents/{id}/revisions/{revisionId}/archive` 经重新校验后以命名 ZIP 返回修订包，工作区更多菜单提供「下载原型包 (.zip)」，对应 Open Design 的下载为 .zip。
- 设计体系 tab：顶栏右侧「新建设计体系」对应 Open Design 设计体系页右上的创建按钮；Open Design 进入专门的创建流程，这里体系属于项目（DC-052），按钮先选项目、再打开该项目 tab 的「设计体系」工作台（库内「去项目编辑」同样落到该子 tab），不另开第二条创建路径。官方目录 20 个分类由 pill 云改为下拉（`DesignFilterSelect`），列表不再被挤到首屏以下。
- 2026-08-20 修正「不另开第二条创建路径」：设计体系可以不挂项目而独立创建（`project_id` 为空 + 必填 `name`，迁移 899），库顶栏「新建设计体系」进入独立创建页 `/{slug}/designs/systems/new`。该页按用户要求复刻 Open Design 的创建流程：置顶栏左「← 返回」右「继续生成」（生成动作在顶栏，无底部栏）、左列固定 hero（三步 · 约 3 分钟 · DESIGN.md/tokens/UI Kit/预览 交付物与示意卡）、右列一张 220px 标签列的分行卡片——GitHub 或网站（含来源 chip、favicon 式标签规则 owner/repo）、从品牌开始、添加文件（拖放/浏览，单个 12 MB 上限）、描述品牌、粘贴 DESIGN.md（编辑/预览分段 + awesome-design-md 参考链接）、备注。二次验收（用户以上游截图纠正）后，「从品牌开始」按上游 BrandPickerModal 完整复刻：65 品牌 / 14 行业分类的参考目录随 views 内置（`brand-references.ts`，fame 分层排序、Google favicon 服务、中文分类标签取上游 zh-CN 文案），弹窗为左侧搜索 + 纵向分类导航、右侧「热门品牌 · 点击添加」chips 与双列品牌墙（悬停出「添加」按钮、分页 + 无限滚动），选中即把 `https://<域名>` 加入来源链接——与上游语义一致；官方体系引用不再由此页添加（项目工作台的参考资料仍支持）。与上游的有意偏差：①智能体与平台必选（P-008 生成即任务）；②名称必填（体系是库内长期实体，上游自动命名）；③描述品牌必填（服务端 brief 为生成的主输入，上游任一来源即可提交）；④粘贴的 DESIGN.md 在提交时落成 `DESIGN.md` 附件进入冻结输入（服务端无专用字段）；⑤备注并入 brief（`\n\n备注：`），提交后不单独存在；⑥「从资料库选择」上游 0.19.2 本就隐藏，不迁移。三次验收（用户追问后）补齐了此前误判为不可迁的两块：「从现有设计系统复制」按上游真实语义迁移——它只是把所选体系的 DESIGN.md 正文填入粘贴框（不是服务端复制端点，此前的省略理由不成立），来源含官方目录（detail 的 `design_markdown`）与工作区已保存体系（V2 包根级 `DESIGN.md`，经包预览的能力令牌文件路由读取），取消选择恢复手输内容（上游 manualDesignMdRef 语义）；高级区「高级 · 仓库、本地代码、Figma」迁移为可展开区——上传 .fig（作为附件原样随任务提供，如实注明 Multica 不做本地解码）、Figma URL（仅收 figma.com/design|file，保存为标签「Figma 设计来源」的链接引用）、备注移回区内；GitHub 仓库行为说明文案（仓库链接走上方来源行、访问取决于智能体自身凭据——上游的「访问方式」面板管理其自带守护进程的连接器授权，Multica 的任务本就以用户凭据在本机执行，无需对应物）。四次跟进（同日）把先前「留作后续切片」的两件事落地：任务 prompt 按上游语义分流 link 引用——`github.com/<owner>/<repo>` 视为代码证据（以智能体自身 git/GitHub CLI 凭据只读克隆、在 DESIGN.md 记录哪些仓库事实支撑了哪些决定，克隆失败须声明而非臆测），其余链接仅为风格参考、不得当代码读；新增 `local_path` 引用种类——服务端只校验绝对路径（POSIX/UNC/盘符，复用 project_resource 的 isAbsoluteLocalPath）并原样冻结、不触文件系统，prompt 告知智能体该目录在执行机上按克隆仓库同等对待；桌面端「关联本地代码」复用既有 pickDirectory IPC 选文件夹生成该引用（共享校验器要求可写，此处只读目录放行），web 端保留说明文案。
- 2026-08-20 首次生成落地页修正：独立详情页此前无条件渲染画布，首次生成期间唯一可见的是空 UI Kit 的兜底文案「UI Kit 暂时不可用」，读起来像失败（用户验收截图）。对照上游（创建后落到带抽取对话流与实时预览的工作台），详情页改为与项目工作台相同的三分支：有内容或 draft/saved → 画布（调整中旧内容持续可见）；generating 或有活动任务且无内容 → 生成中视图（任务活动证据 + 「查看执行过程」实时 transcript + 完成后自动展示的说明），详情查询在任务活动期间以 4s 轮询兜底（完成时刻已有 `project_design_system:changed` 实时失效）；无内容且无任务 → 「生成未完成」视图，展示真实失败原因并可选智能体后重新生成（复用冻结的 input_snapshot）。transcript 复用 `TranscriptButton`（仅依赖 task id 的适配器）。
- 边界：tweaks 与 critique 的真实产出仍待 A6 真实智能体验收；社区缺少 Open Design 未提供的搜索/排序不在本条范围。
- 证据：`designsystemcatalogue` 与 `handler/design_system_catalogue_test.go`、`design_recipe_gallery.test.tsx`、`design-system-library.test.tsx`、`designdocument/critique_test.go`、`project_design_system_prompt_contract_test.go`、`design-document-critique.test.tsx`、`project-design-system-create.test.tsx`、`handler/design_document_attachments_test.go`、`design-task-composer.test.tsx`、`handler/design_document_revision_test.go`。

### DC-059 设计文档任务上下文必须可被守护进程领取并声明取证模式

- 状态：`confirmed`
- 日期：2026-08-19
- 问题：合并 kyle 的仓库取证引擎后，守护进程与 claim 端点按 `execution_ready: true` 与 `input.repository_grounding`（`pending` / `unavailable` / `pinned`）读取设计文档任务上下文，而服务端 `service.DesignDocumentTaskContext` 从未写这两个字段：claim 对每个设计文档任务返回 409 `error_design_document_context`，任务永远领不走；即便领走，`prepareDesignDocumentGrounding` 也会以 `invalid Design Document grounding context` 阻塞。这与 DC-055 的“两侧独立规定、无测试跨越边界”同源，也是 2026-08-18 那个“生成中一下午”的文档的真实成因之一。
- 决策：服务端上下文新增 `execution_ready` 与 `input{schema_version, repository_grounding, repository?}`：首次生成选了仓库为 `pending`、未选为 `unavailable`；调整为 `pinned` 并携带一份合法的取证回执（当前为显式 `unavailable` 回执，说明调整不重读仓库、以不可变基线为据）。claim 按 DC-053 只把文档绑定的那一个仓库交给守护进程（未选仓库则不交任何仓库，避免无关仓库不可达导致阻塞）。prompt 按模式说明：`pending` 时告知 checkout 位置并要求写回 `work/repository-grounding.json`（守护进程按 checkout 逐文件校验 SHA-256）；`pinned` 与 `unavailable` 分别给出不得声称读过代码的声明。
- 边界：取证回执仍只在守护进程本地形成并随 `design_document_grounding` 上报，服务端尚未按修订持久化（需要新的迁移与产品决策）；A6 真实智能体验收仍待完成。
- 证据：`handler/design_document_binding_test.go`（`TestDesignDocumentContextsAreExecutionReadyWithAGroundingEnvelope`）、`handler/daemon_test.go`（claim 仓库范围）、`daemon/project_design_system_prompt_contract_test.go`（按模式的 prompt）。

### DC-060 设计体系与提示词模版是工作区级平台物料，设计稿按项目归属

- 状态：`confirmed`（方向）
- 日期：2026-08-20
- 依据：用户明确表述——设计体系、提示词模版等应被看做工作区（团队空间）的平台物料，供所有项目参考；只有首页产生设计稿时需要绑定项目，以表明是哪个项目的设计稿。
- 含义：给团队空间贡献一套设计体系不要求先有项目；体系与模版的创建、维护、浏览都在工作区层面成立。设计稿（design document）保持项目归属，是唯一必须声明项目的产物。
- 现状核对：设计稿已符合——`design_document.project_id` 为 `NOT NULL`（迁移 880），首页发起时选择项目；设计体系自迁移 899（`899_standalone_design_systems`）起支持 `project_id` 为空的工作区级创建，独立创建页 `/{slug}/designs/systems/new` 即此路径，产物进入库的「团队」范围；社区提示词模版本就不属于任何项目。项目绑定体系（DC-052 的每项目一套）仍然存在且项目工作台继续可用。
- 2026-08-20 落地（首页对齐 Open Design）：上游首页 composer 有「设计体系」选择器，可为本次生成指定任意体系；我们此前只有「仓库 → 项目」自动回退，工作区级体系与官方目录都用不上——这与本条的定位矛盾。现已打通：`POST /api/design-documents` 接受互斥的 `design_system_id`（工作区任一已保存体系，含独立体系）或 `builtin_design_system`（内置目录 slug），二者同传返回 `design_system_ambiguous`；解析器新增两条来源——`cloud_saved_workspace_design_system`（按 id 加载、只校验工作区归属与已保存包，显式选择不回退，不可用即报错而非静默改用项目体系）与 `builtin_catalogue_design_system`（目录只有 DESIGN.md + tokens.css、没有通过 `projectdesignsystem.Validate` 的组件包，因此内联进 `design_context.builtin` 并按内容取 sha256 摘要钉住字节，绝不伪装成平台已校验的包）。选择随冻结输入保存，重新生成沿用。任务 prompt 按 `design_context.source` 分别说明四种情形（项目/仓库自有、工作区显式指定、内置目录内联但不得复制品牌身份、none 不得声称受体系约束）。首页 composer 增加「设计体系」pill（你的体系 + 官方预设，可搜索），底部说明文案按是否指定分别措辞。
- 2026-08-20 续（首页与工作台对齐 Open Design 的第二批）：①设计稿工作台新增「预览/代码」切换——修订响应携带包内 artifact index（`files`，永不为 null），代码视图经预览能力路由逐文件读取（文本内联展示、图片直渲、二进制与超过 1.5MB 仅下载、逐文件下载），这也是上游「本轮产出的文件」清单的对应物；预览侧新增缩放（50–150%，transform 包裹保持内部布局宽度），设备宽度切换此前已存在。上游的「分享」未迁移：我们的预览能力令牌 30 分钟过期，复制链接会在半小时后失效、名不副实，持久分享路由列为后续切片。②首页 composer 增加上游的三模式选择器——设计（默认，创建设计稿）、提问（把提示词交给智能体对话，不产出设计稿）、规划（同样走对话，但包裹规划指令：目标/页面清单/关键流程/状态与边界/开放问题，并要求产出可直接粘贴回需求描述的最终版本——规划到生成的结构化交接列为后续切片）；提问/规划复用既有 chat（createChatSession + sendChatMessage + 跳转会话），不需要项目，设计专属的项目/仓库/任务/设计体系/平台 pill 在这两个模式下隐藏；场景芯片、社区配方、从 Figma 导入都会切回设计模式。③「参考文件」chip 收进上游式「+」菜单（附加文件、从 Figma 导入）；提交按钮改为上游的圆形 ↑（无障碍名随模式变化）；「UI Mockup」芯片改名「原型」；示例提示词区保持上游的单行横滑（网格版被用户否决），复用社区卡的实时预览、分类 pill 超过 6 个折入「更多」。④用户裁定：平台选择（Web/移动端/跨端）从首页 composer 与社区「直接创建」弹层移除——设备切换属于设计稿预览页；目标平台改由场景推导（移动应用场景 → mobile，其余 → web，目录配方用其自带的 platform 建议值），服务端字段与库页的平台标注不变。上游 + 菜单的「引用其它项目 / 插件 / 连接器 / MCP」不迁移：分别没有跨项目文件引用种类与对应的插件/连接器体系，智能体的能力由其自身技能与 MCP 配置决定。
- 待办（后续切片，本条不改变已实现路径）：项目「设计体系」子 tab 与工作区库的关系收敛（项目引用工作区体系，还是项目专属体系并存）；既有项目绑定体系是否迁移为工作区级；复制端点的 standalone 目标支持；设计稿预览的持久分享路由；规划对话到设计生成的结构化交接。
- 证据：`server/migrations/880_design_document.up.sql`、`server/migrations/899_standalone_design_systems.up.sql`、`packages/views/designs/workspace-design-system-create.tsx`。

### DC-061 首页 composer 的打字机 placeholder 采用原生属性驱动

- 状态：`confirmed`
- 日期：2026-08-21
- 依据：用户要求首页输入框的 placeholder 具备打字机效果。上游实现为 Open Design 的 `apps/web/src/components/home-hero/placeholderScenarios.ts` 与 `PlaceholderCarousel.tsx`（type → hold → delete → next 状态机，42/22/1900/320ms）。
- 决策：状态机按原语义移植到 `packages/views/designs/typewriter-placeholder.ts`（纯函数 `advanceTypewriter` + `useTypewriterPlaceholder`），但**驱动 textarea 的原生 `placeholder` 属性而非上游的覆盖层 div**——上游需要覆盖层是因为要在 Lexical 编辑器上画闪烁光标 span，原生属性无需对齐与指针事件处理。焦点暂停语义取整句：聚焦且为空时冻结并显示**当前完整例句**（半删片段读起来像损坏文案；上游则整个隐藏覆盖层，OD issue #118 的动机——光标不得压在移动文字上）。`prefers-reduced-motion` 降级为整句轮换。规划/提问模式与已输入状态回落到静态文案 `STATIC_BRIEF_PLACEHOLDER`。
- 明确不迁移：上游把场景绑定到「空提交 → 建模板」的行为。我们的轮换纯属提示，提交仍要求输入需求描述。
- 证据：`packages/views/designs/typewriter-placeholder.test.ts`（状态机矩阵，node 环境）、`packages/views/designs/design-task-composer.test.tsx`（typing/冻结/回落接线，伪计时器逐 act 步进）。

### DC-062 设计稿工作台以「可交付给实现智能体」为完成标准

- 状态：`confirmed`（目标）
- 日期：2026-08-22
- 依据：用户明确表述——目标是 UI 设计师在 Multica 从首页输入提示词、产出设计稿、预览、微调、修改能完整走完，**一直到交付给 Multica 系统中的其他智能体用于实现真实页面**。据此对首页提交后的整条路由链路（`POST /api/design-documents` → `/{slug}/designs/documents/{id}`）逐段核对了 Open Design 基线 `open-design-v0.19.2`。
- 调研结论（2026-08-22）：
  - **已对齐**：提交后直接进入工作台；生成中的证据卡、执行过程转录与停止；预览的多页签、视口（含平板）、缩放、重载、新标签、全屏；预览/代码切换与逐文件读取下载；版本时间线、草稿/已保存徽标、回退；调整（范围、预设、智能体选择、⌘Enter）；运行中排队调整；零修订死路的重新生成；设计评审面板（此项上游没有）。
  - **本次新增**：静态画布（单文件内联 + `blob:` + `sandbox="allow-same-origin"`）与「标注 → 智能体」，对应上游的 draw / mark / boxSelect。
  - **仍缺且必须补**：① 交付给实现智能体——`design_delivery` 是旧 Figma 设计文件链路，与 Design Document 无关；Issue 任务的 claim 上下文与 prompt 中完全没有设计稿，saved 修订没有任何下游消费者，设计到此为止。② 手动编辑面板（直接改字体/颜色/布局）。③ 截图发对话。④ 演示模式与演讲者备注。⑤ PNG/PDF/PPTX 导出（ZIP 已有）。⑥ 存为模板。⑦ 单文件发布与持久分享。⑧ 重命名与改挂 Issue（`PATCH` 端点不存在）。⑨ 预览/代码分栏。
  - **明确不迁移**：Vercel / Cloudflare Pages 部署（Multica 不承担托管）；OD Studio 的终端、旁路会话与文件树工作区（DC-048 已裁定 Studio 本体不迁移）；逐文件版本管理（我们的版本是文档级不可变修订，语义更强）。
- 决策：迁移顺序按「解除死路 → 精修能力 → 分发能力」排列，依次为 ①交付 → ②手动编辑 → ③导出与截图 → ④演示 → ⑤分享发布 → ⑥模板 → ⑦重命名/改挂 → ⑧分栏。交付排第一，因为在它落地之前整条链路的产物无人消费，其余功能都只是让一份没有出口的设计稿更好看。
- 交付的边界（本条确认，实施细节见后续证据）：只交付 `saved` 修订（草稿不是承诺，P-011/DC-034）；交付建立 Design Document 与 Issue 的可追溯关联并在 Issue 上留下可审计记录；实现智能体在任务工作区内拿到的是经过 Audit 与 Preview 门禁的那个包本身，不是一个它打不开的链接；交付不自动改变 Issue 状态（DC-045）。
- 迁移清单（依次实施，完成即回填证据）：
  1. **交付给实现智能体** — `confirmed`，已完成（2026-08-22）。
  2. **手动编辑面板**（直接改字体/颜色/布局）— `confirmed`，已完成（2026-08-22）。
  3. **导出 PNG / PDF / PPTX 与截图** — `confirmed`，已完成（2026-08-22）。
  4. **演示模式与演讲者备注**。
  5. **持久分享与单文件发布** — `confirmed`，服务端与公开页已完成（2026-08-22，本会话交付）；工作台分享入口尚未接线（`design-document-actions.tsx` 由并行会话 `multica-95` 认领，接线前需先协调）。2026-08-22 曾计划移交并行会话 `design-center-three-tab-b2`（理由：主要是服务端的路由、能力令牌与吊销模型，紧邻该会话已在做的删除/生命周期链路），该会话未接手，最终由本会话实施——此处原记为「由 b2 交付」，2026-08-22 经会话记录核对后更正。产物复用本会话已完成的 `packages/views/designs/inline-prototype.ts`（`inlinePrototypePage` 把一页连同全部资源内联成自包含 HTML）。界线：**导出成单文件下载**属第 3 项（纯客户端），**发布到长期 URL**属本项（服务端）。
  6. **存为模板**。
  7. **重命名与改挂 Issue**。
  8. **预览/代码分栏**。
- 证据（第 3 项）：导出与截图全部在客户端完成，复用静态画布的自包含文档——离开工作台的就是工作台显示的那一份，不需要服务端。光栅化用 `foreignObject` + canvas：只有在文档已经全资源内联（data URI）时才成立，因为 `<img>` 载入的 SVG 不允许取网络资源，也正因如此 canvas 不会被污染、`toBlob` 不会抛。范围按格式语义固定，不弹配置：**图片与单页 HTML 导当前页，PDF 与 PPTX 导全部页**。PDF 用 JPEG + `/DCTDecode`（PDF 唯一原样嵌入的图像编码，换 PNG 就要解码再 deflate，代码更多、文件更大、还多一种打不开的可能）；PPTX 是自写的最小 OOXML 包 + 自写 ZIP（仓库无 zip 依赖，全部 STORE 不压缩——幻灯片主体是 PNG，压不动，而错误的 deflate 流会让 PowerPoint 直接拒绝打开）。页面尺寸按各自像素而非纸张/16:9，移动端长页保持长页。截图优先写剪贴板、失败回落下载。**明确不做「截图发对话」**：标注功能已经把选择器和覆盖元素直接交给智能体，比一张图信息量大；截图的真正用途是发给人，所以出口是剪贴板与文件。测试：`zip-writer.test.ts`（字节布局、CRC 标准向量、目录偏移、UTF-8 名、确定性）、`export-pdf.test.ts`（按 reader 的方式走 xref 偏移、DCTDecode 原样嵌入、页面尺寸）、`export-pptx.test.ts`（content types 覆盖每个 part、**所有 .rels 的 Target 都能解析到真实存在的 part**、媒体与幻灯片对应、XML 转义）。
- 证据（第 2 项）：手动编辑是唯一没有智能体参与的设计文档操作。`POST /api/design-documents/{id}/manual-edit` 校验编辑集（属性白名单 + 值/选择器不得逃逸出规则）后入队 `manual_edit` 任务，钉住当前 base 修订、沿用 pinned 取证；守护进程在原本启动智能体的位置改为**确定性应用**——读回只读 base、`designdocument.ApplyManualEdits` 生成每页一份 `prototype/manual-edits/<page>.css` 并注入 `<link>`、整包写入 `$MULTICA_OUTPUT_DIR`，随后**完全复用**既有收尾链路（收集 → 静态 Audit → Chrome 预览门禁 → 上传 → 新修订 → draft 移动）。即：跳过的是作者，不是校验；一次把页面改瞎的覆盖同样会被门禁拒绝。覆盖写进独立样式表而非改写智能体的规则，是为了让「人改了什么」始终可读，底下的设计保持原样。迁移 900 放宽了 `active_operation` 的 CHECK。测试：`designdocument/manual_edit_test.go`（注入安全矩阵、确定性、链接不累积、清除语义）、`daemon/design_document_manual_edit_test.go`（整包落盘、坏编辑集失败、只有 manual_edit 跳过智能体）、`handler/design_document_manual_edit_test.go`（上下文契约、白名单拒绝、陈旧 base 冲突）、`views/manual-edit-model.test.ts`（待应用编辑集矩阵）。
- 证据（第 1 项）：`POST /api/design-documents/{id}/deliver` 只接受已保存修订并在 Issue 上留系统评论；claim 侧由 `designDeliveryContextForIssue` 按文档自身的 saved 指针解析出 `design_delivery_context`；守护进程经 `GET /api/daemon/tasks/{taskId}/design-delivery/archive` 取包、按钉住的 digest 全量复验后以只读解包到 `.agent_context/design_delivery/package/`；两侧的 wire schema 由跨边界测试锁定（DC-059 的同类漂移）。测试：`handler/design_document_deliver_test.go`（只交付 saved、跨项目拒绝、取消交付、未交付不产生上下文）、`handler/design_delivery_binding_test.go`（schema 一致、信封字段、prompt 必须声明取证模式且禁止照抄原型）。
- 证据（第 5 项）：链接与字节分权。令牌是永不过期的原始随机串（`mds_` 前缀），唯一死法是吊销；公开交换 `GET /api/design-shares/{token}` 每次访问签发一张 30 分钟预览能力（复用 preview capability），`Cache-Control: no-store`，字节路由只认能力不认会话——泄露的令牌最多买到一次访问的档案。一个修订至多一条活链接：创建幂等（201 新建 / 200 返回既有；partial unique index + `isUniqueViolation` 把并发创建收敛到先到者）；令牌按原始值返回，重取链接拿到的就是创建者持有的那个。吊销是终态：列表不显示已吊销链接、二次吊销 404。所有死链（未知 / 已吊销 / 文档已删 / manifest 腐烂）返回**字节相同**的 404，访问者无从分辨踩中哪一种。草稿不可分享（409 `share_draft_revision`）：保存只复制 saved 指针不清 draft 指针，可分享性按「是当前 draft 且从未保存」判定，历史修订在换草稿后仍可分享。公开页 `/shares/{token}`（web 侧匿名路由）：交换 → `inlinePrototypePage` 内联自包含文档 → `sandbox="allow-scripts"`（不给 same-origin）的 opaque-origin iframe；页内导航经注入的 capture-phase 桥 postMessage 回父页并校验 `event.source`。会话侧端点：`POST /api/design-documents/{id}/revisions/{revisionId}/share`、`GET /api/design-documents/{id}/shares`、`DELETE /api/design-documents/{id}/shares/{shareId}`。迁移 901–903（表 + 两个并发索引）；902/903 注册进 cmd/migrate 的 invalid-index 清理钩子；`design_document_share` 进工作区删除清单并在 `DeleteWorkspaceDesignDependents` 增加 CTE 真删。测试：`handler/design_document_share_test.go`（创建幂等与一次吊销、草稿/越权/坏 id 拒绝矩阵、交换能力真实可用且文件路由按原样接受、四类死链同体 404）、core schema tests（畸形响应回落）。

### DC-063 设计稿生成首次真实跑通：门禁契约漂移修复与任务内预检命令

- 状态：`confirmed`
- 日期：2026-09-03
- 依据：用户明确表述"到现在为止都没有在设计页面成功运行一次设计稿生成"，要求实现并跑通。DC-055、DC-057、DC-059 已各修一处两侧独立规定的契约漂移；本条是同一类问题的第四、第五处，加上一个此前没人做过的真实运行。
- 问题（按 2026-09-01 任务 `01a05c8d-728f-75b8-8069-51b871427bd8` 的真实证据，智能体工作 11 分钟产出完整包后被拒）：
  1. `service.builtinDesignContextDigest` 输出裸 hex，而 `designdocument.validateBinding` 要求 `sha256:<hex>` 引用（与已保存体系的 manifest 摘要同形）。首页选了官方目录体系的每一次运行都在守护进程收集阶段以 `design_document_collect_failed: … invalid project design system digest` 结束；服务端完成阶段的 `ValidateArchive` 会以同一理由再拒一次。
  2. 模板残留审计扫描 `coverage.json` 本身，而 prompt 要求智能体在 `template_residue.findings` 里说明没有占位文本——"No lorem ipsum … remain." 即被判 `template_residue_detected`。
  3. 智能体在任务内没有任何办法运行平台门禁：唯一一次审计在智能体退出后执行，`window.location.search` 这类 prompt 没提到的规则直接终结任务，没有第二次机会；prompt 只列了三条"易踩规则"。
  4. Codex 通过 `/bin/zsh -lc` 执行命令，macOS `path_helper` 把 `/usr/local/bin` 排回 PATH 前面，守护进程前置的自身目录被 2026-08-05 的旧安装（0.4.18）遮蔽，智能体报告"documented command unavailable"，靠猜到桌面包路径才跑通。
- 决策与实现：
  - 内置目录体系摘要改为 `sha256:<hex>`；handler `TestCreateDesignDocumentPinsABuiltinDesignSystem` 把服务端从任务上下文导出的 binding 交给真实 `CollectDirectory`，跨边界锁死（`server/internal/service/design_context_resolver.go`、`handler/design_document_design_system_test.go`）。
  - 模板残留扫描不再包含 `coverage` 角色（`designdocument/audit.go`，`TestAuditAcceptsResidueMarkersNamedByTheCoverageSelfReport`）。
  - 新增 `multica design audit`（`cmd/multica/cmd_design.go` → `daemon.PreflightDesignDocumentPackage`）：与 finalize 门禁同一份收集、静态审计、loopback 预览服务与 Chromium 校验，默认读取 `$MULTICA_OUTPUT_DIR`、`.agent_context/design_document/context/task.json`、`$MULTICA_TASK_ID`，失败时退出码 1 并列出**每一条**规则与文件（任务评论只带第一条）。prompt 第 7 步要求 PASS 后才可结束；"易踩规则"补导航成员（`location`/`open`/`opener`/`top`/`parent`/`frames`/`document.write`）与 brief `entry` 必须以 `prototype/` 开头。
  - 守护进程向智能体环境注入 `MULTICA_CLI`（自身二进制绝对路径，agent 级 custom env 不能覆盖），prompt 改为 `"$MULTICA_CLI" design audit`（`daemon/config.go`、`daemon.go`，`TestRunTaskExportsTheDaemonCLIPath`）。
  - 文档：`apps/docs/content/docs/cli.mdx` / `cli.zh.mdx` 增加 `design audit`；缺口盘点见 [open-design-gap-2026-09-03.md](./open-design-gap-2026-09-03.md)。
- 证据：
  - 离线重放：2026-09-01 的真实产物在修复 1、2 并去掉 `window.location.search` 后，经守护进程 finalize 门禁与服务端 `ValidateArchive` 全部通过（0 诊断、1 个预览目标、真实 Chrome）；捆绑 CLI 对原产物报 `FAIL at collect`、对修复副本报 `PASS`。
  - 真实运行：2026-09-03 11:09 在桌面端对文档 `69e1fa63-b8b3-4aae-a757-6f4b35258de8` 点「重新生成」，任务 `01a0653d-fe37-7dd3-95ca-8fd1f1d7e364`（codex，15m16s，45 次工具调用）在任务内运行 audit 两次——第一次 `brief_page_entry_invalid` / `prototype_page_undeclared`（entry 写成 `index.html`），自行改为 `prototype/index.html` 后 PASS；守护进程门禁 audit passed / 0 diagnostics、preview passed / 1 target、7 files；`POST …/design-document/package` 与 `complete` 均 200；修订 `e8978ee4-bfbb-4719-a813-8f666a33bf7f`（rev 1，`sha256:2dabf6b9…`）落库，draft 指针移动，`last_error` 清空；桌面端显示 v1 草稿、可交互 12 秒循环 hero、设计评审五项 9/10（两轮）、「保存为设计稿」可用。
  - 测试：`internal/designdocument`、`internal/service`（resolver）、`cmd/multica`、`internal/daemon` 全包，`internal/handler` 的 DesignDocument / DesignSystem / DesignDelivery 子集（live PostgreSQL）。
- 边界：
  - 本次真实运行发生在 `MULTICA_CLI` 落地之前，智能体是靠自行搜索找到捆绑二进制的；守护进程已重建重启，下一次运行生效。`/usr/local/bin/multica`（0.4.18）仍是旧安装，建议更新或删除。
  - 门禁仍是一次性；守护进程侧的"审计失败→带诊断重新提示智能体"循环未做（Open Design 也没有：OD Next 禁止智能体自检，靠 deliverable validation 与重试）。`last_error` 仍只带第一条诊断，`regenerate` 不携带上次失败原因。
  - 视觉、信息架构与业务质量验收（A6）仍未做；本次产物是"animated hero"演示，只证明链路。
  - 旧文档冻结快照中的裸摘要不需要迁移：`regenerate` 与 `adjust` 都重新解析 `design_context`（已由本次运行验证）。
  - `internal/service` 的 7 个 `TestAutopilotQuota*` 因共享测试库 `multica` 缺少上游合并带来的迁移 448（`rejection_notified_at`）而失败，与本条无关。

### DC-064 设计稿工作台 A6 首轮验收修复：跨 realm 门禁 bug 与 Open Design 预览交互补齐

- 状态：`confirmed`
- 日期：2026-09-03
- 依据：用户对设计稿预览页的六项验收意见，要求对照 Open Design 修复。这是 A6 真人验收的第一批发现。
- 根因（覆盖意见 2、3 及实际上全部画布交互）：`PrototypeCanvas` 的画布文档挂在 `blob:` iframe 里，节点属于 **iframe 自己的 global object**，而全部交互守卫用的是父窗口的 `instanceof Element` / `instanceof HTMLElement`——跨 realm 恒为 false。点击、悬停、框选、跨页链接、编辑回填（`repaintManualEdits` / `applyToCanvas`）在到达任何 handler 之前就被丢弃。jsdom 单 realm 永远无法复现，所以既有单测全绿而真实点击全部无效；DC-062 记录的「标注 → 智能体」「手动编辑面板」实际上是从未被真人点击过的。
- 修复与补齐：
  1. **跨 realm 守卫**：新增 `isElementNode` / `isStyleableElement`（读 `nodeType`，跨 realm 成立），替换画布点击、悬停、`isCanvasUi` 与三处手动编辑回填的全部 `instanceof`；矩阵测试钉住（`prototype-canvas.test.tsx`）。真实桌面端验证：编辑模式点击标题出现选中框、属性面板完整弹出。
  2. **评论交互（OD comment pins）**：标注在画布上留下编号 pin（元素锚定用选择器解析、框选用矩形），点击 pin 滚动并高亮 composer 里对应条目；列表编号与画布一致；每条标注的说明输入原本就有，修复后真正可用。边界：标注仍是会话级草稿，随发送清空，不做 OD 的服务端持久评论与状态流转（需要新的存储模型，另行立项）。
  3. **截图发送到对话**：推翻 DC-062 第 3 项「明确不做截图发对话」（用户本次明确要求）。截图菜单新增「截图发送到对话」，走普通附件路由进入本轮调整（`rasterizePage` → File → `stageAttachments`），智能体按参考文件读取；「复制到剪贴板」保留。
  4. **演示模式**：工具栏新增播放按钮——整窗黑底只留活动预览（脚本运行，演示即播放），底部页码与上/下/退出控件，←/→/空格翻页、Esc 退出。
  5. **历史版本查看**：版本区新增「历史版本」对话框（对应 OD versions 弹层）：左侧版本列表（徽标、说明、页数、智能体、时间），右侧所选版本的活动预览（按修订能力令牌加载）；「查看此版本」把工作台钉到该版本，「回退到此版本」沿用既有指针语义。侧栏行的钉住查看此前已存在。
- 验证：views 全量 5499 通过（含新增演示模式/历史版本/截图三个组件测试与守卫矩阵）、typecheck 通过、lint 0 error；真实桌面端 HMR 验证编辑选中（见 1）；标注 pin 与演示/版本对话框的组件测试覆盖，真人复核因验收屏幕锁定暂缓一次。
- 第二轮验收（2026-09-03，用户对照 OD 截图反馈）：①截图与 PNG/PDF 导出在桌面端报 `Tainted canvases may not be exported`——Electron `webSecurity: false` 下 blob-URL 解码的图被画布判为跨源，光栅化改为 data-URL 优先、blob 兜底、双向重试（`export-raster.ts`）。②标注工具栏重做为 OD 的浮动深色胶囊：框选/钢笔/文字/选元素四工具 + 撤销/重做 + 「为这个标记添加说明」输入直发（钢笔笔迹入包渲染、文字标记落点成 pin、帧外松手的搁浅笔迹按时序收笔）；编辑属性面板从侧栏改为跟随选中元素的浮动面板（坐标补 iframe 偏移与 zoom 缩放、随帧滚动更新）。③多视角对抗式审查（12 个子代理）确认并修复：StrictMode 下 undo/redo 在 setState 更新器里嵌套 setState 导致重做栈重复入队（提取纯转换 `annotation-history.ts`）、ink 图层缺 left/top 落在文档末尾、窄面板工具栏被裁剪、空笔迹产生 Infinity/NaN 锚点、应用修改前置条件提取为 `editApplyBlocker` 纯函数；对应补齐画布交互（doc.open/write/close 填充帧文档）、工具栏发送/排队、撤销重做矩阵、传输决策矩阵等全部测试。views 全量 5519 通过、typecheck/lint 干净。
- 边界：OD 的评论持久化、看板模式、评论状态流转未迁（依赖服务端存储决策）；演讲者备注依赖 deck 产物形态，待 deck 可创建后随演示模式补齐。
- 第三轮验收（2026-09-03 晚，用户报告）：①「点击标注页面渲染报错」定为瞬时环境问题——19:52 并行会话在共享 checkout 切分支时把 JSON/模块写了一半，Vite `vite-json` 对半成品报 `key must be a string` 并弹出错误遮罩；磁盘 JSON 现已全部可解析，模块转换 200，实机点击标注正常打开，无代码改动。②「工具栏跟随标注框」在同一时间窗内按旧模块图渲染所致：现行代码的标注工具栏为画布面板底部的 `absolute bottom-3` 深色胶囊（`design-document-page.tsx`），实机截图证实固定于底部中央；硬刷新（⌘R）后如仍复现再查。③截图发送到对话后，侧边栏附件 chip 现按对象 URL 渲染缩略图（图片类参考文件一律生效），行被移除、随调整发出或组件卸载时均回收 URL。④侧边栏滚动容器补 `overflow-x-hidden`（与库内其它面板惯例一致）——`overflow-y-auto` 会使另一轴同样变 auto，任何超宽子元素都会带出横向滚动条。测试：页面对象 URL stub + 缩略图/回收断言，views 39 项通过、typecheck、lint 0 error。

## 下一步

新 Phase A 的首页入口、`multica.design-document/v1`、不可变 revisions、draft/saved、任务内仓库 Grounding、持续工作空间、现有本地浏览器强制门禁、任务（Issue）可选关联和 A1 至 A6 内部子切片均已确认。用户复核书面规格后，只为 A1 至 A6 编写详细实施计划；计划必须按 DC-040 限定每个子切片的产品、代码、API 和数据范围，并携带退役账本与真实验证门禁。不得恢复独立 Phase B、迁移 `feature/fengchen-fixed-v2`、继续 Open Design Worker/Runtime，或把 Slice B 至 E 混入 Phase A。

2026-08-16 补充：迁移范围已按 DC-048 收窄为 Open Design 的首页、社区和设计体系三个 tab；DC-055 的生产端契约对齐先于所有新切片；DC-052 的设计体系仓库化排在 A1 之前，因为 A2 的仓库选择和 A3 的取证边界都依赖它。当前实施顺序为 DC-055 对齐 → 设计体系仓库化 → A1 至 A6 → 工作区级体系目录 → 社区模板画廊。
