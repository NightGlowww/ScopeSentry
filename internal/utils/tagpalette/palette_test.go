package tagpalette

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeTag(t *testing.T) {
	got, err := NormalizeTag("  prod  ")
	if err != nil || got != "prod" {
		t.Fatalf("NormalizeTag = %q, %v", got, err)
	}
	if _, err := NormalizeTag("   "); !errors.Is(err, ErrInvalidTag) {
		t.Fatalf("empty tag should be rejected, got %v", err)
	}
	if _, err := NormalizeTag(strings.Repeat("字", MaxTagLength+1)); !errors.Is(err, ErrInvalidTag) {
		t.Fatalf("too long tag should be rejected, got %v", err)
	}
	// 正好上限应放行（按字符数，不是字节）
	exact := strings.Repeat("字", MaxTagLength)
	if _, err := NormalizeTag(exact); err != nil {
		t.Fatalf("exact limit rejected: %v", err)
	}
}

func TestNormalizeColor(t *testing.T) {
	cases := []struct {
		in   string
		want string
		err  bool
	}{
		{in: "", want: ""},
		{in: "   ", want: ""},
		{in: "danger", want: "danger"},
		{in: "  SUCCESS ", want: "success"},
		{in: "#fff", want: "#fff"},
		{in: "#12AB34", want: "#12AB34"},
		{in: "red", err: true},
		{in: "#12345", err: true},
		{in: "#gggggg", err: true},
	}
	for _, c := range cases {
		got, err := NormalizeColor(c.in)
		if c.err {
			if !errors.Is(err, ErrInvalidColor) {
				t.Errorf("NormalizeColor(%q) err = %v, want ErrInvalidColor", c.in, err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("NormalizeColor(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}
