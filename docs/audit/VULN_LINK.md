# Nuclei → vulnerability 落库链路排查（生产）

日期：2026-11-19　环境：prod-tx-sh-app-02（服务端 v2.0.117 / 节点扫描镜像 latest）
范围：只读排查 + 一次 example.com 小任务 + 已清理的临时夹具（canary 模板/本地监听）
结论摘要：**服务端没有"消费端"可实现，漏洞是节点直写 Mongo；0 条的真正原因是模板参数 `-t *` 在节点侧解析成目录后一个模板都没执行**。本仓库内另修好了两处读模型键名不匹配。

---

## 1. 链路（每一跳）

```
服务端 ScopeSentry                               节点 ScopeSentry-Scan                存储
─────────────────────────                        ──────────────────────              ─────
POST /api/task/add  ──► task 文档 (Mongo task)
POST /api/task/start
  └─ CreateTaskScan
       ├─ ClearTaskCache(taskID)
       ├─ PushTaskInfoList(taskID, targets) ────► Redis list  TaskInfo:<taskID>
       ├─ GetScanTemplate(task)  ← 组装 `-t *` / `-t <id>.yaml,...`
       └─ RPushNodeTask(node, template) ────────► Redis list  NodeTask:<node>
                                                  │
                                                  ▼
                                           节点轮询 NodeTask:<node>
                                             ├─ ProgressStart/End ──► Redis hash TaskInfo:progress:<taskID>:<target>
                                             ├─ 插件日志 ───────────► Redis list logs:plugins:<module>:<pluginHash>
                                             └─ VulnerabilityScan 模块
                                                  └─ nuclei 插件 (ed93b8af6b72fe54a60efdb932cf6fbc)
                                                       └─ results.Handler.Vulnerability()
                                                            └─ ResultQueues["VulnerabilityScan"]
                                                                 └─ flush 30s/58条
                                                                      └─ InsertVulnerabilityScan()
                                                                           ├─ Mongo vulnerability
                                                                           └─ Mongo vulnerabilityDetail
服务端只读：
  /api/assets/vulnerability           ── internal/services/assets/vulnerability + repositories/assets/vulnerability
  /api/assets/nuclei_result/list      ── internal/services/assets/nucleiresult + repositories/assets/nucleiresult
  /api/dashboard、导出、项目聚合        ── 直接 count/find 同一批集合
```

**结论 1：Redis 里没有任何"漏洞结果队列"键。** 键空间普查（74 keys）只有：
`NodeTask:<node>`、`TaskInfo:<taskID>`、`TaskInfo:progress:<taskID>:<target>`、`TaskInfo:time|tmp:<taskID>`、
`duplicates:<taskID>:<kind>`、`logs:plugins:<module>:<hash>`、`log:<node>`、`node:<name>`、`NodePlg:<name>`、`param:<domain>`。
漏洞结果 **由节点进程直连 Mongo 写入**，不经过 Redis。

**结论 2：服务端 `internal/services/assets/vulnerability` 及其 repository/handler 是纯读**：接口只有
`GetVulnerabilities` / `GetVulnerabilityDetailByHash` → `CountDocuments` / `Find` / `FindDetailByHash`。
仓库里没有任何向 `vulnerability` 写入的代码（除 update18 的历史迁移）。所以"服务端消费端未实现"不是缺陷，是设计。

---

## 2. 生产只读验证（2026-11-19 02:37–03:55）

| 项目 | 结果 | 证据 |
|---|---|---|
| `db.vulnerability` / `db.vulnerabilityDetail` | 0 / 0 | `countDocuments` |
| 模板 `artex-recon-light-20261116` 的 VulnerabilityScan | 已启用：插件 hash `ed93b8af…`，参数 `-s critical,high,medium`，`vullist:["All Poc"]` | `db.ScanTemplates.find()` |
| 节点实际收到的参数 | `"-s critical,high,medium  -t *"`（`-t *` 由 `GetScanTemplate` 因 `All Poc` 追加） | `log:node-tx-sh-02` 里 `Get a new task: {...}` |
| 插件日志键 | `logs:plugins:VulnerabilityScan:ed93b8af6b72fe54a60efdb932cf6fbc` 存在，nuclei 每次 start/run time 都有 | `lrange` |
| 节点 POC 目录 | `/apps/poc` 14319 个 `<pocID>.yaml` | `ls \| wc -l` |
| 服务端消费错误日志 | 无（本就没有消费端） | `log:node-tx-sh-02` 无 insert/parse 报错 |

节点侧对同一个 target 的典型耗时：nuclei `start` → `run time 5~13 分钟`，但期间 **没有任何命中**。

---

## 3. 断点定位（关键实验）

为区分"真的没漏洞"和"模板没执行"，在节点 POC 目录放了一个必然命中的无害夹具模板
`zz-audit-canary-local.yaml`（severity=medium，请求 `http://127.0.0.1:18099/audit-canary`，
宿主上起了一个只记 path 的本地监听）。三条对照：

| 实验 | VulnerabilityScan 参数 | nuclei 表现 | 监听收到请求 | Mongo 落库 |
|---|---|---|---|---|
| A `-t *`（现网模板实际形态） | `-s critical,high,medium -t *` | 跑满 5m00s，结束时无日志错误 | **0** | 0 |
| B 无 `-t`（走 `TemplatesDirectory`） | `-s critical,high,medium` | 20+ 分钟不结束，节点被卡住（后续任务不再被领取） | 0 | 0 |
| C 显式文件 | `-s ... -t /apps/poc/zz-audit-canary-local.yaml` | **957ms** 结束 | **2 次** | **2 条** `vulnerability` + 2 条 `vulnerabilityDetail` |

C 的落库文档（原样）：

```json
{ "vulnid": "zz-audit-canary-local", "vulname": "ZZ Audit Canary Local Listener",
  "level": "medium", "url": "https://www.example.com", "matched": "http://127.0.0.1:18099/audit-canary",
  "taskName": "audit-e1b-…", "rootdomain": "example.com", "project": "", "tags": null, "status": 1 }
```

**断点 1（主因，节点侧）**：`-t *` 被节点映射成 `templates = []string{global.PocDir}`
（`modules/vulnerabilityscan/nuclei/nuclei.go`），再交给 `nuclei.WithTemplatesOrWorkflows(TemplateSources{Templates: [...]})`。
实测这个"目录形态"在节点内置的 nuclei fork 上 **一个模板都不会执行**，且不报错——于是 5 分钟空转、
vulnerability 恒为 0。显式文件则秒级命中并成功落库，说明 **节点→Mongo 的写入链路本身是通的**。

**断点 2（本仓库，已修）**：读模型与写侧键名不一致：
- `vulnerability` 文档里是 `vulname`（节点 `types.VulnResult` 无 bson tag → 字段名小写），
  读模型却解码 `bson:"vulnname"` → 列表 `name` 恒为空；
- 文档里是 `rootdomain`（节点 `VulnResult.RootDomain` 只打了 `yaml:"rootDomain"`，bson 落到小写），
  读模型/索引/关键词检索用 `rootDomain` → `rootDomain` 恒为空、按根域名搜不到、`{rootdomain:1}` 索引建了也用不上。

生产上直接可见：同两条数据，`GET /api/assets/nuclei_result/list` 返回 `"name":"", "rootDomain":""`，
而 Mongo 里是 `vulname/rootdomain` 有值。

**断点 3（稳定性，节点侧）**：不指定 `-t` 时 nuclei 加载 14319 个模板会长时间不返回，
且该 goroutine 不可取消——实测把节点卡死（后续 `NodeTask` 不再被消费），需要重启容器。

---

## 4. 修复

### 4.1 本仓库（已改，`go build ./...` 通过，单测通过）

1. `internal/repositories/assets/nucleiresult/nuclei_result.go`
   - `nucleiDoc` 同时解码两种拼写：`vulname`/`vulnname`、`rootdomain`/`rootDomain`，
     新增 `effectiveVulName()` / `effectiveRootDomain()`，列表与详情统一走它们；
   - 列表 projection 同时取两套键，老数据不丢字段。
2. `internal/services/assets/nucleiresult/nuclei_result.go`
   - keyword 过滤 `$or` 增加 `rootdomain` 分支（原来只有 `rootDomain`），按根域名搜索才能命中新数据。
3. `internal/database/mongodb/initdb.go`
   - `vulnerability` 上补 `{rootdomain:1}` 索引（实际写入键），保留老 `{rootDomain:1}`。
4. `internal/utils/helper/search.go`
   - `SearchKey["vulnerability"]` 增加 `rootdomain`/`rootDomain`，搜索 DSL 可直接按根域名过滤。
5. 新增单测
   - `internal/repositories/assets/nucleiresult/nuclei_result_keymismatch_unit_test.go`
   - `internal/services/assets/nucleiresult/nuclei_result_rootdomain_unit_test.go`

验证：
```
go build ./...                                   # OK
go test ./internal/repositories/assets/nucleiresult/ ./internal/services/assets/nucleiresult/ -run 'TestNucleiDoc|TestBuildListFilter'   # ok ok
```

### 4.2 节点侧（ScopeSentry-Scan，另一仓库，本仓库无法代改）

主因在节点。最小改动（两处）：

```diff
--- a/modules/vulnerabilityscan/nuclei/nuclei.go
+++ b/modules/vulnerabilityscan/nuclei/nuclei.go
@@ case "t":
-    if value == "*" {
-        templates = []string{global.PocDir}
-    } else {
-        templates = strings.Split(value, ",")
-    }
+    if value == "*" {
+        // 目录形态在 SDK 里不会被执行（实测 0 模板），必须展开成文件/通配
+        templates = []string{filepath.Join(global.PocDir, "*.yaml")}
+    } else {
+        templates = strings.Split(value, ",")
+    }
@@ // Execute 结束时
+    p.Log(fmt.Sprintf("nuclei finished, templates=%d targets=%d", len(templates), len(targets)))

--- a/internal/types/types.go
+++ b/internal/types/types.go
-    RootDomain string `yaml:"rootDomain"`
+    RootDomain string `bson:"rootDomain" yaml:"rootDomain"`
```

注意：`*.yaml` 通配是否被该 fork 正常展开需先在测试节点验证；已验证的等价路径是
"显式文件列表"（本报告实验 C）。若通配仍不生效，则退化为把 `All Poc` 展开成 `<pocID>.yaml` 列表
（POC 文件名就是 `<pocID>.yaml`），但要评估参数长度上限。

### 4.3 配置建议（不改代码也能缓解）

- 需要"扫指定 POC"时，模板的 `vullist` 填具体 POC（`-t <pocID>.yaml,...` 形态）——这条路径实测可用。
- `All Poc` 在节点修复前不要指望出结果；临时可把 `vullist` 指向少量关键 POC。
- severity 过滤 `-s critical,high,medium` 会把 info/low 模板全部排除；对 example.com 这类站点，
  这也是为什么"看起来什么都没扫到"——验证链路时建议临时带上 `info`（但当前主因是模板没执行，不是过滤）。

---

## 5. 验证与收尾

- 实验 C 证明链路端到端可用：nuclei 命中 → 节点 `results.Handler.Vulnerability` → 30s 内 `vulnerability`
  + `vulnerabilityDetail` 各 2 条 → 服务端两个列表接口都能读到（`total:2`）。
- 同一批数据暴露了断点 2：接口 `rootDomain/name` 为空，本仓库修复后由 `effective*` 回退取值。
- 生产清理（已完成）：删除测试任务 `audit-vuln-link-*`/`audit-canary-*`/`audit-e2-notpl-*`/`audit-e1b-*`、
  删除测试模板 `audit-fixture-*`、删除测试产生的 2+2 条漏洞数据、移除节点 `/apps/poc` 下的 canary 夹具
  （目录恢复 14319 个文件）、关闭本地监听、节点容器恢复运行。
  现状：`task=7`、`ScanTemplates=4`、`vulnerability=0`、`vulnerabilityDetail=0`。
- 排查中因临时实验（实验 B）把节点卡死后重启过 `scopesentry-scan`；重启后节点名回落为配置文件里的
  `node-test`，已改回 `node-tx-sh-02`（`/apps/config/config.yaml`，备份 `config.yaml.bak.audit`），
  与历史 `log:node-tx-sh-02`/`TaskInfo:progress:*` 记录一致。

## 6. 环境限制

- 本次所有实验都用无害目标（example.com）与本地 canary，未对第三方发起扫描。
- 节点内置 nuclei fork 版本与上游 CLI 行为不一致（本地 nuclei v3.11.1 用同一模板可正常命中），
  目录/默认模板目录的行为需要在节点仓库复现后再定补丁形态；本报告给出的是经验证的方向而非已部署修复。
- 生产禁止部署，故 4.1 的代码修复只做了本地构建与单测，未上线。
