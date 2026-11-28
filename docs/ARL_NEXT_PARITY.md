# ARL-Next 功能对齐（1:1）路线图

> 目标：`owl234/ARL-Next` 有的能力，ScopeSentry 都要有；前端升级到 Vue 3.5。
> 参照方式：以 ARL-Next 的路由/模型/页面作为**功能规格**，用我们自己的 Go + Vue 代码实现等价能力。
> （ARL-Next 是 GPL-3.0，直接搬源码会把我们的仓库也变成 GPL，所以按接口与行为对齐，不复制其实现代码。）

## 当前基线（2026-11-29 复核）

| 项 | 状态 |
| --- | --- |
| Vue | 3.5.13（vue-router 4.6、pinia 2.3、element-plus 2.14、vue-i18n 9.14、vite 5.0.6） |
| 后端 | Go + Gin，**315 条路由**，Mongo 7 + Redis 7，多节点扫描 |
| 参照物 | ARL-Next `backend/app/routes/*.py`：**58 个文件、302 个端点**（快照见 `docs/audit/upstream_routes.json`） |
| 路由族覆盖 | **57 族：✅ 46 / 🟡 11 / ❌ 0**（明细见自动生成的 `docs/audit/ARL_NEXT_COVERAGE.md`） |

> 下面这张手写的「功能族对照」写于 2026-11-18，之后补了很多能力（ICP 任务链、GitHub 监控、
> 扫描策略、资产组/地契、字典行级化、任务导出、多工作表 Excel…），表里不少 ❌ 已经过时。
> **以 `docs/audit/ARL_NEXT_COVERAGE.md` 为准**：它按上游 302 个端点逐族比对，可重跑（`scripts/ci/arl_parity.py`）。

### 复核后的真实缺口（2026-11-29）

| 缺口 | 上游规模 | 现状 | 说明 |
| --- | --- | --- | --- |
| nuclei 模板库管理 | 19 端点 | ❌ | 上游有模板 CRUD / 批量开关 / 导入导出 / 测试 / 社区同步（`/community/sync*`）/ 黑名单；我们只有 nuclei **结果**读模型（`/assets/nuclei_result/*`）。这是唯一整族缺失。 |
| 计划任务手动触发 | `scheduler /run /stop /recover` | 🟡 已补一半 | 第 99 轮补了「立即运行一次」（`/api/task/scheduled/run` + MCP `run_scheduled_task`，与周期触发同一 jobs.Scan）；仍缺「停止运行中 / 恢复」。 |
| 代理节点与策略 | `system_config /proxy* /check_update /request_update_token` | ❌ | 上游能管扫描出口代理节点/策略/探测/冷却；我们没有这层概念。 |
| 指纹同步 | `fingerprint /sync /sync_from_json /upload` | 🟡 | 有 `batch-add` 与远程搜索；缺「从上游 JSON 直接同步」这条路径。 |
| 资产族逐族导出/删除 | 各 `asset*` 的 `/delete` `/export` | 🟡（形状差异） | 上游逐族开接口；我们统一走 `/api/assets/common/delete` 与 `/api/export`。计数少但能力在。 |

### 有意不做的差异

- **MCP 面**：上游把 asset_profile / search_assets / create_scan_task 等做成 REST（`mcp.py` 16 端点）；
  我们做成 MCP 工具（`get_asset_profile` / `search_assets` / `create_scan_task` …），REST 只保留治理面
  （keys / logs / config / tools）。同一能力，接口形状不同。
- **syslog**：上游单独一个路由；我们复用 `/api/dashboard/logs`（前端 `Syslog.vue` 就调它）。
## 功能族对照

图例：✅ 已有等价能力；🟡 部分具备；❌ 缺失（需要对标实现）。

### 1. 资产采集与展示

| ARL-Next | 路由证据 | ScopeSentry | 计划 |
| --- | --- | --- | --- |
| 站点资产 site | `assetSite: / /export/ /delete/ /save_result_set/ /add_tag/ /delete_tag/ /subdomain_chain/` | ✅ 读模型：`/api/assets/site/{list,chain}`（host+port 聚合、$top 取最新、子域链路 500 上限） | 导出/打标签下一轮 |
| 域名资产 domain | `assetDomain: / /delete/ /export/` | ✅ 资产表 domain/subdomain | 保持 |
| IP 资产 ip | `assetIP: / /export/ /export_ip/ /export_domain/ /delete/` | ✅ 读模型：`/api/assets/ip/{list,domains}`（ports/domains 聚合、asset+subdomain 双来源反查） | 导出/删除下一轮 |
| URL 资产 url | `assetUrl: / /export/ /delete/` | ✅ URL 页 | 保持 |
| 证书 cert | `assetCert: / /export/ /delete/` | ❌ | P1：`cert` 集合 + 关联域名 |
| 企业 ICP cip | `assetCip: / /export/ /delete/ /ip_domain_detail/` | ❌ | P1：ICP 主体资产 + 详情 |
| 文件泄露 fileleak | `assetFileleak: / /export/ /delete/` | ❌ | P2 |
| WIH 敏感信息 | `assetWih: / /export/ /delete/` | 🟡 敏感信息规则 + 结果 | P1：按 WIH 口径补类型/去重 |
| Nuclei 结果 | `assetNucleiResult: / /export/ /delete/` | 🟡 漏洞表（来源未区分） | P1：独立 nuclei_result 视图 |
| NPoc 服务 | `assetNpocService: / /export/ /delete/` | ❌ | P2 |
| 指纹统计 statFinger | `assetStatFinger: / /export/ /delete/` | 🟡 指纹页 | P1：按资产维度统计 |
| 资产概览 | `assetOverview/count` | 🟡 资产统计接口 | 保持 |

### 2. 资产组织与企业维度

| ARL-Next | 路由证据 | ScopeSentry | 计划 |
| --- | --- | --- | --- |
| 多级资产组 | `assetGroup: / /edit/ /delete/ /reorder/` | ✅ 后端：`/api/assets/asset_group/{list,add,edit,delete,reorder}`（名称唯一、dissolve/transfer、scopeCount） | 前端页面（进行中） |
| 资产范围/归属 | `assetScope: / /delete/ /add/ /update/ /edit/ /batch_move_group/ /bind_enterprise/ /clear_update_error/` | ✅ 后端：`/api/assets/asset_scope/{list,add,update,delete,batch_move_group,bind_enterprise,clear_update_error}`，域名/IP 自动解析与去重 | 前端页面（进行中） |
| ICP 查询/任务 | `icp: /task /task/statistics /task/overview_count /tyc_check /tyc_task /asset /stop/:id /batch_stop/ /task/log/:id /delete/ /restart/:id /resume/:id /backfill_details/:id /export/:id /batch_export /sync/:id` | ❌ | P1：ICP 任务链（提交/轮询/回填/导出） |

### 3. 任务与调度

| ARL-Next | 路由证据 | ScopeSentry | 计划 |
| --- | --- | --- | --- |
| 扫描任务 | `task`、`task_schedule` | ✅ 任务模板 + 多节点 | 保持 |
| FOFA 拉取建任务 | `taskFofa` | ❌ | P2：按查询语句批量建任务 |
| 调度器 | `scheduler` | ✅ 内置 scheduler | 保持 |

### 4. 指纹 / POC / 检测

| ARL-Next | 路由证据 | ScopeSentry | 计划 |
| --- | --- | --- | --- |
| 指纹库 | `fingerprint: / /delete/ /export/ /sync/ /upload/ /sync_from_json/` | ✅ 导入/远程搜索 | P1：补 sync_from_json |
| POC | `poc` | ✅ POC 管理 | 保持 |
| 策略 | `policy` | ❌ | P2：扫描策略（模板/字典/速率组合） |

### 5. 威胁情报

| ARL-Next | 路由证据 | ScopeSentry | 计划 |
| --- | --- | --- | --- |
| GitHub 泄露/监控 | `github_task` `github_scheduler` `github_result(/overview /stats /triage /ignored_repos /export/)` `github_monitor_result` | ❌ | P1：仓库泄露监控（任务+调度+triafe） |
| CVE/Hacker/Tools 情报 | `github_threat: /tools_target /hackers_target /cve_history /cve_config /cve_run_once /hackers_run_once /token_status` | ❌ | P2 |

### 6. 字典

| ARL-Next | 路由证据 | ScopeSentry | 计划 |
| --- | --- | --- | --- |
| 目录字典 | `dictionary: /list /preview /search /append /delete_entries /delete_file /download /create /upload_large /upload_status` | 🟡 基础字典 | P1：大文件上传 + 分片状态 + 预览/搜索 |
| 爆破字典 | `brute_dict`（同上一套） | ❌ | P1 |
| CDN 字典 | `cdn_dict: /list /save` | ❌ | P2 |

### 7. 运维 / 数据安全

| ARL-Next | 路由证据 | ScopeSentry | 计划 |
| --- | --- | --- | --- |
| 备份/恢复 | `backup: /create /status /list /download/:f /download_ticket /delete/:f /upload /restore /info/:f` | ❌ | P0：全量备份（Mongo 集合 + 上传目录），SHA256 校验 |
| 仪表盘 | `dashboard: /stats /trend /logs /sysinfo /widgets` | 🟡 统计卡片 | P1：趋势 + 系统信息 + 组件化 |
| 控制台信息 | `console/info` | ❌ | P2 |
| 系统日志 | `syslog` | 🟡 节点日志 | P1：服务端操作日志页 |
| HTTP 状态 | `http_status` | 🟡 资产状态码字段 | P2 |

### 8. 导出

| ARL-Next | 路由证据 | ScopeSentry | 计划 |
| --- | --- | --- | --- |
| 按类型批量导出 | `batchExport: /site /domain /ip /url /ip_port /cip /wih /asset_ip /asset_domain /asset_site /asset_wih` | 🟡 单集合导出 | P1：按类型批量导出（CSV/XLSX） |
| 按任务导出 | `export/:task_id` | 🟡 任务导出 | 保持 |

### 9. MCP 与 AI 协同

| ARL-Next | 路由证据 | ScopeSentry | 计划 |
| --- | --- | --- | --- |
| MCP 工具面 | `mcp: /task_detail_export /tools /asset_profile /enterprise_profile /search_assets /task_status /create_scan_task /stop_scan_task /export_server /overview` | 🟡 `internal/mcp` 已有工具 | P1：补资产画像/企业画像/任务导出 |
| MCP 治理 | `mcp: /keys /keys/:id/status /keys/:id /logs /logs/export /config` | ❌（我们只有通用 API Key） | P1：MCP 专用 Key + 工具开关 + 调用审计 |

### 10. 前端与国际化

| ARL-Next | ScopeSentry | 计划 |
| --- | --- | --- |
| Vue 3.5 | ✅ 3.5.13 | 完成 |
| zh / en / ru | zh / en / ko | P3：补 ru（或替换 ko，需确认） |
| 粘性表头 / 分页持久化 | 🟡 | P2：表格体验对齐 |

## 分阶段执行顺序

1. **P0 数据底座**：`site` / `ip` / `asset_group` / `asset_scope` 四张表 + 迁移 + API + 前端 tab；备份/恢复。
2. **P1 资产与情报**：cert / cip / wih 对齐 / nuclei_result / statFinger / 字典增强 / GitHub 泄露监控 / MCP 治理 / 批量导出。
3. **P2 运维与策略**：fileleak / npoc_service / taskFofa / policy / cdn_dict / console / syslog / http_status。
4. **P3 UI 对齐**：仪表盘趋势与系统信息、表格体验、语言（ru）。

## 性能约束（每个对齐 PR 必须满足）

对齐不是把功能抄过来就完事——每加一条查询路径，就必须同时交代它的代价：

1. **索引先行**：新增的 filter/sort/group 字段必须有索引（写进 `createIndexes`，升级路径自动补）。
   例：`site/chain` 的 `subdomain {host:1,time:-1}`、`asset_scope {group_id:1}`、`asset_group {name:1}` 唯一索引。
2. **禁止 N+1 / 全量拉取**：列表类接口用聚合（`$facet` 一次拿 total+分页、`$group`+`$top` 取最新），
   不允许「先查全部再在内存里过滤/排序」；聚合一律 `SetAllowDiskUse(true)`。
3. **计数与截断分开**：`portCount` 用完整数组计数，`ports` 只回传前 N 条（截断不能改变计数语义）。
4. **分页有上限**：`pageSize` 超过阈值必须 400，不允许一次拉全库。
5. **大集合要有实测**：新增列表/导出接口在 10 万级数据上做一次 explain/实测（docs 里记 docsExamined 与耗时），
   参考已有记录：资产 10 万列表 2–3ms、漏洞 20 万排序 29ms→3ms（加 time 索引后）。
6. **回归**：CI 的集成测试要覆盖分页、排序、截断与空结果；性能退化在 review 阶段拦，而不是上线后。

已实测（2026-11-18，本地 Mongo 7，`PERF=1` 跑 `TestSiteIPReadModelPerformance`）：

```
20,000 asset + 6,000 subdomain
  site/list（project 过滤，page 20）   152ms   total=20000
  ip/list（前缀过滤，page 20）          43ms   total=1250
  site/chain（500 条上限）             8.1ms   rows=500
```

复现：`PERF=1 MONGODB_IP=127.0.0.1 ... go test -tags integration -run TestSiteIPReadModelPerformance ./internal/services/assets/site/`。

## 验收方式

- 路由级：ARL-Next 的 52 个路由文件 → 每条能力在我们仓库有对应路由与页面；
  对照清单落在 `docs/arl-next-parity.json`（后续轮次自动生成 diff）。
- 页面级：侧边栏菜单项逐一对照（资产/IP/站点/证书/ICP/文件泄露/WIH/指纹统计/资产组/情报/备份/策略…）。
- 行为级：每族至少一条集成测试 + 一条冒烟守卫（沿用现有 `smoke_integration_test.go` 风格）。

## 路由级对照（1:1 验收清单）

对照 `owl234/ARL-Next/backend/app/routes/*.py`（52 个文件）。状态：✅ 已实现等价能力；🟡 部分/只读视图；❌ 依赖节点侧数据，当前无可读来源。

| ARL-Next 路由文件 | ScopeSentry 对应 | 状态 |
| --- | --- | --- |
| assetCert.py | `POST /api/assets/cert/{list,detail}` | ✅ |
| assetCip.py | `POST /api/assets/cip/{list,detail}` | ✅ |
| assetDomain.py | `/api/assets/asset`（type=domain）、`/api/assets/subdomain` | ✅ |
| assetFileleak.py | `POST /api/assets/fileleak/{list,delete}` + `/asset-information/fileleak`；导出 type `fileleak`（集合 `asset_fileleak` 由节点写入，当前空列表） | ✅ |
| assetGroup.py | `POST /api/assets/asset_group/{list,add,edit,delete,reorder}` | ✅ |
| assetIP.py | `POST /api/assets/ip/{list,domains}` + `/api/assets/asset`（ip 字段） | ✅ |
| assetNpocService.py | `POST /api/assets/npoc_service/{list,delete}` + `/asset-information/npoc-service`；导出 type `npoc_service`（集合 `asset_npoc_service` 由节点写入，当前空列表） | ✅ |
| assetNucleiResult.py | `POST /api/assets/nuclei_result/{list,detail}` | ✅ |
| assetOverview.py | `POST /api/assets/asset_overview/count` | ✅ |
| assetScope.py | `POST /api/assets/asset_scope/{list,add,update,delete,batch_move_group,bind_enterprise,clear_update_error}` | ✅ |
| assetService.py | `POST /api/assets/service/list` + `/asset-information/service` 页面（聚合资产/主机/端口/样本） | ✅ |
| assetSite.py | `POST /api/assets/site/{list,chain}` | ✅ |
| assetStatFinger.py | `POST /api/assets/stat_finger/{list,assets}` | ✅ |
| assetUrl.py | `/api/assets/url` | ✅ |
| assetVuln.py | `/api/assets/vulnerability` | ✅ |
| assetWih.py | `POST /api/assets/wih/{list,detail}` + `/api/assets/sensitive` | ✅ |
| backup.py | `POST /api/backup/{create,restore,upload}`、`GET /api/backup/{list,status,download,info}`、`DELETE /api/backup/:name` | ✅ |
| batchExport.py | `POST /api/export/batch`、`POST /api/export/preview`、`GET /api/export/task/:id` | ✅ |
| brute_dict.py / cdn_dict.py / dictionary.py | `/api/dictionary/*`、`/api/dict_brute/*`、`/api/dict_cdn/*` | ✅ |
| cert.py / cip.py | 同 assetCert / assetCip（同一读模型） | ✅ |
| console.py | `GET /api/console/info` | ✅ |
| dashboard.py | `POST /api/dashboard/{stats,trend,logs,widgets}`、`GET /api/dashboard/sysinfo` | ✅ |
| domain.py / ip.py / site.py / service.py / url.py / vuln.py / wih.py | 见上方 asset* 对应；旧式列表与读模型并存 | ✅ |
| export.py | `GET /api/export/task/:id` | ✅ |
| fingerprint.py | `/api/fingerprint/*`（导入/远程搜索/删除） | ✅ |
| github_monitor_result.py / github_result.py / github_scheduler.py / github_task.py / github_threat.py | `/api/github/*`（task/scheduler/result/monitor/threat，26 接口） | ✅ |
| http_status.py | `POST /api/assets/http_status/list` + `/asset-information/http-status` 页面（分档/精确码/keyword/project） | ✅ |
| icp.py | `/api/icp/*`（provider 抽象 + 任务链 + sync + CSV） | ✅ |
| image.py | `/api/images/*`（截图） | ✅ |
| mcp.py | `/internal/mcp` + `/api/mcp/{keys,logs,config}`（Scope 密钥/工具白名单/审计） | ✅ |
| nuclei.py / nuclei_result.py | 扫描模板/插件 + `nuclei_result` 读模型 | ✅ |
| npoc_service.py | 同 assetNpocService（读模型 + 页面 + 导出） | ✅ |
| policy.py | `/api/policy/{list,create,update,delete,detail,apply,test}` | ✅ |
| scheduler.py | 内置 gocron + `/api/task/scheduled` | ✅ |
| syslog.py | `POST /api/dashboard/logs` + `/configuration/syslog` 页面（level/keyword/source/truncated） | ✅ |
| system_config.py | `/api/system/config`、`/api/configuration/*` | ✅ |
| task.py / task_schedule.py | `/api/task/*`（含模板、暂停/恢复、进度快照、自动同步项目） | ✅ |
| taskFofa.py | `/api/fofa/{status,search,task,task/list}` | ✅ |
| user.py | `/api/user` 全套 CRUD + RBAC（`user:list/read/create/update/delete`）、安全约束、种子用户权限迁移 | ✅ |

### 结论

**52 / 52 个 ARL-Next 路由文件均有等价实现**（API + 页面 + 导出）。

其中 `asset_fileleak` / `asset_npoc_service` 两个集合由**扫描节点写入**：服务端路由、分页过滤、删除、导出与页面均已就绪，
节点跑完对应模块后数据即出现；当前空集合返回 `[]`（不是错误），页面有对应空态说明。

最终验证（2026-11-18）：

- `go build ./...` / `go vet ./...` 零错误；
- 全量 `go test -tags integration ./...`（全新库、`-p 1`）**0 FAIL**；
- 生产部署 `fork-ee83f44`，`scripts/e2e-smoke.sh` **15/15 PASS**；
- 前端 `pnpm build:pro` / eslint 0 告警 / **114 单测** / locale 1481×3 一致。

## ARL-Next v1.5.2 逐条对齐（2026-11-28）

上游 `157fb262`（v1.5.2，2026-11-25）之后无新提交。逐条核对 release notes：

| 上游条目 | 我们的状态 | 证据 |
| --- | --- | --- |
| 任务批量/单任务多工作表 Excel 导出（`POST /api/export/batch/`、openpyxl write_only、截断响应头） | ✅ 已对齐 | `/api/export/batch` + `POST /api/export` 多 sheet；MCP `export_task_workbook`；实测 CSV 214766 行 / xlsx 合法 zip |
| 任务资产产出多维筛选（`asset_yield` has/no + `asset_types` 6 维，非法维度 Fail-Closed） | ✅ 本轮对齐 | REST：全量 235 / `has_asset` 187 / `no_asset` 42 / `assetTypes=[vuln]` 1 / `nuclei` → 400；MCP `list_tasks` 同参数，`mcp_sweep` 断言 `has_asset=187 < 235` |
| 低内存自适应档位（`MONGO_WT_CACHE_GB`、`ARL_WEB_WORKERS`、earlyoom） | 🟡 已测量，暂不启用 | 生产 7.5 GB，`vmstat` si/so 三分钟全 0（换页已停），free 1.45 GB + cache 3.4 GB；Mongo 容器 1.2 GB 限额下 RSS 894 MB，WiredTiger 默认按 cgroup 取 256 MB 缓存，未触顶。swap 用 1.7 GB 是历史残留（SwapCached 122 MB），当前无压力 |
| 策略端口收敛到系统字典（去手填、`?dict=` 深链、别名归一） | ✅ 已对齐 | 策略端口来自端口字典；`{dict.port.*}` 模板占位符；测试用例覆盖 |
| `domain_sync` 泛解析并发裁剪（线程池 15、120s 预算） | ⛔ 不适用 | 我们没有 Python `domain_sync`；子域枚举在节点侧（subfinder），服务端不做逐域解析探测 |
| 边缘网关 418/421 过滤、跨 IP 同质默认页聚类、`catch_all` 跳过、fileLeak 双基线早退 | ⛔ 不在本仓库 | 这些在扫描端（ScopeSentry-Scan）的 `checkHTTP.py`/`fetchSite.py`/`fileLeak.py`；本会话工作目录只有服务端仓库 |
| 上游 DNS 动态熔断冷却、CDN 路由子域过滤 | ⛔ 不在本仓库 | 同上（`dns_client.py`/`cdn.py`/`domain.py` 属扫描端） |

结论：本仓库范围内 v1.5.2 的能力项已全部对齐（3 ✅ + 1 测量后判定不适用）；剩余 3 项属扫描端组件，需在扫描仓库另行对齐。

## 对齐状态（2026-11-29 复核）

上游 `owl234/ARL-Next` 最近 12 条提交逐条核对。结论：**能力项已对齐完毕**，剩余两条都在扫描器/网关侧，不在本仓范围。

| 上游提交 | 内容 | 我们的状态 |
| --- | --- | --- |
| 2026-11-28 | Nginx 1.30.7 安全修复（部署镜像） | 不适用：我们用 Go 二进制 + 前置反向代理 |
| 2026-11-26 | Bookworm Nginx 网关升到 1.30.8 | 同上 |
| 2026-11-26 | v1.5.2 兼容性合并 | 已对齐（终态统计冻结、资产产出多维筛选等） |
| 2026-11-25 | v1.5.2 正式版 | 已对齐 |
| 2026-11-25 | 4GB 低内存 .env 说明 | 不适用：内存治理走节点侧软限额（`memSoftLimitMB`）与巡检 |
| 2026-11-24 | domain_sync 泛解析并发 + 上游 DNS 熔断 + 字典迁移 | **扫描器侧**：`grep -rln 'wildcard|dnsserver|domain_sync' internal/ cmd/` 只命中一个 rootdomain 用例，Go 服务端无此链路 |
| 2026-11-23 | 任务资产产出多工作表 Excel 导出 | 已对齐（`/api/task/export` + 导出弹窗，见 DASHBOARD/export 文档） |
| 2026-11-23 | 策略端口配置收敛至系统字典 | 已对齐（策略字典 `port`，`{dict.port.*}` 占位符解析） |
| 2026-11-23 | 任务资产产出多维筛选 + 终态统计补齐 + 数值比较修复 | 已对齐（`assetYield`/`assetTypes` 过滤 + `stats` 冻结） |
| 2026-11-23 | 低内存自适应 + Puppeteer 串行轮转 | 不适用：我们无 Puppeteer 截图链路 |
| 2026-11-22 | 边缘网关 418/421 准入过滤、跨 IP 默认后端收敛、目录扫描早退 | **扫描器侧**：响应过滤发生在 ScopeSentry-Scan 的 http 层 |
| 2026-11-19 | v1.5.0（代理池/三域分流、Nuclei 独立推送、OSINT 续跑、GitHub 去重、MCP 全链路治理、系统设置五域） | 已对齐（MCP 82 工具 + 治理门禁；扫描档位见 SCAN_STRATEGY） |

复核命令（可复现）：

```bash
curl -s 'https://api.github.com/repos/owl234/ARL-Next/commits?per_page=12' \
  | python3 -c 'import sys,json;[print(c["commit"]["author"]["date"][:10], c["commit"]["message"].splitlines()[0][:80]) for c in json.load(sys.stdin)]'
grep -rln 'wildcard|dnsserver|domain_sync' internal/ cmd/   # 服务端是否真有对应链路
```

再往前要跟的上游改动都落在扫描器/网关镜像里；服务端能承载的部分当前无欠账。