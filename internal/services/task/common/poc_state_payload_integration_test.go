//go:build integration

package common

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// nuclei 插件 hash（与 constants 里的 VulnScan 插件一致）
const itNucleiHash = "ed93b8af6b72fe54a60efdb932cf6fbc"

// 模板停用必须真的影响扫描载荷：界面上点「停用」之后，节点收到的 -t 清单里不能还有它。
// 这是「停用」和「只改一个标签」的分界线。
func TestGetScanTemplateDropsDisabledPocs(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	pocs := mongodb.DB.Collection("PocList")
	enabledID := "it-poc-on-" + suffix
	disabledID := "it-poc-off-" + suffix
	_, err := pocs.InsertMany(ctx, []interface{}{
		bson.M{"name": enabledID, "id": enabledID, "level": "info", "content": "id: " + enabledID, "disabled": false},
		bson.M{"name": disabledID, "id": disabledID, "level": "info", "content": "id: " + disabledID, "disabled": true},
	})
	if err != nil {
		t.Fatalf("seed poc: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pocs.DeleteMany(context.Background(), bson.M{"id": bson.M{"$in": []string{enabledID, disabledID}}})
	})

	templates := mongodb.DB.Collection("ScanTemplates")
	seed := func(name string, vulList []string) primitive.ObjectID {
		t.Helper()
		res, err := templates.InsertOne(ctx, bson.M{
			"name":              name,
			"VulnerabilityScan": []string{itNucleiHash},
			"Parameters": bson.M{
				"VulnerabilityScan": bson.M{itNucleiHash: "-silent"},
			},
			"vullist": vulList,
		})
		if err != nil {
			t.Fatalf("seed template: %v", err)
		}
		id := res.InsertedID.(primitive.ObjectID)
		t.Cleanup(func() { _, _ = templates.DeleteMany(context.Background(), bson.M{"_id": id}) })
		return id
	}

	svc := NewService()

	// 1) 一个启用 + 一个停用：只下发启用的那个
	tpl := seed("it-poc-payload-"+suffix, []string{enabledID, disabledID})
	got, err := svc.GetScanTemplate(ctx, &models.Task{Template: tpl.Hex(), Name: "it-poc-payload-" + suffix})
	if err != nil {
		t.Fatalf("GetScanTemplate: %v", err)
	}
	param := got.Parameters.VulnerabilityScan[itNucleiHash]
	if !strings.Contains(param, enabledID+".yaml") {
		t.Fatalf("启用的模板没下发到 -t 里: %q", param)
	}
	if strings.Contains(param, disabledID+".yaml") {
		t.Fatalf("停用的模板仍在下发清单里: %q", param)
	}

	// 2) 全部停用：把 nuclei 从漏洞扫描阶段摘掉（而不是「什么都不加」让插件跑全部模板）
	allOff := seed("it-poc-alloff-"+suffix, []string{disabledID})
	got2, err := svc.GetScanTemplate(ctx, &models.Task{Template: allOff.Hex(), Name: "it-poc-alloff-" + suffix})
	if err != nil {
		t.Fatalf("GetScanTemplate(all off): %v", err)
	}
	for _, h := range got2.VulnerabilityScan {
		if h == itNucleiHash {
			t.Fatalf("全部模板停用时 nuclei 仍留在 VulnerabilityScan 里: %v", got2.VulnerabilityScan)
		}
	}
}
