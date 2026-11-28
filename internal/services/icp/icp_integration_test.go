//go:build integration

package icp_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	icprepo "github.com/Autumn-27/ScopeSentry/internal/repositories/icp"
	assetgroup "github.com/Autumn-27/ScopeSentry/internal/services/assets/assetgroup"
	icpsvc "github.com/Autumn-27/ScopeSentry/internal/services/icp"
	"go.mongodb.org/mongo-driver/bson"
)

// ICP 服务层集成测试（真连 Mongo，provider 用 fake）。
//
// 覆盖任务要求的四组语义：
//  1. fake 返回固定 3 条 → restart 后状态 3、stats 3/3/3，重复跑 found=0；
//  2. provider 429 → 指数退避重试耗尽 → 暂停 + lastError，resume 继续跑完；
//  3. sync → asset_scope.domain_array 去重追加且不覆盖已有条目（ip_array 不动）；
//  4. CSV 行数 / backfill 逐条失败记录 / manual·none·http 三个 provider 的行为。

// fakeProvider 可编排的 provider
type fakeProvider struct {
	name      string
	domainFn  func(ctx context.Context, domain string) ([]models.ICPRecord, error)
	companyFn func(ctx context.Context, company string) ([]models.ICPRecord, error)
	calls     atomic.Int64
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Status() (bool, string) { return true, "" }

func (f *fakeProvider) QueryDomain(ctx context.Context, domain string) ([]models.ICPRecord, error) {
	f.calls.Add(1)
	if f.domainFn == nil {
		return nil, nil
	}
	return f.domainFn(ctx, domain)
}

func (f *fakeProvider) QueryCompany(ctx context.Context, company string) ([]models.ICPRecord, error) {
	f.calls.Add(1)
	if f.companyFn == nil {
		return nil, nil
	}
	return f.companyFn(ctx, company)
}

var (
	ensureOnce sync.Once
	ensureErr  error
)

// requireMongo 真连 Mongo 时补齐索引；没配环境变量则跳过整个用例
func requireMongo(t *testing.T) {
	t.Helper()
	if mongodb.DB == nil {
		t.Skip("mongo not initialized (set MONGODB_IP/MONGODB_USER/MONGODB_PASSWORD/MONGODB_DATABASE)")
	}
	ensureOnce.Do(func() {
		ensureErr = mongodb.EnsureIndexes(mongodb.DB)
	})
	if ensureErr != nil {
		t.Fatalf("ensure indexes: %v", ensureErr)
	}
}

func uniqueName(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("icp-it-%s-%d", strings.ReplaceAll(t.Name(), "/", "-"), time.Now().UnixNano())
}

func newService(provider icpsvc.ICPProvider) *icpsvc.Service {
	return icpsvc.NewService(
		icpsvc.WithProviderOverride(provider),
		icpsvc.WithRetry(2, time.Millisecond),
		icpsvc.WithRunTimeout(30*time.Second),
	)
}

// fixedRecords 固定 3 条备案记录（域名随查询变化，company/icpNo 固定）
func fixedRecords(query string) []models.ICPRecord {
	return []models.ICPRecord{
		{Domain: "a-" + query, Company: "测试科技有限公司", ICPNo: "京ICP备00000001号-1", SiteName: "官网", Type: "web"},
		{Domain: "b-" + query, Company: "测试科技有限公司", ICPNo: "京ICP备00000001号-2", SiteName: "商城", Type: "web"},
		{Domain: "c-" + query, Company: "测试科技有限公司", ICPNo: "京ICP备00000001号-3", SiteName: "开放平台", Type: "web"},
	}
}

// waitState 轮询任务状态（goroutine + 状态轮询模型的测试侧等价物）
func waitState(t *testing.T, svc *icpsvc.Service, taskID string, want int) *models.IcpTask {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(10 * time.Second)
	var last *models.IcpTask
	for time.Now().Before(deadline) {
		task, err := svc.GetTask(ctx, taskID)
		if err == nil {
			last = task
			if task.State == want {
				return task
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if last == nil {
		t.Fatalf("任务 %s 一直读不到状态", taskID)
	}
	t.Fatalf("任务 %s 状态 = %d, want %d (lastError=%q stats=%+v)",
		taskID, last.State, want, last.LastError, last.Stats)
	return nil
}

// waitStateAny 等到任务进入任意一个目标状态（自动跑可能以 done 或 paused 收尾）
func waitStateAny(t *testing.T, svc *icpsvc.Service, taskID string, want ...int) *models.IcpTask {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(10 * time.Second)
	var last *models.IcpTask
	for time.Now().Before(deadline) {
		task, err := svc.GetTask(ctx, taskID)
		if err == nil {
			last = task
			for _, w := range want {
				if task.State == w {
					return task
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if last == nil {
		t.Fatalf("任务 %s 一直读不到状态", taskID)
	}
	t.Fatalf("任务 %s 状态 = %d, want 之一 %v（lastError=%q）", taskID, last.State, want, last.LastError)
	return nil
}

func createTask(t *testing.T, svc *icpsvc.Service, name, taskType string, queries ...string) *models.IcpTask {
	t.Helper()
	ctx := context.Background()
	task, err := svc.CreateTask(ctx, models.IcpTaskCreateRequest{
		Name:    name,
		Type:    taskType,
		Queries: queries,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() {
		cleanup := context.Background()
		if _, _, _, err := svc.Delete(cleanup, []string{task.ID.Hex()}); err != nil {
			t.Logf("cleanup task %s: %v", task.ID.Hex(), err)
		}
	})
	return task
}

// TestIcpServiceRunDedupExport 固定 3 条 → 完成 / 统计 / 去重 / CSV / 日志
func TestIcpServiceRunDedupExport(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	provider := &fakeProvider{name: "fake", domainFn: func(_ context.Context, domain string) ([]models.ICPRecord, error) {
		return fixedRecords(domain), nil
	}}
	svc := newService(provider)
	// 新建即开跑（service.CreateTask 末尾会 Claim + launch）：直接从「等待完成」开始等。
	// 这里原来先调了一次 Restart，行为改掉之后它会稳定命中 ErrTaskBusy —— 而这条用例
	// 因为从来没人跑过，红着也没人知道。
	task := createTask(t, svc, uniqueName(t), models.IcpTaskTypeDomain, "run.example.com")
	done := waitState(t, svc, task.ID.Hex(), models.IcpTaskStateDone)
	if done.Stats.Total != 3 || done.Stats.Done != 3 || done.Stats.Found != 3 || done.Stats.Failed != 0 {
		t.Fatalf("首次运行 stats = %+v, want total=3 done=3 found=3 failed=0", done.Stats)
	}

	items, total, err := svc.ListResults(ctx, models.IcpAssetQuery{TaskID: task.ID.Hex(), PageIndex: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list results: %v", err)
	}
	if total != 3 || len(items) != 3 {
		t.Fatalf("结果条数 total=%d len=%d, want 3", total, len(items))
	}
	if items[0].Source != "fake" {
		t.Fatalf("结果 source = %q, want fake", items[0].Source)
	}

	// 重跑：唯一键去重，found 必须为 0
	if _, err := svc.Restart(ctx, task.ID.Hex()); err != nil {
		t.Fatalf("restart again: %v", err)
	}
	again := waitState(t, svc, task.ID.Hex(), models.IcpTaskStateDone)
	if again.Stats.Found != 0 {
		t.Fatalf("重跑 found = %d, want 0（去重失效）", again.Stats.Found)
	}
	if again.Stats.Total != 3 || again.Stats.Done != 3 {
		t.Fatalf("重跑 stats = %+v, want total=3 done=3", again.Stats)
	}

	// CSV：表头 + 3 行
	var buf bytes.Buffer
	rows, err := svc.Export(ctx, task.ID.Hex(), &buf)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if rows != 3 {
		t.Fatalf("export rows = %d, want 3", rows)
	}
	records, err := csv.NewReader(bytes.NewReader(buf.Bytes())).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(records) != 4 {
		t.Fatalf("csv 行数 = %d, want 4（表头 + 3 条）", len(records))
	}
	if records[0][1] != "domain" || records[0][3] != "icpNo" {
		t.Fatalf("csv 表头不符: %v", records[0])
	}

	// 日志：至少有一条「任务完成」，且不超过 200 行
	lines, err := svc.Logs(ctx, task.ID.Hex(), 0)
	if err != nil {
		t.Fatalf("logs: %v", err)
	}
	if len(lines) == 0 || len(lines) > models.IcpMaxLogLines {
		t.Fatalf("日志行数 = %d, want 1..%d", len(lines), models.IcpMaxLogLines)
	}
	var hasDone bool
	for _, line := range lines {
		if strings.Contains(line.Line, "任务完成") {
			hasDone = true
		}
	}
	if !hasDone {
		t.Fatalf("日志里没有任务完成记录: %+v", lines)
	}
}

// TestIcpServiceRetryPauseResume provider 429 → 重试耗尽暂停 → resume 继续
func TestIcpServiceRetryPauseResume(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	var failing atomic.Bool
	failing.Store(true)
	provider := &fakeProvider{name: "fake-429", domainFn: func(_ context.Context, domain string) ([]models.ICPRecord, error) {
		if failing.Load() {
			return nil, &icpsvc.ProviderError{StatusCode: http.StatusTooManyRequests, Message: "too many requests"}
		}
		return fixedRecords(domain), nil
	}}
	svc := newService(provider)
	task := createTask(t, svc, uniqueName(t), models.IcpTaskTypeDomain, "retry.example.com")
	// 同上：新建即开跑，429 重试耗尽后自己会变 paused
	paused := waitState(t, svc, task.ID.Hex(), models.IcpTaskStatePaused)
	if !strings.Contains(paused.LastError, "429") {
		t.Fatalf("lastError = %q, want 包含 429", paused.LastError)
	}
	if paused.Stats.Failed == 0 {
		t.Fatalf("暂停后 stats.failed = 0, want >=1")
	}
	// 1 次初始调用 + 2 次重试
	if calls := provider.calls.Load(); calls != 3 {
		t.Fatalf("provider 调用次数 = %d, want 3（初始 + 2 次重试）", calls)
	}

	// 恢复后继续，把同一个查询跑完
	failing.Store(false)
	if _, err := svc.Resume(ctx, task.ID.Hex()); err != nil {
		t.Fatalf("resume: %v", err)
	}
	done := waitState(t, svc, task.ID.Hex(), models.IcpTaskStateDone)
	if done.Stats.Found != 3 || done.Stats.Total != 3 {
		t.Fatalf("resume 后 stats = %+v, want found=3 total=3", done.Stats)
	}
	if done.LastError != "" {
		t.Fatalf("resume 完成后 lastError = %q, want 空", done.LastError)
	}
}

// TestIcpServiceSyncScope sync 去重追加、不覆盖已有条目、不动 ip_array
func TestIcpServiceSyncScope(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	groupSvc := assetgroup.NewService()
	scopeName := uniqueName(t)

	// 先造一个已有条目的地契：两个域名 + 一个 IP
	scopeID, err := groupSvc.CreateScope(ctx, &models.AssetScopeAddRequest{
		Name:      scopeName,
		Scope:     "keep.example.com\ndup.example.com\n10.0.0.1",
		ScopeType: "",
		GroupID:   "",
	})
	if err != nil {
		t.Fatalf("create scope: %v", err)
	}
	t.Cleanup(func() {
		if _, err := groupSvc.DeleteScopes(context.Background(), []string{scopeID}); err != nil {
			t.Logf("cleanup scope %s: %v", scopeID, err)
		}
	})

	provider := &fakeProvider{name: "fake-sync", domainFn: func(_ context.Context, _ string) ([]models.ICPRecord, error) {
		return []models.ICPRecord{
			{Domain: "dup.example.com", Company: "测试科技有限公司", ICPNo: "京ICP备00000002号-1"},
			{Domain: "new1.example.com", Company: "测试科技有限公司", ICPNo: "京ICP备00000002号-2"},
			{Domain: "new2.example.com", Company: "测试科技有限公司", ICPNo: "京ICP备00000002号-3"},
		}, nil
	}}
	svc := newService(provider)
	task := createTask(t, svc, scopeName, models.IcpTaskTypeDomain, "sync.example.com")
	waitState(t, svc, task.ID.Hex(), models.IcpTaskStateDone)

	summary, err := svc.Sync(ctx, task.ID.Hex(), "")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if summary.Added != 2 {
		t.Fatalf("sync added = %d, want 2（dup 已存在，只新增 new1/new2）", summary.Added)
	}
	if summary.ScopeID != scopeID {
		t.Fatalf("sync scopeId = %q, want %q（应命中同名地契）", summary.ScopeID, scopeID)
	}

	scope := loadScope(t, groupSvc, scopeName)
	if got := countOf(scope.DomainArray, "dup.example.com"); got != 1 {
		t.Fatalf("domain_array 里 dup.example.com 出现 %d 次, want 1: %v", got, scope.DomainArray)
	}
	for _, domain := range []string{"keep.example.com", "new1.example.com", "new2.example.com"} {
		if countOf(scope.DomainArray, domain) != 1 {
			t.Fatalf("domain_array 缺少 %s 或有重复: %v", domain, scope.DomainArray)
		}
	}
	if len(scope.DomainArray) != 4 {
		t.Fatalf("domain_array = %v, want 4 条", scope.DomainArray)
	}
	if len(scope.IPArray) != 1 || scope.IPArray[0] != "10.0.0.1" {
		t.Fatalf("ip_array = %v, want [10.0.0.1]（sync 不应改动 IP）", scope.IPArray)
	}
	if countOf(scope.ScopeArray, "10.0.0.1") != 1 || countOf(scope.ScopeArray, "new1.example.com") != 1 {
		t.Fatalf("scope_array = %v, want 含 10.0.0.1 与 new1.example.com", scope.ScopeArray)
	}

	// 幂等：再 sync 一次，不新增
	second, err := svc.Sync(ctx, task.ID.Hex(), "")
	if err != nil {
		t.Fatalf("sync again: %v", err)
	}
	if second.Added != 0 || second.Total != 4 {
		t.Fatalf("二次 sync added=%d total=%d, want added=0 total=4", second.Added, second.Total)
	}
}

func loadScope(t *testing.T, svc assetgroup.Service, name string) *models.AssetScope {
	t.Helper()
	items, _, err := svc.ListScopes(context.Background(), "", name, 1, 50)
	if err != nil {
		t.Fatalf("list scopes: %v", err)
	}
	for i := range items {
		if items[i].Name == name {
			return &items[i]
		}
	}
	t.Fatalf("没找到地契 %s", name)
	return nil
}

func countOf(values []string, want string) int {
	count := 0
	for _, v := range values {
		if v == want {
			count++
		}
	}
	return count
}

// TestIcpServiceBackfill backfill 只补空字段，失败逐条记录
func TestIcpServiceBackfill(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	provider := &fakeProvider{name: "fake-backfill", domainFn: func(_ context.Context, domain string) ([]models.ICPRecord, error) {
		switch domain {
		case "a.example.com":
			return []models.ICPRecord{{Domain: domain, Company: "甲公司", ICPNo: "京ICP备00000003号-1", SiteName: "甲官网"}}, nil
		case "b.example.com":
			return nil, &icpsvc.ProviderError{StatusCode: http.StatusBadGateway, Message: "bad gateway"}
		default:
			return nil, nil
		}
	}}
	svc := newService(provider)
	task := createTask(t, svc, uniqueName(t), models.IcpTaskTypeDomain, "a.example.com", "b.example.com")
	// 新建即开跑：先让它跑完（a 成功、b 502 → 任务暂停），再清掉它写的结果。
	// 不清的话，backfill 把 a.example.com 补成 甲公司/京ICP备…0003号-1 时会撞上自动跑留下的
	// 同一条 (domain, company, icpNo) 唯一键，报 E11000 —— 这不是 backfill 的 bug。
	waitStateAny(t, svc, task.ID.Hex(), models.IcpTaskStatePaused, models.IcpTaskStateDone)

	// 先造两条已有但缺 company/icpNo 的结果
	collection := mongodb.DB.Collection(icprepo.CollectionResult)
	if _, err := collection.DeleteMany(ctx, bson.M{"taskId": task.ID.Hex()}); err != nil {
		t.Fatalf("clean auto-run results: %v", err)
	}
	now := time.Now()
	if _, err := collection.InsertMany(ctx, []interface{}{
		bson.M{"taskId": task.ID.Hex(), "domain": "a.example.com", "company": "", "icpNo": "", "siteName": "", "type": "web", "source": "manual", "createdAt": now},
		bson.M{"taskId": task.ID.Hex(), "domain": "b.example.com", "company": "", "icpNo": "", "siteName": "", "type": "web", "source": "manual", "createdAt": now},
	}); err != nil {
		t.Fatalf("seed results: %v", err)
	}

	summary, err := svc.Backfill(ctx, task.ID.Hex())
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if summary.Scanned != 2 || summary.Updated != 1 || summary.Failed != 1 {
		t.Fatalf("backfill summary = %+v, want scanned=2 updated=1 failed=1", summary)
	}
	if len(summary.Failures) != 1 || summary.Failures[0].Domain != "b.example.com" {
		t.Fatalf("backfill failures = %+v, want 一条 b.example.com", summary.Failures)
	}

	var updated struct {
		Company  string `bson:"company"`
		ICPNo    string `bson:"icpNo"`
		SiteName string `bson:"siteName"`
	}
	if err := collection.FindOne(ctx, bson.M{"taskId": task.ID.Hex(), "domain": "a.example.com"}).Decode(&updated); err != nil {
		t.Fatalf("load backfilled result: %v", err)
	}
	if updated.Company != "甲公司" || updated.ICPNo != "京ICP备00000003号-1" || updated.SiteName != "甲官网" {
		t.Fatalf("补全结果 = %+v, want 甲公司/京ICP备00000003号-1/甲官网", updated)
	}
}

// TestIcpServiceCreateValidation 参数校验
func TestIcpServiceCreateValidation(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()
	svc := newService(&fakeProvider{name: "fake"})

	if _, err := svc.CreateTask(ctx, models.IcpTaskCreateRequest{Type: "domain"}); !errors.Is(err, icpsvc.ErrEmptyQueries) {
		t.Fatalf("空 queries 错误 = %v, want ErrEmptyQueries", err)
	}
	if _, err := svc.CreateTask(ctx, models.IcpTaskCreateRequest{Type: "ip", Queries: []string{"x"}}); !errors.Is(err, icpsvc.ErrInvalidType) {
		t.Fatalf("非法 type 错误 = %v, want ErrInvalidType", err)
	}
	if _, err := svc.CreateTask(ctx, models.IcpTaskCreateRequest{Type: "domain", Queries: []string{"x"}, Provider: "tyc"}); !errors.Is(err, icpsvc.ErrInvalidProvider) {
		t.Fatalf("非法 provider 错误 = %v, want ErrInvalidProvider", err)
	}
}

// TestIcpProviderHTTPContract http provider 契约（httptest 假服务）
func TestIcpProviderHTTPContract(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	var authHeader atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader.Store(r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/icp/domain":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"records":[
				{"domain":"a.example.com","company":"测试科技有限公司","icpNo":"京ICP备00000004号-1","siteName":"官网","type":"web"},
				{"domain":"b.example.com","company":"测试科技有限公司","icpNo":"京ICP备00000004号-2","siteName":"商城","type":"web"},
				{"domain":"c.example.com","company":"测试科技有限公司","icpNo":"京ICP备00000004号-3","siteName":"开放平台","type":"web"}
			]}`))
		case "/icp/company":
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	registry := icpsvc.NewProviderRegistryWithConfig(nil, func(context.Context) icpsvc.ProviderConfig {
		return icpsvc.ProviderConfig{BaseURL: server.URL, Token: "test-token"}
	})
	provider := registry.Resolve(ctx)
	if provider.Name() != models.IcpProviderHTTP {
		t.Fatalf("resolve provider = %q, want http", provider.Name())
	}
	if available, reason := provider.Status(); !available {
		t.Fatalf("http provider 不可用: %s", reason)
	}

	records, err := provider.QueryDomain(ctx, "example.com")
	if err != nil {
		t.Fatalf("query domain: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("records = %d, want 3", len(records))
	}
	if got, _ := authHeader.Load().(string); got != "Bearer test-token" {
		t.Fatalf("Authorization = %q, want Bearer test-token", got)
	}

	_, err = provider.QueryCompany(ctx, "测试科技有限公司")
	var providerErr *icpsvc.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("429 错误类型 = %T, want *ProviderError", err)
	}
	if providerErr.StatusCode != http.StatusTooManyRequests || !providerErr.Retryable() {
		t.Fatalf("429 错误 = %+v, want retryable 429", providerErr)
	}
}

// TestIcpProviderManualAndNone manual 从本地结果回读；none 给出明确原因
func TestIcpProviderManualAndNone(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	company := uniqueName(t)
	collection := mongodb.DB.Collection(icprepo.CollectionResult)
	if _, err := collection.InsertOne(ctx, bson.M{
		"taskId": "manual-import", "domain": "manual.example.com", "company": company,
		"icpNo": "京ICP备00000005号-1", "siteName": "手工导入", "type": "web",
		"source": models.IcpProviderManual, "createdAt": time.Now(),
	}); err != nil {
		t.Fatalf("seed manual record: %v", err)
	}
	t.Cleanup(func() {
		_, _ = collection.DeleteMany(context.Background(), bson.M{"taskId": "manual-import"})
	})

	registry := icpsvc.NewProviderRegistry(icprepo.NewResultRepository())
	manual := registry.Get(ctx, models.IcpProviderManual)
	if manual == nil {
		t.Fatalf("manual provider 未注册")
	}
	if available, _ := manual.Status(); !available {
		t.Fatalf("manual provider 应永远可用")
	}
	records, err := manual.QueryCompany(ctx, company)
	if err != nil {
		t.Fatalf("manual query company: %v", err)
	}
	if len(records) != 1 || records[0].Domain != "manual.example.com" {
		t.Fatalf("manual records = %+v, want 1 条 manual.example.com", records)
	}

	none := registry.Get(ctx, models.IcpProviderNone)
	if none == nil {
		t.Fatalf("none provider 未注册")
	}
	if available, reason := none.Status(); available || strings.TrimSpace(reason) == "" {
		t.Fatalf("none status = (%v, %q), want 不可用且有原因", available, reason)
	}
	if _, err := none.QueryDomain(ctx, "x.example.com"); !errors.Is(err, icpsvc.ErrProviderUnavailable) {
		t.Fatalf("none query 错误 = %v, want ErrProviderUnavailable", err)
	}
}

// TestIcpIndexesExist initdb 必须建出任务要求的索引（含去重唯一索引）
func TestIcpIndexesExist(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	want := map[string][]bson.M{
		icprepo.CollectionTask: {
			{"state": int32(1), "createdAt": int32(-1)},
		},
		icprepo.CollectionResult: {
			{"taskId": int32(1), "createdAt": int32(-1)},
			{"domain": int32(1)},
			{"company": int32(1)},
			{"domain": int32(1), "company": int32(1), "icpNo": int32(1)},
		},
		icprepo.CollectionLog: {
			{"taskId": int32(1), "createdAt": int32(-1)},
		},
	}

	for collection, wants := range want {
		cursor, err := mongodb.DB.Collection(collection).Indexes().List(ctx)
		if err != nil {
			t.Fatalf("list %s indexes: %v", collection, err)
		}
		var indexes []bson.M
		if err := cursor.All(ctx, &indexes); err != nil {
			t.Fatalf("decode %s indexes: %v", collection, err)
		}
		for _, keys := range wants {
			if !hasIndexKeys(indexes, keys) {
				t.Fatalf("%s 缺少索引 %v，实际=%v", collection, keys, indexes)
			}
		}
	}

	// 去重键必须是 unique
	cursor, err := mongodb.DB.Collection(icprepo.CollectionResult).Indexes().List(ctx)
	if err != nil {
		t.Fatalf("list result indexes: %v", err)
	}
	var resultIndexes []bson.M
	if err := cursor.All(ctx, &resultIndexes); err != nil {
		t.Fatalf("decode result indexes: %v", err)
	}
	dedup := findIndex(resultIndexes, bson.M{"domain": int32(1), "company": int32(1), "icpNo": int32(1)})
	if dedup == nil {
		t.Fatalf("icp_result 缺少 (domain,company,icpNo) 索引: %v", resultIndexes)
	}
	if unique, _ := dedup["unique"].(bool); !unique {
		t.Fatalf("icp_result 去重索引不是 unique: %v", dedup)
	}
}

// hasIndexKeys 索引 key 的前缀（顺序敏感）是否匹配
func hasIndexKeys(indexes []bson.M, want bson.M) bool {
	for _, index := range indexes {
		key, _ := index["key"].(bson.M)
		if key == nil {
			continue
		}
		if len(key) != len(want) {
			continue
		}
		matched := true
		for field, order := range want {
			got, ok := key[field]
			if !ok || got != order {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func findIndex(indexes []bson.M, want bson.M) bson.M {
	for _, index := range indexes {
		key, _ := index["key"].(bson.M)
		if key == nil || len(key) != len(want) {
			continue
		}
		matched := true
		for field, order := range want {
			if got, ok := key[field]; !ok || got != order {
				matched = false
				break
			}
		}
		if matched {
			return index
		}
	}
	return nil
}

// TestIcpServiceConcurrentRestartRejected 同一个 task 不允许并发运行
func TestIcpServiceConcurrentRestartRejected(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	entered := make(chan struct{}, 1)
	gate := make(chan struct{})
	provider := &fakeProvider{name: "fake-slow", domainFn: func(_ context.Context, domain string) ([]models.ICPRecord, error) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-gate
		return fixedRecords(domain), nil
	}}
	svc := newService(provider)
	task := createTask(t, svc, uniqueName(t), models.IcpTaskTypeDomain, "slow.example.com")
	// 新建即开跑，provider 卡在 gate 上时任务一定处于 running —— 不需要也不能再 Restart
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatalf("provider 没有被调用")
	}

	// 运行中再 restart：必须被互斥挡住
	if _, err := svc.Restart(ctx, task.ID.Hex()); !errors.Is(err, icpsvc.ErrTaskBusy) {
		t.Fatalf("并发 restart 错误 = %v, want ErrTaskBusy", err)
	}

	close(gate)
	done := waitState(t, svc, task.ID.Hex(), models.IcpTaskStateDone)
	if done.Stats.Found != 3 {
		t.Fatalf("完成后 found = %d, want 3", done.Stats.Found)
	}
}
