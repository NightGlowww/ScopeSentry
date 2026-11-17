package tasksnapshot

import (
	"testing"
)

func TestKeyPrefix(t *testing.T) {
	if got := KeyPrefix("abc123"); got != "TaskInfo:progress:abc123:" {
		t.Fatalf("KeyPrefix = %q", got)
	}
}

func TestTargetFromKey(t *testing.T) {
	prefix := KeyPrefix("task1")
	cases := []struct {
		name   string
		key    string
		want   string
		wantOK bool
	}{
		{name: "plain host", key: prefix + "example.com", want: "example.com", wantOK: true},
		{name: "host with port", key: prefix + "example.com:8080", want: "example.com:8080", wantOK: true},
		{name: "ipv6 target keeps colons", key: prefix + "2001:db8::1", want: "2001:db8::1", wantOK: true},
		{name: "other task prefix", key: KeyPrefix("task2") + "example.com", wantOK: false},
		{name: "empty target", key: prefix, wantOK: false},
		{name: "unrelated key", key: "TaskInfo:tmp:task1", wantOK: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := TargetFromKey(prefix, c.key)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if ok && got != c.want {
				t.Fatalf("target = %q, want %q", got, c.want)
			}
		})
	}
}

func TestBuildRowsSortsAndFilters(t *testing.T) {
	taskID := "task1"
	prefix := KeyPrefix(taskID)
	keys := []string{
		prefix + "b.example.com",
		prefix + "a.example.com",
		prefix + "empty.example.com",
		KeyPrefix("other") + "skip.example.com",
	}
	data := map[string]map[string]string{
		prefix + "b.example.com":                {"All": "done"},
		prefix + "a.example.com":                {"All": "running"},
		prefix + "empty.example.com":            {},
		KeyPrefix("other") + "skip.example.com": {"All": "nope"},
	}

	rows := BuildRows(taskID, keys, data)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (%+v)", len(rows), rows)
	}
	if rows[0].Target != "a.example.com" || rows[1].Target != "b.example.com" {
		t.Fatalf("rows not sorted by target: %+v", rows)
	}
	for _, row := range rows {
		if row.TaskID != taskID {
			t.Errorf("row %q taskID = %q, want %q", row.Target, row.TaskID, taskID)
		}
	}
	if rows[1].Data["All"] != "done" {
		t.Errorf("data lost: %+v", rows[1].Data)
	}
}

func TestBuildRowsHandlesEmptyInput(t *testing.T) {
	if rows := BuildRows("task1", nil, nil); len(rows) != 0 {
		t.Fatalf("rows = %+v, want empty", rows)
	}
}
