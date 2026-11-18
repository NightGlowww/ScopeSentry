package hoststat

import "testing"

func TestWarningsThresholds(t *testing.T) {
	cases := []struct {
		name string
		s    Stats
		want int
	}{
		{"healthy", Stats{CPUs: 4, Load1: 1, DiskTotalBytes: 100, DiskFreePercent: 50, MemAvailablePercent: 40}, 0},
		{"disk low", Stats{CPUs: 4, Load1: 1, DiskTotalBytes: 100, DiskFreePercent: 5, MemAvailablePercent: 40}, 1},
		{"load high", Stats{CPUs: 2, Load1: 9, DiskTotalBytes: 100, DiskFreePercent: 50, MemAvailablePercent: 40}, 1},
		{"mem low", Stats{CPUs: 4, Load1: 1, DiskTotalBytes: 100, DiskFreePercent: 50, MemAvailablePercent: 3}, 1},
		{"all bad", Stats{CPUs: 2, Load1: 9, DiskTotalBytes: 100, DiskFreePercent: 2, MemAvailablePercent: 3}, 3},
		{"unknown fields ignored", Stats{CPUs: 0, Load1: -1, DiskTotalBytes: 0, MemAvailablePercent: -1}, 0},
	}
	for _, c := range cases {
		if got := len(Warnings(c.s)); got != c.want {
			t.Fatalf("%s: got %d warnings %v, want %d", c.name, got, Warnings(c.s), c.want)
		}
	}
}

func TestStatReturnsSomething(t *testing.T) {
	s := Stat("/")
	if s.CPUs <= 0 {
		t.Fatalf("CPUs = %d", s.CPUs)
	}
	if s.DiskTotalBytes == 0 {
		t.Fatalf("disk total = 0 (statfs failed?)")
	}
	if s.DiskFreePercent < 0 || s.DiskFreePercent > 100 {
		t.Fatalf("diskFreePercent = %v", s.DiskFreePercent)
	}
}
