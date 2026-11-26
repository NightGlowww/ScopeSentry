# LLM 判定与主动复核（jev 包）

漏洞有效性判定有三层后端，输出映射到同一套 `Response`，上层阈值不变：

| 形态 | 开启方式 | 能力 |
| --- | --- | --- |
| 纯模型 | 默认 | 读已有证据打分，不碰网络 |
| 内置复核 `builtin` | 环境变量 `JEV_AGENT=builtin` 或 Mongo `config` 里 `{name:"jev.agent", value:"builtin"}` | 重放命中请求 + 打一个随机路径对照，再交给模型 |
| 外部 agent `external` | `AGENT_VERIFIER_CMD="…"` | 任意进程，stdin/stdout JSON 契约 |

优先级：`external` > `builtin` > 纯模型。任何一层失败都会回落，不会让链路停摆。

## 内置复核做什么

1. 从 state 里取 `Matched:`（没有就用 `URL:`）；
2. 直连重放一次，记状态码 / 真实长度 / body sha1 / 前 512 字节；
3. 同 host 打一个 `/.ss-ctl-<random>` 对照，判断是不是 catch-all；
4. 把结果拼在证据后面再问模型，摘要写进 `Model` 字段：
   `agent:builtin(replay=200/8077B control=200(catchall)) | jev-1.14.0`。

## 两条不能违反的规则

**一、拿不到 2xx 就是「无结论」，不是「反证」。**
服务端在境内、目标在 Cloudflare 后面时，重放和对照都会 403 —— 若当成反证，
会把真洞判成假阳性（2026-11-26 实测：`images.hotukdeals.com` 从日本节点 200、从服务端 403）。
这种情况只把 `inconclusive` 写进 Model，判定完全沿用原证据。

**二、catch-all 以「内容同一」为准，不是长度。**
SPA 兜底页往往几十 KB，只按长度比会把两个不同页面判成同一个。
判据：整段 sha1 相等，或前 200 字节相同且长度在 ±20% 内。

## 内网保护

重放前解析目标 host，命中回环 / RFC1918 / 链路本地 / 未指定地址一律跳过，
避免扫描结果里混进内网地址时把服务端变成内网探测器。

## 已知限制

复核从服务端出口发出，与扫描节点（日本）出口不同。目标对境内 IP 有 WAF 时，
复核只会得到 `inconclusive`，此时价值退化为「不做坏判断」，而不是「确认」。
要真正跨出口复核，需要在扫描节点上跑一个回放中继，把 `JEV_PROBE_PROXY` 指过去（未实现）。
