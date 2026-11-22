#!/usr/bin/env bash
# 扫描容器出站：fail-closed。
#   nat OUTPUT：TCP → clash scan-out(7893)（强制走代理）
#   filter OUTPUT：若未被重定向（dport 仍是外网端口）→ DROP，宁可扫不动也不直连出去
set -euo pipefail
CID=$(docker inspect scopesentry-scan --format '{{.Id}}')
CG="system.slice/docker-${CID}.scope"
PORTS="80,443,8080,8443,8000,8888"

# nat：幂等清理 + 重定向
for P in "$CG" "/$CG"; do
  for PORT in 7892 7893; do
    while iptables -t nat -D OUTPUT -m cgroup --path "$P" -p tcp -j REDIRECT --to-ports $PORT 2>/dev/null; do :; done
  done
  for SPEC in "-d 127.0.0.0/8 -j RETURN" "-d 10.0.0.0/8 -j RETURN" "-d 172.16.0.0/12 -j RETURN" \
              "-d 192.168.0.0/16 -j RETURN" "-d 1.15.46.212/32 -j RETURN" "-d 111.229.44.240/32 -j RETURN" \
              "-d 43.142.188.143/32 -j RETURN"; do
    while iptables -t nat -D OUTPUT -m cgroup --path "$P" $SPEC 2>/dev/null; do :; done
  done
done
for DST in 127.0.0.0/8 10.0.0.0/8 172.16.0.0/12 192.168.0.0/16 1.15.46.212/32 111.229.44.240/32 43.142.188.143/32; do
  iptables -t nat -A OUTPUT -m cgroup --path "$CG" -d "$DST" -j RETURN
done
iptables -t nat -A OUTPUT -m cgroup --path "$CG" -p tcp -j REDIRECT --to-ports 7893

# filter：兜底 DROP（重定向后 dport 变成 7893，这里只拦"没被重定向"的外网常见端口）
while iptables -D OUTPUT -m cgroup --path "$CG" -p tcp -m multiport --dports $PORTS -j DROP 2>/dev/null; do :; done
iptables -A OUTPUT -m cgroup --path "$CG" -p tcp -m multiport --dports $PORTS ! -d 127.0.0.0/8 -j DROP

echo "fail-closed applied for $CG"
