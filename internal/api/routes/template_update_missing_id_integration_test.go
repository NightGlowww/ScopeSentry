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

// 保存扫描模板时带 id = 更新。UpdateOne 匹配不到文档也返回 nil，
// 于是「改一个已被删掉的模板」会回 200，界面上看着保存成功，实际什么都没写。
// 这条和通知配置是同一类（写路径假成功），2026-11-29 一起修。
func TestScanTemplateUpdateRejectsMissingID(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)
	ctx := context.Background()
	if mongodb.DB == nil {
		t.Skip("mongo 未连接，跳过模板更新用例")
	}

	// 造一条最小可用模板（保存时会校验核心模块是否齐全）
	tplID := primitive.NewObjectID()
	coll := mongodb.DB.Collection("ScanTemplates")
	if _, err := coll.InsertOne(ctx, bson.M{
		"_id": tplID, "TaskName": "it-tpl-" + t.Name(),
		"TargetHandler":        []string{"6d8b1b0e27e6f8a2f2f1b0a1"},
		"SubdomainScan":        []string{"1"}, "SubdomainSecurity": []string{"1"},
		"PortScanPreparation":  []string{"1"}, "PortScan": []string{"1"},
		"PortFingerprint":      []string{"1"}, "AssetMapping": []string{"1"},
		"AssetHandle":          []string{"1"}, "URLScan": []string{"1"},
		"WebCrawler":           []string{"1"}, "URLSecurity": []string{"1"},
		"DirScan":              []string{"920546788addc6d29ea63e4a314a1b85"}, "VulnerabilityScan": []string{},
		"Parameters":           bson.M{},
	}); err != nil {
		t.Fatalf("seed template: %v", err)
	}
	t.Cleanup(func() { _, _ = coll.DeleteOne(ctx, bson.M{"_id": tplID}) })

	post := func(body map[string]interface{}) *httptest.ResponseRecorder {
		payload, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/task/template/save", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)
		return w
	}

	base := func(id string, name string) map[string]interface{} {
		return map[string]interface{}{
			"id": id,
			"result": map[string]interface{}{
				"TaskName": name,
				"TargetHandler": []string{"6d8b1b0e27e6f8a2f2f1b0a1"},
				"SubdomainScan": []string{"1"}, "SubdomainSecurity": []string{"1"},
				"PortScanPreparation": []string{"1"}, "PortScan": []string{"1"},
				"PortFingerprint": []string{"1"}, "AssetMapping": []string{"1"},
				"AssetHandle": []string{"1"}, "URLScan": []string{"1"},
				"WebCrawler": []string{"1"}, "URLSecurity": []string{"1"},
				"DirScan": []string{"920546788addc6d29ea63e4a314a1b85"},
			},
		}
	}

	// 存在的模板：改名必须落库
	w := post(base(tplID.Hex(), "it-tpl-renamed-"+t.Name()))
	if w.Code != http.StatusOK {
		t.Fatalf("update existing template = %d: %s", w.Code, w.Body.String())
	}
	var doc struct {
		TaskName string `bson:"TaskName"`
	}
	if err := coll.FindOne(ctx, bson.M{"_id": tplID}).Decode(&doc); err != nil {
		t.Fatalf("reload template: %v", err)
	}
	if doc.TaskName != "it-tpl-renamed-"+t.Name() {
		t.Fatalf("模板名没更新: %q", doc.TaskName)
	}

	// 不存在的模板 id：必须明确失败，不能回 200
	ghost := primitive.NewObjectID().Hex()
	if w := post(base(ghost, "ghost-"+t.Name())); w.Code == http.StatusOK {
		t.Fatalf("更新不存在的模板回了 200: %s", w.Body.String())
	}
}
