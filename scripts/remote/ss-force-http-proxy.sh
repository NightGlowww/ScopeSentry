#!/usr/bin/env bash
# 把扫描容器的 HTTP(S)_PROXY 指向 7894（强制 SCAN-OUT 的 mixed 监听），
# 这样 nuclei/katana/httpx 这类"读环境变量"的客户端也不会再按 rules 走 DIRECT。
set -euo pipefail
NAME=scopesentry-scan
NEWIMG="ss-scan:forced-http-$(date +%H%M)"
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
docker rename $NAME "$NAME-old-http-$(date +%s)"
docker run -d --name $NAME --restart always --network host --memory $MEM $BINDARGS \
  -e NodeName=$NODENAME ${BATCH:+-e NCC_BATCH=$BATCH} ${CONC:+-e NCC_CONC=$CONC} \
  -e HTTP_PROXY=http://127.0.0.1:7894 -e HTTPS_PROXY=http://127.0.0.1:7894 -e ALL_PROXY=http://127.0.0.1:7894 \
  -e NO_PROXY=localhost,127.0.0.1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,1.15.46.212,111.229.44.240,43.142.188.143 \
  $NEWIMG >/dev/null

sleep 10
docker ps --format '{{.Names}} {{.Status}}' | grep -v old | grep $NAME
/usr/local/bin/ss-tproxy.sh
/usr/local/bin/ss-egress-guard.sh $NAME 2>/dev/null || bash /tmp/gc.sh $NAME
echo "done"
