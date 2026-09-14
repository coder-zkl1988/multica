# Phase A 真实产品 Gate 验收记录

日期：2026-09-01

结论：**PASS**。在隔离的 Task 7 API、Web、PostgreSQL 和专用 runtime 上，使用真实 headless Chromium 完成仓库分析、仓库专属设计体系生成/保存、Design A、Design B、一次调整、Audit、Preview 和保存闭环。未使用 mock、直接数据库写入产品对象或 HTTP-only 替代 UI 验收。

## 产品闭环

- 真实 UI 创建/选择隔离 Workspace、CRM 验收 Project、`alisafyj/multica` 仓库关联和在线 Design Gate Agent。真实认证流只用于建立隔离身份，未读取或输出任何 cookie、JWT、验证码、代码、令牌或其他凭据。
- repository analysis 通过真实 UI 完成，产出实际 checkout 的固定仓库修订与仓库相对 source grounding。
- 基于该分析生成并保存仓库专属设计体系。Design A（CRM 客户列表）和 Design B（CRM 客户详情/商机流程）均从该已保存仓库体系冻结上下文创建。B 的一次真实调整以已保存基线为起点完成并再次保存。
- A、B 和 B 调整后版本均通过 Audit、可见 Preview 和保存验收。

## 固定锚点与任务耗时

- Workspace `8b6efa4b-6d23-4a2e-b3dd-8b07c26d2a8c`；Project `bed12d68-a044-408b-9b94-0b746fad423e`；Repository association `d9a0a1c7-5df9-4466-85a4-ee0c01960cf7`；Agent `51248c1f-b7cf-4837-9c44-a96055d1a579`。
- repository analysis task `01a05cd2-08b0-700f-95ce-ae786742aed8`：`completed`，19:54:23 至 19:58:39，实际运行 255 秒；固定 checkout commit `a7606af71f98`。
- repository design system `edf027ca-4385-4109-a044-042505f02de9`；saved package `71ceba5f-ff03-40ae-a126-bf931dc454fc`；生成 task `01a05ce1-9b08-7ca6-abc6-51f64bb8de99` 运行 300 秒；saved digest `sha256:5fa50a865277c5405c89f9d2023398f88489b267591518d6961230041ddc4811`。
- Design A document `f2a055e5-ef1d-473c-bf4c-ae43aeaf851c`；v1 revision `e9703f6d-8edf-4015-8c07-f0a877c4f8c1`；task `01a05d3c-efbc-7226-b8c8-0e005b1f9078` 运行 716 秒；22:09:29 保存。
- Design B document `14f2fbb0-c448-46d3-82d5-f38d6497a0b9`；v1 revision `354fbb9b-e637-469d-9c15-0c70489768dd`；task `01a05d4e-3b6a-7e25-97d1-60fc584e54bc` 运行 849 秒。
- Design B 调整 task `01a05d5c-9f87-78cd-9cdd-9648f1e74e27` 运行 300 秒；v2 revision `e8160d12-081d-4e38-b6c3-3ad7dd879ff7` 以 v1 为 base，22:31:33 保存。总调整次数：1。
- A v1、B v1、B v2 的持久化 Audit 均为 `passed=true`、0 diagnostics，Preview verification 均为 `passed=true`；三个 revision 的 `design_system_digest` 均精确等于上述 saved design system digest。
- 用户可见阻塞：无。未发现需要绕过或以 mock 替代的产品阻塞。

## 补充真实 UI 证据

- **共享仓库体系入口**：Home > 设计体系 > 新建设计体系 > 仓库绑定选择验收仓库后，解析到已保存的 `Multica CRM Design System`，不会伪造第二个创建表单。Project > 设计体系 > 该仓库范围解析到同一体系。两个入口均显示 Web 平台、已保存状态和同一套 Source of truth/Token/Component 内容；未提交任何重新生成。
- **A/B 一致性与差异场景**：两份设计详情的 DOM 均显示 `已保存`、`Audit 通过`、`已按仓库取证` 和 `cloud_saved_repository_design_system`。两个 Preview iframe 均可见且可交互，共用 Times 字体、10px 圆角和相同 surface token；标题分别为“客户”和“澄海智造”，证明两个业务场景保留独立内容而不偏离共享体系。
- **Project/Repository provenance**：Repository 范围切换状态为 active，设计体系页可见 `Status: Active`、`Evidence reviewed`、`Provenance`、`Tokens`、`Components` 和在线 UI Kit。桌面 1440x960 下，UI Kit iframe 可见（874x680）且 `elementFromPoint` 命中 iframe，证明资源相关视图不仅仅是隐藏 DOM。

## 布局与交互验证

- 真实 Preview 在 desktop `1440x960` 和受限 `500x900` viewport 下检查；外层与 iframe 均无水平溢出。两种 viewport 均验证主标题、调整后的“当前”状态可见，交互后活动/历史标签更新 `aria-selected`，编辑资料打开可见表单/抽屉。
- 对滚动至视口内的“变更历史”标签执行 `elementFromPoint`，命中标签或其后代并成功真实点击。

## 策略限制

未调用 screenshot、录屏或 browser trace API，也未将 agent 对截图的自述作为验收证据。可视信号仅来自真实 Chromium 的 DOM、accessibility、computed layout、visibility、`elementFromPoint` 和真实交互状态。

## 已发现并修复的实际缺陷

真实链路依次暴露了跨域 bearer、仓库 checkout、V2 package binding、仓库体系 provenance、prompt contract、输出根、JS audit 和显式 revision binding 问题。每个缺陷都以失败测试/定向验证、影响面检查和独立聚焦提交修复；最终工作树不含临时上传诊断或生成的 Next 类型漂移。
