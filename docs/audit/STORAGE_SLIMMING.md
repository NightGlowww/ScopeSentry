# SensitiveBody 瘦身（存量）

> 目标：把「库为什么 19GB、备份为什么 3.3GB/9min」这件事讲清楚，并给出可控、可回滚的处置手段。

## 实测数字（2026-11-28 生产）

| 指标 | 值 |
| --- | --- |
| 全库 dataSize | 19,108 MB |
| SensitiveBody | **18,274 MB / 19,122 条**（占 95.6%） |
| 单条 body | 平均 **1,002,003 字节**，最大 5,929,266 字节，合计 19.16 GB 文本 |
| 全库 storageSize | 5,577 MB（WiredTiger 默认 snappy，约 3.4× 压缩） |
| 其余集合 | HttpBody 362MB、UrlScan 187MB、dictionary_entry 91MB、subdomain 62MB… |

文档结构极简：`{_id, md5, body}`。body 是命中的完整 HTTP 响应，按 md5 去重。
**写入方是扫描节点**（本仓库只读），所以这里能做的是存量瘦身；要止住增长得在扫描端给 body 封顶。

## 手段：`POST /api/system/storage/slim`

```bash
# 1) 先看（默认就是 dry-run，不写库）
curl -s -X POST $BASE/api/system/storage/slim -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' -d '{"collection":"SensitiveBody","limit":500}'

# 2) 确认后分批执行（必须同时给 dryRun:false 与 confirm:SLIM）
curl -s -X POST $BASE/api/system/storage/slim -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' \
  -d '{"collection":"SensitiveBody","dryRun":false,"confirm":"SLIM","limit":500}'
```

参数与门禁：

- `collection`：只允许 `SensitiveBody` / `HttpBody`，其它一律 400，避免拿任意集合试；
- `keepBytes`：默认 65536（64KB），按 **UTF-8 字符边界**截断（`truncateUTF8`，有单测）；
- `limit`：单次最多处理条数，默认 500、上限 2000，可重复调用续跑；
- `dryRun`：不传按 true；真正执行必须 `dryRun:false` + `confirm:"SLIM"`（与备份恢复的 `RESTORE` 同款门禁）；
- 只处理 `bodyTruncated != true` 的文档，反复调用不会重复截断；
- 截断后写 `bodyLen`（原始字节数）与 `bodyTruncated:true`，读侧能提示「已截断」，不会假装完整。

## 预期收益与代价

- 19,122 条里 **18,833 条**超过 64KB（其余本就小或已截断）；
- 按实测均值 1MB → 截到 64KB，本集合逻辑文本量从 19.1GB 降到约 0.7GB；
- **备份会立即变小**（备份按逻辑数据打包），磁盘文件则不会自己缩——WiredTiger 保留空闲页，
  想真正释放磁盘需要 `db.runCommand({compact:'SensitiveBody'})` 或 dump/restore；
- 代价：超过 64KB 的响应体只剩前 64KB，排查「后面那段里有什么」会丢失信息。

## 吞吐（2026-11-28 两轮实测，含一次性能修复）

第一版每批都慢得离谱：`limit=100` 约 24 秒、`limit=200` 约 23 秒、`limit=20` 还是 25 秒——**与条数无关的固定开销**。
用 mongosh 对照才发现 DB 侧同样的查询只要 133ms（IXSCAN on `_id`、扫 129 个键），
元凶是每批结尾那次 `CountDocuments`：它同样带 `$expr + $strLenBytes`，等于每批再全表扫一遍。

修掉之后（批次内不再 count、`afterId` 真正接通）：

| 操作 | 修复前 | 修复后 |
| --- | --- | --- |
| 单批 `limit=200`（干跑） | ~22,864 ms | **184 ms** |
| 第二批（带 `afterId` 续跑） | 与第一批同批数据（游标没接通） | 200 条、**46 ms**、`nextAfterId` 前进 |
| 全量统计 | 只能逐批估（~38 批 × 23s） | **一次聚合 26.1 s**：18,833 条、可回收 17,913,894,695 字节（17.9 GB） |
| `afterId` 超过最大 `_id` | — | 0 条、`hasMore=false`（边界正确） |

结论：**查询侧已经不再是瓶颈**（全量 18,833 条只需 95 批 × ~0.2s）；真实执行时主要成本是 19GB 正文的重写 IO，
建议 `limit=200` 逐批跑、每批用返回的 `nextAfterId` 续跑，`hasMore=false` 即到底。
剩余待处理量不要靠批次返回（已移除），用 `limit=0` 的全量干跑看。
## 反向验证（已做，可复用）

为了确认写路径真的可用且可回滚，做过一次单条试验：

1. 用同一条查询（`bodyTruncated != true` + 长度 > 64KB，按 `_id` 升序）选出服务会处理的那一条，复制到 `SensitiveBody_cibak`；
2. 调接口 `dryRun:false, confirm:SLIM, limit:1` → `updated:1`，`bytesBefore 89,059 → 65,536`；
3. 校验该文档出现 `bodyLen:89059`、`bodyTruncated:true`，body 长度 ≤ 64KB；
4. 从备份还原 body 并 `$unset` 两个标记，删除 `SensitiveBody_cibak`；
5. 复查：`chars=87575`、无标记、无残留集合，`remainingDocs` 回到 18,833。

这次试验还暴露了一个真问题：直接 `body[:keep]` 会把多字节字符切成两半（截 65536 字节后字符数只有 64296），
已改成按 UTF-8 边界回退，并加了汉字/emoji 的单测。

## 还没做（需要决定）

- **实际执行瘦身**：不可逆，等你确认窗口；
- **写入侧封顶**：在扫描端入库前截断（或只存前 N KB + 原文长度），否则跑完又会慢慢长回来；
- **按年龄淘汰**：文档没有时间字段，只能用 `_id` 的 ObjectId 时间戳做「保留最近 N 天」，
  需要先确认「老命中的响应体有没有留存价值」。
## 读侧同步（2026-11-28 已做）

瘦身会让「原始长度」与「库里实际保留的长度」分离，读侧必须如实反映，否则用户看到 64KB 会以为响应体本来就这么短。

- `FindBodyMetaByMD5` 读 `bodyLen` / `bodyTruncated`；
- `GET /api/assets/sensitive/body` 现在返回三个字段：`totalBytes`（原始字节数，来自 `bodyLen`）、
  `truncated`（返回内容少于原始）、`storedTruncated`（库里存的本身就不完整）；
- 前端「敏感信息」弹窗与「WIH 详情」分别提示：
  `asset.bodyTruncated`（正文较大，仅显示前 2MB）与 `asset.bodySlimmed`（数据库只保留了正文的一部分）。

实测往返（同一条 md5=4f89c45312eecf70 的文档）：

| 状态 | 返回 body | totalBytes | truncated | storedTruncated |
| --- | --- | --- | --- | --- |
| 瘦身后 | 64,296 字符（65,536 字节） | 89,059 | true | true |
| 还原后 | 87,575 字符 | 89,059 | false | false |
