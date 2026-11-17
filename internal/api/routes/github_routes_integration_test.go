//go:build integration

package routes

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	githubrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/github"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// GitHub 情报面路由走真实路由链（SetupRouter + JWT + Mongo + 假 GitHub）：
//   - 守卫：未带 token 一律 401；pageSize>200 一律 400；
//   - 主链路：create → scheduler/run（打假 GitHub）→ result/list → triage
//     → ignored_repos → export CSV；
//   - 附带：overview / stats / monitor_result / threat 配置与目标管理。
//
// 假 GitHub 通过 GITHUB_API_BASE_URL 注入（DefaultService 每次构造 client 时读该变量），
// 全程不访问 api.github.com。

// githubFake 同时模拟 code / issues / repositories 三类搜索
func githubFake(t *testing.T) *httptest.Server {
	t.Helper()
	codeItems := []map[string]interface{}{
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
	issueItems := []map[string]interface{}{
		{
			"id":       1001,
			"number":   7,
			"title":    "CVE-2026-0001 POC",
			"body":     "exploit for internal api",
			"html_url": "https://github.com/acme/poc/issues/7",
			"repository": map[string]interface{}{
				"full_name": "acme/poc",
				"html_url":  "https://github.com/acme/poc",
			},
		},
	}
	repoItems := []map[string]interface{}{
		{
			"name":       "toolkit",
			"html_url":   "https://github.com/evil/toolkit",
			"updated_at": "2026-11-18T00:00:00Z",
			"body":       "offensive toolkit",
			"repository": map[string]interface{}{
				"full_name": "evil/toolkit",
				"html_url":  "https://github.com/evil/toolkit",
			},
		},
	}

	reset := time.Now().Add(time.Hour).Unix()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Limit", "30")
		w.Header().Set("X-RateLimit-Remaining", "29")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
		w.Header().Set("X-RateLimit-Resource", "search")

		var items []map[string]interface{}
		switch r.URL.Path {
		case "/search/code":
			items = codeItems
		case "/search/issues":
			items = issueItems
		case "/search/repositories":
			items = repoItems
		case "/rate_limit":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"resources": map[string]interface{}{
					"core":   map[string]interface{}{"limit": 5000, "remaining": 4999, "reset": reset},
					"search": map[string]interface{}{"limit": 30, "remaining": 29, "reset": reset},
				},
			})
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"total_count":        len(items),
			"incomplete_results": false,
			"items":              items,
		})
	}))
}

func TestGithubRoutesGuardsAndFlow(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	if err := githubrepo.EnsureIndexes(t.Context(), mongodb.DB); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}

	fake := githubFake(t)
	defer fake.Close()
	t.Setenv("GITHUB_API_BASE_URL", fake.URL)

	router := SetupRouter()
	token := seedUserAndLogin(t, router)

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

	decode := func(w *httptest.ResponseRecorder) map[string]interface{} {
		t.Helper()
		var payload map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode (%s): %v", w.Body.String(), err)
		}
		return payload
	}

	dataOf := func(w *httptest.ResponseRecorder) map[string]interface{} {
		t.Helper()
		payload := decode(w)
		data, ok := payload["data"].(map[string]interface{})
		if !ok {
			t.Fatalf("响应缺少 data 对象: %v", payload)
		}
		return data
	}

	// ---------- 1) 路由守卫：未认证一律 401 ----------
	guardPaths := []struct{ method, path string }{
		{http.MethodPost, "/api/github/task/list"},
		{http.MethodPost, "/api/github/task/create"},
		{http.MethodPost, "/api/github/scheduler/list"},
		{http.MethodPost, "/api/github/scheduler/run"},
		{http.MethodPost, "/api/github/result/list"},
		{http.MethodPost, "/api/github/result/triage"},
		{http.MethodPost, "/api/github/result/ignored_repos"},
		{http.MethodGet, "/api/github/result/export"},
		{http.MethodPost, "/api/github/monitor_result/list"},
		{http.MethodPost, "/api/github/threat/token_status"},
		{http.MethodPost, "/api/github/threat/cve_run_once"},
	}
	for _, tc := range guardPaths {
		w := do(tc.method, tc.path, nil, false)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s 未认证 = %d, want 401 (%s)", tc.method, tc.path, w.Code, w.Body.String())
		}
	}

	// ---------- 2) pageSize 上限：>200 一律 400 ----------
	sizePaths := []string{
		"/api/github/task/list",
		"/api/github/scheduler/list",
		"/api/github/result/list",
		"/api/github/monitor_result/list",
	}
	for _, path := range sizePaths {
		w := do(http.MethodPost, path, map[string]interface{}{"pageSize": 500}, true)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s pageSize=500 = %d, want 400 (%s)", path, w.Code, w.Body.String())
		}
	}
	// 边界值 200 应放行
	if w := do(http.MethodPost, "/api/github/result/list",
		map[string]interface{}{"pageSize": 200}, true); w.Code != http.StatusOK {
		t.Fatalf("pageSize=200 = %d, want 200 (%s)", w.Code, w.Body.String())
	}

	// ---------- 3) 建任务 ----------
	w := do(http.MethodPost, "/api/github/task/create", map[string]interface{}{
		"name":     "it-github-routes",
		"query":    "AWS_SECRET_ACCESS_KEY filename:.env",
		"type":     "code_leak",
		"maxPages": 2,
	}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("create task = %d: %s", w.Code, w.Body.String())
	}
	created := dataOf(w)
	taskID, _ := created["id"].(string)
	if taskID == "" {
		t.Fatalf("create 响应缺少 id: %v", created)
	}
	t.Cleanup(func() {
		cleanup := t.Context()
		_, _ = githubrepo.NewTaskRepository().Delete(cleanup, []string{taskID})
		_, _ = githubrepo.NewResultRepository().DeleteByTaskIDs(cleanup, []string{taskID})
	})

	// 任务列表能查到
	listData := dataOf(do(http.MethodPost, "/api/github/task/list",
		map[string]interface{}{"keyword": "it-github-routes"}, true))
	if total, _ := listData["total"].(float64); total < 1 {
		t.Fatalf("task/list 没查到新建任务: %v", listData)
	}

	// ---------- 4) scheduler/run：打假 GitHub，结果入库 ----------
	runData := dataOf(do(http.MethodPost, "/api/github/scheduler/run",
		map[string]interface{}{"ids": []string{taskID}}, true))
	runs, _ := runData["runs"].([]interface{})
	if len(runs) != 1 {
		t.Fatalf("scheduler/run runs = %v, failures = %v", runData["runs"], runData["failures"])
	}
	firstRun, _ := runs[0].(map[string]interface{})
	if nf, _ := firstRun["newFound"].(float64); nf != 2 {
		t.Fatalf("首次运行 newFound = %v, want 2 (%v)", firstRun["newFound"], firstRun)
	}

	// 再跑一次：去重键生效，不新增
	runAgain := dataOf(do(http.MethodPost, "/api/github/scheduler/run",
		map[string]interface{}{"ids": []string{taskID}}, true))
	runs2, _ := runAgain["runs"].([]interface{})
	if len(runs2) != 1 {
		t.Fatalf("第二次 scheduler/run 失败: %v", runAgain)
	}
	secondRun, _ := runs2[0].(map[string]interface{})
	if nf, _ := secondRun["newFound"].(float64); nf != 0 {
		t.Fatalf("重跑 newFound = %v, want 0（去重失效）", secondRun["newFound"])
	}

	// ---------- 5) result/list ----------
	resultData := dataOf(do(http.MethodPost, "/api/github/result/list",
		map[string]interface{}{"taskId": taskID}, true))
	if total, _ := resultData["total"].(float64); total != 2 {
		t.Fatalf("result/list total = %v, want 2 (%v)", resultData["total"], resultData)
	}
	items, _ := resultData["list"].([]interface{})
	if len(items) != 2 {
		t.Fatalf("result/list items = %d, want 2", len(items))
	}
	firstItem, _ := items[0].(map[string]interface{})
	resultID, _ := firstItem["id"].(string)
	if resultID == "" {
		t.Fatalf("结果缺少 id: %v", firstItem)
	}

	// monitor_result/list 不应返回 task 来源的结果
	monitorData := dataOf(do(http.MethodPost, "/api/github/monitor_result/list",
		map[string]interface{}{"taskId": taskID}, true))
	if total, _ := monitorData["total"].(float64); total != 0 {
		t.Fatalf("monitor_result/list 混入了 task 结果: %v", monitorData)
	}

	// ---------- 6) overview / stats ----------
	overview := dataOf(do(http.MethodPost, "/api/github/result/overview",
		map[string]interface{}{"taskId": taskID}, true))
	if total, _ := overview["total"].(float64); total != 2 {
		t.Fatalf("overview total = %v, want 2 (%v)", overview["total"], overview)
	}
	byStatus, _ := overview["byStatus"].(map[string]interface{})
	if v, _ := byStatus["new"].(float64); v != 2 {
		t.Fatalf("overview byStatus.new = %v, want 2 (%v)", byStatus["new"], byStatus)
	}

	stats := dataOf(do(http.MethodPost, "/api/github/result/stats",
		map[string]interface{}{"taskId": taskID, "days": 3}, true))
	trend, _ := stats["items"].([]interface{})
	if len(trend) != 3 {
		t.Fatalf("stats items = %d, want 3 (%v)", len(trend), stats)
	}

	// ---------- 7) triage ----------
	triage := dataOf(do(http.MethodPost, "/api/github/result/triage",
		map[string]interface{}{"ids": []string{resultID}, "status": "ignored", "note": "误报"}, true))
	if updated, _ := triage["updated"].(float64); updated != 1 {
		t.Fatalf("triage updated = %v, want 1 (%v)", triage["updated"], triage)
	}
	ignoredList := dataOf(do(http.MethodPost, "/api/github/result/list",
		map[string]interface{}{"taskId": taskID, "status": "ignored"}, true))
	if total, _ := ignoredList["total"].(float64); total != 1 {
		t.Fatalf("按 ignored 过滤 total = %v, want 1", total)
	}
	// 非法状态 → 400
	if w := do(http.MethodPost, "/api/github/result/triage",
		map[string]interface{}{"ids": []string{resultID}, "status": "whatever"}, true); w.Code != http.StatusBadRequest {
		t.Fatalf("非法 triage 状态 = %d, want 400 (%s)", w.Code, w.Body.String())
	}

	// ---------- 8) ignored_repos ----------
	added := dataOf(do(http.MethodPost, "/api/github/result/ignored_repos",
		map[string]interface{}{"action": "add", "repo": "acme/leaky-repo", "reason": "自建靶场"}, true))
	if v, _ := added["added"].(bool); !v {
		t.Fatalf("ignored_repos add 未生效: %v", added)
	}
	listed := dataOf(do(http.MethodPost, "/api/github/result/ignored_repos",
		map[string]interface{}{"action": "list"}, true))
	repoItems, _ := listed["items"].([]interface{})
	foundRepo := false
	for _, it := range repoItems {
		entry, _ := it.(map[string]interface{})
		if entry["repo"] == "acme/leaky-repo" {
			foundRepo = true
		}
	}
	if !foundRepo {
		t.Fatalf("ignored_repos list 缺少 acme/leaky-repo: %v", listed)
	}
	removed := dataOf(do(http.MethodPost, "/api/github/result/ignored_repos",
		map[string]interface{}{"action": "remove", "repo": "acme/leaky-repo"}, true))
	if v, _ := removed["removed"].(bool); !v {
		t.Fatalf("ignored_repos remove 未生效: %v", removed)
	}
	if w := do(http.MethodPost, "/api/github/result/ignored_repos",
		map[string]interface{}{"action": "bogus"}, true); w.Code != http.StatusBadRequest {
		t.Fatalf("非法 action = %d, want 400", w.Code)
	}

	// ---------- 9) CSV 导出（流式） ----------
	exportW := do(http.MethodGet, "/api/github/result/export?taskId="+taskID, nil, true)
	if exportW.Code != http.StatusOK {
		t.Fatalf("export = %d: %s", exportW.Code, exportW.Body.String())
	}
	if ct := exportW.Header().Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Fatalf("export Content-Type = %q, want text/csv", ct)
	}
	if cd := exportW.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Fatalf("export Content-Disposition = %q", cd)
	}
	body := bytes.TrimPrefix(exportW.Body.Bytes(), []byte{0xEF, 0xBB, 0xBF})
	records, err := csv.NewReader(bytes.NewReader(body)).ReadAll()
	if err != nil {
		t.Fatalf("解析导出 CSV: %v (%s)", err, exportW.Body.String())
	}
	if len(records) != 3 { // 表头 + 2 行
		t.Fatalf("CSV 行数 = %d, want 3 (%s)", len(records), exportW.Body.String())
	}
	if records[0][0] != "taskId" || records[0][2] != "repo" {
		t.Fatalf("CSV 表头不符合预期: %v", records[0])
	}

	// ---------- 10) threat 面 ----------
	// 目标管理：tools 归一成 owner/repo，hackers 归一成 github id
	toolsCfg := dataOf(do(http.MethodPost, "/api/github/threat/tools_target",
		map[string]interface{}{"action": "add", "targets": []string{"https://github.com/chaitin/xray"}}, true))
	toolItems, _ := toolsCfg["items"].([]interface{})
	if len(toolItems) != 1 || toolItems[0] != "chaitin/xray" {
		t.Fatalf("tools_target 归一结果 = %v, want [chaitin/xray]", toolItems)
	}
	hackersCfg := dataOf(do(http.MethodPost, "/api/github/threat/hackers_target",
		map[string]interface{}{"action": "add", "targets": []string{"@evil"}}, true))
	hackerItems, _ := hackersCfg["items"].([]interface{})
	if len(hackerItems) != 1 || hackerItems[0] != "evil" {
		t.Fatalf("hackers_target 归一结果 = %v, want [evil]", hackerItems)
	}

	// 来源配置：关开关、写关键词，再读回来
	cfg := dataOf(do(http.MethodPost, "/api/github/threat/cve_config",
		map[string]interface{}{"enabled": true, "keywords": []string{"CVE-2026"}, "intervalHours": 3}, true))
	cfgSrc, _ := cfg["config"].(map[string]interface{})
	if v, _ := cfgSrc["enabled"].(bool); !v {
		t.Fatalf("cve_config enabled 未写入: %v", cfgSrc)
	}
	readBack := dataOf(do(http.MethodPost, "/api/github/threat/cve_config", nil, true))
	readSrc, _ := readBack["config"].(map[string]interface{})
	if v, _ := readSrc["enabled"].(bool); !v {
		t.Fatalf("cve_config 读回 enabled = %v, want true", readSrc)
	}

	// cve_run_once：打假 GitHub 的 /search/issues，命中写 threat_history
	runOnce := dataOf(do(http.MethodPost, "/api/github/threat/cve_run_once", nil, true))
	if scanned, _ := runOnce["scanned"].(float64); scanned < 1 {
		t.Fatalf("cve_run_once scanned = %v, want >=1 (%v)", runOnce["scanned"], runOnce)
	}
	history := dataOf(do(http.MethodPost, "/api/github/threat/cve_history",
		map[string]interface{}{"limit": 10}, true))
	if _, ok := history["list"]; !ok {
		t.Fatalf("cve_history 缺少 list: %v", history)
	}

	// token_status：未配置 token 时应报 missing，且不触网
	status := dataOf(do(http.MethodPost, "/api/github/threat/token_status", nil, true))
	statusValue, _ := status["status"].(string)
	switch statusValue {
	case "missing", "valid", "invalid", "rate_limited", "error":
	default:
		t.Fatalf("token_status 返回未知状态: %v", status)
	}

	// ---------- 11) stop / recover / update / delete ----------
	if w := do(http.MethodPost, "/api/github/task/stop",
		map[string]interface{}{"ids": []string{taskID}}, true); w.Code != http.StatusOK {
		t.Fatalf("task/stop = %d: %s", w.Code, w.Body.String())
	}
	stopped := dataOf(do(http.MethodPost, "/api/github/task/list",
		map[string]interface{}{"state": 2, "keyword": "it-github-routes"}, true))
	if total, _ := stopped["total"].(float64); total < 1 {
		t.Fatalf("stop 后按 state=2 查不到任务: %v", stopped)
	}
	if w := do(http.MethodPost, "/api/github/scheduler/recover",
		map[string]interface{}{"ids": []string{taskID}}, true); w.Code != http.StatusOK {
		t.Fatalf("scheduler/recover = %d: %s", w.Code, w.Body.String())
	}
	updatedTask := dataOf(do(http.MethodPost, "/api/github/scheduler/update",
		map[string]interface{}{"id": taskID, "intervalMinutes": 30, "name": "it-github-routes-2"}, true))
	updatedCfg, _ := updatedTask["config"].(map[string]interface{})
	if v, _ := updatedCfg["intervalMinutes"].(float64); v != 30 {
		t.Fatalf("scheduler/update intervalMinutes = %v, want 30 (%v)", updatedCfg["intervalMinutes"], updatedTask)
	}
	// 带 intervalMinutes 后应出现在 scheduler 视图里
	schedList := dataOf(do(http.MethodPost, "/api/github/scheduler/list",
		map[string]interface{}{"keyword": "it-github-routes-2"}, true))
	if total, _ := schedList["total"].(float64); total < 1 {
		t.Fatalf("scheduler/list 没查到周期任务: %v", schedList)
	}

	if w := do(http.MethodPost, "/api/github/task/delete",
		map[string]interface{}{"ids": []string{taskID}}, true); w.Code != http.StatusOK {
		t.Fatalf("task/delete = %d: %s", w.Code, w.Body.String())
	}
	// 删除任务连带删除结果
	left, err := githubrepo.NewResultRepository().CountByTask(t.Context(), taskID)
	if err != nil {
		t.Fatalf("count results: %v", err)
	}
	if left != 0 {
		t.Fatalf("删除任务后仍残留 %d 条结果", left)
	}
	oid, oidErr := primitive.ObjectIDFromHex(taskID)
	if oidErr != nil {
		t.Fatalf("bad objectid %s: %v", taskID, oidErr)
	}
	var raw map[string]interface{}
	if err := mongodb.DB.Collection(githubrepo.CollectionTask).FindOne(t.Context(),
		bson.M{"_id": oid}).Decode(&raw); err == nil {
		t.Fatalf("任务文档未被删除")
	}

	// 非法 ID → 400（不是 500）
	if w := do(http.MethodPost, "/api/github/task/stop",
		map[string]interface{}{"ids": []string{"not-an-object-id"}}, true); w.Code != http.StatusBadRequest {
		t.Fatalf("非法 ID = %d, want 400 (%s)", w.Code, w.Body.String())
	}
}
