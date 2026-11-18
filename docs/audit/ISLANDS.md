# ScopeSentry 系统孤岛与链路闭环缺口审计

- 审计对象：`/Users/night/Workspaces/security_workspace/_tools/ScopeSentry`（Go+Gin+Vue3），生产 `prod-tx-sh-app-02`（`autumn27/scopesentry:v2.0.109`，本机隧道 `http://127.0.0.1:18082`）
- 审计时间：2026-11-19 01:10 — 01:35 (+08)
- 只读约束：未改产品代码、未部署、未重启、未写库；唯一写入为本报告
- 结论口径：**孤岛 = 断掉的边**（后端无前端 / 前端无后端 / 集合无生产者 / 集合无消费者 / 异步链路缺回调）

---

## 0. 结论摘要

| 维度 | 数量 | 说明 |
| --- | ---: | --- |
| 后端路由（实际注册） | **281** | 25 个 route group + `/healthz`，由 gin `Engine.Routes()` 实跑导出，非静态推断 |
| 前端出现过的 `/api/*` 调用点 | **256** | 去重后 255 个形状；覆盖 238 个后端路由 |
| 有后端无前端 | **29** | 其中 6 个是"处理器存在但路由未注册"的孪生死代码 |
| 有前端无后端（会被调用且必然 404） | **9** | 项目详情看板 7 个 + POC 内容 + 资产截图 + 系统升级，共 11 处面板/按钮实际受影响 |
| 有集合无生产者 | **8** | `notification`、`vulnerability`、`PageMonitoring(+Body)`、`icon`、`screenshot`、`app`、`mp`、`github_threat_history` 等 |
| 有消费者无数据（链路断） | **6** | cert/cip/nuclei/fileleak/npoc_service/page_monitoring 中 2 个断链、4 个真空 |
| 集合总数 | **56** | 其中 **22 个为 0 文档**；**4 个被代码引用但库里从未创建** |

**最关键三条**：

1. **漏洞闭环整体断掉**：nuclei 插件参数写死 `-proxy http://127.0.0.1:7890`，而节点容器内该端口 **不通**（`proxy_7890_closed`）。`vulnerability` / `vulnerabilityDetail` = 0 条，而 `nuclei_result` 读模型、导出（index=vulnerability）、MCP（asset_type=vulnerability）全都指向这个空集合 → 「资产→漏洞」最后一公里是空的。
2. **通知链路是死代码**：`config.Notification()` 在 app 内 **0 个调用方**；yaegi 符号表未导出 `config` 包，插件也调不到；`RunTaskEnd` 只加载 `type:"server"` 的插件，而库里 16 个插件 **全是 `type:""`**。`notification` 集合 0 条。告警/通知功能整体不可达。
3. **任务完成态依赖前端轮询**：`TaskProgress` 只由 `POST /api/task`（列表接口）和 Redis 日志订阅（节点打出 `Task end`）触发；**没有后台定时器**，且状态机没有"失败"态（只有 0/1/2/3 = 待开始/运行/暂停/完成）。节点挂掉 → 任务永久 running，完成回调（快照、项目关联刷新、task-end 插件）永不执行。

---

## 1. 审计方法与证据命令

### 1.1 路由清单（实跑，不靠静态解析）

在 `/tmp` 下的**仓库副本**里加一个打印路由的临时 main（产品仓未改动），跑 `routes.SetupRouter()` 后输出 `Engine.Routes()`：

```bash
rsync -a --exclude .git --exclude dist --exclude node_modules ./ /tmp/island-audit/repo/
# 副本内新增 cmd/routedump/main.go（遍历 r.Routes() 输出 method/path/handler）
# 副本内去掉 internal/bootstrap 的 ExecuteStartupEvents 调用与 mongodb ping 失败 return
#   （否则包 init 阶段连不上库直接 Fatalf，路由表拿不到）
cd /tmp/island-audit/repo && go run ./cmd/routedump > /tmp/island-audit/routes_real.json
# => 281 条
```

### 1.2 前端调用点

扫描 `frontend/src` 下 `.ts/.vue/.js` 里所有 `'/api/...'` 字面量（含模板拼接前缀），得到 256 个去重 URL；把 `:id` / `${id}` 归一成 `*` 后与后端路径形状比对，得到 238 匹配 / 35 后端独有 / 17 前端独有。

### 1.3 DB / Redis / 节点（只读）

```bash
ssh prod-tx-sh-app-02 "docker exec scopesentry-mongodb mongosh -u admin -p *** \
  --authenticationDatabase admin ScopeSentry --quiet --file /tmp/q.js"
ssh prod-tx-sh-app-02 "docker exec scopesentry-redis redis-cli -a *** --no-auth-warning keys '*'"
ssh prod-tx-sh-app-02 "docker logs --tail 3000 scopesentry-scan | grep -iE 'httpx|proxy|error'"
```

---

## 2. 路由面：25 组 × 前端覆盖

| route group | 注册路由 | 前端覆盖 | 前端模块 | 判定 |
| --- | ---: | ---: | --- | --- |
| `/api/assets` | 75 | 67 | `api/asset`, `cert`, `cip`, `fileleak`, `npocService`, `assetReadModel`, `assetService`, `assetGroup`, `assetOverview`, `httpStatus` | 8 条无前端（ip/site/by-host/hosts） |
| `/api/task` | 25 | 25 | `api/task` | 闭环 |
| `/api/github` | 26 | 20 | `api/github`（含 `threat/'+kind+'_config'` 动态拼接） | 6 条无前端 |
| `/api/dictionary`+`dict_brute`/`dict_cdn` | 23 | 22 | `api/dictionary`, `DictionaryManagement` | `manage/save` 无前端 |
| `/api/plugin` | 17 | 16 | `api/plugins` | `remote/search` 无前端 |
| `/api/configuration` | 14 | 14 | `api/Configuration` | 闭环 |
| `/api/icp` | 14 | 14 | `api/icp` | 闭环（但功能从未启用） |
| `/api/mcp` | 9 | 7 | `api/apikey`（keys 复用） | `keys/:id` POST/DELETE 无前端 |
| `/api/export` | 8 | 7 | `api/export` | `export/task/:taskId` 无前端 |
| `/api/user` | 8 | 2 | `api/login` | 用户 CRUD 6 条无前端 |
| `/api/node` | 7 | 7 | `api/node` | 闭环 |
| `/api/poc` | 7 | 7 | `api/poc` | 闭环（前端另有 3 条死路径） |
| `/api/policy` | 7 | 7 | `api/policy` | 闭环（空表） |
| `/api/backup` | 7 | 7 | `api/backup` | 闭环（前端用 `'/'+name` 拼接） |
| `/api/fingerprint` | 6 | 6 | `api/Fingerprint` | 闭环 |
| `/api/project` | 6 | 6 | `api/project` | 闭环 |
| `/api/fofa` | 5 | 5 | `api/fofa` | 闭环（从未使用） |
| `/api/sensitive` | 5 | 5 | `api/sensitive` | 闭环 |
| `/api/dashboard` | 5 | 1 | `api/dashboard` | **4 条无前端**（stats/trend/widgets/sysinfo） |
| `/api/apikey` | 3 | 3 | `api/apikey` | 闭环 |
| `/api/system` | 1 | 1 | `api/dashboard/analysis` | 后端只有 `/system/version`，前端还要 `/system/update` → 404 |
| `/api/console` | 1 | 1 | `api/dashboard` | 闭环 |
| `/api/audit` | 1 | 1 | `api/audit` | 闭环 |
| `/healthz` | 1 | 0 | — | 探活，非 UI |

> 前端目录里还存在**完全没有 views 引用**的 api 模块：`api/department`（全部指向 `/mock/*`）、`api/scommon`、`api/table`、`api/request`、`api/vul`。

---

## 3. 孤岛一：有后端、无前端（29 条）

### 3.1 A 类 —— 处理器写了但路由没注册（死代码）

| 处理器 | 位置 | 前端调用 | 后果 |
| --- | --- | --- | --- |
| `GetScreenshot` | `handlers/assets/asset/asset.go:259` | `POST /api/asset/screenshot`（`AssetInfo2.vue:694`） | 路由从未注册 → 截图 Tab 404；且 `screenshot` 集合 0 条，双保险为空 |
| `GetAssetTitleStatistics` | `handlers/assets/statistics/statistics.go:72` | `POST /api/asset/statistics/title`（`Project/detail/components/Dashboard.vue:229`） | 404，项目看板 title 分布图永久空白 |
| `SearchRemotePlugins` | `routes/plugin/plugin.go` | 无 | 远程插件市场无 UI |
| `UpdateKey`/`DeleteKey` | `routes/mcp/mcp.go` | 前端只调 `keys/:id/status` | 只能启停 MCP key，不能改/删 |
| `Save`(dictionary/manage) | `routes/dictionary` | 无 | 后端可改字典名，UI 无入口 |

### 3.2 B 类 —— 纯后端能力，前端无入口

| 路由 | 处理器 | 说明 |
| --- | --- | --- |
| `POST/GET /api/assets/site/list`、`POST /api/assets/site/chain` | `sitehandler.*` | 站点聚合与链路图，**前端无任何 site 页面** |
| `GET/POST /api/assets/ip/list`、`POST /api/assets/ip/domains` | `iphandler.*` | IP 资产列表页缺失（前端只有 IP 卡片） |
| `POST /api/assets/asset/hosts`、`/asset/by-host` | `asset.*` | 根域→主机、主机→资产下钻未接 |
| `POST /api/dashboard/stats`/`trend`/`widgets`、`GET /sysinfo` | `dashboardhandler.*` | ARL 风格仪表盘后端已实现，前端 Dashboard 走 `/api/assets/statistics` |
| `POST /api/github/result/overview`/`stats`/`ignored_repos`/`export`、`monitor_result/list`、`scheduler/update` | `githubhandler.*` | GitHub 情报统计/忽略仓库/导出/监控面板未接 |
| `GET/POST/PUT/DELETE /api/user[/:id]`、`POST /user/register` | `user.*` | **用户管理界面整体缺失**：后端 6 个 CRUD 全在，前端只有 login/changePassword |
| `GET /api/export/task/:taskId` | `export.TaskExport` | 任务级一键导出无入口 |

---

## 4. 孤岛二：有前端、无后端（必然 404）

### 4.1 会被真实点击、必然失败

| 前端调用 | 调用点 | 后端实际 | 影响 |
| --- | --- | --- | --- |
| `POST /api/project_aggregation/project/info` | `views/Project/detail/components/Dashboard.vue:27` | **无任何 project_aggregation 路由**（全仓 grep 0 命中） | 项目详情页首屏 404 |
| `.../project/asset/count` | 同上 | 无 | 资产数卡片空白 |
| `.../project/vul/statistics` | 同上 | 无 | 漏洞等级饼图空白 |
| `.../project/vul/data` | `components/vul.vue` | 无 | 漏洞列表空白 |
| `.../project/subdomain/data` | `components/Subdomain.vue:20` | 无 | 子域列表空白 |
| `.../project/port/data` | `components/Port.vue:19` | 无 | 端口列表空白 |
| `.../project/service/data` | `components/Service.vue:20` | 无 | 服务列表空白 |
| `POST /api/poc/content` | `views/Poc/Poc.vue:27`、`views/Asset/components/vul.vue:32` | 后端只有 `/api/poc/detail` | POC 内容抽屉打不开 |
| `POST /api/asset/screenshot` | `views/Asset/components/AssetInfo2.vue:694` | 处理器在、路由无 | 截图 Tab 永久空 |
| `POST /api/asset/statistics/title` | `views/Project/detail/components/Dashboard.vue:229` | 处理器在、路由无 | 标题分布图空白 |
| `POST /api/system/update` | `views/Dashboard/Analysis.vue:330` | 后端只有 `GET /api/system/version` | 在线升级按钮必然失败 |

> 项目详情页（`router/index.ts:331 → ProjectDetail.vue`）是**可达页面**，其 5 个 Tab 里 4 个整体 404 —— 本次审计中"看起来能用、实际全空"的最大一块。

### 4.2 前端死包装（定义了但没人调用）

`getPageMonitoringResponseApi`（`/api/page/monitoring/response`）、`getPocNameListApi`（`/api/poc/name/list`）—— 无任何 view 引用，属重构残留。

### 4.3 两侧皆死

`/api/task/scheduled/run`：前端 `taskRunApi` 存在但 `views/Task/ScheduledTask.vue:153` 已注释；后端也无此路由。

---

## 5. 孤岛三：集合 × 生产者 × 消费者（全量 56 个集合）

| 集合 | 文档数 | 最近文档时间 | 生产者（写） | 消费者（读） | 判定 |
| --- | ---: | --- | --- | --- | --- |
| `AssetChangeLog` | 3 | 2026-11-19 01:13:56 | 节点插件（资产变更时写入） | /api/assets/asset/changelog + 详情页 | 闭环（3 条，低频） |
| `DirScanResult` | 44 | — | DirScan/SentryDir 插件 | /api/assets/dirscan、导出、MCP | 闭环 |
| `FingerprintRules` | 6863 | — | 资产包播种 / 指纹管理 API | WebFingerprint 插件 + /api/fingerprint/data | 闭环（规则库） |
| `HttpBody` | 10 | — | 节点 AssetMapping / AssetHandle | 资产详情 body、/api/assets/common/delete_by_search | 闭环 |
| `IPAsset` | 16 | 2026-11-19 01:17:59 | app 内 worker IPAssetHandle（聚合 IPAssetTmp） | /api/assets/ip/list、/api/assets/service/list、MCP | 闭环 |
| `IPAssetTmp` | 1 | — | 节点 PortScan/PortFingerprint | worker IPAssetHandle（消费即删） | 闭环（中间态） |
| `PageMonitoring` | 0 | — | 仅 /api/task/scheduled/pagemonit/add（人工）+ PageMonitoring 插件（未被任何模板引用） | /api/assets/page-monitoring、定时任务 | 断链：空表 → 定时任务 return |
| `PageMonitoringBody` | 0 | — | PageMonitoring 插件（未运行） | /api/assets/page-monitoring/diff | 断链（无生产者） |
| `PocList` | 14338 | 2026-11-19 00:27:30 | 资产包播种 + /api/poc/data/import | /api/poc、节点模板下发、vullist | 闭环（14338） |
| `PortDict` | 3 | — | 初始化 / 端口字典 API | /api/dictionary/port/data、{port.top1000} | 闭环 |
| `ProjectTargetData` | 2 | — | 项目目标同步（AutoBind/RefreshProjectAssociations） | project 聚合查询 | 闭环 |
| `RootDomain` | 4 | 2026-11-18 15:51:08 | TargetHandler / 节点 | /api/assets/root_domain、cip 读模型、MCP | 数据在但 icp/company 全空 → cip 页空转 |
| `ScanTemplates` | 4 | — | 模板 API / 默认初始化 | 任务创建、节点下发 | 闭环 |
| `ScheduledTasks` | 1 | — | 初始化 / 定时任务 API | scheduler（page_monitoring） | 配置在但目标集合空 → 空跑 |
| `SensitiveBody` | 105 | — | URLSecurity/sensitive 插件 | /api/sensitive/*、wih 详情 | 闭环 |
| `SensitiveResult` | 207 | 2026-11-19 01:12:19 | URLSecurity/sensitive 插件 | /api/sensitive/list、/api/assets/wih/*、导出、MCP | 闭环（207，持续增长） |
| `SensitiveRule` | 172 | — | 敏感规则 API / 初始化 | sensitive 插件 + /api/sensitive/rule | 闭环 |
| `TagColor` | 0 | — | /api/assets/tag_color/set | /api/assets/tag_color/list | 闭环（空表） |
| `TaskProgressSnapshot` | 12 | Wed Nov 18 2026 08:05:43 GMT+0000 (Coordinated Universal Time) | 任务完成时 snapshotTaskProgress | /api/task/progress 回放、MCP | 闭环（仅完成任务） |
| `UrlScan` | 7935 | 2026-11-19 01:25:17 | URLScan(wayback/katana) + WebCrawler(rad) | /api/assets/url、导出、MCP、URLSecurity 输入 | 闭环（7934；katana 仅 8） |
| `api_key` | 2 | Wed Nov 18 2026 16:55:24 GMT+0000 (Coordinated Universal Time) | /api/apikey/create | 鉴权中间件 | 闭环 |
| `app` | 0 | — | 未见任何写入方 | /api/assets/app、导出、MCP | 有消费者无生产者 |
| `asset` | 123 | 2026-11-19 01:24:10 | AssetHandle / PortFingerprint / AssetMapping 插件 | /api/assets/asset、cert、导出、MCP | 闭环（115 port 级 + 8 http 级） |
| `asset_group` | 0 | — | 资产分组 API | /api/assets/asset_group/* | 闭环（空表） |
| `asset_scope` | 0 | — | 资产范围 API | /api/assets/asset_scope/* | 闭环（空表） |
| `audit_logs` | 673 | Wed Nov 18 2026 17:30:28 GMT+0000 (Coordinated Universal Time) | middleware.AuditLog（写请求） | /api/audit/list | 闭环（460） |
| `config` | 10 | — | 配置 API + 资产包指纹 | 启动加载、/api/configuration/* | 闭环 |
| `crawler` | 1 | 2026-11-19 01:15:10 | WebCrawler/rad 插件 | /api/assets/crawler、导出、MCP | 闭环（1 条 → rad 基本未产出） |
| `dict_upload_tasks` | 0 | — | /api/dictionary/upload_large | /api/dictionary/upload_status | 闭环（未使用） |
| `dictionary` | 28 | 2026-11-18T16:27:33.786Z | 资产包播种 + 字典 API | /api/dictionary/*、节点字典下发 | 闭环（28） |
| `dictionary_entry` | 709622 | Wed Nov 18 2026 16:27:38 GMT+0000 (Coordinated Universal Time) | 字典行回填任务 | /api/dictionary/preview|search | 闭环（70.9 万行） |
| `export` | 0 | — | /api/export | /api/export/records | 闭环（未使用） |
| `fofa_import` | 0 | — | /api/fofa/task | /api/fofa/task/list | 有消费者无生产者（功能未用） |
| `fs.chunks` | 44 | — | GridFS（字典上传） | 字典预览/下载 | 闭环 |
| `fs.files` | 28 | Wed Nov 18 2026 16:27:38 GMT+0000 (Coordinated Universal Time) | GridFS（字典上传） | 字典预览/下载 | 闭环 |
| `github_result` | 0 | — | GitHub 情报任务（三个开关全 false） | /api/github/result/* | 断链：配置全 disabled |
| `github_task` | 0 | — | /api/github/task/create | /api/github/task/list、scheduler | 闭环（功能未用） |
| `github_threat_config` | 1 | Wed Nov 18 2026 17:23:40 GMT+0000 (Coordinated Universal Time) | /api/github/threat/*_config | 威胁情报调度 | 闭环（enabled 全 false） |
| `github_threat_history` | 0 | — | 威胁情报任务 | /api/github/threat/cve_history | 有消费者无生产者 |
| `icon` | 0 | — | 节点插件写 base64（当前无） | worker IconHandle → 落盘 | 有消费者无生产者 |
| `icp_result` | 0 | — | ICP 任务 | /api/icp/asset | 有消费者无生产者（从未建任务） |
| `icp_task` | 0 | — | /api/icp/task/create | /api/icp/task/list | 闭环（功能未用） |
| `icp_task_log` | 0 | — | ICP 任务执行 | /api/icp/task/log | 有消费者无生产者 |
| `mcp_call_logs` | 4481 | Wed Nov 18 2026 17:29:50 GMT+0000 (Coordinated Universal Time) | MCP guard | /api/mcp/logs | 闭环（4432） |
| `mcp_config` | 1 | Wed Nov 18 2026 15:27:58 GMT+0000 (Coordinated Universal Time) | /api/mcp/config | MCP 服务 | 闭环 |
| `mp` | 0 | — | 未见写入方 | /api/assets/mp、导出、MCP | 有消费者无生产者 |
| `notification` | 0 | — | 无（app 无 config.Notification 调用；yaegi 未导出该符号；task-end 插件 0 个） | 启动加载 + /api/configuration/notification/* | 有消费者无生产者（告警链断） |
| `plugins` | 16 | — | 插件安装 API / 资产包 | /api/plugin/*、节点下发 | 闭环（16） |
| `project` | 2 | — | 项目 API | /api/project/*、聚合 | 闭环（2） |
| `scan_policy` | 0 | — | /api/policy/* | 扫描模板/任务 | 闭环（空表） |
| `scheduled` | 1 | Tue Nov 17 2026 19:29:03 GMT+0000 (Coordinated Universal Time) | 调度初始化 | scheduler（github_intel 每分钟） | 闭环 |
| `screenshot` | 0 | — | 节点插件写 base64（-screenshot true） | worker ScreenshotHandle → 落盘 | 有消费者无生产者（0，asset.screenshot 全空） |
| `subdomain` | 815 | 2026-11-19 01:23:33 | SubdomainScan(subfinder/ksubdomain) | /api/assets/subdomain、MCP、导出 | 闭环（1633） |
| `task` | 6 | — | /api/task/add、调度、MCP | /api/task/*、进度轮询 | 闭环（6） |
| `user` | 1 | 2026-11-18T06:34:11.545Z | 初始化 / 用户 API | 登录、鉴权 | 闭环（1） |
| `vulnerability` | 0 | — | VulnerabilityScan/nuclei（代理 7890 不通） | /api/assets/nuclei_result/*、/api/assets/vulnerability、导出、MCP | 断链：0 条 |
| `vulnerabilityDetail` | 0 | — | nuclei 插件 | vulnerability 详情 | 断链：0 条 |

### 5.1 被代码引用、但库里根本没有的集合（4 个）

| 集合 | 引用处 | 后果 |
| --- | --- | --- |
| `asset_fileleak` | `internal/services/assets/dataview/service.go:16`（注释直说"节点侧未来直接写入即可生效"） | 文件泄露页永远空 |
| `asset_npoc_service` | 同上 :17 | NPoC 服务页永远空 |
| `SubdomainTakerResult` | `repositories/assets/subdomain/taker.go:14`、`mcp/tools.go` 白名单 | 子域接管页 / MCP 查询永远空 |
| `TaskTargetControl` | `repositories/task/task/target_control.go:14` | 目标级并发/暂停控制无落点 |

---

## 6. 重点资产集合闭环核对（cert / cip / wih / nuclei / fileleak / npoc_service / page_monitoring / service）

| 集合/读模型 | 写入方（生产者） | 存储 | 读取方（消费者） | 实测 | 闭环? |
| --- | --- | --- | --- | --- | --- |
| **cert** | 节点 `AssetMapping`(httpx) TLS 探测 → `asset.tls.certificateresponse` | 无独立集合，读模型现算 | `POST /api/assets/cert/list\|detail` → 证书页 | `asset.tls != null` = **117/123**（subjectcn/serial/issuer/fingerprint 齐全） | ✅ 闭环，但依赖模板 `-tlsprobe`（仅全量模板为 true） |
| **cip** | ICP 任务回填 `RootDomain.company/icp` | `RootDomain` | `POST /api/assets/cip/list\|detail` | `RootDomain` 4 条，**company/icp 非空 = 0**；`icp_task` = 0 | ❌ 断链：从未建过 ICP 任务，页面必然全空 |
| **wih** | `URLSecurity/sensitive` 插件 | `SensitiveResult`(207) + `SensitiveBody`(105) | `/api/assets/wih/*`、`/api/sensitive/*`、导出、MCP | 最近写入 01:12；sid 分布 AccessKey2=101 / cannyio=101 | ✅ 闭环（唯一完全健康的读模型） |
| **nuclei** | `VulnerabilityScan/nuclei` 插件 | `vulnerability`(0) + `vulnerabilityDetail`(0) | `/api/assets/nuclei_result/*`、`/api/assets/vulnerability`、导出、MCP | **0 条**；节点日志中 nuclei 以 `-proxy http://127.0.0.1:7890` 启动，容器内 7890 **closed** | ❌ 断链（生产端失败） |
| **fileleak** | **无**（无插件、无写入 API） | `asset_fileleak`（不存在） | `/api/assets/fileleak/list\|delete` → 页面 + 删除 | 集合不存在 | ❌ 纯消费端孤岛 |
| **npoc_service** | **无** | `asset_npoc_service`（不存在） | `/api/assets/npoc_service/list\|delete` | 集合不存在 | ❌ 纯消费端孤岛 |
| **page_monitoring** | `POST /api/task/scheduled/pagemonit/add`（人工）+ `PageMonitoring` 插件（hash `e52b8b16`，**未被任何 ScanTemplate 的 URLSecurity 数组引用**） | `PageMonitoring`(0) / `PageMonitoringBody`(0) | `/api/assets/page-monitoring`、`/diff`、`pagemonit/*` | 集合 0 → `getPageMonitoringData()` 返回空 → `CreatePageMonitoringTask` 直接 `return nil`（连 Redis key 都不建） | ❌ 死锁闭环：没 URL→不建任务→插件不跑→永不产生 URL |
| **service** | 节点 `PortFingerprint` → `IPAssetTmp` → app worker `IPAssetHandle` 聚合 | `IPAsset.ports[].server[]` | `POST /api/assets/service/list` → 服务页；MCP `asset_type=ip` | `IPAsset` 16 条，**带 server = 16/16**（service/webServer/technologies） | ✅ 闭环 |

### 6.1 顺带发现的字段级断点

- `asset` 集合 **115 条 type="other"（端口级）+ 8 条 type="http"（httpx 级）**：整条 URL 侧资产只有 8 条。因为 httpx 带死代理，绝大多数主机拿不到 HTTP 资产，导致 cert、title 统计、tech 统计都只有 8 条样本量。
- `asset.screenshot` 非空 = 0、`asset.body` 非空 = 0：`-screenshot true` 只在 default 模板出现，实际未产出。
- `crawler` 仅 1 条、`DirScanResult` 44 条、`UrlScan` 中 `outputtype=katana` 仅 8 条（3609 regex / 2258 href 来自被动解析，291 wayback）：`rad`/`katana`/`SentryDir` 三个"主动爬/扫"插件基本没产出，同一根因。

---

## 7. 异步链路闭环核对：任务 → 节点 → 结果 → 资产 → 指纹/漏洞 → 通知

| # | 环节 | 实现 | 状态回写 | 实测证据 | 判定 |
| --- | --- | --- | --- | --- | --- |
| 1 | 建任务 | `POST /api/task/add` → Mongo `task` + Redis `TaskInfo:tmp/time` | `status=0, progress=0` | `task` 6 条：5 finished + 1 paused(progress 91) | ✅ |
| 2 | 派发节点 | `RPush NodeTask:<nodeName>`，节点 `Get a new task` 拉取 | — | 节点日志有完整 task JSON | ✅ |
| 3 | 节点心跳 | Redis hash `node:<name>`：`state/updateTime/cpuNum/memNum/running/finished/modulesConfig` | 300s 超时判离线 | node-tx-sh-02 / node-test 均 `state=1`，updateTime 秒级刷新 | ✅ |
| 4 | 进度回写 | **只有两条触发**：① `POST /api/task`（列表接口顺带 `go TaskProgress()`）② Redis 日志订阅收到含 `"Task end"` 的日志 | `taskstate.Derive`：0/1/2/3，**无 failed 态、无超时判定** | 当前任务 progress=91 靠前端列表轮询推进 | ⚠️ 缺后台刷新 + 缺失败态 |
| 5 | 结果入库 | 节点直连 Mongo 写 `asset/subdomain/UrlScan/SensitiveResult/...`（app 无 result 接收 API） | — | subdomain 10 分钟内 198→1633 | ✅（但耦合在节点） |
| 6 | 资产→指纹 | `AssetHandle/WebFingerprint` + `FingerprintRules`(6863) | `asset.technologies` | 非空 **8/123** | ⚠️ 只对 http 资产生效（同 httpx 断点） |
| 7 | 资产→漏洞 | `VulnerabilityScan/nuclei` → `vulnerability` | — | **0 条**，代理不通 | ❌ 断 |
| 8 | 任务完成回调 | `snapshotTaskProgress` + `AutoBind→autoSyncToProject` + `RefreshProjectAssociations` + `RunTaskEnd` | `TaskProgressSnapshot`(12) | 只有 1 个任务、12 条目标级快照 | ⚠️ 依赖 #4，节点不发 `Task end` 永不执行 |
| 9 | task-end 插件 | `RunTaskEnd` 查 `{type:"server", status:true}` | — | **库里 0 个 type="server" 插件**（16 个全 `type:""`） | ❌ 恒空 |
| 10 | 通知 | `config.Notification(msg)` 遍历 `NotificationApi` | — | `config.Notification` **全仓 0 调用方**；yaegi 符号表无 `config` 包；`notification` 0 条 | ❌ 死链 |
| 11 | 失败重试 | — | — | `services/task`、`scheduler`、`handlers/task` 内 grep `retry\|重试` **0 命中** | ❌ 无任何重试 |
| 12 | 插件自检 | `NodePlg:<node>` 记录 `<hash>_install/_check` | — | 出现 `7bbaec64...`（**plugins 集合已无此 hash**，孤儿）；`e8f55f5e(ksubdomain)_check=0` | ⚠️ 有孤儿/未通过项 |
| 13 | 页面监控调度 | scheduler 调 `CreatePageMonitoringTask` | — | Redis 无 `TaskInfo:page_monitoring` → 在 `len(data)==0` 处提前返回 | ❌ 空转 |
| 14 | GitHub 情报 | `scheduled.github_intel_tick` 每分钟 | `last_run_time` 实时刷新 | 但 `github_threat_config` 三个开关 `enabled:false`，`github_task/result/history` 全 0 | ⚠️ 调度活着、业务全关 |

### 7.1 断点根因（按影响排序）

1. **节点代理配置与运行环境不匹配**：`AssetMapping(httpx)`/`URLScan(katana)`/`VulnerabilityScan(nuclei)` 参数都写死 `-proxy http://127.0.0.1:7890`；实测容器内该端口 **closed**（`HTTP_PROXY` 也指向它）。→ http 资产 8 条、katana 8 条、nuclei 0 条。
2. **通知子系统只建了"外壳"**：UI + 集合 + 配置加载 + 发送函数齐备，唯独没有调用点，插件侧也拿不到符号。
3. **任务终态不可达**：无 failed 态 + 无定时器 + 依赖节点日志魔法字符串 `"Task end"`。
4. **读模型"只读现算"依赖上游字段**：cert(asset.tls) / cip(RootDomain.icp) / wih(SensitiveResult) 三种模式，cip 的上游回填任务从未启用 → 页面永远空，且 UI 无"未采集"提示。

---

## 8. 修复清单

### P0（数据链断、功能实质不可用）

| # | 项 | 动作 | 验收 |
| --- | --- | --- | --- |
| P0-1 | 节点代理不可达 | 三个插件参数去掉 `-proxy` 或改环境变量 `SCAN_PROXY`；节点启动时校验代理端口，不通拒绝扫描 | 单目标扫描后 `asset(type=http)`、`vulnerability`、`UrlScan(outputtype=katana)` 均 > 0 |
| P0-2 | 漏洞闭环 | 修 P0-1 后重跑含 `VulnerabilityScan` 的模板，验证页面 / 导出(index=vulnerability) / MCP(asset_type=vulnerability) 三端同数 | 三端计数一致 |
| P0-3 | 项目详情页整体 404 | ① 实现 7 个 `/api/project_aggregation/*`（复用 `repositories/project` 聚合）；或 ② 下线对应 Tab 与看板卡片 | 项目详情页无 4xx |
| P0-4 | `/api/poc/content` 404 | 后端补路由映射到 `GetPocDetail`，或前端改调 `/api/poc/detail` | POC 内容抽屉可打开 |

### P1（闭环缺失、告警/终态不可信）

| # | 项 | 动作 | 验收 |
| --- | --- | --- | --- |
| P1-1 | 通知不可达 | 在 `ProcessTaskProgress` 完成分支与新资产分支调用 `config.Notification`；把 `config.Notification` 加入 yaegi 符号表；提供至少一个 `type:"server"` 的 task-end 插件（或删掉该机制） | 配 webhook，任务完成能收到消息 |
| P1-2 | 任务无 failed 态/无超时 | `task_status` 增 `TaskStatusFailed=4`；后台 ticker 扫 `status∈{0,1}` 且 `now - TaskInfo:time > 阈值` 的任务置 failed 并记原因 | 杀节点后任务在阈值内转 failed |
| P1-3 | 进度刷新依赖前端 | 把 `go TaskProgress()` 从 `POST /api/task` 挪到后台 ticker，接口只读 | 不开页面进度也推进到 100 并触发完成回调 |
| P1-4 | fileleak / npoc_service 纯消费端 | 节点侧补写入（插件或 AssetHandle 派生），或页面标"未接入"并隐藏 | 不再展示永空列表 |
| P1-5 | page_monitoring 死锁 | 把 `e52b8b16` 加进全量模板 URLSecurity 数组；空集合时打 warn | 加一条 URL 后 24h 内产生 `PageMonitoringBody` |
| P1-6 | 用户管理缺失 | 前端补 `api/user` + 用户管理页（后端 6 接口已就绪） | CRUD 可用 |
| P1-7 | 升级入口 404 | 补 `POST /api/system/update` 或移除 `Analysis.vue` 升级表单 | 页面无 4xx |

### P2（孤岛清理、一致性）

| # | 项 | 动作 |
| --- | --- | --- |
| P2-1 | 死处理器 | `GetScreenshot`、`GetAssetTitleStatistics`、`SearchRemotePlugins`、`UpdateKey/DeleteKey`、`dictionary/manage/save` —— 注册路由并接 UI，或删除 |
| P2-2 | 纯后端路由 | site/ip-list/asset-hosts/dashboard(stats/trend/widgets/sysinfo)/github(overview/stats/ignored_repos/export/monitor_result/scheduler-update)/export-task —— 逐条决定"接 UI / 标 experimental / 删" |
| P2-3 | 不存在的集合 | `initdb` 显式创建（含索引）或改为派生视图，避免查询不存在集合静默返回空 |
| P2-4 | MCP/导出覆盖面 | MCP 白名单与 `models.Field` 都缺 `cert/cip/wih/nuclei_result/fileleak/npoc_service/PageMonitoring`（导出还特判 PageMonitoring 却取不到字段），按读模型清单补齐 |
| P2-5 | 前端死包装 | 删 `getPageMonitoringResponseApi`/`getPocNameListApi`/`taskRunApi` 或补齐后端 |
| P2-6 | 魔法字符串耦合 | 节点 `"Task end"`/`"Register Success"` 改结构化事件（Redis Stream），避免日志文案改动即断链 |
| P2-7 | 节点状态孤儿 | `NodePlg` 中 `7bbaec64...` 已不在 `plugins`；`ksubdomain_check=0` 需重装/对齐 |
| P2-8 | ICP 未启用 | UI 提示"需先跑 ICP 任务"，或 cip 页与 `icp_task` 联动引导 |

---

## 9. 附：关键证据（可复现）

```bash
# 1) 集合盘点（count + 最近一条文档时间）
ssh prod-tx-sh-app-02 "docker exec scopesentry-mongodb mongosh -u admin -p *** \
 --authenticationDatabase admin ScopeSentry --quiet --file /tmp/_qi.js"
# => 56 集合；22 个为 0

# 2) 字段级覆盖
db.asset.countDocuments({})                                # 123
db.asset.countDocuments({tls: {$ne: null}})                # 117
db.asset.aggregate([{$group:{_id:"$type",n:{$sum:1}}}])    # other=115, http=8
db.SensitiveResult.countDocuments({})                      # 207
db.vulnerability.countDocuments({})                        # 0
db.PageMonitoring.countDocuments({})                       # 0
db.notification.countDocuments({})                         # 0
db.RootDomain.countDocuments({icp:{$nin:["",null]}})      # 0

# 3) 节点代理（P0-1 判定）
ssh prod-tx-sh-app-02 "docker exec scopesentry-scan sh -c \
 '(timeout 2 sh -c \"echo > /dev/tcp/127.0.0.1/7890\") && echo open || echo closed'"
# => closed

# 4) 插件拓扑
db.plugins.find({},{name:1,module:1,type:1,status:1})   # 16 个，type 全 ""
db.ScanTemplates.find({},{name:1,URLSecurity:1,VulnerabilityScan:1})
# => URLSecurity 只有 sensitive/trufflehog，PageMonitoring(e52b8b16) 未被引用

# 5) 调度与心跳
redis-cli keys '*'                                    # node:<name> / NodeTask / NodePlg / TaskInfo:progress:*
redis-cli hgetall node:node-tx-sh-02                  # state=1, updateTime 秒级
redis-cli exists TaskInfo:page_monitoring             # 0 → 页面监控空转

# 6) 通知死链
grep -rn "config.Notification(" internal/ | grep -v "func Notification"   # 0 命中
grep -rln "internal/config" internal/symbols/                             # 0 命中（yaegi 无 config 包）
```
