//go:build integration

package project

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func newProjectGinCtx() *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/internal/auto-sync", nil)
	return c
}

// issue #203 收口：任务完成后按 BindProject 自动同步目标到项目。
// 这里直接验上下文版本（SyncTaskTargets）的合并与去重语义。
func TestSyncTaskTargetsMergesAndDedupes(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	svc := NewService()
	name := fmt.Sprintf("autosync-%d", time.Now().UnixNano())

	p := &models.Project{Name: name}
	if err := svc.AddProject(newProjectGinCtx(), p); err != nil {
		t.Fatalf("add project: %v", err)
	}
	// AddProject 不回填 p.ID，按名字把 _id 查回来
	var created struct {
		ID primitive.ObjectID `bson:"_id"`
	}
	if err := mongodb.DB.Collection("project").FindOne(context.Background(), bson.M{"name": name}).Decode(&created); err != nil {
		t.Fatalf("find project: %v", err)
	}
	p.ID = created.ID
	t.Cleanup(func() {
		_, _ = mongodb.DB.Collection("project").DeleteMany(context.Background(), bson.M{"name": name})
		_, _ = mongodb.DB.Collection("ProjectTargetData").DeleteMany(context.Background(), bson.M{"project": p.ID.Hex()})
	})

	added, err := svc.SyncTaskTargets(context.Background(), p.ID.Hex(), []string{"a.example.com", "b.example.com"})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if added != 2 {
		t.Fatalf("added = %d, want 2", added)
	}
	target, err := svc.GetTarget(newProjectGinCtx(), p.ID.Hex())
	if err != nil {
		t.Fatalf("get target: %v", err)
	}
	if !strings.Contains(target, "a.example.com") || !strings.Contains(target, "b.example.com") {
		t.Fatalf("target = %q", target)
	}

	// 再同步一次：两条老目标不该重复，只有新的一条算新增
	added, err = svc.SyncTaskTargets(context.Background(), p.ID.Hex(), []string{"a.example.com", "b.example.com", "c.example.com"})
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if added != 1 {
		t.Fatalf("added = %d, want 1", added)
	}
	target, _ = svc.GetTarget(newProjectGinCtx(), p.ID.Hex())
	if strings.Count(target, "a.example.com") != 1 {
		t.Fatalf("duplicate target saved: %q", target)
	}
}
