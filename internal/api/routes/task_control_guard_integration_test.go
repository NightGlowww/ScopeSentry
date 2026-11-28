//go:build integration

package routes

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"go.mongodb.org/mongo-driver/bson"
)

// 任务控制面的三个批量入口此前没有用例点名：stop / target/resume / deleteByFilter。
//
// 它们都是「界面上点了就以为生效」的动作：stop 对不存在的 id 回 200、
// deleteByFilter 少一个筛选条件就是全量删除，都要钉住。
func TestTaskControlGuards(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)
	tasks := mongodb.DB.Collection("task")

	stamp := time.Now().UnixNano()
	keepName := fmt.Sprintf("it-keep-%d", stamp)
	delName := fmt.Sprintf("it-del-%d", stamp)
	docs := []interface{}{
		bson.M{"name": keepName, "status": 3, "taskNum": 0, "target": "keep.invalid"},
		bson.M{"name": delName, "status": 3, "taskNum": 0, "target": "del.invalid"},
	}
	if _, err := tasks.InsertMany(t.Context(), docs); err != nil {
		t.Fatalf("插入任务失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = tasks.DeleteMany(t.Context(), bson.M{"name": bson.M{"$in": []string{keepName, delName}}})
	})

	// 1) stop 对不存在的 id 必须 404，不能「暂停成功」
	w := postJSON(t, router, token, "/api/task/stop", map[string]interface{}{
		"ids": []string{"0123456789abcdef01234567"},
	})
	if w.Code != http.StatusNotFound {
		t.Fatalf("stop 不存在的任务 = %d，期望 404: %s", w.Code, w.Body.String())
	}
	w = postJSON(t, router, token, "/api/task/stop", map[string]interface{}{"ids": []string{}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("stop 空 ids = %d，期望 400: %s", w.Code, w.Body.String())
	}

	// 2) resume 目标：任务不存在必须 404
	w = postJSON(t, router, token, "/api/task/target/resume", map[string]interface{}{
		"taskId": "0123456789abcdef01234567", "target": "a.invalid",
	})
	if w.Code != http.StatusNotFound {
		t.Fatalf("resume 不存在的任务目标 = %d，期望 404: %s", w.Code, w.Body.String())
	}

	// 3) deleteByFilter 没有筛选条件 = 全量删除，必须挡在 400
	w = postJSON(t, router, token, "/api/task/deleteByFilter", map[string]interface{}{"pageIndex": 1, "pageSize": 10})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("deleteByFilter 无筛选条件 = %d，期望 400: %s", w.Code, w.Body.String())
	}

	// 4) 带名字删除：只删命中的那条，旁边的任务必须还在
	w = postJSON(t, router, token, "/api/task/deleteByFilter", map[string]interface{}{"search": delName, "pageIndex": 1, "pageSize": 10})
	if w.Code != http.StatusOK {
		t.Fatalf("deleteByFilter = %d: %s", w.Code, w.Body.String())
	}
	var data struct {
		Deleted int64 `json:"deleted"`
	}
	decodeData(t, w, &data)
	if data.Deleted != 1 {
		t.Fatalf("deleteByFilter 删除 %d 条，期望 1: %s", data.Deleted, w.Body.String())
	}
	if n, _ := tasks.CountDocuments(t.Context(), bson.M{"name": delName}); n != 0 {
		t.Fatalf("命中删除的任务还在库里")
	}
	if n, _ := tasks.CountDocuments(t.Context(), bson.M{"name": keepName}); n != 1 {
		t.Fatalf("没被筛选命中的任务被误删了")
	}
}
