#!/usr/bin/env bash
# 主机采样器：load / 内存 / 每容器占用 / 扫描容器僵尸数，30 秒一条，保留 7 天。
set -euo pipefail
DIR=/var/log/ss-host
mkdir -p "$DIR"
OUT="$DIR/sample-$(date +%Y%m%d).csv"
[ -f "$OUT" ] || echo "ts,load1,mem_used_mb,mem_avail_mb,container,cpu_pct,mem_used,mem_pct,zombies" > "$OUT"
TS=$(date '+%F %T')
LOAD=$(awk '{print $1}' /proc/loadavg)
MEM=$(free -m | awk '/^Mem:/{print $3","$7}')
ZOM=$(docker exec scopesentry-scan sh -c 'ps -eo stat --no-headers' 2>/dev/null | grep -c Z || true)
docker stats --no-stream --format '{{.Name}}|{{.CPUPerc}}|{{.MemUsage}}|{{.MemPerc}}' 2>/dev/null \
  | while IFS='|' read -r n c m p; do
      [ -z "$n" ] && continue
      if [ "$n" = "scopesentry-scan" ]; then z="$ZOM"; else z=""; fi
      echo "$TS,$LOAD,$MEM,\"$n\",$c,\"$m\",$p,$z" >> "$OUT"
    done
find "$DIR" -name 'sample-*.csv' -mtime +7 -delete
