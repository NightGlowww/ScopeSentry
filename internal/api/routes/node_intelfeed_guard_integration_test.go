//go:build integration

package routes

import (
	"net/http"
	"testing"
)

// 节点与情报源的控制面入口此前没有用例点名。
//
// 这两类动作的特点是「回 200 但什么都没发生」最难被发现：
// 清空队列、开关情报源都只写一行状态，界面上的数字看起来合理，人不会去核。
func TestNodeAndIntelFeedGuards(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	// 清一个不存在的节点队列：必须如实回 cleared=0，而不是假装清了一批
	w := postJSON(t, router, token, "/api/node/queue/clear", map[string]interface{}{
		"name": "it-node-does-not-exist",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("queue/clear = %d: %s", w.Code, w.Body.String())
	}
	var cleared struct {
		Cleared int64 `json:"cleared"`
	}
	decodeData(t, w, &cleared)
	if cleared.Cleared != 0 {
		t.Fatalf("不存在的节点清出 %d 条，期望 0", cleared.Cleared)
	}

	// 缺字段要 400：ModulesConfig 为空时不能默默把节点配置清空
	w = postJSON(t, router, token, "/api/node/config/update", map[string]interface{}{
		"name": "it-node", "state": true,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("config/update 缺 ModulesConfig = %d，期望 400: %s", w.Code, w.Body.String())
	}

	// 情报源：缺 id 要 400；没有可改字段也要 400（否则等于一次空写）
	w = postJSON(t, router, token, "/api/system/intel-feeds/update", map[string]interface{}{})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("intel-feeds/update 缺 id = %d，期望 400: %s", w.Code, w.Body.String())
	}
	w = postJSON(t, router, token, "/api/system/intel-feeds/update", map[string]interface{}{"id": "p0"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("intel-feeds/update 无可改字段 = %d，期望 400: %s", w.Code, w.Body.String())
	}

	// 不存在的源：如实回 matched=0，让界面能说「这个源不在库里」
	w = postJSON(t, router, token, "/api/system/intel-feeds/update", map[string]interface{}{
		"id": "it-no-such-feed", "enabled": false,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("intel-feeds/update 未知源 = %d: %s", w.Code, w.Body.String())
	}
	var upd struct {
		Matched  int64 `json:"matched"`
		Modified int64 `json:"modified"`
	}
	decodeData(t, w, &upd)
	if upd.Matched != 0 || upd.Modified != 0 {
		t.Fatalf("未知情报源 matched=%d modified=%d，期望都是 0", upd.Matched, upd.Modified)
	}

	// 抓取间隔越界要 400（1 小时 ~ 30 天）
	w = postJSON(t, router, token, "/api/system/intel-feeds/update", map[string]interface{}{
		"id": "p0", "intervalHours": 0,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("intervalHours=0 = %d，期望 400: %s", w.Code, w.Body.String())
	}
}
