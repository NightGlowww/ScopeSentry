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
	"time"

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

func (r *repository) RPushNodeTask(ctx context.Context, name string, data any) error {
	key := fmt.Sprintf("NodeTask:%s", name)

	// 将 data 序列化为 JSON
	value, err := json.Marshal(data)
	if err != nil {
		return err
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
	keys, err := r.redisClient.Keys(ctx, fmt.Sprintf("TaskInfo:progress:%s:*", taskID)).Result()
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
	keys, err := r.redisClient.Keys(ctx, "NodeTask:*").Result()
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
	keys, err := r.redisClient.Keys(ctx, fmt.Sprintf("TaskInfo:progress:%s:*", taskID)).Result()
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
