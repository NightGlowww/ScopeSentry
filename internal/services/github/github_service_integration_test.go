//go:build integration

package github_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	githubrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/github"
	githubsvc "github.com/Autumn-27/ScopeSentry/internal/services/github"
)

// 真实 Mongo + 假 GitHub（httptest）：
//  1. 跑一次任务 → 结果入库、去重键生效、stats 正确；
//  2. 同 commit 再跑 → 不新增（newFound 不涨，文档数不变）；
//  3. Search 响应剩余额度 0 → 任务停止 + nextRunAt 落在 reset 上；
//  4. triage / ignored_repos / CSV 导出。
//
// 全程不碰 api.github.com：客户端 baseURL 由 NewService(WithBaseURL(...)) 注入。

type fakeGitHub struct {
	mu        sync.Mutex
	requests  int
	remaining int
	reset     time.Time
	items     []map[string]interface{}
	lastQuery string
}

func (f *fakeGitHub) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests++
		f.lastQuery = r.URL.Query().Get("q")
		remaining, reset := f.remaining, f.reset
		items := f.items
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Limit", "30")
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
		w.Header().Set("X-RateLimit-Resource", "search")

		switch r.URL.Path {
		case "/search/code", "/search/issues", "/search/repositories":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"total_count":        len(items),
				"incomplete_results": false,
				"items":              items,
			})
		case "/rate_limit":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"resources": map[string]interface{}{
					"core":   map[string]interface{}{"limit": 5000, "remaining": 4999, "reset": reset.Unix()},
					"search": map[string]interface{}{"limit": 30, "remaining": remaining, "reset": reset.Unix()},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		}
	}
}

func (f *fakeGitHub) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

func (f *fakeGitHub) query() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastQuery
}

func codeSearchItems() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"repository": map[string]interface{}{
				"full_name": "acme/leaky-repo",
				"html_url":  "https://github.com/acme/leaky-repo",
			},
			"path":     "config/prod.env",
			"html_url": "https://github.com/acme/leaky-repo/blob/sha-aaa/config/prod.env",
			"sha":      "sha-aaa",
			"text_matches": []map[string]interface{}{
				{"fragment": "AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG"},
			},
		},
		{
			"repository": map[string]interface{}{
				"full_name": "acme/web-app",
				"html_url":  "https://github.com/acme/web-app",
			},
			"path":     "src/db.go",
			"html_url": "https://github.com/acme/web-app/blob/sha-bbb/src/db.go",
			"sha":      "sha-bbb",
			"text_matches": []map[string]interface{}{
				{"fragment": "password := \"hunter2\""},
			},
		},
	}
}

func requireMongo(t *testing.T) context.Context {
	t.Helper()
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	ctx := context.Background()
	if err := githubrepo.EnsureIndexes(ctx, mongodb.DB); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}
	return ctx
}

// seedTask 直接落一条任务（handler 层建任务走 HTTP，这里只测服务层）
func seedTask(t *testing.T, ctx context.Context, taskType, query string) string {
	t.Helper()
	repo := githubrepo.NewTaskRepository()
	task := &models.GithubTask{
		Name:      "it-" + strconv.FormatInt(time.Now().UnixNano(), 10),
		Query:     query,
		Type:      taskType,
		State:     models.GithubTaskStateWaiting,
		CreatedAt: time.Now(),
	}
	if err := repo.Create(ctx, task); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	id := task.ID.Hex()
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = githubrepo.NewTaskRepository().Delete(cleanupCtx, []string{id})
		_, _ = githubrepo.NewResultRepository().DeleteByTaskIDs(cleanupCtx, []string{id})
	})
	return id
}

func loadTask(t *testing.T, ctx context.Context, id string) *models.GithubTask {
	t.Helper()
	task, err := githubrepo.NewTaskRepository().Get(ctx, id)
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	return task
}

// TestGithubRunDedupAndStats 跑一次 → 入库；再跑一次 → 去重不新增
func TestGithubRunDedupAndStats(t *testing.T) {
	ctx := requireMongo(t)

	fake := &fakeGitHub{
		remaining: 29,
		reset:     time.Now().Add(time.Hour),
		items:     codeSearchItems(),
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	svc := githubsvc.NewService(githubsvc.WithBaseURL(srv.URL))
	taskID := seedTask(t, ctx, models.GithubTypeCodeLeak, "AWS_SECRET_ACCESS_KEY filename:.env")

	// 1) 首次运行
	summary, err := svc.RunTask(ctx, taskID)
	if err != nil {
		t.Fatalf("run task: %v", err)
	}
	if summary.Scanned != 2 || summary.NewFound != 2 {
		t.Fatalf("首次运行统计 = scanned:%d new:%d, want 2/2", summary.Scanned, summary.NewFound)
	}
	if summary.Stopped {
		t.Fatalf("未限速时不应停止: %+v", summary)
	}
	if got := fake.query(); got == "" {
		t.Fatalf("假服务没有收到 q 参数")
	}

	task := loadTask(t, ctx, taskID)
	if task.Stats.Scanned != 2 || task.Stats.NewFound != 2 {
		t.Fatalf("任务累计 stats = %+v, want scanned:2 new:2", task.Stats)
	}
	if task.State != models.GithubTaskStateDone {
		t.Fatalf("state = %d, want %d(完成)", task.State, models.GithubTaskStateDone)
	}
	if task.LastRunAt == nil {
		t.Fatalf("lastRunAt 未写入")
	}

	// 结果内容与严重度
	results, total, err := githubrepo.NewResultRepository().List(ctx, models.GithubResultListQuery{
		TaskID: taskID, PageIndex: 1, PageSize: 10,
	})
	if err != nil {
		t.Fatalf("list results: %v", err)
	}
	if total != 2 || len(results) != 2 {
		t.Fatalf("结果数 = %d (total %d), want 2", len(results), total)
	}
	byPath := map[string]models.GithubResult{}
	for _, r := range results {
		byPath[r.Path] = r
		if r.Status != models.GithubResultStatusNew {
			t.Fatalf("新入库状态 = %q, want new", r.Status)
		}
		if r.Source != models.GithubSourceTask {
			t.Fatalf("source = %q, want task", r.Source)
		}
		if r.Severity == "" {
			t.Fatalf("severity 为空: %+v", r)
		}
		if r.Commit == "" {
			t.Fatalf("commit 为空（去重键不能为空）: %+v", r)
		}
	}
	if got := byPath["config/prod.env"].Severity; got != "critical" {
		t.Fatalf("AWS secret 命中严重度 = %q, want critical", got)
	}
	if got := byPath["src/db.go"].Severity; got != "high" {
		t.Fatalf("password 命中严重度 = %q, want high", got)
	}

	// 2) 同 commit 再跑：去重生效，不新增文档、newFound 不涨
	firstRequests := fake.requestCount()
	summary2, err := svc.RunTask(ctx, taskID)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if summary2.NewFound != 0 {
		t.Fatalf("重跑 newFound = %d, want 0（去重失效）", summary2.NewFound)
	}
	if summary2.Scanned != 2 {
		t.Fatalf("重跑 scanned = %d, want 2", summary2.Scanned)
	}
	if fake.requestCount() <= firstRequests {
		t.Fatalf("重跑没有真正发请求（假服务请求数没涨）")
	}

	_, totalAfter, err := githubrepo.NewResultRepository().List(ctx, models.GithubResultListQuery{
		TaskID: taskID, PageIndex: 1, PageSize: 10,
	})
	if err != nil {
		t.Fatalf("list after rerun: %v", err)
	}
	if totalAfter != 2 {
		t.Fatalf("重跑后文档数 = %d, want 2（唯一键去重失败）", totalAfter)
	}
	task = loadTask(t, ctx, taskID)
	if task.Stats.NewFound != 2 || task.Stats.Scanned != 4 {
		t.Fatalf("重跑后累计 stats = %+v, want scanned:4 new:2", task.Stats)
	}
}

// TestGithubRateLimitStopsRun 剩余额度 0 → 立即停止并写 nextRunAt
func TestGithubRateLimitStopsRun(t *testing.T) {
	ctx := requireMongo(t)

	resetAt := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	fake := &fakeGitHub{
		remaining: 0,
		reset:     resetAt,
		items:     codeSearchItems(),
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	svc := githubsvc.NewService(githubsvc.WithBaseURL(srv.URL))
	taskID := seedTask(t, ctx, models.GithubTypeCodeLeak, "token filename:.env")

	summary, err := svc.RunTask(ctx, taskID)
	if err != nil {
		t.Fatalf("run task: %v", err)
	}
	if !summary.Stopped {
		t.Fatalf("额度为 0 时应停止: %+v", summary)
	}
	if summary.NextRunAt == nil {
		t.Fatalf("停止时必须写 nextRunAt")
	}
	if !summary.NextRunAt.Equal(resetAt) {
		t.Fatalf("nextRunAt = %s, want %s（应取 X-RateLimit-Reset）", summary.NextRunAt, resetAt)
	}
	if fake.requestCount() != 1 {
		t.Fatalf("请求数 = %d, want 1（额度不足后不应继续翻页）", fake.requestCount())
	}

	task := loadTask(t, ctx, taskID)
	if task.State != models.GithubTaskStateWaiting {
		t.Fatalf("state = %d, want %d(待开始)", task.State, models.GithubTaskStateWaiting)
	}
	if task.NextRunAt == nil || !task.NextRunAt.Equal(resetAt) {
		t.Fatalf("任务 nextRunAt = %v, want %s", task.NextRunAt, resetAt)
	}
	// 首屏数据仍应入库（额度是「用完之后」才知道的）
	if _, total, err := githubrepo.NewResultRepository().List(ctx, models.GithubResultListQuery{
		TaskID: taskID, PageIndex: 1, PageSize: 10,
	}); err != nil || total != 2 {
		t.Fatalf("已抓到的 2 条应入库，total=%d err=%v", total, err)
	}
}

// TestGithubTriageIgnoredAndExport 研判 / 黑名单 / CSV 导出
func TestGithubTriageIgnoredAndExport(t *testing.T) {
	ctx := requireMongo(t)

	fake := &fakeGitHub{remaining: 29, reset: time.Now().Add(time.Hour), items: codeSearchItems()}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	svc := githubsvc.NewService(githubsvc.WithBaseURL(srv.URL))
	taskID := seedTask(t, ctx, models.GithubTypeCodeLeak, "secret")

	if _, err := svc.RunTask(ctx, taskID); err != nil {
		t.Fatalf("run task: %v", err)
	}
	repo := githubrepo.NewResultRepository()
	results, _, err := repo.List(ctx, models.GithubResultListQuery{TaskID: taskID, PageIndex: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("结果数 = %d, want 2", len(results))
	}

	// triage
	ids := []string{results[0].ID.Hex()}
	updated, err := repo.Triage(ctx, ids, models.GithubResultStatusTriaged, "tester")
	if err != nil {
		t.Fatalf("triage: %v", err)
	}
	if updated != 1 {
		t.Fatalf("triage updated = %d, want 1", updated)
	}
	triaged, _, err := repo.List(ctx, models.GithubResultListQuery{
		TaskID: taskID, Status: models.GithubResultStatusTriaged, PageIndex: 1, PageSize: 10,
	})
	if err != nil {
		t.Fatalf("list triaged: %v", err)
	}
	if len(triaged) != 1 || triaged[0].TriagedBy != "tester" || triaged[0].TriagedAt == nil {
		t.Fatalf("研判落库不正确: %+v", triaged)
	}

	// overview 统计
	rows, total, err := repo.Overview(ctx, models.GithubResultListQuery{TaskID: taskID})
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if total != 2 {
		t.Fatalf("overview total = %d, want 2", total)
	}
	var seenNew, seenTriaged bool
	for _, row := range rows {
		if row.Type != models.GithubTypeCodeLeak {
			t.Fatalf("overview type = %q, want code_leak", row.Type)
		}
		if row.Status == models.GithubResultStatusNew && row.Count == 1 {
			seenNew = true
		}
		if row.Status == models.GithubResultStatusTriaged && row.Count == 1 {
			seenTriaged = true
		}
	}
	if !seenNew || !seenTriaged {
		t.Fatalf("overview 未按 type/status 拆开: %+v", rows)
	}

	// 按天趋势：今天应当有 2 条
	daily, err := repo.DailyStats(ctx, models.GithubResultListQuery{TaskID: taskID}, 3)
	if err != nil {
		t.Fatalf("daily stats: %v", err)
	}
	today := time.Now().Format("2006-01-02")
	var todayTotal int64
	for _, d := range daily {
		if d.Date == today {
			todayTotal = d.Total
		}
	}
	if todayTotal != 2 {
		t.Fatalf("今日趋势 = %d, want 2 (%+v)", todayTotal, daily)
	}

	// 黑名单仓库
	threatRepo := githubrepo.NewThreatRepository()
	added, err := threatRepo.AddIgnoredRepo(ctx, "acme/leaky-repo", "自建靶场噪声")
	if err != nil {
		t.Fatalf("add ignored repo: %v", err)
	}
	if !added {
		t.Fatalf("add ignored repo 未生效")
	}
	again, err := threatRepo.AddIgnoredRepo(ctx, "acme/leaky-repo", "")
	if err != nil {
		t.Fatalf("add ignored repo twice: %v", err)
	}
	if again {
		t.Fatalf("重复添加应为幂等（added=false）")
	}
	ignored, err := threatRepo.ListIgnoredRepos(ctx)
	if err != nil {
		t.Fatalf("list ignored: %v", err)
	}
	found := false
	for _, item := range ignored {
		if item.Repo == "acme/leaky-repo" {
			found = true
		}
	}
	if !found {
		t.Fatalf("黑名单里没有 acme/leaky-repo: %+v", ignored)
	}
	if removed, err := threatRepo.RemoveIgnoredRepo(ctx, "acme/leaky-repo"); err != nil || !removed {
		t.Fatalf("remove ignored repo = %v, %v", removed, err)
	}

	// CSV 导出：表头 + 2 行数据
	var lines int
	err = repo.Iterate(ctx, models.GithubResultListQuery{TaskID: taskID}, 100, func(*models.GithubResult) error {
		lines++
		return nil
	})
	if err != nil {
		t.Fatalf("iterate: %v", err)
	}
	if lines != 2 {
		t.Fatalf("导出遍历行数 = %d, want 2", lines)
	}
}

// TestGithubSchedulerTick 调度到点即跑（scheduler 视图 = 带 intervalMinutes 的任务）
func TestGithubSchedulerTick(t *testing.T) {
	ctx := requireMongo(t)

	fake := &fakeGitHub{remaining: 29, reset: time.Now().Add(time.Hour), items: codeSearchItems()}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	svc := githubsvc.NewService(githubsvc.WithBaseURL(srv.URL))
	taskRepo := githubrepo.NewTaskRepository()
	task := &models.GithubTask{
		Name:      "it-sched-" + strconv.FormatInt(time.Now().UnixNano(), 10),
		Query:     "secret filename:.env",
		Type:      models.GithubTypeCodeLeak,
		State:     models.GithubTaskStateWaiting,
		CreatedAt: time.Now(),
		Config:    models.GithubTaskConfig{IntervalMinutes: 60},
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("seed scheduled task: %v", err)
	}
	taskID := task.ID.Hex()
	t.Cleanup(func() {
		cleanup := context.Background()
		_, _ = taskRepo.Delete(cleanup, []string{taskID})
		_, _ = githubrepo.NewResultRepository().DeleteByTaskIDs(cleanup, []string{taskID})
	})

	list, total, err := taskRepo.List(ctx, models.GithubTaskListQuery{ScheduledOnly: true, PageIndex: 1, PageSize: 200})
	if err != nil {
		t.Fatalf("list scheduled: %v", err)
	}
	if total < 1 {
		t.Fatalf("scheduler 视图没有任务")
	}
	_ = list

	sched := githubsvc.NewScheduler(svc)
	res, err := sched.Tick(ctx)
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	var ran bool
	for _, r := range res.Tasks {
		if r.TaskID == taskID {
			ran = true
			if r.NewFound != 2 {
				t.Fatalf("调度运行 newFound = %d, want 2", r.NewFound)
			}
		}
	}
	if !ran {
		t.Fatalf("调度没有跑到种子任务: %+v (errors=%v)", res.Tasks, res.Errors)
	}
	after := loadTask(t, ctx, taskID)
	if after.NextRunAt == nil {
		t.Fatalf("周期任务跑完后应写 nextRunAt")
	}
	if after.NextRunAt.Before(time.Now().Add(50 * time.Minute)) {
		t.Fatalf("nextRunAt = %s，看起来没有按 intervalMinutes=60 计算", after.NextRunAt)
	}
}

// baseURL 注入 + 额度耗尽后的 pre-flight 拦截（第二次请求必须不打到网络上）
func TestGithubClientBaseURLAndRateGate(t *testing.T) {
	var calls int32
	resetAt := time.Now().Add(time.Hour).Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Limit", "30")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetAt.Unix(), 10))
		w.Header().Set("X-RateLimit-Resource", "search")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"total_count": 0, "incomplete_results": false, "items": []interface{}{},
		})
	}))
	defer srv.Close()

	client := githubsvc.NewClient(githubsvc.ClientOptions{BaseURL: srv.URL + "/"})
	if got := client.BaseURL(); got != srv.URL {
		t.Fatalf("baseURL = %q, want %q（尾部斜杠应被裁掉）", got, srv.URL)
	}

	// 第一次：观察额度（剩余 0 / reset 未来）
	if _, err := client.Search(context.Background(), githubsvc.SearchKindCode, "secret", 1, 10); err != nil {
		t.Fatalf("首次请求: %v", err)
	}
	if _, ok := client.Exhausted("search", time.Now()); !ok {
		t.Fatalf("观察到 remaining=0 后应判定为额度耗尽")
	}

	// 第二次：pre-flight 直接拒绝，不再发请求
	_, err := client.Search(context.Background(), githubsvc.SearchKindCode, "secret", 2, 10)
	var rlErr *githubsvc.RateLimitedError
	if !errors.As(err, &rlErr) {
		t.Fatalf("第二次请求应返回 RateLimitedError，实际: %v", err)
	}
	if !rlErr.Reset.Equal(resetAt) {
		t.Fatalf("RateLimitedError.Reset = %s, want %s", rlErr.Reset, resetAt)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("服务端被调用 %d 次，want 1（pre-flight 没拦住）", got)
	}

	// token_status 路径：未配置 token 时应直接 missing，不触网
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	svc := githubsvc.NewService(githubsvc.WithBaseURL(srv.URL))
	status := svc.TokenStatus(ctx)
	if status.Status != "missing" && status.Status != "valid" && status.Status != "invalid" &&
		status.Status != "rate_limited" && status.Status != "error" {
		t.Fatalf("token_status 状态异常: %+v", status)
	}
}
