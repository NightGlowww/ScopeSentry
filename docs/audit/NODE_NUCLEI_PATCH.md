# 节点侧 nuclei -t 参数补丁草案

## 结论（生产实测，2026-11-19）

漏洞结果由扫描节点直写 Mongo（vulnerability + vulnerabilityDetail），服务端无消费端。断点在节点对 -t 的解析：只有显式单文件绝对路径能跑出结果。

| -t 形式 | 结果 |
| --- | --- |
| * （现状，模板里 "All Poc" 生成） | 5 分钟 0 命中（被当目录，静默空转） |
| *.yaml | 130s 0 命中 |
| . | 0 命中 |
| /apps/poc | 0 命中 |
| /apps/poc/<单文件>.yaml | 957ms 命中 2 次，落库 2+2 条 |
| a.yaml,b.yaml | 0 命中（不认逗号分隔） |

## 建议补丁（节点仓库）

1. -t 收到目录 / 通配符时自行展开为文件列表（不要交给 nuclei 内核）：

```go
case "t":
    for _, v := range value {
        if strings.ContainsAny(v, "*?") || isDir(v) {
            matches, _ := filepath.Glob(filepath.Join(global.PocDir, "*.yaml"))
            templates = append(templates, matches...)
        } else {
            templates = append(templates, filepath.Join(global.PocDir, v))
        }
    }
```

2. 执行前若 len(templates)==0，打印 error 日志并返回错误（避免「0 模板静默空转」）。
3. types.VulnResult 的 RootDomain 字段补 bson 标签 rootDomain（与读模型双拼写兼容）。

## 复现脚本

```bash
cat > /apps/poc/zz-canary.yaml <<YAML
id: zz-canary-sst
info: {name: ZZ Canary, author: audit, severity: high}
http:
  - method: GET
    path: ["{{BaseURL}}/"]
    matchers: [{type: status, status: [200]}]
YAML
# 分别用 -t * / -t /apps/poc / -t /apps/poc/zz-canary.yaml 跑一次
# 观察 db.vulnerability.countDocuments({name:/Canary/i})
```

## 服务端已做的部分

- 读模型 vulname/vulnname、rootdomain/rootDomain 双拼写解码 + keyword $or 分支（v2.0.118）；
- 扫描模板页面将新增提示：-t 当前节点仅支持单文件，全量模板会被静默忽略。
## 决定性实验（2026-11-19 04:18–04:24）

用全模块模板（VulList=All Poc，服务端追加 `-t *`）+ 一个必然命中的 canary 模板（放在 /apps/poc/zz-canary-sst-1795.yaml）：

```
04:18:59  [Plugins nuclei] target [https://example.com] start
04:23:58  [Plugins nuclei] target [https://example.com] run time: 4m59.29s
vulnerability(canary) = 0
```

对照：同一 canary 用显式单文件 `-t /apps/poc/zz-canary-….yaml` 执行 —— `run time: 957.86ms`，命中并落库 2 条。

结论：`-t *` **会跑满 5 分钟但不会执行 /apps/poc 下的用户 POC**（canary 一次都没被请求）；「全部 POC」在当前节点上等价于空跑。单文件是唯一可用形态。
