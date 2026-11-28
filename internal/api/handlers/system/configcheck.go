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
	dbredis "github.com/Autumn-27/ScopeSentry/internal/database/redis"
	"github.com/Autumn-27/ScopeSentry/internal/services/jev"
	"github.com/Autumn-27/ScopeSentry/internal/services/watchdog"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// criticalModules 少了就跑不出东西的模块：没有子域/端口/URL 就没有资产面。
// optionalModules 是增强项：很多模板（尤其 recon 档）故意不配，缺了不算错。
// VulnerabilityScan 不在任何一类里：nuclei 插件是在组装扫描载荷时注入的
// （见 services/task/common.GetScanTemplate 里往 Parameters.VulnerabilityScan 塞 nuclei 与 -t），
// 模板文档里本来就该是空的 —— 早先把它算成必需，导致自检永远红：7 个模板必然「全部不完整」。
var criticalModules = []string{"SubdomainScan", "PortScan", "URLScan"}

var optionalModules = []string{"PortFingerprint", "AssetMapping", "AssetHandle",
	"WebCrawler", "URLSecurity", "DirScan"}

// moduleConfigured 判断模板里某个模块是否真的配了东西。
//
// 踩过的坑（2026-11-29）：模板文档的真实结构是 Parameters.<模块> = {插件hash: 配置}，
// 而这里原先按顶层 d[模块].(bson.A) 查数组 —— 类型断言永远失败，于是「7 个模板全部不完整」
// 这种吓人的结论其实是自检自己在骗人。现在优先读 Parameters，并兼容顶层数组的老文档。
// 只看 Parameters：扫描载荷就是从这里取的（GetScanTemplate → ParameterParser）。
// 早先还回退到顶层 doc[模块]，但顶层那个镜像往往有值、Parameters 却是空的 ——
// 会让自检报「配好了」而实际跑起来是空的（反向误报比误报更糟）。
func moduleConfigured(params bson.M, module string) bool {
	if params == nil {
		return false
	}
	v, ok := params[module]
	return ok && nonEmptyModule(v)
}

// nonEmptyModule 数组、映射、文档三种形态都算，空值一律不算。
func nonEmptyModule(v interface{}) bool {
	switch x := v.(type) {
	case bson.A:
		return len(x) > 0
	case bson.M:
		return len(x) > 0
	case bson.D:
		return len(x) > 0
	case []interface{}:
		return len(x) > 0
	case map[string]interface{}:
		return len(x) > 0
	}
	return false
}

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
	var critNotes []string
	var optNotes []string
	cur, _ := mongodb.DB.Collection("ScanTemplates").Find(ctx, bson.M{})
	if cur != nil {
		var docs []bson.M
		_ = cur.All(ctx, &docs)
		_ = cur.Close(ctx)
		tplTotal = len(docs)
		for _, d := range docs {
			params, _ := d["Parameters"].(bson.M)
			name, _ := d["name"].(string)
			var critMissing, optMissing []string
			for _, m := range criticalModules {
				if !moduleConfigured(params, m) {
					critMissing = append(critMissing, m)
				}
			}
			for _, m := range optionalModules {
				if !moduleConfigured(params, m) {
					optMissing = append(optMissing, m)
				}
			}
			if len(critMissing) > 0 {
				tplMissing++
				if name != "" && len(critNotes) < 5 {
					critNotes = append(critNotes, name+"("+strings.Join(critMissing, ",")+")")
				}
			}
			if len(optMissing) > 0 && name != "" && len(optNotes) < 5 {
				optNotes = append(optNotes, name+"("+strings.Join(optMissing, ",")+")")
			}
		}
	}
	tplDetail := "total=" + itoa(tplTotal) + " critical_missing=" + itoa(tplMissing)
	if len(critNotes) > 0 {
		tplDetail += " " + strings.Join(critNotes, " ")
	}
	if len(optNotes) > 0 {
		// 未配的增强模块只做信息，不影响结论：recon 档故意不配它们
		tplDetail += " optional_not_configured: " + strings.Join(optNotes, " ")
	}
	items = append(items, checkItem{"templates", tplMissing == 0, tplDetail})

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

	// 4) 节点与队列：节点在线状态在 Redis（node:<name> 哈希），不在 Mongo。
	// 之前查 Mongo 的 node 集合永远是 0，会误报「没有节点」。
	nodes := 0
	fresh := 0
	queue := 0
	staleNames := []string{}
	knownNodes := map[string]bool{}
	orphanQueues := []string{}
	if dbredis.Client != nil {
		if keys, err := dbredis.Client.Keys(ctx, "node:*").Result(); err == nil {
			for _, k := range keys {
				if strings.Contains(k, ":currentTask") {
					continue
				}
				knownNodes[strings.TrimPrefix(k, "node:")] = true
				if t, terr := dbredis.Client.Type(ctx, k).Result(); terr != nil || t != "hash" {
					continue
				}
				nodes++
				// 只数 key 是不够的：2026-11-26 一个节点卡死后心跳停在 20:15，
				// 这里仍然报 nodes=1 ok，队列被堵了半小时没人发现。
				// 所以按 updateTime 判新鲜度，僵尸节点单独列出来。
				upd, herr := dbredis.Client.HGet(ctx, k, "updateTime").Result()
				last, perr := time.ParseInLocation("2006-01-02 15:04:05", upd, time.Local)
				if herr == nil && perr == nil && time.Since(last) < watchdog.StaleAfter {
					fresh++
				} else {
					staleNames = append(staleNames, strings.TrimPrefix(k, "node:"))
				}
			}
		}
		if qk, err := dbredis.Client.Keys(ctx, "NodeTask:*").Result(); err == nil {
			for _, k := range qk {
				n, lerr := dbredis.Client.LLen(ctx, k).Result()
				if lerr != nil {
					continue
				}
				queue += int(n)
				// 孤儿队列：key 里的节点名不在节点集合里（节点改名/删除后的遗留）。
				// 这些任务永远没有节点会去 LPOP，界面却一直显示「排队中」——
				// 也是「最老任务等了 124 小时」这类现象的一种来源。
				if name := strings.TrimPrefix(k, "NodeTask:"); n > 0 && !knownNodes[name] {
					orphanQueues = append(orphanQueues, name+"("+itoa(int(n))+")")
				}
			}
		}
	}
	// 队列里最老的任务等了多久：只看数量看不出「33 个里有 23 个是 5 天前的」，
	// 而后者才是队列永远不流动的真正原因。
	oldest := ""
	if mongodb.DB != nil {
		var t struct {
			CreatTime string `bson:"creatTime"`
		}
		opts := options.FindOne().SetSort(bson.M{"creatTime": 1}).SetProjection(bson.M{"creatTime": 1})
		if err := mongodb.DB.Collection("task").FindOne(ctx, bson.M{"status": 0}, opts).Decode(&t); err == nil && t.CreatTime != "" {
			if created, perr := time.ParseInLocation("2006-01-02 15:04:05", t.CreatTime, time.Local); perr == nil {
				oldest = " oldest=" + time.Since(created).Round(time.Hour).String()
			}
		}
	}
	detail := "nodes=" + itoa(nodes) + " fresh=" + itoa(fresh) + " queued_tasks=" + itoa(queue) + oldest
	if len(staleNames) > 0 {
		detail += " stale=" + strings.Join(staleNames, ",")
	}
	if len(orphanQueues) > 0 {
		detail += " orphan_queues=" + strings.Join(orphanQueues, ",")
	}
	// 有孤儿队列说明有任务永远不会被领取，不能算 ok
	items = append(items, checkItem{"nodes", fresh > 0 && len(orphanQueues) == 0, detail})

	// 5) LLM
	// 判定后端：agent/harness 优先，其次是 LLM。两者都配了才算完整。
	// agentMode: external（配了 AGENT_VERIFIER_CMD）/ builtin（JEV_AGENT=builtin）/ ""（纯模型）。
	// 三者的能力差别很大：external/builtin 会真的重打一遍请求，纯模型只能读旧证据。
	agentMode := jev.AgentMode()
	if agentMode == "" {
		agentMode = "none"
	}
	items = append(items, checkItem{"llm", jev.Enabled() || agentMode != "none",
		"agent=" + agentMode + " provider=" + jev.ProviderName() + " enabled=" + boolStr(jev.Enabled())})

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
