package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestStringListUnmarshal(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{"单字符串", `"asset:read"`, []string{"asset:read"}},
		{"数组", `["asset:read","task:write"]`, []string{"asset:read", "task:write"}},
		{"空数组", `[]`, nil},
		{"null", `null`, nil},
		{"去空去空白", `[" asset:read ","", "asset:read"]`, []string{"asset:read"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got StringList
			if err := json.Unmarshal([]byte(tc.raw), &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestStringListMarshalEmpty(t *testing.T) {
	var s StringList
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != "[]" {
		t.Fatalf("empty list should marshal to [], got %s", data)
	}
}

func TestApiKeyIsExpired(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Minute)
	future := now.Add(time.Minute)

	if (&ApiKey{}).IsExpired(now) {
		t.Fatal("nil expiresAt must mean never expires")
	}
	if !(&ApiKey{ExpiresAt: &past}).IsExpired(now) {
		t.Fatal("past expiresAt must be expired")
	}
	if (&ApiKey{ExpiresAt: &future}).IsExpired(now) {
		t.Fatal("future expiresAt must not be expired")
	}
	var nilKey *ApiKey
	if nilKey.IsExpired(now) {
		t.Fatal("nil key must not panic and must not be expired")
	}
}
