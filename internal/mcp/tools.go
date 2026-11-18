package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/Autumn-27/ScopeSentry/internal/constants"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/app"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/asset"
	assetCommon "github.com/Autumn-27/ScopeSentry/internal/services/assets/common"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/crawler"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/dirscan"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/ip"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/mp"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/page_monitoring"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/root_domain"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/sensitive"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/subdomain"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/url"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/vulnerability"
	"github.com/Autumn-27/ScopeSentry/internal/services/dashboard"
	"github.com/Autumn-27/ScopeSentry/internal/services/dictionary"
	"github.com/Autumn-27/ScopeSentry/internal/services/fingerprint"
	"github.com/Autumn-27/ScopeSentry/internal/services/mcpguard"
	"github.com/Autumn-27/ScopeSentry/internal/services/node"
	"github.com/Autumn-27/ScopeSentry/internal/services/plugin"
	"github.com/Autumn-27/ScopeSentry/internal/services/poc"
	"github.com/Autumn-27/ScopeSentry/internal/services/policy"
	"github.com/Autumn-27/ScopeSentry/internal/services/project"
	"github.com/Autumn-27/ScopeSentry/internal/services/sensitive_rule"
	taskCommon "github.com/Autumn-27/ScopeSentry/internal/services/task/common"
	"github.com/Autumn-27/ScopeSentry/internal/services/task/task"
	"github.com/Autumn-27/ScopeSentry/internal/services/task/template"
	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

type deps struct {
	projectService       project.Service
	taskService          task.Service
	taskCommonService    taskCommon.Service
	templateService      template.Service
	assetService         asset.Service
	rootDomainService    root_domain.Service
	subdomainService     subdomain.Service
	appService           app.Service
	mpService            mp.Service
	urlService           url.Service
	sensitiveService     sensitive.Service
	dirscanService       dirscan.Service
	crawlerService       crawler.Service
	vulnService          vulnerability.Service
	ipService            ip.Service
	pageMonService       page_monitoring.Service
	commonService        assetCommon.Service
	nodeService          node.Service
	pluginService        plugin.Service
	pocService           poc.Service
	fingerprintService   fingerprint.Service
	dictService          dictionary.ManageService
	dashboardService     dashboard.Service
	policyService        *policy.Service
	sensitiveRuleService sensitive_rule.Service
}

var d = &deps{
	projectService:       project.NewService(),
	taskService:          task.NewService(),
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
	dashboardService:     dashboard.NewService(),
	policyService:        policy.NewService(),
	sensitiveRuleService: sensitive_rule.NewService(),
}

func registerTools(server *mcp.Server, allowedTools map[string]bool) {
	names := make([]string, 0, 32)

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
		Name:        "get_asset_detail",
		Description: "获取资产详情，asset_type 支持 asset 或 vulnerability",
	}, getAssetDetail)

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
		Description: "看板统计：七类资产总量 + 节点/任务数量，可按项目过滤",
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

	recordToolNames(names)
	warnUnregisteredTools(names)
}

// warnUnregisteredTools 生产环境的漂移报警：工具注册了但 mcpguard.ToolScopes 里没有，
// 持受限 scope 的 key 调用它会被默认拒绝。本包 init 需要数据库，纯单测覆盖不到，
// 所以留一条运行期告警（集成测试 tools_registry_integration_test.go 会直接失败）。
func warnUnregisteredTools(names []string) {
	for _, name := range names {
		if _, ok := mcpguard.RequiredScope(name); !ok {
			logger.Warn("mcp: tool has no scope registered in mcpguard.ToolScopes",
				zap.String("tool", name))
		}
	}
}

// addTool 统一注册入口。
//
// 两个职责：
//  1. 登记工具名（names），供 ToolNames() 做「治理表 vs 实际工具」漂移测试；
//  2. allowedTools 非 nil 时只注册集合内的工具，让 tools/list 与治理策略一致。
func addTool[In, Out any](server *mcp.Server, allowedTools map[string]bool, names *[]string, tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out]) {
	*names = append(*names, tool.Name)
	if allowedTools != nil && !allowedTools[tool.Name] {
		return
	}
	mcp.AddTool(server, tool, handler)
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
	if input.PageIndex <= 0 {
		input.PageIndex = 1
	}
	if input.PageSize <= 0 {
		input.PageSize = 20
	}
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
}

func listTasks(ctx context.Context, _ *mcp.CallToolRequest, input listTasksInput) (*mcp.CallToolResult, any, error) {
	if input.PageIndex <= 0 {
		input.PageIndex = 1
	}
	if input.PageSize <= 0 {
		input.PageSize = 20
	}
	c := ginContext(ctx)
	tasks, total, err := d.taskService.List(c, input.Search, input.PageIndex, input.PageSize)
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

type listScanTemplatesInput struct {
	Query     string `json:"query,omitempty" jsonschema:"模板名称模糊搜索"`
	PageIndex int    `json:"pageIndex,omitempty" jsonschema:"页码，从 1 开始，默认 1"`
	PageSize  int    `json:"pageSize,omitempty" jsonschema:"每页条数，默认 20"`
}

func listScanTemplates(ctx context.Context, _ *mcp.CallToolRequest, input listScanTemplatesInput) (*mcp.CallToolResult, any, error) {
	if input.PageIndex <= 0 {
		input.PageIndex = 1
	}
	if input.PageSize <= 0 {
		input.PageSize = 20
	}
	result, err := d.templateService.List(ctx, input.PageIndex, input.PageSize, input.Query)
	if err != nil {
		return errorResult("获取模板列表失败", err)
	}
	return jsonToolResult(result)
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
	ScheduledTasks bool                `json:"scheduledTasks,omitempty" jsonschema:"是否创建为计划任务"`
	Hour           int                 `json:"hour,omitempty" jsonschema:"计划任务间隔-小时"`
	Minute         int                 `json:"minute,omitempty" jsonschema:"计划任务间隔-分钟"`
	Day            int                 `json:"day,omitempty" jsonschema:"计划任务间隔-天"`
	Week           int                 `json:"week,omitempty" jsonschema:"计划任务间隔-周（weekly 周期）"`
	CycleType      string              `json:"cycleType,omitempty" jsonschema:"周期类型，如 nhours daily weekly"`
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
	Filter           map[string][]string `json:"filter,omitempty" jsonschema:"精确过滤 JSON，与 search 可组合。filter.project 为项目 ObjectID（list_projects 获取，非项目名）；filter.task 为任务名称。详见工具 description"`
	Sort             map[string]string   `json:"sort,omitempty" jsonschema:"排序。仅 UrlScan/DirScanResult 支持 length：ascending 升序，其他值降序"`
	Sid              string              `json:"sid,omitempty" jsonschema:"敏感信息规则名称，仅 asset_type 为 SensitiveResult 时有效"`
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
		filter := make(map[string][]interface{}, len(input.Filter))
		for k, vals := range input.Filter {
			items := make([]interface{}, len(vals))
			for i, v := range vals {
				items[i] = v
			}
			filter[k] = items
		}
		query.Filter = filter
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
	if input.PageIndex <= 0 {
		input.PageIndex = 1
	}
	if input.PageSize <= 0 {
		input.PageSize = 20
	}

	query := models.SearchRequest{
		PageIndex:        input.PageIndex,
		PageSize:         input.PageSize,
		Index:            index,
		SearchExpression: input.SearchExpression,
		Sort:             input.Sort,
		Sid:              input.Sid,
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
		query.Filter = filter
	}

	c := ginContext(ctx)
	data, err := queryAssets(ctx, c, index, query)
	if err != nil {
		return errorResult("查询资产失败", err)
	}
	return jsonToolResult(data)
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
		return errorResult("添加标签失败", err)
	}
	return jsonToolResult(ginH{"success": true})
}

type listNodesInput struct {
	OnlineOnly bool `json:"online_only,omitempty" jsonschema:"true 时仅返回在线节点，默认 false 返回全部"`
}

func listNodes(ctx context.Context, _ *mcp.CallToolRequest, input listNodesInput) (*mcp.CallToolResult, any, error) {
	result, err := d.nodeService.GetNodeData(ctx, input.OnlineOnly)
	if err != nil {
		return errorResult("获取节点列表失败", err)
	}
	return jsonToolResult(ginH{"list": result})
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
	return jsonToolResult(res)
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
	return jsonToolResult(ginH{"list": list, "total": total})
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
	res, err := d.dashboardService.Stats(ctx, models.DashboardStatsRequest{Project: input.Project})
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
	if err := d.taskService.StopTasks(ginContext(ctx), []string{input.ID}); err != nil {
		return errorResult("暂停任务失败", err)
	}
	return jsonToolResult(ginH{"ok": true, "id": input.ID})
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
	if err := d.taskService.DeleteTasks(ginContext(ctx), ids, input.DeleteAssets); err != nil {
		return errorResult("删除任务失败", err)
	}
	return jsonToolResult(ginH{"ok": true, "deleted": len(ids), "deleteAssets": input.DeleteAssets})
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

func getNodeLogs(ctx context.Context, _ *mcp.CallToolRequest, input nodeNameInput) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(input.Node) == "" {
		return errorResult("node 不能为空", nil)
	}
	res, err := d.nodeService.GetNodeLogs(ctx, input.Node)
	if err != nil {
		return errorResult("读取节点日志失败", err)
	}
	return jsonToolResult(ginH{"node": input.Node, "logs": res})
}

func restartNode(ctx context.Context, _ *mcp.CallToolRequest, input nodeNameInput) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(input.Node) == "" {
		return errorResult("node 不能为空", nil)
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
	if err := d.nodeService.DeleteNodes(ctx, names); err != nil {
		return errorResult("删除节点失败", err)
	}
	return jsonToolResult(ginH{"ok": true, "deleted": len(names)})
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
