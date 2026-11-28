package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	systemhandler "github.com/Autumn-27/ScopeSentry/internal/api/handlers/system"
	"github.com/Autumn-27/ScopeSentry/internal/config"
	"github.com/Autumn-27/ScopeSentry/internal/constants"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	auditrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/audit"
	githubrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/github"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/app"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/asset"
	assetgroup "github.com/Autumn-27/ScopeSentry/internal/services/assets/assetgroup"
	assetCommon "github.com/Autumn-27/ScopeSentry/internal/services/assets/common"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/crawler"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/dirscan"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/ip"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/mp"
	nucleiresultsvc "github.com/Autumn-27/ScopeSentry/internal/services/assets/nucleiresult"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/page_monitoring"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/root_domain"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/sensitive"
	statfingersvc "github.com/Autumn-27/ScopeSentry/internal/services/assets/statfinger"
	statisticssvc "github.com/Autumn-27/ScopeSentry/internal/services/assets/statistics"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/subdomain"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/url"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/vulnerability"
	wihsvc "github.com/Autumn-27/ScopeSentry/internal/services/assets/wih"
	"github.com/Autumn-27/ScopeSentry/internal/services/backup"
	"github.com/Autumn-27/ScopeSentry/internal/services/dashboard"
	"github.com/Autumn-27/ScopeSentry/internal/services/dictionary"
	"github.com/Autumn-27/ScopeSentry/internal/services/export"
	"github.com/Autumn-27/ScopeSentry/internal/services/fingerprint"
	"github.com/Autumn-27/ScopeSentry/internal/services/icp"
	"github.com/Autumn-27/ScopeSentry/internal/services/mcpguard"
	"github.com/Autumn-27/ScopeSentry/internal/services/node"
	"github.com/Autumn-27/ScopeSentry/internal/services/plugin"
	"github.com/Autumn-27/ScopeSentry/internal/services/poc"
	"github.com/Autumn-27/ScopeSentry/internal/services/policy"
	"github.com/Autumn-27/ScopeSentry/internal/services/project"
	"github.com/Autumn-27/ScopeSentry/internal/services/sensitive_rule"
	taskCommon "github.com/Autumn-27/ScopeSentry/internal/services/task/common"
	"github.com/Autumn-27/ScopeSentry/internal/services/task/scheduler"
	"github.com/Autumn-27/ScopeSentry/internal/services/task/task"
	"github.com/Autumn-27/ScopeSentry/internal/services/task/template"
	"github.com/Autumn-27/ScopeSentry/internal/utils/helper"
	"github.com/Autumn-27/ScopeSentry/internal/utils/hoststat"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
)

type deps struct {
	projectService project.Service
	taskService    task.Service
	// 计划任务（scan 与 page_monitoring 同一集合，scheduledTasks 是启用开关）
	schedulerService   scheduler.Service
	taskCommonService  taskCommon.Service
	templateService    template.Service
	assetService       asset.Service
	rootDomainService  root_domain.Service
	subdomainService   subdomain.Service
	appService         app.Service
	mpService          mp.Service
	urlService         url.Service
	sensitiveService   sensitive.Service
	dirscanService     dirscan.Service
	crawlerService     crawler.Service
	vulnService        vulnerability.Service
	ipService          ip.Service
	pageMonService     page_monitoring.Service
	commonService      assetCommon.Service
	nodeService        node.Service
	pluginService      plugin.Service
	pocService         poc.Service
	fingerprintService fingerprint.Service
	dictService        dictionary.ManageService
	// 端口字典（{dict.port.*} 模板占位符的数据源）与 5 类行式字典是两套存储，
	// ARL-Next 侧同样独立；MCP 一直没有覆盖它（2026-11-28 全工具扫描实测）
	portDictService      dictionary.PortService
	dashboardService     dashboard.Service
	policyService        *policy.Service
	sensitiveRuleService sensitive_rule.Service
	exportService        export.Service
	// 备份只读面：Agent 至少要能回答「最近一次备份成没成」
	backupService backup.Service
	// 资产分布统计（端口/服务/应用/标题/图标 TopN）：只读聚合，UI 有、MCP 一直没有
	statisticsService statisticssvc.Service
	// 资产组 / 地契（asset_group + asset_scope，ARL-Next 资产模型）
	assetGroupService assetgroup.Service
	// ICP 主体任务（company → domain → asset 链路的入口）
	icpService *icp.Service
	// 资产读模型：nuclei 命中 / WIH 敏感命中 / 指纹统计（REST 有、MCP 一直没有）
	nucleiService     nucleiresultsvc.Service
	wihService        wihsvc.Service
	statFingerService statfingersvc.Service
	// GitHub 泄露结果（code_leak / cve / hacker 的命中），只读
	githubResultRepo githubrepo.ResultRepository
	// 服务端操作日志（只读，仓库层直接给 List）
	auditRepo auditrepo.Repository
}

// mcpMaxPageSize 是分页类工具的单页硬上限。
// 依据：list_assets 传 pageSize=100000 实测返回 45363 行 / 29.4MB / 26s，
// 客户端超时、LLM 上下文直接被顶爆；而 list_pocs / list_fingerprints 早就有 200 上限，
// 同族工具行为不一致（2026-11-28 审计）。
const mcpMaxPageSize = 200

// capPageSize 统一收敛分页参数：页码至少 1，单页不超过 mcpMaxPageSize。
func capPageSize(pageIndex, pageSize *int) {
	if *pageIndex <= 0 {
		*pageIndex = 1
	}
	if *pageSize <= 0 {
		*pageSize = 20
	}
	if *pageSize > mcpMaxPageSize {
		*pageSize = mcpMaxPageSize
	}
}

// mcpListTextLimit 是列表类响应里单个长文本字段的保留长度。
// 列表要回答「有哪些」，正文属于详情类工具（get_poc / get_scan_template）。
const mcpListTextLimit = 512

// truncateListFields 把 list/items 里指定字段的超长文本截断，并标注 <field>Truncated / <field>Length。
// 背景：list_pocs 默认 200 条、每条带完整 YAML，实测单次响应 12.4MB（2026-11-28）。
func truncateListFields(v any, fields ...string) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return v
	}
	touched := false
	for _, key := range []string{"list", "items", "data"} {
		arr, ok := m[key].([]any)
		if !ok {
			continue
		}
		for _, it := range arr {
			item, ok := it.(map[string]any)
			if !ok {
				continue
			}
			for _, f := range fields {
				s, ok := item[f].(string)
				if !ok || len(s) <= mcpListTextLimit {
					continue
				}
				item[f] = truncateRunes(s, mcpListTextLimit)
				item[f+"Truncated"] = true
				item[f+"Length"] = len(s)
				touched = true
			}
		}
	}
	if !touched {
		return v
	}
	m["truncatedNote"] = fmt.Sprintf("长文本字段已截断到 %d 字符，完整内容请用对应详情工具获取", mcpListTextLimit)
	return m
}

// truncateRunes 按字符（非字节）截断，避免切坏 UTF-8。
func truncateRunes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit])
}

var d = &deps{
	projectService:       project.NewService(),
	taskService:          task.NewService(),
	schedulerService:     scheduler.NewService(),
	taskCommonService:    taskCommon.NewService(),
	templateService:      template.NewService(),
	assetService:         asset.NewService(),
	rootDomainService:    root_domain.NewService(),
	subdomainService:     subdomain.NewService(),
	appService:           app.NewService(),
	mpService:            mp.NewService(),
	urlService:           url.NewService(),
	sensitiveService:     sensitive.NewService(),
	dirscanService:       dirscan.NewService(),
	crawlerService:       crawler.NewService(),
	vulnService:          vulnerability.NewService(),
	ipService:            ip.NewService(),
	pageMonService:       page_monitoring.NewService(),
	commonService:        assetCommon.NewService(),
	nodeService:          node.NewService(),
	pluginService:        plugin.NewService(),
	pocService:           poc.NewService(),
	fingerprintService:   fingerprint.NewService(),
	dictService:          dictionary.NewManageService(),
	portDictService:      dictionary.NewPortService(),
	dashboardService:     dashboard.NewService(),
	policyService:        policy.NewService(),
	sensitiveRuleService: sensitive_rule.NewService(),
	exportService:        export.NewService(),
	backupService:        backup.NewService(),
	assetGroupService:    assetgroup.NewService(),
	statisticsService:    statisticssvc.NewService(),
	// icp 服务是进程内单例（与 HTTP handler 共用），构造只建仓库句柄，不碰库
	icpService:        icp.DefaultService(),
	nucleiService:     nucleiresultsvc.NewService(),
	wihService:        wihsvc.NewService(),
	statFingerService: statfingersvc.NewService(),
	githubResultRepo:  githubrepo.NewResultRepository(),
	auditRepo:         auditrepo.NewRepository(),
}

func registerTools(server *mcp.Server, allowedTools map[string]bool) {
	names := make([]string, 0, 32)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "get_asset_profile",
		Description: "获取某个目标（域名/IP/URL）的综合资产画像：子域数量与样本、端口与服务指纹、URL 数量、漏洞与敏感信息。用于一次回答「这个目标到底有什么」。",
	}, getAssetProfile)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "get_enterprise_profile",
		Description: "按企业（RootDomain.company）或根域聚合：旗下根域清单与子域/资产/URL/漏洞总量。",
	}, getEnterpriseProfile)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "search_assets",
		Description: "跨库检索：按关键词在子域、Web 资产、URL、漏洞、敏感信息里同时查找，返回每类命中数与样本。",
	}, searchAssets)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "list_projects",
		Description: "获取按标签分组的项目列表",
	}, listProjects)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "list_projects_data",
		Description: "分页获取项目列表，支持搜索",
	}, listProjectsData)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "get_project",
		Description: "根据项目 ID 获取项目详情",
	}, getProject)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "create_project",
		Description: "创建新项目。必填 name 和有效 target，可选 tag、template、node 等。名称已存在且 target 整理后仍有目标时，只替换该项目的资产范围。",
	}, createProject)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "list_tasks",
		Description: "分页获取扫描任务列表",
	}, listTasks)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "get_task",
		Description: "根据任务 ID 获取任务详情",
	}, getTask)

	// 计划任务（周期扫描）与一次性扫描在同一个集合，但扫描任务列表不返回它们：
	// create_scan_task 支持 scheduledTasks/cycleType，却没有任何工具能看见或关掉已建的计划，
	// 于是 agent 建了周期任务之后就失明（2026-11-28 对照 71 个工具确认）。
	// 任务归属补录：扫描产出归档到项目是这条链路的最后一公里，
	// 之前只有 bindProject（建任务时就定死），跑完才想归档只能回 UI。
	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "assign_tasks_project",
		Description: "把已有任务挂到指定项目下（projectId 传空字符串表示解除归属）。只改归属，不触发扫描",
	}, assignTasksProject)

	// 目标级暂停/恢复：整任务停会把已经跑出结果的目标一起扔掉，
	// 大批量目标失控时只能一个一个目标停（issue #277 的 REST 能力，MCP 一直没有）。
	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "pause_task_targets",
		Description: "暂停（跳过）任务里的指定目标：未被节点领取的目标会从队列摘除并减少 taskNum，已领取的只记录状态",
	}, pauseTaskTargets)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "resume_task_targets",
		Description: "恢复被 pause_task_targets 暂停的目标",
	}, resumeTaskTargets)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "list_scheduled_tasks",
		Description: "分页获取计划任务（周期扫描，含页面监控）列表：名称、周期、下次/上次执行时间、启用状态、节点与模板",
	}, listScheduledTasks)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "get_scheduled_task",
		Description: "根据任务 ID 获取计划任务详情（目标、忽略、节点、周期参数、模板、绑定项目）",
	}, getScheduledTask)

	// 备份只读面：运维 Agent 要能回答「最近一次备份成没成」，但备份的创建/恢复/删除
	// 是不可逆动作，MCP 不给写入口（UI 里 restore 还要 confirm:"RESTORE"）。
	// 存储占用：解释「库为什么这么大 / 备份为什么久」，只读。
	// 瘦身入口不给 MCP——那是对存量数据的不可逆操作，只保留 REST + confirm 门禁。
	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "get_storage_report",
		Description: "各集合占用（字节/文档数，按大小降序）+ 合计：用于回答「库为什么这么大、备份为什么久」",
	}, getStorageReport)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "list_backups",
		Description: "列出数据库备份文件（名称、大小、生成时间、SHA256、备注）与当前备份/恢复任务状态",
	}, listBackups)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "set_scheduled_task_state",
		Description: "启用或停用计划任务。只改 scheduledTasks 开关，其余字段与周期保持不变；停用后不再触发新的扫描",
	}, setScheduledTaskState)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "list_scan_templates",
		Description: "分页获取扫描模板列表",
	}, listScanTemplates)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "get_scan_template",
		Description: "根据模板 ID 获取扫描模板详情",
	}, getScanTemplate)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "list_plugin_modules",
		Description: "获取扫描模板的全部模块名（流水线阶段）。扫描模板由这些模块构成，每个模块下挂载若干插件。",
	}, listPluginModules)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "list_plugins",
		Description: "获取可用扫描插件，可按 module 过滤。返回每个插件的 hash、name、module、默认 parameter。创建扫描模板时，模块字段引用的就是插件 hash。",
	}, listPlugins)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "create_scan_template",
		Description: createScanTemplateToolDesc,
	}, createScanTemplate)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "create_scan_task",
		Description: createScanTaskToolDesc,
	}, createScanTask)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "list_assets",
		Description: listAssetsToolDesc,
	}, listAssets)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "count_assets",
		Description: countAssetsToolDesc,
	}, countAssets)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "asset_distribution",
		Description: "资产分布 TopN：按端口/服务/应用/标题/图标统计资产数量，支持与 list_assets 相同的 search 与 filter。回答「哪些端口/服务/标题占主导」。注意后端聚合有上限：port/service 最多 200 个桶、title 最多 50 个，超出时 truncated=true（列表本身仍按 limit 截断）",
	}, assetDistribution)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "get_asset_detail",
		Description: "获取资产详情，asset_type 支持 asset 或 vulnerability",
	}, getAssetDetail)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "intel_for_assets",
		Description: intelForAssetsDesc,
	}, intelForAssets)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "add_asset_tag",
		Description: "为资产添加标签",
	}, addAssetTag)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "list_nodes",
		Description: "获取扫描节点列表，online_only=true 时仅返回在线节点",
	}, listNodes)

	// ---- 只读扩展：POC / 指纹 / 字典 / 看板 ----
	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "list_pocs",
		Description: "分页查询 POC 列表，可按名称搜索、按危险等级过滤",
	}, listPocs)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "get_poc",
		Description: "按 POC ID 获取单个 POC 的完整内容",
	}, getPoc)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "list_fingerprints",
		Description: "分页查询 Web 指纹规则",
	}, listFingerprints)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "list_dictionaries",
		Description: "按类型（port/subdomain/dir/brute）分页列出字典",
	}, listDictionaries)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "preview_dictionary",
		Description: "预览某个字典的前 N 行（上限 200）",
	}, previewDictionary)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "search_dictionary_lines",
		Description: "在某个字典内按关键字搜行",
	}, searchDictionaryLines)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "get_dashboard_stats",
		Description: "按项目查询看板计数。vulnerability 为全部扫描候选记录，包含各处置状态；vulnConfirmed 为状态已确认为存在的记录数。汇报已确认漏洞数时使用 vulnConfirmed。",
	}, getDashboardStats)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "get_console_info",
		Description: "控制台信息：版本、uptime、节点与任务数量、依赖状态",
	}, getConsoleInfo)

	// ---- 写/控：任务、扫描策略、敏感信息规则 ----
	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "get_task_progress",
		Description: "查询任务的目标级进度（分页）",
	}, getTaskProgress)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "start_task",
		Description: "启动指定任务（任务需处于暂停/待开始状态）",
	}, startTask)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "stop_task",
		Description: "暂停指定任务",
	}, stopTask)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "delete_task",
		Description: "删除指定任务，可选同时删除其资产",
	}, deleteTask)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "list_scan_policies",
		Description: "分页查询扫描策略",
	}, listScanPolicies)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "get_scan_policy",
		Description: "按 ID 获取扫描策略详情（配置、启用状态、被任务/模板引用的用量统计）",
	}, getScanPolicy)

	// 策略试算：只做「合并 + 冲突检查」的纯计算，不写任何东西。
	// 没有它，Agent 建完策略只能凭描述猜效果，或者直接拿去建任务（污染任务表）。
	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "apply_scan_policy",
		Description: "策略试算：把策略合并到指定扫描模板上，返回生效后的配置、应用到的字段与被策略覆盖的冲突提示；不创建任务、不改模板",
	}, applyScanPolicy)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "create_scan_policy",
		Description: "创建扫描策略",
	}, createScanPolicy)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "delete_scan_policies",
		Description: "批量删除扫描策略",
	}, deleteScanPolicies)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "list_sensitive_rules",
		Description: "分页查询敏感信息规则",
	}, listSensitiveRules)

	// ---- 资产读模型补充（REST 早就有，MCP 一直缺）----
	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "list_nuclei_results",
		Description: "分页查询 nuclei 命中结果（按 severity/host/keyword/taskName/project 过滤）",
	}, listNucleiResults)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "list_wih_results",
		Description: "分页查询 WIH 敏感信息命中（按规则名 type/project/keyword 过滤）",
	}, listWIHResults)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "list_stat_finger",
		Description: "按技术名聚合的指纹统计（出现次数 / 资产数 / 最近出现 / 样例主机）",
	}, listStatFinger)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "list_github_results",
		Description: "分页查询 GitHub 泄露结果（code_leak / cve / hacker，按 status/type/taskId 过滤）",
	}, listGithubResults)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "set_sensitive_rule_state",
		Description: "批量启用/停用敏感信息规则",
	}, setSensitiveRuleState)

	// ---- 节点运维 + 字典维护 ----
	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "list_node_plugins",
		Description: "列出某节点上各插件的安装/检查状态",
	}, listNodePlugins)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "get_node_logs",
		Description: "读取某节点的日志文本",
	}, getNodeLogs)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "restart_node",
		Description: "重启指定节点",
	}, restartNode)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "delete_nodes",
		Description: "批量删除节点记录",
	}, deleteNodes)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "create_dictionary",
		Description: "按类型新建字典（重名报错），content 为换行分隔的行",
	}, createDictionary)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "append_dictionary_lines",
		Description: "向已有字典追加行（唯一索引去重）",
	}, appendDictionaryLines)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "delete_dictionary",
		Description: "按类型 + 名称删除整个字典",
	}, deleteDictionary)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "delete_dictionary_entries",
		Description: "删除字典中的指定行",
	}, deleteDictionaryEntries)

	// ---- 端口字典（{dict.port.*} 的数据源，与行式字典分开存储）----
	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "list_port_dict",
		Description: "分页查询端口字典（扫描模板 {dict.port.<name>} 占位符的数据源）",
	}, listPortDict)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "set_port_dict_entry",
		Description: "新增或修改一条端口字典：不给 id 是新增（name 唯一），给 id 是改那一条",
	}, setPortDictEntry)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "delete_port_dict_entries",
		Description: "按 id 批量删除端口字典条目（不可逆，需 confirm=true）",
	}, deletePortDictEntries)

	// ---- 数据导出 ----
	addTool(server, allowedTools, &names, &mcp.Tool{
		Name: "export_task_workbook",
		Description: "按任务导出产出：xlsx（默认，一个工作簿里子域名/资产/URL/漏洞/敏感信息各一张表）" +
			"或 zip（每集合一个 CSV）。可只导给定时段（startTime/endTime）。" +
			"文件落在导出目录并登记到 list_export_records，客户端用 REST 下载接口取。",
	}, exportTaskWorkbook)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "list_export_records",
		Description: "列出历史导出记录（文件名、类型、状态、大小）",
	}, listExportRecords)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "get_export_fields",
		Description: "查询某类数据可导出的字段列表",
	}, getExportFields)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "create_export_task",
		Description: "创建数据导出任务（异步生成文件），返回文件名用于后续下载/删除",
	}, createExportTask)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "delete_export_files",
		Description: "删除导出的文件与记录",
	}, deleteExportFiles)

	// ---- 插件安装 / 重检 / 卸载 ----
	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "reinstall_plugin",
		Description: "在指定节点上重新安装插件",
	}, reinstallPlugin)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "recheck_plugin",
		Description: "让指定节点重新检查插件可用性（不改文件）",
	}, recheckPlugin)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "uninstall_plugin",
		Description: "从指定节点卸载插件",
	}, uninstallPlugin)

	// ---- 资产组 / 地契 / ICP 任务 / 服务端操作日志 ----
	// 全部只读、全部复用 asset:read（见 mcpguard.ToolScopes 的说明）。
	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "list_asset_groups",
		Description: listAssetGroupsToolDesc,
	}, listAssetGroups)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "list_asset_scopes",
		Description: listAssetScopesToolDesc,
	}, listAssetScopes)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "list_icp_tasks",
		Description: listICPTasksToolDesc,
	}, listICPTasks)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "list_audit_logs",
		Description: listAuditLogsToolDesc,
	}, listAuditLogs)

	// ---- 写侧第一批（v2.0.88）：资产组 / 地契 / ICP 任务 ----
	// 只做 5 个，其余写操作（集团编辑/重排、地契改删移动、ICP 重启/恢复/删除、
	// 结果入库与地契同步）仍在 Web 端。删除类工具强制 confirm=true。
	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "create_asset_group",
		Description: createAssetGroupToolDesc,
	}, createAssetGroup)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "delete_asset_groups",
		Description: deleteAssetGroupsToolDesc,
	}, deleteAssetGroups)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "create_asset_scope",
		Description: createAssetScopeToolDesc,
	}, createAssetScope)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "create_icp_task",
		Description: createICPTaskToolDesc,
	}, createICPTask)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "stop_icp_tasks",
		Description: stopICPTasksToolDesc,
	}, stopICPTasks)

	addTool(server, allowedTools, &names, &mcp.Tool{
		Name:        "get_system_info",
		Description: "系统运行状态：版本、启动时间、运行时长、节点在线数、任务数、Mongo/Redis 连接状态，以及主机磁盘/负载/内存（hostStats/hostWarnings）。用于排障与健康检查。",
	}, getSystemInfo)

	recordToolNames(names)
	warnUnregisteredTools(names)
}

// warnUnregisteredTools 生产环境的漂移报警：工具注册了但 mcpguard.ToolScopes 里没有，
// 持受限 scope 的 key 调用它会被默认拒绝。本包 init 需要数据库，纯单测覆盖不到，
// 所以留一条运行期告警（集成测试 tools_registry_integration_test.go 会直接失败）。
//
// 每个进程只报一次：warnUnregisteredTools 在每次建 server 时都会走到，
// 之前是「每个未登记工具每条请求一行 Warn」，实测一个进程刷出 98199 行，
// 把真正有用的日志埋掉（2026-11-28 实测）。
func warnUnregisteredTools(names []string) {
	warnUnregisteredOnce.Do(func() {
		missing := make([]string, 0, 4)
		for _, name := range names {
			if _, ok := mcpguard.RequiredScope(name); !ok {
				missing = append(missing, name)
			}
		}
		if len(missing) == 0 {
			logger.Info("mcp: tool scopes aligned with mcpguard.ToolScopes",
				zap.Int("tools", len(names)))
			return
		}
		logger.Error("mcp: tools missing from mcpguard.ToolScopes (restricted keys will be denied)",
			zap.Int("count", len(missing)), zap.Strings("tools", missing))
	})
	// 注册期被 schema 校验挡下来的工具：对客户端表现为 unknown tool，
	// 不单独报一条的话只能从「工具数少了」自己发现
	warnSkippedOnce.Do(func() {
		skippedToolsMu.Lock()
		dropped := make([]string, 0, len(skippedTools))
		for n := range skippedTools {
			dropped = append(dropped, n)
		}
		skippedToolsMu.Unlock()
		if len(dropped) == 0 {
			return
		}
		sort.Strings(dropped)
		logger.Error("mcp: tools declared but NOT registered (invalid schema; clients see unknown tool)",
			zap.Int("count", len(dropped)), zap.Strings("tools", dropped))
	})
}

var warnUnregisteredOnce sync.Once

// warnSkippedOnce 只报一次被跳过的工具清单
var warnSkippedOnce sync.Once

// addTool 统一注册入口。
//
// 两个职责：
//  1. 登记工具名（names），供 ToolNames() 做「治理表 vs 实际工具」漂移测试；
//  2. allowedTools 非 nil 时只注册集合内的工具，让 tools/list 与治理策略一致。
//
// 注册期被跳过的工具名：会记录到 skippedTools，由 warnUnregisteredTools 汇总上报。
// 单独留着的理由：只记一行 ERROR 的话，工具从 tools/list 里消失这件事对客户端完全不可见
// （2026-11-28 实测：list_tasks 因为 tag 里写了 “空=全部” 被整条跳过，客户端只看到 unknown tool）。
var (
	skippedToolsMu sync.Mutex
	// 用 map 去重：Stateless 模式下每个请求都会新建 server 并重新注册一遍工具，
	// 用 slice 记录会按请求数量级膨胀（同 warnUnregisteredTools 早期那 98199 行日志的坑）。
	skippedTools = map[string]struct{}{}
)

func addTool[In, Out any](server *mcp.Server, allowedTools map[string]bool, names *[]string, tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out]) {
	*names = append(*names, tool.Name)
	if allowedTools != nil && !allowedTools[tool.Name] {
		return
	}
	// 单个工具的 schema 非法（例如 struct tag 以 "WORD=" 开头）会让 go-sdk 直接 panic，
	// 一旦发生在注册阶段，整个 /mcp 端点对所有客户端 500。这里隔离到单个工具：
	// 坏的那个跳过并记错误，其余工具照常可用。
	defer func() {
		if r := recover(); r != nil {
			skippedToolsMu.Lock()
			skippedTools[tool.Name] = struct{}{}
			skippedToolsMu.Unlock()
			logger.Error("mcp: tool registration skipped (invalid schema)",
				zap.String("tool", tool.Name), zap.Any("panic", r))
		}
	}()
	mcp.AddTool(server, tool, guardTool(tool.Name, handler))
}

// guardTool 给单个工具的执行期加 recover。
//
// 为什么必须在工具层做：MCP 工具由 go-sdk 的 jsonrpc2 在自己的 goroutine 里调用，
// 不在 gin.Recovery 的管辖范围。任何一个工具里的 nil 解引用都会打死整个进程
// （2026-11-28 实测：get_asset_detail 传未知资产 id 让生产容器重启了 5 次）。
// 这里把 panic 收成一条工具级错误 + 堆栈日志，服务继续活着。
func guardTool[In, Out any](name string, handler mcp.ToolHandlerFor[In, Out]) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (res *mcp.CallToolResult, out Out, err error) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error("mcp: tool handler panicked",
					zap.String("tool", name), zap.Any("panic", rec), zap.Stack("stack"))
				r, _, e := errorResult("工具执行异常，已中断（服务未受影响）: "+name, nil)
				var zero Out
				res, out, err = r, zero, e
			}
		}()
		return handler(ctx, req, in)
	}
}

var (
	registeredToolNamesMu sync.RWMutex
	registeredToolNames   []string
)

// recordToolNames 幂等登记工具名列表（每次建 server 都会走到，内容恒定）
func recordToolNames(names []string) {
	registeredToolNamesMu.Lock()
	defer registeredToolNamesMu.Unlock()
	if len(registeredToolNames) == 0 {
		registeredToolNames = append([]string(nil), names...)
	}
}

// ToolNames 返回本包实际注册过的全部 MCP 工具名
func ToolNames() []string {
	registeredToolNamesMu.RLock()
	defer registeredToolNamesMu.RUnlock()
	return append([]string(nil), registeredToolNames...)
}

type listProjectsDataInput struct {
	Search    string `json:"search,omitempty" jsonschema:"项目名称模糊搜索关键词"`
	PageIndex int    `json:"pageIndex,omitempty" jsonschema:"页码，从 1 开始，默认 1"`
	PageSize  int    `json:"pageSize,omitempty" jsonschema:"每页条数，默认 20"`
}

func listProjects(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	c := ginContext(ctx)
	result, err := d.projectService.GetProjectsByTag(c)
	if err != nil {
		return errorResult("获取项目列表失败", err)
	}
	return jsonToolResult(ginH{"list": result})
}

func listProjectsData(ctx context.Context, _ *mcp.CallToolRequest, input listProjectsDataInput) (*mcp.CallToolResult, any, error) {
	capPageSize(&input.PageIndex, &input.PageSize)
	c := ginContext(ctx)
	result, err := d.projectService.GetProjectsData(c, input.Search, input.PageIndex, input.PageSize)
	if err != nil {
		return errorResult("获取项目数据失败", err)
	}
	return jsonToolResult(result)
}

type getProjectInput struct {
	ID string `json:"id" jsonschema:"项目 MongoDB ObjectID"`
}

func getProject(ctx context.Context, _ *mcp.CallToolRequest, input getProjectInput) (*mcp.CallToolResult, any, error) {
	if input.ID == "" {
		return errorResult("id 不能为空", nil)
	}
	c := ginContext(ctx)
	result, err := d.projectService.GetProjectContent(c, input.ID)
	if err != nil {
		return errorResult("获取项目详情失败", err)
	}
	if result == nil {
		return errorResult("项目不存在", nil)
	}
	return jsonToolResult(result)
}

type createProjectInput struct {
	Name           string   `json:"name" jsonschema:"项目名称，必填"`
	Tag            string   `json:"tag,omitempty" jsonschema:"项目标签，用于分组"`
	Target         string   `json:"target" jsonschema:"扫描目标，必填。多行或逗号分隔，支持域名/IP/URL 等"`
	Template       string   `json:"template,omitempty" jsonschema:"关联扫描模板 ID"`
	Node           []string `json:"node,omitempty" jsonschema:"指定扫描节点名称列表"`
	AllNode        bool     `json:"allNode,omitempty" jsonschema:"是否使用全部节点"`
	Ignore         string   `json:"ignore,omitempty" jsonschema:"忽略目标列表，格式同 target"`
	Duplicates     string   `json:"duplicates,omitempty" jsonschema:"去重策略"`
	ScheduledTasks bool     `json:"scheduledTasks,omitempty" jsonschema:"是否启用定时扫描"`
	Hour           int      `json:"hour,omitempty" jsonschema:"定时扫描间隔（小时），仅在启用 scheduledTasks 时有效"`
}

func createProject(ctx context.Context, _ *mcp.CallToolRequest, input createProjectInput) (*mcp.CallToolResult, any, error) {
	if input.Name == "" || strings.TrimSpace(input.Target) == "" {
		return errorResult("name 和 target 不能为空", nil)
	}
	p := &models.Project{
		Name:           input.Name,
		Tag:            input.Tag,
		Target:         input.Target,
		Template:       input.Template,
		Node:           input.Node,
		AllNode:        input.AllNode,
		Ignore:         input.Ignore,
		Duplicates:     input.Duplicates,
		ScheduledTasks: input.ScheduledTasks,
		Hour:           input.Hour,
		Tp:             "project",
	}
	c := ginContext(ctx)
	id, replaced, err := d.projectService.ApplyProjectScope(c, p)
	if err != nil {
		return errorResult("创建项目失败", err)
	}
	if replaced {
		return jsonToolResult(ginH{
			"success":  true,
			"message":  "项目范围已替换",
			"id":       id,
			"replaced": true,
		})
	}
	return jsonToolResult(ginH{
		"success":  true,
		"message":  "项目创建成功",
		"id":       id,
		"replaced": false,
	})
}

type listTasksInput struct {
	Search    string `json:"search,omitempty" jsonschema:"任务名称模糊搜索"`
	PageIndex int    `json:"pageIndex,omitempty" jsonschema:"页码，从 1 开始，默认 1"`
	PageSize  int    `json:"pageSize,omitempty" jsonschema:"每页条数，默认 20"`
	// 产出口径（对齐 ARL-Next 1.5.2）：回答「哪些任务真的扫出了东西 / 哪些白跑了一趟」
	BindProject string   `json:"bindProject,omitempty" jsonschema:"按归属项目 ObjectID 过滤（list_projects 获取）"`
	AssetYield  string   `json:"assetYield,omitempty" jsonschema:"产出口径：has_asset 有产出 / no_asset 终态且无产出；留空表示不过滤"`
	AssetTypes  []string `json:"assetTypes,omitempty" jsonschema:"产出维度（site/domain/url/ip/vuln/wih）；留空表示全部维度。不支持的维度会直接报错"`
}

func listTasks(ctx context.Context, _ *mcp.CallToolRequest, input listTasksInput) (*mcp.CallToolResult, any, error) {
	capPageSize(&input.PageIndex, &input.PageSize)
	c := ginContext(ctx)
	tasks, total, err := d.taskService.List(c, input.Search, input.PageIndex, input.PageSize, task.ListOptions{
		BindProject: input.BindProject,
		AssetYield:  input.AssetYield,
		AssetTypes:  input.AssetTypes,
	})
	if err != nil {
		return errorResult("获取任务列表失败", err)
	}
	return jsonToolResult(ginH{"list": tasks, "total": total})
}

type getTaskInput struct {
	ID string `json:"id" jsonschema:"任务 MongoDB ObjectID"`
}

func getTask(ctx context.Context, _ *mcp.CallToolRequest, input getTaskInput) (*mcp.CallToolResult, any, error) {
	if input.ID == "" {
		return errorResult("id 不能为空", nil)
	}
	c := ginContext(ctx)
	result, err := d.taskService.GetTaskDetail(c, input.ID)
	if err != nil {
		return errorResult("获取任务详情失败", err)
	}
	if result == nil {
		return errorResult("任务不存在", nil)
	}
	return jsonToolResult(result)
}

type assignTasksProjectInput struct {
	IDs       []string `json:"ids" jsonschema:"任务 ObjectID 列表，必填"`
	ProjectID string   `json:"projectId" jsonschema:"项目 ObjectID，用 list_projects 获取；传空字符串表示解除归属"`
}

func assignTasksProject(ctx context.Context, _ *mcp.CallToolRequest, input assignTasksProjectInput) (*mcp.CallToolResult, any, error) {
	if len(input.IDs) == 0 {
		return errorResult("ids 不能为空", nil)
	}
	c := ginContext(ctx)
	updated, err := d.taskService.AssignProject(c, input.IDs, input.ProjectID)
	if err != nil {
		return errorResult("更新任务归属失败", err)
	}
	// 只有真的改到文档时 updated > 0。0 有两种可能：id 不存在，或归属本来就是目标值
	// （解除归属时尤其常见）——不判失败，但把话说清楚，避免「以为归档了其实没动」。
	if updated == 0 {
		return jsonToolResult(ginH{
			"updated":   0,
			"requested": len(input.IDs),
			"projectId": input.ProjectID,
			"note":      "没有任务被更新：id 不存在/已删除，或归属本来就是目标值",
		})
	}
	return jsonToolResult(ginH{"updated": updated, "requested": len(input.IDs), "projectId": input.ProjectID})
}

type taskTargetsInput struct {
	TaskID  string   `json:"taskId" jsonschema:"任务 ObjectID，用 list_tasks 获取"`
	Targets []string `json:"targets" jsonschema:"要暂停/恢复的目标（域名或 IP，需与任务里的目标字符串一致）"`
}

func pauseTaskTargets(ctx context.Context, _ *mcp.CallToolRequest, input taskTargetsInput) (*mcp.CallToolResult, any, error) {
	return runTargetControl(ctx, input, "pause")
}

func resumeTaskTargets(ctx context.Context, _ *mcp.CallToolRequest, input taskTargetsInput) (*mcp.CallToolResult, any, error) {
	return runTargetControl(ctx, input, "resume")
}

// runTargetControl 逐个目标执行暂停/恢复：单个目标失败不影响其余目标，
// 结果按目标回执，避免「一个拼错就整批白跑」。
func runTargetControl(ctx context.Context, input taskTargetsInput, action string) (*mcp.CallToolResult, any, error) {
	if input.TaskID == "" || len(input.Targets) == 0 {
		return errorResult("taskId 与 targets 不能为空", nil)
	}
	c := ginContext(ctx)
	done := make([]string, 0, len(input.Targets))
	failed := make([]ginH, 0)
	for _, t := range input.Targets {
		var err error
		if action == "pause" {
			err = d.taskService.PauseTarget(c, input.TaskID, t)
		} else {
			err = d.taskService.ResumeTarget(c, input.TaskID, t)
		}
		if err != nil {
			failed = append(failed, ginH{"target": t, "error": err.Error()})
			continue
		}
		done = append(done, t)
	}
	if len(done) == 0 {
		return errorResult("没有目标被处理", nil)
	}
	return jsonToolResult(ginH{"action": action, "done": done, "failed": failed})
}

type listScheduledTasksInput struct {
	Search    string `json:"search,omitempty" jsonschema:"任务名称模糊搜索"`
	PageIndex int    `json:"pageIndex,omitempty" jsonschema:"页码，从 1 开始，默认 1"`
	PageSize  int    `json:"pageSize,omitempty" jsonschema:"每页条数，默认 20"`
}

func listScheduledTasks(ctx context.Context, _ *mcp.CallToolRequest, input listScheduledTasksInput) (*mcp.CallToolResult, any, error) {
	capPageSize(&input.PageIndex, &input.PageSize)
	c := ginContext(ctx)
	tasks, total, err := d.schedulerService.GetScheduledData(c, input.Search, input.PageIndex, input.PageSize)
	if err != nil {
		return errorResult("获取计划任务列表失败", err)
	}
	return jsonToolResult(ginH{"list": tasks, "total": total})
}

type getScheduledTaskInput struct {
	ID string `json:"id" jsonschema:"计划任务 MongoDB ObjectID，用 list_scheduled_tasks 获取"`
}

func getScheduledTask(ctx context.Context, _ *mcp.CallToolRequest, input getScheduledTaskInput) (*mcp.CallToolResult, any, error) {
	if input.ID == "" {
		return errorResult("id 不能为空", nil)
	}
	c := ginContext(ctx)
	task, err := d.schedulerService.GetScheduledTaskDetail(c, input.ID)
	if err != nil {
		if errors.Is(err, scheduler.ErrScheduledTaskNotFound) {
			return errorResult("计划任务不存在", nil)
		}
		return errorResult("获取计划任务详情失败", err)
	}
	if task == nil {
		return errorResult("计划任务不存在", nil)
	}
	return jsonToolResult(task)
}

type getScanPolicyInput struct {
	ID string `json:"id" jsonschema:"扫描策略 ObjectID，用 list_scan_policies 获取"`
}

func getScanPolicy(ctx context.Context, _ *mcp.CallToolRequest, input getScanPolicyInput) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(input.ID) == "" {
		return errorResult("id 不能为空", nil)
	}
	detail, err := d.policyService.Detail(ctx, input.ID)
	if err != nil {
		return errorResult("获取策略详情失败", err)
	}
	if detail == nil {
		return errorResult("策略不存在", nil)
	}
	return jsonToolResult(detail)
}

type applyScanPolicyInput struct {
	PolicyID   string `json:"policyId" jsonschema:"扫描策略 ObjectID，用 list_scan_policies 获取"`
	TemplateID string `json:"templateId,omitempty" jsonschema:"要合并到的扫描模板 ObjectID（list_scan_templates 获取）；留空表示按策略自身配置试算"`
}

func applyScanPolicy(ctx context.Context, _ *mcp.CallToolRequest, input applyScanPolicyInput) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(input.PolicyID) == "" {
		return errorResult("policyId 不能为空", nil)
	}
	var raw json.RawMessage
	if strings.TrimSpace(input.TemplateID) != "" {
		b, err := json.Marshal(map[string]string{"templateId": strings.TrimSpace(input.TemplateID)})
		if err != nil {
			return errorResult("构造模板引用失败", err)
		}
		raw = json.RawMessage(b)
	}
	res, err := d.policyService.Apply(ctx, input.PolicyID, raw)
	if err != nil {
		return errorResult("策略试算失败", err)
	}
	if res == nil {
		return errorResult("策略不存在", nil)
	}
	return jsonToolResult(res)
}

func getStorageReport(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	rep, err := systemhandler.StorageStats(ctx, 10)
	if err != nil {
		return errorResult("获取存储占用失败", err)
	}
	return jsonToolResult(rep)
}

func listBackups(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	list, err := d.backupService.List()
	if err != nil {
		return errorResult("获取备份列表失败", err)
	}
	status := d.backupService.Status()
	return jsonToolResult(ginH{"list": list, "total": len(list), "status": status})
}

type setScheduledTaskStateInput struct {
	ID      string `json:"id" jsonschema:"计划任务 MongoDB ObjectID，用 list_scheduled_tasks 获取"`
	Enabled bool   `json:"enabled" jsonschema:"true 启用（按周期触发扫描），false 停用（保留配置但不再触发）"`
}

func setScheduledTaskState(ctx context.Context, _ *mcp.CallToolRequest, input setScheduledTaskStateInput) (*mcp.CallToolResult, any, error) {
	if input.ID == "" {
		return errorResult("id 不能为空", nil)
	}
	c := ginContext(ctx)
	task, err := d.schedulerService.SetScheduledTaskState(c, input.ID, input.Enabled)
	if err != nil {
		if errors.Is(err, scheduler.ErrScheduledTaskNotFound) {
			return errorResult("计划任务不存在", nil)
		}
		return errorResult("切换计划任务状态失败", err)
	}
	return jsonToolResult(ginH{
		"id":             task.ID.Hex(),
		"name":           task.Name,
		"scheduledTasks": task.ScheduledTasks,
		"cycleType":      task.CycleType,
		"hour":           task.Hour,
		"minute":         task.Minute,
		"nextTime":       task.NextTime,
	})
}

type listScanTemplatesInput struct {
	Query     string `json:"query,omitempty" jsonschema:"模板名称模糊搜索"`
	PageIndex int    `json:"pageIndex,omitempty" jsonschema:"页码，从 1 开始，默认 1"`
	PageSize  int    `json:"pageSize,omitempty" jsonschema:"每页条数，默认 20"`
}

func listScanTemplates(ctx context.Context, _ *mcp.CallToolRequest, input listScanTemplatesInput) (*mcp.CallToolResult, any, error) {
	capPageSize(&input.PageIndex, &input.PageSize)
	result, err := d.templateService.List(ctx, input.PageIndex, input.PageSize, input.Query)
	if err != nil {
		return errorResult("获取模板列表失败", err)
	}
	// 模板里 template_json 可达数百 KB，列表只要「有哪些」，正文留给 get_scan_template。
	return jsonToolResult(truncateListFields(result, "template_json", "templateJson", "vullist"))
}

type getScanTemplateInput struct {
	ID string `json:"id" jsonschema:"扫描模板 MongoDB ObjectID"`
}

func getScanTemplate(ctx context.Context, _ *mcp.CallToolRequest, input getScanTemplateInput) (*mcp.CallToolResult, any, error) {
	if input.ID == "" {
		return errorResult("id 不能为空", nil)
	}
	c := ginContext(ctx)
	result, err := d.templateService.Detail(c, input.ID)
	if err != nil {
		return errorResult("获取模板详情失败", err)
	}
	return jsonToolResult(result)
}

type createScanTemplateInput struct {
	Name         string                       `json:"name" jsonschema:"模板名称，必填"`
	Modules      map[string][]string          `json:"modules,omitempty" jsonschema:"模块到插件 hash 列表的映射。key 为模块名(用 list_plugin_modules 获取)，value 为该模块下要启用的插件 hash 数组(用 list_plugins 获取，按数组顺序执行)。例 {\"SubdomainScan\":[\"d60ba73c...\"]}"`
	Parameters   map[string]map[string]string `json:"parameters,omitempty" jsonschema:"可选，覆盖插件运行参数。结构为 模块名->插件hash->参数字符串。不提供时自动使用插件默认参数"`
	VulList      []string                     `json:"vullist,omitempty" jsonschema:"可选，nuclei POC 模板 ID 列表，仅 VulnerabilityScan 使用 nuclei 时有效"`
	TemplateJSON string                       `json:"template_json,omitempty" jsonschema:"可选，完整 ScanTemplate JSON。提供时优先于 modules，用于高级自定义"`
}

func createScanTemplate(ctx context.Context, _ *mcp.CallToolRequest, input createScanTemplateInput) (*mcp.CallToolResult, any, error) {
	if input.Name == "" && input.TemplateJSON == "" {
		return errorResult("name 或 template_json 至少提供一个", nil)
	}

	var tmpl *models.ScanTemplate

	switch {
	case input.TemplateJSON != "":
		tmpl = &models.ScanTemplate{}
		if err := json.Unmarshal([]byte(input.TemplateJSON), tmpl); err != nil {
			return errorResult("template_json 格式无效", err)
		}
		if tmpl.Name == "" {
			tmpl.Name = input.Name
		}
	case len(input.Modules) > 0:
		built, err := buildTemplateFromModules(ctx, input)
		if err != nil {
			return errorResult(err.Error(), nil)
		}
		tmpl = built
	default:
		tmpl = &models.ScanTemplate{Name: input.Name}
	}

	if tmpl.Name == "" {
		return errorResult("模板名称不能为空", nil)
	}

	id, err := d.templateService.Save(ctx, "", tmpl)
	if err != nil {
		return errorResult("创建模板失败", err)
	}
	return jsonToolResult(ginH{"success": true, "id": id, "message": "模板创建成功，可用于 create_scan_task 的 template 参数"})
}

// buildTemplateFromModules 根据「模块->插件hash列表」组装扫描模板，并自动回填插件默认参数
func buildTemplateFromModules(ctx context.Context, input createScanTemplateInput) (*models.ScanTemplate, error) {
	validModule := make(map[string]bool, len(constants.PLUGINSMODULES))
	for _, m := range constants.PLUGINSMODULES {
		validModule[m] = true
	}

	templateMap := map[string]any{
		"name": input.Name,
	}
	paramMap := map[string]map[string]string{}

	for module, hashes := range input.Modules {
		if !validModule[module] {
			return nil, fmt.Errorf("无效的模块名: %s（用 list_plugin_modules 获取合法模块）", module)
		}
		if len(hashes) == 0 {
			continue
		}
		templateMap[module] = hashes

		modParams := map[string]string{}
		for _, hash := range hashes {
			// 优先使用调用方提供的参数覆盖
			if input.Parameters != nil {
				if mp, ok := input.Parameters[module]; ok {
					if v, ok := mp[hash]; ok {
						modParams[hash] = v
						continue
					}
				}
			}
			// 否则回填插件默认参数
			plg, err := d.pluginService.GetPluginByHash(ctx, hash)
			if err != nil || plg == nil {
				return nil, fmt.Errorf("插件 hash 不存在: %s（模块 %s）", hash, module)
			}
			if plg.Module != module {
				return nil, fmt.Errorf("插件 %s(hash=%s) 属于模块 %s，不能放入模块 %s", plg.Name, hash, plg.Module, module)
			}
			modParams[hash] = plg.Parameter
		}
		if len(modParams) > 0 {
			paramMap[module] = modParams
		}
	}

	if len(paramMap) > 0 {
		templateMap["Parameters"] = paramMap
	}
	if len(input.VulList) > 0 {
		templateMap["vullist"] = input.VulList
	}

	data, err := json.Marshal(templateMap)
	if err != nil {
		return nil, fmt.Errorf("组装模板失败: %w", err)
	}
	tmpl := &models.ScanTemplate{}
	if err := json.Unmarshal(data, tmpl); err != nil {
		return nil, fmt.Errorf("组装模板失败: %w", err)
	}
	return tmpl, nil
}

type createScanTaskInput struct {
	Name           string              `json:"name" jsonschema:"任务名称，必填，不可重复"`
	Target         string              `json:"target,omitempty" jsonschema:"扫描目标，targetSource 为 general 时必填，多行或逗号分隔"`
	Node           []string            `json:"node" jsonschema:"执行扫描的节点名称列表，必填"`
	Template       string              `json:"template,omitempty" jsonschema:"扫描模板 ObjectID，必填才能执行扫描"`
	AllNode        bool                `json:"allNode,omitempty" jsonschema:"是否自动加入全部在线节点"`
	Ignore         string              `json:"ignore,omitempty" jsonschema:"忽略目标列表，格式同 target"`
	Duplicates     string              `json:"duplicates,omitempty" jsonschema:"去重策略，如 None"`
	Project        []string            `json:"project,omitempty" jsonschema:"关联项目 ObjectID 列表，targetSource 为 project 时必填"`
	TargetSource   string              `json:"targetSource,omitempty" jsonschema:"目标来源，可选 general project asset RootDomain subdomain UrlScan 及对应 Source 后缀，默认 general，详见工具 description"`
	TargetTp       string              `json:"targetTp,omitempty" jsonschema:"Source 来源时的选取方式，search 或 select"`
	Search         string              `json:"search,omitempty" jsonschema:"从资产库筛选目标的 search 表达式，语法同 list_assets，详见工具 description"`
	Filter         map[string][]string `json:"filter,omitempty" jsonschema:"精确过滤，与 search 可组合，filter.project 为项目 ObjectID"`
	TargetNumber   int                 `json:"targetNumber,omitempty" jsonschema:"search 模式下目标数量上限，0 表示不限制"`
	TargetIds      []string            `json:"targetIds,omitempty" jsonschema:"select 模式下选中的资产 ObjectID 列表"`
	BindProject    string              `json:"bindProject,omitempty" jsonschema:"绑定项目 ObjectID，扫描结果归属"`
	ScheduledTasks bool                `json:"scheduledTasks,omitempty" jsonschema:"是否创建为计划任务（true 时只建周期任务、不额外立刻跑一次；建好后用 list_scheduled_tasks 查看、set_scheduled_task_state 停用）"`
	Hour           int                 `json:"hour,omitempty" jsonschema:"计划任务间隔-小时"`
	Minute         int                 `json:"minute,omitempty" jsonschema:"计划任务间隔-分钟"`
	Day            int                 `json:"day,omitempty" jsonschema:"计划任务间隔-天"`
	Week           int                 `json:"week,omitempty" jsonschema:"计划任务间隔-周（weekly 周期）"`
	CycleType      string              `json:"cycleType,omitempty" jsonschema:"周期类型，如 nhours daily weekly"`
	// 注意：jsonschema tag 里不能出现「WORD=VALUE」形式，MCP 会把它当默认值标记并在注册时 panic，
	// 结果是这个工具从 tools/list 里静默消失（2026-11-29 实测：写完部署后工具数从 82 掉到 81）。
	ScanProfile string `json:"scanProfile,omitempty" jsonschema:"扫描档位。留空按模板完整扫描；填 recon 表示只做资产测绘（跳过 nuclei 与目录扫描，任务通常几分钟结束），需要漏洞面时再用完整档位跑一遍"`
}

func createScanTask(ctx context.Context, _ *mcp.CallToolRequest, input createScanTaskInput) (*mcp.CallToolResult, any, error) {
	if input.Name == "" || len(input.Node) == 0 {
		return errorResult("name 和 node 不能为空", nil)
	}
	c := ginContext(ctx)
	exists, err := d.taskService.CheckTaskNameExists(c, input.Name)
	if err != nil {
		return errorResult("检查任务名失败", err)
	}
	if exists {
		return errorResult("任务名已存在", nil)
	}

	targetSource := input.TargetSource
	if targetSource == "" {
		targetSource = "general"
	}

	taskModel := &models.Task{
		Name:           input.Name,
		Target:         input.Target,
		Node:           input.Node,
		Template:       input.Template,
		AllNode:        input.AllNode,
		Ignore:         input.Ignore,
		Duplicates:     input.Duplicates,
		Project:        input.Project,
		TargetSource:   targetSource,
		TargetTp:       input.TargetTp,
		Search:         input.Search,
		TargetNumber:   input.TargetNumber,
		TargetIds:      input.TargetIds,
		BindProject:    input.BindProject,
		ScheduledTasks: input.ScheduledTasks,
		Hour:           input.Hour,
		Minute:         input.Minute,
		Day:            input.Day,
		Week:           input.Week,
		CycleType:      input.CycleType,
		ScanProfile:    input.ScanProfile,
	}
	if len(input.Filter) > 0 {
		filter := make(map[string][]interface{}, len(input.Filter))
		for k, vals := range input.Filter {
			items := make([]interface{}, len(vals))
			for i, v := range vals {
				items[i] = v
			}
			filter[k] = items
		}
		taskModel.Filter = filter
	}
	// scheduledTasks=true 时走计划任务入口，而不是「插一条一次性任务」。
	// 之前只调 Insert，参数被静默忽略：agent 以为建了周期任务，实际只跑一次（docs/audit/MCP_GAPS.md §5.5）。
	// 这里也不再额外插一条一次性任务：传 true 的含义就是「按周期跑」，不该顺手把目标扫一遍。
	if input.ScheduledTasks {
		if err := d.schedulerService.CreateScheduledTask(c, taskModel); err != nil {
			return errorResult("创建计划任务失败", err)
		}
		return jsonToolResult(ginH{
			"success":       true,
			"id":            taskModel.ID.Hex(),
			"scheduled":     true,
			"cycleType":     taskModel.CycleType,
			"hour":          taskModel.Hour,
			"minute":        taskModel.Minute,
			"day":           taskModel.Day,
			"week":          taskModel.Week,
			"scheduledTask": taskModel.ScheduledTasks,
		})
	}
	taskID, err := d.taskCommonService.Insert(ctx, taskModel)
	if err != nil {
		return errorResult("创建扫描任务失败", err)
	}
	return jsonToolResult(ginH{"success": true, "id": taskID})
}

type listAssetsInput struct {
	AssetType        string              `json:"asset_type" jsonschema:"资产类型，必填。如 asset、RootDomain、subdomain、app、mp、UrlScan、SensitiveResult、DirScanResult、crawler、vulnerability、PageMonitoring、IPAsset、SubdomainTakerResult"`
	PageIndex        int                 `json:"pageIndex,omitempty" jsonschema:"页码，从 1 开始，默认 1"`
	PageSize         int                 `json:"pageSize,omitempty" jsonschema:"每页条数，默认 20"`
	SearchExpression string              `json:"search,omitempty" jsonschema:"搜索表达式，非 SQL，语法详见工具 description"`
	Filter           map[string][]string `json:"filter,omitempty" jsonschema:"精确过滤 JSON，与 search 可组合。filter.project 为项目 ObjectID（list_projects 获取）；filter.task 为任务名称；statuscode/port/length 可传数字或数字字符串"`
	Sort             map[string]string   `json:"sort,omitempty" jsonschema:"排序。仅 UrlScan/DirScanResult 支持 length：ascending 升序，其他值降序"`
	Sid              string              `json:"sid,omitempty" jsonschema:"敏感信息规则名称，仅 asset_type 为 SensitiveResult 时有效"`
}

type assetDistributionInput struct {
	Dimension        string              `json:"dimension" jsonschema:"统计维度，必填：port / service / app / title / icon"`
	SearchExpression string              `json:"search,omitempty" jsonschema:"搜索表达式，语法同 list_assets"`
	Filter           map[string][]string `json:"filter,omitempty" jsonschema:"精确过滤 JSON，语法同 list_assets；filter.project 为项目 ObjectID"`
	Limit            int                 `json:"limit,omitempty" jsonschema:"返回前 N 项，默认 20，最多 200"`
}

// assetDistribution 资产分布 TopN。
//
// REST 侧是 5 个端点（/api/assets/statistics/{port,service,app,title,icon}），
// 返回结构各不相同（Port/Service/App/Title/Icon 各包一层）。MCP 侧统一成
// [{value, count, iconHash?}]，Agent 不用为每个维度记一套字段名。
func assetDistribution(ctx context.Context, _ *mcp.CallToolRequest, input assetDistributionInput) (*mcp.CallToolResult, any, error) {
	dim := strings.ToLower(strings.TrimSpace(input.Dimension))
	if dim == "" {
		return errorResult("dimension 不能为空（port/service/app/title/icon）", nil)
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	query := models.SearchRequest{Index: "asset", SearchExpression: input.SearchExpression}
	if len(input.Filter) > 0 {
		query.Filter = mcpFilterValues(input.Filter)
	}
	c := ginContext(ctx)

	type bucket struct {
		Value    string `json:"value"`
		Count    int64  `json:"count"`
		IconHash string `json:"iconHash,omitempty"`
	}
	out := make([]bucket, 0, limit)
	total := 0

	switch dim {
	case "port":
		res, err := d.statisticsService.GetPortStatistics(c, &query)
		if err != nil {
			return errorResult("端口统计失败", err)
		}
		total = len(res.Port)
		for _, it := range res.Port {
			if len(out) >= limit {
				break
			}
			out = append(out, bucket{Value: it.Value, Count: it.Number})
		}
	case "service":
		res, err := d.statisticsService.GetTypeStatistics(c, &query)
		if err != nil {
			return errorResult("服务统计失败", err)
		}
		total = len(res.Service)
		for _, it := range res.Service {
			if len(out) >= limit {
				break
			}
			out = append(out, bucket{Value: it.Value, Count: it.Number})
		}
	case "app":
		res, err := d.statisticsService.GetAppStatistics(c, &query)
		if err != nil {
			return errorResult("应用统计失败", err)
		}
		// 注意字段名是 Product（应用=产品指纹），不是 App
		total = len(res.Product)
		for _, it := range res.Product {
			if len(out) >= limit {
				break
			}
			out = append(out, bucket{Value: it.Value, Count: it.Number})
		}
	case "title":
		res, err := d.statisticsService.GetTitleStatistics(c, &query)
		if err != nil {
			return errorResult("标题统计失败", err)
		}
		total = len(res.Title)
		for _, it := range res.Title {
			if len(out) >= limit {
				break
			}
			out = append(out, bucket{Value: it.Value, Count: it.Number})
		}
	case "icon":
		res, err := d.statisticsService.GetIconStatistics(c, &query)
		if err != nil {
			return errorResult("图标统计失败", err)
		}
		total = len(res.Icon)
		for _, it := range res.Icon {
			if len(out) >= limit {
				break
			}
			// 图标内容是 base64，几百 KB：列表只给 hash 和数量，取原图走 get_asset_detail
			out = append(out, bucket{Value: it.IconHash, Count: it.Number, IconHash: it.IconHash})
		}
	default:
		return errorResult("dimension 只能是 port/service/app/title/icon", nil)
	}

	// 后端聚合自带上限（port/service 200、title 50，见 statistics.go 的 $limit），
	// 桶数正好等于上限说明「还有更多但被截了」，必须显式告诉调用方，不能让人以为这就是全集。
	capByDim := map[string]int{"port": 200, "service": 200, "title": 50, "app": 0, "icon": 0}
	capped := false
	if c := capByDim[dim]; c > 0 && total >= c {
		capped = true
	}
	return jsonToolResult(ginH{
		"dimension": dim,
		"limit":     limit,
		"buckets":   total,
		"truncated": capped,
		"list":      out,
	})
}

type countAssetsInput struct {
	AssetType        string              `json:"asset_type" jsonschema:"资产类型，必填。取值同 list_assets"`
	SearchExpression string              `json:"search,omitempty" jsonschema:"搜索表达式，语法同 list_assets"`
	Filter           map[string][]string `json:"filter,omitempty" jsonschema:"精确过滤 JSON，语法同 list_assets；filter.project 为项目 ObjectID"`
	Sid              string              `json:"sid,omitempty" jsonschema:"敏感信息规则名称，仅 asset_type 为 SensitiveResult 时有效"`
}

func countAssets(ctx context.Context, _ *mcp.CallToolRequest, input countAssetsInput) (*mcp.CallToolResult, any, error) {
	index, err := normalizeAssetIndex(input.AssetType)
	if err != nil {
		return errorResult(err.Error(), nil)
	}

	query := models.SearchRequest{
		Index:            index,
		SearchExpression: input.SearchExpression,
		Sid:              input.Sid,
	}
	if len(input.Filter) > 0 {
		query.Filter = mcpFilterValues(input.Filter)
	}

	total, err := d.commonService.TotalData(ctx, &query)
	if err != nil {
		return errorResult("统计资产数量失败", err)
	}
	return jsonToolResult(ginH{"total": total})
}

func listAssets(ctx context.Context, _ *mcp.CallToolRequest, input listAssetsInput) (*mcp.CallToolResult, any, error) {
	index, err := normalizeAssetIndex(input.AssetType)
	if err != nil {
		return errorResult(err.Error(), nil)
	}
	capped := input.PageSize > mcpMaxPageSize
	capPageSize(&input.PageIndex, &input.PageSize)

	query := models.SearchRequest{
		PageIndex:        input.PageIndex,
		PageSize:         input.PageSize,
		Index:            index,
		SearchExpression: input.SearchExpression,
		Sort:             input.Sort,
		Sid:              input.Sid,
	}
	if len(input.Filter) > 0 {
		query.Filter = mcpFilterValues(input.Filter)
	}

	c := ginContext(ctx)
	data, err := queryAssets(ctx, c, index, query)
	if err != nil {
		return errorResult("查询资产失败", err)
	}
	// 统一带上 total/pageIndex/pageSize：不同索引的 REST 响应有的带 total、有的不带
	// （asset/subdomain/UrlScan/SensitiveResult 都没有），客户端拿不到总数就没法翻页，
	// 只能反复试探到空页为止（2026-11-28 实测）。
	out := ginH{}
	if b, e := json.Marshal(data); e == nil {
		_ = json.Unmarshal(b, &out)
	}
	if _, has := out["total"]; !has {
		countQuery := models.SearchRequest{
			Index:            index,
			SearchExpression: input.SearchExpression,
			Sid:              input.Sid,
		}
		if len(input.Filter) > 0 {
			countQuery.Filter = query.Filter
		}
		if total, e := d.commonService.TotalData(ctx, &countQuery); e == nil {
			out["total"] = total
		}
	}
	out["pageIndex"] = input.PageIndex
	out["pageSize"] = input.PageSize
	if capped {
		out["pageSizeCapped"] = mcpMaxPageSize
		out["note"] = fmt.Sprintf("pageSize 已收敛到上限 %d；大结果集请按页取或用 count_assets 拿总数", mcpMaxPageSize)
	}
	// IPAsset 是「按 IP 分页、按 IP×端口×服务展开」的视图：一页 3 个 IP 可能展开成
	// 10 条端口行，total 数的是 IP。不说明的话客户端会以为分页坏了（2026-11-28 实测）。
	if index == "IPAsset" {
		out["groupBy"] = "ip"
		out["totalIsGroupCount"] = true
		out["note"] = "按 IP 分页后按 IP×端口×服务展开，list 行数通常大于 pageSize；total 是 IP 数"
	}
	aliasAssetRowFields(out)
	return jsonToolResult(out)
}

// aliasAssetRowFields 给资产行补上「滤镜键同名」的别名字段。
//
// 起因（2026-11-29 输出契约审计）：filter 的键和返回行的字段不是一套名字——
//
//	filter {"statuscode":["200"]} 命中 4935 条，但行里叫 status，没有 statuscode；
//	search_assets 返回的行叫 host，list_assets 返回的行叫 domain。
//
// 对人是小别扭，对自动化是硬伤：拿到 total 却验证不了 rows 是否真的被过滤，
// 也没法把两个工具的结果按同一个键合并。这里只做只读别名，不改原字段。
func aliasAssetRowFields(out ginH) {
	rows, ok := out["list"].([]interface{})
	if !ok {
		return
	}
	for _, raw := range rows {
		row, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if _, has := row["host"]; !has {
			if v, ok := row["domain"]; ok {
				row["host"] = v
			}
		}
		if _, has := row["statuscode"]; !has {
			if v, ok := row["status"]; ok {
				row["statuscode"] = v
			}
		}
	}
}

// mcpFilterValues 把 MCP 的 []string 过滤值原样转成 []interface{}。
//
// 这里曾经按字段名做「字符串 → 数字」归一，结果把 port 过滤打成静默 0：
// asset.port 在库里存的是字符串 "443"，转成 int 后等值匹配不到任何文档。
// 实测（2026-11-28）：
//
//	filter {"port":["443"]}  → REST total 6530，MCP total 0   ← 归一惹的祸
//	filter {"port":[443]}    → REST total 0
//	filter {"statuscode":["200"]} 与 [200] 都是 4935（该字段两种形态都能命中）
//
// 后端 helper.buildFilterForms 对「数值歧义字段」收到 string 时会同时下发字符串与数字两种形态，
// 哪种存法都能命中。所以 MCP 只要和 REST 一样原样透传字符串，就自动拿到这层归一。
func mcpFilterValues(filter map[string][]string) map[string][]interface{} {
	out := make(map[string][]interface{}, len(filter))
	for k, vals := range filter {
		items := make([]interface{}, 0, len(vals))
		for _, v := range vals {
			items = append(items, v)
		}
		out[k] = items
	}
	return out
}

// resolveProjectID 允许传项目名或 ObjectID：资产里的 project 存的是 ID，
// 直接拿名字去等值匹配会永远得到 0（get_dashboard_stats 的老问题）。
func resolveProjectID(c *gin.Context, project string) (string, error) {
	project = strings.TrimSpace(project)
	if project == "" {
		return "", nil
	}
	if len(project) == 24 {
		if _, err := primitive.ObjectIDFromHex(project); err == nil {
			return project, nil
		}
	}
	res, err := d.projectService.GetProjectsData(c, project, 1, 200)
	if err != nil {
		return "", err
	}
	for _, list := range res.Result {
		for _, p := range list {
			if strings.EqualFold(strings.TrimSpace(p.Name), project) {
				return p.ID, nil
			}
		}
	}
	return "", fmt.Errorf("项目不存在: %s", project)
}

type getAssetDetailInput struct {
	AssetType string `json:"asset_type" jsonschema:"资产类型。详情查询支持 asset 或 vulnerability"`
	ID        string `json:"id" jsonschema:"资产 MongoDB ObjectID；vulnerability 类型传 hash 值"`
}

func getAssetDetail(ctx context.Context, _ *mcp.CallToolRequest, input getAssetDetailInput) (*mcp.CallToolResult, any, error) {
	if input.ID == "" {
		return errorResult("id 不能为空", nil)
	}
	c := ginContext(ctx)
	index, err := normalizeAssetIndex(input.AssetType)
	if err != nil {
		return errorResult(err.Error(), nil)
	}

	switch index {
	case "asset":
		result, err := d.assetService.GetAssetByID(c, input.ID)
		if err != nil {
			return errorResult("获取资产详情失败", err)
		}
		if result == nil {
			return errorResult("资产不存在", nil)
		}
		return jsonToolResult(result)
	case "vulnerability":
		result, err := d.vulnService.GetVulnerabilityDetailByHash(c, input.ID)
		if err != nil {
			return errorResult("获取漏洞详情失败", err)
		}
		return jsonToolResult(result)
	default:
		return errorResult("asset_type 仅支持 asset 或 vulnerability 的详情查询", nil)
	}
}

type addAssetTagInput struct {
	AssetType string `json:"asset_type" jsonschema:"资产类型，同 list_assets 的 asset_type"`
	ID        string `json:"id" jsonschema:"资产 MongoDB ObjectID"`
	Tag       string `json:"tag" jsonschema:"要添加的标签名称"`
}

func addAssetTag(ctx context.Context, _ *mcp.CallToolRequest, input addAssetTagInput) (*mcp.CallToolResult, any, error) {
	index, err := normalizeAssetIndex(input.AssetType)
	if err != nil {
		return errorResult(err.Error(), nil)
	}
	if input.ID == "" || input.Tag == "" {
		return errorResult("id 和 tag 不能为空", nil)
	}
	c := ginContext(ctx)
	req := &models.TagRequest{Type: index, ID: input.ID, Tag: input.Tag}
	if err := d.commonService.AddTag(c, req); err != nil {
		// 资产 id 不存在时驱动层吐的是 "mongo: no documents in result"，
		// 对调用方毫无意义（像是后端崩了）；这里换成能据以纠正参数的结论（2026-11-28 审计）。
		if strings.Contains(err.Error(), "no documents in result") {
			return errorResult(fmt.Sprintf("%s 不存在: %s", index, input.ID), nil)
		}
		return errorResult("添加标签失败", err)
	}
	return jsonToolResult(ginH{"ok": true, "assetType": index, "id": input.ID, "tag": input.Tag})
}

type listNodesInput struct {
	OnlineOnly bool `json:"online_only,omitempty" jsonschema:"true 时仅返回在线节点，默认 false 返回全部"`
}

func listNodes(ctx context.Context, _ *mcp.CallToolRequest, input listNodesInput) (*mcp.CallToolResult, any, error) {
	result, err := d.nodeService.GetNodeData(ctx, input.OnlineOnly)
	if err != nil {
		return errorResult("获取节点列表失败", err)
	}
	// 顺带给出「该用哪台」：外部集成（如 huntdeck）只看 JSON，
	// 没有 queueLen 判断就会一直往最忙的节点推，把队列塞满（2026-11-20 实测）。
	// 规则：在线且队列最短者优先。
	recommended := ""
	best := int64(1 << 62)
	for _, n := range result {
		if fmt.Sprintf("%v", n.State) != "1" {
			continue
		}
		if n.QueueLen < best {
			best = n.QueueLen
			recommended = n.Name
		}
	}
	out := ginH{"list": result}
	if recommended != "" {
		out["recommended"] = recommended
		out["recommendedQueueLen"] = best
	}
	return jsonToolResult(out)
}

func listPluginModules(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	return jsonToolResult(ginH{"modules": constants.PLUGINSMODULES})
}

type listPluginsInput struct {
	Module string `json:"module,omitempty" jsonschema:"按模块名过滤，留空返回全部扫描插件。模块名用 list_plugin_modules 获取"`
	Search string `json:"search,omitempty" jsonschema:"按插件名称模糊搜索（仅在未指定 module 时生效）"`
}

type pluginBrief struct {
	Hash      string `json:"hash"`
	Name      string `json:"name"`
	Module    string `json:"module"`
	Parameter string `json:"parameter"`
	Type      string `json:"type"`
	Status    bool   `json:"status"`
}

func toPluginBriefs(plugins []models.Plugin) []pluginBrief {
	briefs := make([]pluginBrief, 0, len(plugins))
	for _, p := range plugins {
		// 跳过服务端插件，它们不参与扫描流水线
		if p.Type == "server" {
			continue
		}
		briefs = append(briefs, pluginBrief{
			Hash:      p.Hash,
			Name:      p.Name,
			Module:    p.Module,
			Parameter: p.Parameter,
			Type:      p.Type,
			Status:    p.Status,
		})
	}
	return briefs
}

func listPlugins(ctx context.Context, _ *mcp.CallToolRequest, input listPluginsInput) (*mcp.CallToolResult, any, error) {
	c := ginContext(ctx)

	if input.Module != "" {
		plugins, err := d.pluginService.ListByModule(c, input.Module)
		if err != nil {
			return errorResult("获取插件列表失败", err)
		}
		return jsonToolResult(ginH{"list": toPluginBriefs(plugins)})
	}

	resp, err := d.pluginService.List(c, &models.PluginListRequest{
		PageIndex: 1,
		PageSize:  500,
		Search:    input.Search,
	})
	if err != nil {
		return errorResult("获取插件列表失败", err)
	}
	return jsonToolResult(ginH{"list": toPluginBriefs(resp.List), "total": resp.Total})
}

func queryAssets(ctx context.Context, c *gin.Context, index string, query models.SearchRequest) (any, error) {
	switch index {
	case "asset":
		list, err := d.assetService.GetAssets(c, query)
		return ginH{"list": list}, err
	case "RootDomain":
		return d.rootDomainService.GetRootDomainData(c, query)
	case "subdomain":
		list, err := d.subdomainService.GetSubdomains(ctx, query)
		return ginH{"list": list}, err
	case "app":
		return d.appService.GetAppData(c, query)
	case "mp":
		return d.mpService.GetMPData(c, query)
	case "UrlScan":
		list, err := d.urlService.GetURLs(ctx, query)
		return ginH{"list": list}, err
	case "SensitiveResult":
		list, err := d.sensitiveService.GetSensitiveInfo(ctx, query)
		return ginH{"list": list}, err
	case "DirScanResult":
		list, err := d.dirscanService.List(ctx, query)
		return ginH{"list": list}, err
	case "crawler":
		list, err := d.crawlerService.GetCrawlers(ctx, query)
		return ginH{"list": list}, err
	case "vulnerability":
		return d.vulnService.GetVulnerabilities(ctx, query)
	case "PageMonitoring":
		return d.pageMonService.GetResult(c, query)
	case "IPAsset":
		return d.ipService.GetIPAssets(c, query)
	case "SubdomainTakerResult":
		return querySubdomainTaker(ctx, query)
	default:
		return nil, fmt.Errorf("不支持的资产类型: %s", index)
	}
}

func querySubdomainTaker(ctx context.Context, query models.SearchRequest) (any, error) {
	takerService := subdomain.NewTakerService()
	c := ginContext(ctx)
	return takerService.GetSubdomainTakerData(c, query)
}

func normalizeAssetIndex(assetType string) (string, error) {
	assetType = strings.TrimSpace(assetType)
	if assetType == "" {
		return "", fmt.Errorf("asset_type 不能为空")
	}
	aliases := map[string]string{
		"asset":                "asset",
		"web":                  "asset",
		"rootdomain":           "RootDomain",
		"root_domain":          "RootDomain",
		"root-domain":          "RootDomain",
		"subdomain":            "subdomain",
		"app":                  "app",
		"mp":                   "mp",
		"miniprogram":          "mp",
		"mini_program":         "mp",
		"url":                  "UrlScan",
		"urlscan":              "UrlScan",
		"sensitive":            "SensitiveResult",
		"sensitiveresult":      "SensitiveResult",
		"sensitive_result":     "SensitiveResult",
		"dirscan":              "DirScanResult",
		"dir_scan":             "DirScanResult",
		"dirscanresult":        "DirScanResult",
		"directory":            "DirScanResult",
		"crawler":              "crawler",
		"vulnerability":        "vulnerability",
		"vuln":                 "vulnerability",
		"pagemonitoring":       "PageMonitoring",
		"page_monitoring":      "PageMonitoring",
		"ip":                   "IPAsset",
		"ipasset":              "IPAsset",
		"subdomaintaker":       "SubdomainTakerResult",
		"subdomain_taker":      "SubdomainTakerResult",
		"subdomaintakerresult": "SubdomainTakerResult",
	}
	key := strings.ToLower(assetType)
	if v, ok := aliases[key]; ok {
		return v, nil
	}
	if _, ok := aliases[strings.ReplaceAll(key, "-", "_")]; ok {
		return aliases[strings.ReplaceAll(key, "-", "_")], nil
	}
	// 允许直接传 MongoDB 集合名
	valid := []string{"asset", "RootDomain", "subdomain", "app", "mp", "UrlScan",
		"SensitiveResult", "DirScanResult", "crawler", "vulnerability",
		"PageMonitoring", "IPAsset", "SubdomainTakerResult"}
	for _, v := range valid {
		if v == assetType {
			return v, nil
		}
	}
	return "", fmt.Errorf("不支持的 asset_type: %s", assetType)
}

type ginH map[string]any

// ---------------------------------------------------------------------------
// 只读扩展工具
// ---------------------------------------------------------------------------

type listPocsInput struct {
	Search    string   `json:"search,omitempty" jsonschema:"POC 名称或模板 ID 模糊搜索"`
	Level     []string `json:"level,omitempty" jsonschema:"危险等级过滤：critical/high/medium/low/info"`
	PageIndex int      `json:"pageIndex,omitempty" jsonschema:"页码，从 1 开始，默认 1"`
	PageSize  int      `json:"pageSize,omitempty" jsonschema:"每页条数，默认 20，最大 200"`
}

func listPocs(ctx context.Context, _ *mcp.CallToolRequest, input listPocsInput) (*mcp.CallToolResult, any, error) {
	if input.PageIndex <= 0 {
		input.PageIndex = 1
	}
	if input.PageSize <= 0 {
		input.PageSize = 20
	}
	if input.PageSize > 200 {
		input.PageSize = 200
	}
	req := &models.PocListRequest{
		Search:    input.Search,
		PageIndex: input.PageIndex,
		PageSize:  input.PageSize,
	}
	req.Filter.Level = input.Level
	res, err := d.pocService.GetPocList(ginContext(ctx), req)
	if err != nil {
		return errorResult("获取 POC 列表失败", err)
	}
	// 200 条带完整 YAML 实测 12.4MB；列表只回摘要，正文用 get_poc。
	return jsonToolResult(truncateListFields(res, "content"))
}

type getPocInput struct {
	ID string `json:"id" jsonschema:"POC 的 MongoDB ObjectID"`
}

func getPoc(ctx context.Context, _ *mcp.CallToolRequest, input getPocInput) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(input.ID) == "" {
		return errorResult("id 不能为空", nil)
	}
	res, err := d.pocService.GetPocDetail(ginContext(ctx), &models.PocDetailRequest{ID: input.ID})
	if err != nil {
		return errorResult("获取 POC 详情失败", err)
	}
	return jsonToolResult(res)
}

type listFingerprintsInput struct {
	Search    string `json:"search,omitempty" jsonschema:"指纹名称/关键字模糊搜索"`
	PageIndex int    `json:"pageIndex,omitempty" jsonschema:"页码，默认 1"`
	PageSize  int    `json:"pageSize,omitempty" jsonschema:"每页条数，默认 20，最大 200"`
}

func listFingerprints(ctx context.Context, _ *mcp.CallToolRequest, input listFingerprintsInput) (*mcp.CallToolResult, any, error) {
	if input.PageIndex <= 0 {
		input.PageIndex = 1
	}
	if input.PageSize <= 0 {
		input.PageSize = 20
	}
	if input.PageSize > 200 {
		input.PageSize = 200
	}
	list, total, err := d.fingerprintService.List(ctx, input.Search, input.PageIndex, input.PageSize)
	if err != nil {
		return errorResult("获取指纹列表失败", err)
	}
	// rule 是整段指纹 YAML，600 条一页能到几百 KB；列表只回摘要。
	return jsonToolResult(truncateListFields(ginH{"list": list, "total": total}, "rule"))
}

type listDictionariesInput struct {
	Kind      string `json:"kind,omitempty" jsonschema:"字典类型：subdomain/dir/brute/cdn/other，默认 subdomain"`
	Search    string `json:"search,omitempty" jsonschema:"字典名称模糊搜索"`
	PageIndex int    `json:"pageIndex,omitempty" jsonschema:"页码，默认 1"`
	PageSize  int    `json:"pageSize,omitempty" jsonschema:"每页条数，默认 20，最大 200"`
}

func listDictionaries(ctx context.Context, _ *mcp.CallToolRequest, input listDictionariesInput) (*mcp.CallToolResult, any, error) {
	kind := input.Kind
	if strings.TrimSpace(kind) == "" {
		kind = "subdomain"
	}
	if input.PageIndex <= 0 {
		input.PageIndex = 1
	}
	if input.PageSize <= 0 {
		input.PageSize = 20
	}
	if input.PageSize > 200 {
		input.PageSize = 200
	}
	res, err := d.dictService.ListPaged(ctx, kind, input.Search, input.PageIndex, input.PageSize)
	if err != nil {
		return errorResult("获取字典列表失败", err)
	}
	return jsonToolResult(res)
}

type previewDictionaryInput struct {
	Kind  string `json:"kind,omitempty" jsonschema:"字典类型，默认 subdomain"`
	Name  string `json:"name" jsonschema:"字典名称"`
	Limit int    `json:"limit,omitempty" jsonschema:"预览行数，默认 50，上限 200"`
}

func previewDictionary(ctx context.Context, _ *mcp.CallToolRequest, input previewDictionaryInput) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(input.Name) == "" {
		return errorResult("name 不能为空", nil)
	}
	kind := input.Kind
	if strings.TrimSpace(kind) == "" {
		kind = "subdomain"
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	res, err := d.dictService.Preview(ctx, kind, input.Name, limit)
	if err != nil {
		return errorResult("预览字典失败", err)
	}
	return jsonToolResult(res)
}

type searchDictionaryLinesInput struct {
	Kind    string `json:"kind,omitempty" jsonschema:"字典类型，默认 subdomain"`
	Name    string `json:"name" jsonschema:"字典名称"`
	Keyword string `json:"keyword" jsonschema:"行内关键字"`
	Limit   int    `json:"limit,omitempty" jsonschema:"返回行数上限，默认 50，最大 200"`
}

func searchDictionaryLines(ctx context.Context, _ *mcp.CallToolRequest, input searchDictionaryLinesInput) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Keyword) == "" {
		return errorResult("name 和 keyword 不能为空", nil)
	}
	kind := input.Kind
	if strings.TrimSpace(kind) == "" {
		kind = "subdomain"
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	res, err := d.dictService.SearchLines(ctx, kind, input.Name, input.Keyword, limit)
	if err != nil {
		return errorResult("搜索字典内容失败", err)
	}
	return jsonToolResult(res)
}

type getDashboardStatsInput struct {
	Project string `json:"project,omitempty" jsonschema:"可选的项目名，空表示全部项目"`
}

func getDashboardStats(ctx context.Context, _ *mcp.CallToolRequest, input getDashboardStatsInput) (*mcp.CallToolResult, any, error) {
	projectID, err := resolveProjectID(ginContext(ctx), input.Project)
	if err != nil {
		return errorResult("解析项目失败", err)
	}
	res, err := d.dashboardService.Stats(ctx, models.DashboardStatsRequest{Project: projectID})
	if err != nil {
		return errorResult("获取看板统计失败", err)
	}
	return jsonToolResult(res)
}

func getConsoleInfo(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	res, err := d.dashboardService.ConsoleInfo(ctx)
	if err != nil {
		return errorResult("获取控制台信息失败", err)
	}
	return jsonToolResult(res)
}

// ---------------------------------------------------------------------------
// 写/控工具：任务 / 扫描策略 / 敏感信息规则
// ---------------------------------------------------------------------------

type taskIDInput struct {
	ID string `json:"id" jsonschema:"任务 ID"`
}

func getTaskProgress(ctx context.Context, _ *mcp.CallToolRequest, input struct {
	ID        string `json:"id" jsonschema:"任务 ID"`
	PageIndex int    `json:"pageIndex,omitempty" jsonschema:"页码，默认 1"`
	PageSize  int    `json:"pageSize,omitempty" jsonschema:"每页条数，默认 20"`
}) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(input.ID) == "" {
		return errorResult("id 不能为空", nil)
	}
	pi, ps := input.PageIndex, input.PageSize
	if pi <= 0 {
		pi = 1
	}
	if ps <= 0 {
		ps = 20
	}
	res, err := d.taskService.GetTaskProgress(ginContext(ctx), input.ID, pi, ps)
	if err != nil {
		return errorResult("获取任务进度失败", err)
	}
	return jsonToolResult(res)
}

func startTask(ctx context.Context, _ *mcp.CallToolRequest, input taskIDInput) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(input.ID) == "" {
		return errorResult("id 不能为空", nil)
	}
	if err := d.taskService.StartTasks(ginContext(ctx), []string{input.ID}); err != nil {
		return errorResult("启动任务失败", err)
	}
	return jsonToolResult(ginH{"ok": true, "id": input.ID})
}

func stopTask(ctx context.Context, _ *mcp.CallToolRequest, input taskIDInput) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(input.ID) == "" {
		return errorResult("id 不能为空", nil)
	}
	c := ginContext(ctx)
	// 先确认任务存在：之前对任意 id 都回 ok:true，调用方无法区分
	//「确实停了」和「id 打错了」（2026-11-28 审计）。
	detail, err := d.taskService.GetTaskDetail(c, input.ID)
	if err != nil {
		return errorResult("读取任务失败", err)
	}
	if detail == nil {
		return errorResult("任务不存在", nil)
	}
	prev := detail.Status
	if err := d.taskService.StopTasks(c, []string{input.ID}); err != nil {
		return errorResult("暂停任务失败", err)
	}
	res := ginH{"ok": true, "id": input.ID, "prevStatus": prev}
	if prev == 2 {
		res["note"] = "任务此前已是暂停状态"
	} else if prev == 3 {
		res["note"] = "任务已结束，暂停标记只用于阻止自动重投"
	}
	return jsonToolResult(res)
}

type deleteTaskInput struct {
	IDs          []string `json:"ids" jsonschema:"要删除的任务 ID 列表"`
	DeleteAssets bool     `json:"deleteAssets,omitempty" jsonschema:"是否同时删除任务产出的资产，默认 false"`
}

func deleteTask(ctx context.Context, _ *mcp.CallToolRequest, input deleteTaskInput) (*mcp.CallToolResult, any, error) {
	ids := make([]string, 0, len(input.IDs))
	for _, id := range input.IDs {
		if strings.TrimSpace(id) != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return errorResult("ids 不能为空", nil)
	}
	c := ginContext(ctx)
	// 先按 _id 查一遍真实存在的任务：之前直接回 deleted=len(ids)，
	// id 打错或者已被删掉也照样报「删了 N 个」（2026-11-28 审计）。
	found, err := d.taskService.GetTasksByIDs(c, ids)
	if err != nil {
		return errorResult("读取任务失败", err)
	}
	exist := make(map[string]bool, len(found))
	for _, t := range found {
		exist[t.ID.Hex()] = true
	}
	hits := make([]string, 0, len(exist))
	missing := make([]string, 0)
	for _, id := range ids {
		if exist[id] {
			hits = append(hits, id)
		} else {
			missing = append(missing, id)
		}
	}
	if len(hits) == 0 {
		return errorResult("任务不存在: "+strings.Join(missing, ", "), nil)
	}
	if err := d.taskService.DeleteTasks(c, hits, input.DeleteAssets); err != nil {
		return errorResult("删除任务失败", err)
	}
	res := ginH{"ok": true, "deleted": len(hits), "deleteAssets": input.DeleteAssets}
	if len(missing) > 0 {
		res["missing"] = missing
	}
	return jsonToolResult(res)
}

type listScanPoliciesInput struct {
	Keyword   string `json:"keyword,omitempty" jsonschema:"策略名称关键字"`
	Enabled   *bool  `json:"enabled,omitempty" jsonschema:"按启用状态过滤"`
	PageIndex int    `json:"pageIndex,omitempty" jsonschema:"页码，默认 1"`
	PageSize  int    `json:"pageSize,omitempty" jsonschema:"每页条数，默认 20，最大 200"`
}

func listScanPolicies(ctx context.Context, _ *mcp.CallToolRequest, input listScanPoliciesInput) (*mcp.CallToolResult, any, error) {
	if d.policyService == nil {
		return errorResult("扫描策略服务未初始化", nil)
	}
	pi, ps := input.PageIndex, input.PageSize
	if pi <= 0 {
		pi = 1
	}
	if ps <= 0 {
		ps = 20
	}
	if ps > 200 {
		ps = 200
	}
	list, total, err := d.policyService.List(ctx, models.ScanPolicyListQuery{
		Keyword: input.Keyword, Enabled: input.Enabled, PageIndex: pi, PageSize: ps,
	})
	if err != nil {
		return errorResult("获取扫描策略失败", err)
	}
	return jsonToolResult(ginH{"list": list, "total": total})
}

type createScanPolicyInput struct {
	Name        string         `json:"name" jsonschema:"策略名称"`
	Description string         `json:"description,omitempty" jsonschema:"策略描述"`
	Config      map[string]any `json:"config,omitempty" jsonschema:"策略配置对象（并发、超时、插件开关等），结构同 UI 导出"`
	Enabled     *bool          `json:"enabled,omitempty" jsonschema:"是否启用，默认 true"`
}

func createScanPolicy(ctx context.Context, _ *mcp.CallToolRequest, input createScanPolicyInput) (*mcp.CallToolResult, any, error) {
	if d.policyService == nil {
		return errorResult("扫描策略服务未初始化", nil)
	}
	if strings.TrimSpace(input.Name) == "" {
		return errorResult("name 不能为空", nil)
	}
	var cfg models.ScanPolicyConfig
	if len(input.Config) > 0 {
		raw, err := json.Marshal(input.Config)
		if err != nil {
			return errorResult("config 序列化失败", err)
		}
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return errorResult("config 结构不符合扫描策略格式", err)
		}
	}
	res, err := d.policyService.Create(ctx, models.ScanPolicyCreateRequest{
		Name: input.Name, Description: input.Description, Config: cfg, Enabled: input.Enabled,
	})
	if err != nil {
		return errorResult("创建扫描策略失败", err)
	}
	return jsonToolResult(res)
}

type deleteScanPoliciesInput struct {
	IDs []string `json:"ids" jsonschema:"要删除的策略 ID 列表"`
}

func deleteScanPolicies(ctx context.Context, _ *mcp.CallToolRequest, input deleteScanPoliciesInput) (*mcp.CallToolResult, any, error) {
	if d.policyService == nil {
		return errorResult("扫描策略服务未初始化", nil)
	}
	ids := make([]string, 0, len(input.IDs))
	for _, id := range input.IDs {
		if strings.TrimSpace(id) != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return errorResult("ids 不能为空", nil)
	}
	n, err := d.policyService.Delete(ctx, ids)
	if err != nil {
		return errorResult("删除扫描策略失败", err)
	}
	// 一条都没删掉说明策略不存在：不能回 ok:true（2026-11-28 全工具扫描实测）
	if n == 0 {
		return errorResult("策略不存在: "+strings.Join(ids, ","), nil)
	}
	return jsonToolResult(ginH{"ok": true, "deleted": n})
}

type listSensitiveRulesInput struct {
	Search    string `json:"search,omitempty" jsonschema:"规则名称/正则关键字"`
	PageIndex int    `json:"pageIndex,omitempty" jsonschema:"页码，默认 1"`
	PageSize  int    `json:"pageSize,omitempty" jsonschema:"每页条数，默认 20，最大 200"`
}

func listSensitiveRules(ctx context.Context, _ *mcp.CallToolRequest, input listSensitiveRulesInput) (*mcp.CallToolResult, any, error) {
	if d.sensitiveRuleService == nil {
		return errorResult("敏感信息规则服务未初始化", nil)
	}
	pi, ps := input.PageIndex, input.PageSize
	if pi <= 0 {
		pi = 1
	}
	if ps <= 0 {
		ps = 20
	}
	if ps > 200 {
		ps = 200
	}
	list, total, err := d.sensitiveRuleService.RuleList(ginContext(ctx), input.Search, pi, ps)
	if err != nil {
		return errorResult("获取敏感信息规则失败", err)
	}
	return jsonToolResult(ginH{"list": list, "total": total})
}

type setSensitiveRuleStateInput struct {
	IDs   []string `json:"ids" jsonschema:"规则 ID 列表"`
	State bool     `json:"state" jsonschema:"true 启用，false 停用"`
}

func setSensitiveRuleState(ctx context.Context, _ *mcp.CallToolRequest, input setSensitiveRuleStateInput) (*mcp.CallToolResult, any, error) {
	if d.sensitiveRuleService == nil {
		return errorResult("敏感信息规则服务未初始化", nil)
	}
	ids := make([]string, 0, len(input.IDs))
	for _, id := range input.IDs {
		if strings.TrimSpace(id) != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return errorResult("ids 不能为空", nil)
	}
	n, err := d.sensitiveRuleService.RuleUpdateState(ginContext(ctx), ids, input.State)
	if err != nil {
		return errorResult("更新规则状态失败", err)
	}
	// 0 条更新说明这些 id 都不存在（或状态本来就一致且没记录被匹配），别回 ok:true 冒充成功
	if n == 0 {
		return errorResult("规则不存在或状态未变化: "+strings.Join(ids, ","), nil)
	}
	return jsonToolResult(ginH{"ok": true, "updated": n, "state": input.State})
}

// ---------------------------------------------------------------------------
// 节点运维 + 字典维护
// ---------------------------------------------------------------------------

type nodeNameInput struct {
	Node string `json:"node" jsonschema:"节点名称"`
}

func listNodePlugins(ctx context.Context, _ *mcp.CallToolRequest, input nodeNameInput) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(input.Node) == "" {
		return errorResult("node 不能为空", nil)
	}
	res, err := d.nodeService.GetNodePlugin(ctx, input.Node)
	if err != nil {
		return errorResult("获取节点插件失败", err)
	}
	return jsonToolResult(ginH{"node": input.Node, "plugins": res})
}

type getNodeLogsInput struct {
	Node   string `json:"node" jsonschema:"节点名称"`
	TailKB int    `json:"tailKB,omitempty" jsonschema:"只取日志尾部多少 KB，默认 128，上限 1024；日志按时间追加，尾部才是现场"`
}

// mcpMaxLogTailKB 是 get_node_logs 单次返回的上限。
// 依据：node-jp-01 的日志一次读全量实测 318KB，排障只关心尾部（2026-11-28 实测）。
const mcpMaxLogTailKB = 1024

func getNodeLogs(ctx context.Context, _ *mcp.CallToolRequest, input getNodeLogsInput) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(input.Node) == "" {
		return errorResult("node 不能为空", nil)
	}
	tailKB := input.TailKB
	if tailKB <= 0 {
		tailKB = 128
	}
	if tailKB > mcpMaxLogTailKB {
		tailKB = mcpMaxLogTailKB
	}
	res, err := d.nodeService.GetNodeLogs(ctx, input.Node)
	if err != nil {
		return errorResult("读取节点日志失败", err)
	}
	out := ginH{"node": input.Node, "logBytes": len(res), "tailKB": tailKB}
	limit := tailKB * 1024
	if len(res) > limit {
		// 按字节切尾部再从第一个换行处对齐，避免把半行/半个字符甩给调用方
		cut := res[len(res)-limit:]
		if i := strings.IndexByte(cut, '\n'); i >= 0 && i+1 < len(cut) {
			cut = cut[i+1:]
		}
		out["logs"] = cut
		out["truncated"] = true
		out["headBytes"] = len(res) - len(cut)
		out["note"] = "只返回日志尾部；需要更早内容请调大 tailKB（上限 1024）"
	} else {
		out["logs"] = res
		out["truncated"] = false
	}
	return jsonToolResult(out)
}

// knownNodes 返回当前节点名集合。
// 用途：动作类工具（重启/插件重检/卸载）在「节点配错名字」时必须报错——
// 之前它们对不存在的节点也返回 ok:true，调用方（尤其 AI agent）会以为指令已下发，
// 实际 RefreshConfig 只往一个不存在的节点名写了条配置，什么也没发生（2026-11-28 审计）。
func knownNodes(ctx context.Context) (map[string]bool, error) {
	nodes, err := d.nodeService.GetNodeData(ctx, false)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		set[n.Name] = true
	}
	return set, nil
}

// requireNode 校验节点存在，并把「现在有哪些节点」带进错误里，省掉调用方再查一次。
func requireNode(ctx context.Context, name string) error {
	set, err := knownNodes(ctx)
	if err != nil {
		return err
	}
	if !set[name] {
		avail := make([]string, 0, len(set))
		for n := range set {
			avail = append(avail, n)
		}
		sort.Strings(avail)
		return fmt.Errorf("节点不存在: %s（当前节点: %s）", name, strings.Join(avail, ", "))
	}
	return nil
}

func restartNode(ctx context.Context, _ *mcp.CallToolRequest, input nodeNameInput) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(input.Node) == "" {
		return errorResult("node 不能为空", nil)
	}
	if err := requireNode(ctx, input.Node); err != nil {
		return errorResult("重启节点失败", err)
	}
	if err := d.nodeService.RestartNode(ctx, input.Node); err != nil {
		return errorResult("重启节点失败", err)
	}
	return jsonToolResult(ginH{"ok": true, "node": input.Node})
}

type deleteNodesInput struct {
	Names []string `json:"names" jsonschema:"节点名称列表"`
}

func deleteNodes(ctx context.Context, _ *mcp.CallToolRequest, input deleteNodesInput) (*mcp.CallToolResult, any, error) {
	names := make([]string, 0, len(input.Names))
	for _, n := range input.Names {
		if strings.TrimSpace(n) != "" {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return errorResult("names 不能为空", nil)
	}
	set, err := knownNodes(ctx)
	if err != nil {
		return errorResult("删除节点失败", err)
	}
	hits := make([]string, 0, len(names))
	missing := make([]string, 0)
	for _, n := range names {
		if set[n] {
			hits = append(hits, n)
		} else {
			missing = append(missing, n)
		}
	}
	if len(hits) == 0 {
		return errorResult("节点不存在: "+strings.Join(missing, ", "), nil)
	}
	if err := d.nodeService.DeleteNodes(ctx, hits); err != nil {
		return errorResult("删除节点失败", err)
	}
	res := ginH{"ok": true, "deleted": len(hits)}
	if len(missing) > 0 {
		res["missing"] = missing
	}
	return jsonToolResult(res)
}

type createDictionaryInput struct {
	Kind    string   `json:"kind" jsonschema:"字典类型：subdomain/dir/brute/cdn/other"`
	Name    string   `json:"name" jsonschema:"字典名称"`
	Content string   `json:"content,omitempty" jsonschema:"字典内容，换行分隔；与 lines 二选一"`
	Lines   []string `json:"lines,omitempty" jsonschema:"字典行数组；与 content 二选一"`
}

func createDictionary(ctx context.Context, _ *mcp.CallToolRequest, input createDictionaryInput) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Kind) == "" {
		return errorResult("kind 和 name 不能为空", nil)
	}
	if d.dictService == nil {
		return errorResult("字典服务未初始化", nil)
	}
	content := input.Content
	if content == "" && len(input.Lines) > 0 {
		content = strings.Join(input.Lines, "\n")
	}
	if strings.TrimSpace(content) == "" {
		return errorResult("content/lines 不能同时为空", nil)
	}
	res, err := d.dictService.CreateByKind(ctx, input.Kind, input.Name, content)
	if err != nil {
		return errorResult("创建字典失败", err)
	}
	return jsonToolResult(res)
}

type appendDictionaryLinesInput struct {
	Kind  string   `json:"kind" jsonschema:"字典类型"`
	Name  string   `json:"name" jsonschema:"字典名称"`
	Lines []string `json:"lines" jsonschema:"要追加的行"`
}

func appendDictionaryLines(ctx context.Context, _ *mcp.CallToolRequest, input appendDictionaryLinesInput) (*mcp.CallToolResult, any, error) {
	if d.dictService == nil {
		return errorResult("字典服务未初始化", nil)
	}
	if strings.TrimSpace(input.Name) == "" || len(input.Lines) == 0 {
		return errorResult("name 和 lines 不能为空", nil)
	}
	res, err := d.dictService.Append(ctx, input.Kind, input.Name, input.Lines)
	if err != nil {
		return errorResult("追加字典行失败", err)
	}
	return jsonToolResult(res)
}

type deleteDictionaryInput struct {
	Kind string `json:"kind" jsonschema:"字典类型"`
	Name string `json:"name" jsonschema:"字典名称"`
}

func deleteDictionary(ctx context.Context, _ *mcp.CallToolRequest, input deleteDictionaryInput) (*mcp.CallToolResult, any, error) {
	if d.dictService == nil {
		return errorResult("字典服务未初始化", nil)
	}
	if strings.TrimSpace(input.Name) == "" {
		return errorResult("name 不能为空", nil)
	}
	res, err := d.dictService.DeleteFileByName(ctx, input.Kind, input.Name)
	if err != nil {
		return errorResult("删除字典失败", err)
	}
	return jsonToolResult(res)
}

type deleteDictionaryEntriesInput struct {
	Kind    string   `json:"kind" jsonschema:"字典类型"`
	Name    string   `json:"name" jsonschema:"字典名称"`
	Entries []string `json:"entries" jsonschema:"要删除的行"`
}

func deleteDictionaryEntries(ctx context.Context, _ *mcp.CallToolRequest, input deleteDictionaryEntriesInput) (*mcp.CallToolResult, any, error) {
	if d.dictService == nil {
		return errorResult("字典服务未初始化", nil)
	}
	if strings.TrimSpace(input.Name) == "" || len(input.Entries) == 0 {
		return errorResult("name 和 entries 不能为空", nil)
	}
	res, err := d.dictService.DeleteEntries(ctx, input.Kind, input.Name, input.Entries)
	if err != nil {
		return errorResult("删除字典行失败", err)
	}
	return jsonToolResult(res)
}

// ---------------------------------------------------------------------------
// 端口字典（{dict.port.*} 的数据源）
// ---------------------------------------------------------------------------

type listPortDictInput struct {
	Search    string `json:"search,omitempty" jsonschema:"按名称/值模糊搜索"`
	PageIndex int    `json:"pageIndex,omitempty" jsonschema:"页码，默认 1"`
	PageSize  int    `json:"pageSize,omitempty" jsonschema:"每页条数，默认 20，最大 200"`
}

func listPortDict(ctx context.Context, _ *mcp.CallToolRequest, input listPortDictInput) (*mcp.CallToolResult, any, error) {
	if d.portDictService == nil {
		return errorResult("端口字典服务未初始化", nil)
	}
	pi, ps := input.PageIndex, input.PageSize
	capPageSize(&pi, &ps)
	list, total, err := d.portDictService.Get(ctx, strings.TrimSpace(input.Search), pi, ps)
	if err != nil {
		return errorResult("获取端口字典失败", err)
	}
	return jsonToolResult(ginH{"list": list, "total": total, "pageIndex": pi, "pageSize": ps})
}

type setPortDictEntryInput struct {
	ID    string `json:"id,omitempty" jsonschema:"已有条目的 id；留空表示新增"`
	Name  string `json:"name" jsonschema:"字典键（模板里写 {dict.port.<name>}）"`
	Value string `json:"value" jsonschema:"端口列表值，例如 80,443"`
}

func setPortDictEntry(ctx context.Context, _ *mcp.CallToolRequest, input setPortDictEntryInput) (*mcp.CallToolResult, any, error) {
	if d.portDictService == nil {
		return errorResult("端口字典服务未初始化", nil)
	}
	name := strings.TrimSpace(input.Name)
	value := strings.TrimSpace(input.Value)
	if name == "" {
		return errorResult("name 不能为空", nil)
	}
	if value == "" {
		return errorResult("value 不能为空", nil)
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		if err := d.portDictService.Add(ctx, name, value); err != nil {
			return errorResult("新增端口字典失败", err)
		}
		return jsonToolResult(ginH{"ok": true, "action": "created", "name": name})
	}
	if err := d.portDictService.Update(ctx, id, name, value); err != nil {
		return errorResult("修改端口字典失败", err)
	}
	return jsonToolResult(ginH{"ok": true, "action": "updated", "id": id, "name": name})
}

type deletePortDictEntriesInput struct {
	IDs     []string `json:"ids" jsonschema:"要删除的条目 id 列表"`
	Confirm bool     `json:"confirm,omitempty" jsonschema:"必须显式传 true：删除不可逆"`
}

func deletePortDictEntries(ctx context.Context, _ *mcp.CallToolRequest, input deletePortDictEntriesInput) (*mcp.CallToolResult, any, error) {
	if d.portDictService == nil {
		return errorResult("端口字典服务未初始化", nil)
	}
	ids := make([]string, 0, len(input.IDs))
	for _, id := range input.IDs {
		if v := strings.TrimSpace(id); v != "" {
			ids = append(ids, v)
		}
	}
	if len(ids) == 0 {
		return errorResult("ids 不能为空", nil)
	}
	// 与 delete_asset_groups 同一口径：不可逆操作必须显式确认
	if !input.Confirm {
		return errorResult(`拒绝执行: confirm is required: delete_port_dict_entries 是不可逆操作，确认执行请在参数里显式传 "confirm": true`, nil)
	}
	n, err := d.portDictService.Delete(ctx, ids)
	if err != nil {
		return errorResult("删除端口字典失败", err)
	}
	if n == 0 {
		return errorResult("条目不存在: "+strings.Join(ids, ","), nil)
	}
	return jsonToolResult(ginH{"ok": true, "deleted": n})
}

// ---------------------------------------------------------------------------
// 资产读模型（nuclei / WIH / 指纹统计）与 GitHub 泄露结果
// ---------------------------------------------------------------------------

type listNucleiResultsInput struct {
	Project    string `json:"project,omitempty" jsonschema:"项目名，精确匹配"`
	Severity   string `json:"severity,omitempty" jsonschema:"严重级别，如 high / critical"`
	TemplateID string `json:"templateId,omitempty" jsonschema:"nuclei 模板 ID"`
	Keyword    string `json:"keyword,omitempty" jsonschema:"按 URL/名称模糊搜索"`
	TaskName   string `json:"taskName,omitempty" jsonschema:"按任务名过滤"`
	Host       string `json:"host,omitempty" jsonschema:"按主机名过滤"`
	Status     int    `json:"status,omitempty" jsonschema:"处置状态：0 全部 1 未处理 2 处理中 3 忽略 4 疑似 5 确认 6 已处理"`
	PageIndex  int    `json:"pageIndex,omitempty" jsonschema:"页码，默认 1"`
	PageSize   int    `json:"pageSize,omitempty" jsonschema:"每页条数，默认 20，最大 200"`
}

func listNucleiResults(ctx context.Context, _ *mcp.CallToolRequest, input listNucleiResultsInput) (*mcp.CallToolResult, any, error) {
	if d.nucleiService == nil {
		return errorResult("nuclei 结果服务未初始化", nil)
	}
	pi, ps := input.PageIndex, input.PageSize
	capPageSize(&pi, &ps)
	res, err := d.nucleiService.List(ctx, models.NucleiResultListRequest{
		Project:    strings.TrimSpace(input.Project),
		Severity:   strings.TrimSpace(input.Severity),
		TemplateID: strings.TrimSpace(input.TemplateID),
		Keyword:    strings.TrimSpace(input.Keyword),
		TaskName:   strings.TrimSpace(input.TaskName),
		Host:       strings.TrimSpace(input.Host),
		Status:     input.Status,
		PageIndex:  pi,
		PageSize:   ps,
	})
	if err != nil {
		return errorResult("获取 nuclei 结果失败", err)
	}
	return jsonToolResult(res)
}

type listWIHResultsInput struct {
	Type      string `json:"type,omitempty" jsonschema:"命中规则名（sid）"`
	Project   string `json:"project,omitempty" jsonschema:"项目名，精确匹配"`
	Keyword   string `json:"keyword,omitempty" jsonschema:"按 URL / 命中内容模糊搜索"`
	PageIndex int    `json:"pageIndex,omitempty" jsonschema:"页码，默认 1"`
	PageSize  int    `json:"pageSize,omitempty" jsonschema:"每页条数，默认 20，最大 200"`
}

func listWIHResults(ctx context.Context, _ *mcp.CallToolRequest, input listWIHResultsInput) (*mcp.CallToolResult, any, error) {
	if d.wihService == nil {
		return errorResult("WIH 服务未初始化", nil)
	}
	pi, ps := input.PageIndex, input.PageSize
	capPageSize(&pi, &ps)
	res, err := d.wihService.List(ctx, models.WIHListRequest{
		Type:      strings.TrimSpace(input.Type),
		Project:   strings.TrimSpace(input.Project),
		Keyword:   strings.TrimSpace(input.Keyword),
		PageIndex: pi,
		PageSize:  ps,
	})
	if err != nil {
		return errorResult("获取 WIH 结果失败", err)
	}
	return jsonToolResult(res)
}

type listStatFingerInput struct {
	Keyword   string `json:"keyword,omitempty" jsonschema:"技术名子串，不区分大小写"`
	Project   string `json:"project,omitempty" jsonschema:"项目名，精确匹配"`
	PageIndex int    `json:"pageIndex,omitempty" jsonschema:"页码，默认 1"`
	PageSize  int    `json:"pageSize,omitempty" jsonschema:"每页条数，默认 20，最大 200"`
}

func listStatFinger(ctx context.Context, _ *mcp.CallToolRequest, input listStatFingerInput) (*mcp.CallToolResult, any, error) {
	if d.statFingerService == nil {
		return errorResult("指纹统计服务未初始化", nil)
	}
	pi, ps := input.PageIndex, input.PageSize
	capPageSize(&pi, &ps)
	res, err := d.statFingerService.ListTechnologies(ctx, models.StatFingerListRequest{
		Keyword:   strings.TrimSpace(input.Keyword),
		Project:   strings.TrimSpace(input.Project),
		PageIndex: pi,
		PageSize:  ps,
	})
	if err != nil {
		return errorResult("获取指纹统计失败", err)
	}
	return jsonToolResult(res)
}

type listGithubResultsInput struct {
	TaskID    string `json:"taskId,omitempty" jsonschema:"采集任务 ID"`
	Type      string `json:"type,omitempty" jsonschema:"结果类型：code_leak / cve / hacker"`
	Status    string `json:"status,omitempty" jsonschema:"研判状态：new / triaged / ignored"`
	Project   string `json:"project,omitempty" jsonschema:"项目名"`
	Keyword   string `json:"keyword,omitempty" jsonschema:"按仓库 / 路径 / 标题模糊搜索"`
	PageIndex int    `json:"pageIndex,omitempty" jsonschema:"页码，默认 1"`
	PageSize  int    `json:"pageSize,omitempty" jsonschema:"每页条数，默认 20，最大 200"`
}

func listGithubResults(ctx context.Context, _ *mcp.CallToolRequest, input listGithubResultsInput) (*mcp.CallToolResult, any, error) {
	if d.githubResultRepo == nil {
		return errorResult("GitHub 结果仓库未初始化", nil)
	}
	pi, ps := input.PageIndex, input.PageSize
	capPageSize(&pi, &ps)
	list, total, err := d.githubResultRepo.List(ctx, models.GithubResultListQuery{
		TaskID:    strings.TrimSpace(input.TaskID),
		Type:      strings.TrimSpace(input.Type),
		Status:    strings.TrimSpace(input.Status),
		Project:   strings.TrimSpace(input.Project),
		Keyword:   strings.TrimSpace(input.Keyword),
		PageIndex: pi,
		PageSize:  ps,
	})
	if err != nil {
		return errorResult("获取 GitHub 泄露结果失败", err)
	}
	return jsonToolResult(ginH{"list": list, "total": total, "pageIndex": pi, "pageSize": ps})
}

// ---------------------------------------------------------------------------
// 数据导出
// ---------------------------------------------------------------------------

func listExportRecords(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	if d.exportService == nil {
		return errorResult("导出服务未初始化", nil)
	}
	records, err := d.exportService.GetExportRecords(ctx)
	if err != nil {
		return errorResult("获取导出记录失败", err)
	}
	return jsonToolResult(ginH{"list": records})
}

type getExportFieldsInput struct {
	Index string `json:"index" jsonschema:"数据类型，如 asset/subdomain/url/ip/poc/vulnerability"`
}

func getExportFields(ctx context.Context, _ *mcp.CallToolRequest, input getExportFieldsInput) (*mcp.CallToolResult, any, error) {
	if d.exportService == nil {
		return errorResult("导出服务未初始化", nil)
	}
	if strings.TrimSpace(input.Index) == "" {
		return errorResult("index 不能为空", nil)
	}
	fields, err := d.exportService.GetExportFields(ctx, input.Index)
	if err != nil {
		return errorResult("获取导出字段失败", err)
	}
	return jsonToolResult(ginH{"index": input.Index, "fields": fields})
}

type createExportTaskInput struct {
	Index            string                   `json:"index" jsonschema:"数据类型，如 asset/subdomain/url/ip"`
	Fields           []string                 `json:"fields" jsonschema:"要导出的字段列表，可先用 get_export_fields 查询"`
	FileType         string                   `json:"fileType,omitempty" jsonschema:"文件类型：xlsx/json，默认 xlsx（csv 会被导出文件名校验拒绝）"`
	Quantity         int                      `json:"quantity,omitempty" jsonschema:"导出行数上限，默认 1000，最大 200000"`
	SearchExpression string                   `json:"searchExpression,omitempty" jsonschema:"搜索表达式，语法同列表页搜索框"`
	Filter           map[string][]interface{} `json:"filter,omitempty" jsonschema:"字段过滤条件"`
}

func createExportTask(ctx context.Context, _ *mcp.CallToolRequest, input createExportTaskInput) (*mcp.CallToolResult, any, error) {
	if d.exportService == nil {
		return errorResult("导出服务未初始化", nil)
	}
	if strings.TrimSpace(input.Index) == "" || len(input.Fields) == 0 {
		return errorResult("index 与 fields 不能为空", nil)
	}
	fileType := input.FileType
	if strings.TrimSpace(fileType) == "" {
		fileType = "xlsx"
	}
	switch fileType {
	case "xlsx", "json":
	default:
		return errorResult("fileType 只支持 xlsx/json", nil)
	}
	qty := input.Quantity
	if qty <= 0 {
		qty = 1000
	}
	if qty > 200000 {
		qty = 200000
	}
	searchQuery, err := helper.GetSearchQuery(models.SearchRequest{
		Index:            input.Index,
		SearchExpression: input.SearchExpression,
		Filter:           input.Filter,
	})
	if err != nil {
		return errorResult("构建查询条件失败", err)
	}
	fileName := uuid.NewString() + "." + fileType
	if err := d.exportService.CreateExportTask(ctx, &models.ExportTask{
		FileName:   fileName,
		CreateTime: helper.GetNowTimeString(),
		Quantity:   qty,
		DataType:   input.Index,
		FileType:   fileType,
		State:      0,
	}); err != nil {
		return errorResult("创建导出任务失败", err)
	}
	go d.exportService.ProcessExportTask(context.Background(), models.ExportRun{
		Filter:   searchQuery,
		FileName: fileName,
		Index:    input.Index,
		FileType: fileType,
		Field:    input.Fields,
		Quantity: qty,
	})
	return jsonToolResult(ginH{"ok": true, "fileName": fileName, "quantity": qty, "fileType": fileType})
}

type deleteExportFilesInput struct {
	FileNames []string `json:"fileNames" jsonschema:"导出文件名列表（来自 list_export_records）"`
}

func deleteExportFiles(ctx context.Context, _ *mcp.CallToolRequest, input deleteExportFilesInput) (*mcp.CallToolResult, any, error) {
	if d.exportService == nil {
		return errorResult("导出服务未初始化", nil)
	}
	names := make([]string, 0, len(input.FileNames))
	for _, n := range input.FileNames {
		if strings.TrimSpace(n) == "" {
			continue
		}
		if !d.exportService.IsValidFileName(n) {
			return errorResult("非法文件名: "+n, nil)
		}
		names = append(names, n)
	}
	if len(names) == 0 {
		return errorResult("fileNames 不能为空", nil)
	}
	// 先落盘检查：导出记录和文件是两套数据（文件可能已被清理/从未落盘，
	// 记录也可能已被删），只回 len(names) 无法判断到底删了几份（2026-11-28 审计）。
	base := config.GlobalConfig.System.ExportPath()
	onDisk := make([]string, 0, len(names))
	noFile := make([]string, 0)
	for _, n := range names {
		if _, statErr := os.Stat(filepath.Join(base, n)); statErr == nil {
			onDisk = append(onDisk, n)
		} else {
			noFile = append(noFile, n)
		}
	}
	records, err := d.exportService.DeleteExportFiles(ctx, names)
	if err != nil {
		return errorResult("删除导出文件失败", err)
	}
	res := ginH{"ok": true, "deleted": len(names), "filesRemoved": len(onDisk), "recordsRemoved": records}
	if len(noFile) > 0 {
		res["missingFiles"] = noFile
	}
	return jsonToolResult(res)
}

// ---------------------------------------------------------------------------
// 插件安装 / 重检 / 卸载
// ---------------------------------------------------------------------------

type pluginNodeInput struct {
	Node   string `json:"node" jsonschema:"节点名称"`
	Hash   string `json:"hash" jsonschema:"插件 hash（可用 list_plugins 查）"`
	Module string `json:"module" jsonschema:"插件所属模块，如 AssetMapping / PortScan"`
}

// requireNodePlugin 校验「节点存在 + 该节点确实装着这个插件」。
// 之前 recheck/reinstall/uninstall 三个工具对着任意 node/hash 组合都回 ok:true，
// 运维脚本据此判断「已重装」会完全误判（2026-11-28 审计）。
func requireNodePlugin(ctx context.Context, nodeName, hash string) error {
	if err := requireNode(ctx, nodeName); err != nil {
		return err
	}
	list, err := d.nodeService.GetNodePlugin(ctx, nodeName)
	if err != nil {
		return err
	}
	for _, p := range list {
		if p.Hash == hash {
			return nil
		}
	}
	return fmt.Errorf("节点 %s 上未安装插件 hash=%s", nodeName, hash)
}

func reinstallPlugin(ctx context.Context, _ *mcp.CallToolRequest, input pluginNodeInput) (*mcp.CallToolResult, any, error) {
	if d.pluginService == nil {
		return errorResult("插件服务未初始化", nil)
	}
	if strings.TrimSpace(input.Node) == "" || strings.TrimSpace(input.Hash) == "" {
		return errorResult("node 与 hash 不能为空", nil)
	}
	if err := requireNodePlugin(ctx, input.Node, input.Hash); err != nil {
		return errorResult("重新安装插件失败", err)
	}
	if err := d.pluginService.Reinstall(ginContext(ctx), &models.PluginReinstallRequest{
		Node: input.Node, Hash: input.Hash, Module: input.Module,
	}); err != nil {
		return errorResult("重新安装插件失败", err)
	}
	return jsonToolResult(ginH{"ok": true, "node": input.Node, "hash": input.Hash, "action": "reinstall"})
}

func recheckPlugin(ctx context.Context, _ *mcp.CallToolRequest, input pluginNodeInput) (*mcp.CallToolResult, any, error) {
	if d.pluginService == nil {
		return errorResult("插件服务未初始化", nil)
	}
	if strings.TrimSpace(input.Node) == "" || strings.TrimSpace(input.Hash) == "" || strings.TrimSpace(input.Module) == "" {
		return errorResult("node / hash / module 都不能为空", nil)
	}
	if err := requireNodePlugin(ctx, input.Node, input.Hash); err != nil {
		return errorResult("重新检查插件失败", err)
	}
	if err := d.pluginService.Recheck(ginContext(ctx), &models.PluginRecheckRequest{
		Node: input.Node, Hash: input.Hash, Module: input.Module,
	}); err != nil {
		return errorResult("重新检查插件失败", err)
	}
	return jsonToolResult(ginH{"ok": true, "node": input.Node, "hash": input.Hash, "action": "recheck"})
}

func uninstallPlugin(ctx context.Context, _ *mcp.CallToolRequest, input pluginNodeInput) (*mcp.CallToolResult, any, error) {
	if d.pluginService == nil {
		return errorResult("插件服务未初始化", nil)
	}
	if strings.TrimSpace(input.Node) == "" || strings.TrimSpace(input.Hash) == "" || strings.TrimSpace(input.Module) == "" {
		return errorResult("node / hash / module 都不能为空", nil)
	}
	if err := requireNodePlugin(ctx, input.Node, input.Hash); err != nil {
		return errorResult("卸载插件失败", err)
	}
	if err := d.pluginService.Uninstall(ginContext(ctx), &models.PluginUninstallRequest{
		Node: input.Node, Hash: input.Hash, Module: input.Module,
	}); err != nil {
		return errorResult("卸载插件失败", err)
	}
	return jsonToolResult(ginH{"ok": true, "node": input.Node, "hash": input.Hash, "action": "uninstall"})
}

// ---------------------------------------------------------------------------
// 资产组 / 地契 / ICP 任务 / 服务端操作日志（只读扩展）
//
// 入参字段与后端 handler 严格同名同义（逐个读签名得来，不猜）：
//   - assetgroup.ListGroups / ListScopes → internal/api/handlers/assets/assetgroup/handler.go
//     ListGroups 无入参；ListScopes 绑 {groupId, search, pageIndex, pageSize}
//   - icp.ListTasks → internal/api/handlers/icp/handler.go 的 taskListRequest
//     {state, type, keyword, provider, pageIndex, pageSize} → models.IcpTaskListQuery
//   - audit.List → internal/api/handlers/audit/audit.go 直接绑 models.AuditLogQuery
//     {user, path, method, statusMin, pageIndex, pageSize}
//
// 与 REST 的唯一差别：MCP 不经 gin 绑定，handler 层的校验（分页上限、闭集枚举）
// 由 mcpguard.Normalize* 补回来，避免「调用成功 + 空结果」这类静默错误。
// ---------------------------------------------------------------------------

func listAssetGroups(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	if d.assetGroupService == nil {
		return errorResult("资产组服务未初始化", nil)
	}
	groups, err := d.assetGroupService.ListGroups(ctx)
	if err != nil {
		return errorResult("获取资产组列表失败", err)
	}
	if groups == nil {
		// 与 HTTP handler 一致：空列表返回 []，不是 null
		groups = []models.AssetGroup{}
	}
	return jsonToolResult(ginH{"items": groups, "total": len(groups)})
}

type listAssetScopesInput struct {
	GroupID   string `json:"groupId,omitempty" jsonschema:"按集团过滤：集团 ObjectID（list_asset_groups 返回的 id）；unassigned / 0 / 空 表示未分组"`
	Search    string `json:"search,omitempty" jsonschema:"地契名称或范围文本模糊搜索（不区分大小写）"`
	PageIndex int    `json:"pageIndex,omitempty" jsonschema:"页码，从 1 开始，默认 1"`
	PageSize  int    `json:"pageSize,omitempty" jsonschema:"每页条数，默认 20，最大 500"`
}

func listAssetScopes(ctx context.Context, _ *mcp.CallToolRequest, input listAssetScopesInput) (*mcp.CallToolResult, any, error) {
	if d.assetGroupService == nil {
		return errorResult("资产组服务未初始化", nil)
	}
	groupID, err := mcpguard.NormalizeScopeGroupID(input.GroupID)
	if err != nil {
		return errorResult("参数不合法", err)
	}
	pageIndex, pageSize, err := mcpguard.NormalizePaging(input.PageIndex, input.PageSize,
		mcpguard.DefaultPageSize, mcpguard.MaxPageSizeAssetScope)
	if err != nil {
		return errorResult("参数不合法", err)
	}
	items, total, err := d.assetGroupService.ListScopes(ctx, groupID, strings.TrimSpace(input.Search), pageIndex, pageSize)
	if err != nil {
		return errorResult("获取地契列表失败", err)
	}
	if items == nil {
		items = []models.AssetScope{}
	}
	return jsonToolResult(ginH{
		"list":      items,
		"total":     total,
		"pageIndex": pageIndex,
		"pageSize":  pageSize,
	})
}

type listICPTasksInput struct {
	State     *int   `json:"state,omitempty" jsonschema:"按状态过滤：0 待开始 / 1 运行中 / 2 已暂停 / 3 已完成；不传表示全部"`
	Type      string `json:"type,omitempty" jsonschema:"按任务类型过滤：company（企业名查域名）/ domain（域名查主体）；不传表示全部"`
	Keyword   string `json:"keyword,omitempty" jsonschema:"任务名或查询目标模糊搜索（后端对 name / queries 做不区分大小写正则）"`
	Provider  string `json:"provider,omitempty" jsonschema:"按数据来源过滤：manual / none / http；不传表示全部"`
	PageIndex int    `json:"pageIndex,omitempty" jsonschema:"页码，从 1 开始，默认 1"`
	PageSize  int    `json:"pageSize,omitempty" jsonschema:"每页条数，默认 20，最大 200"`
}

func listICPTasks(ctx context.Context, _ *mcp.CallToolRequest, input listICPTasksInput) (*mcp.CallToolResult, any, error) {
	if d.icpService == nil {
		return errorResult("ICP 服务未初始化", nil)
	}
	state, err := mcpguard.NormalizeICPTaskState(input.State)
	if err != nil {
		return errorResult("参数不合法", err)
	}
	taskType, err := mcpguard.NormalizeICPTaskType(input.Type)
	if err != nil {
		return errorResult("参数不合法", err)
	}
	provider, err := mcpguard.NormalizeICPProvider(input.Provider)
	if err != nil {
		return errorResult("参数不合法", err)
	}
	pageIndex, pageSize, err := mcpguard.NormalizePaging(input.PageIndex, input.PageSize,
		mcpguard.DefaultPageSize, mcpguard.MaxPageSizeICP)
	if err != nil {
		return errorResult("参数不合法", err)
	}
	list, total, err := d.icpService.ListTasks(ctx, models.IcpTaskListQuery{
		State:     state,
		Type:      taskType,
		Keyword:   strings.TrimSpace(input.Keyword),
		Provider:  provider,
		PageIndex: pageIndex,
		PageSize:  pageSize,
	})
	if err != nil {
		return errorResult("获取 ICP 任务列表失败", err)
	}
	if list == nil {
		list = []models.IcpTask{}
	}
	return jsonToolResult(ginH{
		"list":      list,
		"total":     total,
		"pageIndex": pageIndex,
		"pageSize":  pageSize,
	})
}

type listAuditLogsInput struct {
	User      string `json:"user,omitempty" jsonschema:"按操作者用户名模糊搜索（不区分大小写）"`
	Path      string `json:"path,omitempty" jsonschema:"按请求路径模糊搜索，如 /api/task/add"`
	Method    string `json:"method,omitempty" jsonschema:"按 HTTP 方法精确过滤：GET/POST/PUT/PATCH/DELETE/HEAD/OPTIONS（大小写不敏感）"`
	StatusMin int    `json:"statusMin,omitempty" jsonschema:"状态码下界：0 不过滤；给 400 表示只看 4xx/5xx，给 500 表示只看 5xx（范围 100-599）"`
	PageIndex int    `json:"pageIndex,omitempty" jsonschema:"页码，从 1 开始，默认 1"`
	PageSize  int    `json:"pageSize,omitempty" jsonschema:"每页条数，默认 20，最大 200"`
}

func listAuditLogs(ctx context.Context, _ *mcp.CallToolRequest, input listAuditLogsInput) (*mcp.CallToolResult, any, error) {
	if d.auditRepo == nil {
		return errorResult("审计仓库未初始化", nil)
	}
	method, err := mcpguard.NormalizeAuditMethod(input.Method)
	if err != nil {
		return errorResult("参数不合法", err)
	}
	statusMin, err := mcpguard.NormalizeAuditStatusMin(input.StatusMin)
	if err != nil {
		return errorResult("参数不合法", err)
	}
	pageIndex, pageSize, err := mcpguard.NormalizePaging(input.PageIndex, input.PageSize,
		mcpguard.DefaultPageSize, mcpguard.MaxPageSizeAudit)
	if err != nil {
		return errorResult("参数不合法", err)
	}
	list, total, err := d.auditRepo.List(ctx, models.AuditLogQuery{
		User:      strings.TrimSpace(input.User),
		Path:      strings.TrimSpace(input.Path),
		Method:    method,
		StatusMin: statusMin,
		PageIndex: pageIndex,
		PageSize:  pageSize,
	})
	if err != nil {
		return errorResult("获取操作日志失败", err)
	}
	if list == nil {
		list = []models.AuditLog{}
	}
	return jsonToolResult(ginH{
		"list":      list,
		"total":     total,
		"pageIndex": pageIndex,
		"pageSize":  pageSize,
	})
}

// ---------------------------------------------------------------------------
// 写侧第一批（v2.0.88）：资产组 / 地契 / ICP 任务
//
// 与只读侧一致的约定：入参字段与后端 handler / models 结构逐字同名，
// handler 里的 binding / 闭集 / 上限由 mcpguard.Normalize* 在调用服务前补回。
// 与只读侧不同的约定：所有工具都**先校验再落库**，任何越界都是报错，不静默截断；
// 删除类不可逆操作额外要求 confirm=true（见 mcpguard.RequireConfirm）。
//
// 为什么不在 internal/mcp 里做校验：本包 init 依赖真实 MongoDB，
// 这里的逻辑无法在无库环境单测；mcpguard 是纯函数层，验证成本最低。
// ---------------------------------------------------------------------------

type createAssetGroupInput struct {
	Name        string `json:"name" jsonschema:"集团名称，必填，1-64 个字符，全局唯一（重名报 asset group name already exists，不覆盖）"`
	Description string `json:"description,omitempty" jsonschema:"集团说明，可选，会去首尾空白"`
	SortOrder   int    `json:"sortOrder,omitempty" jsonschema:"排序值，可选，列表按它升序"`
}

func createAssetGroup(ctx context.Context, _ *mcp.CallToolRequest, input createAssetGroupInput) (*mcp.CallToolResult, any, error) {
	if d.assetGroupService == nil {
		return errorResult("资产组服务未初始化", nil)
	}
	name, err := mcpguard.NormalizeWriteName("name", input.Name, mcpguard.MaxAssetGroupNameRunes)
	if err != nil {
		return errorResult("参数不合法", err)
	}
	id, err := d.assetGroupService.CreateGroup(ctx, &models.AssetGroupAddRequest{
		Name:        name,
		Description: strings.TrimSpace(input.Description),
		SortOrder:   input.SortOrder,
	})
	if err != nil {
		return errorResult("创建资产组失败", err)
	}
	return jsonToolResult(ginH{"ok": true, "id": id, "name": name})
}

type deleteAssetGroupsInput struct {
	IDs           []string `json:"ids" jsonschema:"要删除的集团 ObjectID 列表，必填；去重后 1-20 个（list_asset_groups 的 id）"`
	Action        string   `json:"action,omitempty" jsonschema:"删除策略：dissolve（默认，该集团下的地契移入未分组）/ transfer（先划转到 targetGroupId 再删集团）"`
	TargetGroupID string   `json:"targetGroupId,omitempty" jsonschema:"当 action 为 transfer 时必填：接收地契的集团 ObjectID，不能是本批被删的集团之一"`
	Confirm       bool     `json:"confirm,omitempty" jsonschema:"必填 true。本操作不可逆（集团记录物理删除，无回滚接口），缺省/传 false 直接报错"`
}

// deleteAssetGroupFailure 单条删除失败记录（逐条记录，不中断整批）
type deleteAssetGroupFailure struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

func deleteAssetGroups(ctx context.Context, _ *mcp.CallToolRequest, input deleteAssetGroupsInput) (*mcp.CallToolResult, any, error) {
	if d.assetGroupService == nil {
		return errorResult("资产组服务未初始化", nil)
	}
	// 顺序有意为之：confirm 在最前面判。参数错误（比如 ids 超限）不该让调用方
	// 以为「差一点就删了」，而 confirm 缺失是唯一「什么都没发生」的确定性信号。
	if err := mcpguard.RequireConfirm(input.Confirm, "delete_asset_groups"); err != nil {
		return errorResult("拒绝执行", err)
	}
	ids, err := mcpguard.NormalizeDeleteAssetGroupIDs(input.IDs)
	if err != nil {
		return errorResult("参数不合法", err)
	}
	action, targetGroupID, err := mcpguard.NormalizeAssetGroupAction(input.Action, input.TargetGroupID, ids)
	if err != nil {
		return errorResult("参数不合法", err)
	}

	deleted := make([]string, 0, len(ids))
	failed := make([]deleteAssetGroupFailure, 0, len(ids))
	for _, id := range ids {
		// 逐个集团删除：service.DeleteGroup 没有批量接口，且每个集团都要先搬地契，
		// 单条失败（如集团已被别的请求删掉）不应该让整批停在半路。
		if delErr := d.assetGroupService.DeleteGroup(ctx, &models.AssetGroupDeleteRequest{
			ID:            id,
			Action:        action,
			TargetGroupID: targetGroupID,
		}); delErr != nil {
			failed = append(failed, deleteAssetGroupFailure{ID: id, Error: delErr.Error()})
			continue
		}
		deleted = append(deleted, id)
	}

	if len(deleted) == 0 {
		parts := make([]string, 0, len(failed))
		for _, f := range failed {
			parts = append(parts, f.ID+": "+f.Error)
		}
		return errorResult(fmt.Sprintf("删除资产组全部失败（%d 个，action=%s）", len(failed), action),
			fmt.Errorf("%s", strings.Join(parts, "; ")))
	}
	return jsonToolResult(ginH{
		"ok":            len(failed) == 0,
		"action":        action,
		"targetGroupId": targetGroupID,
		"deleted":       deleted,
		"failed":        failed,
		"total":         len(ids),
	})
}

type createAssetScopeInput struct {
	Name      string `json:"name" jsonschema:"地契名称，必填，1-128 个字符"`
	Scope     string `json:"scope" jsonschema:"资产范围文本，必填；域名/IP 用逗号、分号、空格或换行分隔，每个条目必须是合法域名或 IP"`
	ScopeType string `json:"scopeType,omitempty" jsonschema:"范围类型：domain / ip / mixed；留空由服务端按内容推断"`
	GroupID   string `json:"groupId,omitempty" jsonschema:"归属集团 ObjectID（list_asset_groups 的 id）；留空表示未分组。不接受 unassigned / 0 这类只读过滤标记"`
}

func createAssetScope(ctx context.Context, _ *mcp.CallToolRequest, input createAssetScopeInput) (*mcp.CallToolResult, any, error) {
	if d.assetGroupService == nil {
		return errorResult("资产组服务未初始化", nil)
	}
	name, err := mcpguard.NormalizeWriteName("name", input.Name, mcpguard.MaxAssetScopeNameRunes)
	if err != nil {
		return errorResult("参数不合法", err)
	}
	scopeText, err := mcpguard.NormalizeAssetScopeRange(input.Scope)
	if err != nil {
		return errorResult("参数不合法", err)
	}
	scopeType, err := mcpguard.NormalizeAssetScopeType(input.ScopeType)
	if err != nil {
		return errorResult("参数不合法", err)
	}
	groupID, err := mcpguard.NormalizeScopeOwnerGroupID(input.GroupID)
	if err != nil {
		return errorResult("参数不合法", err)
	}
	id, err := d.assetGroupService.CreateScope(ctx, &models.AssetScopeAddRequest{
		Name:      name,
		Scope:     scopeText,
		ScopeType: scopeType,
		GroupID:   groupID,
	})
	if err != nil {
		// 走到这里最常见的是「集团不存在」或「某个条目不是合法域名/IP」，
		// 服务层错误里带具体条目与原因，原样透出比包装成「创建地契失败」更有用。
		return errorResult("创建地契失败", err)
	}
	return jsonToolResult(ginH{
		"ok":        true,
		"id":        id,
		"name":      name,
		"scopeType": scopeType, // 空串表示由服务端按内容推断；要看推断结果请用 list_asset_scopes
		"groupId":   groupID,
	})
}

type createICPTaskInput struct {
	Name string `json:"name,omitempty" jsonschema:"任务名，可选，≤128 字符；留空用第一个查询目标作为任务名"`
	Type string `json:"type,omitempty" jsonschema:"任务类型：company（企业名查备案域名）/ domain（域名反查备案主体，默认）"`
	// 注意：jsonschema tag 里不能出现 "word=" 这种片段——schema 生成器会把逗号后的
	// "type=company" 当成 key=value 选项，整个 tag 报错，工具直接注册失败。
	// 2026-11-27 实测：create_icp_task 因此从未出现在工具列表里。
	Queries  []string `json:"queries,omitempty" jsonschema:"查询目标数组。type 为 company 时填企业名，为 domain 时填域名；会去空去重，domain 类型自动小写去尾点"`
	Target   string   `json:"target,omitempty" jsonschema:"单值便捷字段，仅当 queries 为空时生效"`
	Provider string   `json:"provider,omitempty" jsonschema:"数据来源：manual（本地回读）/ none（显式关闭）/ http（外部 ICP 服务）；留空按当前配置解析"`
}

func createICPTask(ctx context.Context, _ *mcp.CallToolRequest, input createICPTaskInput) (*mcp.CallToolResult, any, error) {
	if d.icpService == nil {
		return errorResult("ICP 服务未初始化", nil)
	}
	taskType, err := mcpguard.NormalizeICPTypeForCreate(input.Type)
	if err != nil {
		return errorResult("参数不合法", err)
	}
	queries, err := mcpguard.NormalizeICPQueriesForCreate(taskType, input.Queries, input.Target)
	if err != nil {
		return errorResult("参数不合法", err)
	}
	var name string
	// 名称交给服务层回落（空 = 用 queries[0]），这里只在显式给了名字时校验长度，
	// 避免把「留空」误判成「必填缺失」。
	if strings.TrimSpace(input.Name) != "" {
		if name, err = mcpguard.NormalizeWriteName("name", input.Name, mcpguard.MaxICPTaskNameRunes); err != nil {
			return errorResult("参数不合法", err)
		}
	}
	provider, err := mcpguard.NormalizeICPProvider(input.Provider)
	if err != nil {
		return errorResult("参数不合法", err)
	}
	task, err := d.icpService.CreateTask(ctx, models.IcpTaskCreateRequest{
		Name:     name,
		Type:     taskType,
		Queries:  queries,
		Provider: provider,
	})
	if err != nil {
		return errorResult("创建 ICP 任务失败", err)
	}
	// 与 REST handler 一致返回整条任务；CreateTask 内部已 Claim 并异步 launch，
	// 所以 state 通常已经是 1（运行中），进度用 list_icp_tasks 轮询。
	return jsonToolResult(ginH{"ok": true, "task": task})
}

type stopICPTasksInput struct {
	IDs []string `json:"ids" jsonschema:"要停止（暂停）的任务 ObjectID 列表，必填；去重后 1-50 个（list_icp_tasks 的 id）"`
}

func stopICPTasks(ctx context.Context, _ *mcp.CallToolRequest, input stopICPTasksInput) (*mcp.CallToolResult, any, error) {
	if d.icpService == nil {
		return errorResult("ICP 服务未初始化", nil)
	}
	ids, err := mcpguard.NormalizeStopICPIDs(input.IDs)
	if err != nil {
		return errorResult("参数不合法", err)
	}
	updated, err := d.icpService.Stop(ctx, ids)
	if err != nil {
		return errorResult("停止 ICP 任务失败", err)
	}
	// updated 是 ModifiedCount：已完成(3) 或已暂停(2) 的任务不会被改，
	// 所以 updated < len(ids) 是正常结果，不是失败。
	return jsonToolResult(ginH{"ok": true, "updated": updated, "ids": ids})
}

func getSystemInfo(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	res, err := d.dashboardService.ConsoleInfo(ctx)
	if err != nil {
		return errorResult("获取系统状态失败", err)
	}
	h := hoststat.Stat("/")
	res.HostStats = &models.HostStats{
		CPUs:                h.CPUs,
		Load1:               h.Load1,
		DiskFreeBytes:       h.DiskFreeBytes,
		DiskTotalBytes:      h.DiskTotalBytes,
		DiskFreePercent:     h.DiskFreePercent,
		MemAvailablePercent: h.MemAvailablePercent,
	}
	res.HostWarnings = hoststat.Warnings(h)
	return jsonToolResult(res)
}

type exportTaskWorkbookInput struct {
	TaskID    string `json:"taskId" jsonschema:"任务 ID（list_tasks 返回的 id）"`
	Format    string `json:"format,omitempty" jsonschema:"xlsx（默认，一个工作簿多张表）或 zip（每集合一个 CSV）"`
	StartTime string `json:"startTime,omitempty" jsonschema:"只导这段发现时间的记录，格式 YYYY-MM-DD HH:MM:SS，可只给一边"`
	EndTime   string `json:"endTime,omitempty" jsonschema:"同上，结束时间"`
}

// exportTaskWorkbook 按任务导出（对应 REST 的 /api/export/task/{id}）。
//
// 为什么单独一个工具：create_export_task 只能导单个 index，
// 而「一个任务产出了什么」需要子域+资产+URL+漏洞+敏感一起给，
// 一次调用出一个多工作表 xlsx 比让调用方拼五次更接近真实用法。
func exportTaskWorkbook(ctx context.Context, _ *mcp.CallToolRequest, input exportTaskWorkbookInput) (*mcp.CallToolResult, any, error) {
	if d.exportService == nil {
		return errorResult("导出服务未初始化", nil)
	}
	taskID := strings.TrimSpace(input.TaskID)
	if taskID == "" {
		return errorResult("taskId 不能为空", nil)
	}
	format := strings.ToLower(strings.TrimSpace(input.Format))
	if format == "" {
		format = "xlsx"
	}
	if format != "xlsx" && format != "zip" {
		return errorResult("format 只支持 xlsx/zip", nil)
	}

	plan, err := export.NewBatchService().PrepareTask(ctx, taskID, input.StartTime, input.EndTime)
	if err != nil {
		return errorResult("准备任务导出失败", err)
	}

	fileName := uuid.NewString() + "." + format
	if err := d.exportService.CreateExportTask(ctx, &models.ExportTask{
		FileName:   fileName,
		CreateTime: helper.GetNowTimeString(),
		Quantity:   int(plan.RowCount),
		DataType:   "task:" + taskID,
		FileType:   format,
		State:      0,
	}); err != nil {
		return errorResult("创建导出任务失败", err)
	}

	// 与 create_export_task 一样异步写文件：任务产出可能几十万行，别让调用方等超时。
	go func(plan *export.TaskExportPlan, fileName, format string) {
		bg := context.Background()
		path := filepath.Join(config.GlobalConfig.System.ExportPath(), fileName)
		f, ferr := os.Create(path)
		if ferr != nil {
			logger.Error("mcp export_task_workbook: create file: " + ferr.Error())
			return
		}
		var written int64
		if format == "xlsx" {
			written, ferr = plan.StreamXLSX(bg, f)
		} else {
			written, ferr = plan.Stream(bg, f)
		}
		_ = f.Close()
		if ferr != nil {
			logger.Error("mcp export_task_workbook: stream: " + ferr.Error())
			return
		}
		info, serr := os.Stat(path)
		if serr != nil {
			logger.Error("mcp export_task_workbook: stat: " + serr.Error())
			return
		}
		if err := d.exportService.FinishExportTask(bg, fileName, float64(info.Size())/1024/1024); err != nil {
			logger.Error("mcp export_task_workbook: finish: " + err.Error())
		}
		logger.Info(fmt.Sprintf("mcp export_task_workbook: %s done rows=%d bytes=%d", fileName, written, info.Size()))
	}(plan, fileName, format)

	return jsonToolResult(ginH{
		"ok": true, "fileName": fileName, "format": format,
		"taskName": plan.TaskName, "rows": plan.RowCount,
		"startTime": plan.StartTime, "endTime": plan.EndTime,
	})
}
