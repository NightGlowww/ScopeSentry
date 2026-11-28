package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/config"
	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/scheduler/jobs"
	"github.com/Autumn-27/ScopeSentry/internal/utils/helper"

	"github.com/go-co-op/gocron"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
)

// JobHandler 表示任务处理函数的类型
type JobHandler func(params string, nextTime string) error

// Job 表示一个计划任务
type Job struct {
	ID          string    `bson:"_id"`
	Name        string    `bson:"name"`
	Schedule    string    `bson:"schedule"` // cron 表达式
	Handler     string    `bson:"handler"`  // 处理函数名称
	Params      string    `bson:"params"`
	LastRunTime time.Time `bson:"last_run_time"`
	NextRunTime time.Time `bson:"next_run_time"`
	CreatedAt   time.Time `bson:"created_at"`
	UpdatedAt   time.Time `bson:"updated_at"`
	CycleType   string    `bson:"cycle_type"` // 周期类型：cron 或 interval
	Interval    struct {
		Days    int `bson:"days"`
		Hours   int `bson:"hours"`
		Minutes int `bson:"minutes"`
	} `bson:"interval"`
}

// Scheduler 表示计划任务调度器
type Scheduler struct {
	// started 标记 Start() 是否已执行；之后注册 handler 需要回头补排历史任务
	// mu 保护 jobs 映射与 started：AddJob/UpdateJob/RemoveJob 会来自并发 HTTP 请求，
	// gocron 自身线程安全，但 map 并发读写会让进程直接 panic。
	mu             sync.Mutex
	started        bool
	scheduler      *gocron.Scheduler
	db             *mongo.Database
	jobsCollection *mongo.Collection
	logger         *zap.Logger
	jobs           map[string]*gocron.Job
	handlers       map[string]JobHandler // 存储注册的处理函数
}

var (
	globalScheduler *Scheduler
	once            sync.Once
)

// 启动后注册的 handler（例如 github intel 在调度器 Start 之后才注册）
// 需要把库里已存在但当时被跳过的任务补排进来，否则它永远只在 DB 里躺着。

// GetGlobalScheduler 获取全局调度器实例（惰性初始化）。
//
// 以前这里直接返回 globalScheduler，而它要等 InitializeGlobalScheduler() 才会被建出来。
// 任何在初始化之前跑的删除/更新路径（例如 /api/task/scheduled/delete、
// 插件删除时摘调度任务）就会对 nil 取锁直接 nil pointer dereference，
// 被 Gin 的 Recovery 兜住后对外表现为 500（2026-11-28 集成测试实测）。
// InitializeGlobalScheduler 内部是 sync.Once，重复调用安全。
func GetGlobalScheduler() *Scheduler {
	InitializeGlobalScheduler()
	return globalScheduler
}

// InitializeGlobalScheduler 初始化全局调度器
func InitializeGlobalScheduler() {
	once.Do(func() {
		globalScheduler = NewScheduler()
		globalScheduler.RegisterHandler("scan", func(params string, nextTime string) error {
			return jobs.Scan(params, nextTime)
		})
		globalScheduler.RegisterHandler("page_monitoring", func(params string, nextTime string) error {
			return jobs.CreatePageMonitoringTask()
		})
		globalScheduler.RegisterHandler("server_plugin", func(params string, nextTime string) error {
			return jobs.ServerPluginRunner(params, nextTime)
		})
		// POC 每日同步：handler 名 poc_sync，未设置 POC_SYNC_URL 时函数内部直接返回
		globalScheduler.RegisterHandler("poc_sync", func(params string, nextTime string) error {
			return jobs.POCSync()
		})
	})
}

// NewScheduler 创建一个新的调度器实例
func NewScheduler() *Scheduler {
	loc, err := time.LoadLocation(config.GlobalConfig.System.Timezone)
	if err != nil {
		// 处理错误，例如使用默认时区
		loc = time.Local
	}
	return &Scheduler{
		scheduler:      gocron.NewScheduler(loc),
		db:             mongodb.DB,
		jobsCollection: mongodb.DB.Collection("scheduled"),
		logger:         logger.Logger,
		jobs:           make(map[string]*gocron.Job),
		handlers:       make(map[string]JobHandler),
	}
}

// RegisterHandler 注册一个任务处理函数
func (s *Scheduler) RegisterHandler(name string, handler JobHandler) {
	s.handlers[name] = handler
	s.logger.Info("Registered job handler", zap.String("handler_name", name))
	if s.isStarted() {
		s.schedulePendingForHandler(name)
	}
}

// schedulePendingForHandler 补排库里属于该 handler、但还没排进内存的任务。
// 场景：调度器 Start() 先于某个模块的 RegisterHandler()，loadJobs 只能跳过。
func (s *Scheduler) schedulePendingForHandler(name string) {
	cursor, err := s.jobsCollection.Find(context.Background(), bson.M{"handler": name})
	if err != nil {
		s.logger.Error("Failed to load pending jobs", zap.String("handler", name), zap.Error(err))
		return
	}
	defer cursor.Close(context.Background())
	for cursor.Next(context.Background()) {
		var job Job
		if err := cursor.Decode(&job); err != nil {
			continue
		}
		if s.hasJob(job.ID) {
			continue
		}
		if !s.scheduleJob(job) {
			continue
		}
		s.logger.Info("Late-registered handler scheduled its job",
			zap.String("job_id", job.ID), zap.String("handler", name))
	}
}

// Start 启动调度器
func (s *Scheduler) Start() {
	s.scheduler.StartAsync()
	s.loadJobs()
	s.mu.Lock()
	s.started = true
	s.mu.Unlock()
}

// isStarted 读 started（加锁）。
func (s *Scheduler) isStarted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}

// setJob / hasJob / getJob / takeJob 统一收口 jobs 映射的并发访问。
func (s *Scheduler) setJob(id string, j *gocron.Job) {
	s.mu.Lock()
	s.jobs[id] = j
	s.mu.Unlock()
}

func (s *Scheduler) hasJob(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.jobs[id]
	return ok
}

func (s *Scheduler) getJob(id string) (*gocron.Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	return j, ok
}

func (s *Scheduler) takeJob(id string) (*gocron.Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if ok {
		delete(s.jobs, id)
	}
	return j, ok
}

// Stop 停止调度器
func (s *Scheduler) Stop() {
	s.scheduler.Stop()
}

// AddJob 添加一个新的任务
func (s *Scheduler) AddJob(job *Job) error {
	// 检查处理函数是否已注册
	if _, exists := s.handlers[job.Handler]; !exists {
		return fmt.Errorf("handler %s not registered", job.Handler)
	}

	// 检查任务是否已经存在（内存中）
	if s.hasJob(job.ID) {
		// 任务已存在，直接返回成功
		return nil
	}

	// 检查任务是否已经存在于 MongoDB 中
	var existingJob Job
	err := s.jobsCollection.FindOne(context.Background(), bson.M{"_id": job.ID}).Decode(&existingJob)
	if err == nil {
		// 任务在数据库中已存在，直接返回成功
		return nil
	} else if err != mongo.ErrNoDocuments {
		// 查询出错（不是"未找到文档"的错误）
		return fmt.Errorf("failed to check if job exists: %w", err)
	}

	// 将任务保存到 MongoDB
	job.CreatedAt = helper.GetNowTime()
	job.UpdatedAt = helper.GetNowTime()

	_, err = s.jobsCollection.InsertOne(context.Background(), job)
	if err != nil {
		return err
	}

	var schedJob *gocron.Job
	var err2 error

	// 根据周期类型添加任务
	if job.CycleType == "interval" {
		// 使用 interval 方式添加任务
		schedJob, err2 = s.scheduler.Every(job.Interval.Days).Days().
			At(fmt.Sprintf("%02d:%02d", job.Interval.Hours, job.Interval.Minutes)).
			Do(s.executeJob, job)
	} else {
		// 使用 cron 方式添加任务
		schedJob, err2 = s.scheduler.Cron(job.Schedule).Do(s.executeJob, job)
	}

	if err2 != nil {
		return err2
	}

	s.setJob(job.ID, schedJob)

	// 添加日志记录
	s.logger.Info("Successfully added new job",
		zap.String("job_id", job.ID),
		zap.String("job_name", job.Name),
		zap.String("cycle_type", job.CycleType),
		zap.Time("next_run_time", schedJob.NextRun()),
	)

	return nil
}

// RemoveJob 移除一个任务
func (s *Scheduler) RemoveJob(jobID string) error {
	// 从调度器中移除任务
	if job, exists := s.takeJob(jobID); exists {
		s.scheduler.RemoveByReference(job)
	}

	// 从 MongoDB 中删除任务
	_, err := s.jobsCollection.DeleteOne(context.Background(), bson.M{"_id": jobID})
	if err != nil {
		return err
	}
	s.logger.Info("Successfully remove job")
	return nil
}

// UpdateJob 更新任务
func (s *Scheduler) UpdateJob(job *Job) error {
	// 检查处理函数是否已注册
	if _, exists := s.handlers[job.Handler]; !exists {
		return fmt.Errorf("handler %s not registered", job.Handler)
	}

	job.UpdatedAt = helper.GetNowTime()

	// 更新 MongoDB 中的任务
	_, err := s.jobsCollection.ReplaceOne(context.Background(), bson.M{"_id": job.ID}, job)
	if err != nil {
		return err
	}

	// 从调度器中移除旧任务
	if oldJob, exists := s.getJob(job.ID); exists {
		s.scheduler.RemoveByReference(oldJob)
	}

	// 添加新任务到调度器
	schedJob, err := s.scheduler.Cron(job.Schedule).Do(s.executeJob, job)
	if err != nil {
		return err
	}

	s.setJob(job.ID, schedJob)
	return nil
}

// loadJobs 从 MongoDB 加载所有任务
func (s *Scheduler) loadJobs() {
	cursor, err := s.jobsCollection.Find(context.Background(), bson.M{})
	if err != nil {
		s.logger.Error("Failed to load jobs from MongoDB", zap.Error(err))
		return
	}
	defer cursor.Close(context.Background())

	for cursor.Next(context.Background()) {
		var job Job
		if err := cursor.Decode(&job); err != nil {
			s.logger.Error("Failed to decode job", zap.Error(err))
			continue
		}

		// 检查处理函数是否已注册：后注册的 handler 会在 RegisterHandler 里补排，
		// 这里只提示，不当成错误（历史上每次启动都刷一条 ERROR）
		if _, exists := s.handlers[job.Handler]; !exists {
			s.logger.Warn("Job handler not registered yet, deferred until it registers",
				zap.String("job_id", job.ID),
				zap.String("handler", job.Handler),
			)
			continue
		}
		s.scheduleJob(job)
	}
}

// scheduleJob 把一条 job 排进内存（cron / interval 两种周期）。成功返回 true。
func (s *Scheduler) scheduleJob(job Job) bool {
	// executeJob 收的是 *Job；这里必须传指针，传值会在 gocron 反射调用时 panic
	jobCopy := job
	var schedJob *gocron.Job
	var err error
	if job.CycleType == "interval" {
		schedJob, err = s.scheduler.Every(job.Interval.Days).Days().
			At(fmt.Sprintf("%02d:%02d", job.Interval.Hours, job.Interval.Minutes)).
			Do(s.executeJob, &jobCopy)
	} else {
		schedJob, err = s.scheduler.Cron(job.Schedule).Do(s.executeJob, &jobCopy)
	}
	if err != nil {
		s.logger.Error("Failed to schedule job", zap.String("job_id", job.ID), zap.Error(err))
		return false
	}
	s.setJob(job.ID, schedJob)
	return true
}

// executeJob 执行任务
func (s *Scheduler) executeJob(job *Job) {
	s.logger.Info("Executing job",
		zap.String("job_id", job.ID),
		zap.String("job_name", job.Name),
		zap.String("handler", job.Handler),
	)

	// 获取并执行处理函数
	handler, exists := s.handlers[job.Handler]
	if !exists {
		s.logger.Error("Handler not found",
			zap.String("job_id", job.ID),
			zap.String("handler", job.Handler),
		)
		return
	}

	// 更新最后执行时间
	job.LastRunTime = helper.GetNowTime()
	if j, ok := s.getJob(job.ID); ok {
		job.NextRunTime = j.NextRun()
	}

	// 执行处理函数
	if err := handler(job.Params, helper.FormatTime(job.NextRunTime)); err != nil {
		s.logger.Error("Job execution failed",
			zap.String("job_id", job.ID),
			zap.String("job_name", job.Name),
			zap.Error(err),
		)
	}

	_, err := s.jobsCollection.UpdateOne(
		context.Background(),
		bson.M{"_id": job.ID},
		bson.M{
			"$set": bson.M{
				"last_run_time": job.LastRunTime,
				"next_run_time": job.NextRunTime,
			},
		},
	)
	if err != nil {
		s.logger.Error("Failed to update job execution time", zap.Error(err))
	}
}
