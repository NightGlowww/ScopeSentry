//go:build integration

package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/constants"
	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/database/redis"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 队列条目是**提交时序列化的快照**：模板后来去掉 nuclei 也改不动已经排队的那条。
// 现场（2026-11-29）：模板早已没有 nuclei 插件，排队任务照跑满 ~20 分钟/目标，
// 而界面只说「排队中第 3 位」，看不出这条为什么还会跑 nuclei。
// 这里造两条排队任务（快照一条带 nuclei、一条不带），确认列表按快照本身判定。
func TestTaskListReportsQueuedNucleiSnapshot(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)
	ctx := context.Background()
	if redis.Client == nil || mongodb.DB == nil {
		t.Skip("mongo/redis 未连接，跳过队列快照用例")
	}

	tasks := mongodb.DB.Collection("task")
	mk := func(suffix string) (primitive.ObjectID, string) {
		name := "it-snapshot-" + suffix + "-" + t.Name()
		ins, err := tasks.InsertOne(ctx, bson.M{
			"name": name, "status": 0, "progress": 0, "taskNum": 1,
			"node": []string{"node-it-snapshot"},
			"creatTime": "2026-11-29 01:00:00",
		})
		if err != nil {
			t.Fatalf("insert task %s: %v", suffix, err)
		}
		oid := ins.InsertedID.(primitive.ObjectID)
		t.Cleanup(func() { _, _ = tasks.DeleteOne(ctx, bson.M{"_id": oid}) })
		return oid, name
	}
	oidNuclei, nameNuclei := mk("nuclei")
	oidPlain, namePlain := mk("plain")

	key := "NodeTask:node-it-snapshot"
	t.Cleanup(func() { _ = redis.Client.Del(ctx, key).Err() })
	_ = redis.Client.Del(ctx, key).Err()
	push := func(id primitive.ObjectID, vulnScan []string) {
		payload, err := json.Marshal(map[string]interface{}{
			"id":                id.Hex(),
			"TaskName":          "queued",
			"VulnerabilityScan": vulnScan,
			"DirScan":           []string{constants.PluginDirScan},
		})
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		if err := redis.Client.RPush(ctx, key, string(payload)).Err(); err != nil {
			t.Fatalf("push queue: %v", err)
		}
	}
	push(oidNuclei, []string{constants.PluginNuclei})
	push(oidPlain, []string{})

	p, _ := json.Marshal(map[string]interface{}{
		"status": 0, "pageIndex": 1, "pageSize": 50,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/task/", bytes.NewReader(p))
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
				QueuePos     int    `json:"queuePos"`
				QueuedNuclei bool   `json:"queuedNuclei"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	seen := map[string]struct {
		pos    int
		nuclei bool
	}{}
	for _, it := range resp.Data.List {
		if it.Name == nameNuclei || it.Name == namePlain {
			seen[it.Name] = struct {
				pos    int
				nuclei bool
			}{it.QueuePos, it.QueuedNuclei}
		}
	}
	n, ok := seen[nameNuclei]
	if !ok {
		t.Fatalf("带 nuclei 的排队任务没出现在列表里")
	}
	if !n.nuclei || n.pos < 1 {
		t.Fatalf("快照带 nuclei 却没标出来: pos=%d nuclei=%v", n.pos, n.nuclei)
	}
	pl, ok := seen[namePlain]
	if !ok {
		t.Fatalf("不带 nuclei 的排队任务没出现在列表里")
	}
	if pl.nuclei {
		t.Fatalf("快照没有 nuclei 却被标记")
	}
}
