#!/usr/bin/env bash
# 构建 linux/amd64 服务端二进制（内嵌前端）。
#
# 为什么要单独一个脚本：cmd/main/static 里除了 Vite 产物，还有一套独立的 /map 微前端
# （map.html + js/ + css/ + lib/ + png/ + draco/）。直接 rsync --delete 会把它们删掉，
# 线上访问 /map 就会 500「Failed to read map.html」。
set -euo pipefail

# CDPATH 会让 cd 打印解析结果，命令替换会把两行都吞进来，必须 >/dev/null
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." >/dev/null 2>&1 && pwd -P)"
cd "$ROOT"

echo "[build] frontend"
(cd frontend && pnpm build:pro)

echo "[build] backup /map assets"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
for p in map.html js css lib png draco; do
  if [ -e "cmd/main/static/$p" ]; then cp -a "cmd/main/static/$p" "$tmp/"; fi
done

echo "[build] replace static with new frontend"
rsync -a --delete frontend/dist-pro/ cmd/main/static/

echo "[build] restore /map assets"
if [ -n "$(ls -A "$tmp")" ]; then cp -a "$tmp/." cmd/main/static/; fi

echo "[build] go build linux/amd64"
VERSION="$(python3 -c 'import json;print(json.load(open("version.json"))["server"])')"
echo "[build] version: $VERSION"
mkdir -p dist/ScopeSentry_linux_amd64_v1
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags "-s -w -X github.com/Autumn-27/ScopeSentry/internal/constants.Version=${VERSION}" \
  -o dist/ScopeSentry_linux_amd64_v1/ScopeSentry ./cmd/main

ls -lh dist/ScopeSentry_linux_amd64_v1/ScopeSentry
