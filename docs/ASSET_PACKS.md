# 资产包（字典 / nuclei 模板）

ScopeSentry 的字典与 POC 是**数据**，不是代码。它们以「资产包」形式随二进制分发，
启动时播种进现有集合（`dictionary` + GridFS、`PocList`），
扫描参数、UI、MCP 工具读到的都是原本那套数据模型——产品不认识任何外部仓库。

## 1. 包格式

```text
<packdir>/
├── manifest.json
├── dict/<kind>/<name>.txt      # kind ∈ dir | subdomain | brute | cdn | other
└── poc/<template-id>.yaml      # 标准 nuclei 模板
```

```json
{
  "schema": 1,
  "source": {"name": "可选，仅溯源", "ref": "可选"},
  "packs": [
    {"kind": "dir", "name": "web-core", "description": "...",
     "embedded": true, "lines": 10046, "bytes": 151639, "sha256": "..."}
  ],
  "pocs": []
}
```

约束：

- `kind` 只能是 `dir`/`subdomain`/`brute`/`cdn`/`other`（见 `internal/models/dictionary_parity.go`）
- `name` 必须小写：扫描参数里 `{dict.<kind>.<name>}` 解析时整体转小写
- `embedded: true` 的字典进 `internal/constants/assets/dict`，随二进制分发；
  `false` 的进 `dist/dict-packs`，由运维按需导入（避免二进制膨胀）
- POC 文件名必须等于模板 `id`，且要有 `info` 块、`http`/`requests` 块和 `matchers`
- 模板 `id` 建议带命名空间前缀（内置模板统一 `scopesentry-`），避免与官方 nuclei-templates 撞 id

## 2. 安装

```bash
# 只校验包（sha256 / 行数 / YAML 结构），不落盘
python3 tools/assetpack/import.py --pack-dir /path/to/pack --check

# 安装：字典进 assets/dict 或 dist/dict-packs，POC 进 assets/poc，并重写 manifest
python3 tools/assetpack/import.py --pack-dir /path/to/pack
```

安装后重新编译即可内嵌。校验门禁：`go test ./internal/constants/` 会核对
「manifest → 文件 → sha256 → 行数」，任何漂移直接失败；`make assetpack-check` 额外跑
`nuclei -validate`。

## 3. 播种（运行时）

`SeedAssetPacks`（`internal/database/mongodb/assetpack_seed.go`）在启动时把内嵌包写入：

- 字典 → `dictionary`（meta）+ GridFS（内容，文件名 = meta `_id` 的 hex）
  行级索引不在这里建（`entries_synced=false`），首次预览/搜索时由字典服务回填，
  启动路径不背十几万行的写放大
- POC → `PocList`（`name/content/id/level/tags/time`），节点按已有协议下载执行

幂等靠**内容指纹**：`config.AssetPackVersion` 的值是内嵌包清单与模板内容的 sha256 前 16 位。
换了资产指纹就变，下次启动自动补写；运营手工删掉的包不会被复活（逐项存在性检查）；
单个包失败只记日志、不写指纹，下次启动重试。

## 4. 内置内容

| kind | 包名 | 说明 | 分发 |
| --- | --- | --- | --- |
| dir | `web-core` | 通用/后台/登录/API/支付回调 + 分语言 top100 + CMS/服务组件路径 | 内嵌 |
| dir | `web-full` | 全量路径（main.txt + asp/aspx/jsp/php），体量大 | dist |
| dir | `backup-paths` | 备份/源码泄露/敏感文件名与目录 | 内嵌 |
| dir | `webshell-paths` | WebShell 落地目录与常见文件名 | 内嵌 |
| subdomain | `subdomain-ext` | 主机名前缀扩展集（业务/区域/环境/云厂商） | 内嵌 |
| brute | `user-cn` | 拼音全名/姓/名/首字母 + CN 邮箱/手机号形态 | 内嵌 |
| brute | `user-common` | 通用用户名 top10000 + 西文常见名 | 内嵌 |
| brute | `pass-top` | 口令 top1000/top20000 + 后台专项口令 | 内嵌 |
| brute | `pass-middleware` | 中间件/运维平台默认口令 | 内嵌 |
| other | `param-names` | 参数名 Fuzz（sqli/xss/ssrf/lfi/rce/redirect/login） | 内嵌 |
| other | `shiro-keys` | Shiro rememberMe 常见硬编码密钥 | dist |
| other | `upload-bypass` | 上传绕过：后缀变形/Content-Type/.htaccess | 内嵌 |
| other | `http-headers` | UA、认证头、IP 伪造头、MIME、方法 | 内嵌 |
| other | `cred-pairs` | user:pass 凭据对 | 内嵌 |
| other | `cn-regions` | 行政区划地名 | 内嵌 |

POC 共 10 条（`scopesentry-*`），覆盖泛微/蓝凌/通达/用友/帆软/致远/Nacos/Shiro 的
确定性判据型检测（文件读取回显、未授权列表、认证绕过正反对照），全部通过 `nuclei -validate`。

## 5. 接一个新来源

1. 在**产品仓之外**写一次性转换器，把源数据整理成上面的包目录（排序固定、去重保序即可）
2. `python3 tools/assetpack/import.py --pack-dir <目录>` 安装
3. `go test ./internal/constants/` + `make assetpack-check` 通过后提交资产

转换器不进产品仓：它依赖外部目录结构，属于一次性胶水，删掉源数据时一起删。
产品只依赖 `internal/constants/assets` 这份数据。

## 6. 来源与合规

内置字典与模板由公开安全资料整理而来，属派生数据（行级字典、判据型模板），
包清单只保留 `description` 与哈希，不记录外部路径。需要回溯时看 git 历史的引入提交；
移除某来源只需删除对应 `assets/dict/<kind>/<name>.txt` 或 `assets/poc/<id>.yaml` 并重新安装清单。

## 7. POC 更新语义（导入 / 同步）

POC 不是只增不改的静态表：上游模板会改 matcher、修误报、加请求变体。导入路径按「内容」而不是「id 是否存在」处理。

- `POST /api/poc/data/import` 默认 **overwrite=true**：同 template id 内容变了 → 原地更新，**保留 `_id`**（扫描模板 `VulList` 引用不断）；内容没变 → 计入 `repeat_num`；新 id → 新增。
- 传 `overwrite=false`（form 字段或 query）保留旧行为：只增不改，适合把某个受控包钉死。
- 响应字段：`success_num`（新增）/ `update_num`（更新）/ `repeat_num`（未变）/ `error_num`。
- 内容比较用归一化指纹：统一换行、去行尾空白、去尾部空行后再算 sha256，所以上游只改格式不会触发全量重写；落库内容仍是原文。
- 老库没有 `content_hash` 的文档：只为发生 id 冲突的那几条补算一次，不扫全量内容。
- `Nuclei Monitor` 用 `UpsertPoc` 增量同步：`created` 走通知/建任务，`updated` 覆盖文件，`unchanged` 跳过；整页都没有变化即认为追平并结束本轮。
- 节点侧收到 `poc: add:<ids>` 后重新拉取模板文件——新增和更新是同一语义（写入/覆盖）。

> 验证：`SCOPESENTRY_TEST_MONGODB_URI=... go test -tags integration ./internal/services/poc/ -run TestImportPocOverwriteSemantics`，覆盖新增 / 更新保 `_id` / 原样重导 / 仅空白差异 / `overwrite=false` / Upsert 三态。
