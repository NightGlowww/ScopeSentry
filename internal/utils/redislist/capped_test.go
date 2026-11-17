package redislist

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type fakeClient struct {
	pushed     []string
	pushedHead bool

	pushErr error
	trimErr error

	trimCalls int
	trimKey   string
	trimStart int64
	trimStop  int64

	expireCalls int
	expireKey   string
	expireTTL   time.Duration
	expireErr   error
}

func (f *fakeClient) RPush(_ context.Context, _ string, values ...interface{}) *redis.IntCmd {
	for _, v := range values {
		f.pushed = append(f.pushed, fmt.Sprint(v))
	}
	return redis.NewIntResult(int64(len(f.pushed)), f.pushErr)
}

func (f *fakeClient) LPush(_ context.Context, _ string, values ...interface{}) *redis.IntCmd {
	f.pushedHead = true
	for _, v := range values {
		f.pushed = append(f.pushed, fmt.Sprint(v))
	}
	return redis.NewIntResult(int64(len(f.pushed)), f.pushErr)
}

func (f *fakeClient) Expire(_ context.Context, key string, ttl time.Duration) *redis.BoolCmd {
	f.expireCalls++
	f.expireKey = key
	f.expireTTL = ttl
	return redis.NewBoolResult(true, f.expireErr)
}

func (f *fakeClient) LTrim(_ context.Context, key string, start, stop int64) *redis.StatusCmd {
	f.trimCalls++
	f.trimKey = key
	f.trimStart = start
	f.trimStop = stop
	return redis.NewStatusResult("OK", f.trimErr)
}

func TestMaxLenFromConfig(t *testing.T) {
	cases := []struct {
		configured int
		want       int64
	}{
		{configured: 0, want: DefaultMaxLen},
		{configured: -5, want: DefaultMaxLen},
		{configured: 1000, want: 1000},
		{configured: 50, want: 50},
	}
	for _, c := range cases {
		if got := MaxLenFromConfig(c.configured); got != c.want {
			t.Errorf("MaxLenFromConfig(%d) = %d, want %d", c.configured, got, c.want)
		}
	}
}

func TestPushCappedKeepsNewestEntries(t *testing.T) {
	fake := &fakeClient{}
	err := PushCapped(context.Background(), fake, "log:node-1", 3, "line-a", "line-b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fake.pushed) != 2 {
		t.Fatalf("pushed %d values, want 2", len(fake.pushed))
	}
	if fake.trimCalls != 1 {
		t.Fatalf("trim called %d times, want 1", fake.trimCalls)
	}
	if fake.trimKey != "log:node-1" {
		t.Errorf("trim key = %q, want log:node-1", fake.trimKey)
	}
	// 尾部最新：保留最后 maxLen 条
	if fake.trimStart != -3 || fake.trimStop != -1 {
		t.Errorf("trim range = [%d, %d], want [-3, -1]", fake.trimStart, fake.trimStop)
	}
}

func TestPushCappedFallsBackToDefaultMaxLen(t *testing.T) {
	fake := &fakeClient{}
	if err := PushCapped(context.Background(), fake, "k", 0, "v"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.trimStart != -DefaultMaxLen || fake.trimStop != -1 {
		t.Errorf("trim range = [%d, %d], want [-%d, -1]", fake.trimStart, fake.trimStop, DefaultMaxLen)
	}
}

func TestPushCappedSkipsEmptyValues(t *testing.T) {
	fake := &fakeClient{}
	if err := PushCapped(context.Background(), fake, "k", 10); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fake.pushed) != 0 || fake.trimCalls != 0 {
		t.Fatalf("expected no redis calls, got push=%d trim=%d", len(fake.pushed), fake.trimCalls)
	}
}

func TestPushCappedReportsPushErrorWithoutTrim(t *testing.T) {
	fake := &fakeClient{pushErr: errors.New("boom")}
	err := PushCapped(context.Background(), fake, "k", 10, "v")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if fake.trimCalls != 0 {
		t.Errorf("trim should not run after a failed push, ran %d times", fake.trimCalls)
	}
}

func TestPushCappedReportsTrimError(t *testing.T) {
	fake := &fakeClient{trimErr: errors.New("trim boom")}
	err := PushCapped(context.Background(), fake, "k", 10, "v")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() == "" || fake.trimCalls != 1 {
		t.Fatalf("expected trim error to be reported, calls=%d err=%v", fake.trimCalls, err)
	}
}

func TestPushHeadCappedKeepsNewestEntriesAtHead(t *testing.T) {
	fake := &fakeClient{}
	if err := PushHeadCapped(context.Background(), fake, "TaskInfo:abc", 50, "a"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.trimCalls != 1 {
		t.Fatalf("trim called %d times, want 1", fake.trimCalls)
	}
	// 头部最新：保留最前 maxLen 条
	if fake.trimStart != 0 || fake.trimStop != 49 {
		t.Errorf("trim range = [%d, %d], want [0, 49]", fake.trimStart, fake.trimStop)
	}
}

func TestPushHeadCappedFallsBackToDefaultMaxLen(t *testing.T) {
	fake := &fakeClient{}
	if err := PushHeadCapped(context.Background(), fake, "k", -1, "v"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.trimStart != 0 || fake.trimStop != DefaultMaxLen-1 {
		t.Errorf("trim range = [%d, %d], want [0, %d]", fake.trimStart, fake.trimStop, DefaultMaxLen-1)
	}
}
