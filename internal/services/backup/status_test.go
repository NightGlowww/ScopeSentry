package backup

import (
	"errors"
	"testing"
)

func TestJobTrackerSerializesTasks(t *testing.T) {
	tracker := newJobTracker()

	if got := tracker.snapshot(); got.Running || got.Current != "idle" || got.Progress != 0 || got.LastError != "" {
		t.Fatalf("unexpected initial status: %+v", got)
	}
	if !tracker.tryStart("create") {
		t.Fatal("first task must acquire the slot")
	}
	if tracker.tryStart("restore") {
		t.Fatal("second task must be rejected while one is running")
	}
	tracker.report("create", 55)
	if got := tracker.snapshot(); !got.Running || got.Current != "create" || got.Progress != 55 {
		t.Fatalf("unexpected running status: %+v", got)
	}
	tracker.report("create", 300)
	if got := tracker.snapshot(); got.Progress != 100 {
		t.Fatalf("progress must be clamped to 100, got %d", got.Progress)
	}
	tracker.finish(nil)
	if got := tracker.snapshot(); got.Running || got.Current != "idle" || got.LastError != "" {
		t.Fatalf("unexpected status after success: %+v", got)
	}

	if !tracker.tryStart("restore") {
		t.Fatal("slot must be reusable after finish")
	}
	tracker.finish(errors.New("boom"))
	got := tracker.snapshot()
	if got.Running {
		t.Fatal("tracker still running after failure")
	}
	if got.LastError != "boom" {
		t.Fatalf("expected lastError=boom, got %+v", got)
	}
	if !tracker.tryStart("upload") {
		t.Fatal("slot must be reusable after failure")
	}
	tracker.finish(nil)
}
