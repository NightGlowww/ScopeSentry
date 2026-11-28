// Package dashboard 仪表盘 / 控制台读模型服务（对标 ARL-Next dashboard.py 与 console.py）。
//
// 全部只读：只做 countDocuments / $group / Redis 读 / /proc 读，不写任何集合。
// 统计口径与资产列表保持一致（同一批集合、同一个 project 字段），
// 所以仪表盘上的数字和点进列表看到的总数不会互相打脸。
package dashboard

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/config"
	"github.com/Autumn-27/ScopeSentry/internal/constants"
	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	redisdb "github.com/Autumn-27/ScopeSentry/internal/database/redis"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/repositories/dashboard"
	project "github.com/Autumn-27/ScopeSentry/internal/repositories/project"
	"github.com/Autumn-27/ScopeSentry/internal/services/node"
	"github.com/Autumn-27/ScopeSentry/internal/utils/helper"
	"github.com/Autumn-27/ScopeSentry/internal/utils/sysinfo"
	"go.mongodb.org/mongo-driver/bson"
	"go.uber.org/zap"
)

// 分页上限：dashboard/logs 默认 50 条，单页最多 200（handler 的 binding 把 >200 拒成 400）
const (
	defaultLogPageSize = 50
	maxPageSize        = 200
	fallbackTimezone   = "Asia/Shanghai"
)

// ErrInvalidDays 趋势窗口只允许 7 / 30（handler 已用 binding 拦过，这里是内部兜底）
var ErrInvalidDays = errors.New("dashboard: days must be 7 or 30")

// ErrProjectNotFound 传入的项目标识（ID 或名称）在库里不存在。
//
// 以前不校验：asset.project 存的是项目 ObjectID 字符串，传项目**名称**时条件永远不命中，
// 于是仪表盘返回全 0 且 code=200——调用方（尤其是自动化 / MCP）会据此断言
// 「这个项目没有资产」，实际只是标识写错了（2026-11-29 实测）。
var ErrProjectNotFound = errors.New("dashboard: project not found")

// Service 仪表盘读模型服务
type Service interface {
	// Stats 七类总量（project 可选）
	Stats(ctx context.Context, req models.DashboardStatsRequest) (*models.DashboardStats, error)
	// Trend 按天趋势（自动补零）
	Trend(ctx context.Context, req models.DashboardTrendRequest) (*models.DashboardTrend, error)
	// Logs 服务端日志分页 + 过滤
	Logs(ctx context.Context, req models.DashboardLogsRequest) (*models.DashboardLogsResult, error)
	// SysInfo 本机 CPU/内存/磁盘/uptime/Go 运行时
	SysInfo(ctx context.Context) (*models.DashboardSysInfo, error)
	// Widgets 一次返回多卡片（stats + trend + sysinfo）
	Widgets(ctx context.Context, req models.DashboardWidgetsRequest) (*models.DashboardWidgets, error)
	// ConsoleInfo 控制台信息（version/uptime/节点/任务/依赖状态）
	ConsoleInfo(ctx context.Context) (*models.ConsoleInfo, error)
}

type service struct {
	repo        dashboard.Repository
	projectRepo project.Repository
}

// NewService 创建仪表盘服务实例
func NewService() Service {
	return &service{
		repo:        dashboard.NewRepository(),
		projectRepo: project.NewRepository(),
	}
}

// resolveProject 把「项目 ID 或项目名称」规范成资产集合里 project 字段的真实取值。
//
// 空字符串 = 全库（保持原语义）。三种标识写法都要工作，否则就是一次静默的假成功：
// 传 ObjectID、传名称、名称写错（后者必须报错，不能回全 0）。
func (s *service) resolveProject(ctx context.Context, idOrName string) (string, error) {
	if idOrName == "" {
		return "", nil
	}
	if s.projectRepo != nil {
		if p, err := s.projectRepo.FindByID(ctx, idOrName); err != nil {
			return "", err
		} else if p != nil && !p.ID.IsZero() {
			return p.ID.Hex(), nil
		}
		if p, err := s.projectRepo.FindByName(ctx, idOrName); err != nil {
			return "", err
		} else if p != nil && !p.ID.IsZero() {
			return p.ID.Hex(), nil
		}
		return "", fmt.Errorf("%w: %s", ErrProjectNotFound, idOrName)
	}
	return idOrName, nil
}

// Stats 七类总量。
//
// 每条计数都是一次独立的 countDocuments：把它们并发起来，
// 跨机房（Mongo 与应用不在同一台机）时 9 次串行往返会明显拖慢首屏。
func (s *service) Stats(ctx context.Context, req models.DashboardStatsRequest) (*models.DashboardStats, error) {
	project := strings.TrimSpace(req.Project)
	resolved, err := s.resolveProject(ctx, project)
	if err != nil {
		return nil, err
	}
	base := bsonProjectFilter(resolved)

	out := &models.DashboardStats{Project: project, GeneratedAt: helper.FormatTime(time.Now())}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	// record 只收敛第一个错误：仪表盘上某一块查不到时，其它块仍然要出数。
	record := func(err error) {
		if err == nil {
			return
		}
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		mu.Unlock()
	}

	count := func(collection string, filter bson.M, dst *int64) {
		// 每个 goroutine 只写自己那个字段，互不重叠，不需要锁。
		value, err := s.repo.Count(ctx, collection, filter)
		if err != nil {
			record(err)
			return
		}
		*dst = value
	}

	wg.Add(6)
	go func() { defer wg.Done(); count("asset", base, &out.Asset) }()
	go func() { defer wg.Done(); count("subdomain", base, &out.Subdomain) }()
	go func() { defer wg.Done(); count("UrlScan", base, &out.URL) }()
	go func() {
		defer wg.Done()
		count("vulnerability", base, &out.Vulnerability)
		// 处置分布：首页要能看出「待处理 / 已忽略 / 已确认」，不能只有一个总数。
		count("vulnerability", withStatus(base, 1), &out.VulnPending)
		count("vulnerability", withStatus(base, 3), &out.VulnIgnored)
		count("vulnerability", withStatus(base, 5), &out.VulnConfirmed)
	}()
	go func() { defer wg.Done(); count("SensitiveResult", base, &out.Sensitive) }()
	go func() {
		defer wg.Done()
		// task 的 project 是数组（一个任务可以绑多个项目），{project: x} 命中数组元素
		count("task", base, &out.Task)
		count("task", withStatus(base, constants.TaskStatusRunning), &out.TaskRunning)
		count("task", withStatus(base, constants.TaskStatusFinished), &out.TaskFinished)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		// 节点计数来自 Redis：Redis 抖动不该让整个统计卡片 500。
		// 降级成 0 并留一条 warn，console/info 的 redis.connected 会说明原因。
		total, online, err := s.nodeCounts(ctx)
		if err != nil {
			logger.Warn("dashboard: node counts unavailable", zap.String("err", err.Error()))
			return
		}
		out.Node, out.NodeOnline = total, online
	}()

	wg.Wait()
	if firstErr != nil {
		return nil, fmt.Errorf("dashboard stats: %w", firstErr)
	}
	return out, nil
}

// Trend 按天趋势：资产（asset.time）/ 漏洞（vulnerability.time）/ 完成任务（task.endTime, status=3）。
//
// 补零：先把窗口内每一天都铺出来，再用聚合结果覆盖，前端不需要自己补日期。
// 时区：窗口与日期字符串都用配置时区（默认 Asia/Shanghai），与写库时的口径一致。
func (s *service) Trend(ctx context.Context, req models.DashboardTrendRequest) (*models.DashboardTrend, error) {
	days := req.Days
	if days == 0 {
		days = models.DashboardTrendDays7
	}
	if days != models.DashboardTrendDays7 && days != models.DashboardTrendDays30 {
		return nil, ErrInvalidDays
	}

	loc, tzName := dashboardLocation()
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	startDay := today.AddDate(0, 0, -(days - 1))

	startStr := startDay.Format("2006-01-02") + " 00:00:00"
	endStr := today.Format("2006-01-02") + " 23:59:59"

	project := strings.TrimSpace(req.Project)
	resolved, err := s.resolveProject(ctx, project)
	if err != nil {
		return nil, err
	}
	base := bsonProjectFilter(resolved)

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
		asset    map[string]int64
		vuln     map[string]int64
		task     map[string]int64
	)
	record := func(err error) {
		if err == nil {
			return
		}
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		mu.Unlock()
	}

	wg.Add(3)
	go func() {
		defer wg.Done()
		rows, err := s.repo.TrendByDay(ctx, "asset", "time", base, startStr, endStr)
		record(err)
		asset = rows
	}()
	go func() {
		defer wg.Done()
		rows, err := s.repo.TrendByDay(ctx, "vulnerability", "time", base, startStr, endStr)
		record(err)
		vuln = rows
	}()
	go func() {
		defer wg.Done()
		// 任务「完成」的口径：status=3 且 endTime 落在窗口内（完成时才写 endTime）
		taskBase := withStatus(base, constants.TaskStatusFinished)
		rows, err := s.repo.TrendByDay(ctx, "task", "endTime", taskBase, startStr, endStr)
		record(err)
		task = rows
	}()
	wg.Wait()
	if firstErr != nil {
		return nil, fmt.Errorf("dashboard trend: %w", firstErr)
	}

	points, totals := buildTrendPoints(days, startDay, asset, vuln, task)
	labels, assets, vulns, tasks := trendSeries(points)

	return &models.DashboardTrend{
		Days:     days,
		Timezone: tzName,
		Start:    startStr,
		End:      endStr,
		Labels:   labels,
		Assets:   assets,
		Vulns:    vulns,
		Tasks:    tasks,
		Points:   points,
		Totals:   totals,
	}, nil
}

// buildTrendPoints 铺出窗口内每一天并补零（没有数据的日期返回 0），同时算窗口合计。
//
// 独立成纯函数的原因：补零/排序是这条接口最容易写错的地方，
// 抽出来就能在没有 Mongo 的情况下单测（见 dashboard_test.go）。
func buildTrendPoints(days int, startDay time.Time, asset, vuln, task map[string]int64) ([]models.DashboardTrendPoint, models.DashboardTrendTotals) {
	points := make([]models.DashboardTrendPoint, 0, days)
	var totals models.DashboardTrendTotals
	for i := 0; i < days; i++ {
		date := startDay.AddDate(0, 0, i).Format("2006-01-02")
		point := models.DashboardTrendPoint{
			Date:          date,
			Asset:         asset[date], // 没有数据就是 map 的零值 0，即补零
			Vulnerability: vuln[date],
			Task:          task[date],
		}
		totals.Asset += point.Asset
		totals.Vulnerability += point.Vulnerability
		totals.Task += point.Task
		points = append(points, point)
	}
	return points, totals
}

// trendSeries 把按天的 points 摊成列式序列（与 ARL-Next 的 days/assets/vulns 同形状）。
// 下标与 points 一一对应，前端喂图表时不需要再遍历。
func trendSeries(points []models.DashboardTrendPoint) (labels []string, assets, vulns, tasks []int64) {
	labels = make([]string, len(points))
	assets = make([]int64, len(points))
	vulns = make([]int64, len(points))
	tasks = make([]int64, len(points))
	for i, point := range points {
		labels[i] = point.Date
		assets[i] = point.Asset
		vulns[i] = point.Vulnerability
		tasks[i] = point.Task
	}
	return labels, assets, vulns, tasks
}

// SysInfo 本机指标（Linux 读 /proc，其它平台返回 available=false）
func (s *service) SysInfo(ctx context.Context) (*models.DashboardSysInfo, error) {
	return sysinfo.Collect(), nil
}

// Widgets 一次返回多卡片，避免前端为 stats / trend / sysinfo 打三个请求。
func (s *service) Widgets(ctx context.Context, req models.DashboardWidgetsRequest) (*models.DashboardWidgets, error) {
	days := req.Days
	if days == 0 {
		days = models.DashboardTrendDays7
	}
	includeSysInfo := true
	if req.IncludeSysInfo != nil {
		includeSysInfo = *req.IncludeSysInfo
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
		stats    *models.DashboardStats
		trend    *models.DashboardTrend
		info     *models.DashboardSysInfo
	)
	record := func(err error) {
		if err == nil {
			return
		}
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		mu.Unlock()
	}

	wg.Add(2)
	go func() {
		defer wg.Done()
		res, err := s.Stats(ctx, models.DashboardStatsRequest{Project: req.Project})
		if err != nil {
			record(err)
			return
		}
		stats = res
	}()
	go func() {
		defer wg.Done()
		res, err := s.Trend(ctx, models.DashboardTrendRequest{Days: days, Project: req.Project})
		if err != nil {
			record(err)
			return
		}
		trend = res
	}()
	if includeSysInfo {
		wg.Add(1)
		go func() {
			defer wg.Done()
			info = sysinfo.Collect()
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, fmt.Errorf("dashboard widgets: %w", firstErr)
	}

	return &models.DashboardWidgets{
		Stats:       stats,
		Trend:       trend,
		SysInfo:     info,
		GeneratedAt: helper.FormatTime(time.Now()),
	}, nil
}

// ConsoleInfo 控制台信息（对标 ARL-Next console/info）。
func (s *service) ConsoleInfo(ctx context.Context) (*models.ConsoleInfo, error) {
	version := constants.Version
	if version == "" {
		version = "dev"
	}
	_, tzName := dashboardLocation()
	uptime := sysinfo.ProcessUptime()

	info := &models.ConsoleInfo{
		Version:       version,
		StartTime:     helper.FormatTime(sysinfo.ProcessStart()),
		UptimeSeconds: uint64(uptime.Seconds()),
		UptimeHuman:   sysinfo.HumanDuration(uptime),
		Timezone:      tzName,
		CollectedAt:   helper.FormatTime(time.Now()),
	}

	// 这个端点的价值就是「把故障说清楚」：节点数/任务数拿不到时降级成 0，
	// 由下面的 mongo/redis 连接状态说明原因，而不是整块 500（否则 Redis 一挂
	// 连「Redis 挂了」都查不出来）。
	if total, online, err := s.nodeCounts(ctx); err != nil {
		logger.Warn("console info: node counts unavailable", zap.String("err", err.Error()))
	} else {
		info.NodeCount, info.NodeOnline = total, online
	}
	if count, err := s.repo.Count(ctx, "task", bson.M{}); err != nil {
		logger.Warn("console info: task count unavailable", zap.String("err", err.Error()))
	} else {
		info.TaskCount = count
	}
	if count, err := s.repo.Count(ctx, "task", bson.M{"status": constants.TaskStatusRunning}); err != nil {
		logger.Warn("console info: running task count unavailable", zap.String("err", err.Error()))
	} else {
		info.TaskRunning = count
	}

	info.Mongo = mongoStatus(ctx)
	info.Redis = redisStatus(ctx)
	return info, nil
}

// nodeCounts 节点总数 / 在线数。
//
// 存活判定复用 services/node.ClassifyLiveness：与节点列表页同一套口径
// （容忍节点与服务器的时区/时钟差异），但这里只读、不回写节点状态。
func (s *service) nodeCounts(ctx context.Context) (total, online int64, err error) {
	beats, err := s.repo.NodeHeartbeats(ctx)
	if err != nil {
		return 0, 0, err
	}

	loc, _ := dashboardLocation()
	now := time.Now().In(loc)
	timeout := node.NodeTimeout()

	total = int64(len(beats))
	for _, beat := range beats {
		if node.ClassifyLiveness(beat.State, beat.UpdateTime, now, loc, timeout).Alive {
			online++
		}
	}
	return total, online, nil
}

// mongoStatus 探一次 Mongo
func mongoStatus(ctx context.Context) models.ConsoleDependency {
	if mongodb.Client == nil {
		return models.ConsoleDependency{Connected: false, Error: "mongo client not initialized"}
	}
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	start := time.Now()
	err := mongodb.Client.Ping(pingCtx, nil)
	latency := float64(time.Since(start).Microseconds()) / 1000
	if err != nil {
		return models.ConsoleDependency{Connected: false, LatencyMs: latency, Error: err.Error()}
	}
	return models.ConsoleDependency{Connected: true, LatencyMs: latency}
}

// redisStatus 探一次 Redis
func redisStatus(ctx context.Context) models.ConsoleDependency {
	if redisdb.Client == nil {
		return models.ConsoleDependency{Connected: false, Error: "redis client not initialized"}
	}
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	start := time.Now()
	err := redisdb.Client.Ping(pingCtx).Err()
	latency := float64(time.Since(start).Microseconds()) / 1000
	if err != nil {
		return models.ConsoleDependency{Connected: false, LatencyMs: latency, Error: err.Error()}
	}
	return models.ConsoleDependency{Connected: true, LatencyMs: latency}
}

// bsonProjectFilter 把 project 转成过滤条件；空字符串表示全库（空 filter 走集合元数据）
func bsonProjectFilter(project string) bson.M {
	if project == "" {
		return bson.M{}
	}
	return bson.M{"project": project}
}

// withStatus 在原有条件上加 status（$and 组合，避免 project/status 谁覆盖谁的问题）
func withStatus(base bson.M, status int) bson.M {
	statusCond := bson.M{"status": status}
	if len(base) == 0 {
		return statusCond
	}
	return bson.M{"$and": bson.A{base, statusCond}}
}

// dashboardLocation 仪表盘时区：配置优先，回退 Asia/Shanghai（需求口径）
func dashboardLocation() (*time.Location, string) {
	name := strings.TrimSpace(config.GlobalConfig.System.Timezone)
	if name == "" {
		name = fallbackTimezone
	}
	loc, err := time.LoadLocation(name)
	if err != nil || loc == nil {
		loc, err = time.LoadLocation(fallbackTimezone)
		if err != nil || loc == nil {
			loc = time.FixedZone(fallbackTimezone, 8*3600)
		}
		name = fallbackTimezone
	}
	return loc, name
}
