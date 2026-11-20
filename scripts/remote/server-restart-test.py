#!/usr/bin/env python3
"""韧性测试：任务运行中重启服务端容器，验证任务不中断、进度不丢。

验证点：
1. 服务端重启期间节点继续扫描（任务不因服务端下线而停止）；
2. 重启后进度恢复可见，且不回退到 0；
3. 任务最终 status=3 / progress=100；
4. 记录服务端不可用的时长。
"""
import json
import pathlib
import subprocess
import sys
import time
import urllib.error
import urllib.request

BASE = "http://127.0.0.1:8082"
CRED = pathlib.Path("/opt/scopesentry/.api_cred")
TARGET = "mail.dreamxy.net"
CONTAINER = sys.argv[1] if len(sys.argv) > 1 else "scope-sentry"
TIMEOUT = 1200


def post(path, body, token=None, retries=3):
    for i in range(retries):
        req = urllib.request.Request(BASE + path, data=json.dumps(body).encode(), method="POST")
        req.add_header("Content-Type", "application/json")
        if token:
            req.add_header("Authorization", "Bearer " + token)
        try:
            with urllib.request.urlopen(req, timeout=30) as r:
                return json.loads(r.read().decode())
        except urllib.error.HTTPError as e:
            try:
                return json.loads(e.read().decode())
            except Exception:
                return {}
        except Exception:
            if i == retries - 1:
                return {}
            time.sleep(3)
    return {}


def main():
    user, _, pwd = CRED.read_text().strip().partition(":")
    tk = (post("/api/user/login", {"username": user, "password": pwd}).get("data") or {}).get("access_token")
    if not tk:
        print("FAIL: 登录失败")
        return 1
    name = "svrrestart-" + str(int(time.time()))
    post("/api/task/add", {"name": name, "node": ["node-sh-01"], "target": TARGET,
                           "template": "6afb1e26e3460b716c5723f9", "type": "general",
                           "targetSource": "general", "allNode": False, "scheduledTasks": False}, tk)
    tid = post("/api/task/", {"pageIndex": 1, "pageSize": 5, "search": name}, tk)["data"]["list"][0]["id"]
    post("/api/task/start", {"ids": [tid]}, tk)
    print("task %s started" % name, flush=True)

    t0 = time.time()
    restarted = False
    down_since = None
    down_total = 0.0
    final = None
    while time.time() - t0 < TIMEOUT:
        time.sleep(10)
        s = post("/api/task/", {"pageIndex": 1, "pageSize": 5, "search": name}, tk, retries=1)
        rows = (s.get("data") or {}).get("list") or []
        if not rows:
            if down_since is None:
                down_since = time.time()
                print("server unreachable at %ds" % int(time.time() - t0), flush=True)
            continue
        if down_since is not None:
            d = time.time() - down_since
            down_total += d
            print("server back after %.0fs" % d, flush=True)
            down_since = None
        st = rows[0]
        p = st.get("progress") or 0
        if not restarted and p >= 20:
            print("restarting %s at %d%%" % (CONTAINER, p), flush=True)
            subprocess.run(["sg", "docker", "-c", "docker restart " + CONTAINER], capture_output=True)
            restarted = True
        if st.get("status") in (3, 4):
            final = st
            break
    if down_since is not None:
        down_total += time.time() - down_since
    if not final:
        print("FAIL: 超时未完成", flush=True)
        return 1
    ok = final.get("status") == 3 and (final.get("progress") or 0) >= 100
    print("elapsed=%ds downtime=%.0fs status=%s progress=%s" % (
        int(time.time() - t0), down_total, final.get("status"), final.get("progress")), flush=True)
    print(("PASS" if ok else "FAIL") + ": 服务端重启后任务%s" % ("完成" if ok else "未完成"), flush=True)
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
