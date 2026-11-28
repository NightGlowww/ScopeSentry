// Package node -----------------------------
// @file      : service.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2025/4/30 15:01
// -------------------------------------------
package node

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/config"
	"github.com/Autumn-27/ScopeSentry/internal/logger"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/repositories/node"
	"github.com/Autumn-27/ScopeSentry/internal/services/watchdog"
	"go.uber.org/zap"
)

type Service interface {
	GetNodeData(ctx context.Context, online bool) ([]models.NodeData, error)
	RefreshConfig(ctx context.Context, msg models.Message) error
	DeleteNodes(ctx context.Context, names []string) error
	GetNodeLogs(ctx context.Context, name string) (string, error)
	GetNodePlugin(ctx context.Context, nodeName string) ([]models.NodePluginInfo, error)
	RestartNode(ctx context.Context, nodeName string) error
	// ClearNodeQueue 清空节点待领取队列，返回清掉的条数
	ClearNodeQueue(ctx context.Context, nodeName string) (int64, error)
	// MoveQueuedTasks 把待开始任务从积压节点迁到空闲节点
	MoveQueuedTasks(ctx context.Context, from, to string, limit int) (int, error)
	// EnsureMemSample 惰性写入内存采样
	EnsureMemSample(ctx context.Context, name string, mem, running float64)
	// MemPeaks 最近一小时每个节点的峰值/均值
	MemPeaks(ctx context.Context) map[string][2]float64
	// MemHighMinutes 每个节点连续高压（≥90%）的分钟数
	MemHighMinutes(ctx context.Context) map[string]int
}

// ClearNodeQueue 清空节点待领取队列。
// 场景：任务洪泛把队列堆满，真实任务永远排不上；此前只能登服务器 redis-cli 手工 DEL。
func (s *service) ClearNodeQueue(ctx context.Context, nodeName string) (int64, error) {
	if nodeName == "" {
		return 0, fmt.Errorf("node name is required")
	}
	return s.nodeRepo.ClearNodeQueue(ctx, nodeName)
}

type service struct {
	nodeRepo node.Repository
}

func NewService() Service {
	return &service{
		nodeRepo: node.NewRepository(),
	}
}

// GetNodeData 获取所有节点数据。
//
// 存活判定见 ClassifyLiveness：容忍节点与服务器之间的时区/时钟差异，
// 解析失败时保留节点而不是把它从列表里抹掉（#254）。
func (s *service) GetNodeData(ctx context.Context, online bool) ([]models.NodeData, error) {
	nodes, err := s.nodeRepo.GetAllNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get node keys: %w", err)
	}

	loc, err := time.LoadLocation(config.GlobalConfig.System.Timezone)
	if err != nil {
		logger.Warn("Failed to load timezone, fall back to UTC",
			zap.String("timezone", config.GlobalConfig.System.Timezone), zap.Error(err))
		loc = time.UTC
	}
	now := time.Now().In(loc)
	timeout := NodeTimeout()

	result := make([]models.NodeData, 0, len(nodes))
	for _, nodeData := range nodes {
		lv := ClassifyLiveness(nodeData.State, nodeData.UpdateTime, now, loc, timeout)
		switch {
		case lv.ParseFailed:
			logger.Warn("Node heartbeat timestamp unparsable, keeping node",
				zap.String("node", nodeData.Name), zap.String("time", nodeData.UpdateTime))
		case lv.ClockSkew:
			logger.Debug("Node clock is ahead of server",
				zap.String("node", nodeData.Name), zap.Duration("skew", -lv.TimeDiff))
		case lv.UsedUTC:
			logger.Debug("Node heartbeat matched UTC interpretation",
				zap.String("node", nodeData.Name))
		}

		if nodeData.State == "1" && !lv.Alive {
			// 心跳刚过期（<2 倍超时）不判定离线：服务端忙一下、节点 GC 抖一下都会让
			// 单次心跳晚几秒，直接把 state 改成 3 会来回闪（2026-11-20 实测：节点容器
			// 健康、任务在跑，列表却短暂显示「离线」）。这段时间只标 stale。
			if lv.TimeDiff > 2*timeout {
				go s.updateNodeState(context.Background(), nodeData.Name)
				nodeData.State = "3"
			} else {
				nodeData.Stale = true
			}
		}

		if online && nodeData.State != "1" {
			continue
		}
		// 队列长度：让界面能看出这个节点积压了多少任务
		if q, qerr := s.nodeRepo.NodeQueueLen(ctx, nodeData.Name); qerr == nil {
			nodeData.QueueLen = q
		}
		// 惰性采样：把容器内存写进趋势（4 分钟一条）
		if rv, perr := strconv.ParseFloat(nodeData.Running, 64); perr == nil {
			s.nodeRepo.EnsureMemSample(ctx, nodeData.Name, nodeData.ContainerMem, rv)
		}
		// 「此刻在跑什么、多久没产出」：队列不动的根因就在这里。
		// 阈值与巡检共用 StalledAfterMinutes()，避免界面说卡住、巡检判正常。
		rt := s.nodeRepo.NodeRuntime(ctx, nodeData.Name)
		if rt.TaskID != "" {
			nodeData.CurrentTaskID = rt.TaskID
			nodeData.CurrentTaskName = rt.TaskName
			nodeData.CurrentModule = rt.Module
			nodeData.CurrentTarget = rt.Target
			nodeData.CurrentSince = rt.Since
			nodeData.IdleMinutes = rt.IdleMinutes
			nodeData.Stalled = rt.IdleMinutes >= watchdog.StalledAfterMinutes()
		}
		result = append(result, nodeData)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result, nil
}

// updateNodeState 更新节点状态
func (s *service) updateNodeState(ctx context.Context, nodeID string) {
	err := s.nodeRepo.UpdateNodeState(ctx, nodeID, "3")
	if err != nil {
		logger.Error("Failed to update node state",
			zap.String("node_id", nodeID),
			zap.Error(err))
	}
}

func (s *service) RefreshConfig(ctx context.Context, msg models.Message) error {
	jsonBytes, err := json.Marshal(msg)
	if err != nil {

		return fmt.Errorf("models.Message转换失败: %v", err)
	}

	msgStr := string(jsonBytes)
	if msg.Name == "all" {
		nodes, err := s.nodeRepo.GetAllNodes(ctx)
		if err != nil {
			return fmt.Errorf("failed to get node keys: %w", err)
		}
		for _, nodeData := range nodes {
			err := s.nodeRepo.RefreshConfig(ctx, nodeData.Name, msgStr)
			if err != nil {
				return err
			}
		}
	} else {
		err := s.nodeRepo.RefreshConfig(ctx, msg.Name, msgStr)
		if err != nil {
			return err
		}
	}
	return nil
}

// requireNodes 校验节点存在。
//
// 节点数据在 Redis 的 node:<name> 里，删/重启一个不存在的名字以前也是「成功」：
// 界面弹「操作成功」，实际什么都没有发生（2026-11-28 实测 /api/node/delete、/api/node/restart）。
func (s *service) requireNodes(ctx context.Context, names []string) error {
	list, err := s.GetNodeData(ctx, false)
	if err != nil {
		return err
	}
	exist := make(map[string]bool, len(list))
	for _, n := range list {
		exist[n.Name] = true
	}
	var missing []string
	for _, n := range names {
		if n != "" && !exist[n] {
			missing = append(missing, n)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	avail := make([]string, 0, len(exist))
	for n := range exist {
		avail = append(avail, n)
	}
	sort.Strings(avail)
	return fmt.Errorf("节点不存在: %s（当前节点: %s）", strings.Join(missing, ", "), strings.Join(avail, ", "))
}

// DeleteNodes 删除多个节点
func (s *service) DeleteNodes(ctx context.Context, names []string) error {
	if err := s.requireNodes(ctx, names); err != nil {
		return err
	}
	return s.nodeRepo.DeleteNodes(ctx, names)
}

// GetNodeLogs 获取节点日志
func (s *service) GetNodeLogs(ctx context.Context, name string) (string, error) {
	return s.nodeRepo.GetNodeLogs(ctx, name)
}

// GetNodePlugin 获取节点插件信息
func (s *service) GetNodePlugin(ctx context.Context, nodeName string) ([]models.NodePluginInfo, error) {
	return s.nodeRepo.GetNodePlugin(ctx, nodeName)
}

// RestartNode 重启节点
func (s *service) RestartNode(ctx context.Context, nodeName string) error {
	if err := s.requireNodes(ctx, []string{nodeName}); err != nil {
		return err
	}
	msg := models.Message{
		Name:    nodeName,
		Type:    "restart",
		Content: "",
	}
	return s.RefreshConfig(ctx, msg)
}

// MoveQueuedTasks 把待开始任务从一个节点迁到另一个节点（缓解队列积压）。
func (s *service) MoveQueuedTasks(ctx context.Context, from, to string, limit int) (int, error) {
	return s.nodeRepo.MoveQueuedTasks(ctx, from, to, limit)
}

// EnsureMemSample 惰性写入内存采样
func (s *service) EnsureMemSample(ctx context.Context, name string, mem, running float64) {
	s.nodeRepo.EnsureMemSample(ctx, name, mem, running)
}

// MemPeaks 最近一小时每个节点的峰值/均值
func (s *service) MemPeaks(ctx context.Context) map[string][2]float64 {
	return s.nodeRepo.MemPeaks(ctx)
}

// MemHighMinutes 每个节点连续高压（≥90%）的分钟数
func (s *service) MemHighMinutes(ctx context.Context) map[string]int {
	return s.nodeRepo.MemHighMinutes(ctx)
}
