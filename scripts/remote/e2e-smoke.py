#!/usr/bin/env python3
"""端到端冒烟：建任务 → 轮询完成 → 查结果 → 处置 → 统计 → 导出 → 校验行数。

在服务器本机运行（凭据读 /opt/scopesentry/.api_cred，不进仓库）。
任何一步不符合预期即 exit 1，供发版后自动或手动执行：
    scripts/e2e-smoke.sh            # 从开发机触发（scp 到服务器执行）
    SMOKE=1 scripts/release.sh 2.0.x  # 发版后自动跑
"""
import json
import pathlib
import re
import sys
import time
import urllib.error
import urllib.request
import zipfile
import io

BASE = "http://127.0.0.1:8082"
CRED = pathlib.Path("/opt/scopesentry/.api_cred")
TARGET = "mail.dreamxy.net"
NODE = "node-sh-01"
TASK_TIMEOUT = 900


def _req(path, body=None, token=None, method="POST", raw=False):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + path, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req, timeout=90) as r:
            payload = r.read()
            return json.loads(payload.decode()) if not raw else payload
    except urllib.error.HTTPError as e:
        payload = e.read()
        if raw:
            return payload
        try:
            return json.loads(payload.decode())
        except Exception:
            return {}


def fail(msg):
    print("FAIL: " + msg, flush=True)
    sys.exit(1)


def main():
    if not CRED.exists():
        fail("缺少 " + str(CRED))
    user, _, pwd = CRED.read_text().strip().partition(":")
    tk = (_req("/api/user/login", {"username": user, "password": pwd}).get("data") or {}).get("access_token")
    if not tk:
        fail("登录失败")

    name = "smoke-" + str(int(time.time()))
    print("[1/5] 创建任务 " + name, flush=True)
    _req("/api/task/add", {"name": name, "node": [NODE], "target": TARGET,
                          "template": "6afb1e26e3460b716c5723f9", "type": "general",
                          "targetSource": "general", "allNode": False, "scheduledTasks": False}, tk)
    lst = (_req("/api/task/", {"pageIndex": 1, "pageSize": 5, "search": name}, tk).get("data") or {}).get("list") or []
    if not lst:
        fail("任务创建后查不到")
    tid = lst[0]["id"]
    r = _req("/api/task/start", {"ids": [tid]}, tk)
    if r.get("code") != 200:
        fail("启动任务失败: " + str(r)[:120])

    print("[2/5] 等待任务完成", flush=True)
    t0 = time.time()
    last = -1
    drops = 0
    while time.time() - t0 < TASK_TIMEOUT:
        time.sleep(15)
        rows = (_req("/api/task/", {"pageIndex": 1, "pageSize": 5, "search": name}, tk).get("data") or {}).get("list") or [{}]
        s = rows[0]
        p = s.get("progress") or 0
        if p < last:
            drops += 1
        last = max(last, p)
        if s.get("status") in (3, 4):
            break
    if s.get("status") != 3:
        fail("任务未成功完成，status=" + str(s.get("status")))
    if drops:
        fail("进度出现回退 %d 次" % drops)
    print("      完成，用时 %ds，进度 %d%%" % (int(time.time() - t0), last), flush=True)

    print("[3/5] 校验结果与处置闭环", flush=True)
    res = (_req("/api/assets/nuclei_result/list", {"pageIndex": 1, "pageSize": 10, "taskName": name}, tk).get("data") or {})
    total = res.get("total") or 0
    if total == 0:
        fail("该任务没有任何漏洞结果，无法验证处置链路")
    before = _req("/api/assets/nuclei_result/stats", {"status": 0, "taskName": name}, tk).get("data") or {}
    vid = res["list"][0]["id"]
    _req("/api/assets/nuclei_result/status", {"id": vid, "status": 5}, tk)
    after = _req("/api/assets/nuclei_result/stats", {"status": 0, "taskName": name}, tk).get("data") or {}
    _req("/api/assets/nuclei_result/status", {"id": vid, "status": 1}, tk)
    if int(after.get("5", 0)) != int(before.get("5", 0)) + 1:
        fail("处置后「已确认」计数未增加: before=%s after=%s" % (before, after))
    print("      结果 %d 条，处置统计 %s → %s" % (total, before, after), flush=True)

    print("[4/5] 导出并校验文件", flush=True)
    _req("/api/export", {"index": "UrlScan", "type": "all", "field": ["url"],
                         "quantity": 50, "filetype": "xlsx"}, tk)
    rec = None
    for _ in range(20):
        time.sleep(3)
        d = _req("/api/export/records", None, tk, "GET").get("data")
        lst = (d or {}).get("list") if isinstance(d, dict) else d
        if lst and lst[0].get("state") == 1:
            rec = lst[0]
            break
    if not rec:
        fail("导出记录未在 60 秒内变为完成态")
    if int(rec.get("quantity") or 0) == 0:
        fail("导出记录数量为 0（projection 缺字段？）")
    raw = _req("/api/export/download?filename=" + rec["file_name"], None, tk, "GET", raw=True)
    z = zipfile.ZipFile(io.BytesIO(raw))
    xml = z.read("xl/worksheets/sheet1.xml").decode("utf-8", "ignore")
    rows = len(re.findall(r"<row ", xml))
    if rows < 2:
        fail("导出文件只有 %d 行（应含表头 + 数据）" % rows)
    print("      导出 %d 行，%d 字节" % (rows, len(raw)), flush=True)

    print("[5/5] 通过：任务→结果→处置→导出 全链路正常", flush=True)


if __name__ == "__main__":
    main()
