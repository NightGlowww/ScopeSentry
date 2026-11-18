# 项目详情页 4 个 Tab 全 404 —— 修复报告

- 目标孤岛：`docs/audit/ISLANDS.md` 第 4.1 节 / 修复清单 **P0-3**（项目详情页整体 404）
- 结论口径：后端「完全未注册」→ 本次补齐 7 条路由 + 最小可用聚合；前端字段对齐真实返回、补空态与失败提示
- 约束遵守：**未部署、未重启生产、未写生产库**；生产侧只有只读查询（`mongosh` find/aggregate/explain）
- 日期：2026-11-19

---

## 1. 根因判定：不是「注册了没实现」，而是「完全未注册」

| 检查 | 结果 |
| --- | --- |
| `grep -rn 'project_aggregation' internal/`（Go 源码） | **0 命中**：没有 handler、没有 route、没有 service/repository |
| 前端调用点 | 7 处，全部指向 `/api/project_aggregation/*`（`frontend/src/api/ProjectAggregation/index.ts`） |
| 历史实现 | Python 版 `api/project_aggregation.py`（git `9ac4e1d`，325 行）随 Go 重写整体丢失；Go 版重写时只保留了 `/api/project/*` 的 CRUD |
| 运行时证据 | HTTP 层测试里未注册时是 404；本次注册后同样请求返回 200/400/404（业务）——见第 5 节 |

即：**路由组整组不存在**，不是 handler 空实现。

---

## 2. 前端实际调用契约（修复前）

`frontend/src/api/ProjectAggregation/index.ts` + 4 个组件：

| 前端调用 | 组件 | 发送体 | 期望 `data` |
| --- | --- | --- | --- |
| `POST /project/info` | Dashboard.vue:60 | `{id}` | `{name,tag,scheduledTasks,hour,AssetCount,root_domains[],next_time}` |
| `POST /project/asset/count` | Dashboard.vue:61 | `{id}` | `{subdomainCount,vulCount}` |
| `POST /project/vul/statistics` | Dashboard.vue:121 | `{id}` | `[{_id:<level>,count}]`（饼图直接吃 `_id`/`count`） |
| `POST /project/vul/data` | Dashboard.vue:194 | `{id}` | `{list:[{time,level,vulname,url,matched}]}` |
| `POST /project/subdomain/data` | Subdomain.vue:170 | `{search,filter,fq}` | `{list:[{id,host,type,value[],ip[],time}]}`（`value/ip` 直接 `forEach`） |
| `POST /project/port/data` | Port.vue:158 | `{search,filter,fq}` | `{list:[{id,port,host,ip,service,time,count?}]}`（`count` 渲染成 `(n)`） |
| `POST /project/service/data` | Service.vue:164 | `{search,filter,fq}` | `{list:[{id,service,host,ip,port,time,count?}]}` |

前端还有三个隐含约束（修复中必须满足，否则页面直接崩）：

1. `Subdomain.vue` 的 `value`/`ip` formatter 无条件 `forEach` → 后端 `null` 必须归一成 `[]`；
2. `Dashboard.vue` 的 `getVulLevelName(level)` 走 `levelMap[level].flag` → 未知等级会抛异常（已在前端加兜底）；
3. 表格 `rowKey="id"` → 每行必须有 `id`（聚合行用分组键充当 id）。

---

## 3. 后端实现（新增 5 个文件 + 1 处注册）

### 3.1 路由（7 条，全部 `middleware.AuthMiddleware()`）

`internal/api/routes/project_aggregation/project_aggregation.go` → 在 `internal/api/routes/routes.go` 的 `/api` 组内注册：

```
POST /api/project_aggregation/project/info
POST /api/project_aggregation/project/asset/count
POST /api/project_aggregation/project/vul/statistics
POST /api/project_aggregation/project/vul/data
POST /api/project_aggregation/project/subdomain/data
POST /api/project_aggregation/project/port/data
POST /api/project_aggregation/project/service/data
```

统一请求体（`models.ProjectAggregationRequest`）：

```json
{ "id": "<项目ObjectID>", "pageIndex": 1, "pageSize": 20,
  "search": "", "filter": {"project": ["<ObjectID>"]}, "fq": {"sub_host": "mail."},
  "startTime": "2026-11-18 00:00:00", "endTime": "2026-11-19 23:59:59" }
```

- `id` 与 `filter.project` 二者取其一即可（后者兼容前端现有写法）；
- `startTime/endTime` 可选，接受 `YYYY-MM-DD` 或 `YYYY-MM-DD HH:MM:SS`（日期形态自动补 `00:00:00`/`23:59:59`）；
- 错误映射：非法 id / 非法时间 / `pageSize>200` → **400**；项目不存在 → **404**；其它 → **500**。

### 3.2 项目匹配口径

生产实测：`project` 字段在 `asset/subdomain/UrlScan/vulnerability/SensitiveResult/DirScanResult/RootDomain/crawler` 是**字符串**（ObjectID 十六进制），在 `IPAsset` 是**字符串数组**。因此统一用：

```js
{ project: { $in: [ "<hex>", ObjectId("<hex>") ] } }   // 兼容两种历史写法，数组字段同样命中
```

### 3.3 聚合语句（`$facet` 一次拿 total + 分页，全部 `SetAllowDiskUse(true)`）

子域名（索引 `{project:1,time:-1}` 提供顺序）：

```js
[ { $match: { $and: [ { project: { $in: [hex, ObjectId(hex)] } } ] } },
  { $sort: { time: -1 } },
  { $project: { id: { $toString: "$_id" }, host: 1, type: 1,
                value: { $ifNull: ["$value", []] }, ip: { $ifNull: ["$ip", []] },
                rootDomain: 1, time: 1 } },
  { $facet: { total: [ { $count: "n" } ],
              rows:  [ { $skip: 0 }, { $limit: 20 } ] } } ]
```

端口聚合（`count` = 该端口资产数，样例字段取该端口**最近一条**资产）：

```js
[ { $match: { $and: [ projectMatch, { port: { $nin: ["", null] } } ] } },
  { $sort: { time: -1 } },
  { $group: { _id: "$port", count: { $sum: 1 },
              host: { $first: "$host" }, ip: { $first: "$ip" },
              service: { $first: "$service" }, time: { $max: "$time" } } },
  { $match: { _id: { $nin: ["", null] } } },
  { $addFields: { id: { $toString: "$_id" }, port: { $toString: "$_id" } } },
  { $facet: { total: [ { $count: "n" } ],
              rows:  [ { $sort: { count: -1, _id: 1 } }, { $skip: 0 }, { $limit: 20 } ] } } ]
```

服务聚合（`service` 为空时退到 `type`，与 Python 版一致）：

```js
{ $group: { _id: { $cond: [ { $gt: [ { $strLenCP: { $ifNull: ["$service",""] } }, 0 ] },
                           "$service", "$type" ] },
            count: { $sum: 1 }, host: { $first: "$host" }, ip: { $first: "$ip" },
            port: { $first: "$port" }, time: { $max: "$time" } } }
```

漏洞分布 / 列表：

```js
// statistics
[ { $match }, { $group: { _id: "$level", count: { $sum: 1 } } },
  { $match: { _id: { $nin: ["", null] } } }, { $sort: { count: -1, _id: 1 } } ]
// data
[ { $match }, { $sort: { time: -1 } },
  { $project: { id: { $toString: "$_id" }, url:1, vulname:1, level:1, time:1, matched:1 } },
  { $facet: { total: [{$count:"n"}], rows: [{$skip},{$limit}] } } ]
```

资产计数（`/asset/count`）：6 次 `CountDocuments({project:{$in:[...]}})`，命 `{project:1}` 索引。

搜索/过滤复用统一解析器：`helper.GetSearchQuery`（`search` DSL + `filter` 走 `filterKeyCache`），
表头模糊搜索（`fq`）按 Tab 本地映射（不改全局 `fuzzyQueryKeyCache`，避免影响其它页面）：

| Tab | fq key → 字段 |
| --- | --- |
| 子域名 | `sub_host→host`,`sub_value→value`,`sub_ip→ip` |
| 端口 | `port_port→port`,`port_domain→host`,`port_ip→ip`,`port_protocol→service` |
| 服务 | `service_service→service`,`service_domain→host`,`service_ip→ip`,`service_port→port` |

> 注：全局缓存里 `service_service→type,webServer,protocol` 与本 Tab 的返回字段（`service`）不一致，
> 若直接复用会导致"搜 https 搜不到 https 行"，所以本包内做了独立映射，并把正则做了 `QuoteMeta`。

---

## 4. 索引与性能（`docs/ARL_NEXT_PARITY.md` 性能约束逐条对照）

| 约束 | 落实情况 |
| --- | --- |
| 索引先行 | 未新增字段：`project`、`time`、`level`、`service`、`port` 均已有索引，生产已存在 `{project:1}`、`{project:1,time:-1}`、`{project:1,level:1,time:-1}`、`{service:1,time:-1}`（`internal/database/mongodb/initdb.go:503+ / 715 / 764`），无需迁移 |
| 禁止 N+1 / 全量拉取 | 列表一律单条 `$facet`（total+rows 一次聚合）；端口/服务用 `$group`+`$first`，不再像 Python 版那样"先 group 再逐组 find" |
| 聚合 SetAllowDiskUse(true) | 所有 `Aggregate` 调用均带 `options.Aggregate().SetAllowDiskUse(true)` |
| 分页上限 | `pageSize>200 → 400`（service 层 `ErrPageSizeTooLarge`，HTTP 层验证见 5.1） |
| 大集合实测 | 生产 explain 见下（`docsExamined == nReturned`，无内存 SORT） |

生产 explain（`executionStats`，项目 `artex-own-infra`）：

```
## explain subdomain facet
[{"plan":"{\"stage\":\"PROJECTION_SIMPLE\",...\"inputStage\":{\"stage\":\"FETCH\",\"inputStage\":
   {\"stage\":\"SORT_MERGE\",\"sortPattern\":{\"time\":-1},\"inputStages\":[{\"stage\":\"IXSCAN\",
   \"keyPattern\":{\"project\":1,\"time\":-1}...}]",
  "nReturned":2,"execMs":11,"docsExamined":2,"keysExamined":2},
 {"stage":"$project"},{"stage":"$facet"}]

## explain asset port groups
[{"plan":"...\"stage\":\"IXSCAN\",\"keyPattern\":{\"project\":1,\"time\":-1}...\"SORT_MERGE\"",
  "nReturned":1,"execMs":1,"docsExamined":1,"keysExamined":1},
 {"stage":"$group"},{"stage":"$addFields"},{"stage":"$facet"}]
```

结论：`IXSCAN` + `SORT_MERGE`（索引提供排序，无阻塞式 `SORT`），`docsExamined == nReturned`（无多余文档扫描）。

---

## 5. 验证

### 5.1 路由级集成测试（真实路由 + 鉴权中间件 + Mongo/Redis，本地 throwaway 容器）

`internal/api/routes/project_aggregation_integration_test.go`

```bash
docker run -d --name pd-mongo-test -p 27111:27017 mongo:7.0.28
docker run -d --name pd-redis-test -p 27112:6379 redis:7.4-alpine
MONGODB_IP=127.0.0.1 MONGODB_PORT=27111 MONGODB_DATABASE=projectagg_test MONGODB_USER= MONGODB_PASSWORD= \
REDIS_IP=127.0.0.1 REDIS_PORT=27112 \
go test -tags integration -count=1 -v -run TestProjectAggregationRoutes ./internal/api/routes/
```

实测输出（节选，状态码即证据）：

```
--- PASS: TestProjectAggregationRoutes (0.02s)
    --- PASS: TestProjectAggregationRoutes/未认证被拦下      (401 /api/project_aggregation/project/info)
    --- PASS: TestProjectAggregationRoutes/project_info     (200)
    --- PASS: TestProjectAggregationRoutes/asset_count      (200)
    --- PASS: TestProjectAggregationRoutes/vul_statistics   (200)
    --- PASS: TestProjectAggregationRoutes/vul_data         (200)
    --- PASS: TestProjectAggregationRoutes/subdomain_data   (200)
    --- PASS: TestProjectAggregationRoutes/port_data        (200)
    --- PASS: TestProjectAggregationRoutes/service_data     (200)
    --- PASS: TestProjectAggregationRoutes/参数与不存在项目   (pageSize=201→400, 非法 id→400, 缺失项目→404)
    --- PASS: TestProjectAggregationRoutes/时间范围          (200, total=1)
```

### 5.2 聚合语义集成测试

`internal/services/projectaggregation/service_integration_test.go`（同一套本地库）：

```
ok  github.com/Autumn-27/ScopeSentry/internal/services/projectaggregation  0.369s
ok  github.com/Autumn-27/ScopeSentry/internal/api/routes                   0.609s
```

覆盖：`$facet` 分页（total/跨页）、端口 count 聚合与"最近一条"样例、服务分组含空 service 回退、
子域名 `null→[]` 归一、时间范围（含仅日期形态）、空项目返回 `[]` 而非 `nil`、四类参数错误分类、
`filter.project` 与 `id` 等价。

### 5.3 生产只读实测（`ssh prod-tx-sh-app-02` + `docker exec scopesentry-mongodb mongosh`）

项目 `artex-own-infra`（`6afad734e3460b716c5723f7`），与后端完全相同的管道：

```
## info => {name:"artex-own-infra", tag:"artex", scheduledTasks:false, hour:0,
            root_domains:["dreamxy.net"], AssetCount: Long(13)}
## count asset        => 1
## count subdomain    => 2
## count vulnerability=> 0
## count UrlScan      => 9
## count SensitiveResult => 0
## count DirScanResult   => 0
## vul statistics     => []
## subdomain facet    => [{"total":[{"n":2}],"rows":[
     {"host":"mail.dreamxy.net","type":"A","value":[],"ip":["104.21.92.201","172.67.197.177"],"rootDomain":"dreamxy.net","time":"2026-11-16 18:14:00"},
     {"host":"dreamxy.net","type":"SOA","value":[],"ip":[],"rootDomain":"dreamxy.net","time":"2026-11-16 18:13:44"}]}]
## port groups        => [{"total":[{"n":1}],"rows":[
     {"port":"443","count":1,"host":"mail.dreamxy.net","ip":"104.21.92.201","service":"https","time":"2026-11-16 18:14:42"}]}]
## service groups     => [{"total":[{"n":1}],"rows":[
     {"service":"https","count":1,"host":"mail.dreamxy.net","ip":"104.21.92.201","port":"443","time":"2026-11-16 18:14:42"}]}]
```

**聚合结果非空**：子域名 2 / 端口 1 / 服务 1 / 资产 1 / URL 9。漏洞与敏感信息为 0（见第 6 节，属数据链缺口，不是接口问题）。

### 5.4 构建/静态检查

```bash
GOPROXY=https://goproxy.cn,direct go build ./...        # exit 0
go vet ./internal/{models,repositories/projectaggregation,services/projectaggregation,api/handlers/project_aggregation,api/routes/...}   # exit 0
gofmt -l <新增文件>                                      # 无输出
cd frontend && pnpm build:pro                            # Build successful. Please see dist-pro directory
cd frontend && pnpm ts:check | grep -E 'Project/detail|ProjectAggregation'   # 无输出（本孤岛 0 类型错误）
```

---

## 6. 前端对齐与容错

| 项 | 处理 |
| --- | --- |
| 字段对齐 | `api/ProjectAggregation/index.ts` 重写为与后端一致的 7 组类型（含 `total`、`pageIndex`、`pageSize`、`rootDomain`、`count`） |
| 分页 | 三个 Tab 接 `useTable` 的 `currentPage/pageSize/total`，`Table` 走 `v-model:currentPage` + `:pagination`；原来只传 `{list}`、不传页码，永远只能看第一页 |
| 时间范围 | 每个 Tab 增加 `ElDatePicker type="datetimerange" value-format="YYYY-MM-DD HH:mm:ss"`，改动即回第 1 页重查 |
| 空态 | `Table :empty-text="t('common.noData')"`；Dashboard 饼图无数据时显示占位块，四个统计卡片空列表时显示"暂无数据" |
| 失败提示 | axios 拦截器保留 toast；组件内再加 `loadError` + `ElAlert`（页内常驻可重试提示）+ `ElMessage.error`；`Dashboard` 四个统计接口改 `Promise.allSettled`，单接口失败不再让整块 skeleton 卡死 |
| 渲染兜底 | `value/ip` formatter 加 `(x || [])`；`getVulLevelName` 对未知等级兜底为原值；`vulTableData` 由 `let + reactive` 改为 `ref`（原来整体赋新数组会丢响应性，漏洞表格永远空白） |
| 删除入口 | 端口/服务 Tab 的行是聚合行（id = 分组键），移除删除按钮与 selection 列，避免"按端口号删资产"的误操作 |

---

## 7. 剩余缺口（不在本次改动范围 / 依赖其它孤岛）

1. **漏洞 Tab 仍然为空 —— 数据链断，不是接口问题**：`vulnerability` 集合 0 条（ISLANDS P0-1：nuclei 写死 `-proxy 127.0.0.1:7890`，容器内该端口 closed）。接口已就绪，`P0-1/P0-2` 修好后自动有数据。
2. **敏感信息 / 目录扫描计数为 0**：同项目下 `SensitiveResult`/`DirScanResult` 的 `project` 字段是空串（写入时未回填），因此按项目聚合不到。属 AutoBind/任务完成后回填链路问题（ISLANDS 7.1 第 8 条），需要单独修。
3. **`IPAsset` 基本无项目归属**：17 条里 `project` 为 `[]` 的占 15 条（`IPAssetHandle` 未回填 project）；本次聚合未把 IPAsset 计入端口/服务 Tab，改为只以 `asset` 为准（避免同一端口重复计数）。如需按 IP 视角聚合，应先修 IPAsset 的 project 回填。
4. **`POST /api/asset/statistics/title` 仍 404**（ISLANDS P2-1：handler 在、路由未注册）：Dashboard"标题分布"卡片恒空。前端已按空数组容错，不再拖垮其它统计卡；补路由属 P2-1，本次未动（避免与其它工作范围交叉）。
5. **`AssetCount` 字段可能是陈旧值**：项目文档里 `AssetCount=13`，实际 `asset` 文档 1 条（该字段由任务完成回调维护，见 ISLANDS P1-3）。顶部"总资产数"读它、底部"资产/子域/漏洞"读实时计数，两者口径不同，属既有语义，未改。
6. **`next_time` 恒为空**：只有 `scheduled._id == 项目id` 存在时才返回；当前库里 `scheduled` 只有 `github_intel_tick`，项目定时扫描的调度任务在 Go 版尚未落地。
7. **未部署**：本次只改本地仓库 + 本地构建（`go build`/`pnpm build:pro`），生产仍是 `autumn27/scopesentry:v2.0.109`；上线后需重跑一次 `scripts/e2e-smoke.sh` 并抽查项目详情页四个 Tab。

---

## 8. 变更文件清单

后端（新增 5 + 修改 1）：

| 文件 | 行数 | 说明 |
| --- | ---: | --- |
| `internal/models/project_aggregation.go` | 108 | 请求/响应/行模型 |
| `internal/repositories/projectaggregation/repository.go` | 296 | `$facet`/`$group` 聚合、Explain |
| `internal/services/projectaggregation/service.go` | 399 | 项目匹配、fq 映射、时间范围、分页上限、错误分类 |
| `internal/api/handlers/project_aggregation/project_aggregation.go` | 158 | 7 个 HTTP handler + 错误映射 |
| `internal/api/routes/project_aggregation/project_aggregation.go` | 31 | 路由组注册 |
| `internal/api/routes/routes.go` | +4 | 挂载路由组 |
| `internal/services/projectaggregation/service_integration_test.go` | 313 | 聚合语义集成测试 |
| `internal/api/routes/project_aggregation_integration_test.go` | 270 | 路由/鉴权/参数集成测试 |

前端（修改 7）：

`frontend/src/api/ProjectAggregation/index.ts`、`views/Project/detail/ProjectDetail.vue`、
`views/Project/detail/components/{Dashboard,Subdomain,Port,Service}.vue`。

---

## 9. 复现命令汇总

```bash
# 1) 后端构建
cd /Users/night/Workspaces/security_workspace/_tools/ScopeSentry && GOPROXY=https://goproxy.cn,direct go build ./...
# 2) 集成测试（本地 throwaway mongo/redis；跑完 docker rm -f pd-mongo-test pd-redis-test）
MONGODB_IP=127.0.0.1 MONGODB_PORT=27111 MONGODB_DATABASE=projectagg_test MONGODB_USER= MONGODB_PASSWORD= \
REDIS_IP=127.0.0.1 REDIS_PORT=27112 \
go test -tags integration -count=1 ./internal/services/projectaggregation/ ./internal/api/routes/ -run TestProjectAggregation
# 3) 前端构建
cd frontend && pnpm build:pro
# 4) 生产只读抽查（不写库）
ssh prod-tx-sh-app-02 "docker exec scopesentry-mongodb mongosh -u admin -p *** --authenticationDatabase admin \
  ScopeSentry --quiet --eval 'db.subdomain.aggregate([{\$match:{project:\"6afad734e3460b716c5723f7\"}},{\$count:\"n\"}]).toArray()'"
```
