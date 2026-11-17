//go:build integration

package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/database/redis"
	"go.mongodb.org/mongo-driver/bson"
)

// 批量导出三条路由要在真实路由链上验：鉴权挂没挂、CSV/zip 的响应头与内容、
// 未知 type/字段 400、任务不存在 404、预览 JSON 结构。
func TestExportBatchRoutes(t *testing.T) {
	if mongodb.DB == nil || redis.Client == nil {
		t.Skip("mongo/redis not initialized")
	}

	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	project := "proj-export-routes-" + suffix
	taskID := "taskid-routes-" + suffix
	taskName := "task-export-routes-" + suffix

	ctx := context.Background()
	subColl := mongodb.DB.Collection("subdomain")
	taskColl := mongodb.DB.Collection("task")
	t.Cleanup(func() {
		_, _ = subColl.DeleteMany(ctx, bson.M{"project": project})
		_, _ = taskColl.DeleteMany(ctx, bson.M{"id": taskID})
	})

	docs := make([]interface{}, 0, 3)
	for i := 0; i < 3; i++ {
		docs = append(docs, bson.M{
			"host":       fmt.Sprintf("er-%s-%d.example.com", suffix, i),
			"type":       "A",
			"value":      []string{fmt.Sprintf("198.18.41.%d", i+1)},
			"ip":         []string{fmt.Sprintf("198.18.41.%d", i+1)},
			"time":       fmt.Sprintf("2026-11-18 12:00:0%d", i+1),
			"project":    project,
			"taskName":   taskName,
			"rootDomain": "example.com",
			"tags":       []string{"中文标签"},
		})
	}
	if _, err := subColl.InsertMany(ctx, docs); err != nil {
		t.Fatalf("seed subdomain: %v", err)
	}
	if _, err := taskColl.InsertOne(ctx, bson.M{"id": taskID, "name": taskName}); err != nil {
		t.Fatalf("seed task: %v", err)
	}

	do := func(method, path string, body interface{}, withToken bool) *httptest.ResponseRecorder {
		t.Helper()
		var payload []byte
		if body != nil {
			var err error
			payload, err = json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
		}
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		if withToken {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		router.ServeHTTP(w, req)
		return w
	}

	// 1) 三条路由都必须挂 WithAuth
	for _, tc := range []struct {
		method string
		path   string
		body   interface{}
	}{
		{http.MethodPost, "/api/export/batch", map[string]interface{}{"type": "domain"}},
		{http.MethodGet, "/api/export/task/" + taskID, nil},
		{http.MethodPost, "/api/export/preview", map[string]interface{}{"type": "domain"}},
	} {
		w := do(tc.method, tc.path, tc.body, false)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s %s = %d, want 401: %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}

	// 2) 批量导出 CSV：BOM、Content-Disposition 文件名、行数、中文不乱码
	w := do(http.MethodPost, "/api/export/batch", map[string]interface{}{
		"type":    "domain",
		"project": project,
	}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("batch export = %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Errorf("content-type = %q, want text/csv", ct)
	}
	disposition := w.Header().Get("Content-Disposition")
	if matched, _ := regexp.MatchString(`^attachment; filename="domain-\d{14}\.csv"$`, disposition); !matched {
		t.Errorf("content-disposition = %q, want domain-<yyyymmddHHMMSS>.csv", disposition)
	}
	if got := w.Header().Get("X-Export-Rows"); got != "3" {
		t.Errorf("X-Export-Rows = %q, want 3", got)
	}
	body := w.Body.Bytes()
	if !bytes.HasPrefix(body, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatalf("csv missing utf-8 BOM: % x", body[:4])
	}
	lines := strings.Split(strings.TrimRight(string(body[3:]), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("csv lines = %d, want header+3: %v", len(lines), lines)
	}
	if !strings.HasPrefix(lines[0], "host,type,value,ip,time") {
		t.Errorf("header = %q, want 默认列顺序以 host,type,value,ip,time 开头", lines[0])
	}
	if !strings.Contains(string(body), "中文标签") {
		t.Errorf("csv does not contain chinese tag: %s", body)
	}

	// 3) 字段子集：只出请求的列
	w = do(http.MethodPost, "/api/export/batch", map[string]interface{}{
		"type":    "domain",
		"project": project,
		"fields":  []string{"host", "rootDomain"},
	}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("batch export(subset) = %d: %s", w.Code, w.Body.String())
	}
	subsetLines := strings.Split(strings.TrimRight(string(w.Body.Bytes()[3:]), "\n"), "\n")
	if subsetLines[0] != "host,rootDomain" {
		t.Errorf("subset header = %q, want host,rootDomain", subsetLines[0])
	}
	if len(subsetLines) != 4 {
		t.Errorf("subset lines = %d, want 4", len(subsetLines))
	}

	// 4) 未知 type / 未知字段 -> 400
	for _, tc := range []struct {
		name string
		body map[string]interface{}
		key  string
	}{
		{"unknown type", map[string]interface{}{"type": "nope"}, "unknown export type"},
		{"unknown field", map[string]interface{}{"type": "domain", "fields": []string{"nope"}}, "unknown export field"},
		{"bad time", map[string]interface{}{"type": "domain", "startTime": "2026/11/18"}, "invalid export request"},
	} {
		w := do(http.MethodPost, "/api/export/batch", tc.body, true)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d, want 400: %s", tc.name, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), tc.key) {
			t.Errorf("%s body = %s, want contain %q", tc.name, w.Body.String(), tc.key)
		}
	}

	// 5) 按任务导出：合法 zip，文件名带 taskId
	w = do(http.MethodGet, "/api/export/task/"+taskID, nil, true)
	if w.Code != http.StatusOK {
		t.Fatalf("task export = %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/zip") {
		t.Errorf("task content-type = %q, want application/zip", ct)
	}
	disposition = w.Header().Get("Content-Disposition")
	wantName := fmt.Sprintf(`attachment; filename="task-%s-\d{14}\.zip"`, regexp.QuoteMeta(taskID))
	if matched, _ := regexp.MatchString("^"+wantName+"$", disposition); !matched {
		t.Errorf("task content-disposition = %q, want task-%s-<ts>.zip", disposition, taskID)
	}
	if !strings.HasPrefix(w.Body.String(), "PK") {
		t.Errorf("task export body is not a zip")
	}
	if got := w.Header().Get("X-Export-Rows"); got != "3" {
		t.Errorf("task X-Export-Rows = %q, want 3（仅 subdomain 命中）", got)
	}

	// 6) 任务不存在 -> 404
	w = do(http.MethodGet, "/api/export/task/taskid-not-exist", nil, true)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing task = %d, want 404: %s", w.Code, w.Body.String())
	}

	// 7) 预览：total/rows/fields 齐全
	w = do(http.MethodPost, "/api/export/preview", map[string]interface{}{
		"type":    "domain",
		"project": project,
	}, true)
	if w.Code != http.StatusOK {
		t.Fatalf("preview = %d: %s", w.Code, w.Body.String())
	}
	var preview struct {
		Data struct {
			Type            string     `json:"type"`
			Collection      string     `json:"collection"`
			Fields          []string   `json:"fields"`
			AvailableFields []string   `json:"availableFields"`
			Rows            [][]string `json:"rows"`
			Total           int64      `json:"total"`
			Limit           int        `json:"limit"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatalf("decode preview: %v (%s)", err, w.Body.String())
	}
	if preview.Data.Collection != "subdomain" || preview.Data.Total != 3 {
		t.Errorf("preview = %+v, want subdomain/3", preview.Data)
	}
	if len(preview.Data.Rows) != 3 || len(preview.Data.Fields) == 0 {
		t.Errorf("preview rows/fields = %d/%v", len(preview.Data.Rows), preview.Data.Fields)
	}
	if preview.Data.Limit != 20 {
		t.Errorf("preview limit = %d, want 20", preview.Data.Limit)
	}
	for _, row := range preview.Data.Rows {
		if len(row) != len(preview.Data.Fields) {
			t.Errorf("preview row width = %d, want %d", len(row), len(preview.Data.Fields))
		}
	}
}
