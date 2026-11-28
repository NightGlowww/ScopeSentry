//go:build integration

package routes

import (
	"net/http"
	"testing"
)

// 插件状态/手动运行对「列表里没有这个 hash」的处理。
//
// status 以前是无匹配的 UpdateOne → nil → 200：界面上把一个已经被清理的插件关掉会提示成功，
// 调度器那边什么都没变化；run 则回 500。两种都会让人以为动作生效了。
func TestPluginStatusAndRunUnknownHash(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	ghost := "0123456789abcdef0123456789abcdef"

	w := postJSON(t, router, token, "/api/plugin/status", map[string]interface{}{
		"id": ghost, "status": false,
	})
	if w.Code != http.StatusNotFound {
		t.Fatalf("禁用不存在的插件 = %d，期望 404: %s", w.Code, w.Body.String())
	}

	w = postJSON(t, router, token, "/api/plugin/run", map[string]interface{}{"hash": ghost})
	if w.Code != http.StatusNotFound {
		t.Fatalf("运行不存在的插件 = %d，期望 404: %s", w.Code, w.Body.String())
	}
}
