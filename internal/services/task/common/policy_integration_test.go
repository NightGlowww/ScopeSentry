//go:build integration

package common

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 建任务链路消费扫描策略（docs/audit/SCAN_POLICY.md）：
//   - 任务带 policyId → 读策略 → 用策略的 templateId 换模板 → 用策略的 plugins 收窄各阶段清单；
//   - 任务不带 policyId → 完全走原路径（模板原样下发）。

func seedWireTemplate(t *testing.T, ctx context.Context, name string, dirPlugins []string) primitive.ObjectID {
	t.Helper()
	params := bson.M{}
	for _, p := range dirPlugins {
		params[p] = "-d " + p
	}
	res, err := mongodb.DB.Collection("ScanTemplates").InsertOne(ctx, bson.M{
		"name":    name,
		"DirScan": dirPlugins,
		"Parameters": bson.M{
			"DirScan": params,
		},
	})
	if err != nil {
		t.Fatalf("seed template: %v", err)
	}
	id := res.InsertedID.(primitive.ObjectID)
	t.Cleanup(func() {
		_, _ = mongodb.DB.Collection("ScanTemplates").DeleteMany(context.Background(), bson.M{"_id": id})
	})
	return id
}

func seedWirePolicy(t *testing.T, ctx context.Context, name, templateID string, plugins []string) primitive.ObjectID {
	t.Helper()
	p := models.ScanPolicy{
		Name:        name,
		Description: "integration",
		Enabled:     true,
		Config: models.ScanPolicyConfig{
			TemplateID: templateID,
			Plugins:    plugins,
		},
	}
	res, err := mongodb.DB.Collection("scan_policy").InsertOne(ctx, p)
	if err != nil {
		t.Fatalf("seed policy: %v", err)
	}
	id := res.InsertedID.(primitive.ObjectID)
	t.Cleanup(func() {
		_, _ = mongodb.DB.Collection("scan_policy").DeleteMany(context.Background(), bson.M{"_id": id})
	})
	return id
}

// 策略消费（docs/audit/SCAN_POLICY.md §78-95 的优先级）：
//
//   - 模板：用户选的赢，策略模板只在任务没填模板时兜底；
//   - 插件：某阶段里命中策略插件 → 只留命中的（保持模板原顺序）；整阶段 0 命中 → 清单不动 + 告警。
//
// 这个用例原来断言「策略模板覆盖任务模板」，与文档和 taskpolicy.ApplyToTask 的优先级相反。
// 它一直没被发现，是因为 internal/services/task/common 的集成用例从来没被执行过
// （integration.sh 只跑 internal/api/routes 和 internal/mcp）。
func TestGetScanTemplateAppliesPolicy(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	// 任务自选 tpl-a（dir-a1/dir-a2）；策略指向别的模板 tpl-b，并只点名 dir-a1
	tplA := seedWireTemplate(t, ctx, "wire-tpl-a-"+suffix, []string{"dir-a1", "dir-a2"})
	tplB := seedWireTemplate(t, ctx, "wire-tpl-b-"+suffix, []string{"dir-b1", "dir-b2"})
	policyID := seedWirePolicy(t, ctx, "wire-policy-"+suffix, tplB.Hex(), []string{"dir-a1"})

	svc := NewService()
	task := &models.Task{
		Template: tplA.Hex(),
		PolicyID: policyID.Hex(),
		Name:     "wire-task-" + suffix,
		Node:     []string{"node-x"},
		Type:     "general",
	}
	tpl, err := svc.GetScanTemplate(ctx, task)
	if err != nil {
		t.Fatalf("GetScanTemplate: %v", err)
	}
	// 模板用任务选的 tpl-a，不被策略的 tpl-b 顶掉
	if got := tpl.DirScan; len(got) != 1 || got[0] != "dir-a1" {
		t.Fatalf("DirScan = %v, want [dir-a1]（模板取任务的 tpl-a，插件按策略白名单收窄）", got)
	}
	if _, ok := tpl.Parameters.DirScan["dir-a2"]; ok {
		t.Fatalf("dir-a2 应被策略白名单移出参数表: %v", tpl.Parameters.DirScan)
	}
	if _, ok := tpl.Parameters.DirScan["dir-a1"]; !ok {
		t.Fatalf("dir-a1 的参数应保留: %v", tpl.Parameters.DirScan)
	}
}

// 任务没填模板时，策略模板兜底：这时策略的插件白名单作用在策略模板上。
func TestGetScanTemplateFallsBackToPolicyTemplate(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	tplB := seedWireTemplate(t, ctx, "wire-tpl-fb-"+suffix, []string{"dir-b1", "dir-b2"})
	policyID := seedWirePolicy(t, ctx, "wire-policy-fb-"+suffix, tplB.Hex(), []string{"dir-b2"})

	svc := NewService()
	task := &models.Task{
		PolicyID: policyID.Hex(),
		Name:     "wire-task-fb-" + suffix,
		Node:     []string{"node-x"},
		Type:     "general",
	}
	tpl, err := svc.GetScanTemplate(ctx, task)
	if err != nil {
		t.Fatalf("GetScanTemplate: %v", err)
	}
	if got := tpl.DirScan; len(got) != 1 || got[0] != "dir-b2" {
		t.Fatalf("DirScan = %v, want [dir-b2]（无任务模板时用策略模板 + 白名单）", got)
	}
	if _, ok := tpl.Parameters.DirScan["dir-b1"]; ok {
		t.Fatalf("dir-b1 应被策略白名单移出参数表: %v", tpl.Parameters.DirScan)
	}
}

func TestGetScanTemplateWithoutPolicyKeepsTemplate(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tplA := seedWireTemplate(t, ctx, "wire-tpl-plain-"+suffix, []string{"dir-a1", "dir-a2"})

	svc := NewService()
	task := &models.Task{
		Template: tplA.Hex(),
		Name:     "wire-task-plain-" + suffix,
		Node:     []string{"node-x"},
		Type:     "general",
	}
	tpl, err := svc.GetScanTemplate(ctx, task)
	if err != nil {
		t.Fatalf("GetScanTemplate: %v", err)
	}
	if got := tpl.DirScan; len(got) != 2 {
		t.Fatalf("DirScan = %v, want 2（不选策略时行为不变）", got)
	}
	if len(tpl.Parameters.DirScan) != 2 {
		t.Fatalf("Parameters.DirScan = %v, want 2", tpl.Parameters.DirScan)
	}
}