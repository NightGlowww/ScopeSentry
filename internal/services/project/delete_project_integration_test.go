//go:build integration

package project

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

// 删除项目但保留资产时，资产上的 project 字段必须清空：
// 否则列表里会显示一个已删除的项目 ID，按项目筛选也永远命中不到。
func TestDeleteProjectsUnsetsAssetProjectWhenKeepingAssets(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	svc := NewService()
	name := fmt.Sprintf("delproj-%d", time.Now().UnixNano())
	host := fmt.Sprintf("delproj-%d.example.com", time.Now().UnixNano())
	assets := mongodb.DB.Collection("asset")
	t.Cleanup(func() {
		_, _ = assets.DeleteMany(context.Background(), bson.M{"host": host})
		_, _ = mongodb.DB.Collection("project").DeleteMany(context.Background(), bson.M{"name": name})
	})

	if err := svc.AddProject(newProjectGinCtx(), &models.Project{Name: name}); err != nil {
		t.Fatalf("add project: %v", err)
	}
	var created struct {
		ID primitive.ObjectID `bson:"_id"`
	}
	if err := mongodb.DB.Collection("project").FindOne(context.Background(), bson.M{"name": name}).Decode(&created); err != nil {
		t.Fatalf("find project: %v", err)
	}
	projectID := created.ID.Hex()

	if _, err := assets.InsertOne(context.Background(), bson.M{
		"host": host, "port": "443", "project": projectID, "time": helperNow(),
	}); err != nil {
		t.Fatalf("seed asset: %v", err)
	}

	if err := svc.DeleteProjects(newProjectGinCtx(), []string{projectID}, false); err != nil {
		t.Fatalf("delete project: %v", err)
	}

	var got struct {
		Project string `bson:"project"`
	}
	if err := assets.FindOne(context.Background(), bson.M{"host": host}).Decode(&got); err != nil {
		t.Fatalf("asset should be kept: %v", err)
	}
	if got.Project != "" {
		t.Fatalf("asset project = %q, want empty after project deletion", got.Project)
	}
	if n, _ := mongodb.DB.Collection("ProjectTargetData").CountDocuments(context.Background(), bson.M{"id": projectID}); n != 0 {
		t.Fatalf("ProjectTargetData rows left: %d", n)
	}
}

// 时间格式与生产一致即可，测试只关心字段存在
func helperNow() string { return time.Now().Format("2006-01-02 15:04:05") }
