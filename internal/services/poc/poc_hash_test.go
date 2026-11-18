package poc

import (
	"strings"
	"testing"
)

func TestPocContentHashNormalizesFormatting(t *testing.T) {
	base := "id: demo\ninfo:\n  name: Demo\n  severity: high\nhttp:\n  - method: GET\n    path:\n      - '{{BaseURL}}/'\n"
	variants := []string{
		base,
		strings.ReplaceAll(base, "\n", "\r\n"),
		base + "\n\n",
		strings.ReplaceAll(base, "\n", "   \n"),
	}
	want := pocContentHash(base)
	for i, variant := range variants {
		if got := pocContentHash(variant); got != want {
			t.Fatalf("variant %d should hash identically: %s != %s", i, got, want)
		}
	}
	changed := strings.ReplaceAll(base, "severity: high", "severity: critical")
	if pocContentHash(changed) == want {
		t.Fatal("content change must change the hash")
	}
}

func TestPocLevelAndTags(t *testing.T) {
	if got := pocLevel("  HIGH "); got != "high" {
		t.Fatalf("level = %q", got)
	}
	if got := pocLevel(""); got != "unknown" {
		t.Fatalf("empty level = %q", got)
	}
	tags := pocTags(" tech,wordpress,tech , ,cve")
	if len(tags) != 3 || tags[0] != "tech" || tags[2] != "cve" {
		t.Fatalf("tags = %#v", tags)
	}
}
