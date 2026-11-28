//go:build integration

package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// rad / subfinder 的配置文件保存此前没有用例点名。
//
// 保存走的是对象存储/文件，静默失败的表现是：界面显示已保存，节点拉到的还是旧配置，
// 扫描器行为完全不变——没有用例就只能靠人肉发现。
func TestToolConfigSaveRoundTrip(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	get := func(path string) string {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", path, w.Code, w.Body.String())
		}
		var data struct {
			Content string `json:"content"`
		}
		decodeData(t, w, &data)
		return data.Content
	}

	// 路径写全字面量：拼出来的字符串在引用索引里只能看到截断的前缀，
	// 等于这条路由没人点名（第一版就是这么被漏掉的）。
	cases := []struct {
		tool     string
		dataPath string
		savePath string
	}{
		{"rad", "/api/configuration/rad/data", "/api/configuration/rad/save"},
		{"subfinder", "/api/configuration/subfinder/data", "/api/configuration/subfinder/save"},
	}
	for _, tc := range cases {
		tool, savePath := tc.tool, tc.savePath
		original := get(tc.dataPath)
		t.Cleanup(func() {
			postJSON(t, router, token, savePath, map[string]interface{}{"content": original})
		})

		marker := "# it-" + tool + "-marker"
		w := postJSON(t, router, token, savePath, map[string]interface{}{"content": marker})
		if w.Code != http.StatusOK {
			t.Fatalf("%s save = %d: %s", tool, w.Code, w.Body.String())
		}
		got := get(tc.dataPath)
		if strings.TrimSpace(got) != marker {
			t.Fatalf("%s 保存后读回 = %q，期望 %q", tool, got, marker)
		}
	}
}
