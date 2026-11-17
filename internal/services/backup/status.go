// services/backup-------------------------------------
// @file      : status.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2026/11/18 01:20
// -------------------------------------------

package backup

import (
	"sync"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// jobTracker 保证同一时间只有一个备份/恢复/上传任务，并对外暴露进度。
type jobTracker struct {
	mu        sync.Mutex
	running   bool
	current   string
	progress  int
	lastError string
}

func newJobTracker() *jobTracker {
	return &jobTracker{current: "idle"}
}

// tryStart 原子地抢占任务槽位。
func (t *jobTracker) tryStart(current string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.running {
		return false
	}
	t.running = true
	t.current = current
	t.progress = 0
	t.lastError = ""
	return true
}

// report 更新阶段与进度；任务结束后调用会被忽略。
func (t *jobTracker) report(current string, progress int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.running {
		return
	}
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	if current != "" {
		t.current = current
	}
	t.progress = progress
}

// finish 释放槽位并记录错误。
func (t *jobTracker) finish(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.running = false
	t.current = "idle"
	if err != nil {
		t.lastError = err.Error()
	}
}

func (t *jobTracker) snapshot() models.BackupStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	return models.BackupStatus{
		Running:   t.running,
		Current:   t.current,
		Progress:  t.progress,
		LastError: t.lastError,
	}
}
