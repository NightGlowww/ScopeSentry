package dictionary

import (
	"bufio"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// 上传行数统计：以 '\n' 计数，末行没有换行符要补 1。
func TestLineCounterTotal(t *testing.T) {
	cases := []struct {
		content string
		want    int64
	}{
		{content: "", want: 0},
		{content: "a\n", want: 1},
		{content: "a\nb\n", want: 2},
		{content: "a\nb", want: 2},
		{content: "a", want: 1},
		{content: "a\n\n\nb\n", want: 4},
	}
	for _, c := range cases {
		counter := &lineCounter{}
		if _, err := counter.Write([]byte(c.content)); err != nil {
			t.Fatalf("lineCounter.Write: %v", err)
		}
		if got := counter.total(); got != c.want {
			t.Fatalf("total(%q) = %d, want %d", c.content, got, c.want)
		}
	}

	// 分块写入（模拟 io.Copy 的分片）也要算对
	counter := &lineCounter{}
	for _, chunk := range []string{"a\nb", "\nc\nd", "\n"} {
		if _, err := counter.Write([]byte(chunk)); err != nil {
			t.Fatalf("lineCounter.Write: %v", err)
		}
	}
	if got := counter.total(); got != 4 {
		t.Fatalf("分块写入 total = %d, want 4", got)
	}
}

// 进度 = 已处理行 / 总行数；done 固定 100，failed 保留比例。
func TestComputeProgress(t *testing.T) {
	task := &models.DictionaryUploadTask{Status: models.DictUploadStatusRunning, TotalLines: 1000, ProcessedLines: 250}
	if got := computeProgress(task); got != 25 {
		t.Fatalf("running progress = %v, want 25", got)
	}
	task.Status = models.DictUploadStatusDone
	if got := computeProgress(task); got != 100 {
		t.Fatalf("done progress = %v, want 100", got)
	}
	task.Status = models.DictUploadStatusRunning
	task.TotalLines = 0
	if got := computeProgress(task); got != 0 {
		t.Fatalf("未知总数 progress = %v, want 0", got)
	}
	task.Status = models.DictUploadStatusFailed
	task.TotalLines = 100
	task.ProcessedLines = 40
	if got := computeProgress(task); got != 40 {
		t.Fatalf("failed progress = %v, want 40", got)
	}
}

// 任务快照：get 必须返回副本，后台改状态不能影响已发出的快照。
func TestTaskStoreReturnsSnapshots(t *testing.T) {
	store := newTaskStore()
	now := time.Now()
	finished := now.Add(time.Second)
	task := &models.DictionaryUploadTask{
		TaskID:     "t1",
		Kind:       "dir",
		Name:       "default",
		Status:     models.DictUploadStatusRunning,
		TotalLines: 100,
		CreatedAt:  now,
		FinishedAt: &finished,
	}
	store.put(cloneTask(task))
	task.Status = models.DictUploadStatusDone

	got := store.get("t1")
	if got == nil {
		t.Fatalf("store.get 未取到任务")
	}
	if got.Status != models.DictUploadStatusRunning {
		t.Fatalf("快照被后台修改污染: status=%s", got.Status)
	}
	got.Status = models.DictUploadStatusFailed
	again := store.get("t1")
	if again.Status != models.DictUploadStatusRunning {
		t.Fatalf("取出的副本被改后影响到了存储: %s", again.Status)
	}
	if again.FinishedAt == nil || !again.FinishedAt.Equal(finished) {
		t.Fatalf("FinishedAt 拷贝不正确: %v", again.FinishedAt)
	}
	if store.get("missing") != nil {
		t.Fatalf("不存在的任务应当返回 nil")
	}
}

// 内存任务回收：只清终态且超 TTL 的。
func TestTaskStorePrune(t *testing.T) {
	store := newTaskStore()
	old := time.Now().Add(-2 * uploadTaskTTL)
	store.put(&models.DictionaryUploadTask{TaskID: "done-old", Status: models.DictUploadStatusDone, UpdatedAt: old})
	store.put(&models.DictionaryUploadTask{TaskID: "done-new", Status: models.DictUploadStatusDone, UpdatedAt: time.Now()})
	store.put(&models.DictionaryUploadTask{TaskID: "running-old", Status: models.DictUploadStatusRunning, UpdatedAt: old})

	store.prune(uploadTaskTTL)
	if store.get("done-old") != nil {
		t.Fatalf("过期终态任务应当被回收")
	}
	if store.get("done-new") == nil {
		t.Fatalf("未过期任务不该被回收")
	}
	if store.get("running-old") == nil {
		t.Fatalf("运行中任务不该被回收")
	}
}

// 上传解析用的分行口径与 NormalizeLine 一致：CRLF 能被吃掉、空行丢弃。
func TestUploadLineNormalization(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("admin\r\nroot\r\n\r\nAdmin\r\n"))
	lines := make([]string, 0, 4)
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			if v, ok := NormalizeLine(line); ok {
				lines = append(lines, v)
			}
		}
		if err != nil {
			break
		}
	}
	want := []string{"admin", "root", "Admin"}
	if len(lines) != len(want) {
		t.Fatalf("读到 %v, want %v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("lines[%d] = %q, want %q", i, lines[i], want[i])
		}
	}
}
