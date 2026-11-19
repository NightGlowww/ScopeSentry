package dictionary

import (
	"testing"
)

func TestFilterLines(t *testing.T) {
	values := []string{"Example.com", "test.EXAMPLE.com", "api.abc.io", "xyz.com", "ABC.def"}
	seqs := []int64{1, 2, 3, 4, 5}

	got := filterLines(values, seqs, "example", 50)
	if len(got) != 2 {
		t.Fatalf("example: got %d matches, want 2", len(got))
	}
	if got[0].Value != "Example.com" || got[0].Seq != 1 {
		t.Fatalf("first match = %+v", got[0])
	}

	got = filterLines(values, seqs, "abc", 1)
	if len(got) != 1 {
		t.Fatalf("limit=1 should cap results, got %d", len(got))
	}

	if got = filterLines(values, seqs, "nomatch", 50); len(got) != 0 {
		t.Fatalf("nomatch: got %d", len(got))
	}

	// 大小写不敏感：ABC 匹配 api.abc.io 与 ABC.def
	if got = filterLines(values, seqs, "ABC", 50); len(got) != 2 {
		t.Fatalf("ABC case-insensitive: got %d", len(got))
	}
}

func TestLineCacheInvalidate(t *testing.T) {
	c := newLineCache()
	c.lines["d1"] = cacheEntry{values: []string{"a"}, seqs: []int64{1}}
	if v, _, ok, _ := c.load(nil, nil, "d1"); !ok || len(v) != 1 {
		t.Fatalf("cache hit failed: %v %v", v, ok)
	}
	c.invalidate("d1")
	c.mu.RLock()
	_, exists := c.lines["d1"]
	c.mu.RUnlock()
	if exists {
		t.Fatal("invalidate did not remove entry")
	}
}
