# 运维：Tailscale 直连与安全组

## 症状

网页打开极慢：单个 API 1–16 秒，静态 favicon 甚至 4 分 57 秒；`tailscale status` 里所有对端
`curAddr` 为 `-`、`relay lax/sfo`。

## 根因

腾讯云轻量应用服务器默认只放行 **TCP 22/80/443 + ICMP**，**入站 UDP 41641 被防火墙丢弃**。
WireGuard 打洞需要双方能把 UDP 包打进来，任何一侧被丢就永远收敛不到直连，退回 DERP 中继。
而 Tailscale 在中国大陆没有 DERP 节点——最近是香港 238ms，实际被选中的是洛杉矶 176ms、旧金山 140ms。

排除了主机侧：`iptables` 的 `ts-input` 链明确 `ACCEPT udp dport 41641`，`ufw` inactive，
`YJ-FIREWALL-INPUT` 只有针对特定 IP 的 22 端口封禁。

反例佐证：东京那台（AWS）`curAddr = 13.140.77.62:41641`，一直直连——因为它放行了 UDP。

## 修复

四台轻量全部加一条入站规则（`dreamxy` 账号，`ap-shanghai`）：

```bash
tccli lighthouse CreateFirewallRules --region ap-shanghai --profile dreamxy \
  --InstanceId lhins-xxx \
  --FirewallRules '[{"Protocol":"UDP","Port":"41641","CidrBlock":"0.0.0.0/0","Action":"ACCEPT",
                    "FirewallRuleDescription":"tailscale direct (wireguard)"}]'
```

| 实例 | 名称 | 公网 IP |
| --- | --- | --- |
| `lhins-lmcxuhgq` | prod-tx-sh-app-01 | 1.15.46.212 |
| `lhins-hwoloayw` | prod-tx-sh-app-02 | 111.229.44.240 |
| `lhins-8x0vseqw` | dev-tx-sh-app-02 | 43.142.188.143 |
| `lhins-mlegt826` | dev-tx-sh-app-01 | 43.143.38.5 |

## 效果（实测）

| 指标 | 修复前 | 修复后 |
| --- | --- | --- |
| prod → dev-01 | via DERP(lax) 323–736ms | **via 10.0.0.11:41641 2ms** |
| prod → mbp-surge | relay lax | **via 61.173.146.223:30204 6ms** |
| `curl http://100.115.186.92:8082/` | 1.0–16.2 秒 | **17ms** |

## 注意

- 这条规则**必须留在大盘**：换机器/重装系统/新建实例时默认没有，忘了就是「网页卡到不可用」。
- UDP 41641 对 `0.0.0.0/0` 开放是 Tailscale 的标准做法：WireGuard 对未认证包静默丢弃，不回应。
- 想更收紧就按对端公网 IP 逐条放行（Mac/手机出口 IP 会变，`0.0.0.0/0` 更省事）。
- 排查口诀：`tailscale status` 看 `curAddr` 是否为 `-`；`tailscale ping <peer>` 看 `via DERP` 还是 `via <ip>:41641`。
