package system

// 配置自检：一次列出「该配的东西有没有真的接上」。
//
// 起因（2026-11-26）：一天之内踩了三次同一类坑——
//   · 6 个扫描模板里 5 个模块是 null，任务照跑但爬虫/目录扫描没执行，界面上看不出来；
//   · FOFA key 存在 config 里，但 subfinder 的 provider 列表是空的，key 从没被用上；
//   · GitHub 泄露监控页面空白，因为既没有任务也没有 token。
// 共同点：配置项存在，但没接到消费者上。这个端点把「有没有接上」变成可查的事实。

import (
	"context"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/services/jev"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

var coreModules = []string{"SubdomainScan", "PortScan", "PortFingerprint", "AssetMapping",
	"AssetHandle", "URLScan", "WebCrawler", "URLSecurity", "DirScan", "VulnerabilityScan"}

type checkItem struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// ConfigCheck 只读自检，不改任何东西。
func ConfigCheck(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	items := []checkItem{}
	if mongodb.DB == nil {
		response.Success(c, gin.H{"ok": false, "items": items}, "api.success")
		return
	}

	// 1) 模板完整性
	var tplMissing int
	var tplTotal int
	var badNames []string
	cur, _ := mongodb.DB.Collection("ScanTemplates").Find(ctx, bson.M{})
	if cur != nil {
		var docs []bson.M
		_ = cur.All(ctx, &docs)
		_ = cur.Close(ctx)
		tplTotal = len(docs)
		for _, d := range docs {
			miss := 0
			for _, m := range coreModules {
				arr, _ := d[m].(bson.A)
				if len(arr) == 0 {
					miss++
				}
			}
			if miss > 0 {
				tplMissing++
				if n, _ := d["name"].(string); n != "" && len(badNames) < 5 {
					badNames = append(badNames, n)
				}
			}
		}
	}
	items = append(items, checkItem{"templates", tplMissing == 0,
		strings.TrimSpace(strings.Join([]string{
			"total=" + itoa(tplTotal), "incomplete=" + itoa(tplMissing), strings.Join(badNames, ","),
		}, " "))})

	// 2) subfinder provider：有 key 却没接进去的，单独点出来
	var sfVal string
	if err := mongodb.DB.Collection("config").FindOne(ctx, bson.M{"name": "SubfinderApiConfig"}).Decode(&bson.M{}); err == nil {
		var doc bson.M
		if mongodb.DB.Collection("config").FindOne(ctx, bson.M{"name": "SubfinderApiConfig"}).Decode(&doc) == nil {
			sfVal, _ = doc["value"].(string)
		}
	}
	wired := 0
	if sfVal != "" {
		for _, line := range strings.Split(sfVal, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") || !strings.Contains(line, ":") {
				continue
			}
			if !strings.HasSuffix(line, "[]") && strings.Contains(line, "[") {
				wired++
			}
		}
	}
	var keyDocs []bson.M
	if kc, err := mongodb.DB.Collection("config").Find(ctx, bson.M{"type": bson.M{"$in": []string{"fofa", "github", "llm", "openai"}}}); err == nil {
		_ = kc.All(ctx, &keyDocs)
		_ = kc.Close(ctx)
	}
	items = append(items, checkItem{"subfinder_providers", wired > 0,
		"wired=" + itoa(wired) + " stored_keys=" + itoa(len(keyDocs))})

	// 3) GitHub 泄露监控
	gt, _ := mongodb.DB.Collection("github_task").CountDocuments(ctx, bson.M{})
	gtok, _ := mongodb.DB.Collection("config").CountDocuments(ctx, bson.M{"name": "github.token"})
	items = append(items, checkItem{"github_monitor", gt > 0 && gtok > 0,
		"tasks=" + itoa(int(gt)) + " token=" + itoa(int(gtok))})

	// 4) 节点与队列
	nodes, _ := mongodb.DB.Collection("node").CountDocuments(ctx, bson.M{})
	items = append(items, checkItem{"nodes", nodes > 0, "nodes=" + itoa(int(nodes))})

	// 5) LLM
	items = append(items, checkItem{"llm", jev.Enabled(),
		"provider=" + jev.ProviderName() + " enabled=" + boolStr(jev.Enabled())})

	ok := true
	for _, it := range items {
		if !it.OK {
			ok = false
		}
	}
	response.Success(c, gin.H{"ok": ok, "items": items}, "api.success")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte(48 + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
