#!/usr/bin/env bash
# 重建扫描容器：加 --init（tini 作为 PID 1，回收孤儿进程）+ pids-limit，其余保持不变。
# 用途：清理僵尸进程堆积（2026-11-23 实测 prod-02 曾累积 614 个 chrome 僵尸）。
set -euo pipefail
NAME=scopesentry-scan
NEWIMG="ss-scan:init-$(date +%H%M)"
MEM=$(docker inspect $NAME --format '{{.HostConfig.Memory}}')
BINDS=$(docker inspect $NAME --format '{{range .HostConfig.Binds}}{{.}} {{end}}')
ENVS=$(docker inspect $NAME --format '{{range .Config.Env}}{{println .}}{{end}}')
NODENAME=$(echo "$ENVS" | grep '^NodeName=' | cut -d= -f2)
BATCH=$(echo "$ENVS" | grep '^NCC_BATCH=' | cut -d= -f2 || true)
CONC=$(echo "$ENVS" | grep '^NCC_CONC=' | cut -d= -f2 || true)
echo "node=$NODENAME mem=$MEM binds=[$BINDS]"
docker commit $NAME $NEWIMG >/dev/null
BINDARGS=""; for b in $BINDS; do BINDARGS="$BINDARGS -v $b"; done
docker stop $NAME >/dev/null
docker rename $NAME "$NAME-old-init-$(date +%s)"
docker run -d --name $NAME --restart always --init --pids-limit 2048 --network host --memory $MEM $BINDARGS \
  -e NodeName=$NODENAME ${BATCH:+-e NCC_BATCH=$BATCH} ${CONC:+-e NCC_CONC=$CONC} \
  -e HTTP_PROXY=http://127.0.0.1:7894 -e HTTPS_PROXY=http://127.0.0.1:7894 -e ALL_PROXY=http://127.0.0.1:7894 \
  -e NO_PROXY=localhost,127.0.0.1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,1.15.46.212,111.229.44.240,43.142.188.143 \
  $NEWIMG >/dev/null
sleep 12
docker ps --format '{{.Names}} {{.Status}}' | grep -v old | grep $NAME
systemctl restart ss-tproxy.service 2>/dev/null || true
/usr/local/bin/ss-egress-guard.sh $NAME | tail -1
echo "PID1: $(docker exec $NAME cat /proc/1/comm)"
echo "zombies: $(docker exec $NAME sh -c 'ps -eo stat --no-headers' | grep -c Z || true)"
