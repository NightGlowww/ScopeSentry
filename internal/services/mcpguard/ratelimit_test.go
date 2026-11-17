package mcpguard

import (
	"testing"
	"time"
)

func TestRateLimiterWindow(t *testing.T) {
	l := newRateLimiter()
	base := time.Unix(1700000000, 0)
	l.setClock(func() time.Time { return base })

	// 3 次放行，第 4 次拒绝
	for i := 0; i < 3; i++ {
		if !l.allow("key-1", 3) {
			t.Fatalf("call %d should be allowed", i+1)
		}
	}
	if l.allow("key-1", 3) {
		t.Fatal("4th call should be rate limited")
	}
	if got := l.current("key-1"); got != 3 {
		t.Fatalf("counter = %d, want 3", got)
	}

	// 不同 key 互不影响
	if !l.allow("key-2", 3) {
		t.Fatal("other key should not be affected")
	}

	// 跨窗口重置
	base = base.Add(rateWindow)
	l.setClock(func() time.Time { return base })
	if !l.allow("key-1", 3) {
		t.Fatal("counter should reset after the window")
	}
}

func TestRateLimiterUnlimited(t *testing.T) {
	l := newRateLimiter()
	for i := 0; i < 1000; i++ {
		if !l.allow("key", 0) {
			t.Fatal("limit<=0 must mean unlimited")
		}
	}
}
