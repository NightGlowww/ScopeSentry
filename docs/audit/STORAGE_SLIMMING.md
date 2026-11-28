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

## 吞吐（实测）

`limit=100` 单次约 24 秒（要读全文再写回），`limit=500` 约 2 分钟。
18,833 条约需 **38 次 × 500 条 ≈ 75 分钟**，建议分批跑、每次跑完看 `remainingDocs`。

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