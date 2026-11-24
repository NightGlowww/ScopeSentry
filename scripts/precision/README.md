# 漏洞发现精准度：测量与整改（2026-11-24）

## 测量方法

对 `ScopeSentry.vulnerability` 全量导出，按模板自身声称的判据，对 `vulnerabilityDetail.res` 做离线复验：

- Yonyou：正文必须含字面量 `[fonts]` 或 `/etc/passwd` 的 `root:.*:0:0:`
- Open Proxy：必须 200 且响应来自内网设备（含内网签名、非 CDN 错误页）
- Seeyon：必须含 `sessionid`/`JSESSIONID`/`jdbc:`
- Express 开发模式：必须含堆栈帧 `at ...:行:列`
- Exposed JSON：必须是 JSON 且含密钥类字段

## 结果（76 条）

| 模板 | 命中 | 真 | 误报 | 根因 |
| --- | --- | --- | --- | --- |
| Yonyou U8 CRM 任意文件读取 | 44 | 0 | 44 | 正则 `(?i)[fonts]` 是字符类，等于“含字母即命中” |
| Open Proxy To Internal Network | 28 | 0 | 28 | DSL 只按正文片段判断，CDN 的 400/403 也算命中 |
| Seeyon OA A6 | 2 | 0 | 2 | 无 sessionid/jdbc 标记 |
| Node.js Express 开发模式 | 1 | 0 | 1 | 状态 400 |
| Exposed JSON 配置 | 1 | 0 | 1 | 无密钥字段 |

**整改前 precision = 0/76 = 0%**

## 已做的整改

1. `PocList` 内两条 Yonyou 模板的正则改为 `(?i)\[fonts\]`（字面量），复验 76 条历史响应：0 条会再命中。
2. `Open Proxy To Internal Network` 从 `PocList` 删除，原文备份在 `PocListBackupFP20261124`（改写前不启用）。
3. 76 条历史误报标记 `status=3`（已忽略）+ `tags=["auto-fp-20261124"]`，不再计入待处理。
4. 扫描端源码加错误响应闸：`modules/vulnerabilityscan/nuclei/nuclei.go` 的 `responseStatus()`，4xx/5xx（保留 401/403）直接丢弃并打日志；`go vet` 通过，待随下个二进制发布。

## 工具

- `lint_templates.py`：对导出的 `PocList` 做静态检查。R1 字面量被写成字符类；R2 只有状态码 matcher；R3 唯一内容判据是常见单词。当前全量 14379 条：R1=2（已修）、R2=18、R3=9。
- `ctrl-template.yaml`：字面量对照模板，只打自有夹具（`/fixture-winini` 返回 `[fonts]`，`/fixture-plain` 返回普通 HTML），用于验证“有证据才命中”。

## 复现

```bash
# 导出模板集
mongosh --quiet < dump.js > pocs.json
python3 lint_templates.py pocs.json | head
```

