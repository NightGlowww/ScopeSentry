// Package task -----------------------------
// @file      : task.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2025/5/4 22:04
// -------------------------------------------
package task

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/constants"
	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/database/redis"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/utils/redislist"
	"github.com/gin-gonic/gin"
	redisdriver "github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// TaskQueueTTL 是任务队列（TaskInfo:<taskID> / NodeTask:<node>）的滑动存活时间。
//
// issue #200：这两类队列以前只在“删除任务”时清理，节点长期离线或被移除后，
// 消息会永久留在 Redis 里。每次投递都会把整条队列的 TTL 续上，所以：
//   - 一直在投递的队列不会中途过期；
//   - 没人消费的队列在最后一次投递 30 天后自动回收；
//   - 节点重新上线会由 checkNodeTask 重新投递待开始/运行中的任务。
//
// 取 30 天而不是几天：任务队列是“节点领了才消费”，节点维护一周以上很常见，
// TTL 太短会把还在排队的任务清掉。30 天足够覆盖运维窗口，又能止住无限增长。
const TaskQueueTTL = 30 * 24 * time.Hour

// Repository 定义任务数据访问接口
type Repository interface {
	Count(ctx *gin.Context, filter interface{}) (int64, error)
	Find(ctx context.Context, filter interface{}, opts *options.FindOptions) ([]models.Task, error)
	FindByID(ctx context.Context, id primitive.ObjectID) (*models.Task, error)
	Insert(ctx context.Context, task *models.Task) (string, error) // 新增方法
	ClearTaskCache(ctx context.Context, id string) error
	PushTaskInfoList(ctx context.Context, id string, targetList []string) error
	FindTemplateByID(ctx context.Context, id string) (*models.ScanTemplate, error)
	GetCollectionCursor(ctx *gin.Context, collection string, filter bson.M) (*mongo.Cursor, error)
	RPushNodeTask(ctx context.Context, name string, data any) error
	CheckTaskNameExists(ctx context.Context, name string) (bool, error)
	DeleteMany(ctx *gin.Context, ids []primitive.ObjectID) (int64, error)
	UpdateOne(ctx context.Context, filter bson.M, update bson.M) error
	// UpdateMany 批量更新（项目归属补录需要一次改多条）
	UpdateMany(ctx context.Context, filter bson.M, update bson.M) (*mongo.UpdateResult, error)
	GetProgressFromRedis(ctx *gin.Context, key string) (map[string]string, error)
	// 新增Redis操作方法
	Exists(ctx context.Context, key string) (bool, error)
	// ProgressSummary 汇总节点写入的逐目标进度 hash：
	// key 形如 TaskInfo:progress:<taskId>:<target>，hash 内是各模块的 <Module>_start/_end 时间戳。
	// 返回目标总数、已完成目标数、以及最晚的 end 时间（用于 task.endTime）。
	ProgressSummary(ctx context.Context, taskID string) (targets int, completedTargets int, partial float64, lastEnd string, err error)
	SCard(ctx context.Context, key string) (int64, error)
	Get(ctx context.Context, key string) (string, error)
	Del(ctx context.Context, keys ...string) error
	// 任务完成后的进度明细快照（issue #278）
	CollectProgressSnapshot(ctx context.Context, taskID string) ([]models.TaskProgressSnapshot, error)
	SaveProgressSnapshot(ctx context.Context, taskID string, rows []models.TaskProgressSnapshot) error
	GetProgressSnapshot(ctx context.Context, taskID string) (map[string]map[string]string, error)
	DeleteProgressSnapshot(ctx context.Context, taskIDs ...string) error
	// RemoveQueuedNodeTasks 清掉 NodeTask:* 里还没被领取的该任务消息
	RemoveQueuedNodeTasks(ctx context.Context, taskID string) (int64, error)
	// HasQueuedTask 任务是否仍在某个节点的待领取队列里
	HasQueuedTask(ctx context.Context, taskID string) (bool, error)
	// NodeQueueLen 节点待领取队列长度
	NodeQueueLen(ctx context.Context, node string) (int64, error)
	// TaskCmdFail 任务期间工具失败总数
	TaskCmdFail(ctx context.Context, taskID string) int64
	// TaskOutcome 任务产出统计（按任务名计数）
	TaskOutcome(ctx context.Context, taskName string) map[string]int64
	// CountTasksCreatedSince 统计某时间之后创建的任务数
	CountTasksCreatedSince(ctx context.Context, since string) (int64, error)
	// TaskQueueSnapshot 任务在节点队列中的位次 + 快照里带哪些重模块
	TaskQueueSnapshot(ctx context.Context) map[string]models.TaskQueueEntry
	// RecentTaskDurationsMinutes 最近若干已完成任务的单任务耗时样本（分钟，未过滤）
	RecentTaskDurationsMinutes(ctx context.Context, limit int64) []float64
	// CountQueuedByPrefix 统计同前缀任务在节点队列里的数量（公平性限制用）
	CountQueuedByPrefix(ctx context.Context, node, prefix string) (int, error)
	// CurrentActivity 当前正在执行的模块/目标/开始时间
	CurrentActivity(ctx context.Context, taskID string) (module string, target string, since string)
	// 目标级暂停（issue #277）
	AddTargetControl(ctx context.Context, taskID, target string) error
	RemoveTargetControl(ctx context.Context, taskID, target string) error
	ListTargetControls(ctx context.Context, taskID string) ([]models.TaskTargetControl, error)
	RemoveTaskTarget(ctx context.Context, taskID, target string) (int64, error)
	PrependTaskTarget(ctx context.Context, taskID, target string) error
}

// repository 实现Repository接口
type repository struct {
	db          *mongo.Database
	redisClient *redisdriver.Client
}

// NewRepository 创建任务数据访问实例
func NewRepository() Repository {
	return &repository{
		db:          mongodb.DB,
		redisClient: redis.Client,
	}
}

// Count 统计文档数量
func (r *repository) Count(ctx *gin.Context, filter interface{}) (int64, error) {
	return r.db.Collection("task").CountDocuments(ctx.Request.Context(), filter)
}

// Find 查询多个文档
func (r *repository) Find(ctx context.Context, filter interface{}, opts *options.FindOptions) ([]models.Task, error) {
	cursor, err := r.db.Collection("task").Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var tasks []models.Task
	if err = cursor.All(ctx, &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

// FindByID 根据ID查询单个任务
func (r *repository) FindByID(ctx context.Context, id primitive.ObjectID) (*models.Task, error) {
	var task models.Task
	err := r.db.Collection("task").FindOne(ctx, bson.M{"_id": id}).Decode(&task)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil // 没找到返回 nil
		}
		return nil, fmt.Errorf("failed to find task by ID: %w", err)
	}
	return &task, nil
}

// Insert 插入一个新任务文档
func (r *repository) Insert(ctx context.Context, task *models.Task) (string, error) {
	result, err := r.db.Collection("task").InsertOne(ctx, task)
	if err != nil {
		return "", err
	}
	// 尝试将 InsertedID 转换为 ObjectID 并返回其 Hex 字符串
	if oid, ok := result.InsertedID.(primitive.ObjectID); ok {
		return oid.Hex(), nil
	}
	return "", fmt.Errorf("failed to convert InsertedID to ObjectID")
}

func (r *repository) ClearTaskCache(ctx context.Context, id string) error {
	keysToDelete := []string{
		fmt.Sprintf("TaskInfo:tmp:%s", id),
		fmt.Sprintf("TaskInfo:%s", id),
		fmt.Sprintf("TaskInfo:time:%s", id),
		// 新增的两类辅助键也要一起清，否则删了任务 Redis 里还留着
		fmt.Sprintf("TaskInfo:cmdfail:%s", id),
		fmt.Sprintf("TaskInfo:progmax:%s", id),
		fmt.Sprintf("TaskInfo:progdone:%s", id),
	}

	// 查找 progress 相关 key
	progressKeys, err := r.redisClient.Keys(ctx, fmt.Sprintf("TaskInfo:progress:%s:*", id)).Result()
	if err != nil {
		return fmt.Errorf("failed to get progress keys: %w", err)
	}
	keysToDelete = append(keysToDelete, progressKeys...)

	// 查找 duplicates 相关 key
	duplicateKeys, err := r.redisClient.Keys(ctx, fmt.Sprintf("duplicates:%s:*", id)).Result()
	if err != nil {
		return fmt.Errorf("failed to get duplicate keys: %w", err)
	}
	keysToDelete = append(keysToDelete, duplicateKeys...)

	// 删除所有 key
	if len(keysToDelete) > 0 {
		if err := r.redisClient.Del(ctx, keysToDelete...).Err(); err != nil {
			return fmt.Errorf("failed to delete redis keys: %w", err)
		}
	}

	// 任务的进度快照也要一起清掉，避免删任务后留下孤儿文档（issue #278）
	if err := r.DeleteProgressSnapshot(ctx, id); err != nil {
		return err
	}
	// 队列里还没被节点领取的消息同样要清，否则节点重新上线会把已删除的任务跑起来
	_, _ = r.RemoveQueuedNodeTasks(ctx, id)
	return nil
}

func (r *repository) PushTaskInfoList(ctx context.Context, id string, targetList []string) error {
	if len(targetList) == 0 {
		return nil
	}

	// 将字符串切片转为 interface{} 类型的切片
	values := make([]interface{}, len(targetList))
	for i, v := range targetList {
		values[i] = v
	}

	// 推入 Redis 列表。issue #200：队列只在删任务时清，节点长期不消费会一直堆着，
	// 这里给整条队列滑动 TTL，没人领的旧消息会自动回收。
	// 保持 LPush（最新在前）语义，避免节点侧消费顺序变化。
	key := fmt.Sprintf("TaskInfo:%s", id)
	if err := redislist.PushQueuedHead(ctx, r.redisClient, key, TaskQueueTTL, values...); err != nil {
		return fmt.Errorf("failed to lpush TaskInfo list: %w", err)
	}

	return nil
}

func (r *repository) FindTemplateByID(ctx context.Context, id string) (*models.ScanTemplate, error) {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, fmt.Errorf("invalid ObjectID: %w", err)
	}

	var result models.ScanTemplate
	err = r.db.Collection("ScanTemplates").FindOne(ctx, bson.M{"_id": objectID}).Decode(&result)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil // 没找到返回 nil
		}
		return nil, fmt.Errorf("failed to find template: %w", err)
	}

	return &result, nil
}

// GetCollectionCursor 获取集合游标
func (r *repository) GetCollectionCursor(ctx *gin.Context, collection string, filter bson.M) (*mongo.Cursor, error) {
	return r.db.Collection(collection).Find(ctx.Request.Context(), filter)
}

// nodeTaskPushMu 串行化「查重 + 入队」：
// 两个来源（建任务、节点注册时的补投）可能同时推同一任务，
// 各自读队列时都还没看到对方写入，结果同一条消息出现两次（2026-11-20 实测）。
var nodeTaskPushMu sync.Mutex

func (r *repository) RPushNodeTask(ctx context.Context, name string, data any) error {
	key := fmt.Sprintf("NodeTask:%s", name)

	// 将 data 序列化为 JSON
	value, err := json.Marshal(data)
	if err != nil {
		return err
	}

	nodeTaskPushMu.Lock()
	defer nodeTaskPushMu.Unlock()

	// 入队前查重：该节点队列里已有同一任务就不再加一条。
	// 节点是 LPOP 原子出队，所以「队列里没有」等价于「还没被领走」。
	var probe struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(value, &probe) == nil && probe.ID != "" {
		if items, lerr := r.redisClient.LRange(ctx, key, 0, -1).Result(); lerr == nil {
			needle := fmt.Sprintf("\"id\":\"%s\"", probe.ID)
			found := 0
			var firstRaw string
			for _, raw := range items {
				if strings.Contains(raw, needle) {
					found++
					if firstRaw == "" {
						firstRaw = raw
					}
				}
			}
			if found > 0 {
				// 已经不在了就不要再加；顺手把历史遗留的多余副本清掉（只留一条）。
				// 队列里的重复条目会让节点把同一个任务跑两遍（2026-11-20 实测仍有残留）。
				if found > 1 {
					if _, rerr := r.redisClient.LRem(ctx, key, int64(found-1), firstRaw).Result(); rerr == nil {
						logger.Warn(fmt.Sprintf("queue %s: removed %d duplicate entr(ies) for task %s", key, found-1, probe.ID))
					}
				}
				return nil
			}
		}
	}

	// 使用 Redis RPush 插入数据，并给队列滑动 TTL（issue #200）
	return redislist.PushQueued(ctx, r.redisClient, key, TaskQueueTTL, value)
}

// CheckTaskNameExists 检查任务名是否存在
func (r *repository) CheckTaskNameExists(ctx context.Context, name string) (bool, error) {
	filter := bson.M{"name": name}
	count, err := r.db.Collection("task").CountDocuments(ctx, filter)
	if err != nil {
		return false, fmt.Errorf("failed to check task name: %w", err)
	}
	return count > 0, nil
}

func (r *repository) DeleteMany(ctx *gin.Context, ids []primitive.ObjectID) (int64, error) {
	result, err := r.db.Collection("task").DeleteMany(ctx.Request.Context(), bson.M{"_id": bson.M{"$in": ids}})
	if err != nil {
		return 0, err
	}

	// 连带删除进度快照，避免孤儿文档（issue #278）。删快照失败不应让任务删除报错，
	// 但要让运维看到日志。
	if result.DeletedCount > 0 {
		hexIDs := make([]string, 0, len(ids))
		for _, id := range ids {
			hexIDs = append(hexIDs, id.Hex())
		}
		if err := r.DeleteProgressSnapshot(ctx.Request.Context(), hexIDs...); err != nil {
			logger.Error(fmt.Sprintf("failed to delete progress snapshot: %v", err))
		}
	}
	return result.DeletedCount, nil
}

func (r *repository) UpdateMany(ctx context.Context, filter bson.M, update bson.M) (*mongo.UpdateResult, error) {
	return r.db.Collection("task").UpdateMany(ctx, filter, update)
}

func (r *repository) UpdateOne(ctx context.Context, filter bson.M, update bson.M) error {
	_, err := r.db.Collection("task").UpdateOne(ctx, filter, update)
	return err
}

// GetProgressFromRedis 从Redis获取进度信息
func (r *repository) GetProgressFromRedis(ctx *gin.Context, key string) (map[string]string, error) {
	result, err := r.redisClient.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get progress from redis: %w", err)
	}
	return result, nil
}

// Exists 检查Redis键是否存在
func (r *repository) Exists(ctx context.Context, key string) (bool, error) {
	result, err := r.redisClient.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check key existence: %w", err)
	}
	return result > 0, nil
}

// SCard 获取集合中元素的数量
func (r *repository) SCard(ctx context.Context, key string) (int64, error) {
	result, err := r.redisClient.SCard(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get set cardinality: %w", err)
	}
	return result, nil
}

// Get 获取Redis键的值
func (r *repository) Get(ctx context.Context, key string) (string, error) {
	result, err := r.redisClient.Get(ctx, key).Result()
	if err != nil {
		if err == redisdriver.Nil {
			return "", nil // 键不存在返回空字符串
		}
		return "", fmt.Errorf("failed to get key value: %w", err)
	}
	return result, nil
}

// Del 删除Redis键
func (r *repository) Del(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	_, err := r.redisClient.Del(ctx, keys...).Result()
	if err != nil {
		return fmt.Errorf("failed to delete keys: %w", err)
	}
	return nil
}

// ProgressSummary 见接口注释。节点从不写 TaskInfo:tmp:<id>，真正的进度在逐目标 hash 里，
// 之前服务端只认 tmp 集合，导致任务进度永远是 0。
func (r *repository) ProgressSummary(ctx context.Context, taskID string) (int, int, float64, string, error) {
	keys, err := r.scanKeys(ctx, fmt.Sprintf("TaskInfo:progress:%s:*", taskID))
	if err != nil {
		return 0, 0, 0, "", fmt.Errorf("failed to list progress keys: %w", err)
	}
	lastEnd := ""
	completed := 0
	partial := 0.0
	// 每个目标的完成比例要单调：模块数会随执行推进增加，
	// 直接算 ended/started 会让同一目标的比例回退（实测 61%→33%）。
	hwmKey := fmt.Sprintf("TaskInfo:progmax:%s", taskID)
	hwm, _ := r.redisClient.HGetAll(ctx, hwmKey).Result()
	if hwm == nil {
		hwm = map[string]string{}
	}
	hwmUpdates := map[string]interface{}{}
	for _, key := range keys {
		fields, err := r.redisClient.HGetAll(ctx, key).Result()
		if err != nil {
			continue
		}
		started, ended := 0, 0
		for name, ts := range fields {
			if name == "node" || ts == "" {
				continue
			}
			if strings.HasSuffix(name, "_start") {
				started++
			} else if strings.HasSuffix(name, "_end") {
				ended++
				if ts > lastEnd {
					lastEnd = ts
				}
			}
		}
		if started > 0 && ended >= started {
			completed++
		} else if started > 0 {
			f := float64(ended) / float64(started)
			if f > 0.99 {
				f = 0.99
			}
			// 与历史高水位取大：模块变多时比例不倒退
			if prev, ok := hwm[key]; ok {
				if pf, err := strconv.ParseFloat(prev, 64); err == nil && pf > f {
					f = pf
				}
			}
			hwmUpdates[key] = f
			partial += f
		}
	}
	// 已完成目标数也要存高水位：节点跑完一个目标后会删掉它的 hash，
	// 该目标就从 statistics 里消失，完成数会倒退（实测 71%→66%）。
	doneKey := fmt.Sprintf("TaskInfo:progdone:%s", taskID)
	if prevStr, err := r.redisClient.Get(ctx, doneKey).Result(); err == nil {
		if prevDone, perr := strconv.Atoi(prevStr); perr == nil && prevDone > completed {
			completed = prevDone
		}
	}
	if completed > 0 {
		_ = r.redisClient.Set(ctx, doneKey, strconv.Itoa(completed), TaskQueueTTL).Err()
	}

	if len(hwmUpdates) > 0 {
		// 顺手把高水位写回，TTL 与任务队列一致，避免永久堆积
		_ = r.redisClient.HMSet(ctx, hwmKey, hwmUpdates).Err()
		_ = r.redisClient.Expire(ctx, hwmKey, TaskQueueTTL).Err()
	}
	return len(keys), completed, partial, lastEnd, nil
}

// HasQueuedTask 判断该任务是否还躺在任一节点的 NodeTask 队列里（还没被领取）。
// 用于僵尸判定：既没有进度 hash、又不在队列里 = 没有任何节点会再跑它。
func (r *repository) HasQueuedTask(ctx context.Context, taskID string) (bool, error) {
	keys, err := r.scanKeys(ctx, "NodeTask:*")
	if err != nil {
		return false, err
	}
	for _, key := range keys {
		items, err := r.redisClient.LRange(ctx, key, 0, -1).Result()
		if err != nil {
			continue
		}
		for _, item := range items {
			if strings.Contains(item, taskID) {
				return true, nil
			}
		}
	}
	return false, nil
}

// CurrentActivity 返回任务当前正在执行的模块与目标。
//
// 列表页只显示百分比时，长任务（目录扫描单目标 7 分钟）看起来像卡死。
// 这里从逐目标进度 hash 里找「有 _start、没有 _end」的最新模块，
// 用它的开始时间算已执行时长，让界面能显示「正在目录扫描 xxx，已 7 分钟」。
func (r *repository) CurrentActivity(ctx context.Context, taskID string) (module string, target string, since string) {
	keys, err := r.scanKeys(ctx, fmt.Sprintf("TaskInfo:progress:%s:*", taskID))
	if err != nil {
		return "", "", ""
	}
	latest := ""
	for _, key := range keys {
		tgt := key[strings.LastIndex(key, ":")+1:]
		fields, err := r.redisClient.HGetAll(ctx, key).Result()
		if err != nil {
			continue
		}
		for name, ts := range fields {
			if name == "node" || ts == "" {
				continue
			}
			if !strings.HasSuffix(name, "_start") {
				continue
			}
			mod := strings.TrimSuffix(name, "_start")
			if end, ok := fields[mod+"_end"]; ok && end != "" {
				continue // 该模块已结束
			}
			if ts > latest {
				latest = ts
				module = mod
				target = tgt
				since = ts
			}
		}
	}
	return module, target, since
}

// NodeQueueLen 返回某节点待领取队列的长度（背压判断用）。
func (r *repository) NodeQueueLen(ctx context.Context, node string) (int64, error) {
	return r.redisClient.LLen(ctx, "NodeTask:"+node).Result()
}

// TaskCmdFail 返回该任务期间各节点上报的外部工具失败次数之和。
// 节点在任务结束时写入 TaskInfo:cmdfail:<taskID>（field=节点名）。
func (r *repository) TaskCmdFail(ctx context.Context, taskID string) int64 {
	m, err := r.redisClient.HGetAll(ctx, "TaskInfo:cmdfail:"+taskID).Result()
	if err != nil || len(m) == 0 {
		return 0
	}
	var total int64
	for _, v := range m {
		if n, perr := strconv.ParseInt(v, 10, 64); perr == nil {
			total += n
		}
	}
	return total
}

// TaskOutcome 统计某个任务名在各集合里的产出数量（任务详情页用）。
// 走 taskName 索引；集合缺字段或不存在时按 0 处理，不阻断页面。
func (r *repository) TaskOutcome(ctx context.Context, taskName string) map[string]int64 {
	out := map[string]int64{"subdomain": 0, "url": 0, "asset": 0, "vulnerability": 0}
	if strings.TrimSpace(taskName) == "" {
		return out
	}
	specs := []struct {
		key  string
		coll string
	}{
		{"subdomain", "subdomain"},
		{"url", "UrlScan"},
		{"asset", "asset"},
		{"vulnerability", "vulnerability"},
	}
	for _, s := range specs {
		n, err := r.db.Collection(s.coll).CountDocuments(ctx, bson.M{"taskName": taskName})
		if err == nil {
			out[s.key] = n
		}
	}
	return out
}

// CountQueuedByPrefix 统计某节点队列里 taskName 以 prefix 开头的待执行任务数。
// 用于「同一来源不能占满队列」的公平性限制（2026-11-20）。
func (r *repository) CountQueuedByPrefix(ctx context.Context, node, prefix string) (int, error) {
	items, err := r.redisClient.LRange(ctx, "NodeTask:"+node, 0, -1).Result()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, raw := range items {
		var payload struct {
			TaskName string `json:"TaskName"`
			Name     string `json:"name"`
		}
		if json.Unmarshal([]byte(raw), &payload) != nil {
			continue
		}
		name := payload.TaskName
		if name == "" {
			name = payload.Name
		}
		if strings.HasPrefix(name, prefix) {
			n++
		}
	}
	return n, nil
}

// RecentTaskDurationsMinutes 最近 limit 个已完成任务的单任务耗时（分钟）。
// 只回样本，不做统计：均值会被「暂停过夜」「自动重投」这类异常值拉爆
// （2026-11-29 实测均值算出 7 小时/任务，实际约 40 分钟），口径交给可单测的纯函数。
func (r *repository) RecentTaskDurationsMinutes(ctx context.Context, limit int64) []float64 {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	// 只看已完成的；时间是「开始/结束」两个字符串字段。
	// startedAt 是 2026-11-29 才补上的写入（此前只有模型定义、没人写），
	// 所以历史任务只能退回 creatTime：那段耗时包含排队等待，属于上限估计。
	// 上限比「未知」有用：队列里 6 个任务要不要再加一个，靠它判断。
	cur, err := r.db.Collection("task").Find(ctx,
		bson.M{"status": 3, "endTime": bson.M{"$ne": ""}},
		options.Find().SetSort(bson.D{{Key: "endTime", Value: -1}}).
			SetLimit(limit).
			SetProjection(bson.M{"startedAt": 1, "creatTime": 1, "endTime": 1}))
	if err != nil {
		return nil
	}
	defer cur.Close(ctx)
	const layout = "2006-01-02 15:04:05"
	out := make([]float64, 0, limit)
	for cur.Next(ctx) {
		var row struct {
			StartedAt string `bson:"startedAt"`
			CreatTime string `bson:"creatTime"`
			EndTime   string `bson:"endTime"`
		}
		if cur.Decode(&row) != nil {
			continue
		}
		startRaw := row.StartedAt
		if startRaw == "" {
			startRaw = row.CreatTime
		}
		start, err1 := time.ParseInLocation(layout, startRaw, time.Local)
		end, err2 := time.ParseInLocation(layout, row.EndTime, time.Local)
		if err1 != nil || err2 != nil || !end.After(start) {
			continue
		}
		out = append(out, end.Sub(start).Minutes())
	}
	return out
}

// CountTasksCreatedSince 统计某时间点之后创建的任务数（防刷用）。
// creatTime 是 "YYYY-MM-DD HH:MM:SS" 字符串，按字符串比较即可（同格式定长）。
func (r *repository) CountTasksCreatedSince(ctx context.Context, since string) (int64, error) {
	return r.db.Collection("task").CountDocuments(ctx, bson.M{"creatTime": bson.M{"$gte": since}})
}

// TaskQueueSnapshot 返回「任务 → 队列位次 + 快照里带哪些重模块」。
// 队列是 Redis 列表 NodeTask:<node>，元素是**提交时序列化**的任务 JSON。
// 一次性把所有节点队列扫一遍，避免每个任务单查一次（2026-11-20）。
//
// 为什么连插件清单一起读：队列条目是快照，模板/档位后来改了都不影响它。
// 只报位次的话，「明明把 nuclei 关了，这条排队任务还在跑 nuclei」就无解——
// 现场（2026-11-29）就是这样：模板早就没有 nuclei 插件了，排队的旧任务照跑 20 分钟。
func (r *repository) TaskQueueSnapshot(ctx context.Context) map[string]models.TaskQueueEntry {
	out := map[string]models.TaskQueueEntry{}
	keys, err := r.scanKeys(ctx, "NodeTask:*")
	if err != nil {
		return out
	}
	for _, key := range keys {
		items, err := r.redisClient.LRange(ctx, key, 0, -1).Result()
		if err != nil {
			continue
		}
		for i, raw := range items {
			var payload struct {
				ID                string   `json:"id"`
				VulnerabilityScan []string `json:"VulnerabilityScan"`
				DirScan           []string `json:"DirScan"`
			}
			if err := json.Unmarshal([]byte(raw), &payload); err != nil || payload.ID == "" {
				continue
			}
			entry := models.TaskQueueEntry{
				Pos:     i + 1,
				Nuclei:  containsHash(payload.VulnerabilityScan, constants.PluginNuclei),
				DirScan: containsHash(payload.DirScan, constants.PluginDirScan),
			}
			// 同一个任务在多个节点队列里出现时取最靠前的位置
			if prev, ok := out[payload.ID]; !ok || entry.Pos < prev.Pos {
				out[payload.ID] = entry
			}
		}
	}
	return out
}

// containsHash 判断插件清单里是否有某个 hash（空清单 = 该模块不会跑）。
func containsHash(list []string, hash string) bool {
	for _, h := range list {
		if h == hash {
			return true
		}
	}
	return false
}

// scanKeys 用 SCAN 迭代代替 KEYS。
// KEYS 是 O(N) 且会阻塞 Redis（单线程），任务列表每刷一次都调（TaskQueuePositions）
// 等于每次都扫全库；键一多就是肉眼可见的卡顿。SCAN 增量迭代、不阻塞（2026-11-20）。
func (r *repository) scanKeys(ctx context.Context, pattern string) ([]string, error) {
	var keys []string
	var cursor uint64
	for {
		batch, next, err := r.redisClient.Scan(ctx, cursor, pattern, 200).Result()
		if err != nil {
			return keys, err
		}
		keys = append(keys, batch...)
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return keys, nil
}
