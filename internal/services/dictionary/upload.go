// services-------------------------------------
// @file      : upload.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2026/11/18 03:10
// -------------------------------------------

package dictionary

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	dictrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/dictionary"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	// uploadPersistInterval 进度落库节流：最多每 800ms 刷一次
	uploadPersistInterval = 800 * time.Millisecond
	// uploadReaderBuffer 解析临时文件的读缓冲
	uploadReaderBuffer = 256 * 1024
	// uploadTaskTTL 内存任务保留时长（终态任务过期后只留 Mongo 记录）
	uploadTaskTTL = 24 * time.Hour
	// uploadWorkerTimeout 单个任务的处理上限，防止临时文件/协程泄漏
	uploadWorkerTimeout = 60 * time.Minute
	// uploadRecoverTimeout 重启恢复扫描超时
	uploadRecoverTimeout = 30 * time.Second
)

// processStartedAt 进程启动时刻：重启恢复只处理这之前创建的任务，
// 避免把本进程刚提交、正在跑的任务误标成 failed。
var processStartedAt = time.Now()

// UploadRequest 大文件上传入参（handler 把 multipart 流直接递进来，不在内存里攒）
type UploadRequest struct {
	Kind string
	Name string
	Mode string
	// Reader 文件流
	Reader io.Reader
	// TotalBytes 客户端声明的长度，>0 时用于提前拒绝超大文件
	TotalBytes int64
}

// UploadService 大文件上传：流式落临时文件 → 后台解析入库 → 进度可查
type UploadService interface {
	Start(ctx context.Context, req UploadRequest) (*models.DictionaryUploadTask, error)
	Status(ctx context.Context, taskID string) (*models.DictionaryUploadTask, error)
}

type uploadService struct {
	repo   dictrepo.UploadTaskRepository
	manage *manageService
	store  *taskStore
}

var (
	sharedUploadOnce sync.Once
	sharedUpload     *uploadService
)

// NewUploadService 进程级单例（任务表要跨 handler 共享）
func NewUploadService() UploadService {
	sharedUploadOnce.Do(func() {
		sharedUpload = &uploadService{
			repo:   dictrepo.NewUploadTaskRepository(),
			manage: sharedManageService(),
			store:  newTaskStore(),
		}
		sharedUpload.recoverStaleTasks()
	})
	return sharedUpload
}

// Start 接收文件流：落临时文件 → 建任务 → 后台解析。
func (s *uploadService) Start(ctx context.Context, req UploadRequest) (*models.DictionaryUploadTask, error) {
	canonical, err := NormalizeKind(req.Kind)
	if err != nil {
		return nil, err
	}
	safeName, err := SanitizeDictionaryName(req.Name)
	if err != nil {
		return nil, err
	}
	mode, err := NormalizeUploadMode(req.Mode)
	if err != nil {
		return nil, err
	}
	if req.TotalBytes > MaxUploadBytes {
		return nil, fmt.Errorf("%w: %d > %d", ErrUploadTooLarge, req.TotalBytes, MaxUploadBytes)
	}
	if req.Reader == nil {
		return nil, fmt.Errorf("%w: empty upload body", ErrEmptyLines)
	}

	// 快速失败：create 重名 / append 目标不存在，不必先传完 200MB
	existing, err := s.manage.repo.FindMetaByNameCategory(ctx, safeName, canonical)
	if err != nil {
		return nil, err
	}
	if mode == models.DictUploadModeCreate && existing != nil {
		return nil, fmt.Errorf("%w: %s/%s", ErrDictionaryExists, canonical, safeName)
	}
	if mode == models.DictUploadModeAppend && existing == nil {
		return nil, fmt.Errorf("%w: %s/%s", ErrDictionaryNotFound, canonical, safeName)
	}

	s.pruneStore()

	tmp, err := os.CreateTemp("", "scopesentry-dict-*.upload")
	if err != nil {
		return nil, fmt.Errorf("create dictionary temp file: %w", err)
	}
	tempPath := tmp.Name()
	cleanup := func() {
		tmp.Close()
		_ = os.Remove(tempPath)
	}

	counter := &lineCounter{}
	// 多读 1 字节用于判定是否超限
	written, err := io.Copy(io.MultiWriter(tmp, counter), io.LimitReader(req.Reader, MaxUploadBytes+1))
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("stream dictionary upload: %w", err)
	}
	if written > MaxUploadBytes {
		cleanup()
		return nil, fmt.Errorf("%w: > %d bytes", ErrUploadTooLarge, MaxUploadBytes)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return nil, fmt.Errorf("close dictionary temp file: %w", err)
	}

	now := time.Now()
	task := &models.DictionaryUploadTask{
		TaskID:     primitive.NewObjectID().Hex(),
		Kind:       canonical,
		Name:       safeName,
		Mode:       mode,
		Status:     models.DictUploadStatusPending,
		TotalLines: counter.total(),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	s.publish(task)

	// 先取快照再起协程：之后 task 只归后台协程所有，避免读写同一结构体
	snapshot := cloneTask(task)
	go s.run(task, tempPath)
	return snapshot, nil
}

// Status 查任务：先内存（运行时快照），再 Mongo（重启后的终态记录）。
func (s *uploadService) Status(ctx context.Context, taskID string) (*models.DictionaryUploadTask, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("%w: taskId is required", ErrUploadNotFound)
	}
	if task := s.store.get(taskID); task != nil {
		return task, nil
	}
	task, err := s.repo.Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, fmt.Errorf("%w: %s", ErrUploadNotFound, taskID)
	}
	task.Progress = computeProgress(task)
	return task, nil
}

// run 后台解析：逐行入库（唯一索引去重），最后重建内容文件并落统计。
func (s *uploadService) run(task *models.DictionaryUploadTask, tempPath string) {
	defer os.Remove(tempPath)

	ctx, cancel := context.WithTimeout(context.Background(), uploadWorkerTimeout)
	defer cancel()

	task.Status = models.DictUploadStatusRunning
	task.UpdatedAt = time.Now()
	s.publish(task)

	meta, created, err := s.prepareMeta(ctx, task)
	if err != nil {
		s.failTask(ctx, task, created, meta, err)
		return
	}

	file, err := os.Open(tempPath)
	if err != nil {
		s.failTask(ctx, task, created, meta, fmt.Errorf("open dictionary temp file: %w", err))
		return
	}
	defer file.Close()

	reader := bufio.NewReaderSize(file, uploadReaderBuffer)
	batch := make([]string, 0, entryBatchSize)
	lastPersist := time.Now()

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		unlock := s.manage.locks.Lock(meta.ID.Hex())
		defer unlock()
		if err := s.manage.ensureEntriesLocked(ctx, meta); err != nil {
			return err
		}
		added, duplicated, err := s.manage.insertValues(ctx, meta, batch)
		if err != nil {
			return err
		}
		task.Added += added
		task.Duplicated += duplicated
		batch = batch[:0]
		return nil
	}

	for {
		line, readErr := reader.ReadString('\n')
		if line != "" {
			task.ProcessedLines++
			if value, ok := NormalizeLine(line); ok && len(value) <= MaxLineLength {
				batch = append(batch, value)
				if len(batch) >= entryBatchSize {
					if err := flush(); err != nil {
						s.failTask(ctx, task, created, meta, err)
						return
					}
				}
			}
			if time.Since(lastPersist) >= uploadPersistInterval {
				task.UpdatedAt = time.Now()
				s.publish(task)
				lastPersist = time.Now()
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			s.failTask(ctx, task, created, meta, fmt.Errorf("read dictionary temp file: %w", readErr))
			return
		}
	}
	if err := flush(); err != nil {
		s.failTask(ctx, task, created, meta, err)
		return
	}

	// 收尾：行级集合 → 内容文件 → 统计（与追加/删行共用同一把写锁）
	unlock := s.manage.locks.Lock(meta.ID.Hex())
	count, size, err := s.manage.rebuildBlobLocked(ctx, meta)
	if err == nil {
		err = s.manage.persistStatsLocked(ctx, meta, count, size)
	}
	unlock()
	if err != nil {
		s.failTask(ctx, task, created, meta, err)
		return
	}

	finished := time.Now()
	task.Status = models.DictUploadStatusDone
	task.ProcessedLines = task.TotalLines
	task.FinishedAt = &finished
	task.UpdatedAt = finished
	s.publish(task)
}

// prepareMeta create 模式建元数据（重名报错），append 模式复用已有元数据。
func (s *uploadService) prepareMeta(ctx context.Context, task *models.DictionaryUploadTask) (*models.DictionaryMeta, bool, error) {
	// 名字锁：保证同名的两个上传不会各建一条元数据
	unlockName := s.manage.locks.Lock("name:" + task.Kind + "/" + task.Name)
	defer unlockName()

	existing, err := s.manage.repo.FindMetaByNameCategory(ctx, task.Name, task.Kind)
	if err != nil {
		return nil, false, err
	}
	if task.Mode == models.DictUploadModeAppend {
		if existing == nil {
			return nil, false, fmt.Errorf("%w: %s/%s", ErrDictionaryNotFound, task.Kind, task.Name)
		}
		return existing, false, nil
	}
	if existing != nil {
		return nil, false, fmt.Errorf("%w: %s/%s", ErrDictionaryExists, task.Kind, task.Name)
	}
	id, err := s.manage.repo.InsertMeta(ctx, task.Name, task.Kind, "0.00")
	if err != nil {
		return nil, false, err
	}
	meta, err := metaFromID(id, task.Name, task.Kind)
	if err != nil {
		return nil, false, err
	}
	return meta, true, nil
}

// failTask 失败收尾：记错误状态；如果是本任务新建的字典，把半成品一起清掉。
func (s *uploadService) failTask(ctx context.Context, task *models.DictionaryUploadTask, created bool, meta *models.DictionaryMeta, cause error) {
	logger.Error(fmt.Sprintf("dictionary upload %s failed: %v", task.TaskID, cause))

	if created && meta != nil {
		unlock := s.manage.locks.Lock(meta.ID.Hex())
		_ = s.manage.entryRepo.DeleteAll(ctx, meta.ID.Hex())
		_ = s.manage.repo.DeleteFileSafe(ctx, meta.ID.Hex())
		_ = s.manage.repo.DeleteMetaByNameCategory(ctx, meta.Name, meta.Category)
		unlock()
	}

	finished := time.Now()
	task.Status = models.DictUploadStatusFailed
	task.Error = cause.Error()
	task.FinishedAt = &finished
	task.UpdatedAt = finished
	s.publish(task)
}

// publish 把当前状态发布到内存表并持久化（内存是运行态，Mongo 是重启后的终态）。
func (s *uploadService) publish(task *models.DictionaryUploadTask) {
	task.Progress = computeProgress(task)
	task.UpdatedAt = time.Now()
	snapshot := cloneTask(task)
	s.store.put(snapshot)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.repo.Upsert(ctx, snapshot); err != nil {
		logger.Error(fmt.Sprintf("persist dictionary upload task %s: %v", snapshot.TaskID, err))
	}
}

// pruneStore 清理过期的内存任务（终态 + 超过 TTL）。
func (s *uploadService) pruneStore() {
	s.store.prune(uploadTaskTTL)
}

// recoverStaleTasks 进程重启后把残留的 pending/running 标记为 failed。
func (s *uploadService) recoverStaleTasks() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), uploadRecoverTimeout)
		defer cancel()
		n, err := s.repo.MarkStaleFailed(ctx, "process restarted, upload task aborted", processStartedAt)
		if err != nil {
			logger.Error(fmt.Sprintf("recover dictionary upload tasks: %v", err))
			return
		}
		if n > 0 {
			logger.Warn(fmt.Sprintf("marked %d interrupted dictionary upload task(s) as failed", n))
		}
	}()
}

// computeProgress 进度百分比：已处理行 / 总行数。
func computeProgress(task *models.DictionaryUploadTask) float64 {
	switch task.Status {
	case models.DictUploadStatusDone:
		return 100
	case models.DictUploadStatusFailed:
		if task.TotalLines > 0 {
			return float64(task.ProcessedLines) * 100 / float64(task.TotalLines)
		}
		return 0
	}
	if task.TotalLines <= 0 {
		return 0
	}
	p := float64(task.ProcessedLines) * 100 / float64(task.TotalLines)
	if p > 100 {
		p = 100
	}
	return p
}

// cloneTask 返回一份拷贝，避免 http 读与后台写同时碰同一结构体。
func cloneTask(task *models.DictionaryUploadTask) *models.DictionaryUploadTask {
	if task == nil {
		return nil
	}
	c := *task
	if task.FinishedAt != nil {
		finished := *task.FinishedAt
		c.FinishedAt = &finished
	}
	return &c
}

// taskStore 内存任务表
type taskStore struct {
	mu    sync.RWMutex
	tasks map[string]*models.DictionaryUploadTask
}

func newTaskStore() *taskStore {
	return &taskStore{tasks: make(map[string]*models.DictionaryUploadTask)}
}

func (ts *taskStore) put(task *models.DictionaryUploadTask) {
	ts.mu.Lock()
	ts.tasks[task.TaskID] = task
	ts.mu.Unlock()
}

func (ts *taskStore) get(taskID string) *models.DictionaryUploadTask {
	ts.mu.RLock()
	task, ok := ts.tasks[taskID]
	ts.mu.RUnlock()
	if !ok {
		return nil
	}
	return cloneTask(task)
}

func (ts *taskStore) prune(ttl time.Duration) {
	deadline := time.Now().Add(-ttl)
	ts.mu.Lock()
	defer ts.mu.Unlock()
	for id, task := range ts.tasks {
		if task.Status != models.DictUploadStatusDone && task.Status != models.DictUploadStatusFailed {
			continue
		}
		if task.UpdatedAt.After(deadline) {
			continue
		}
		delete(ts.tasks, id)
	}
}

// lineCounter 边写边数行：total = '\n' 个数（+1 当末行没有换行符）。
type lineCounter struct {
	lines int64
	size  int64
	last  byte
}

func (c *lineCounter) Write(p []byte) (int, error) {
	c.size += int64(len(p))
	c.lines += int64(bytes.Count(p, []byte{'\n'}))
	if len(p) > 0 {
		c.last = p[len(p)-1]
	}
	return len(p), nil
}

func (c *lineCounter) total() int64 {
	if c.size == 0 {
		return 0
	}
	if c.last == '\n' {
		return c.lines
	}
	return c.lines + 1
}
