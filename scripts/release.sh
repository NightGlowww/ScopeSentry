#!/usr/bin/env bash
# 一键发版：审计门禁 → 构建 → 校验 → 上传 → 部署 → 健康检查
#
# 用法：
#   scripts/release.sh 2.0.73            # 正常发版
#   SKIP_AUDIT=1 scripts/release.sh 2.0.73   # 跳过审计门禁（不建议）
#   DRY_RUN=1 scripts/release.sh 2.0.73      # 只跑门禁 + 打印计划，不构建/不上传
#
# 环境变量：
#   PROD_HOST   默认 prod-tx-sh-app-01（8G 主服务器；02 已降级为纯节点）
#   DEPLOY_DIR  默认 /opt/scopesentry/deploy
#   IMAGE_PREFIX 默认 autumn27/scopesentry
set -euo pipefail

VERSION="${1:-}"
if [[ -z "$VERSION" ]]; then
  echo "用法: $0 <version> 例如 $0 2.0.73" >&2
  exit 2
fi
VERSION="${VERSION#v}"

# CDPATH 会让 cd 打印解析结果，命令替换会把两行都吞进来，必须 >/dev/null
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." >/dev/null 2>&1 && pwd -P)"
# MCP schema 标签门禁：struct tag 以 WORD= 开头会被 go-sdk 判为非法并 panic，
# 表现为整个 /mcp 端点 500（v2.0.125 真实事故）。发布前拦一道。
if grep -nE 'jsonschema:"[A-Za-z_][A-Za-z0-9_]*=' "$ROOT"/internal/mcp/tools.go >/tmp/mcp_schema_bad.txt 2>/dev/null; then
  echo "✗ MCP schema 标签非法（以 WORD= 开头）："
  cat /tmp/mcp_schema_bad.txt
  rm -f /tmp/mcp_schema_bad.txt
  exit 1
fi
cd "$ROOT"

# 2026-11-20 起生产主服务器迁到 prod-tx-sh-app-01（8G）；
# prod-tx-sh-app-02 已降级为纯扫描节点，不再部署服务端。
PROD_HOST="${PROD_HOST:-prod-tx-sh-app-01}"
DEPLOY_DIR="${DEPLOY_DIR:-/opt/scopesentry/deploy}"
IMAGE_PREFIX="${IMAGE_PREFIX:-autumn27/scopesentry}"

echo "== [0/7] 生产探活"
if [[ "${DRY_RUN:-0}" != "1" ]]; then
  # 主机不可用时直接失败：历史上 sshd 无响应时仍继续构建+上传，留下半截二进制且白等 5 分钟
  if ! ssh -o BatchMode=yes -o ConnectTimeout=8 "$PROD_HOST" \
      'curl -fsS -m 6 http://127.0.0.1:8082/healthz >/dev/null' 2>/dev/null; then
    echo "✗ 生产不可达：$PROD_HOST 的 /healthz 无响应，终止发布" >&2
    exit 1
  fi

  # 目标主机身份校验：/healthz 通了不等于它跑着服务端。
  # 2026-11-20 事故：PROD_HOST 默认值仍指向已降级为纯扫描节点的机器，
  # 发版"成功"但线上完全没变。这里确认容器存在，否则直接终止。
  if ! ssh -o BatchMode=yes -o ConnectTimeout=8 "$PROD_HOST" \
      'docker ps --format "{{.Names}}" | grep -qx scope-sentry' 2>/dev/null; then
    echo "✗ $PROD_HOST 上没有 scope-sentry 容器——它很可能只是扫描节点，不是服务端" >&2
    echo "  请确认 PROD_HOST，或检查该机容器状态" >&2
    exit 1
  fi
  echo "   生产健康（scope-sentry 容器在运行）"
fi

echo "== [1/7] 审计门禁"
if [[ "${SKIP_AUDIT:-0}" == "1" ]]; then
  echo "   SKIP_AUDIT=1，跳过"
else
  # Vue 组件导入检查：缺 import 时组件会渲染成未解析标签，界面静默失效
  # （2026-11-20：任务弹窗的告警条因此长期不显示）
  python3 scripts/lint-vue-imports.py
  python3 scripts/check-ui-audit.py docs/ui-audit-baseline.json
fi

echo "== [2/7] 版本号 → $VERSION"
if [[ "${DRY_RUN:-0}" == "1" ]]; then
  echo "   DRY_RUN 模式：后续步骤仅打印"
else
  python3 - "$VERSION" <<'PY'
import json, sys
p = "version.json"
d = json.load(open(p, encoding="utf-8"))
d["server"] = sys.argv[1]
open(p, "w", encoding="utf-8").write(json.dumps(d, ensure_ascii=False, indent=2) + "\n")
PY
fi

echo "== [3/7] 构建（前端 + 内嵌二进制）"
if [[ "${DRY_RUN:-0}" == "1" ]]; then
  echo "   bash scripts/build-linux.sh"
else
  bash scripts/build-linux.sh
fi

BIN="dist/ScopeSentry_linux_amd64_v1/ScopeSentry"
if [[ "${DRY_RUN:-0}" != "1" ]]; then
  echo "== [4/7] 校验产物"
  SIZE=$(stat -f%z "$BIN" 2>/dev/null || stat -c%s "$BIN")
  SHA=$(shasum -a 256 "$BIN" | awk '{print $1}')
  echo "   size=$SIZE sha=$SHA"
fi

echo "== [5/7] 上传并校验"
if [[ "${DRY_RUN:-0}" == "1" ]]; then
  echo "   scp $BIN $PROD_HOST:$DEPLOY_DIR/ScopeSentry-v$VERSION"
else
  # 目标目录可能不存在（换机后常见）：先建目录再上传，否则 scp 报 No such file
  ssh -o BatchMode=yes "$PROD_HOST" "mkdir -p $DEPLOY_DIR" 2>/dev/null || true
  scp -o BatchMode=yes "$BIN" "$PROD_HOST:$DEPLOY_DIR/ScopeSentry-v$VERSION"
  REMOTE_SHA=$(ssh -o BatchMode=yes "$PROD_HOST" "sha256sum $DEPLOY_DIR/ScopeSentry-v$VERSION | cut -d' ' -f1")
  if [[ "$REMOTE_SHA" != "$SHA" ]]; then
    echo "   sha256 不一致：local=$SHA remote=$REMOTE_SHA" >&2
    # 仅在校验失败时清理半截上传；此前这行被误插在比较之前且无条件执行，
    # 会把刚传好的文件删掉，docker cp 报 lstat no such file（2.0.131 前两次部署失败）
    ssh -o BatchMode=yes "$PROD_HOST" "rm -f $DEPLOY_DIR/ScopeSentry-v$VERSION" 2>/dev/null || true
    exit 1
  fi
  echo "   sha256 一致"
fi

echo "== [6/7] 部署"
if [[ "${DRY_RUN:-0}" == "1" ]]; then
  echo "   docker cp / commit / compose up --no-deps scope-sentry"
else
  # 记录当前生效的镜像 tag：部署失败时切回它（2026-11-20 增加）
  # 以「正在运行的容器」为准，而不是 compose 文件：文件可能被钉在旧 tag 上
  PREV_TAG="$(ssh -o BatchMode=yes "$PROD_HOST" "docker inspect scope-sentry --format \"{{.Config.Image}}\" 2>/dev/null | grep -oE v[0-9]+[.][0-9]+[.][0-9]+ | head -1")" || true
  if [[ -n "$PREV_TAG" ]]; then
    ssh -o BatchMode=yes "$PROD_HOST" "echo $PREV_TAG > /opt/scopesentry/ScopeSentry/.prev_image" || true
    echo "   当前线上镜像：$PREV_TAG（已记录，可用于回滚）"
  fi
  ssh -o BatchMode=yes "$PROD_HOST" "set -e
    cd /opt/scopesentry/ScopeSentry
    docker cp $DEPLOY_DIR/ScopeSentry-v$VERSION scope-sentry:/opt/ScopeSentry/ScopeSentry
    docker exec scope-sentry chmod +x /opt/ScopeSentry/ScopeSentry
    docker commit scope-sentry $IMAGE_PREFIX:v$VERSION >/dev/null
    # 两个 compose 文件都改：fork 覆盖单机文件里的 image，只改一个会导致
    # 「compose up 成功但镜像还是老的」（2026-11-20 实测：fork 里还钉着 v2.0.148）
    for CF in single-host-deployment.yml docker-compose.fork.yml; do
      [ -f \"\$CF\" ] || continue
      PREV=\$(grep -oE \"$IMAGE_PREFIX:v[0-9.]+\" \"\$CF\" | head -1)
      if [ -n \"\$PREV\" ]; then
        sed -i \"s#\$PREV#$IMAGE_PREFIX:v$VERSION#\" \"\$CF\"
      fi
    done
    docker compose -f single-host-deployment.yml -f docker-compose.fork.yml up -d --no-deps scope-sentry
    sleep 9
    curl -s http://127.0.0.1:8082/healthz
  "
fi

echo "== [6.5/7] 清理历史产物（保留最近 3 个）"
if [[ "${DRY_RUN:-0}" != "1" ]]; then
  # 每次发布留一份 ~100MB 二进制；不清理会把系统盘写满（2026-11-19 事故）
  ssh -o BatchMode=yes "$PROD_HOST" "ls -t $DEPLOY_DIR/ScopeSentry-v* 2>/dev/null | tail -n +4 | xargs -r rm -f" || true
fi

echo "== [7/7] 收尾与版本断言"
if [[ "${DRY_RUN:-0}" != "1" ]]; then
  # 版本断言：健康检查通过不等于新版本生效。
  # 2026-11-20 事故：compose 里镜像 tag 没跟着改，容器"Running"但版本还是旧的。
  RUN_VER="$(ssh -o BatchMode=yes -o ConnectTimeout=8 "$PROD_HOST" \
    'curl -fsS -m 6 http://127.0.0.1:8082/healthz' 2>/dev/null \
    | python3 -c 'import sys,json;print(json.load(sys.stdin).get("version",""))' 2>/dev/null || true)"
  if [[ "$RUN_VER" != "$VERSION" ]]; then
    echo "✗ 线上版本不符：期望 $VERSION，实际 ${RUN_VER:-未知}" >&2
    RB="$(ssh -o BatchMode=yes "$PROD_HOST" "cat /opt/scopesentry/ScopeSentry/.prev_image 2>/dev/null" || true)"
    if [[ -n "$RB" ]]; then
      echo "   尝试回滚到 $RB ..." >&2
      CUR="$(ssh -o BatchMode=yes "$PROD_HOST" "cd /opt/scopesentry/ScopeSentry && grep -ohE v[0-9]+.[0-9]+.[0-9]+ docker-compose.fork.yml | head -1" || true)"
      if [[ -n "$CUR" ]]; then
        ssh -o BatchMode=yes "$PROD_HOST" "cd /opt/scopesentry/ScopeSentry && sed -i s#$CUR#$RB# docker-compose.fork.yml && docker compose -f single-host-deployment.yml -f docker-compose.fork.yml up -d --no-deps scope-sentry" >/dev/null 2>&1 || true
        sleep 10
        if ssh -o BatchMode=yes "$PROD_HOST" "curl -fsS -m 6 http://127.0.0.1:8082/healthz" >/dev/null 2>&1; then
          echo "   已回滚到 $RB，服务健康" >&2
        else
          echo "   回滚后健康检查仍失败，需要人工介入" >&2
        fi
      fi
    else
      echo "   未记录发布前镜像（.prev_image 缺失），无法自动回滚" >&2
    fi
    exit 1
  fi
  echo "   ✓ 线上版本已生效：$RUN_VER"

  # compose 文件一致性断言：fork 文件会覆盖单机文件的 image，
  # 两边 tag 不一致时 compose 会拉起旧镜像（2026-11-20 实测 v2.0.148 覆盖 v2.0.205）
  TAGS="$(ssh -o BatchMode=yes "$PROD_HOST" "cd /opt/scopesentry/ScopeSentry && for f in single-host-deployment.yml docker-compose.fork.yml; do [ -f \"\$f\" ] && echo -n \"\$f=\" && (grep -ohE $IMAGE_PREFIX:v[0-9.]+ \"\$f\" | head -1 || echo none); done" 2>/dev/null || true)"
  UNIQ="$(echo "$TAGS" | sed "s/.*=//" | sort -u | wc -l | tr -d " ")"
  if [[ "$UNIQ" != "1" ]]; then
    echo "✗ compose 文件镜像 tag 不一致：" >&2
    echo "$TAGS" >&2
    echo "  后加载的文件会覆盖前面的，可能导致拉起旧镜像" >&2
    exit 1
  fi
  echo "   ✓ compose 文件镜像一致"
  git checkout -q -- cmd/main/static && git clean -fdq cmd/main/static || true
  echo "   健康检查通过；记得提交 version.json 与打 tag"
fi
