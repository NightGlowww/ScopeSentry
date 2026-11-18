package diskguard

import (
	"strings"
	"testing"
)

func TestCheckRejectsLowDisk(t *testing.T) {
	orig := FreePercent
	defer func() { FreePercent = orig }()
	for _, free := range []float64{0, 1.5, 2.99} {
		FreePercent = func(string) float64 { return free }
		err := Check("/")
		if err == nil {
			t.Fatalf("free=%.2f%%: expected rejection", free)
		}
		if !strings.Contains(err.Error(), "磁盘可用空间") {
			t.Fatalf("free=%.2f%%: unexpected error %v", free, err)
		}
	}
}

func TestCheckAllowsHealthy(t *testing.T) {
	orig := FreePercent
	defer func() { FreePercent = orig }()
	for _, free := range []float64{3.0, 10, 50, -1} { // -1 = 未知，不拦
		FreePercent = func(string) float64 { return free }
		if err := Check("/"); err != nil {
			t.Fatalf("free=%.2f%%: unexpected error %v", free, err)
		}
	}
}
