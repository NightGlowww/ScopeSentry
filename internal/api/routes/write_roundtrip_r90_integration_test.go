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

// task/assignProject：把任务补录到项目。它改的是任务的 bindProject，
// 而项目视图就是按这个字段聚合的——写不进去不会报错，只会表现为「补录了但项目里没有」。
func TestAssignProjectRoundTrip(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)
	ctx := context.Background()
	if mongodb.DB == nil {
		t.Skip("mongo 未连接，跳过补录项目用例")
	}

	projID := primitive.NewObjectID()
	projects := mongodb.DB.Collection("project")
	if _, err := projects.InsertOne(ctx, bson.M{"_id": projID, "name": "it-assign-" + t.Name(), "tag": "it"}); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	tasks := mongodb.DB.Collection("task")
	ins, err := tasks.InsertOne(ctx, bson.M{
		"name": "it-assign-task-" + t.Name(), "status": 3, "taskNum": 0,
		"creatTime": "2026-11-29 01:00:00", "endTime": "2026-11-29 01:10:00",
	})
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}
	oid := ins.InsertedID.(primitive.ObjectID)
	t.Cleanup(func() {
		_, _ = tasks.DeleteOne(ctx, bson.M{"_id": oid})
		_, _ = projects.DeleteOne(ctx, bson.M{"_id": projID})
	})

	do := func(body map[string]interface{}) *httptest.ResponseRecorder {
		payload, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/task/assignProject", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)
		return w
	}
	boundProject := func() string {
		var doc struct {
			BindProject string `bson:"bindProject"`
		}
		if err := tasks.FindOne(ctx, bson.M{"_id": oid}).Decode(&doc); err != nil {
			t.Fatalf("reload task: %v", err)
		}
		return doc.BindProject
	}

	w := do(map[string]interface{}{"ids": []string{oid.Hex()}, "projectId": projID.Hex()})
	if w.Code != http.StatusOK {
		t.Fatalf("assignProject = %d: %s", w.Code, w.Body.String())
	}
	if got := boundProject(); got != projID.Hex() {
		t.Fatalf("bindProject 没写进去: %q", got)
	}

	// projectId 传空 = 解除绑定（任务回到「未归属」）
	w = do(map[string]interface{}{"ids": []string{oid.Hex()}, "projectId": ""})
	if w.Code != http.StatusOK {
		t.Fatalf("unassign = %d: %s", w.Code, w.Body.String())
	}
	if got := boundProject(); got != "" {
		t.Fatalf("解除绑定后 bindProject 应为空，实际 %q", got)
	}

	// 不存在的项目必须拒绝，不能静默把任务绑到一个不存在的 id 上
	ghost := primitive.NewObjectID().Hex()
	if w := do(map[string]interface{}{"ids": []string{oid.Hex()}, "projectId": ghost}); w.Code == http.StatusOK {
		t.Fatalf("绑定不存在的项目居然成功: %s", w.Body.String())
	}
}

// 通知配置是 name-value 覆盖写。空库（新装/被清理过）里 UpdateOne 匹配不到文档时
// 会静默什么都不做——接口回 200，GET 回来还是空，配置就这样永远存不下来。
func TestNotificationConfigPersistsOnEmptyCollection(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)
	ctx := context.Background()
	if mongodb.DB == nil {
		t.Skip("mongo 未连接，跳过通知配置用例")
	}
	cfg := mongodb.DB.Collection("config")
	_, _ = cfg.DeleteMany(ctx, bson.M{"name": "notification"})
	t.Cleanup(func() { _, _ = cfg.DeleteMany(ctx, bson.M{"name": "notification"}) })

	do := func(method, path string, body interface{}) *httptest.ResponseRecorder {
		var payload []byte
		if body != nil {
			payload, _ = json.Marshal(body)
		}
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)
		return w
	}

	w := do(http.MethodPost, "/api/configuration/notification/config/update", map[string]interface{}{
		"itProbeRound90": "on",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("config update = %d: %s", w.Code, w.Body.String())
	}

	read := do(http.MethodGet, "/api/configuration/notification/config/data", nil)
	if read.Code != http.StatusOK {
		t.Fatalf("config read = %d: %s", read.Code, read.Body.String())
	}
	var resp struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(read.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if got := resp.Data["itProbeRound90"]; got != "on" {
		t.Fatalf("空库上第一次写入没生效（UpdateOne 匹配不到文档 = 静默丢弃）: %v", resp.Data)
	}

	// 改一条不存在的通知：必须明确失败，不能回 200（用户会以为保存成功）
	ghost := primitive.NewObjectID().Hex()
	w = do(http.MethodPost, "/api/configuration/notification/update", map[string]interface{}{
		"id": ghost, "name": "ghost", "method": "POST",
		"url": "http://127.0.0.1:9/x", "contentType": "application/json", "data": "{}", "state": false,
	})
	if w.Code == http.StatusOK {
		t.Fatalf("更新不存在的通知回了 200: %s", w.Body.String())
	}
}
