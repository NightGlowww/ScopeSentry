package mcp

// 资产查询相关工具说明（写入 Tool.Description，供 MCP 客户端展示）
const listAssetsToolDesc = `查询 ScopeSentry 资产列表。

【接口与请求体】
与各资产 POST 接口一致（如 /api/assets/asset），body 为 models.SearchRequest:
- pageIndex, pageSize: 分页
- search: 搜索表达式字符串（对应前端 Csearch 搜索框 searchParams）
- filter: 精确过滤对象（对应前端项目/任务下拉、统计侧栏点击、表格列筛选，合并后传入）
- sort: 排序（多数类型无效）
- sid: 仅 SensitiveResult 敏感规则名

MCP 的 asset_type 会映射为后端 Index（如 asset→"asset"），再调用 helper.GetSearchQuery 生成 MongoDB 查询。

【前端参数分工（Csearch.vue + 各资产页）】
- search: 用户在搜索框输入的 DSL，如 domain=baidu && port==443
- filter.project: ElTreeSelect 选中值，为项目 ObjectID 数组（非项目名）
- filter.task: ElSelect 选中值，为任务名称数组（非任务 ID）；也可用动态标签 task=名称 写入 filter
- filter 还可来自: 统计侧栏点击(port/service/app/icon)、表格列筛选(statuscode/level/type/status 等)
- 注意: 各资产页 searchKeywordsData 里虽有 project 提示，但后端 SearchToMongoDB 未注册 project 关键字，project 只能走 filter

asset_type 可选值:
- asset: Web/端口资产
- RootDomain: 根域名
- subdomain: 子域名
- app: 移动应用
- mp: 小程序
- UrlScan: URL
- SensitiveResult: 敏感信息
- DirScanResult: 目录扫描
- crawler: 爬虫
- vulnerability: 漏洞
- PageMonitoring: 页面监控
- IPAsset: IP 聚合资产
- SubdomainTakerResult: 子域接管

【search 搜索表达式】（非 SQL，为自定义 DSL，由 helper.SearchToMongoDB 解析）
- field=value : 模糊匹配（不区分大小写）
- field=="值" : 精确匹配，含空格的值需加双引号
- field!="值" : 排除匹配
- expr1 && expr2 : 与
- expr1 || expr2 : 或
- (expr) : 分组
- 通用 search 字段(所有类型，SearchToMongoDB 注入): tag→tags、task→taskName（任务名）、rootDomain
- project 不支持 search，只能通过 filter.project（值为项目 ObjectID，list_projects / list_projects_data 获取）

各 asset_type 可用 search 关键字 → MongoDB 字段:
- asset: domain→host, ip, port, service, app→technologies, title, statuscode, icon→faviconmmh3, banner→metadata, type, body, header→rawheaders
- RootDomain: domain, icp, company
- subdomain: domain→host, ip, type, value
- app: name, icp, company, category, description, url, apk
- mp: name, icp, company, category, description, url
- UrlScan: url→output, input, source, resultId, type→outputtype（无 statuscode search；HTTP 状态码仅 filter.status）
- SensitiveResult: url, sname→sid, body, info→match, md5
- DirScanResult: url, statuscode→status, redirect→msg, length
- vulnerability: url, vulname, matched, request, response, level
- crawler: url, method, body, resultId
- PageMonitoring: url, hash, diff, response
- IPAsset: ip, domain→ports.server.domain, port→ports.port, service→ports.server.service, webServer→ports.server.webServer, app→ports.server.technologies
- SubdomainTakerResult: domain→input, value, type→cname, response

search 示例:
- domain=baidu && port==443
- port==443 && service=nginx
- title="后台登录" || body=admin

【filter 精确过滤】（与 search 可组合；由 helper.GetSearchQuery 追加 $and 条件）
JSON 对象，key 为筛选维度，value 为字符串数组（同 key 多值为 OR，不同 key 之间为 AND）。
仅 filterKeyCache 中定义的 key 生效，其余 key 会被忽略。

filter 要点:
- project: 仅 filter，值为项目 ObjectID 数组（数据库存 ID；展示时部分接口会转成项目名）
- task: 可用 search（task=="任务名"）或 filter.task（任务名数组）；值为 list_tasks 的 name，非任务 ID
- 同 key 多值 OR，不同 key AND；未在 filterKeyCache 的 key 被忽略

全局 filter key → MongoDB 字段:
- project→project, port→port, service→service, app→technologies
- icon→faviconmmh3, statuscode→statuscode, status→status
- level→level, type→type, color→color, tags→tags
- task→taskName, sname→sid

各 asset_type 可用 filter（未列出的 key 对该集合无效或字段不存在）:
- asset: project, port, service, app, icon, statuscode, type, task, tags
- RootDomain: project, tags
- subdomain: project, type, task, tags
- app / mp: project, tags
- UrlScan: status（HTTP 状态码，非 statuscode）, tags
- SensitiveResult: status（1未处理/2处理中/3忽略/4疑似/5确认/6已处理）, color, sname, tags
- DirScanResult: status（HTTP 状态码）, tags
- crawler: project, task, tags
- vulnerability: project, level（critical/high/medium/low/info/unknown）, status（1-6）, task, tags
- PageMonitoring: tags
- IPAsset: project, port, service, app（嵌套 ports 字段，走聚合查询）
- SubdomainTakerResult: tags

filter 示例（project 必须为 ObjectID，不可用项目名）:
- asset: {"project":["<项目ObjectID>"],"port":["443"]}
- subdomain: {"project":["<项目ObjectID>"],"type":["A"]}
- vulnerability: {"project":["<项目ObjectID>"],"level":["high"]}

search + filter 组合示例:
- search: domain=baidu && port==443，filter: {"project":["<项目ObjectID>"]}

注意:
- 勿在 search 中写 project=... 或 project=="..."（无效或与 && 组合报错）
- DirScanResult 的 HTTP 状态码仅在 search 中用 statuscode（如 statuscode==200）
- UrlScan 无 statuscode search；状态筛选用 filter.status
- SensitiveResult 规则名筛选：search 用 sname=规则名，或 filter.sname

【sort 排序】
- UrlScan / DirScanResult: 支持 {"length":"ascending"} 升序，其他值（含 "descending"、"-1"）为降序
- 其余类型: 服务端固定按 time 或 _id 排序，sort 参数通常无效

【sid 参数】
仅 SensitiveResult: 传敏感规则名称（sid），用于按规则展开匹配详情（对应前端点击规则名）`

// createScanTemplateToolDesc 扫描模板创建说明
const createScanTemplateToolDesc = `创建扫描模板。

扫描模板由多个【模块】(流水线阶段)组成，每个模块下挂载若干【插件】，
模块字段引用的是插件的 hash（不是插件 id，也不是插件名）。

推荐创建流程:
1. list_plugin_modules 获取全部模块名
2. list_plugins (可按 module 过滤) 获取每个模块下可用插件的 hash 与默认 parameter
3. 用 modules 参数指定 模块->插件hash列表 (同模块内按数组顺序执行)
4. 本工具会自动按插件默认参数回填 Parameters；如需自定义参数用 parameters 覆盖

参数说明:
- name: 模板名称，必填
- modules: {模块名: [插件hash,...]}，如 {"SubdomainScan":["d60ba73c..."],"PortScan":["..."]}
- parameters: 可选，{模块名: {插件hash: 参数字符串}}，覆盖默认参数
- vullist: 可选，nuclei POC 模板 ID 列表 (VulnerabilityScan 用 nuclei 时)
- template_json: 可选，完整 ScanTemplate JSON，优先级最高，用于高级自定义

创建成功返回模板 id，可直接用于 create_scan_task 的 template 参数。

常见模块: TargetHandler, SubdomainScan, SubdomainSecurity, PortScanPreparation,
PortScan, PortFingerprint, AssetMapping, AssetHandle, URLScan, WebCrawler,
URLSecurity, DirScan, VulnerabilityScan, PassiveScan`

// createScanTaskToolDesc 扫描任务创建说明（与 Web 端 /api/task/add 及 common.Insert 逻辑一致）
const createScanTaskToolDesc = `创建扫描任务。必填 name、node；template 为扫描模板 ObjectID（list_scan_templates 获取）。

【目标来源 targetSource】决定任务目标如何解析（对应 internal/services/task/common/common.go）:
- general: 直接使用 target 字段（多行/逗号分隔域名、IP、URL）
- project: 从关联项目读取目标，填 project（项目 ObjectID 数组），不需 target
- asset: 从 Web 资产库搜索选取，需 search；可选 project、filter、targetNumber
- RootDomain: 从根域名库搜索选取，需 search；可选 project、filter、targetNumber
- subdomain: 从子域名库搜索选取，需 search；可选 project、filter、targetNumber
- UrlScan: 从 URL 扫描结果搜索选取，需 search；可选 project、filter、targetNumber
- assetSource / RootDomainSource / subdomainSource / UrlScanSource: 从对应资产页创建任务
  - targetTp=search: 用 search + filter + project + targetNumber 筛选目标
  - targetTp=select: 用 targetIds 指定资产 ObjectID 列表

【search】搜索表达式，语法同 list_assets（如 task=="某任务名"、domain=^example.com）。
从子域名续扫示例: targetSource=subdomain, search=task=="子域名收集任务名"

【filter】精确过滤 JSON，与 search 可组合；filter.project 为项目 ObjectID。
【targetNumber】search 模式下的目标数量上限，0 表示不限制。
【targetIds】select 模式下选中的资产 ObjectID 列表。

【其他参数】
- allNode: 自动加入全部在线节点
- ignore / duplicates: 忽略目标、去重策略
- bindProject: 绑定项目（结果归属）
- scheduledTasks + cycleType/hour/minute/day/week: 计划任务

根域名完整信息收集推荐两阶段: 先用 general + 仅 SubdomainScan/SubdomainSecurity 扫根域名；
完成后用 subdomain + search=task=="上一任务名" 创建后续模块任务。`

// countAssetsToolDesc 资产数量统计（对应 POST /api/assets/common/total）
const countAssetsToolDesc = `统计符合条件的资产数量（Web 端分页「共 N 条」同源接口 /api/assets/common/total）。

参数与 list_assets 的 search/filter 一致，但不分页，只返回 total。
asset_type 取值同 list_assets（asset、RootDomain、subdomain、vulnerability 等）。

示例：统计某项目下子域名数量
{"asset_type":"subdomain","filter":{"project":["<项目ObjectID>"]}}

示例：统计某任务产生的 Web 资产
{"asset_type":"asset","search":"task==\"某任务名\""}`

// listAssetGroupsToolDesc 资产组（集团）列表说明
const listAssetGroupsToolDesc = `资产组（集团）列表，对应 POST /api/assets/asset_group/list。

资产模型三个层次（ARL-Next 口径，与「项目」不同）:
- asset_group: 企业/集团维度，一个集团下挂多个「地契」
- asset_scope: 地契，一段资产范围（域名/IP 文本，解析成 domainArray / ipArray）
- project: 扫描与资产归属的容器，与集团无关

返回字段:
- id / name / description / sortOrder / createdAt / updatedAt
- scopeCount: 该集团下的地契数量（列表接口现算，不是存储字段）

响应结构与 Web 端一致: {"items":[...], "total": N}。
拿到 id 后可传给 list_asset_scopes 的 groupId 查看该集团覆盖了哪些域名/IP。`

// listAssetScopesToolDesc 地契列表说明
const listAssetScopesToolDesc = `地契（asset_scope）列表，对应 POST /api/assets/asset_scope/list。

地契 = 一个集团下的一段资产范围。scope 是用户填写的原始文本，
解析后落成 scopeArray / domainArray / ipArray；企业名绑定在 enterprise 字段。

入参（与 handler 同名）:
- groupId: 集团 ObjectID（list_asset_groups 的 id）。传 unassigned 或 0 或留空 = 只看未分组；
           其它值必须是 24 位 ObjectID，否则直接报错（避免「过滤后空列表」的静默失败）
- search: 对地契名称与范围文本做不区分大小写的模糊匹配
- pageIndex / pageSize: 分页，默认 1 / 20，pageSize 上限 500

返回: {"list":[...], "total": N, "pageIndex": n, "pageSize": n}
每条含 id / name / scope / scopeType(domain|ip|mixed) / groupId / scopeArray /
domainArray / ipArray / enterprise / updateError / icpUpdatedAt / createdAt / updatedAt。

典型链路: list_asset_groups 找集团 → list_asset_scopes 看覆盖域名 → 用 list_assets 查这些域名的资产。
updateError 非空表示 ICP 回填或同步失败过，可结合 list_icp_tasks 排查。`

// listICPTasksToolDesc ICP 任务列表说明
const listICPTasksToolDesc = `ICP 主体查询任务列表，对应 POST /api/icp/task/list（复用 services/icp.ListTasks）。

任务模型:
- type=company: 用企业名查其备案域名；type=domain: 用域名反查备案主体
- state: 0 待开始 / 1 运行中 / 2 已暂停 / 3 已完成（2 可用 REST 接口 resume 续跑）
- stats: {total, done, found, failed}，found 只计真正新增入库的条数
- queries / cursor: 查询列表与下一个待处理下标（resume 从这里继续）

入参（与 handler taskListRequest 同名）:
- state: 0-3，闭集外直接报错（Mongo 里匹配不到会返回空列表，容易误判成「没有任务」）
- type: company / domain（大小写不敏感）；provider: manual / none / http
- keyword: 对 name 与 queries 做不区分大小写模糊匹配
- pageIndex / pageSize: 默认 1 / 20，pageSize 上限 200（与 REST 一致）

返回: {"list":[...], "total": N, "pageIndex": n, "pageSize": n}

注意: 创建/停止/恢复/删除任务属于写操作，MCP 未暴露（P1 只读面）。
查询结果落库、地契同步等能力在 REST /api/icp/{asset,sync,backfill_details}，同样未暴露。`

// listAuditLogsToolDesc 服务端操作日志说明
const listAuditLogsToolDesc = `服务端操作日志（audit_logs），对应 POST /api/audit/list。

日志由审计中间件写入，记录的是「谁、用什么方法、打了哪个路径、结果如何」：
- user: 操作者用户名；ip / ua: 来源
- method + path + status: 请求特征与响应码
- durationMs: 处理耗时；at: 时间（按时间倒序返回）

入参（直接对应 models.AuditLogQuery，与 handler 同名）:
- user: 用户名模糊匹配（不区分大小写）
- path: 路径模糊匹配，如 /api/task/add
- method: GET/POST/PUT/PATCH/DELETE/HEAD/OPTIONS，大小写不敏感；其它值报错
- statusMin: 0 不过滤；给 400 只看 4xx+5xx，给 500 只看 5xx；范围 100-599
- pageIndex / pageSize: 默认 1 / 20，pageSize 上限 200（与 REST 一致）

返回: {"list":[...], "total": N, "pageIndex": n, "pageSize": n}

用途示例:
- 自证「我刚才那次写操作有没有落到服务端」: path=/api/task/add, method=POST
- 找失败请求: statusMin=400
注意: 日志里能看到其他账号的路径与 IP，属运维敏感面；当前挂在 asset:read 下，
若需要隔离请按 docs/audit/MCP_TOOLS_V2.md 的说明拆 audit:read。`

// ---------------------------------------------------------------------------
// 写侧工具说明（v2.0.88：资产组 / 地契 / ICP 任务）
//
// 写侧描述比只读侧多两件必须写清的事：
//  1. 副作用范围（改了哪些集合、能不能撤回）——调用方是 Agent，没有 UI 二次确认；
//  2. 与 REST 的差异（哪些 handler 校验由 MCP 侧补、哪些没补）。
// ---------------------------------------------------------------------------

// createAssetGroupToolDesc 新建资产组（集团）
const createAssetGroupToolDesc = `新建资产组（集团），对应 POST /api/assets/asset_group/add（service.CreateGroup）。

资产模型三层（ARL-Next 口径）:
- asset_group: 企业/集团维度，一个集团下挂多个「地契」
- asset_scope: 地契，一段资产范围（域名/IP 文本，解析成 domainArray / ipArray）
- project: 扫描与资产归属的容器，与集团无关

入参（逐字对齐 models.AssetGroupAddRequest）:
- name: 必填，1-64 个字符（按 rune 计）；**全局唯一**，重名直接报错
  （asset group name already exists），不会静默覆盖或改名
- description: 可选，去首尾空白后落库
- sortOrder: 可选，整数，列表按它排序（同值按创建时间）

返回: {"ok": true, "id": "<ObjectID>", "name": "<落库名>"}
id 可直接作为 create_asset_scope 的 groupId。

scope: asset:write（只读 key 会被拒，这是有意设计：建集团会改变资产模型的归属结构）。
同名判断是「先查再插」，并发下仍可能插入同名记录（repo 无唯一索引），需要幂等请先 list_asset_groups 查重。
`

// deleteAssetGroupsToolDesc 删除资产组（高危，需 confirm）
const deleteAssetGroupsToolDesc = `【不可逆】删除资产组（集团），对应 POST /api/assets/asset_group/delete（按 id 逐个调用 service.DeleteGroup）。

集团记录会被物理删除，没有回滚接口；恢复只能重新创建（id 会变，地契归属需要重新挂）。
地契本身不删，按 action 决定它们的去向。

**必须显式传 confirm: true 才会执行**；缺省或传 false 直接报错（ErrConfirmRequired），
不会「默认执行」——MCP 调用方是 Agent，没有 Web 端那种二次确认弹窗。

入参:
- ids: 必填，1-20 个集团 ObjectID（list_asset_groups 的 id）。
        MCP 侧去空白、转小写、去重后再判上限，超过 20 报错（不静默截断）
- action: dissolve（默认）把该集团下的地契移入未分组；transfer 先划转到 targetGroupId 再删集团
- targetGroupId: action=transfer 时必填，必须是 **本批被删集合之外** 的有效集团 ObjectID
- confirm: 必填 true

两处比 REST 更严:
1. action=dissolve 时传了 targetGroupId 会报错（REST 会静默忽略，容易让人以为地契被划转了）；
2. targetGroupId 不允许是本批要删的集团之一（REST 只挡「自己转自己」，挡不住同批互转后一起删）。

返回: {"ok": bool, "action": "dissolve|transfer", "deleted": [...], "failed": [{id, error}], "total": n}
逐个集团执行，单个失败不影响其它（避免半路中断后调用方完全不知道哪些成功了）。
全部失败时返回 isError。

scope: asset:write
`

// createAssetScopeToolDesc 新建地契
const createAssetScopeToolDesc = `新建地契（asset_scope），对应 POST /api/assets/asset_scope/add（service.CreateScope）。

地契 = 一个集团下的一段资产范围。scope 是原始文本，服务端按空白/逗号/分号/换行切分，
解析后落成 scopeArray / domainArray / ipArray；资产匹配、ICP 回填、覆盖度统计都读解析后的数组。

入参（逐字对齐 models.AssetScopeAddRequest）:
- name: 必填，1-128 个字符
- scope: 必填，范围文本，如 "example.com,example.org" 或每行一个域名/IP。
         每个条目必须是合法域名或 IP，否则服务层报 "… is not a valid domain or ip"（带具体条目）
- scopeType: 可选，domain / ip / mixed；留空按内容自动推断（只有域名=domain，只有 IP=ip，都有=mixed）
- groupId: 可选，归属集团 ObjectID（list_asset_groups 的 id）；留空 = 未分组。
           未分组标记 "unassigned" / "0" 在写侧不接受（那是只读过滤用的标记）

本工具**没有 url 字段**：handler 的 AssetScopeAddRequest 只有 name/scope/scopeType/groupId，
「一段范围」的载体就是 scope 文本；给单个 URL 时请传 URL 的 host（地契存的是域名/IP，不是 URL）。

返回: {"ok": true, "id": "<ObjectID>", "name": "...", "scopeType": "...", "groupId": "...", "entries": n}

与 REST 的差异（都是收紧）:
- name / scope 为空或超长在 MCP 侧先报错，错误里带字段名（REST 由 binding:"required" 给 400）
- groupId 必须是 24 位 ObjectID 或留空；集团不存在报 asset group not found（服务层 ensureGroup）

scope: asset:write
`

// createICPTaskToolDesc 新建 ICP 任务
const createICPTaskToolDesc = `新建 ICP 主体查询任务，对应 POST /api/icp/task/create（service.CreateTask）。

数据链路: 企业名 → 备案域名（icp_result）→ 可用 create_asset_scope 落成地契。

入参（逐字对齐 models.IcpTaskCreateRequest）:
- type: company（企业名查域名，默认回落 domain）/ domain（域名反查主体）
- queries: 查询目标数组（企业名或域名）；为空时可用 target 给单个值
- target: 单值便捷字段，仅当 queries 为空时生效
- name: 任务名，留空 = 用第一个查询目标；上限 128 字符
- provider: manual / none / http；留空由服务端按当前配置解析（配置了 icp.provider_url 时是 http，
           否则 manual 只回读本地已有记录）。provider 决定「能不能真的查到新数据」

归一（mcpguard.Normalize* 补 handler 不做的事）:
- type 大小写不敏感，闭集外报错；domain 类型下 queries 自动小写、去尾点、去重；企业名保持原样
- queries 与 target 都为空直接报错（不静默建空任务）

**副作用**: 创建后会立刻 Claim 并启动后台 goroutine 跑全流程（与 REST 一致），
不在本请求里同步等待。进度用 list_icp_tasks 看 state/stats（0 待开始 / 1 运行中 / 2 暂停 / 3 完成）。
对外部 ICP 源有真实调用，按 provider 会产生配额消耗。

返回: {"ok": true, "task": {...icp_task 全字段...}}

scope: icp:write
`

// stopICPTasksToolDesc 停止（暂停）ICP 任务
const stopICPTasksToolDesc = `停止 ICP 任务（把 state 改成 2 已暂停），对应 POST /api/icp/task/stop（service.Stop）。

语义是**暂停**而不是删除，可逆:
- 任务、日志、已入库的 icp_result 全部保留
- 已完成的查询不会被重跑，恢复用 REST POST /api/icp/task/resume（从 cursor 继续）
- 只有 state ∈ {0 待开始, 1 运行中, 2 已暂停} 会被改；3 已完成不覆写

因为可逆，本工具**不要求** confirm（对比 delete_asset_groups 那种不可逆操作）。

入参:
- ids: 必填，1-50 个任务 ObjectID（list_icp_tasks 的 id）。
        MCP 侧去空白、转小写、去重后再判上限；非法 ObjectID 直接报错（不交给仓库层报 "invalid id"）

返回: {"ok": true, "updated": n, "ids": [...]}
updated 是**实际被修改**的文档数（ModifiedCount）：对已完成或已暂停的任务重复停止时 updated 会小于 len(ids)，
这是正常的，不是失败。

scope: icp:write
`
