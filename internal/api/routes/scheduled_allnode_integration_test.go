//go:build integration

package routes

// 计划任务创建的两条边界：
//  1) allNode=true + node=[] → 允许（服务层 CreateTaskScan 会补齐全部节点）
//  2) allNode=false + node=[] → 400，且文案指向「节点」，不再是「目标不能为空」
// 第 1 条以前是被双重拦死的：前端 required 规则 + 后端 len(node)==0，
// 于是界面上「自动加入全部在线节点」那个开关点开也没用。

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"go.mongodb.org/mongo-driver/bson"
)

func TestScheduledAddAllNodeWithoutNodeList(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)
	stamp := time.Now().UnixNano()
	name := fmt.Sprintf("it-allnode-%d", stamp)

	w := postJSON(t, router, token, "/api/task/scheduled/add", map[string]interface{}{
		"name": name, "node": []string{}, "allNode": true,
		"cycleType": "nhours", "hour": 24, "scheduledTasks": false,
		"target": "it-allnode.example.com", "targetSource": "general",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("allNode=true 且不传 node 应当允许，实际 %d: %s", w.Code, w.Body.String())
	}
	t.Cleanup(func() {
		_, _ = mongodb.DB.Collection("ScheduledTasks").DeleteMany(t.Context(), bson.M{"name": bson.M{"$regex": "^" + name}})
	})

	// 落库的 allNode 必须是 true，否则调度时补不齐节点
	var doc struct {
		AllNode bool `bson:"allNode"`
	}
	if err := mongodb.DB.Collection("ScheduledTasks").FindOne(t.Context(), bson.M{"name": name}).Decode(&doc); err != nil {
		t.Fatalf("读计划任务失败: %v", err)
	}
	if !doc.AllNode {
		t.Fatalf("allNode 没有落库")
	}

	// allNode=false 且没有节点：必须 400，且提示节点
	bad := postJSON(t, router, token, "/api/task/scheduled/add", map[string]interface{}{
		"name": name + "-bad", "node": []string{}, "allNode": false,
		"scheduledTasks": false, "target": "it-allnode.example.com",
	})
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("没节点且未开 allNode 应当 400，实际 %d: %s", bad.Code, bad.Body.String())
	}
	// 文案由 go-i18n 渲染（中文/英文都可能有），两种写法都认；
	// 关键是「指向节点」而不是以前的「目标不能为空」
	body := bad.Body.String()
	if !strings.Contains(body, "节点") && !strings.Contains(strings.ToLower(body), "node") {
		t.Fatalf("错误文案应指向节点，实际: %s", body)
	}
	if strings.Contains(body, "目标不能为空") || strings.Contains(body, "Target cannot be empty") {
		t.Fatalf("错误文案仍在说目标为空: %s", body)
	}
}
