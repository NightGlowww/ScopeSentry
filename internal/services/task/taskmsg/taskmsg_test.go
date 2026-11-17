package taskmsg

import "testing"

func TestTaskID(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
		ok   bool
	}{
		{name: "scan template lower id", raw: `{"id":"abc123","name":"t"}`, want: "abc123", ok: true},
		{name: "page monitoring upper ID", raw: `{"ID":"page_monitoring","type":"page_monitoring"}`, want: "page_monitoring", ok: true},
		{name: "missing id", raw: `{"name":"t"}`, ok: false},
		{name: "invalid json", raw: `not-json`, ok: false},
		{name: "empty", raw: ``, ok: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := TaskID([]byte(c.raw))
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v", ok, c.ok)
			}
			if ok && got != c.want {
				t.Fatalf("id = %q, want %q", got, c.want)
			}
		})
	}
}

func TestBelongsTo(t *testing.T) {
	if !BelongsTo([]byte(`{"id":"a"}`), "a") {
		t.Fatal("expected match")
	}
	if BelongsTo([]byte(`{"id":"a"}`), "b") {
		t.Fatal("unexpected match")
	}
	if BelongsTo([]byte(`{"id":"a"}`), "") {
		t.Fatal("empty task id must not match")
	}
}
