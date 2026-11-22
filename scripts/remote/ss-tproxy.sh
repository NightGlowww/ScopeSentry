#!/usr/bin/env bash
# 扫描容器裸 TCP → clash 的 scan-out 监听(7893)，该监听固定走 MESL 组，
# 不受 GEOIP,CN,DIRECT 之类规则影响（那正是 1.15.46.212 被腾讯封 80 的原因）。
set -euo pipefail
CID=$(docker inspect scopesentry-scan --format '{{.Id}}')
CG="system.slice/docker-${CID}.scope"

# 幂等清理：两种 cgroup 写法 × 两个历史端口(7892/7893)
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
echo "tproxy applied (7893, forced MESL) for $CG"
