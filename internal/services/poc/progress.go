package poc

import (
	"sync"
	"time"
)

// ImportProgress 进程内的 POC 导入进度。
//
// 之前导入是纯黑盒：点完「导入」只返回 "Importing in progress"，
// 14403 个文件要跑十几秒到几分钟，期间界面没有任何反馈（2026-11-20）。
type ImportProgress struct {
	mu         sync.RWMutex
	Running    bool      `json:"running"`
	Total      int       `json:"total"`
	Done       int       `json:"done"`
	Success    int       `json:"success"`
	Update     int       `json:"update"`
	Repeat     int       `json:"repeat"`
	Failed     int       `json:"failed"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
	LastError  string    `json:"lastError"`
}

// GlobalImportProgress 供 handler 读取。
var GlobalImportProgress = &ImportProgress{}

// Begin 开始一次导入。
func (p *ImportProgress) Begin(total int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Running = true
	p.Total = total
	p.Done = 0
	p.Success, p.Update, p.Repeat, p.Failed = 0, 0, 0, 0
	p.StartedAt = time.Now()
	p.FinishedAt = time.Time{}
	p.LastError = ""
}

// Step 每处理完一个文件调用一次。
func (p *ImportProgress) Step(kind string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Done++
	switch kind {
	case "success":
		p.Success++
	case "update":
		p.Update++
	case "repeat":
		p.Repeat++
	case "failed":
		p.Failed++
	}
}

// Finish 结束导入。
func (p *ImportProgress) Finish(lastErr string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Running = false
	p.FinishedAt = time.Now()
	p.LastError = lastErr
}

// ImportProgressSnapshot 快照 DTO：刻意不带 mutex。
// 直接把 ImportProgress 按值返回会复制锁（go vet: "copies lock value"），
// 复制出来的锁对 JSON 无害，但这是个会掩盖真实并发问题的坏习惯。
type ImportProgressSnapshot struct {
	Running    bool      `json:"running"`
	Total      int       `json:"total"`
	Done       int       `json:"done"`
	Success    int       `json:"success"`
	Update     int       `json:"update"`
	Repeat     int       `json:"repeat"`
	Failed     int       `json:"failed"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
	LastError  string    `json:"lastError"`
}

// Snapshot 返回一份可用于 JSON 的快照。
func (p *ImportProgress) Snapshot() ImportProgressSnapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return ImportProgressSnapshot{
		Running:    p.Running,
		Total:      p.Total,
		Done:       p.Done,
		Success:    p.Success,
		Update:     p.Update,
		Repeat:     p.Repeat,
		Failed:     p.Failed,
		StartedAt:  p.StartedAt,
		FinishedAt: p.FinishedAt,
		LastError:  p.LastError,
	}
}
