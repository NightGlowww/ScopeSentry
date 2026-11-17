package mcpaudit

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

type fakeWriter struct {
	mu      sync.Mutex
	logs    []models.McpCallLog
	batches int
	block   chan struct{}
}

func (f *fakeWriter) InsertLogs(_ context.Context, logs []models.McpCallLog) error {
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logs = append(f.logs, logs...)
	f.batches++
	return nil
}

func (f *fakeWriter) TrimLogs(_ context.Context, _ int64) (int64, error) { return 0, nil }

func (f *fakeWriter) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.logs)
}

func TestRecorderFlushWritesBatch(t *testing.T) {
	writer := &fakeWriter{}
	r := newRecorder(writer)
	t.Cleanup(r.stop)

	for i := 0; i < 5; i++ {
		r.record(models.McpCallLog{
			KeyID:  "k1",
			Tool:   "list_assets",
			Status: models.McpLogStatusSuccess,
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if got := writer.count(); got != 5 {
		t.Fatalf("written = %d, want 5", got)
	}
	written, dropped, failures := r.stats()
	if written != 5 || dropped != 0 || failures != 0 {
		t.Fatalf("stats = written:%d dropped:%d failures:%d", written, dropped, failures)
	}
}

// Record 必须永不阻塞：写库被卡住时仍然立刻返回，队列满了直接丢弃并计数
func TestRecorderNeverBlocksAndDrops(t *testing.T) {
	block := make(chan struct{})
	writer := &fakeWriter{block: block}
	r := newRecorder(writer)
	t.Cleanup(func() {
		close(block)
		r.stop()
	})

	done := make(chan struct{})
	go func() {
		for i := 0; i < auditQueueSize+auditBatchSize*4; i++ {
			r.record(models.McpCallLog{Tool: "list_assets", Status: models.McpLogStatusSuccess})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("record blocked; it must never wait on the writer")
	}

	if _, dropped, _ := r.stats(); dropped == 0 {
		t.Fatal("expected drops once the queue is full and the writer is stuck")
	}
}

// 写入失败只计数，不影响调用方
func TestRecorderFlushFailureIsCounted(t *testing.T) {
	writer := &failingWriter{}
	r := newRecorder(writer)
	t.Cleanup(r.stop)

	r.record(models.McpCallLog{Tool: "x", Status: models.McpLogStatusDenied})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.flush(ctx); err != nil {
		t.Fatalf("flush should not surface writer errors: %v", err)
	}
	_, _, failures := r.stats()
	if failures != 1 {
		t.Fatalf("failures = %d, want 1", failures)
	}
}

type failingWriter struct{}

func (f *failingWriter) InsertLogs(context.Context, []models.McpCallLog) error {
	return context.DeadlineExceeded
}
func (f *failingWriter) TrimLogs(context.Context, int64) (int64, error) { return 0, nil }
