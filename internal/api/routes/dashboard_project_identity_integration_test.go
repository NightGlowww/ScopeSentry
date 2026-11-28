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

// 仪表盘按项目过滤时，asset.project 存的是项目 ObjectID 字符串。
// 以前传**项目名称**不报错，只是条件永不命中 → 回 200 + 全 0，
// 调用方（尤其是自动化 / MCP）会据此断言「这个项目没有资产」（2026-11-29 实测）。
// 现在：名称与 ID 都接受；两者都不匹配就回 404，不再假装是 0。
func TestDashboardProjectAcceptsNameAndRejectsUnknown(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)
	ctx := context.Background()
	if mongodb.DB == nil {
		t.Skip("mongo 未连接，跳过仪表盘项目用例")
	}

	projID := primitive.NewObjectID()
	projName := "it-dash-proj-" + t.Name()
	projects := mongodb.DB.Collection("project")
	if _, err := projects.InsertOne(ctx, bson.M{
		"_id": projID, "name": projName, "tag": "it",
	}); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	assets := mongodb.DB.Collection("asset")
	if _, err := assets.InsertOne(ctx, bson.M{
		"domain": "dash-it.example", "port": "80", "type": "http",
		"project": projID.Hex(),
	}); err != nil {
		t.Fatalf("insert asset: %v", err)
	}
	t.Cleanup(func() {
		_, _ = assets.DeleteMany(ctx, bson.M{"project": projID.Hex()})
		_, _ = projects.DeleteOne(ctx, bson.M{"_id": projID})
	})

	stats := func(project string) (int, int) {
		body, _ := json.Marshal(map[string]interface{}{"project": project})
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/dashboard/stats", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			return w.Code, -1
		}
		var resp struct {
			Data struct {
				Asset int `json:"asset"`
			} `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		return w.Code, resp.Data.Asset
	}

	codeByID, countByID := stats(projID.Hex())
	if codeByID != http.StatusOK || countByID < 1 {
		t.Fatalf("按 ID 过滤应有资产: code=%d asset=%d", codeByID, countByID)
	}
	codeByName, countByName := stats(projName)
	if codeByName != http.StatusOK {
		t.Fatalf("按名称过滤不应报错: code=%d", codeByName)
	}
	if countByName != countByID {
		t.Fatalf("名称与 ID 结果不一致: name=%d id=%d", countByName, countByID)
	}

	codeUnknown, _ := stats("definitely-not-a-project-" + t.Name())
	if codeUnknown == http.StatusOK {
		t.Fatalf("未知项目应报 404，实际 200（又变成静默全 0）")
	}
}
