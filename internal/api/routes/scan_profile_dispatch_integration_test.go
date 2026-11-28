//go:build integration

package routes

import (
	"context"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	taskcommon "github.com/Autumn-27/ScopeSentry/internal/services/task/common"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 扫描档位必须在下发路径上真的生效：节点拿到的是 GetScanTemplate 解析出来的模板副本，
// 只测 scanprofile.Apply 这个纯函数是不够的——漏了那一行调用，单元测试照样全绿，
// 而任务仍然会跑 nuclei 与目录扫描（2026-11-29 加这层保险）。
func TestGetScanTemplateAppliesScanProfile(t *testing.T) {
	ctx := context.Background()
	// 注意集合名大小写：仓储用的是 "ScanTemplates"（Mongo 对集合名大小写敏感，
	// 写成 scan_template 会读不到，测试就变成"模板没找到"的假失败）
	coll := mongodb.DB.Collection("ScanTemplates")

	tpl := models.ScanTemplate{
		Name:              "it-dispatch-profile-" + t.Name(),
		SubdomainScan:     []string{"sub"},
		DirScan:           []string{"dir"},
		VulnerabilityScan: []string{"vul"},
		Parameters: models.Parameters{
			DirScan:           map[string]string{"dir": "-d x -t 8"},
			VulnerabilityScan: map[string]string{"vul": "-s high"},
		},
	}
	ins, err := coll.InsertOne(ctx, tpl)
	if err != nil {
		t.Fatalf("insert template: %v", err)
	}
	oid := ins.InsertedID.(primitive.ObjectID)
	t.Cleanup(func() { _, _ = coll.DeleteOne(ctx, bson.M{"_id": oid}) })

	svc := taskcommon.NewService()

	// recon：两个重模块必须被摘掉，其余模块保留
	gotRecon, err := svc.GetScanTemplate(ctx, &models.Task{
		Name: "it-recon", Template: oid.Hex(), ScanProfile: "recon",
	})
	if err != nil {
		t.Fatalf("GetScanTemplate(recon): %v", err)
	}
	if len(gotRecon.VulnerabilityScan) != 0 || len(gotRecon.DirScan) != 0 {
		t.Fatalf("recon 档位没摘重模块: vul=%v dir=%v", gotRecon.VulnerabilityScan, gotRecon.DirScan)
	}
	if len(gotRecon.SubdomainScan) != 1 {
		t.Fatalf("recon 档位误删了非重模块: %v", gotRecon.SubdomainScan)
	}

	// 缺省档位：模板原样下发（这次是带 nuclei 的模板）
	gotFull, err := svc.GetScanTemplate(ctx, &models.Task{
		Name: "it-full", Template: oid.Hex(),
	})
	if err != nil {
		t.Fatalf("GetScanTemplate(full): %v", err)
	}
	if len(gotFull.VulnerabilityScan) != 1 || len(gotFull.DirScan) != 1 {
		t.Fatalf("缺省档位不该裁剪模板: vul=%v dir=%v", gotFull.VulnerabilityScan, gotFull.DirScan)
	}
}
