//go:build integration

package routes

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 备份上传与存储瘦身这两条写路径此前没有用例点名。
//
// 它们的共同风险是「说了做了但其实没做」：上传一个坏包若回 200，用户会以为备份已入库；
// 瘦身若在没确认串时真跑，就是一次不可逆的数据截断。
func TestBackupAndSlimGuards(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	// 1) 备份状态是纯读，任何时刻都要能答（界面顶部那块进度条靠它）
	req := httptest.NewRequest(http.MethodGet, "/api/backup/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("backup/status = %d: %s", w.Code, w.Body.String())
	}
	var status map[string]interface{}
	decodeData(t, w, &status)
	if status == nil {
		t.Fatalf("backup/status 没返回状态对象: %s", w.Body.String())
	}

	// 2) 上传一个不是备份包的文件：必须 400，不能入库
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "it-not-a-backup.zip")
	if err != nil {
		t.Fatalf("建表单失败: %v", err)
	}
	_, _ = fw.Write([]byte("this is definitely not a backup archive"))
	_ = mw.Close()
	upReq := httptest.NewRequest(http.MethodPost, "/api/backup/upload", &buf)
	upReq.Header.Set("Content-Type", mw.FormDataContentType())
	upReq.Header.Set("Authorization", "Bearer "+token)
	upW := httptest.NewRecorder()
	router.ServeHTTP(upW, upReq)
	if upW.Code != http.StatusBadRequest {
		t.Fatalf("上传非备份包 = %d，期望 400: %s", upW.Code, upW.Body.String())
	}

	// 3) 没带文件字段：400
	empty := httptest.NewRecorder()
	emptyReq := httptest.NewRequest(http.MethodPost, "/api/backup/upload", bytes.NewReader(nil))
	emptyReq.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	emptyReq.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(empty, emptyReq)
	if empty.Code != http.StatusBadRequest {
		t.Fatalf("上传缺 file 字段 = %d，期望 400: %s", empty.Code, empty.Body.String())
	}

	// 4) 瘦身默认 dry-run：只统计，不动库
	w = postJSON(t, router, token, "/api/system/storage/slim", map[string]interface{}{
		"collection": "SensitiveBody", "keepBytes": 1024, "limit": 1,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("slim 干跑 = %d: %s", w.Code, w.Body.String())
	}
	var slim struct {
		DryRun  bool   `json:"dryRun"`
		Updated int64  `json:"updated"`
		Collect string `json:"collection"`
	}
	decodeData(t, w, &slim)
	if !slim.DryRun || slim.Updated != 0 {
		t.Fatalf("默认必须是干跑且不写库：dryRun=%v updated=%d", slim.DryRun, slim.Updated)
	}

	// 5) 白名单外的集合（这里是任务表）：400
	w = postJSON(t, router, token, "/api/system/storage/slim", map[string]interface{}{
		"collection": "task", "keepBytes": 1024,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("slim 非白名单集合 = %d，期望 400: %s", w.Code, w.Body.String())
	}

	// 6) 真正执行必须带 confirm=SLIM
	w = postJSON(t, router, token, "/api/system/storage/slim", map[string]interface{}{
		"collection": "SensitiveBody", "keepBytes": 1024, "dryRun": false,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("slim 无 confirm = %d，期望 400: %s", w.Code, w.Body.String())
	}
}
