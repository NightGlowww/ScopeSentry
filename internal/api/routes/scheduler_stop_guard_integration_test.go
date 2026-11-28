//go:build integration

package routes

import (
	"net/http"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"go.mongodb.org/mongo-driver/bson"
)

// 「暂停」类动作最怕回 200 但没暂停：周期任务照跑，用户以为已经停了。
// 这两条路径此前没有被任何用例点名。
func TestSchedulerStopGuards(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	// GitHub 周期任务：空 ids → 400，不存在的 id → 404
	w := postJSON(t, router, token, "/api/github/scheduler/stop", map[string]interface{}{})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("github scheduler/stop 空 ids = %d，期望 400: %s", w.Code, w.Body.String())
	}
	w = postJSON(t, router, token, "/api/github/scheduler/stop", map[string]interface{}{
		"ids": []string{"0123456789abcdef01234567"},
	})
	if w.Code != http.StatusNotFound {
		t.Fatalf("github scheduler/stop 不存在的 id = %d，期望 404: %s", w.Code, w.Body.String())
	}

	// 页面监控的周期配置是库里唯一一条（id=page_monitoring）。
	// 改它必须真的落库，否则界面上调整抓取间隔等于没调。
	scheduled := mongodb.DB.Collection("ScheduledTasks")
	var before struct {
		Hour           int  `bson:"hour"`
		ScheduledTasks bool `bson:"scheduledTasks"`
	}
	if err := scheduled.FindOne(t.Context(), bson.M{"id": "page_monitoring"}).Decode(&before); err != nil {
		t.Fatalf("读不到 page_monitoring 周期配置: %v", err)
	}
	t.Cleanup(func() {
		_, _ = scheduled.UpdateOne(t.Context(), bson.M{"id": "page_monitoring"},
			bson.M{"$set": bson.M{"hour": before.Hour, "scheduledTasks": before.ScheduledTasks}})
	})
	newHour := before.Hour + 2
	w = postJSON(t, router, token, "/api/task/scheduled/pagemonit/update", map[string]interface{}{
		"hour": newHour, "scheduledTasks": false,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("pagemonit/update = %d: %s", w.Code, w.Body.String())
	}
	var after struct {
		Hour           int  `bson:"hour"`
		ScheduledTasks bool `bson:"scheduledTasks"`
	}
	if err := scheduled.FindOne(t.Context(), bson.M{"id": "page_monitoring"}).Decode(&after); err != nil {
		t.Fatalf("更新后读回失败: %v", err)
	}
	if after.Hour != newHour || after.ScheduledTasks {
		t.Fatalf("周期配置没落库：hour=%d scheduledTasks=%v，期望 %d/false", after.Hour, after.ScheduledTasks, newHour)
	}
}
