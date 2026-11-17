//go:build integration

package assetgroup

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
)

// ARL-Next 对齐：asset_group / asset_scope 的 CRUD、地契解析、批量移动、删除策略
func TestAssetGroupAndScopeLifecycle(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	ctx := context.Background()
	svc := NewService()
	prefix := fmt.Sprintf("parity-%d", time.Now().UnixNano())
	defer func() {
		_, _ = mongodb.DB.Collection("asset_group").DeleteMany(ctx, bson.M{"name": bson.M{"$regex": "^" + prefix}})
		_, _ = mongodb.DB.Collection("asset_scope").DeleteMany(ctx, bson.M{"name": bson.M{"$regex": "^" + prefix}})
	}()

	// 建两个集团
	groupA, err := svc.CreateGroup(ctx, &models.AssetGroupAddRequest{Name: prefix + "-A", Description: "a"})
	if err != nil {
		t.Fatalf("create group A: %v", err)
	}
	groupB, err := svc.CreateGroup(ctx, &models.AssetGroupAddRequest{Name: prefix + "-B"})
	if err != nil {
		t.Fatalf("create group B: %v", err)
	}
	// 名称唯一
	if _, err := svc.CreateGroup(ctx, &models.AssetGroupAddRequest{Name: prefix + "-A"}); err == nil {
		t.Fatal("duplicate group name should fail")
	}

	// 地契：域名 + IP 混合，自动解析并去重
	scopeID, err := svc.CreateScope(ctx, &models.AssetScopeAddRequest{
		Name:    prefix + "-scope",
		Scope:   "a.example.com\nb.example.com\n10.0.0.1\na.example.com",
		GroupID: groupA,
	})
	if err != nil {
		t.Fatalf("create scope: %v", err)
	}
	var stored models.AssetScope
	if err := mongodb.DB.Collection("asset_scope").FindOne(ctx, bson.M{"name": prefix + "-scope"}).Decode(&stored); err != nil {
		t.Fatalf("load scope: %v", err)
	}
	if stored.ScopeType != ScopeTypeMixed {
		t.Fatalf("scopeType = %q, want mixed", stored.ScopeType)
	}
	if len(stored.DomainArray) != 2 || len(stored.IPArray) != 1 {
		t.Fatalf("arrays = %v / %v", stored.DomainArray, stored.IPArray)
	}
	if _, err := svc.CreateScope(ctx, &models.AssetScopeAddRequest{Name: prefix + "-bad", Scope: "not a domain!"}); err == nil {
		t.Fatal("invalid scope should fail")
	}

	// 集团列表带 scopeCount
	groups, err := svc.ListGroups(ctx)
	if err != nil {
		t.Fatalf("list groups: %v", err)
	}
	countA := int64(0)
	for _, g := range groups {
		if g.ID.Hex() == groupA {
			countA = g.ScopeCount
		}
	}
	if countA != 1 {
		t.Fatalf("scopeCount = %d, want 1", countA)
	}

	// 绑定企业 + 写错误后清理
	if err := svc.BindEnterprise(ctx, scopeID, "Example Corp"); err != nil {
		t.Fatalf("bind enterprise: %v", err)
	}
	if _, err := mongodb.DB.Collection("asset_scope").UpdateOne(ctx, bson.M{"name": prefix + "-scope"}, bson.M{"$set": bson.M{"update_error": "icp failed"}}); err != nil {
		t.Fatalf("seed update_error: %v", err)
	}
	if err := svc.ClearUpdateErrors(ctx, []string{scopeID}); err != nil {
		t.Fatalf("clear error: %v", err)
	}

	// 批量移动到 B
	if err := svc.MoveScopes(ctx, []string{scopeID}, groupB); err != nil {
		t.Fatalf("move scope: %v", err)
	}
	items, total, err := svc.ListScopes(ctx, groupB, prefix, 1, 20)
	if err != nil {
		t.Fatalf("list scopes: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].Enterprise != "Example Corp" {
		t.Fatalf("scopes after move = %+v total=%d", items, total)
	}
	if items[0].UpdateError != "" {
		t.Fatalf("update_error = %q, want empty", items[0].UpdateError)
	}

	// 删除 B：transfer 到 A
	if err := svc.DeleteGroup(ctx, &models.AssetGroupDeleteRequest{ID: groupB, Action: "transfer", TargetGroupID: groupA}); err != nil {
		t.Fatalf("delete group B: %v", err)
	}
	var afterTransfer models.AssetScope
	if err := mongodb.DB.Collection("asset_scope").FindOne(ctx, bson.M{"_id": stored.ID}).Decode(&afterTransfer); err != nil {
		t.Fatalf("reload scope: %v", err)
	}
	if afterTransfer.GroupID != groupA {
		t.Fatalf("groupID after transfer = %q, want %s", afterTransfer.GroupID, groupA)
	}
	// 删除 A：dissolve → 未分组
	if err := svc.DeleteGroup(ctx, &models.AssetGroupDeleteRequest{ID: groupA, Action: "dissolve"}); err != nil {
		t.Fatalf("delete group A: %v", err)
	}
	if err := mongodb.DB.Collection("asset_scope").FindOne(ctx, bson.M{"_id": stored.ID}).Decode(&afterTransfer); err != nil {
		t.Fatalf("reload scope: %v", err)
	}
	if afterTransfer.GroupID != "" {
		t.Fatalf("groupID after dissolve = %q, want empty", afterTransfer.GroupID)
	}
}
