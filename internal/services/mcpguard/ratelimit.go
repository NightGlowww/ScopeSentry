package mcpguard

import (
	"sync"
	"time"
)

// rateLimiter 每 key 的固定窗口计数器（进程内）。
//
// 选择进程内计数而不是 Redis：
//   - MCP 调用是低频治理面，多实例下按实例计数足够（超限是保护，不是精确计费）；
//   - 不引入额外网络往返，避免主请求被限速判定拖慢。
//
// 单实例内是精确的；多实例部署时实际上限 ≈ 实例数 × limit（已在任务报告中标注）。
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rateBucket
	now     func() time.Time
}

type rateBucket struct {
	windowStart time.Time
	count       int
}

// rateWindow 固定窗口长度
const rateWindow = time.Minute

// maxTrackedBuckets 触发陈旧桶清理的水位，避免 key 数量失控时内存无界增长
const maxTrackedBuckets = 4096

func newRateLimiter() *rateLimiter {
	return &rateLimiter{
		buckets: make(map[string]*rateBucket),
		now:     time.Now,
	}
}

// allow 记一次并判断是否放行；limit <= 0 表示不限速
func (l *rateLimiter) allow(key string, limit int) bool {
	if limit <= 0 {
		return true
	}
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	if len(l.buckets) > maxTrackedBuckets {
		l.sweepLocked(now)
	}

	b, ok := l.buckets[key]
	if !ok || now.Sub(b.windowStart) >= rateWindow {
		l.buckets[key] = &rateBucket{windowStart: now, count: 1}
		return true
	}
	if b.count >= limit {
		return false
	}
	b.count++
	return true
}

// current 返回当前窗口内已计数（测试/观测用）
func (l *rateLimiter) current(key string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if b, ok := l.buckets[key]; ok {
		return b.count
	}
	return 0
}

// sweepLocked 清掉两个窗口以前的桶（调用方持锁）
func (l *rateLimiter) sweepLocked(now time.Time) {
	for k, b := range l.buckets {
		if now.Sub(b.windowStart) >= 2*rateWindow {
			delete(l.buckets, k)
		}
	}
}

// setClock 注入时钟（单测用）
func (l *rateLimiter) setClock(fn func() time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.now = fn
}
