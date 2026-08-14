package main

// productMapCatalogNode is an AI-derived product capability node. SourcePaths
// keep every generated node traceable to the code boundary used to infer it.
type productMapCatalogNode struct {
	Name        string
	Slug        string
	Description string
	SourcePaths []string
	Children    []productMapCatalogNode
}

var multicaProductCatalog = []productMapCatalogNode{
	{
		Name: "项目与任务协作", Slug: "multica-work-management",
		Description: "围绕项目、任务、个人待办和收件箱组织团队研发工作。",
		SourcePaths: []string{"apps/web/app/[workspaceSlug]/(dashboard)/projects", "apps/web/app/[workspaceSlug]/(dashboard)/issues", "apps/web/app/[workspaceSlug]/(dashboard)/my-issues", "apps/web/app/[workspaceSlug]/(dashboard)/inbox"},
		Children: []productMapCatalogNode{
			{Name: "项目", Slug: "multica-projects", Description: "项目列表与项目详情。", SourcePaths: []string{"apps/web/app/[workspaceSlug]/(dashboard)/projects"}},
			{Name: "任务", Slug: "multica-issues", Description: "任务列表、详情和状态协作。", SourcePaths: []string{"apps/web/app/[workspaceSlug]/(dashboard)/issues"}},
			{Name: "我的任务", Slug: "multica-my-issues", Description: "当前成员负责和关注的任务视图。", SourcePaths: []string{"apps/web/app/[workspaceSlug]/(dashboard)/my-issues"}},
			{Name: "收件箱", Slug: "multica-inbox", Description: "集中处理协作通知与待办。", SourcePaths: []string{"apps/web/app/[workspaceSlug]/(dashboard)/inbox"}},
		},
	},
	{
		Name: "AI 智能体能力", Slug: "multica-ai-workforce",
		Description: "创建和组织智能体、团队及可复用 skill。",
		SourcePaths: []string{"apps/web/app/[workspaceSlug]/(dashboard)/agents", "apps/web/app/[workspaceSlug]/(dashboard)/squads", "apps/web/app/[workspaceSlug]/(dashboard)/skills"},
		Children: []productMapCatalogNode{
			{Name: "智能体", Slug: "multica-agents", Description: "智能体创建、配置与详情管理。", SourcePaths: []string{"apps/web/app/[workspaceSlug]/(dashboard)/agents"}},
			{Name: "智能体团队", Slug: "multica-squads", Description: "按协作目标编排多个智能体。", SourcePaths: []string{"apps/web/app/[workspaceSlug]/(dashboard)/squads"}},
			{Name: "Skills", Slug: "multica-skills", Description: "管理智能体可复用的执行能力。", SourcePaths: []string{"apps/web/app/[workspaceSlug]/(dashboard)/skills"}},
		},
	},
	{
		Name: "自动化与运行时", Slug: "multica-automation-runtime",
		Description: "按规则自动执行任务，并管理智能体运行环境。",
		SourcePaths: []string{"apps/web/app/[workspaceSlug]/(dashboard)/autopilots", "apps/web/app/[workspaceSlug]/(dashboard)/runtimes"},
		Children: []productMapCatalogNode{
			{Name: "自动化", Slug: "multica-autopilots", Description: "周期性或条件触发的自动执行。", SourcePaths: []string{"apps/web/app/[workspaceSlug]/(dashboard)/autopilots"}},
			{Name: "运行时", Slug: "multica-runtimes", Description: "运行机器、执行器和环境配置。", SourcePaths: []string{"apps/web/app/[workspaceSlug]/(dashboard)/runtimes"}},
		},
	},
	{
		Name: "质量与测试", Slug: "multica-quality",
		Description: "从测试用例、计划、任务到运行结果的质量闭环。",
		SourcePaths: []string{"apps/web/app/[workspaceSlug]/(dashboard)/tests"},
		Children: []productMapCatalogNode{
			{Name: "测试用例", Slug: "multica-test-cases", Description: "测试用例库与详情。", SourcePaths: []string{"apps/web/app/[workspaceSlug]/(dashboard)/tests"}},
			{Name: "测试计划", Slug: "multica-test-plans", Description: "组织批量测试范围与执行策略。", SourcePaths: []string{"apps/web/app/[workspaceSlug]/(dashboard)/tests/plans"}},
			{Name: "测试执行", Slug: "multica-test-runs", Description: "测试任务、运行过程和结果。", SourcePaths: []string{"apps/web/app/[workspaceSlug]/(dashboard)/tests/jobs", "apps/web/app/[workspaceSlug]/(dashboard)/tests/runs"}},
		},
	},
	{
		Name: "设计中心", Slug: "multica-design-center",
		Description: "设计稿、设计系统、画布帧与恢复任务的一体化工作区。",
		SourcePaths: []string{"apps/web/app/[workspaceSlug]/(dashboard)/designs"},
		Children: []productMapCatalogNode{
			{Name: "设计资产", Slug: "multica-design-assets", Description: "设计列表、详情与草稿。", SourcePaths: []string{"apps/web/app/[workspaceSlug]/(dashboard)/designs", "apps/web/app/[workspaceSlug]/(dashboard)/designs/drafts"}},
			{Name: "设计系统", Slug: "multica-design-systems", Description: "可复用的设计规范与组件资产。", SourcePaths: []string{"apps/web/app/[workspaceSlug]/(dashboard)/designs/systems"}},
			{Name: "画布与恢复", Slug: "multica-design-frames", Description: "帧级查看和设计恢复任务。", SourcePaths: []string{"apps/web/app/[workspaceSlug]/(dashboard)/designs/[id]/frames", "apps/web/app/[workspaceSlug]/(dashboard)/designs/restore-tasks"}},
		},
	},
	{
		Name: "产品与发布治理", Slug: "multica-product-governance",
		Description: "产品地图、PMO 发布状态和产品追溯能力。",
		SourcePaths: []string{"apps/web/app/[workspaceSlug]/(dashboard)/products", "apps/web/app/[workspaceSlug]/(dashboard)/pmo"},
		Children: []productMapCatalogNode{
			{Name: "产品地图", Slug: "multica-product-map", Description: "查看产品、功能模块、代码证据和任务追溯关系。", SourcePaths: []string{"apps/web/app/[workspaceSlug]/(dashboard)/products", "packages/views/products"}},
			{Name: "PMO 发布状态", Slug: "multica-pmo", Description: "管理发布配置和可信上线状态。", SourcePaths: []string{"apps/web/app/[workspaceSlug]/(dashboard)/pmo"}},
		},
	},
	{
		Name: "组织与平台治理", Slug: "multica-platform-governance",
		Description: "成员、工作区设置、用量和计费管理。",
		SourcePaths: []string{"apps/web/app/[workspaceSlug]/(dashboard)/members", "apps/web/app/[workspaceSlug]/(dashboard)/settings", "apps/web/app/[workspaceSlug]/(dashboard)/usage", "apps/web/app/[workspaceSlug]/(dashboard)/billing"},
		Children: []productMapCatalogNode{
			{Name: "成员管理", Slug: "multica-members", Description: "成员详情、权限和协作关系。", SourcePaths: []string{"apps/web/app/[workspaceSlug]/(dashboard)/members"}},
			{Name: "工作区设置", Slug: "multica-settings", Description: "工作区基础配置与集成设置。", SourcePaths: []string{"apps/web/app/[workspaceSlug]/(dashboard)/settings"}},
			{Name: "用量与计费", Slug: "multica-usage-billing", Description: "平台用量和计费信息。", SourcePaths: []string{"apps/web/app/[workspaceSlug]/(dashboard)/usage", "apps/web/app/[workspaceSlug]/(dashboard)/billing"}},
		},
	},
}

var yuanwuProductCatalog = []productMapCatalogNode{
	{
		Name: "获客与线索运营", Slug: "yuanwu-acquisition",
		Description: "从活动获客、线索沉淀到分派和私域运营。",
		SourcePaths: []string{"src/router/modules/activity.js", "src/router/modules/clue.js", "src/router/modules/dispatch.js", "src/router/modules/private-domain.js"},
		Children: []productMapCatalogNode{
			{Name: "活动管理", Slug: "yuanwu-activity", Description: "营销活动配置与执行入口。", SourcePaths: []string{"src/router/modules/activity.js"}},
			{Name: "线索管理", Slug: "yuanwu-clue", Description: "线索查询、跟进与转化管理。", SourcePaths: []string{"src/router/modules/clue.js"}},
			{Name: "线索分派", Slug: "yuanwu-dispatch", Description: "线索分配和流转规则。", SourcePaths: []string{"src/router/modules/dispatch.js"}},
			{Name: "私域运营", Slug: "yuanwu-private-domain", Description: "私域客户运营与触达。", SourcePaths: []string{"src/router/modules/private-domain.js"}},
		},
	},
	{
		Name: "咨询与客户管理", Slug: "yuanwu-consultation-customer",
		Description: "覆盖咨询接待、客户档案、会话和服务质检。",
		SourcePaths: []string{"src/router/modules/consult.js", "src/router/modules/consulting-digital.js", "src/router/modules/customer.js", "src/router/modules/customerManage.js", "src/router/modules/chat.js", "src/router/modules/recording.js", "src/router/modules/voiceCheck.js"},
		Children: []productMapCatalogNode{
			{Name: "咨询管理", Slug: "yuanwu-consult", Description: "咨询接待、咨询记录和数字化咨询。", SourcePaths: []string{"src/router/modules/consult.js", "src/router/modules/consulting-digital.js"}},
			{Name: "客户档案", Slug: "yuanwu-customer", Description: "客户信息、标签和客户关系管理。", SourcePaths: []string{"src/router/modules/customer.js", "src/router/modules/customerManage.js"}},
			{Name: "在线会话", Slug: "yuanwu-chat", Description: "咨询会话与消息协同。", SourcePaths: []string{"src/router/modules/chat.js"}},
			{Name: "录音与质检", Slug: "yuanwu-quality-inspection", Description: "咨询录音、语音核验和服务质量检查。", SourcePaths: []string{"src/router/modules/recording.js", "src/router/modules/voiceCheck.js"}},
		},
	},
	{
		Name: "预约与服务履约", Slug: "yuanwu-appointment-service",
		Description: "客户预约、到院看板和标准服务流程。",
		SourcePaths: []string{"src/router/modules/userReserve.js", "src/router/modules/appt-dashboard.js", "src/router/modules/serviceflow.js"},
		Children: []productMapCatalogNode{
			{Name: "客户预约", Slug: "yuanwu-reservation", Description: "预约创建、调整和查询。", SourcePaths: []string{"src/router/modules/userReserve.js"}},
			{Name: "预约看板", Slug: "yuanwu-appointment-dashboard", Description: "到院与预约履约数据看板。", SourcePaths: []string{"src/router/modules/appt-dashboard.js"}},
			{Name: "服务流程", Slug: "yuanwu-service-flow", Description: "院内服务流程配置和节点跟踪。", SourcePaths: []string{"src/router/modules/serviceflow.js"}},
		},
	},
	{
		Name: "订单与医疗服务", Slug: "yuanwu-order-medical",
		Description: "订单交易、主订单、病历和医生资质管理。",
		SourcePaths: []string{"src/router/modules/order.js", "src/router/modules/master-order.js", "src/router/modules/medical.js", "src/router/modules/case-history-manager.js", "src/router/modules/doctorAuth.js"},
		Children: []productMapCatalogNode{
			{Name: "订单管理", Slug: "yuanwu-orders", Description: "订单查询、处理和主订单归集。", SourcePaths: []string{"src/router/modules/order.js", "src/router/modules/master-order.js"}},
			{Name: "医疗业务", Slug: "yuanwu-medical", Description: "医疗服务业务和就诊信息。", SourcePaths: []string{"src/router/modules/medical.js"}},
			{Name: "病历管理", Slug: "yuanwu-case-history", Description: "客户病历和历史记录管理。", SourcePaths: []string{"src/router/modules/case-history-manager.js"}},
			{Name: "医生认证", Slug: "yuanwu-doctor-auth", Description: "医生资料和认证状态管理。", SourcePaths: []string{"src/router/modules/doctorAuth.js"}},
		},
	},
	{
		Name: "商品与内容资源", Slug: "yuanwu-goods-content",
		Description: "商品、内容素材、案例模板和对比照片管理。",
		SourcePaths: []string{"src/router/modules/goods.js", "src/router/modules/content.js", "src/router/modules/resource.js", "src/router/modules/template.js", "src/router/modules/contrast-photo.js"},
		Children: []productMapCatalogNode{
			{Name: "商品管理", Slug: "yuanwu-goods", Description: "商品资料、上下架和服务商品配置。", SourcePaths: []string{"src/router/modules/goods.js"}},
			{Name: "内容管理", Slug: "yuanwu-content", Description: "业务内容和素材维护。", SourcePaths: []string{"src/router/modules/content.js"}},
			{Name: "资源管理", Slug: "yuanwu-resource", Description: "公共业务资源配置。", SourcePaths: []string{"src/router/modules/resource.js"}},
			{Name: "案例与影像", Slug: "yuanwu-case-assets", Description: "案例模板和前后对比照片。", SourcePaths: []string{"src/router/modules/template.js", "src/router/modules/contrast-photo.js"}},
		},
	},
	{
		Name: "库存与供应链", Slug: "yuanwu-inventory-supply",
		Description: "库存台账、盘点、采购、退货和扣减管理。",
		SourcePaths: []string{"src/router/modules/inventory.js", "src/router/modules/inventory-management.js", "src/router/modules/inventory-info.js", "src/router/modules/inventory-list.js", "src/router/modules/inventory-check.js", "src/router/modules/purchase.js", "src/router/modules/return-management.js", "src/router/modules/deduct.js", "src/router/modules/inspection.js"},
		Children: []productMapCatalogNode{
			{Name: "库存台账", Slug: "yuanwu-inventory", Description: "库存信息、列表和综合管理。", SourcePaths: []string{"src/router/modules/inventory.js", "src/router/modules/inventory-management.js", "src/router/modules/inventory-info.js", "src/router/modules/inventory-list.js"}},
			{Name: "库存盘点", Slug: "yuanwu-inventory-check", Description: "盘点任务、差异和结果处理。", SourcePaths: []string{"src/router/modules/inventory-check.js"}},
			{Name: "采购管理", Slug: "yuanwu-purchase", Description: "采购业务和入库协作。", SourcePaths: []string{"src/router/modules/purchase.js"}},
			{Name: "退货与扣减", Slug: "yuanwu-return-deduct", Description: "退货、库存扣减和异常处理。", SourcePaths: []string{"src/router/modules/return-management.js", "src/router/modules/deduct.js"}},
			{Name: "验收管理", Slug: "yuanwu-inspection", Description: "物资和采购验收流程。", SourcePaths: []string{"src/router/modules/inspection.js"}},
		},
	},
	{
		Name: "经营分析与系统治理", Slug: "yuanwu-operations-governance",
		Description: "经营数据、目标配额、租户、用户和后台工具。",
		SourcePaths: []string{"src/router/modules/data.js", "src/router/modules/quota.js", "src/router/modules/target.js", "src/router/modules/admin.js", "src/router/modules/tenant.js", "src/router/modules/userManagement.js", "src/router/modules/tools.js", "src/router/modules/errormanager.js", "src/router/modules/base.js", "src/router/modules/list.js"},
		Children: []productMapCatalogNode{
			{Name: "经营数据", Slug: "yuanwu-data-dashboard", Description: "核心经营指标与数据看板。", SourcePaths: []string{"src/router/modules/data.js"}},
			{Name: "目标与配额", Slug: "yuanwu-target-quota", Description: "经营目标和业务配额管理。", SourcePaths: []string{"src/router/modules/target.js", "src/router/modules/quota.js"}},
			{Name: "组织与用户", Slug: "yuanwu-tenant-users", Description: "租户、用户及其权限配置。", SourcePaths: []string{"src/router/modules/tenant.js", "src/router/modules/userManagement.js"}},
			{Name: "后台管理", Slug: "yuanwu-admin-tools", Description: "后台管理、基础配置和运维工具。", SourcePaths: []string{"src/router/modules/admin.js", "src/router/modules/tools.js", "src/router/modules/errormanager.js", "src/router/modules/base.js", "src/router/modules/list.js"}},
		},
	},
}
