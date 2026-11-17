//go:build integration

package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/database/redis"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 字典对标接口（ARL-Next dictionary/brute/cdn）走真实路由链：
// create → list → preview → search → append（重复行不增加）→ delete_entries
// → download（内容一致）→ delete_file，外加分页上限 / 路径穿越 / 大文件上传。
func TestDictionaryParityRoutes(t *testing.T) {
	if mongodb.DB == nil || redis.Client == nil {
		t.Skip("mongo/redis not initialized")
	}
	if err := mongodb.EnsureIndexes(mongodb.DB); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}

	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	prefix := fmt.Sprintf("dict-parity-%d", time.Now().UnixNano())
	dirName := prefix + "-dir"
	uploadName := prefix + "-upload"
	t.Cleanup(func() { cleanupDictionaries(t, prefix) })

	do := func(method, path string, body interface{}) *httptest.ResponseRecorder {
		t.Helper()
		var reader *bytes.Reader
		if body != nil {
			payload, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}
			reader = bytes.NewReader(payload)
		} else {
			reader = bytes.NewReader(nil)
		}
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, reader)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)
		return w
	}

	decode := func(w *httptest.ResponseRecorder) map[string]interface{} {
		t.Helper()
		var payload map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode %s: %v", w.Body.String(), err)
		}
		return payload
	}

	dataOf := func(payload map[string]interface{}) map[string]interface{} {
		t.Helper()
		data, ok := payload["data"].(map[string]interface{})
		if !ok {
			t.Fatalf("response missing data object: %v", payload)
		}
		return data
	}

	// 1) create：内容里带重复行与空行，入库应当去重
	content := "admin\nadmin\n\nroot\n/test/login\n"
	w := do(http.MethodPost, "/api/dictionary/create", map[string]interface{}{
		"kind": "dir", "name": dirName, "content": content,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	created := dataOf(decode(w))
	if got := int(created["lineCount"].(float64)); got != 3 {
		t.Fatalf("create lineCount = %d, want 3（去重后）", got)
	}
	dictID, _ := created["id"].(string)
	if dictID == "" {
		t.Fatalf("create 未返回 id: %v", created)
	}

	// 重复 create 应当 400
	if w = do(http.MethodPost, "/api/dictionary/create", map[string]interface{}{
		"kind": "dir", "name": dirName, "content": "x",
	}); w.Code != http.StatusBadRequest {
		t.Fatalf("重复 create = %d, want 400: %s", w.Code, w.Body.String())
	}

	// 2) list：按 kind + search 命中，kind 面板里能看到 dir
	w = do(http.MethodPost, "/api/dictionary/list", map[string]interface{}{
		"kind": "dir", "search": prefix, "pageIndex": 1, "pageSize": 20,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("list = %d: %s", w.Code, w.Body.String())
	}
	listData := dataOf(decode(w))
	if total := int(listData["total"].(float64)); total < 1 {
		t.Fatalf("list total = %d, want >= 1", total)
	}
	found := false
	for _, raw := range listData["list"].([]interface{}) {
		item := raw.(map[string]interface{})
		if item["name"] == dirName {
			found = true
			if item["kind"] != "dir" {
				t.Fatalf("list item kind = %v, want dir", item["kind"])
			}
			if int(item["lineCount"].(float64)) != 3 {
				t.Fatalf("list item lineCount = %v, want 3", item["lineCount"])
			}
		}
	}
	if !found {
		t.Fatalf("list 未返回刚创建的字典: %v", listData["list"])
	}
	kindsSeen := map[string]bool{}
	for _, raw := range listData["kinds"].([]interface{}) {
		kindsSeen[raw.(map[string]interface{})["kind"].(string)] = true
	}
	if !kindsSeen["dir"] || !kindsSeen["brute"] || !kindsSeen["cdn"] {
		t.Fatalf("kind 面板未覆盖规范 kind: %v", kindsSeen)
	}

	// 分页上限 200：201 必须 400
	if w = do(http.MethodPost, "/api/dictionary/list", map[string]interface{}{
		"kind": "dir", "pageIndex": 1, "pageSize": 201,
	}); w.Code != http.StatusBadRequest {
		t.Fatalf("pageSize=201 = %d, want 400: %s", w.Code, w.Body.String())
	}

	// 3) preview：行内容与顺序一致
	w = do(http.MethodPost, "/api/dictionary/preview", map[string]interface{}{
		"kind": "dir", "name": dirName, "limit": 200,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("preview = %d: %s", w.Code, w.Body.String())
	}
	previewData := dataOf(decode(w))
	lines := toStrings(previewData["lines"])
	wantLines := []string{"admin", "root", "/test/login"}
	if len(lines) != len(wantLines) {
		t.Fatalf("preview lines = %v, want %v", lines, wantLines)
	}
	for i := range wantLines {
		if lines[i] != wantLines[i] {
			t.Fatalf("preview lines[%d] = %q, want %q", i, lines[i], wantLines[i])
		}
	}

	// 4) search：子串命中
	w = do(http.MethodPost, "/api/dictionary/search", map[string]interface{}{
		"kind": "dir", "name": dirName, "keyword": "log", "limit": 50,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("search = %d: %s", w.Code, w.Body.String())
	}
	searchData := dataOf(decode(w))
	if got := int(searchData["returned"].(float64)); got != 1 {
		t.Fatalf("search returned = %d, want 1: %v", got, searchData)
	}

	// 5) append：一条重复一条新增 → added=1 duplicated=1
	w = do(http.MethodPost, "/api/dictionary/append", map[string]interface{}{
		"kind": "dir", "name": dirName, "lines": []string{"admin", "newentry", "newentry"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("append = %d: %s", w.Code, w.Body.String())
	}
	appendData := dataOf(decode(w))
	if added := int(appendData["added"].(float64)); added != 1 {
		t.Fatalf("append added = %d, want 1（重复行不增加）: %v", added, appendData)
	}
	if total := int(appendData["total"].(float64)); total != 4 {
		t.Fatalf("append total = %d, want 4", total)
	}

	// 6) delete_entries
	w = do(http.MethodPost, "/api/dictionary/delete_entries", map[string]interface{}{
		"kind": "dir", "name": dirName, "entries": []string{"newentry"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("delete_entries = %d: %s", w.Code, w.Body.String())
	}
	deleteData := dataOf(decode(w))
	if deleted := int(deleteData["deleted"].(float64)); deleted != 1 {
		t.Fatalf("delete_entries deleted = %d, want 1", deleted)
	}
	if total := int(deleteData["total"].(float64)); total != 3 {
		t.Fatalf("delete_entries total = %d, want 3", total)
	}

	// 7) download：内容与预览一致
	w = do(http.MethodGet, "/api/dictionary/download?kind=dir&name="+dirName, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("download = %d: %s", w.Code, w.Body.String())
	}
	downloaded := w.Body.String()
	if downloaded != "admin\nroot\n/test/login\n" {
		t.Fatalf("download 内容不一致: %q", downloaded)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "dir_") {
		t.Fatalf("download Content-Disposition = %q", cd)
	}

	// 7.1) 空字典：create 不带 content，下载应当 200 + 空内容（历史语义）
	emptyName := prefix + "-empty"
	w = do(http.MethodPost, "/api/dictionary/create", map[string]interface{}{
		"kind": "other", "name": emptyName,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("create 空字典 = %d: %s", w.Code, w.Body.String())
	}
	if got := int(dataOf(decode(w))["lineCount"].(float64)); got != 0 {
		t.Fatalf("空字典 lineCount = %d, want 0", got)
	}
	w = do(http.MethodGet, "/api/dictionary/download?kind=other&name="+emptyName, nil)
	if w.Code != http.StatusOK || w.Body.Len() != 0 {
		t.Fatalf("空字典 download = %d %q", w.Code, w.Body.String())
	}
	w = do(http.MethodPost, "/api/dictionary/preview", map[string]interface{}{
		"kind": "other", "name": emptyName, "limit": 10,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("空字典 preview = %d: %s", w.Code, w.Body.String())
	}
	if got := len(toStrings(dataOf(decode(w))["lines"])); got != 0 {
		t.Fatalf("空字典 preview 行数 = %d, want 0", got)
	}

	// 7.2) 每次写操作都会重建内容文件，旧版本必须清掉（否则大字典会越写越胖）
	n, err := mongodb.DB.Collection("fs.files").CountDocuments(context.Background(), bson.M{"filename": dictID})
	if err != nil {
		t.Fatalf("统计 fs.files 失败: %v", err)
	}
	if n > 1 {
		t.Fatalf("fs.files 残留 %d 个版本，应当只有 1 个", n)
	}

	// 8) 路径穿越名字必须 400

	if w = do(http.MethodPost, "/api/dictionary/create", map[string]interface{}{
		"kind": "dir", "name": "../evil", "content": "x",
	}); w.Code != http.StatusBadRequest {
		t.Fatalf("路径穿越 create = %d, want 400: %s", w.Code, w.Body.String())
	}

	// 9) dict_cdn / dict_brute 的 list / save
	if w = do(http.MethodPost, "/api/dict_cdn/save", map[string]interface{}{
		"name": prefix + "-cdn", "content": "akamai\ncloudflare\n",
	}); w.Code != http.StatusOK {
		t.Fatalf("dict_cdn/save = %d: %s", w.Code, w.Body.String())
	}
	if w = do(http.MethodPost, "/api/dict_brute/save", map[string]interface{}{
		"name": prefix + "-brute", "lines": []string{"admin", "admin", "root"},
	}); w.Code != http.StatusOK {
		t.Fatalf("dict_brute/save = %d: %s", w.Code, w.Body.String())
	}
	w = do(http.MethodPost, "/api/dict_cdn/list", map[string]interface{}{"search": prefix})
	if w.Code != http.StatusOK {
		t.Fatalf("dict_cdn/list = %d: %s", w.Code, w.Body.String())
	}
	cdnList := dataOf(decode(w))
	if total := int(cdnList["total"].(float64)); total != 1 {
		t.Fatalf("dict_cdn/list total = %d, want 1: %v", total, cdnList)
	}
	w = do(http.MethodPost, "/api/dict_brute/list", map[string]interface{}{"search": prefix})
	if w.Code != http.StatusOK {
		t.Fatalf("dict_brute/list = %d: %s", w.Code, w.Body.String())
	}
	bruteList := dataOf(decode(w))
	for _, raw := range bruteList["list"].([]interface{}) {
		item := raw.(map[string]interface{})
		if item["name"] == prefix+"-brute" && int(item["lineCount"].(float64)) != 2 {
			t.Fatalf("brute 字典未去重: %v", item)
		}
	}

	// 10) delete_file：删掉后 preview 变 404，list 不再返回
	if w = do(http.MethodPost, "/api/dictionary/delete_file", map[string]interface{}{
		"kind": "dir", "name": dirName,
	}); w.Code != http.StatusOK {
		t.Fatalf("delete_file = %d: %s", w.Code, w.Body.String())
	}
	if w = do(http.MethodPost, "/api/dictionary/preview", map[string]interface{}{
		"kind": "dir", "name": dirName, "limit": 10,
	}); w.Code != http.StatusNotFound {
		t.Fatalf("删除后 preview = %d, want 404: %s", w.Code, w.Body.String())
	}
	w = do(http.MethodPost, "/api/dictionary/list", map[string]interface{}{
		"kind": "dir", "search": dirName, "pageIndex": 1, "pageSize": 20,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("删除后 list = %d: %s", w.Code, w.Body.String())
	}
	if total := int(dataOf(decode(w))["total"].(float64)); total != 0 {
		t.Fatalf("删除后 list total = %d, want 0", total)
	}

	// 11) 大文件上传：5 万行 → 轮询到 done → preview 上限 200 行、total=50000
	content50k := buildLines(50000)
	w = uploadDictionary(t, router, token, "dir", uploadName, "create", content50k)
	if w.Code != http.StatusOK {
		t.Fatalf("upload_large = %d: %s", w.Code, w.Body.String())
	}
	task := dataOf(decode(w))
	taskID, _ := task["taskId"].(string)
	if taskID == "" {
		t.Fatalf("upload_large 未返回 taskId: %v", task)
	}
	if got := int(task["totalLines"].(float64)); got != 50000 {
		t.Fatalf("upload totalLines = %d, want 50000", got)
	}

	deadline := time.Now().Add(120 * time.Second)
	finalStatus := ""
	var finalTask map[string]interface{}
	for time.Now().Before(deadline) {
		w = do(http.MethodGet, "/api/dictionary/upload_status?taskId="+taskID, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("upload_status = %d: %s", w.Code, w.Body.String())
		}
		finalTask = dataOf(decode(w))
		finalStatus, _ = finalTask["status"].(string)
		if finalStatus == "done" || finalStatus == "failed" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if finalStatus != "done" {
		t.Fatalf("上传任务未完成: status=%s task=%v", finalStatus, finalTask)
	}
	if processed := int(finalTask["processedLines"].(float64)); processed != 50000 {
		t.Fatalf("upload processedLines = %d, want 50000", processed)
	}
	if progress, _ := finalTask["progress"].(float64); progress != 100 {
		t.Fatalf("upload progress = %v, want 100", finalTask["progress"])
	}

	w = do(http.MethodPost, "/api/dictionary/preview", map[string]interface{}{
		"kind": "dir", "name": uploadName, "limit": 200,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("大文件 preview = %d: %s", w.Code, w.Body.String())
	}
	bigPreview := dataOf(decode(w))
	if total := int(bigPreview["total"].(float64)); total != 50000 {
		t.Fatalf("大文件 preview total = %d, want 50000", total)
	}
	if got := len(toStrings(bigPreview["lines"])); got != 200 {
		t.Fatalf("大文件 preview 行数 = %d, want 200（上限）", got)
	}

	// 11.1) append 模式上传：与现有 5 万行去重，只加真正的新行
	w = uploadDictionary(t, router, token, "dir", uploadName, "append", []byte("path-000000\npath-000001\nnew-1\n"))
	if w.Code != http.StatusOK {
		t.Fatalf("append 模式 upload = %d: %s", w.Code, w.Body.String())
	}
	appendTask := dataOf(decode(w))
	appendTaskID, _ := appendTask["taskId"].(string)
	appendDeadline := time.Now().Add(60 * time.Second)
	var appendFinal map[string]interface{}
	for time.Now().Before(appendDeadline) {
		w = do(http.MethodGet, "/api/dictionary/upload_status?taskId="+appendTaskID, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("append upload_status = %d: %s", w.Code, w.Body.String())
		}
		appendFinal = dataOf(decode(w))
		if status, _ := appendFinal["status"].(string); status == "done" || status == "failed" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if status, _ := appendFinal["status"].(string); status != "done" {
		t.Fatalf("append 上传未完成: %v", appendFinal)
	}
	if added := int(appendFinal["added"].(float64)); added != 1 {
		t.Fatalf("append 上传 added = %d, want 1（两行已存在）", added)
	}
	if duplicated := int(appendFinal["duplicated"].(float64)); duplicated != 2 {
		t.Fatalf("append 上传 duplicated = %d, want 2", duplicated)
	}

	// 12) 不存在的 taskId → 404

	if w = do(http.MethodGet, "/api/dictionary/upload_status?taskId=does-not-exist", nil); w.Code != http.StatusNotFound {
		t.Fatalf("未知 taskId = %d, want 404: %s", w.Code, w.Body.String())
	}
}

// uploadDictionary 走 multipart 直连 handler（等价前端上传）。
func uploadDictionary(t *testing.T, router *gin.Engine, token, kind, name, mode string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("kind", kind); err != nil {
		t.Fatalf("write kind: %v", err)
	}
	if err := writer.WriteField("name", name); err != nil {
		t.Fatalf("write name: %v", err)
	}
	if mode != "" {
		if err := writer.WriteField("mode", mode); err != nil {
			t.Fatalf("write mode: %v", err)
		}
	}
	part, err := writer.CreateFormFile("file", "dict.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/dictionary/upload_large", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// buildLines 生成 n 行字典内容（带少量重复，验证去重后仍能全部入库）。
func buildLines(n int) []byte {
	var sb strings.Builder
	sb.Grow(n * 12)
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "path-%06d\n", i)
	}
	return []byte(sb.String())
}

func toStrings(raw interface{}) []string {
	items, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, _ := item.(string)
		out = append(out, s)
	}
	return out
}

// cleanupDictionaries 清掉本次用例造的数据（元数据 / 行 / GridFS 内容 / 上传任务）。
func cleanupDictionaries(t *testing.T, prefix string) {
	t.Helper()
	ctx := context.Background()
	pattern := bson.M{"$regex": "^" + regexp.QuoteMeta(prefix)}

	cursor, err := mongodb.DB.Collection("dictionary").Find(ctx, bson.M{"name": pattern})
	if err == nil {
		var ids []string
		for cursor.Next(ctx) {
			var doc struct {
				ID primitive.ObjectID `bson:"_id"`
			}
			if err := cursor.Decode(&doc); err == nil {
				ids = append(ids, doc.ID.Hex())
			}
		}
		_ = cursor.Close(ctx)
		if len(ids) > 0 {
			_, _ = mongodb.DB.Collection("dictionary_entry").DeleteMany(ctx, bson.M{"dict_id": bson.M{"$in": ids}})
			files, ferr := mongodb.DB.Collection("fs.files").Find(ctx, bson.M{"filename": bson.M{"$in": ids}})
			if ferr == nil {
				var fileIDs []interface{}
				for files.Next(ctx) {
					var f struct {
						ID interface{} `bson:"_id"`
					}
					if err := files.Decode(&f); err == nil {
						fileIDs = append(fileIDs, f.ID)
					}
				}
				_ = files.Close(ctx)
				if len(fileIDs) > 0 {
					_, _ = mongodb.DB.Collection("fs.chunks").DeleteMany(ctx, bson.M{"files_id": bson.M{"$in": fileIDs}})
					_, _ = mongodb.DB.Collection("fs.files").DeleteMany(ctx, bson.M{"_id": bson.M{"$in": fileIDs}})
				}
			}
		}
	}

	_, _ = mongodb.DB.Collection("dictionary").DeleteMany(ctx, bson.M{"name": pattern})
	_, _ = mongodb.DB.Collection("dict_upload_tasks").DeleteMany(ctx, bson.M{"name": pattern})
}

// 历史 /manage 接口回归：本次把 create/save/delete/download 改成走行级存储，
// 老接口的入参出参必须保持不变（前端字典管理页还在用）。
func TestDictionaryLegacyManageRoutesStillWork(t *testing.T) {
	if mongodb.DB == nil || redis.Client == nil {
		t.Skip("mongo/redis not initialized")
	}
	if err := mongodb.EnsureIndexes(mongodb.DB); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}

	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	prefix := fmt.Sprintf("dict-legacy-%d", time.Now().UnixNano())
	name := prefix + "-legacy"
	t.Cleanup(func() { cleanupDictionaries(t, prefix) })

	multipartPost := func(path string, fields map[string]string, fileContent []byte) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		for k, v := range fields {
			if err := writer.WriteField(k, v); err != nil {
				t.Fatalf("write field %s: %v", k, err)
			}
		}
		part, err := writer.CreateFormFile("file", "dict.txt")
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := part.Write(fileContent); err != nil {
			t.Fatalf("write file: %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("close multipart: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, path, &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	call := func(method, path string, payload interface{}) *httptest.ResponseRecorder {
		t.Helper()
		var reader *bytes.Reader
		if payload != nil {
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			reader = bytes.NewReader(raw)
		} else {
			reader = bytes.NewReader(nil)
		}
		req := httptest.NewRequest(method, path, reader)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// create：内容含重复行，落盘内容应当已去重
	if rec := multipartPost("/api/dictionary/manage/create",
		map[string]string{"name": name, "category": "dir"}, []byte("a\nb\na\n")); rec.Code != http.StatusOK {
		t.Fatalf("legacy create = %d: %s", rec.Code, rec.Body.String())
	}

	// list：拿到 ID（前端下载/保存都用 ID）
	rec := call(http.MethodGet, "/api/dictionary/manage/list", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("legacy list = %d: %s", rec.Code, rec.Body.String())
	}
	var listPayload struct {
		Data struct {
			List []struct {
				ID       string `json:"id"`
				Name     string `json:"name"`
				Category string `json:"category"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode legacy list: %v", err)
	}
	dictID := ""
	for _, item := range listPayload.Data.List {
		if item.Name == name {
			dictID = item.ID
		}
	}
	if dictID == "" {
		t.Fatalf("legacy list 未返回新建字典: %s", rec.Body.String())
	}

	// download：去重后的内容
	rec = call(http.MethodGet, "/api/dictionary/manage/download?id="+dictID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("legacy download = %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "a\nb\n" {
		t.Fatalf("legacy download 内容 = %q, want %q", got, "a\nb\n")
	}

	// save：整份替换
	if rec = multipartPost("/api/dictionary/manage/save?id="+dictID, map[string]string{}, []byte("c\n")); rec.Code != http.StatusOK {
		t.Fatalf("legacy save = %d: %s", rec.Code, rec.Body.String())
	}
	rec = call(http.MethodGet, "/api/dictionary/manage/download?id="+dictID, nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "c\n" {
		t.Fatalf("legacy save 后 download = %d %q", rec.Code, rec.Body.String())
	}

	// delete：删完后下载失败（历史行为是非 200）
	if rec = call(http.MethodPost, "/api/dictionary/manage/delete", map[string]interface{}{"ids": []string{dictID}}); rec.Code != http.StatusOK {
		t.Fatalf("legacy delete = %d: %s", rec.Code, rec.Body.String())
	}
	if rec = call(http.MethodGet, "/api/dictionary/manage/download?id="+dictID, nil); rec.Code == http.StatusOK {
		t.Fatalf("删除后 legacy download 仍返回 200: %s", rec.Body.String())
	}
}

// 老数据迁移：只有元数据 + GridFS 内容的字典（改造前创建的）第一次被行级接口访问时
// 应当自动回填行级集合，且回填后下载内容与去重结果一致（不会出现"半份字典"）。
func TestDictionaryLegacyBackfillFromGridFS(t *testing.T) {
	if mongodb.DB == nil || redis.Client == nil {
		t.Skip("mongo/redis not initialized")
	}
	if err := mongodb.EnsureIndexes(mongodb.DB); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}

	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	prefix := fmt.Sprintf("dict-backfill-%d", time.Now().UnixNano())
	name := prefix + "-legacy"
	t.Cleanup(func() { cleanupDictionaries(t, prefix) })

	ctx := context.Background()
	res, err := mongodb.DB.Collection("dictionary").InsertOne(ctx, bson.M{
		"name": name, "category": "dir", "size": "0.01",
	})
	if err != nil {
		t.Fatalf("seed legacy dictionary: %v", err)
	}
	dictID, ok := res.InsertedID.(primitive.ObjectID)
	if !ok {
		t.Fatalf("unexpected inserted id type: %T", res.InsertedID)
	}
	if err := mongodb.CreateGridFSFile(mongodb.DB, dictID.Hex(), []byte("legacy-a\nlegacy-b\nlegacy-a\n")); err != nil {
		t.Fatalf("seed legacy gridfs content: %v", err)
	}

	call := func(method, path string, payload interface{}) *httptest.ResponseRecorder {
		t.Helper()
		var reader *bytes.Reader
		if payload != nil {
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			reader = bytes.NewReader(raw)
		} else {
			reader = bytes.NewReader(nil)
		}
		req := httptest.NewRequest(method, path, reader)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1) preview 首次访问触发回填
	rec := call(http.MethodPost, "/api/dictionary/preview", map[string]interface{}{
		"kind": "dir", "name": name, "limit": 10,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("legacy preview = %d: %s", rec.Code, rec.Body.String())
	}
	var previewResp struct {
		Data struct {
			Total int64    `json:"total"`
			Lines []string `json:"lines"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &previewResp); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if previewResp.Data.Total != 2 {
		t.Fatalf("回填后 total = %d, want 2（去重）", previewResp.Data.Total)
	}
	if len(previewResp.Data.Lines) != 2 || previewResp.Data.Lines[0] != "legacy-a" || previewResp.Data.Lines[1] != "legacy-b" {
		t.Fatalf("回填后 lines = %v", previewResp.Data.Lines)
	}

	// 2) download 内容按行级集合重建（去重后的）
	rec = call(http.MethodGet, "/api/dictionary/download?kind=dir&name="+name, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("legacy download = %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "legacy-a\nlegacy-b\n" {
		t.Fatalf("回填后 download = %q", got)
	}

	// 3) append 在回填后的行级集合上继续工作
	rec = call(http.MethodPost, "/api/dictionary/append", map[string]interface{}{
		"kind": "dir", "name": name, "lines": []string{"legacy-c", "legacy-a"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("legacy append = %d: %s", rec.Code, rec.Body.String())
	}
	var appendResp struct {
		Data struct {
			Added      int64 `json:"added"`
			Duplicated int64 `json:"duplicated"`
			Total      int64 `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &appendResp); err != nil {
		t.Fatalf("decode append: %v", err)
	}
	if appendResp.Data.Added != 1 || appendResp.Data.Duplicated != 1 || appendResp.Data.Total != 3 {
		t.Fatalf("回填后 append = %+v, want added=1 duplicated=1 total=3", appendResp.Data)
	}
}

// 字典相关集合必须有对应索引：唯一索引缺失会静默丢失去重语义，
// 排序/过滤索引缺失会让列表退化成全集合扫描。
func TestDictionaryIndexesExist(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	if err := mongodb.EnsureIndexes(mongodb.DB); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}

	type indexSpec struct {
		Key    bson.D `bson:"key"`
		Unique bool   `bson:"unique"`
	}
	indexesOf := func(collection string) []indexSpec {
		t.Helper()
		cursor, err := mongodb.DB.Collection(collection).Indexes().List(context.Background())
		if err != nil {
			t.Fatalf("list %s indexes: %v", collection, err)
		}
		var specs []indexSpec
		if err := cursor.All(context.Background(), &specs); err != nil {
			t.Fatalf("decode %s indexes: %v", collection, err)
		}
		return specs
	}
	hasIndex := func(specs []indexSpec, keys ...string) (bool, bool) {
		for _, spec := range specs {
			if len(spec.Key) != len(keys) {
				continue
			}
			match := true
			for i, k := range keys {
				if spec.Key[i].Key != k {
					match = false
					break
				}
			}
			if match {
				return true, spec.Unique
			}
		}
		return false, false
	}

	cases := []struct {
		collection string
		keys       []string
		unique     bool
	}{
		{collection: "dictionary", keys: []string{"category", "name"}},
		{collection: "dictionary", keys: []string{"category", "updated_at"}},
		{collection: "dictionary", keys: []string{"updated_at"}},
		{collection: "dictionary", keys: []string{"name"}},
		{collection: "dictionary_entry", keys: []string{"dict_id", "value"}, unique: true},
		{collection: "dictionary_entry", keys: []string{"dict_id", "seq"}},
		{collection: "dictionary_entry", keys: []string{"category"}},
		{collection: "dict_upload_tasks", keys: []string{"task_id"}, unique: true},
		{collection: "dict_upload_tasks", keys: []string{"status"}},
		{collection: "dict_upload_tasks", keys: []string{"created_at"}},
		{collection: "fs.files", keys: []string{"filename", "uploadDate"}},
	}
	for _, c := range cases {
		found, unique := hasIndex(indexesOf(c.collection), c.keys...)
		if !found {
			t.Fatalf("%s 缺少索引 %v", c.collection, c.keys)
		}
		if c.unique && !unique {
			t.Fatalf("%s 的索引 %v 必须是唯一索引", c.collection, c.keys)
		}
	}
}
