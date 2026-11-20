#!/usr/bin/env python3
"""页面接口基线探测：测各页面首屏依赖接口的响应时间与字节数。

用途：改动后跑一遍，和 docs/perf-baseline.md 里的数字对比，
发现"某个页面突然变慢"（比 UI 上肉眼判断可靠）。

在服务端主机上执行（需要能访问 127.0.0.1:8082）：
    python3 perf-probe.py --user ScopeSentry --password ***
"""
import argparse
import json
import time
import urllib.request

BASE = "http://127.0.0.1:8082"

CASES = [
    ("task list", "POST", "/api/task/", {"pageIndex": 1, "pageSize": 20}),
    ("task 第3页", "POST", "/api/task/", {"pageIndex": 3, "pageSize": 20}),
    ("subdomain", "POST", "/api/assets/subdomain", {"pageIndex": 1, "pageSize": 20}),
    ("subdomain 深分页", "POST", "/api/assets/subdomain", {"pageIndex": 1200, "pageSize": 20}),
    ("url", "POST", "/api/assets/url", {"pageIndex": 1, "pageSize": 20}),
    ("asset", "POST", "/api/assets/asset", {"pageIndex": 1, "pageSize": 20}),
    ("nuclei list", "POST", "/api/assets/nuclei_result/list", {"pageIndex": 1, "pageSize": 20}),
    ("nuclei stats", "POST", "/api/assets/nuclei_result/stats", {"status": 0}),
    ("node list", "GET", "/api/node", None),
    ("healthz", "GET", "/healthz", None),
]


def call(method, path, body, token=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + path, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    t0 = time.time()
    with urllib.request.urlopen(req, timeout=120) as resp:
        payload = resp.read()
    return round((time.time() - t0) * 1000, 1), len(payload), payload


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--user", default="ScopeSentry")
    ap.add_argument("--password", required=True)
    ap.add_argument("--json", action="store_true")
    args = ap.parse_args()

    _, _, raw = call("POST", "/api/user/login", {"username": args.user, "password": args.password})
    token = json.loads(raw.decode())["data"]["access_token"]

    rows = []
    for name, method, path, body in CASES:
        try:
            ms, size, _ = call(method, path, body, token)
            rows.append({"name": name, "ms": ms, "bytes": size})
        except Exception as exc:  # noqa: BLE001
            rows.append({"name": name, "error": str(exc)[:80]})

    if args.json:
        print(json.dumps(rows, ensure_ascii=False, indent=2))
        return
    print("%-22s %10s %10s" % ("endpoint", "ms", "bytes"))
    for r in rows:
        if "error" in r:
            print("%-22s %10s %10s" % (r["name"], "ERR", r["error"][:40]))
        else:
            print("%-22s %10.1f %10d" % (r["name"], r["ms"], r["bytes"]))


if __name__ == "__main__":
    main()
