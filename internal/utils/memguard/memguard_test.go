package memguard

import (
	"strings"
	"testing"
)

func TestCheckRejectsLowMemory(t *testing.T) {
	orig := AvailablePercent
	defer func() { AvailablePercent = orig }()
	for _, p := range []float64{0, 5, 9.9, 14.99} {
		AvailablePercent = func() float64 { return p }
		err := Check()
		if err == nil {
			t.Fatalf("avail=%.2f%%: expected rejection", p)
		}
		if !strings.Contains(err.Error(), "可用内存") {
			t.Fatalf("avail=%.2f%%: unexpected error %v", p, err)
		}
	}
}

func TestCheckAllowsHealthy(t *testing.T) {
	orig := AvailablePercent
	defer func() { AvailablePercent = orig }()
	for _, p := range []float64{15.0, 30, 67.8, -1} { // -1 = 未知平台，不拦
		AvailablePercent = func() float64 { return p }
		if err := Check(); err != nil {
			t.Fatalf("avail=%.2f%%: unexpected error %v", p, err)
		}
	}
}
