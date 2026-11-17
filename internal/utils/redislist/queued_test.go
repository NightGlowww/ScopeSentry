package redislist

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPushQueuedSetsSlidingTTL(t *testing.T) {
	fake := &fakeClient{}
	ttl := 7 * 24 * time.Hour

	if err := PushQueued(context.Background(), fake, "NodeTask:node-1", ttl, "payload"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fake.pushed) != 1 || fake.pushed[0] != "payload" {
		t.Fatalf("pushed = %v, want [payload]", fake.pushed)
	}
	if fake.expireCalls != 1 {
		t.Fatalf("expire called %d times, want 1", fake.expireCalls)
	}
	if fake.expireKey != "NodeTask:node-1" {
		t.Errorf("expire key = %q, want NodeTask:node-1", fake.expireKey)
	}
	if fake.expireTTL != ttl {
		t.Errorf("expire ttl = %v, want %v", fake.expireTTL, ttl)
	}
}

func TestPushQueuedHeadUsesLPush(t *testing.T) {
	fake := &fakeClient{}
	if err := PushQueuedHead(context.Background(), fake, "TaskInfo:abc", time.Hour, "a", "b"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fake.pushedHead {
		t.Error("PushQueuedHead should use LPush to keep the historical order")
	}
	if fake.expireCalls != 1 {
		t.Errorf("expire called %d times, want 1", fake.expireCalls)
	}
}

func TestPushQueuedSkipsTTLWhenZero(t *testing.T) {
	fake := &fakeClient{}
	if err := PushQueued(context.Background(), fake, "k", 0, "v"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.expireCalls != 0 {
		t.Fatalf("expire should be skipped when ttl<=0, ran %d times", fake.expireCalls)
	}
}

func TestPushQueuedSkipsEmptyValues(t *testing.T) {
	fake := &fakeClient{}
	if err := PushQueued(context.Background(), fake, "k", time.Hour); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fake.pushed) != 0 || fake.expireCalls != 0 {
		t.Fatalf("expected no redis calls, got push=%d expire=%d", len(fake.pushed), fake.expireCalls)
	}
}

func TestPushQueuedReportsExpireError(t *testing.T) {
	fake := &fakeClient{expireErr: errors.New("expire boom")}
	if err := PushQueued(context.Background(), fake, "k", time.Hour, "v"); err == nil {
		t.Fatal("expected error, got nil")
	}
}
