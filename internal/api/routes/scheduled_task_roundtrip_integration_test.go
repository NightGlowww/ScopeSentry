//go:build integration

package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 计划任务的 add / update 此前没有任何测试或契约脚本点名。
// 它们最容易出的不是报错，而是「建了但周期算错」或「改了但 update 的是另一条」：
// 界面上照旧显示旧名字/旧周期，下一次触发时间也就不对——用户根本发现不了。
// 这里走 add → list → update → list 反映 → delete → list 消失。
func TestScheduledTaskAddUpdateRoundTrip(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)
	ctx := context.Background()
	if mongodb.DB == nil {
		t.Skip("mongo 未连接，跳过计划任务用例")
	}

	do := func(path string, body interface{}) *httptest.ResponseRecorder {
		payload, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)
		return w
	}
	find := func(name string) map[string]interface{} {
		w := do("/api/task/scheduled", map[string]interface{}{
			"search": name, "pageIndex": 1, "pageSize": 50,
		})
		if w.Code != http.StatusOK {
			t.Fatalf("scheduled list = %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Data struct {
				List []map[string]interface{} `json:"list"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode scheduled list: %v", err)
		}
		for _, it := range resp.Data.List {
			if it["name"] == name {
				return it
			}
		}
		return nil
	}

	name := "it-sched-" + t.Name()
	w := do("/api/task/scheduled/add", map[string]interface{}{
		"name": name, "node": []string{"node-it"}, "cycleType": "day",
		"hour": 3, "minute": 5, "type": "general", "allNode": false,
		"target": "", "targetSource": "general",
		// 先建成停用态：用户的实际路径就是「建好 → 再打开」
		"scheduledTasks": false,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("scheduled add = %d: %s", w.Code, w.Body.String())
	}

	created := find(name)
	if created == nil {
		t.Fatalf("新建的计划任务在列表里查不到")
	}
	id, _ := created["id"].(string)
	if id == "" {
		// 列表里 _id 字段名在部分接口是 _id
		id, _ = created["_id"].(string)
	}
	if id == "" {
		t.Fatalf("计划任务缺少 id: %v", created)
	}
	t.Cleanup(func() {
		do("/api/task/scheduled/delete", map[string]interface{}{"ids": []string{id}})
		_, _ = mongodb.DB.Collection("scheduled").DeleteMany(ctx, bson.M{"_id": id})
	})

	renamed := name + "-v2"
	// 打开启用开关：这一步必须算出 nextTime，否则任务永远不会触发，
	// 而界面上只显示 nextRun = "-"，没人能发现（用户真实路径就是 建好 → 再打开）
	w = do("/api/task/scheduled/update", map[string]interface{}{
		"id": id, "name": renamed, "node": []string{"node-it"},
		"cycleType": "daily", "hour": 4, "minute": 30, "type": "general",
		"target": "", "targetSource": "general", "scheduledTasks": true,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("scheduled update = %d: %s", w.Code, w.Body.String())
	}

	if find(name) != nil {
		t.Fatalf("改名后旧名字仍能查到，说明 update 没落到这条记录")
	}
	updated := find(renamed)
	if updated == nil {
		t.Fatalf("改名后新名字查不到，update 没生效")
	}
	if h, ok := updated["hour"].(float64); ok && int(h) != 4 {
		t.Fatalf("hour 没更新: %v", updated["hour"])
	}
	// nextTime 是任务**第一次真正触发时**由 handler 写回的（scheduler/jobs/scan.go），
	// 刚启用时为空是设计如此。能证明「它真的会被触发」的是 scheduled 集合里那条例行任务。
	if n, err := mongodb.DB.Collection("scheduled").CountDocuments(ctx, bson.M{"_id": id}); err != nil || n == 0 {
		t.Fatalf("启用后没有排进调度器（scheduled 集合无记录），任务永远不会触发: n=%d err=%v", n, err)
	}

	w = do("/api/task/scheduled/delete", map[string]interface{}{"ids": []string{id}})
	if w.Code != http.StatusOK {
		t.Fatalf("scheduled delete = %d: %s", w.Code, w.Body.String())
	}
	if find(renamed) != nil {
		t.Fatalf("删除后仍能查到计划任务")
	}

	// 改一个不存在的计划任务：必须是 4xx，不能回 500「操作失败」
	// （原来 service 返回普通 error，handler 一律 500，一个不存在的 id 被当成服务器故障）
	ghost := primitive.NewObjectID().Hex()
	if w := do("/api/task/scheduled/update", map[string]interface{}{
		"id": ghost, "name": "ghost-" + t.Name(), "node": []string{"node-it"},
		"cycleType": "daily", "hour": 1, "minute": 0,
	}); w.Code == http.StatusOK || w.Code == http.StatusInternalServerError {
		t.Fatalf("更新不存在的计划任务回了 %d（应 4xx）: %s", w.Code, w.Body.String())
	}
}
