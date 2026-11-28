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

// task/sync（把任务目标同步到项目）此前没有用例点名。
// 它的假成功很隐蔽：同步「成功」但项目里没有目标，之后按项目看资产永远是空的。
func TestTaskSyncCreatesProjectWithTargets(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)
	ctx := context.Background()
	if mongodb.DB == nil {
		t.Skip("mongo 未连接，跳过任务同步用例")
	}

	domain := "it-sync-" + t.Name() + ".example.com"
	tasks := mongodb.DB.Collection("task")
	ins, err := tasks.InsertOne(ctx, bson.M{
		"name": "it-sync-task-" + t.Name(), "status": 3, "taskNum": 1,
		"target": domain, "creatTime": "2026-11-29 01:00:00", "endTime": "2026-11-29 01:10:00",
	})
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}
	t.Cleanup(func() { _, _ = tasks.DeleteOne(ctx, bson.M{"_id": ins.InsertedID}) })
	id := ins.InsertedID.(interface{ Hex() string }).Hex()

	projName := "it-sync-proj-" + t.Name()
	projects := mongodb.DB.Collection("project")
	t.Cleanup(func() {
		var p struct {
			ID primitive.ObjectID `bson:"_id"`
		}
		if err := projects.FindOne(ctx, bson.M{"name": projName}).Decode(&p); err == nil {
			_, _ = mongodb.DB.Collection("ProjectTargetData").DeleteMany(ctx, bson.M{"id": p.ID.Hex()})
		}
		_, _ = projects.DeleteMany(ctx, bson.M{"name": projName})
	})

	payload, _ := json.Marshal(map[string]interface{}{
		"ids": []string{id}, "option": "new", "name": projName,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/task/sync", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("task sync = %d: %s", w.Code, w.Body.String())
	}

	// 项目必须真的建出来
	var proj struct {
		ID   primitive.ObjectID `bson:"_id"`
		Name string             `bson:"name"`
	}
	if err := projects.FindOne(ctx, bson.M{"name": projName}).Decode(&proj); err != nil {
		t.Fatalf("同步后项目没建出来: %v（响应 %s）", err, w.Body.String())
	}
	// 目标存在独立集合 ProjectTargetData（project.target 是历史字段，不是这条链路的存储）
	var targetDoc struct {
		Target string `bson:"target"`
	}
	if err := mongodb.DB.Collection("ProjectTargetData").
		FindOne(ctx, bson.M{"id": proj.ID.Hex()}).Decode(&targetDoc); err != nil {
		t.Fatalf("同步后项目目标没落库: %v（响应 %s）", err, w.Body.String())
	}
	if !jsonContains(targetDoc.Target, domain) && targetDoc.Target != domain &&
		!bytes.Contains([]byte(targetDoc.Target), []byte(domain)) {
		t.Fatalf("项目目标里没有待同步域名 %s: %q", domain, targetDoc.Target)
	}
}

func jsonContains(raw string, needle string) bool {
	var v interface{}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return false
	}
	b, _ := json.Marshal(v)
	return bytes.Contains(b, []byte(needle))
}
