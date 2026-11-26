package jev

import (
	"strings"
	"testing"
)

func TestTargetFromState(t *testing.T) {
	state := "Template: x\nSeverity: high\nURL: https://a.example/\nMatched: https://a.example/debug/pprof/goroutine?debug=1\nRecorded request:\nGET /\n"
	if got := targetFromState(state); got != "https://a.example/debug/pprof/goroutine?debug=1" {
		t.Fatalf("matched should win, got %q", got)
	}
	if got := targetFromState("URL: https://b.example/x\n"); got != "https://b.example/x" {
		t.Fatalf("url fallback, got %q", got)
	}
	if got := targetFromState("Template: nothing\n"); got != "" {
		t.Fatalf("no target expected, got %q", got)
	}
}

func TestControlURL(t *testing.T) {
	src := "https://a.example/debug/pprof/goroutine?debug=1"
	ctl := controlURL(src)
	if ctl == "" || ctl == src {
		t.Fatalf("control must differ, got %q", ctl)
	}
	if strings.Contains(ctl, "?") {
		t.Fatalf("control must drop the query, got %q", ctl)
	}
	if controlURL(src) == ctl {
		t.Fatal("control path must be random per call")
	}
}

func TestSimilarLen(t *testing.T) {
	if !similarLen(1000, 1100) {
		t.Fatal("10 percent apart should be similar")
	}
	if similarLen(1000, 2000) {
		t.Fatal("2x apart must not be similar")
	}
	if similarLen(0, 0) {
		t.Fatal("zero must not be similar")
	}
}

func TestSkipHost(t *testing.T) {
	for _, h := range []string{"127.0.0.1", "10.1.2.3:8080", "192.168.1.1", "169.254.169.254", "0.0.0.0", ""} {
		if !skipHost(h) {
			t.Fatalf("%q should be skipped", h)
		}
	}
	for _, h := range []string{"8.8.8.8", "1.1.1.1:443"} {
		if skipHost(h) {
			t.Fatalf("%q should be allowed", h)
		}
	}
}
