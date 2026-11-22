#!/usr/bin/env bash
# 用独立链做 fail-closed：内网/环回/服务器 IP RETURN，其余落到 Web 端口 DROP。
set -uo pipefail
PORTS="80,443,8080,8443,8000,8888"
apply() {
  local NAME="$1"
  local CID=$(docker inspect "$NAME" --format '{{.Id}}' 2>/dev/null || true)
  [ -z "$CID" ] && { echo "skip $NAME"; return; }
  local SHORT=${CID:0:8}
  local CG="system.slice/docker-${CID}.scope"
  local CH="SS_GUARD_${SHORT}"
  # 清掉历史写法
  while iptables -D OUTPUT -m cgroup --path "$CG" -p tcp -m multiport --dports $PORTS ! -d 127.0.0.0/8 -j DROP 2>/dev/null; do :; done
  while iptables -D OUTPUT -m cgroup --path "$CG" -p tcp -m multiport --dports $PORTS -j DROP 2>/dev/null; do :; done
  while iptables -D OUTPUT -m cgroup --path "$CG" -p tcp -m multiport --destination 127.0.0.0/8 -j ACCEPT 2>/dev/null; do :; done
  iptables -F "$CH" 2>/dev/null || iptables -N "$CH"
  for D in 127.0.0.0/8 10.0.0.0/8 172.16.0.0/12 192.168.0.0/16 100.64.0.0/10 1.15.46.212/32 111.229.44.240/32 43.142.188.143/32; do
    iptables -A "$CH" -d "$D" -j RETURN
  done
  iptables -A "$CH" -p tcp -m multiport --dports $PORTS -j DROP
  iptables -A "$CH" -j RETURN
  iptables -C OUTPUT -m cgroup --path "$CG" -j "$CH" 2>/dev/null || iptables -I OUTPUT 1 -m cgroup --path "$CG" -j "$CH"
  echo "chained $NAME -> $CH"
}
for n in "$@"; do apply "$n"; done
