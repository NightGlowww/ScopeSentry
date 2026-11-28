//go:build integration

package routes

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"go.mongodb.org/mongo-driver/bson"
)

// 计划任务的「立即运行一次」：界面上点一下就按当前配置生成一条扫描任务。
//
// 走的是调度器 fire 时同一个 jobs.Scan，所以手动跑与按周期跑的产物必须一致：
// 任务名 = 计划任务名 + "-" + 触发时间，计划任务的 lastTime 刷新、nextTime 不被清空。
func TestScheduledTaskRunNow(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	stamp := time.Now().UnixNano()
	name := fmt.Sprintf("it-sched-run-%d", stamp)
	w := postJSON(t, router, token, "/api/task/scheduled/add", map[string]interface{}{
		"name": name, "node": []string{"node-it"}, "cycleType": "day",
		"hour": 3, "minute": 5, "type": "general", "allNode": false,
		"target": "", "targetSource": "general", "scheduledTasks": false,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("scheduled add = %d: %s", w.Code, w.Body.String())
	}

	scheduled := mongodb.DB.Collection("ScheduledTasks")
	tasks := mongodb.DB.Collection("task")
	t.Cleanup(func() {
		_, _ = scheduled.DeleteMany(t.Context(), bson.M{"name": name})
		_, _ = tasks.DeleteMany(t.Context(), bson.M{"name": bson.M{"$regex": "^" + name + "-"}})
	})

	var doc struct {
		ID       interface{} `bson:"_id"`
		NextTime string      `bson:"nextTime"`
	}
	if err := scheduled.FindOne(t.Context(), bson.M{"name": name}).Decode(&doc); err != nil {
		t.Fatalf("读计划任务失败: %v", err)
	}
	idHex := fmt.Sprintf("%v", doc.ID)
	if oid, ok := doc.ID.(interface{ Hex() string }); ok {
		idHex = oid.Hex()
	}

	// 没有目标的计划任务：必须 400 说「目标不能为空」，而不是把底层 "Target is null" 当 500 抛出去
	runNoTarget := postJSON(t, router, token, "/api/task/scheduled/run", map[string]interface{}{"id": idHex})
	if runNoTarget.Code != http.StatusBadRequest {
		t.Fatalf("无目标的计划任务立即运行 = %d，期望 400: %s", runNoTarget.Code, runNoTarget.Body.String())
	}

	// 不存在的 id → 404，不能回 200 假装触发了
	w = postJSON(t, router, token, "/api/task/scheduled/run", map[string]interface{}{
		"id": "0123456789abcdef01234567",
	})
	if w.Code != http.StatusNotFound {
		t.Fatalf("run 不存在的计划任务 = %d，期望 404: %s", w.Code, w.Body.String())
	}

	targetedName := name + "-target"
	w = postJSON(t, router, token, "/api/task/scheduled/add", map[string]interface{}{
		"name": targetedName, "node": []string{"node-it"}, "cycleType": "day",
		"hour": 4, "minute": 10, "type": "general", "allNode": false,
		"target": "it-run.invalid", "targetSource": "general", "scheduledTasks": false,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("scheduled add(targeted) = %d: %s", w.Code, w.Body.String())
	}
	t.Cleanup(func() { _, _ = scheduled.DeleteMany(t.Context(), bson.M{"name": targetedName}) })

	var targeted struct {
		ID       interface{} `bson:"_id"`
		NextTime string      `bson:"nextTime"`
	}
	if err := scheduled.FindOne(t.Context(), bson.M{"name": targetedName}).Decode(&targeted); err != nil {
		t.Fatalf("读有目标的计划任务失败: %v", err)
	}
	targetedID := fmt.Sprintf("%v", targeted.ID)
	if oid, ok := targeted.ID.(interface{ Hex() string }); ok {
		targetedID = oid.Hex()
	}

	w = postJSON(t, router, token, "/api/task/scheduled/run", map[string]interface{}{"id": targetedID})
	if w.Code != http.StatusOK {
		t.Fatalf("run = %d: %s", w.Code, w.Body.String())
	}

	// 产物：一条名为 <计划任务名>-<触发时间> 的扫描任务
	var created struct {
		Name   string `bson:"name"`
		Status int    `bson:"status"`
	}
	if err := tasks.FindOne(t.Context(), bson.M{"name": bson.M{"$regex": "^" + targetedName + "-"}}).Decode(&created); err != nil {
		t.Fatalf("立即运行没生成扫描任务: %v", err)
	}
	if !strings.HasPrefix(created.Name, targetedName+"-") {
		t.Fatalf("生成的任务名 = %q，期望以 %q 开头", created.Name, targetedName+"-")
	}

	// lastTime 要刷新，nextTime 不能被手动触发清空
	var after struct {
		LastTime string `bson:"lastTime"`
		NextTime string `bson:"nextTime"`
	}
	if err := scheduled.FindOne(t.Context(), bson.M{"name": targetedName}).Decode(&after); err != nil {
		t.Fatalf("重读计划任务失败: %v", err)
	}
	if after.LastTime == "" {
		t.Fatalf("立即运行后 lastTime 没刷新")
	}
	if after.NextTime != targeted.NextTime {
		t.Fatalf("立即运行把 nextTime 改了：%q → %q", targeted.NextTime, after.NextTime)
	}
}