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

// 待开始（status=0）但已在节点队列里的任务，列表必须回 queued / queuePos。
// 现场：线上有任务在队列里躺了 30 小时仍是「待开始 0%」，用户分不清排队还是卡死。
// 以前这两个字段只在 status=1 时计算，所以待开始的任务永远拿不到（2026-11-29）。
func TestTaskListQueueInfoForPendingTask(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)
	ctx := context.Background()

	name := "it-queue-info-" + t.Name()
	tasks := mongodb.DB.Collection("task")
	ins, err := tasks.InsertOne(ctx, bson.M{
		"name":      name,
		"node":      []string{"node-it-queue"},
		"status":    0,
		"progress":  0,
		"target":    "example.invalid",
		"creatTime": "2026-11-29 00:00:00",
		"taskNum":   1,
	})
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}
	id := ins.InsertedID.(primitive.ObjectID).Hex()
	t.Cleanup(func() { _, _ = tasks.DeleteOne(ctx, bson.M{"_id": ins.InsertedID}) })

	if redis.Client == nil {
		t.Skip("redis 未连接，跳过队列位次用例")
	}
	key := "NodeTask:node-it-queue"
	payload, _ := json.Marshal(map[string]string{"id": id})
	if err := redis.Client.LPush(ctx, key, string(payload)).Err(); err != nil {
		t.Fatalf("push queue: %v", err)
	}
	t.Cleanup(func() { _ = redis.Client.Del(ctx, key).Err() })

	do := func(body interface{}) *httptest.ResponseRecorder {
		p, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/task/", bytes.NewReader(p))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)
		return w
	}

	// 任务列表的 search 语法与资产不同，这里直接按状态取一页再按名字挑，
	// 避免用例被 DSL 细节绑死（用例要验的是排队字段，不是搜索）。
	w := do(map[string]interface{}{"status": 0, "pageIndex": 1, "pageSize": 50})
	if w.Code != http.StatusOK {
		t.Fatalf("task list = %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			List []struct {
				Name     string `json:"name"`
				Queued   bool   `json:"queued"`
				QueuePos int    `json:"queuePos"`
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
		if !it.Queued {
			t.Fatalf("待开始在队列里，queued 应为 true")
		}
		if it.QueuePos < 1 {
			t.Fatalf("queuePos = %d, want >=1", it.QueuePos)
		}
	}
	if !found {
		t.Fatalf("列表里没找到任务 %s: %s", name, w.Body.String())
	}
}
