//go:build integration

package routes

import (
	"archive/zip"
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func uploadPOC(t *testing.T, router http.Handler, token, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("建表单失败: %v", err)
	}
	_, _ = fw.Write(content)
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/poc/data/import", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// POC 导入这条路径此前没有用例点名。
//
// 它是异步的：接口回 200 只代表「收下了」，所以这里既要钉住格式门禁，
// 也要确认真收下一个合法 zip（不然「导入中」永远等不到结果）。
func TestPOCImportGuards(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	// 非 zip 直接拒
	w := uploadPOC(t, router, token, "it-poc.yaml", []byte("id: it-poc"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非 zip 导入 = %d，期望 400: %s", w.Code, w.Body.String())
	}

	// 扩展名判断必须大小写无关：用户从 Windows 拖上来的 zip 常是 .ZIP
	w = uploadPOC(t, router, token, "IT-POC.ZIP", []byte("not really a zip"))
	if w.Code != http.StatusOK {
		t.Fatalf("大写 .ZIP 导入 = %d，期望 200（大小写不该挡人）: %s", w.Code, w.Body.String())
	}

	// 合法空壳 zip：必须被接收（返回 200 与 in progress 提示）
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	zf, err := zw.Create(fmt.Sprintf("it-poc-%d.yaml", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("建 zip 失败: %v", err)
	}
	_, _ = zf.Write([]byte("id: it-poc-fixture\ninfo:\n  name: it-poc-fixture\n  severity: info\n"))
	_ = zw.Close()
	w = uploadPOC(t, router, token, "it-poc-bundle.zip", zbuf.Bytes())
	if w.Code != http.StatusOK {
		t.Fatalf("合法 zip 导入 = %d，期望 200: %s", w.Code, w.Body.String())
	}

	// 进度接口要能答（前端点完导入就靠它轮询）
	req := httptest.NewRequest(http.MethodGet, "/api/poc/data/import/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	sw := httptest.NewRecorder()
	router.ServeHTTP(sw, req)
	if sw.Code != http.StatusOK {
		t.Fatalf("导入进度 = %d: %s", sw.Code, sw.Body.String())
	}
	var snap map[string]interface{}
	decodeData(t, sw, &snap)
	if snap == nil {
		t.Fatalf("导入进度没返回快照: %s", sw.Body.String())
	}
}
