#!/usr/bin/env python3
"""在服务器本机校验节点在线状态，输出 "在线/总数"，失败输出原因。

被 scripts/release.sh 的 [7/7] 调用：读 /opt/scopesentry/.api_cred 登录，
再查 /api/node。凭据不进仓库。
"""
import json
import pathlib
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


def main():
    if not CRED.exists():
        print("NO_CRED")
        return
    user, _, pwd = CRED.read_text().strip().partition(":")
    try:
        tk = post("/api/user/login", {"username": user, "password": pwd})["data"]["access_token"]
        req = urllib.request.Request(BASE + "/api/node")
        req.add_header("Authorization", "Bearer " + tk)
        with urllib.request.urlopen(req, timeout=8) as r:
            data = json.loads(r.read().decode())
    except Exception as e:
        print("API_FAIL:" + str(e)[:40])
        return
    lst = (data.get("data") or {}).get("list") or []
    online = sum(1 for n in lst if str(n.get("state")) == "1")
    print(f"{online}/{len(lst)}")


if __name__ == "__main__":
    main()
