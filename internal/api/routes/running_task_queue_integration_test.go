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
	"github.com/Autumn-27/ScopeSentry/internal/database/redis"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 运行中的任务不能在界面上显示成「排队中」。
// 现场（2026-11-29 任务页实测）：一条 Running 43h58m 的任务同时显示「Queued (5 ahead)」——
// 因为重投/恢复会在 NodeTask 里留下旧消息，而列表对 status=1 也读了队列位次。
// 排队信息只对 status=0（待开始）有意义。
func TestRunningTaskNotReportedAsQueued(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)
	ctx := context.Background()
	if redis.Client == nil || mongodb.DB == nil {
		t.Skip("mongo/redis 未连接，跳过运行态队列用例")
	}

	tasks := mongodb.DB.Collection("task")
	name := "it-running-queued-" + t.Name()
	ins, err := tasks.InsertOne(ctx, bson.M{
		"name": name, "status": 1, "progress": 42, "taskNum": 1,
		"node": []string{"node-it-running"},
		"creatTime": "2026-11-29 01:00:00", "startedAt": "2026-11-29 01:00:05",
	})
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}
	oid := ins.InsertedID.(primitive.ObjectID)
	id := oid.Hex()
	t.Cleanup(func() { _, _ = tasks.DeleteOne(ctx, bson.M{"_id": oid}) })

	key := "NodeTask:node-it-running"
	if err := redis.Client.Del(ctx, key).Err(); err != nil {
		t.Fatalf("reset queue: %v", err)
	}
	payload, _ := json.Marshal(map[string]string{"id": id})
	if err := redis.Client.RPush(ctx, key, string(payload)).Err(); err != nil {
		t.Fatalf("push queue: %v", err)
	}
	t.Cleanup(func() { _ = redis.Client.Del(ctx, key).Err() })

	body, _ := json.Marshal(map[string]interface{}{"status": 1, "pageIndex": 1, "pageSize": 50})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/task/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("task list = %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data struct {
			List []struct {
				Name         string `json:"name"`
				Queued       bool   `json:"queued"`
				QueuePos     int    `json:"queuePos"`
				QueuedNuclei bool   `json:"queuedNuclei"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, it := range resp.Data.List {
		if it.Name != name {
			continue
		}
		found = true
		if it.Queued || it.QueuePos != 0 || it.QueuedNuclei {
			t.Fatalf("运行中的任务不该带排队信息: queued=%v pos=%d nuclei=%v",
				it.Queued, it.QueuePos, it.QueuedNuclei)
		}
	}
	if !found {
		t.Fatalf("列表里没找到任务 %s", name)
	}
}
