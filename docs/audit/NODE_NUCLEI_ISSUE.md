# 待提交 issue（节点仓库）

> 仓库地址不在本工作区，这里准备好可直接粘贴的 issue 正文。

## 标题

`[bug] nuclei: -t 只接受单文件路径，目录/通配符/多值全部静默 0 命中（All Poc 永远不出结果）`

## 正文

**现象**

服务端「扫描模板 → 选择全部 POC」会把策略参数拼成 `-t *` 下发到节点。节点侧 nuclei 插件收到后：

- `-t *`：跑满约 5 分钟、无报错，但 `/apps/poc` 下的模板**一个都没执行**（用必然命中的 canary 模板验证，0 请求、0 命中）；
- `-t /apps/poc`（目录）：0 命中；
- `-t *.yaml`（通配符）：0 命中；
- `-t a.yaml,b.yaml`（逗号）：`error: "No templates available"`，32ms 返回；
- `-t a.yaml -t b.yaml`（重复参数）：同上，27ms 返回；
- `-t /apps/poc/<单文件>.yaml`：**957ms 命中并落库 2+2 条**。

**影响**

漏洞扫描常年 0 结果：`vulnerability` / `vulnerabilityDetail` 恒为 0，资产漏洞页、nuclei 结果页、导出、MCP 查询四端同时为空。

**期望**

1. `-t` 收到目录或通配符时，由节点自行展开成文件列表后再交给 nuclei（不要依赖 nuclei 内核解析）；
2. 展开后 `len(templates)==0` 时打印 error 日志并返回错误，而不是静默空转；
3. `types.VulnResult.RootDomain` 补 `bson:"rootDomain"`（当前服务端读模型需要双拼写兼容才能取到值）。

**最小复现**

```bash
cat > /apps/poc/zz-canary.yaml <<YAML
id: zz-canary-sst
info: {name: ZZ Canary, author: audit, severity: high}
http:
  - method: GET
    path: ["{{BaseURL}}/"]
    matchers: [{type: status, status: [200]}]
YAML
# 分别用 -t *、-t /apps/poc、-t /apps/poc/zz-canary.yaml 跑一次 example.com
# 观察 mongosh: db.vulnerability.countDocuments({name:/Canary/i})
```

**参考**：`docs/audit/NODE_NUCLEI_PATCH.md`（含补丁草案与完整实测矩阵）
