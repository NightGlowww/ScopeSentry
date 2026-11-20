// common-------------------------------------
// @file      : common.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2025/6/9 21:03
// -------------------------------------------

package common

import (
	"context"
	"errors"
	"fmt"
	"github.com/Autumn-27/ScopeSentry/internal/constants"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/repositories/task/task"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/asset"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/root_domain"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/subdomain"
	"github.com/Autumn-27/ScopeSentry/internal/services/assets/url"
	"github.com/Autumn-27/ScopeSentry/internal/services/dictionary"
	"github.com/Autumn-27/ScopeSentry/internal/services/node"
	"github.com/Autumn-27/ScopeSentry/internal/services/project"
	"github.com/Autumn-27/ScopeSentry/internal/services/task/taskpolicy"
	"github.com/Autumn-27/ScopeSentry/internal/utils/diskguard"
	"github.com/Autumn-27/ScopeSentry/internal/utils/helper"
	"github.com/Autumn-27/ScopeSentry/internal/utils/memguard"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Service 定义任务服务接口
type Service interface {
	Insert(ctx context.Context, task *models.Task) (string, error)
	CreateTaskScan(ctx context.Context, task models.Task, id string, stopToStart bool) (int64, error)
	GetScanTemplate(ctx context.Context, task *models.Task) (*models.ScanTemplate, error)
	ParameterParser(ctx context.Context, parameters models.Parameters) (models.Parameters, error)
}

type service struct {
	taskRepo          task.Repository
	projectService    project.Service
	assetService      asset.Service
	rootDomainService root_domain.Service
	subdomainService  subdomain.Service
	urlService        url.Service
	nodeService       node.Service
	dictionaryService dictionary.ManageService
	portsService      dictionary.PortService
	// policyTasks 扫描策略消费端（docs/audit/SCAN_POLICY.md）：未选策略时整条链路不触达
	policyTasks *taskpolicy.Service
}

// NewService 创建任务服务实例
func NewService() Service {
	return &service{
		taskRepo:          task.NewRepository(),
		projectService:    project.NewService(),
		assetService:      asset.NewService(),
		rootDomainService: root_domain.NewService(),
		subdomainService:  subdomain.NewService(),
		urlService:        url.NewService(),
		nodeService:       node.NewService(),
		dictionaryService: dictionary.NewManageService(),
		portsService:      dictionary.NewPortService(),
		policyTasks:       taskpolicy.NewService(),
	}
}

func (s *service) Insert(ctx context.Context, task *models.Task) (string, error) {
	// task.TargetSource 分为普通、从资产选择数据进行创建（支持资产、根域名、子域名、url 这里修改原始逻辑 统一为后边的情况）、从项目、从资产、从根域名、从子域名、从url
	var query models.SearchRequest
	var target string
	var err error
	query.PageSize = task.TargetNumber
	query.SearchExpression = task.Search
	query.Filter = make(map[string][]interface{})
	if len(task.Filter) > 0 {
		for k, v := range task.Filter {
			query.Filter[k] = v
		}
	}
	if len(task.Project) > 0 {
		projects := make([]interface{}, len(task.Project))
		for i, v := range task.Project {
			projects[i] = v
		}
		query.Filter["project"] = projects
	}
	if task.TargetSource == "general" {
		//普通的情况不需要做改变
	} else if task.TargetSource == "project" {
		// 从项目中获取数据，读取projet的目标
		target, err = s.projectService.GetTargets(ctx, task.Project)
		if err != nil {
			return "", err
		}
	} else if task.TargetSource == "asset" || (task.TargetSource == "assetSource" && task.TargetTp == "search") {
		// 从资产处创建
		target, err = s.assetService.GetTaskTarget(ctx, query)
		if err != nil {
			return "", err
		}
	} else if task.TargetSource == "RootDomain" || (task.TargetSource == "RootDomainSource" && task.TargetTp == "search") {
		// 从根域名创建
		target, err = s.rootDomainService.GetTaskTarget(ctx, query)
		if err != nil {
			return "", err
		}
	} else if task.TargetSource == "subdomain" || (task.TargetSource == "subdomainSource" && task.TargetTp == "search") {
		// 从子域名处创建
		target, err = s.subdomainService.GetTaskTarget(ctx, query)
		if err != nil {
			return "", err
		}
	} else if task.TargetSource == "UrlScan" || (task.TargetSource == "UrlScanSource" && task.TargetTp == "search") {
		// 从url扫描处创建
		target, err = s.urlService.GetTaskTarget(ctx, query)
		if err != nil {
			return "", err
		}
	} else if task.TargetSource == "assetSource" && task.TargetTp == "select" {
		target, err = s.assetService.GetTaskTargetByIDs(ctx, task.TargetIds)
		if err != nil {
			return "", err
		}
	} else if task.TargetSource == "RootDomainSource" && task.TargetTp == "select" {
		target, err = s.rootDomainService.GetTaskTargetByIDs(ctx, task.TargetIds)
		if err != nil {
			return "", err
		}
	} else if task.TargetSource == "subdomainSource" && task.TargetTp == "select" {
		target, err = s.subdomainService.GetTaskTargetByIDs(ctx, task.TargetIds)
		if err != nil {
			return "", err
		}
	} else if task.TargetSource == "UrlScanSource" && task.TargetTp == "select" {
		target, err = s.urlService.GetTaskTargetByIDs(ctx, task.TargetIds)
		if err != nil {
			return "", err
		}
	}
	if task.Target == "" {
		task.Target = target
	}
	var result []string
	result, _ = helper.GetTargetList(task.Target, task.Ignore)
	task.Target = strings.Join(result, "\n")
	task.TaskNum = len(result)
	if task.TaskNum == 0 {
		return "", errors.New("Target is null")
	}
	task.Progress = 0
	task.CreatTime = helper.GetNowTimeString()
	task.EndTime = ""
	// 任务刚入库时扫描节点还没有领取，先标记为待开始（0），
	// 等节点产出进度后由 TaskProgress 转为运行中（1）。见 issue #288。
	task.Status = constants.TaskStatusPending
	task.Type = task.TargetSource
	// 扫描策略接入：任务带了 policyId 才消费。任务没填模板时用策略的 templateId 兜底并随任务落库；
	// 任务已填模板则以任务为准（用户选择优先）。策略缺失 / 停用只记日志、按模板默认值继续。
	s.applyTaskPolicy(ctx, task)
	insertId, err := s.taskRepo.Insert(ctx, task)
	if err != nil {
		return "", err
	}
	go func() {
		_, err = s.CreateTaskScan(context.Background(), *task, insertId, false)
		if err != nil {
			logger.Error(fmt.Sprintf("CreateTaskScan %v", err))
		}
	}()
	hex, err := primitive.ObjectIDFromHex(insertId)
	if err != nil {
		return "", err
	}
	return hex.Hex(), nil
}

func (s *service) CreateTaskScan(ctx context.Context, task models.Task, id string, stopToStart bool) (int64, error) {
	logger.Info(fmt.Sprintf("CreateTaskScan task:%v %v", id, task.Name))
	if err := diskguard.Check("/"); err != nil {
		logger.Warn(fmt.Sprintf("CreateTaskScan rejected task %s: %v", id, err))
		return 0, err
	}
	// 内存闸：2026-11-19 事故中两个 nuclei 并发把主机打到 swap 抖动 4.5 小时；
	// 磁盘闸之外的第二道防线，主机可用内存不足时不再叠加扫描负载。
	if err := memguard.Check(); err != nil {
		logger.Warn(fmt.Sprintf("CreateTaskScan rejected task %s: %v", id, err))
		return 0, err
	}
	task.ID, _ = primitive.ObjectIDFromHex(id)
	task.IsStart = stopToStart
	if task.AllNode {
		// 选择自动加入后 获取所有节点
		allNode, err := s.nodeService.GetNodeData(ctx, false)
		if err != nil {
			return 0, err
		}
		var nameMap = make(map[string]struct{})
		for _, n := range task.Node {
			nameMap[n] = struct{}{}
		}
		for _, n := range allNode {
			if _, exists := nameMap[n.Name]; !exists {
				nameMap[n.Name] = struct{}{}
				task.Node = append(task.Node, n.Name)
			}
		}
	}

	// 如果是暂停之后再开始的，则不需要删除缓存和填入目标
	if !stopToStart {
		// 删除缓存
		err := s.taskRepo.ClearTaskCache(ctx, id)
		if err != nil {
			return 0, err
		}
		// 生成原始target
		targets, err := helper.GetTargetList(task.Target, task.Ignore)
		if err != nil {
			return 0, err
		}
		err = s.taskRepo.PushTaskInfoList(ctx, id, targets)
		if err != nil {
			return 0, err
		}
	}
	template, err := s.GetScanTemplate(ctx, &task)
	if err != nil {
		return 0, err
	}
	// 背压：节点一次只跑一个任务，队列堆太深只会让后面的任务一直「排队中」。
	// 2026-11-20 实测：某个 MCP key 每分钟建 3-4 个任务并立即 start，
	// 队列瞬间堆到 16 个，真实任务永远排不上。这里直接拒绝而不是静默排队。
	// 单节点排队上限：默认 5（节点一次跑 1~2 个任务，堆太深会让后面的永远等）。
	// 批量派发场景（外部集成一次推很多）可用 TASK_QUEUE_MAX_PER_NODE 调高。
	maxQueuedPerNode := 5
	if v := strings.TrimSpace(os.Getenv("TASK_QUEUE_MAX_PER_NODE")); v != "" {
		if n, perr := strconv.Atoi(v); perr == nil && n > 0 && n <= 50 {
			maxQueuedPerNode = n
		}
	}
	// 容器内存压力：节点容器被 cgroup 限住时会被压死（心跳停、任务卡住）。
	// 宿主 memNum 看不出来（实测宿主 24% / 容器 82%），所以按节点的 containerMem 判断：
	// 已接近上限且还在跑任务，就不要再往里塞（2026-11-20）。
	const containerMemLimit = 90.0
	nodeMem := map[string]float64{}
	nodeRunning := map[string]float64{}
	if nodes, nerr := s.nodeService.GetNodeData(ctx, false); nerr == nil {
		for _, nd := range nodes {
			nodeMem[nd.Name] = nd.ContainerMem
			if v, perr := strconv.ParseFloat(nd.Running, 64); perr == nil {
				nodeRunning[nd.Name] = v
			}
		}
	}

	for _, n := range task.Node {
		if mem := nodeMem[n]; mem >= containerMemLimit && nodeRunning[n] >= 1 {
			return 0, fmt.Errorf("节点 %s 容器内存已用 %.1f%%（接近上限）且正在执行任务，为避免压死节点本次不派发；可换节点或等当前任务结束", n, mem)
		}
		if q, qerr := s.taskRepo.NodeQueueLen(ctx, n); qerr == nil && q >= int64(maxQueuedPerNode) {
			return 0, fmt.Errorf("节点 %s 队列已积压 %d 个任务（上限 %d），请等待当前任务完成后再提交", n, q, maxQueuedPerNode)
		}
		template.Target = ""
		err := s.taskRepo.RPushNodeTask(ctx, n, template)
		if err != nil {
			logger.Error(fmt.Sprintf("CreateTaskScan task:%v error:%v", n, err))
			continue
		}
	}
	logger.Info(fmt.Sprintf("CreateTaskScan task:%v success", task.ID.Hex()))
	return 0, nil
}

func (s *service) GetScanTemplate(ctx context.Context, task *models.Task) (*models.ScanTemplate, error) {
	// 扫描策略消费端：策略的 plugins 收窄各阶段插件清单；任务没填模板时用策略模板兜底
	// （任务已填模板则以任务为准，见 taskpolicy.ApplyToTask 的优先级说明）。
	policyCfg, _ := s.taskPolicyConfig(ctx, task)
	templateID := strings.TrimSpace(task.Template)
	if templateID == "" && policyCfg != nil {
		templateID = strings.TrimSpace(policyCfg.TemplateID)
	}
	if templateID == "" {
		return &models.ScanTemplate{}, fmt.Errorf("task %s has no scan template", task.Name)
	}
	scanTemplate, err := s.taskRepo.FindTemplateByID(ctx, templateID)
	if err != nil {
		return &models.ScanTemplate{}, err
	}
	if scanTemplate == nil {
		// FindTemplateByID 找不到时返回 (nil, nil)；原实现会紧接着解引用 panic，这里改成明确报错。
		return &models.ScanTemplate{}, fmt.Errorf("scan template not found: %s", templateID)
	}
	if policyCfg != nil {
		applied, warnings := taskpolicy.ApplyToTemplate(scanTemplate, *policyCfg)
		for _, w := range warnings {
			logger.Warn(fmt.Sprintf("task policy %s: %s (%s)", task.PolicyID, w.Message, w.Code))
		}
		if len(applied) > 0 {
			logger.Info(fmt.Sprintf("task policy %s applied on template %s: %v", task.PolicyID, templateID, applied))
		}
	}
	if len(scanTemplate.VulList) != 0 {
		if helper.StringInSlice("ed93b8af6b72fe54a60efdb932cf6fbc", scanTemplate.VulnerabilityScan) {
			vulTmp := ""
			if helper.StringInSlice("All Poc", scanTemplate.VulList) {
				vulTmp = "*"
			} else {
				for _, vul := range scanTemplate.VulList {
					vulTmp += vul + ".yaml" + ","
				}
				vulTmp = strings.TrimRight(vulTmp, ",")
			}
			if _, exists := scanTemplate.Parameters.VulnerabilityScan["ed93b8af6b72fe54a60efdb932cf6fbc"]; exists {
				scanTemplate.Parameters.VulnerabilityScan["ed93b8af6b72fe54a60efdb932cf6fbc"] = scanTemplate.Parameters.VulnerabilityScan["ed93b8af6b72fe54a60efdb932cf6fbc"] + " -t " + vulTmp
			} else {
				scanTemplate.Parameters.VulnerabilityScan["ed93b8af6b72fe54a60efdb932cf6fbc"] = "-t " + vulTmp
			}
		}
	}
	scanTemplate.Parameters, err = s.ParameterParser(ctx, scanTemplate.Parameters)
	if err != nil {
		return &models.ScanTemplate{}, err
	}
	scanTemplate.VulList = []string{}
	scanTemplate.ID = task.ID.Hex()
	scanTemplate.Ignore = task.Ignore
	scanTemplate.Type = task.Type
	scanTemplate.Duplicates = task.Duplicates
	scanTemplate.IsStart = task.IsStart
	scanTemplate.TaskName = task.Name
	return scanTemplate, err
}

// applyTaskPolicy 把策略参数落到待入库的任务上（当前只有扫描模板会随任务持久化）。
// 策略读不到只记日志并返回：建任务不能因为策略被删掉而失败。
func (s *service) applyTaskPolicy(ctx context.Context, task *models.Task) {
	cfg, err := s.taskPolicyConfig(ctx, task)
	if err != nil {
		// 策略被删除/停用：任务按模板默认值继续，但必须留下可见提示
		task.PolicyWarnings = append(task.PolicyWarnings,
			"policy_unavailable: 策略不存在或已停用，本次任务按模板默认值执行")
		return
	}
	if cfg == nil {
		return
	}
	applied, warnings := taskpolicy.ApplyToTask(task, *cfg)
	for _, w := range warnings {
		logger.Warn(fmt.Sprintf("task policy %s: %s (%s)", task.PolicyID, w.Message, w.Code))
		task.PolicyWarnings = append(task.PolicyWarnings, w.Code+": "+w.Message)
	}
	if len(applied) > 0 {
		logger.Info(fmt.Sprintf("task policy %s applied on task %s: %v", task.PolicyID, task.Name, applied))
	}
}

// taskPolicyConfig 读任务上挂的策略参数；未选策略返回 (nil, nil)，调用方按「无策略」处理。
func (s *service) taskPolicyConfig(ctx context.Context, task *models.Task) (*models.ScanPolicyConfig, error) {
	if task == nil || s.policyTasks == nil {
		return nil, nil
	}
	id := strings.TrimSpace(task.PolicyID)
	if id == "" {
		return nil, nil
	}
	cfg, err := s.policyTasks.Config(ctx, id)
	if err != nil {
		logger.Warn(fmt.Sprintf("task policy %s ignored: %v", id, err))
		return nil, err
	}
	return &cfg, nil
}

var placeholderRegex = regexp.MustCompile(`\{(.*?)\}`)

// ParameterParser 解析参数中的字典和端口引用
func (s *service) ParameterParser(ctx context.Context, parameters models.Parameters) (models.Parameters, error) {
	// 1. 获取字典数据
	dictList := make(map[string]string)
	dicts, err := s.dictionaryService.List(ctx)
	if err != nil {
		return models.Parameters{}, err
	}
	for _, dict := range dicts {
		dictList[strings.ToLower(dict.Category+"."+dict.Name)] = dict.ID.Hex()
	}

	// 2. 获取端口数据
	portList := make(map[string]string)
	ports, _, err := s.portsService.Get(ctx, "", 1, 1000)
	if err != nil {
		return models.Parameters{}, err
	}
	for _, port := range ports {
		portList[strings.ToLower(port.Name)] = port.Value
	}

	// 公共处理函数
	processMap := func(m map[string]string) {
		for k, v := range m {
			newValue := placeholderRegex.ReplaceAllStringFunc(v, func(match string) string {
				// match 就是 {dict.xxx} 或 {port.xxx}
				inner := strings.Trim(match, "{}")
				parts := strings.SplitN(inner, ".", 2)
				if len(parts) != 2 {
					return match
				}

				tp := strings.ToLower(parts[0])
				val := strings.ToLower(parts[1])

				switch tp {
				case "dict":
					if real, ok := dictList[val]; ok {
						return real
					}
					logger.Error(fmt.Sprintf("未找到字典参数: %s", val))
				case "port":
					if real, ok := portList[val]; ok {
						return real
					}
					logger.Error(fmt.Sprintf("未找到端口参数: %s", val))
				}
				return match
			})
			m[k] = newValue
		}
	}

	// 3. 自动批量处理所有字段（集中在一个 slice 中，方便以后加字段）
	allMaps := []*map[string]string{
		&parameters.TargetHandler,
		&parameters.SubdomainScan,
		&parameters.SubdomainSecurity,
		&parameters.PortScanPreparation,
		&parameters.PortScan,
		&parameters.PortFingerprint,
		&parameters.AssetMapping,
		&parameters.AssetHandle,
		&parameters.URLScan,
		&parameters.WebCrawler,
		&parameters.URLSecurity,
		&parameters.DirScan,
		&parameters.VulnerabilityScan,
	}

	for _, mp := range allMaps {
		processMap(*mp)
	}

	return parameters, nil
}
