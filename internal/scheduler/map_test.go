//go:build integration

package scheduler

import (
	"fmt"
	"sync"
	"testing"

	"github.com/go-co-op/gocron"
	"go.uber.org/zap"
)

// 并发 Add/Remove 计划任务时，jobs 映射必须被 mu 保护：
// 没有锁时 Go 会直接 fatal error: concurrent map writes（不是可 recover 的 panic）。
// 本用例在 -race 下会放大任何未加锁访问。
func TestJobMapConcurrentAccess(t *testing.T) {
	s := &Scheduler{
		jobs:     make(map[string]*gocron.Job),
		handlers: make(map[string]JobHandler),
		logger:   zap.NewNop(),
	}
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := fmt.Sprintf("job-%d", n)
			s.setJob(id, nil)
			if !s.hasJob(id) {
				t.Errorf("hasJob(%s) = false after setJob", id)
			}
			if _, ok := s.getJob(id); !ok {
				t.Errorf("getJob(%s) missing", id)
			}
			if _, ok := s.takeJob(id); !ok {
				t.Errorf("takeJob(%s) missing", id)
			}
			if s.hasJob(id) {
				t.Errorf("hasJob(%s) = true after takeJob", id)
			}
		}(i)
	}
	wg.Wait()
}
