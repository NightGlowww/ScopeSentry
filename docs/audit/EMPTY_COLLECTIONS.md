# ScopeSentry 空集合定性审计（EMPTY_COLLECTIONS）

- 审计对象：`/Users/night/Workspaces/security_workspace/_tools/ScopeSentry`（Go + Gin + Vue3，生产 `prod-tx-sh-app-02`）
- 审计时间：2026-11-19 04:10 — 05:05 (+08)
- 只读约束：未改产品代码、未部署、未重启、未写库；唯一写入为本文件
- 取证方式：生产 `mongosh` 只读查询（count / find / aggregate）+ 仓库静态检索（文件:行）
- 前置文档：`docs/audit/ISLANDS.md`

---

## 0. 结论摘要

生产库实测 **57 个集合**（含 `fs.chunks` / `fs.files` 两个 GridFS 系统集合），其中 **23 个为 0 文档**。

| 定性 | 数量 | 集合 |
| --- | ---: | --- |
| **A 生产者存在、尚未产出** | 19 | `PageMonitoring` `PageMonitoringBody` `TagColor` `asset_group` `asset_scope` `dict_upload_tasks` `export` `fofa_import` `github_result` `github_task` `github_threat_history` `icp_result` `icp_task` `icp_task_log` `notification` `scan_policy` `screenshot` `vulnerability` `vulnerabilityDetail` |
| **A（中间态，健康空表）** | 1 | `IPAssetTmp` |
| **B 生产者从未实现（只有读）** | 2（+2 集合不存在） | `app` `mp`；另有 `asset_fileleak` `asset_npoc_service` 连集合都未创建 |
| **C 已被替代 / 应删除** | 1 | `icon` |

> 计数口径：空集合 23 = A 19 + A中间态 1 + B 2 + C 1。`asset_fileleak` `asset_npoc_service` 两个 B 类实体因集合不存在，不计入 23。

**与 ISLANDS.md 的三处硬冲突（本报告以实测为准）**：

| 项 | ISLANDS.md | 本次实测 | 影响 |
| --- | --- | --- | --- |
| 集合总数 / 空集合 | 56 / 22 | **57 / 23** | ISLANDS 汇总行少数 1 个；`IPAssetTmp` 从 1 条掉到 0 条 |
| 节点代理 7890 | `proxy_7890_closed` | **PROXY_OPEN** | 「nuclei 因代理不通而 0 结果」的根因判定失效，真实原因是扫描目标本身无洞 |
| `asset.tls` 非空 | 117 / 123 | **2 / 125** | cert 读模型实际只有 2 行，不是「闭环良好」 |

---

## 1. 全集合计数（只读实测）

**非空（34）**：`UrlScan` 30400、`subdomain` 23975、`PocList` 14338、`FingerprintRules` 6863、`dictionary_entry` 709622、`mcp_call_logs` 4610、`audit_logs` 1065、`SensitiveResult` 207、`SensitiveRule` 172、`asset` 125、`SensitiveBody` 105、`DirScanResult` 44、`fs.chunks` 44、`fs.files` 28、`dictionary` 28、`IPAsset` 19、`TaskProgressSnapshot` 16、`plugins` 16、`HttpBody` 11、`AssetChangeLog` 10、`config` 10、`task` 7、`RootDomain` 5、`ScanTemplates` 4、`PortDict` 3、`ProjectTargetData` 2、`api_key` 2、`project` 2、`ScheduledTasks` 1、`scheduled` 1、`github_threat_config` 1、`mcp_config` 1、`user` 1、`crawler` 1

**空（23）**：`IPAssetTmp` `PageMonitoring` `PageMonitoringBody` `TagColor` `app` `asset_group` `asset_scope` `dict_upload_tasks` `export` `fofa_import` `github_result` `github_task` `github_threat_history` `icon` `icp_result` `icp_task` `icp_task_log` `mp` `notification` `scan_policy` `screenshot` `vulnerability` `vulnerabilityDetail`

---

## 2. 逐集合定性表

> 定性口径
> **A** = 写入代码存在且路径可达（路由已注册 / 插件已挂模板 / 调度已启用），只是当前没有数据
> **B** = 只有读路径，全仓不存在任何 Insert/Update；或上游字段从未被任何代码填充
> **C** = 曾被使用，但已被新设计取代，继续保留只会造成误读

| 集合名 | 计数 | 定性 | 证据（文件:行 / 运行时） | 建议动作 |
| --- | ---: | :--: | --- | --- |
| `IPAssetTmp` | 0 | A（健康中间态） | 写：节点 `PortScan`/`PortFingerprint`；读+删：`internal/worker/ip_asset.go:344`（Find）、`:385`（DeleteMany）。唯一索引 `update18.go:230` | **保持**。0 条 = 队列已排空，是正常态，不要删 |
| `PageMonitoring` | 0 | A | 插件已定义 `internal/constants/defaults.go:248-249`（hash `e52b8b16…`），但 4 个模板的 `URLSecurity` 实测为 `["1aa212b9…"]` / `["2949994c…","1aa212b9…"]` / `null`，**均不含 e52b8b16**。人工入口 `/api/task/scheduled/pagemonit/add` 存在（`repositories/task/scheduler/scheduler.go:105-142`） | **补生产者**：把 `e52b8b16` 加入全量模板 `URLSecurity`；再补一条 seed URL |
| `PageMonitoringBody` | 0 | A | 生产者为同一个 PageMonitoring 插件；消费 `internal/repositories/assets/page_monitoring/page_monitoring.go:32` | 级联于上一条；随插件启用自然产生 |
| `TagColor` | 0 | A（设计如此） | 写：`internal/repositories/assets/tagcolor/tagcolor.go:63`（UpdateOne 带 SetUpsert(true)）；路由 `/api/assets/tag_color/set` | **保持**。空表即「全部用默认色」，非缺陷 |
| `asset_group` | 0 | A | 写：`internal/repositories/assets/assetgroup/repository.go:140`（CreateGroup）；另有 `icp/sync.go:120`（MergeDomains）作为第二写入方 | **保持 + 接页面**；ICP 一旦跑通会自动填充 |
| `asset_scope` | 0 | A | 写：`assetgroup/repository.go:237`（CreateScope）；`icp/sync.go` 合并域名时写入 | 同上 |
| `dict_upload_tasks` | 0 | A | 写：`internal/repositories/dictionary/upload_task.go:42`（Upsert）；路由 `/dictionary/upload_large`、`/dictionary/upload_status`（`routes/dictionary/dictionary.go:24-25`） | **保持**；大字典上传入口从未使用 |
| `export` | 0 | A | 写：`internal/repositories/export/export.go:60`（InsertOne）；handler `handlers/export/export.go:94` 调 CreateExportTask；8 条路由已注册 | **保持**。0 条仅说明从未点过导出 |
| `fofa_import` | 0 | A | 写：`internal/repositories/fofa/import.go:46`（Create）；5 条路由 `routes/fofa/fofa.go:17-21` 全注册 | **保持**；FOFA 功能整体未启用，可在 UI 标注「未配置 key」 |
| `github_result` | 0 | A（开关关闭） | 写：`internal/repositories/github/result.go:65`（UpsertHits）；实测 `github_threat_config` 中 cve/hacker/tools 三个 enabled 全为 false | **补配置或下线入口**；三开关全 false，调度跑也 0 产出 |
| `github_task` | 0 | A | 写：`internal/repositories/github/task.go:75`（Create）；路由 `/api/github/task/create` | 保持；从未建过情报任务 |
| `github_threat_history` | 0 | A（开关关闭） | 写：`internal/repositories/github/threat.go:258`（UpsertHistory）；依赖同一组 enabled 开关 | 与 `github_result` 同批处置 |
| `icon` | 0 | **C** | 消费：`internal/worker/iconhandle.go:54` 读 fav3/content；但 v18 起图标改存**资产文档** `asset.iconcontent`（`models/asset.go:38`），由 `update18.go:351-355` 解码 base64 写盘后 `:393` 清空该字段。全仓无任何代码向 `icon` 集合 Insert | **删除集合 + 删除 `worker/iconhandle.go`**；该设计已被 `asset.iconcontent` 取代 |
| `icp_task` | 0 | A | 写：`internal/repositories/icp/task.go:88`（InsertOne）；14 个 handler 全注册（`handlers/icp/handler.go:82-387`：Create/Stop/Restart/Resume/Delete/Log/Statistics/Sync/Backfill/Export） | **补生产者**：建一个 ICP 任务即可激活 3 个集合 |
| `icp_result` | 0 | A（+ 循环依赖） | 写：`icp/result.go:60`（UpsertRecords）。但 `services/icp/provider.go:147-157` 的 Resolve() 在未配置 `icp.provider`/`icp.provider_url` 时**回落 manual，而 manual 读的正是 `icp_result` 自己**（`provider.go:104`） | **补配置**：必须配置 http provider（`config` 里 icp.provider_url / icp.token），否则任务空跑自锁 |
| `icp_task_log` | 0 | A | 写：`internal/repositories/icp/log.go:60`（InsertOne），仅在任务执行时触发 | 级联于 `icp_task` |
| `notification` | 0 | A | **生产者存在**：`services/configuration/service.go:161` AddNotification 调 InsertOne(collNotification)，路由 `/configuration/notification/add` 已注册。空表原因是**从未配置过通知渠道** | **保持**；真正断点不在集合，在投递（见 §4.1） |
| `scan_policy` | 0 | A（+ 无消费方） | 写：`repositories/policy/policy.go:78`（Create）；7 条路由 + MCP 3 个工具（`mcp/tools.go:1407/1440/1466`）。但任务创建链路 `services/task/`、`handlers/task/` 内检索 policyService / Apply 调用 **0 命中**，policy.Apply 只做模板合并、不落任务 | **补生产者 + 打通**：建策略，并让建任务时读取策略；否则是「写了没人用」的配置坟场 |
| `screenshot` | 0 | A | 消费：`internal/worker/screenshot.go:55`（按 hash 读、落盘、`:97` 删除）。同插件 AssetHandle 的兄弟产物 `HttpBody` = **11 条**，而 `screenshot` = 0；`default` 模板 AssetMapping 参数含 `-screenshot true` | **补生产者**：该分支在节点插件内未产出，需在节点侧排查 httpx 截图参数 |
| `vulnerability` | 0 | A（真实运行、零产出） | nuclei 插件 `ed93b8af…` **已挂在全部 4 个模板的 VulnerabilityScan**。节点日志实证运行：target www.baidu.com run time 13m26.7s、target example.com run time 957ms。代理 7890 实测 **PROXY_OPEN** | **补真实目标**：模板与插件都已就绪，缺的是有洞的扫描目标 |
| `vulnerabilityDetail` | 0 | A | 生产者为同一 nuclei 插件；消费 `repositories/assets/nucleiresult/nuclei_result.go:42` | 级联于 `vulnerability` |
| `app` | 0 | **B** | `internal/repositories/assets/app/app.go:14-56` 只定义 CountDocuments / Find，**无任何写方法**；全仓 Insert 列表里无 app。消费方 `/api/assets/app`、导出、MCP 全指向它 | **补生产者或删集合**：建议先隐藏页面；若要保留，需新增节点侧移动应用指纹插件 |
| `mp` | 0 | **B** | `internal/repositories/assets/mp/*.go:14-36` 同样只有 CountDocuments / Find | 同上（小程序资产） |

### 2.1 代码引用但库中从未创建（4 个）

| 集合 | 计数 | 定性 | 证据 | 建议 |
| --- | ---: | :--: | --- | --- |
| `asset_fileleak` | 不存在 | **B** | `internal/services/assets/dataview/service.go:16` 定义常量，**注释原文：「节点侧未来直接写入即可生效」**；repository `dataview/repository.go` 只有 List + DeleteByIDs。导出侧 `services/export/schema.go:189` 也指向它 | **补生产者或下线页面**；否则 `/api/assets/fileleak/list` 与导出每次都在查不存在的集合 |
| `asset_npoc_service` | 不存在 | **B** | 同 `dataview/service.go:17`；导出 `export/schema.go:196` | 同上 |
| `SubdomainTakerResult` | 不存在 | A（潜在） | 读：`repositories/assets/subdomain/taker.go:14`，MCP 白名单 `mcp/tools.go:1123`；写方为节点 SubdomainSecurity/SubdomainTakeover 插件（`c0c71c10…`），该插件在 default 与 artex-recon-full 模板中已挂载 | **补 initdb 建集合+索引**；插件已挂，跑通即有数据 |
| `TaskTargetControl` | 不存在 | A（潜在） | `repositories/task/task/target_control.go:14` | 补 initdb 建集合；或若功能未完成则删引用 |

> 这 4 个集合未出现在 getCollectionNames() 中，且 `initdb.go` 全文只有 1 处 CreateCollection（`:171` 仅 notification），其余集合靠「首次写入时隐式创建」。因此**只读不写 = 集合永不诞生**，查询静默返回空。

---

## 3. 任务点名的 15 项特别核查

任务清单里有一部分**不是集合**，而是路由组 / 计算型读模型。逐个对齐：

| 任务点名 | 实际形态 | 实测 | 定性 | 说明 |
| --- | --- | --- | :--: | --- |
| `fileleak` | 路由组 + 读模型（后端集合 `asset_fileleak`） | 集合不存在 | **B** | 2 条路由 /api/assets/fileleak/list 与 delete 已注册，无生产者 |
| `asset_fileleak` | 集合 | 不存在 | **B** | 见 §2.1 |
| `npoc_service` | 路由组 + 读模型（`asset_npoc_service`） | 集合不存在 | **B** | 同 fileleak |
| `asset_npoc_service` | 集合 | 不存在 | **B** | 见 §2.1 |
| `cert` | **计算型读模型，无独立集合** | 源 `asset.tls`；125 条资产中 **tls 非空仅 2 条** | A | 读模型有三级字段回退（`repositories/assets/cert/cert.go:30-43`：tls.certificateresponse.* / tls.* / tlsdata.*），设计正确；问题是 -tlsprobe true 只在 1 个模板出现。**不是空集合，是样本量缺失** |
| `cip` | **计算型读模型，无独立集合** | 源 `RootDomain.company/icp`；5 条 RootDomain **company 与 icp 全为空串** | **B** | `repositories/assets/cip/cip.go:23` 注释自认「只读，不写任何集合」；全仓无任何代码给 RootDomain.company / RootDomain.icp 赋值（检索 0 命中）。**结构性永远空** |
| `PageMonitoring` / `PageMonitoringBody` | 集合 | 均 0 | A | 插件未挂模板（见 §2） |
| `asset_group` / `asset_scope` | 集合 | 均 0 | A | 写入方法齐备（`repository.go:140/237`） |
| `scan_policy` | 集合 | 0 | A | 写入齐备但任务链路不消费 |
| `scheduled` | 集合 | **1 条（非空）** | — | github_intel_tick，每分钟；last_run_time 实时刷新 → 调度器活着，业务侧开关全关 |
| `github_task` / `github_result` / `github_threat_history` | 集合 | 全 0 | A | 写入齐备；`github_threat_config` 三开关全 false |
| `github_threat_config` | 集合 | **1 条（非空）** | — | cve / hacker / tools 三个 enabled 均为 false，ignoredRepos 为空 |
| `icp_task` / `icp_result` / `icp_task_log` | 集合 | 全 0 | A | 服务层 + 14 handler 完整；provider 缺配置时自锁 |
| `export` | 集合 | 0 | A | InsertOne 齐备，从未导出 |
| `plugins_log` | **不存在** | 全仓检索 0 命中；库中无此集合 | — | 名称不存在。对应能力实际落在 `audit_logs`（1065 条，middleware.AuditLog 写）+ Redis `NodePlg` 键（插件自检 install / check） |

---

## 4. 三个与「空集合」相邻、但根因不在集合的结构性缺口

**4.1 通知链断了，但不在集合层**
`notification` 的**生产者是健全的**（`configuration/service.go:161` 的 InsertOne）。断点在投递端：config.Notification 在 `internal/` 内 **0 个调用方**（仅定义），yaegi 符号表未导出 config 包；RunTaskEnd 只查 type 为 server 且 status 为 true（`services/plugin/plugin.go:849`），而库里 16 个插件 type 全为空串。→ 空表本身无害，**配了渠道也不会发**。

**4.2 插件 status 全 false，但不是执行闸门**
16 个插件实测 status 为 false，然而扫描确实在跑（subdomain 23975、UrlScan 30400）。代码里 status 只在 `services/plugin/plugin.go:849` 作为 type 为 server 时的过滤条件使用，**不控制节点侧模块执行**。判读插件是否生效要看 `ScanTemplates` 里的模块数组，不要看 status。

**4.3 initdb 只显式建 1 个集合**
`initdb.go` 全文 CreateCollection 仅 1 处（`:171` notification），其余 56 个集合全靠首次写入隐式创建。这是 B 类集合「查询永不报错、永远返回空」的机制性原因——MongoDB 对不存在的集合执行 find 返回空游标而非错误。

---

## 5. P0 应先修的 3 个

选型标准：**阻断的下游入口数 × 修复成本 × 是否让功能「看起来能用实际全空」**。

### P0-1 vulnerability / vulnerabilityDetail（0 / 0）

价值链终点。资产采了 125 条、URL 3 万条、敏感信息 207 条，但「资产→漏洞」最后一跳是空的。它同时让 **4 个入口**失效：/api/assets/vulnerability、/api/assets/nuclei_result/*、导出（index=vulnerability）、MCP（asset_type=vulnerability）。

关键判断：**生产链已就绪**——nuclei 插件 ed93b8af 挂在全部 4 个模板，节点日志有 13m26s 的真实执行记录，代理 7890 实测 OPEN。缺的只是**有洞的目标**。这意味着修复成本最低、收益最直接：换一个真实目标重跑即可验证「页面 / 导出 / MCP 三端同数」。

### P0-2 PageMonitoring / PageMonitoringBody（0 / 0）

一个**自锁死循环**：插件 e52b8b16 未被任何模板的 URLSecurity 引用 → 不产出 URL → PageMonitoring 表空 → CreatePageMonitoringTask 在数据为空处提前 return → 永远不建任务。UI 侧 /api/assets/page-monitoring 与 /diff 都是可达页面，用户看到的是「功能在、永远空」。

修复成本极低（把 hash 加进模板数组 + 补一条 seed URL），但当前状态下两个集合会永久为 0，且调度器每分钟空转（Redis 里 page_monitoring 的任务信息从未建立）。

### P0-3 B 类纯消费端三件套：cip + asset_fileleak + asset_npoc_service

同一根因：**读模型的上游从未实现**。

- **cip**：RootDomain 的 company / icp 全仓无写入方（5 条记录实测全为空串），`cip.go:23` 自己注释「只读，不写任何集合」→ C 端页面（主体列表 + 详情 + 域名下钻）**结构性不可能有数据**。
- **asset_fileleak / asset_npoc_service**：集合连创建都没有，`dataview/service.go:16-17` 注释直说「节点侧**未来**直接写入」→ 两个页面 + 导出 + 2 条删除路由全部打在空气上。

这类问题比 A 类更严重：A 类是「还没跑」，B 类是「永远不会跑」，且 MongoDB 对不存在集合/空字段返回空而非报错，**没有任何错误信号**，只能靠审计发现。

> 紧随其后（P1）：app/mp（同 B 类，但页面入口弱）、icon（C 类，删集合 + 删 worker）、scan_policy（写了没人用）、通知投递链（§4.1）。

---

## 6. 处置动作汇总

| 动作 | 集合 |
| --- | --- |
| **补生产者**（代码/配置） | PageMonitoring、PageMonitoringBody（挂模板）、icp_result（配 http provider）、github_result、github_threat_history（开 enabled）、screenshot（节点侧截图分支）、vulnerability、vulnerabilityDetail（真实目标） |
| **打通消费链** | scan_policy（建任务时读策略）、notification（config.Notification 加调用方 + yaegi 导出 + 加一个 type 为 server 的插件） |
| **补 initdb 建集合** | asset_fileleak、asset_npoc_service、SubdomainTakerResult、TaskTargetControl |
| **补生产者或下线页面** | asset_fileleak、asset_npoc_service、app、mp、cip |
| **删集合** | icon（+ 删 worker/iconhandle.go） |
| **保持不动** | IPAssetTmp、TagColor、export、fofa_import、dict_upload_tasks、asset_group、asset_scope、icp_task、icp_task_log、github_task |

---

## 7. 可复现命令

1) 全集合计数（§1）

    ssh prod-tx-sh-app-02 "docker exec scopesentry-mongodb mongosh -u admin -p *** --authenticationDatabase admin ScopeSentry --quiet --eval 'JSON.stringify(db.getCollectionNames().sort().map(function(c){return {c:c, n:db.getCollection(c).countDocuments({})}}))'"

2) 模板里到底挂了哪些插件（PageMonitoring 判定）

    db.ScanTemplates.find({},{name:1,URLSecurity:1,VulnerabilityScan:1})
    注意：URLSecurity 为空数组或 null 即表示插件未挂

3) cert 读模型真实样本量

    var n=0; db.asset.find({},{tls:1}).forEach(function(d){ if(d.tls && Object.keys(d.tls).length>0) n++; }); print(n)   // 结果 2（资产总数 125）
    db.asset.countDocuments({type:"http"})      结果 10
    db.asset.countDocuments({screenshot:/./})   结果 0
    db.asset.countDocuments({iconcontent:/./})  结果 0

4) cip 上游是否被填充（全空即 B 类）

    db.RootDomain.find({},{domain:1,company:1,icp:1})

5) 代理实况（ISLANDS 的 closed 结论已失效）

    ssh prod-tx-sh-app-02 "docker exec scopesentry-scan bash -c '(timeout 2 bash -c "echo > /dev/tcp/127.0.0.1/7890") && echo PROXY_OPEN || echo PROXY_CLOSED'"

6) nuclei 是否真跑过

    ssh prod-tx-sh-app-02 "docker logs --tail 8000 scopesentry-scan 2>&1 | grep -i nuclei"

7) 代码侧：谁在写、谁只读

    grep -rn --include='*.go' -E 'InsertOne|InsertMany|BulkWrite' internal/ | grep -viE '_test'
    grep -rn --include='*.go' '"icon"' internal/       结果只有 worker 读 + 删

---

## 8. 附：本次实测与 ISLANDS.md 的差异清单（供修订）

| 项 | ISLANDS.md | 实测（2026-11-19 04:10-05:05） |
| --- | --- | --- |
| 集合总数 / 空集合 | 56 / 22 | **57 / 23** |
| IPAssetTmp | 1 条 | **0 条** |
| 节点代理 7890 | closed（判定为 P0-1 根因） | **OPEN**；nuclei 有真实执行日志 |
| asset.tls 非空 | 117/123（判 cert「闭环」） | **2/125**（cert 几乎空） |
| asset type=http | 8 | **10** |
| subdomain | 815 | **23975** |
| UrlScan | 7935 | **30400** |
| audit_logs | 673 | **1065** |
| mcp_call_logs | 4481 | **4610** |
| notification 生产者 | 「无生产者」 | **有**（AddNotification 调 InsertOne）；断点在投递函数 |
| plugins 判读 | 隐含 status 为执行开关 | status 全 false 但扫描在跑；真正开关是模板模块数组 |
## 处置记录（截至 v2.0.128）

| 集合 | 处置 |
| --- | --- |
| `scan_policy` | 已接线（v2.0.128）：建任务 policyId → templateId 兜底 + plugins 按阶段裁剪；生产已验证策略插件进入 NodeTask 载荷 |
| `PageMonitoring` / `PageMonitoringBody` | 已闭环：三处断点修复 + 启动期自动注册；实测 cron 每分钟触发，25s 内 body 0→1 |
| `app` / `mp` / `cip` / `fileleak` / `npoc_service` | 已标注：页面顶部说明「无写入方」 |
| `github_result` / `github_task` / `github_threat_history` | 未启用属预期：采集开关默认 false 且需 GitHub Token；缺 token 时 Threat 页显示 token=missing |
| `icon` | 待删（C 类）：v18 后改存 asset.iconcontent，无写入方，worker 仍在读 |
