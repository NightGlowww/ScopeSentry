//go:build integration

package routes

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// FOFA 批量建任务走真实路由链（SetupRouter + JWT + Mongo + 假 FOFA 服务）：
//   - 守卫：4 个端点未带 token 一律 401；search size>100 / 建任务 size>1000 / per-host>50 / pageSize>200 一律 400；
//   - provider：未配置 key → available=false + 明确原因；配置假服务 → 200 + 余额；
//   - 建任务：per-host 3 个 host → task 集合 3 条（每条 1 个目标）；single → 1 条任务 3 个目标；
//   - 去重：同 query 再导入 → skipped 计数，不重复建任务。
//
// 假 FOFA 通过系统配置 fofa.base_url + fofa.key 注入，全程不访问 fofa.info。

// fofaFake 假 FOFA：按 query 返回预置 host 列表，记录收到的 page/size
type fofaFake struct {
	mu      sync.Mutex
	hosts   map[string][]string
	calls   []fofaCall
	badKeys map[string]bool
}

type fofaCall struct {
	Query string
	Page  int
	Size  int
}

func (f *fofaFake) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/info/my":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error":             false,
				"username":          "it@example.com",
				"fofa_point":        1000,
				"remain_free_point": 9,
				"remain_api_query":  3,
				"vip_level":         2,
				"is_vip":            true,
			})
		case "/api/v1/search/all":
			decoded, _ := base64.StdEncoding.DecodeString(r.URL.Query().Get("qbase64"))
			query := string(decoded)
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			size, _ := strconv.Atoi(r.URL.Query().Get("size"))
			f.mu.Lock()
			f.calls = append(f.calls, fofaCall{Query: query, Page: page, Size: size})
			hosts := append([]string(nil), f.hosts[query]...)
			bad := f.badKeys[query]
			f.mu.Unlock()

			if bad {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"error":  true,
					"errmsg": "[-700] 账号无效",
				})
				return
			}
			start := (page - 1) * size
			if start > len(hosts) {
				start = len(hosts)
			}
			end := start + size
			if end > len(hosts) {
				end = len(hosts)
			}
			rows := make([][]string, 0, end-start)
			for _, host := range hosts[start:end] {
				rows = append(rows, []string{host})
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error":           false,
				"total":           len(hosts),
				"page":            page,
				"size":            len(rows),
				"results":         rows,
				"consumed_fpoint": 1,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func (f *fofaFake) setHosts(query string, hosts []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hosts[query] = hosts
}

func (f *fofaFake) callsFor(query string) []fofaCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fofaCall, 0)
	for _, call := range f.calls {
		if call.Query == query {
			out = append(out, call)
		}
	}
	return out
}

// fofaSetConfig 写入系统配置并在用例结束后还原（原来不存在就删掉）
func fofaSetConfig(t *testing.T, name, value string) {
	t.Helper()
	ctx := context.Background()
	collection := mongodb.DB.Collection("config")

	var prev struct {
		Value interface{} `bson:"value"`
	}
	found := collection.FindOne(ctx, bson.M{"name": name}).Decode(&prev) == nil
	if found {
		t.Cleanup(func() {
			_, _ = collection.UpdateOne(context.Background(), bson.M{"name": name}, bson.M{"$set": bson.M{"value": prev.Value}})
		})
	} else {
		t.Cleanup(func() {
			_, _ = collection.DeleteMany(context.Background(), bson.M{"name": name})
		})
	}
	if _, err := collection.UpdateOne(ctx, bson.M{"name": name}, bson.M{"$set": bson.M{
		"name": name, "value": value, "type": "fofa",
	}}, options.Update().SetUpsert(true)); err != nil {
		t.Fatalf("set config %s: %v", name, err)
	}
}

// fofaDeleteConfig 删除系统配置（并登记还原）
func fofaDeleteConfig(t *testing.T, name string) {
	t.Helper()
	ctx := context.Background()
	collection := mongodb.DB.Collection("config")
	var prev struct {
		Value interface{} `bson:"value"`
	}
	if collection.FindOne(ctx, bson.M{"name": name}).Decode(&prev) == nil {
		t.Cleanup(func() {
			_, _ = collection.UpdateOne(context.Background(), bson.M{"name": name}, bson.M{"$set": bson.M{"value": prev.Value}})
		})
	}
	if _, err := collection.DeleteMany(ctx, bson.M{"name": name}); err != nil {
		t.Fatalf("delete config %s: %v", name, err)
	}
}

func TestFofaRoutesGuardsAndImportFlow(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	if err := mongodb.EnsureIndexes(mongodb.DB); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	q1 := "fofa-it-" + suffix + "-q1"
	q2 := "fofa-it-" + suffix + "-q2"
	qBulk := "fofa-it-" + suffix + "-bulk"
	qBad := "fofa-it-" + suffix + "-bad"

	q1Raw := []string{
		"http://A-" + suffix + ".example.com:8443/path?x=1",
		"https://b-" + suffix + ".example.com",
		"1.2.3.4:8080",
		"d-" + suffix + ".example.com.",
		"http://user:pw@e-" + suffix + ".example.com:443/x",
	}
	q1Want := []string{
		"a-" + suffix + ".example.com:8443",
		"b-" + suffix + ".example.com",
		"1.2.3.4:8080",
		"d-" + suffix + ".example.com",
		"e-" + suffix + ".example.com:443",
	}
	q2Raw := []string{
		"http://f-" + suffix + ".example.com:8080/admin",
		"g-" + suffix + ".example.com",
		"10.0.1.5",
	}
	q2Want := []string{
		"f-" + suffix + ".example.com:8080",
		"g-" + suffix + ".example.com",
		"10.0.1.5",
	}
	bulkHosts := make([]string, 0, 60)
	for i := 0; i < 60; i++ {
		bulkHosts = append(bulkHosts, fmt.Sprintf("bulk%d-%s.example.com", i, suffix))
	}

	fake := &fofaFake{
		hosts: map[string][]string{
			q1:    q1Raw,
			q2:    q2Raw,
			qBulk: bulkHosts,
			qBad:  {"bad.example.com"},
		},
		badKeys: map[string]bool{qBad: true},
	}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	// provider 配置：先制造「未配置 key」再写入 key + 假服务地址
	fofaDeleteConfig(t, "fofa.key")
	fofaSetConfig(t, "fofa.base_url", server.URL)

	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	namePrefix := "fofa-it-" + suffix
	t.Cleanup(func() {
		ctx := context.Background()
		// 只清理本次用例建的任务与导入记录
		_, _ = mongodb.DB.Collection("task").DeleteMany(ctx, bson.M{"name": bson.M{"$regex": "^" + namePrefix}})
		_, _ = mongodb.DB.Collection("fofa_import").DeleteMany(ctx, bson.M{"query": bson.M{"$regex": "^" + namePrefix}})
	})

	do := func(method, path string, body interface{}, withAuth bool) *httptest.ResponseRecorder {
		t.Helper()
		var reader *bytes.Reader
		if body != nil {
			payload, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			reader = bytes.NewReader(payload)
		} else {
			reader = bytes.NewReader(nil)
		}
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, reader)
		req.Header.Set("Content-Type", "application/json")
		if withAuth {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		router.ServeHTTP(w, req)
		return w
	}

	dataOf := func(w *httptest.ResponseRecorder) map[string]interface{} {
		t.Helper()
		var payload map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode (%s): %v", w.Body.String(), err)
		}
		data, ok := payload["data"].(map[string]interface{})
		if !ok {
			t.Fatalf("响应缺少 data 对象: %v", payload)
		}
		return data
	}

	stringsOf := func(value interface{}) []string {
		t.Helper()
		raw, ok := value.([]interface{})
		if !ok {
			t.Fatalf("不是数组: %v", value)
		}
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			text, _ := item.(string)
			out = append(out, text)
		}
		return out
	}

	numberOf := func(data map[string]interface{}, key string) int {
		t.Helper()
		value, ok := data[key].(float64)
		if !ok {
			t.Fatalf("%s 不是数字: %v", key, data[key])
		}
		return int(value)
	}

	// ---------- 1) 未认证一律 401 ----------
	for _, path := range []string{"/api/fofa/status", "/api/fofa/search", "/api/fofa/task", "/api/fofa/task/list"} {
		if w := do(http.MethodPost, path, nil, false); w.Code != http.StatusUnauthorized {
			t.Fatalf("%s 未认证 = %d, want 401 (%s)", path, w.Code, w.Body.String())
		}
	}

	// ---------- 2) 未配置 key → available=false + reason ----------
	status := dataOf(do(http.MethodPost, "/api/fofa/status", nil, true))
	if status["provider"] != models.FofaProviderNone {
		t.Fatalf("未配置时 provider = %v, want none (%v)", status["provider"], status)
	}
	if available, _ := status["available"].(bool); available {
		t.Fatalf("未配置时 available = true, want false")
	}
	if reason, _ := status["reason"].(string); !strings.Contains(reason, "fofa.key") {
		t.Fatalf("未配置时 reason = %q, want 提到 fofa.key", reason)
	}
	if candidates, _ := status["candidates"].([]interface{}); len(candidates) != 2 {
		t.Fatalf("candidates = %v, want 2 个内置 provider", status["candidates"])
	}

	// ---------- 3) 配置假服务 → 200 + 余额 ----------
	fofaSetConfig(t, "fofa.key", "it-key-"+suffix)
	status = dataOf(do(http.MethodPost, "/api/fofa/status", nil, true))
	if status["provider"] != models.FofaProviderHTTP {
		t.Fatalf("配置后 provider = %v, want http (%v)", status["provider"], status)
	}
	if available, _ := status["available"].(bool); !available {
		t.Fatalf("配置后 available = %v, want true (%v)", status["available"], status)
	}
	if configured, _ := status["configured"].(bool); !configured {
		t.Fatalf("配置后 configured = %v, want true", status["configured"])
	}
	balance, _ := status["balance"].(map[string]interface{})
	if balance == nil {
		t.Fatalf("status 缺少 balance: %v", status)
	}
	if point, _ := balance["fofaPoint"].(float64); point != 1000 {
		t.Fatalf("balance.fofaPoint = %v, want 1000", balance["fofaPoint"])
	}

	// ---------- 4) search 分页 / size 上限 / provider 错误 ----------
	preview := dataOf(do(http.MethodPost, "/api/fofa/search", map[string]interface{}{
		"query": q1, "page": 1, "size": 2,
	}, true))
	previewHosts := stringsOf(preview["hosts"])
	if len(previewHosts) != 2 || previewHosts[0] != q1Want[0] || previewHosts[1] != q1Want[1] {
		t.Fatalf("search page1 = %v, want 前两条 %v", previewHosts, q1Want[:2])
	}
	if page := numberOf(preview, "page"); page != 1 {
		t.Fatalf("search page = %d, want 1", page)
	}
	if total := numberOf(preview, "total"); total != len(q1Raw) {
		t.Fatalf("search total = %d, want %d", total, len(q1Raw))
	}
	calls := fake.callsFor(q1)
	if len(calls) != 1 || calls[0].Page != 1 || calls[0].Size != 2 {
		t.Fatalf("provider 收到调用 %+v, want page=1 size=2", calls)
	}

	secondPage := dataOf(do(http.MethodPost, "/api/fofa/search", map[string]interface{}{
		"query": q1, "page": 2, "size": 2,
	}, true))
	secondHosts := stringsOf(secondPage["hosts"])
	if len(secondHosts) != 2 || secondHosts[0] != q1Want[2] || secondHosts[1] != q1Want[3] {
		t.Fatalf("search page2 = %v, want %v", secondHosts, q1Want[2:4])
	}

	if w := do(http.MethodPost, "/api/fofa/search", map[string]interface{}{
		"query": q1, "size": models.FofaMaxPreviewSize + 1,
	}, true); w.Code != http.StatusBadRequest {
		t.Fatalf("search size=101 = %d, want 400 (%s)", w.Code, w.Body.String())
	}
	if w := do(http.MethodPost, "/api/fofa/search", map[string]interface{}{}, true); w.Code != http.StatusBadRequest {
		t.Fatalf("search 缺 query = %d, want 400 (%s)", w.Code, w.Body.String())
	}
	if w := do(http.MethodPost, "/api/fofa/search", map[string]interface{}{"query": qBad}, true); w.Code != http.StatusBadRequest {
		t.Fatalf("search provider 业务错误 = %d, want 400 (%s)", w.Code, w.Body.String())
	}

	// ---------- 5) per-host：3 个 host → 3 个任务 ----------
	perHost := dataOf(do(http.MethodPost, "/api/fofa/task", map[string]interface{}{
		"query":      q1,
		"size":       3,
		"taskName":   namePrefix,
		"project":    "0123456789abcdef01234567",
		"createMode": models.FofaCreateModePerHost,
	}, true))
	if got := numberOf(perHost, "created"); got != 3 {
		t.Fatalf("per-host created = %d, want 3 (%v)", got, perHost)
	}
	if got := numberOf(perHost, "skipped"); got != 0 {
		t.Fatalf("per-host skipped = %d, want 0", got)
	}
	if got := numberOf(perHost, "failed"); got != 0 {
		t.Fatalf("per-host failed = %d, want 0 (%v)", got, perHost["errors"])
	}
	hosts := stringsOf(perHost["hosts"])
	if len(hosts) != 3 || hosts[0] != q1Want[0] || hosts[2] != q1Want[2] {
		t.Fatalf("per-host hosts = %v, want 前三条 %v", hosts, q1Want[:3])
	}
	taskIDs := stringsOf(perHost["taskIds"])
	if len(taskIDs) != 3 {
		t.Fatalf("per-host taskIds = %v, want 3", taskIDs)
	}
	if importID, _ := perHost["importId"].(string); importID == "" {
		t.Fatalf("per-host 缺 importId: %v", perHost)
	}

	var taskDocs []models.Task
	cursor, err := mongodb.DB.Collection("task").Find(context.Background(), bson.M{
		"name": bson.M{"$regex": "^" + namePrefix + "-"},
	})
	if err != nil {
		t.Fatalf("find tasks: %v", err)
	}
	if err := cursor.All(context.Background(), &taskDocs); err != nil {
		t.Fatalf("decode tasks: %v", err)
	}
	if len(taskDocs) != 3 {
		t.Fatalf("task 集合 = %d 条, want 3", len(taskDocs))
	}
	seenTargets := map[string]int{}
	for _, doc := range taskDocs {
		seenTargets[doc.Target]++
		if doc.TaskNum != 1 {
			t.Fatalf("任务 %s taskNum = %d, want 1", doc.Name, doc.TaskNum)
		}
		if doc.TargetSource != "general" {
			t.Fatalf("任务 %s targetSource = %q, want general", doc.Name, doc.TargetSource)
		}
		if len(doc.Project) != 1 || doc.Project[0] != "0123456789abcdef01234567" {
			t.Fatalf("任务 %s project = %v, want 绑定项目", doc.Name, doc.Project)
		}
		if doc.BindProject != "0123456789abcdef01234567" {
			t.Fatalf("任务 %s bindProject = %q", doc.Name, doc.BindProject)
		}
	}
	for _, want := range q1Want[:3] {
		if seenTargets[want] != 1 {
			t.Fatalf("target %q 出现 %d 次, want 1（%v）", want, seenTargets[want], seenTargets)
		}
	}

	// ---------- 6) 同 query 再导入 → 全部 skipped ----------
	repeat := dataOf(do(http.MethodPost, "/api/fofa/task", map[string]interface{}{
		"query":      q1,
		"size":       3,
		"taskName":   namePrefix + "-repeat",
		"createMode": models.FofaCreateModePerHost,
	}, true))
	if got := numberOf(repeat, "created"); got != 0 {
		t.Fatalf("重复导入 created = %d, want 0", got)
	}
	if got := numberOf(repeat, "skipped"); got != 3 {
		t.Fatalf("重复导入 skipped = %d, want 3 (%v)", got, repeat)
	}
	var count int64
	count, err = mongodb.DB.Collection("task").CountDocuments(context.Background(), bson.M{
		"name": bson.M{"$regex": "^" + namePrefix},
	})
	if err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count != 3 {
		t.Fatalf("重复导入后 task = %d 条, want 3（去重失效）", count)
	}

	// ---------- 7) single：1 个任务 3 个目标 ----------
	single := dataOf(do(http.MethodPost, "/api/fofa/task", map[string]interface{}{
		"query":      q2,
		"size":       3,
		"taskName":   namePrefix + "-single",
		"createMode": models.FofaCreateModeSingle,
	}, true))
	if got := numberOf(single, "created"); got != 1 {
		t.Fatalf("single created = %d, want 1 (%v)", got, single)
	}
	if got := numberOf(single, "failed"); got != 0 {
		t.Fatalf("single failed = %d, want 0 (%v)", got, single["errors"])
	}
	if ids := stringsOf(single["taskIds"]); len(ids) != 1 {
		t.Fatalf("single taskIds = %v, want 1", ids)
	}

	var singleTask models.Task
	if err := mongodb.DB.Collection("task").FindOne(context.Background(), bson.M{
		"name": namePrefix + "-single",
	}).Decode(&singleTask); err != nil {
		t.Fatalf("load single task: %v", err)
	}
	targets := strings.Split(singleTask.Target, "\n")
	if len(targets) != 3 {
		t.Fatalf("single 任务目标 = %d 条, want 3 (%q)", len(targets), singleTask.Target)
	}
	for i, want := range q2Want {
		if targets[i] != want {
			t.Fatalf("single 目标[%d] = %q, want %q（保序）", i, targets[i], want)
		}
	}
	if singleTask.TaskNum != 3 {
		t.Fatalf("single taskNum = %d, want 3", singleTask.TaskNum)
	}

	// ---------- 7b) 单条失败：同名任务已存在 → failed + errors[] ----------
	q3 := "fofa-it-" + suffix + "-q3"
	q3Host := "dup-" + suffix + ".example.com"
	fake.setHosts(q3, []string{"https://" + q3Host})
	dupName := "fofa-dup-" + suffix
	dupTaskName := dupName + "-" + q3Host
	if _, err := mongodb.DB.Collection("task").InsertOne(context.Background(), bson.M{
		"name":      dupTaskName,
		"target":    q3Host,
		"taskNum":   1,
		"status":    0,
		"creatTime": "-",
	}); err != nil {
		t.Fatalf("seed dup task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = mongodb.DB.Collection("task").DeleteMany(context.Background(), bson.M{"name": bson.M{"$regex": "^" + dupName}})
	})
	failed := dataOf(do(http.MethodPost, "/api/fofa/task", map[string]interface{}{
		"query":      q3,
		"size":       1,
		"taskName":   dupName,
		"createMode": models.FofaCreateModePerHost,
	}, true))
	if got := numberOf(failed, "created"); got != 0 {
		t.Fatalf("同名任务 created = %d, want 0", got)
	}
	if got := numberOf(failed, "failed"); got != 1 {
		t.Fatalf("同名任务 failed = %d, want 1 (%v)", got, failed)
	}
	errs, _ := failed["errors"].([]interface{})
	if len(errs) != 1 {
		t.Fatalf("同名任务 errors = %v, want 1 条", failed["errors"])
	}
	firstErr, _ := errs[0].(map[string]interface{})
	if host, _ := firstErr["host"].(string); host != q3Host {
		t.Fatalf("errors[0].host = %v, want %q", firstErr["host"], q3Host)
	}
	if msg, _ := firstErr["error"].(string); !strings.Contains(msg, "task name already exists") {
		t.Fatalf("errors[0].error = %q, want 含 task name already exists", msg)
	}

	// 失败不写入去重基准：删掉冲突任务后重试，同一个 host 应该能建出来
	if _, err := mongodb.DB.Collection("task").DeleteMany(context.Background(), bson.M{"name": dupTaskName}); err != nil {
		t.Fatalf("remove dup task: %v", err)
	}
	retry := dataOf(do(http.MethodPost, "/api/fofa/task", map[string]interface{}{
		"query":      q3,
		"size":       1,
		"taskName":   dupName,
		"createMode": models.FofaCreateModePerHost,
	}, true))
	if got := numberOf(retry, "created"); got != 1 {
		t.Fatalf("失败后重试 created = %d, want 1（失败的 host 不应进入去重基准，%v）", got, retry)
	}

	// ---------- 8) 上限 / 参数校验 ----------
	if w := do(http.MethodPost, "/api/fofa/task", map[string]interface{}{
		"query": q1, "size": models.FofaMaxImportSize + 1, "taskName": namePrefix, "createMode": models.FofaCreateModeSingle,
	}, true); w.Code != http.StatusBadRequest {
		t.Fatalf("size=1001 = %d, want 400 (%s)", w.Code, w.Body.String())
	}
	if w := do(http.MethodPost, "/api/fofa/task", map[string]interface{}{
		"query": qBulk, "size": 60, "taskName": namePrefix + "-bulk", "createMode": models.FofaCreateModePerHost,
	}, true); w.Code != http.StatusBadRequest {
		t.Fatalf("per-host 60 hosts = %d, want 400 (%s)", w.Code, w.Body.String())
	}
	if w := do(http.MethodPost, "/api/fofa/task", map[string]interface{}{
		"query": q1, "taskName": namePrefix, "createMode": "nope",
	}, true); w.Code != http.StatusBadRequest {
		t.Fatalf("非法 createMode = %d, want 400 (%s)", w.Code, w.Body.String())
	}
	if w := do(http.MethodPost, "/api/fofa/task", map[string]interface{}{
		"query": q1, "createMode": models.FofaCreateModeSingle,
	}, true); w.Code != http.StatusBadRequest {
		t.Fatalf("缺 taskName = %d, want 400 (%s)", w.Code, w.Body.String())
	}

	// ---------- 9) 导入历史 ----------
	if w := do(http.MethodPost, "/api/fofa/task/list", map[string]interface{}{"pageSize": 500}, true); w.Code != http.StatusBadRequest {
		t.Fatalf("pageSize=500 = %d, want 400 (%s)", w.Code, w.Body.String())
	}
	history := dataOf(do(http.MethodPost, "/api/fofa/task/list", map[string]interface{}{
		"query": namePrefix, "pageSize": 200,
	}, true))
	list, _ := history["list"].([]interface{})
	if len(list) < 2 {
		t.Fatalf("导入历史 = %d 条, want >=2 (%v)", len(list), history)
	}
	recordsByQuery := map[string]map[string]interface{}{}
	for _, raw := range list {
		item, _ := raw.(map[string]interface{})
		query, _ := item["query"].(string)
		if _, exists := recordsByQuery[query]; !exists {
			recordsByQuery[query] = item
		}
	}
	repeatRecord, ok := recordsByQuery[q1]
	if !ok {
		t.Fatalf("导入历史缺少 q1 记录: %v", recordsByQuery)
	}
	if skipped, _ := repeatRecord["skipped"].(float64); int(skipped) != 3 {
		t.Fatalf("q1 最近一次导入 skipped = %v, want 3", repeatRecord["skipped"])
	}
	singleRecord, ok := recordsByQuery[q2]
	if !ok {
		t.Fatalf("导入历史缺少 q2 记录: %v", recordsByQuery)
	}
	if created, _ := singleRecord["created"].(float64); int(created) != 1 {
		t.Fatalf("q2 导入 created = %v, want 1", singleRecord["created"])
	}
	if mode, _ := singleRecord["createMode"].(string); mode != models.FofaCreateModeSingle {
		t.Fatalf("q2 createMode = %v, want single", singleRecord["createMode"])
	}
	if hostsRaw, _ := singleRecord["hosts"].([]interface{}); len(hostsRaw) != 3 {
		t.Fatalf("q2 导入 hosts = %v, want 3", singleRecord["hosts"])
	}
}
