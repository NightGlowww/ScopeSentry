package helper

import (
	"strings"
	"testing"
)

// IP 段展开：正常正向、单地址、反向（历史会无限循环）、超大段（历史会 OOM）。
func TestGenerateIPRange(t *testing.T) {
	got, err := GenerateIPRange("10.0.0.1", "10.0.0.3")
	if err != nil || len(got) != 3 || got[0] != "10.0.0.1" || got[2] != "10.0.0.3" {
		t.Fatalf("forward range = %v, %v", got, err)
	}
	got, err = GenerateIPRange("10.0.0.1", "10.0.0.1")
	if err != nil || len(got) != 1 {
		t.Fatalf("single = %v, %v", got, err)
	}
	if _, err = GenerateIPRange("10.0.0.5", "10.0.0.1"); err == nil {
		t.Fatal("reverse range must error, otherwise it loops ~4B times")
	}
	if _, err = GenerateIPRange("1.0.0.0", "255.255.255.255"); err == nil {
		t.Fatal("oversized range must error")
	} else if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err = GenerateIPRange("abc", "10.0.0.1"); err == nil {
		t.Fatal("invalid IP must error")
	}
}

// 反向 IP 段在 GetTargetList 里应退化为原字符串，而不是卡死或吞掉。
func TestGetTargetListReverseRangeDegrades(t *testing.T) {
	got, err := GetTargetList("10.0.0.5-10.0.0.1", "")
	if err != nil {
		t.Fatalf("error %v", err)
	}
	if len(got) != 1 || got[0] != "10.0.0.5-10.0.0.1" {
		t.Fatalf("got %v, want literal fallback", got)
	}
}
