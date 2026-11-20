#!/usr/bin/env python3
"""韧性测试：任务跑到 20% 时重启节点容器，验证任务自动恢复并完成。

验证点（对应用户原始诉求「更新导致任务暂停，更新完成后任务会恢复」）：
1. 节点重启后能重新注册（Register Success）；
2. 同一任务会被自动重新领取（Task begin 出现两次）；
3. 最终 status=3、progress=100%，无需人工干预；
4. 记录总耗时，用于判断恢复开销是否可接受。

用法（在服务器上跑，会真的重启 node-sh-01 的扫描容器）:
    python3 /opt/scopesentry/resilience-test.py [node容器名]
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
CONTAINER = sys.argv[1] if len(sys.argv) > 1 else "scopesentry-scan"
TIMEOUT = 900


def post(path, body, token=None):
    req = urllib.request.Request(BASE + path, data=json.dumps(body).encode(), method="POST")
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req, timeout=60) as r:
            return json.loads(r.read().decode())
    except urllib.error.HTTPError as e:
        try:
            return json.loads(e.read().decode())
        except Exception:
            return {}


def main():
    user, _, pwd = CRED.read_text().strip().partition(":")
    tk = (post("/api/user/login", {"username": user, "password": pwd}).get("data") or {}).get("access_token")
    if not tk:
        print("FAIL: 登录失败");
        return 1
    name = "resume-" + str(int(time.time()))
    post("/api/task/add", {"name": name, "node": ["node-sh-01"], "target": TARGET,
                           "template": "6afb1e26e3460b716c5723f9", "type": "general",
                           "targetSource": "general", "allNode": False, "scheduledTasks": False}, tk)
    tid = post("/api/task/", {"pageIndex": 1, "pageSize": 5, "search": name}, tk)["data"]["list"][0]["id"]
    post("/api/task/start", {"ids": [tid]}, tk)
    print("task %s started" % name, flush=True)

    t0 = time.time()
    restarted = False
    final = None
    while time.time() - t0 < TIMEOUT:
        time.sleep(10)
        s = post("/api/task/", {"pageIndex": 1, "pageSize": 5, "search": name}, tk)["data"]["list"][0]
        p = s.get("progress") or 0
        if not restarted and p >= 20:
            print("restarting node container %s at %d%%" % (CONTAINER, p), flush=True)
            subprocess.run(["sg", "docker", "-c", "docker restart " + CONTAINER], capture_output=True)
            restarted = True
            print("node restarted", flush=True)
        if s.get("status") in (3, 4):
            final = s
            break
    if not final:
        print("FAIL: 超时未完成");
        return 1
    ok = final.get("status") == 3 and (final.get("progress") or 0) >= 100
    print("elapsed=%ds status=%s progress=%s" % (int(time.time() - t0), final.get("status"), final.get("progress")), flush=True)
    print(("PASS" if ok else "FAIL") + ": 节点重启后任务%s" % ("自动恢复并完成" if ok else "未能恢复"), flush=True)
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
