#!/usr/bin/env python3
"""服务器本机节点健康校验。

默认输出 "在线/总数"（release.sh [7/7] 用）。
加 --json 输出每个节点的 name/version/state，供 deploy-node.sh 做版本一致性断言。
凭据读 /opt/scopesentry/.api_cred（600），不进仓库。
"""
import json
import pathlib
import sys
import urllib.request

CRED = pathlib.Path("/opt/scopesentry/.api_cred")
BASE = "http://127.0.0.1:8082"


def post(path, body, token=None):
    req = urllib.request.Request(BASE + path, data=json.dumps(body).encode(), method="POST")
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    with urllib.request.urlopen(req, timeout=8) as r:
        return json.loads(r.read().decode())


def fetch_nodes():
    if not CRED.exists():
        raise RuntimeError("NO_CRED")
    user, _, pwd = CRED.read_text().strip().partition(":")
    tk = post("/api/user/login", {"username": user, "password": pwd})["data"]["access_token"]
    req = urllib.request.Request(BASE + "/api/node")
    req.add_header("Authorization", "Bearer " + tk)
    with urllib.request.urlopen(req, timeout=8) as r:
        data = json.loads(r.read().decode())
    return (data.get("data") or {}).get("list") or []


def main():
    want_json = "--json" in sys.argv
    try:
        nodes = fetch_nodes()
    except Exception as e:
        print("API_FAIL:" + str(e)[:40])
        return
    if want_json:
        out = [{"name": n.get("name"), "version": n.get("version"), "state": str(n.get("state"))} for n in nodes]
        print(json.dumps(out, ensure_ascii=False))
        return
    online = sum(1 for n in nodes if str(n.get("state")) == "1")
    print(f"{online}/{len(nodes)}")


if __name__ == "__main__":
    main()
