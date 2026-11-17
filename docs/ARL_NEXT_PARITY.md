# ARL-Next 功能对齐（1:1）路线图

> 目标：`owl234/ARL-Next` 有的能力，ScopeSentry 都要有；前端升级到 Vue 3.5。
> 参照方式：以 ARL-Next 的路由/模型/页面作为**功能规格**，用我们自己的 Go + Vue 代码实现等价能力。
> （ARL-Next 是 GPL-3.0，直接搬源码会把我们的仓库也变成 GPL，所以按接口与行为对齐，不复制其实现代码。）

## 当前基线（2026-11-18）

| 项 | 状态 |
| --- | --- |
| Vue | **3.5.13**（本次升级；vue-router 4.6、pinia 2.3、element-plus 2.14、vue-i18n 9.14、vite 5.0.6、plugin-vue 5.2、vue-tsc 2.2） |
| 后端 | Go + Gin，171 条路由，Mongo 7 + Redis 7，多节点扫描 |
| 参照物 | ARL-Next `backend/app/routes/*.py`（52 个路由文件）、`frontend/src/views` |

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
