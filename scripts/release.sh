#!/usr/bin/env bash
# 发布一个版本：bump version.json → PR → 合并 → 打 tag → GitHub Release。
# 用法：scripts/release.sh 2.0.1 ["release notes"]
set -euo pipefail

VERSION="${1:?usage: scripts/release.sh X.Y.Z [notes]}"
NOTES="${2:-}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if ! [[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "version must be semver X.Y.Z" >&2; exit 1
fi
if [ -n "$(git status --porcelain)" ]; then
  echo "working tree not clean" >&2; exit 1
fi
git fetch origin --tags
git checkout main
git pull --ff-only
if git rev-parse "v$VERSION" >/dev/null 2>&1; then
  echo "tag v$VERSION already exists" >&2; exit 1
fi

BRANCH="chore/release-v$VERSION"
git checkout -b "$BRANCH"
python3 - "$VERSION" <<\PY
import json,sys
p="version.json"
d=json.load(open(p))
d["server"]=sys.argv[1]
open(p,"w").write(json.dumps(d, ensure_ascii=False, indent=2)+"\n")
PY
git add version.json
git commit -m "chore(release): v$VERSION"
git push -u origin "$BRANCH"
gh pr create -R NightGlowww/ScopeSentry --base main --head "NightGlowww:$BRANCH" \
  --title "chore(release): v$VERSION" --body "Release v$VERSION"
gh pr merge "$BRANCH" -R NightGlowww/ScopeSentry --squash --delete-branch --admin
git checkout main && git pull --ff-only
git tag -a "v$VERSION" -m "v$VERSION"
git push origin "v$VERSION"
if [ -n "$NOTES" ]; then
  gh release create "v$VERSION" -R NightGlowww/ScopeSentry --title "v$VERSION" --notes "$NOTES"
else
  gh release create "v$VERSION" -R NightGlowww/ScopeSentry --title "v$VERSION" --generate-notes
fi
echo "released v$VERSION"
