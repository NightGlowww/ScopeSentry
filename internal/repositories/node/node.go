package node

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/utils/redislist"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/database/redis"

	redisdriver "github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Repository interface {
	GetAllNodes(ctx context.Context) ([]models.NodeData, error)
	UpdateNodeState(ctx context.Context, nodeID string, state string) error
	RefreshConfig(ctx context.Context, node string, msg string) error
	DeleteNodes(ctx context.Context, names []string) error
	GetNodeLogs(ctx context.Context, name string) (string, error)
	GetNodePlugin(ctx context.Context, nodeName string) ([]models.NodePluginInfo, error)
	// NodeQueueLen 节点待领取队列长度
	NodeQueueLen(ctx context.Context, name string) (int64, error)
	// ClearNodeQueue 清空节点待领取队列
	ClearNodeQueue(ctx context.Context, name string) (int64, error)
	// MoveQueuedTasks 把待开始任务从一个节点的队列迁到另一个节点
	MoveQueuedTasks(ctx context.Context, from, to string, limit int) (int, error)
	// EnsureMemSample 惰性写入容器内存采样（4 分钟一条）
	EnsureMemSample(ctx context.Context, name string, mem, running float64)
	// MemPeaks 最近一小时每个节点的内存峰值/均值
	MemPeaks(ctx context.Context) map[string][2]float64
	// MemHighMinutes 每个节点连续高压（≥90%）的分钟数
	MemHighMinutes(ctx context.Context) map[string]int
}

type repository struct {
	redisClient *redisdriver.Client
	collection  *mongo.Collection
}

func NewRepository() Repository {
	return &repository{
		redisClient: redis.Client,
		collection:  mongodb.DB.Collection("plugins"),
	}
}

// GetAllNodes 获取所有节点
func (r *repository) GetAllNodes(ctx context.Context) ([]models.NodeData, error) {
	// Redis 未初始化（降级启动/单元测试）时返回错误而不是 nil deref：
	// 调用方 RefreshConfig 只记日志，不能让进程崩在后台 goroutine 里。
	if r.redisClient == nil {
		return nil, fmt.Errorf("redis client is not initialized")
	}
	// 获取所有以 node: 开头的键
	keys, err := r.redisClient.Keys(ctx, "node:*").Result()
	if err != nil {
		return nil, err
	}

	var nodes []models.NodeData
	for _, key := range keys {
		// 获取哈希中的所有字段和值
		hashData, err := r.redisClient.HGetAll(ctx, key).Result()
		if err != nil {
			continue
		}

		name := strings.TrimPrefix(key, "node:")
		// 空名字的 node 键（历史遗留/启动时 NodeName 为空）会在界面显示成一台离线节点，直接跳过
		if strings.TrimSpace(name) == "" {
			continue
		}

		// 构造结构体，提供默认值
		node := models.NodeData{
			Name:          name,
			State:         hashData["state"],
			UpdateTime:    hashData["updateTime"],
			Running:       hashData["running"],
			Finished:      hashData["finished"],
			CPUNum:        hashData["cpuNum"],
			MemNum:        hashData["memNum"],
			MaxTaskNum:    hashData["maxTaskNum"],
			Version:       hashData["version"],
			ModulesConfig: hashData["modulesConfig"],
		}
		// 外部工具失败累计：历史节点没有这个字段，缺失按 0 处理
		if v, ok := hashData["cmdFail"]; ok {
			if n, perr := strconv.ParseInt(v, 10, 64); perr == nil {
				node.CmdFail = n
			}
		}
		if v, ok := hashData["cmdFail10m"]; ok {
			if n, perr := strconv.ParseInt(v, 10, 64); perr == nil {
				node.CmdFail10m = n
			}
		}
		// 容器内存占用：老版本节点没有这个字段，缺失保持 0（界面按「不支持」处理）
		if v, ok := hashData["containerMem"]; ok {
			if f, perr := strconv.ParseFloat(v, 64); perr == nil {
				node.ContainerMem = f
			}
		}
		nodes = append(nodes, node)
	}

	return nodes, nil
}

// UpdateNodeState 更新节点状态
func (r *repository) UpdateNodeState(ctx context.Context, nodeID string, state string) error {
	key := "node:" + nodeID
	_, err := r.redisClient.HSet(ctx, key, "state", state).Result()
	return err
}

// refreshConfigMaxLen 每个节点的配置刷新队列最多保留多少条。
//
// 配置刷新是「状态型」消息（指纹/字典/系统配置变更后通知节点重载），节点只关心近期的几条；
// 节点离线时旧实现会无限堆积，删节点后这些 key 也永远没人清。
const refreshConfigMaxLen = 100

// refreshConfigTTL 配置刷新队列的存活时间：节点长期离线后自动回收。
const refreshConfigTTL = 7 * 24 * time.Hour

// RefreshConfig 向指定节点的任务列表右侧插入一条刷新消息（限长 + 滑动 TTL）
func (r *repository) RefreshConfig(ctx context.Context, node string, msg string) error {
	key := "refresh_config:" + node
	return redislist.PushQueuedCapped(ctx, r.redisClient, key, refreshConfigMaxLen, refreshConfigTTL, msg)
}

// DeleteNodes 删除多个节点，并清掉跟它相关的队列。
//
// 只删 node:<name> 会留下 refresh_config:<name> / NodeTask:<name> 这些孤儿 key：
// 节点已经不在列表里，消息永远不会被消费。
func (r *repository) DeleteNodes(ctx context.Context, names []string) error {
	for _, name := range names {
		keys := []string{"node:" + name, "refresh_config:" + name, "NodeTask:" + name}
		if err := r.redisClient.Del(ctx, keys...).Err(); err != nil {
			return err
		}
	}
	return nil
}

// GetNodeLogs 获取节点日志
func (r *repository) GetNodeLogs(ctx context.Context, name string) (string, error) {
	logKey := "log:" + name
	logs, err := r.redisClient.LRange(ctx, logKey, 0, -1).Result()
	if err != nil {
		return "", err
	}
	return strings.Join(logs, ""), nil
}

// GetNodePlugin 获取节点插件信息
func (r *repository) GetNodePlugin(ctx context.Context, nodeName string) ([]models.NodePluginInfo, error) {
	// 从MongoDB获取所有插件信息
	projection := bson.M{
		"_id":    0,
		"name":   1,
		"hash":   1,
		"module": 1,
	}

	cursor, err := r.collection.Find(ctx, bson.M{}, options.Find().SetProjection(projection))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var plugins []models.Plugin
	if err := cursor.All(ctx, &plugins); err != nil {
		return nil, err
	}

	// 构建插件映射
	pluginMap := make(map[string]models.Plugin)
	for _, plugin := range plugins {
		pluginMap[plugin.Hash] = plugin
	}

	// 从Redis获取节点插件状态
	key := "NodePlg:" + nodeName
	hashData, err := r.redisClient.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	var result []models.NodePluginInfo
	for hash, plugin := range pluginMap {
		installValue := hashData[hash+"_install"]
		checkValue := hashData[hash+"_check"]

		// 如果Redis中没有数据，默认为"0"
		if installValue == "" {
			installValue = "0"
		}
		if checkValue == "" {
			checkValue = "0"
		}

		result = append(result, models.NodePluginInfo{
			Name:    plugin.Name,
			Install: installValue,
			Check:   checkValue,
			Hash:    hash,
			Module:  plugin.Module,
		})
	}

	return result, nil
}

// NodeQueueLen 返回该节点待领取队列长度（NodeTask:<name>）。
func (r *repository) NodeQueueLen(ctx context.Context, name string) (int64, error) {
	return r.redisClient.LLen(ctx, "NodeTask:"+name).Result()
}

// ClearNodeQueue 清空该节点待领取队列，返回清掉的条数。
// 场景：任务洪泛把队列堆满后，真实任务永远排不上；界面需要一键清空入口
// （此前只能在服务器上 redis-cli DEL）。
func (r *repository) ClearNodeQueue(ctx context.Context, name string) (int64, error) {
	key := "NodeTask:" + name
	n, err := r.redisClient.LLen(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, nil
	}
	return n, r.redisClient.Del(ctx, key).Err()
}

// MoveQueuedTasks 把 from 节点待领取队列里的「待开始」任务迁到 to 节点。
// 场景：某节点积压（超过背压上限后新任务被拒），而其他节点空转，
// 手工改 Mongo+Redis 才能搬——2026-11-20 实测堵死过一次。
// 只迁 status=0（待开始）的任务：运行中的任务已经在该节点上跑，不能换。
func (r *repository) MoveQueuedTasks(ctx context.Context, from, to string, limit int) (int, error) {
	if from == "" || to == "" || from == to {
		return 0, fmt.Errorf("源节点和目标节点必须是两个不同的节点")
	}
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	srcKey := "NodeTask:" + from
	dstKey := "NodeTask:" + to
	items, err := r.redisClient.LRange(ctx, srcKey, 0, -1).Result()
	if err != nil {
		return 0, err
	}
	moved := 0
	for _, raw := range items {
		if moved >= limit {
			break
		}
		var payload struct {
			ID string `json:"id"`
		}
		if json.Unmarshal([]byte(raw), &payload) != nil || payload.ID == "" {
			continue
		}
		oid, oerr := primitive.ObjectIDFromHex(payload.ID)
		if oerr != nil {
			continue
		}
		// 只搬还在排队（未开始）的任务
		res, uerr := mongodb.DB.Collection("task").UpdateOne(ctx,
			bson.M{"_id": oid, "status": 0},
			bson.M{"$set": bson.M{"node": []string{to}}})
		if uerr != nil || res.ModifiedCount == 0 {
			continue
		}
		if _, lerr := r.redisClient.LRem(ctx, srcKey, 1, raw).Result(); lerr != nil {
			continue
		}
		if perr := redislist.PushQueued(ctx, r.redisClient, dstKey, 30*time.Minute, raw); perr != nil {
			// 推失败就回滚 Mongo，避免任务既不在队列也不在原节点
			_, _ = mongodb.DB.Collection("task").UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": bson.M{"node": []string{from}}})
			continue
		}
		moved++
	}
	return moved, nil
}
