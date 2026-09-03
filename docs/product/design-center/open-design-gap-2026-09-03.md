# Open Design 迁移缺口盘点（2026-09-03）

> 状态：`evidence`
> 对照基线：Open Design `HEAD 09bd500d4`（v0.21.1 之后，2026-09-01）与 Multica 主分支 `75ecc1747` 工作树
> 方法：两份只读源码盘点（Open Design 四个面 + Multica 设计 tab 现状），逐项对照 DC-047 至 DC-062 已确认范围。只列 Multica 仍缺或只做了一半的项；已对齐项不重复。

## 1. 结论

- 三个 tab（首页 / 社区 / 设计体系）的骨架已齐，Design Document 工作台已覆盖 Open Design Studio 的核心闭环（预览、版本、调整、手动编辑、标注、导出、交付）。
- 2026-09-03 之前，这条链路**从未真实跑通**过：不是功能缺失，而是三处门禁契约漂移和一个执行环境问题（见 DC-063）。修复后同一份需求在 15 分钟内产出可预览的草稿。
- 剩余缺口集中在四类：**Open Design 0.20/0.21 的新行为**（我们的基线是 0.19.2）、**DC-062 未完成项**（演示模式、存为模板、重命名/改挂、分栏）、**接线未完成的半成品**（分享入口、我的体系、最近生成）、**产物形态**（只有 prototype 可创建）。

## 2. 首页（composer）

| 缺口 | Open Design | Multica 现状 | 建议 |
| --- | --- | --- | --- |
| 场景 chip 集合与顺序 | 0.20.1 起为 prototype, deck, image, document, hyperframes, web-clone, video, audio, live-artifact, webgl；线框图 / 移动应用降为 Prototype 的二级场景；默认选中 Prototype | 4 个可用（原型 / 幻灯片 / 文档 / 网站复刻）+ 6 个灰态；线框图 / 移动应用已是二级场景 | 已与 0.20.1 的结构一致；灰态 chip 只在对应产物契约落地后点亮，不必追顺序 |
| 「设计体系」选择器的 Auto 选项 | key 存在但无消费者 | 无 | 不迁 |
| 工作目录 / 关联本地代码 | 首页 footer 可选本地文件夹让智能体直接读 | 仅项目「仓库」pill（GitHub repo 资源）；本地目录仅在设计体系创建页（桌面端） | 后续切片：首页复用 `local_path` 引用种类 |
| 执行模型切换（本地 CLI / 云 / BYOK） | Home 顶部 `InlineModelSwitcher` | 由所选智能体决定 | 不迁（智能体模型属于智能体配置） |
| `@` 提及（tabs / 插件 / skills / MCP / 设计文件） | 有 | 无 | 后续；先做「设计文件」引用（把 Figma 导入的设计文件作为参考） |
| 「从 Figma 导入」 | `.fig` 离线解码或 Figma URL → `od-figma-migration` | 只是装填 `figma-migration` 配方，无 URL / 文件选择；附件白名单不含 `.fig` | 补文件选择（接受 `.fig`/zip）与 URL 输入，或复用既有 `design_file` 导入链路作为参考附件 |
| 最近生成 | 最近项目条：封面、所有者筛选、类型筛选、排序、多选删除、重命名 / 复制 / 删除、移动到团队空间 | 10 张卡，按 12 个项目扇出查询；无筛选 / 排序 / 批量 | 改用已存在的工作区级 `GET /api/design-documents`；筛选排序视需要 |
| 空提交 = 提交当前打字机示例 | 有 | 有意不迁（DC-061） | 维持 |
| 首次运行引导、What's new、体验问卷 | 有 | 无 | 不迁 |

## 3. 社区

| 缺口 | Open Design | Multica 现状 | 建议 |
| --- | --- | --- | --- |
| 排序（Trending / Newest，持久化） | 有 | 固定目录顺序 | 小切片：客户端排序 + 记忆 |
| Remix（复制示例产物为新项目再改） | 有（要求插件带可复制示例） | 「直接创建」只带 prompt，不复制示例文件 | 需要 Design Document 支持「以模板产物为 base」的首次生成；与「存为模板」同一模型 |
| 收藏（Saved chip） | 有 | 无 | 小切片 |
| 详情弹层的分享（社交、复制链接、导出） | 有 | 有「直接创建 / 填入首页 / 复制 prompt」 | 后续 |
| 发布社区 / 工作区模板 | Share to OpenDesign、Add to my plugins、Publish repo | 无写入端点（schema 有 `origin='workspace'`） | 与「存为模板」（DC-062 ⑥）一起做 |
| 非 prototype 产物形态（deck / image / video / hyperframes / audio / live） | 全部可创建 | 只能浏览，创建被 `recipe_mode_unsupported` 拒绝 | 按产物契约逐个点亮；deck 优先（社区 81 条、首页已有幻灯片 chip） |

## 4. 设计体系

| 缺口 | Open Design | Multica 现状 | 建议 |
| --- | --- | --- | --- |
| 「我的」范围 | Mine / Team / Official | 「我的」永远为空（payload 无作者） | 目录响应加 `created_by`，或删掉该范围 |
| 独立（工作区级）体系的编辑入口 | 列表 → 详情可编辑 | 库详情只读；无项目的体系没有进入 `/designs/systems/{id}` 的链接 | 库详情加「打开」按钮指向体系页（不限项目） |
| 删除体系 | 有（含团队权限） | 无 `DELETE /api/project-design-systems/{id}`，无 UI | 小切片（应用事务内清理 package 行与对象） |
| 复制到独立体系 | 复制来源可任意 | `copy` 要求 `project_id` | DC-060 待办 |
| Logo / 字体 / 图片资产的上传编辑、逐模块编辑 DESIGN.md、快捷键 | DesignKitView 完整编辑 | 官方体系只读展示；项目体系用智能体调整 | 维持「智能体调整」路线；Logo 上传可作为参考资料 |
| 下载 .zip + SKILLS.md、Rebuild token contract、Package audit 修复提示 | 有 | 包预览 / 校验有；无下载 | 后续 |
| 品牌 URL 程序化抽取（不经智能体） | `brands/engine` | 无（由智能体按参考链接抽取） | 不迁（P-010：由智能体负责语义理解） |

## 5. Design Document 工作台（对应 Studio）

| 缺口 | Open Design | Multica 现状 | 建议 |
| --- | --- | --- | --- |
| 演示模式 + 演讲者备注 | Present（本页 / 全屏 / 新标签）、演讲者视图、逐页备注编辑 | 无；deck 配方 prompt 要求备注但无人渲染 | DC-062 ④，与 deck 产物形态一起做 |
| 存为模板 | Share → Save as template | 无（仅旧 Figma 设计文件有 publish-template） | DC-062 ⑥ |
| 重命名 / 改挂任务 | 项目重命名、文件重命名 / 移动 | 无 `PATCH /api/design-documents/{id}`；改挂只能借交付选择器 | DC-062 ⑦：一个 PATCH 端点 + 标题内联编辑 |
| 预览 / 代码分栏 | Preview / Code 切换（非分栏） | 同为切换 | DC-062 ⑧ 可降级：Open Design 也不是分栏 |
| 分享入口接线 | Get a share link / Stop sharing | 服务端与公开页已完成，工作台没有入口；桌面端无 `/shares` 路由 | 接线：⋯ 菜单「复制分享链接 / 停止分享」 |
| 工作台内删除文档 | 有 | 只在卡片菜单 | ⋯ 菜单补「删除」 |
| 版本预览 / 从任意版本恢复 | Versions 弹层含预览与「切换到此版本」 | 版本时间线 + 回退，历史版本可查看 | 已对齐 |
| 检查面板（Inspect：颜色 / 字号 / 内边距 / 圆角，保存） | 有 | 手动编辑面板覆盖 | 已对齐 |
| 评论模式（元素 pin + 评论列表 + 发给智能体） | 有 | 标注 → 智能体（框选 / 选元素） | 基本对齐；缺「保存的评论列表 / 状态」 |
| 截图发对话 | 有 | 有意不做（DC-062 第 3 项） | 维持 |
| 手动编辑：布局（flex / grid）、图片替换、删除元素、撤销 | 有 | 属性白名单（字体 / 颜色 / 间距等） | 逐步扩白名单；删除元素与图片替换优先 |
| 交接给代码智能体（Handoff：本地编辑器、CLI prompt 选框架） | 有 | 「交付实现」把已保存修订包交给 Issue 的智能体 | 已对齐（形态不同） |
| 部署（Vercel / Cloudflare）、社交分享网格 | 有 | 有意不迁（DC-062） | 维持 |
| 终端 / 浏览器 / 旁路会话 / Sketch 标签 | 有 | 有意不迁（DC-048） | 维持 |
| 团队协作（在线状态、只读共享） | 有 | 无 | 后续 |

## 6. 生成管线（智能体侧）

| 差异 | Open Design | Multica | 说明 |
| --- | --- | --- | --- |
| 自检 | OD Next 明令禁止智能体在写完后自行预览 / 截图 / 校验，靠守护进程 deliverable validation + 失败重试 | 2026-09-03 起：智能体必须在任务内运行 `"$MULTICA_CLI" design audit`（同一收集 / 审计 / Chromium 门禁）直到 PASS | 两者相反；Multica 的门禁规则多（网络、导航、脚本 API、模板残留、brief ↔ 原型一致性），一次性门禁不给智能体反馈是历史上跑不通的主因之一，见 DC-063 |
| 失败反馈 | 结构化 run-failure 分类 + 本地化错误卡 + Retry | `last_error` 只带第一条诊断；重新生成不携带上次失败原因 | 后续：`last_error` 带完整诊断列表，regenerate prompt 附上次失败 |
| 评审循环 | Design Jury 默认关闭；`fallbackPolicy ship_best` | 五视角循环写入 `critique.json`，不决定草稿成立 | 已按 DC-050 落地 |
| 反 slop lint | `lint-artifact.ts`（`od lint`） | 无 | 可作为 audit 的 warning 级规则加入 |
| 产物契约 | 单个 `index.html`「写完即交付」 | brief / coverage / prototype 三件套 + manifest | Multica 更严格，换取可审计与可交付 |

## 7. 建议顺序

1. 分享入口接线、工作台内删除、重命名 / 改挂（都是小切片，解除现有半成品）。
2. `last_error` 完整诊断 + regenerate 携带失败原因（提高二次成功率）。
3. deck 产物形态（首页 chip、社区 81 条配方、演示模式与备注一起落地）。
4. 存为模板 → 社区 Remix → 工作区模板发布（同一模型）。
5. 设计体系库：我的范围、独立体系编辑入口、删除、复制到独立体系。
6. 首页：本地目录引用、Figma 导入的真实输入、最近生成改工作区端点。
