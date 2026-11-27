package middleware

import (
	"encoding/json"
	"testing"
)

func TestClampPageSizeJSON(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		max     int
		wantOK  bool
		wantPS  float64
		wantFrm int
	}{
		{"无 pageSize", `{"pageIndex":1}`, 5000, false, 0, 0},
		{"未超上限", `{"pageIndex":1,"pageSize":200}`, 5000, false, 0, 0},
		{"刚好等于上限", `{"pageSize":5000}`, 5000, false, 0, 0},
		{"超上限", `{"pageIndex":1,"pageSize":100000}`, 5000, true, 5000, 100000},
		{"字符串型 pageSize 不动", `{"pageSize":"100000"}`, 5000, false, 0, 0},
		{"非法 JSON", `not-json`, 5000, false, 0, 0},
		{"null body", `null`, 5000, false, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, from, ok := clampPageSizeJSON([]byte(c.body), c.max)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if !ok {
				return
			}
			if from != c.wantFrm {
				t.Fatalf("from = %d, want %d", from, c.wantFrm)
			}
			var m map[string]any
			if err := json.Unmarshal(out, &m); err != nil {
				t.Fatalf("result not json: %v", err)
			}
			if m["pageSize"].(float64) != c.wantPS {
				t.Fatalf("pageSize = %v, want %v", m["pageSize"], c.wantPS)
			}
		})
	}
}
