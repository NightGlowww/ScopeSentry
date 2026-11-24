#!/usr/bin/env python3
"""节点健康检查：读 Redis 心跳，超过阈值判不健康。

容器内是 Python 3.7（zoneinfo 要 3.9+），所以这里只用 datetime 朴素比较；
容器 TZ=Asia/Shanghai，节点写心跳也用本地时间，两边同源。
"""
import datetime
import json
import socket
import sys
import time


def command(sock, reader, *parts):
    data = [str(p).encode() for p in parts]
    payload = b"*" + str(len(data)).encode() + b"\r\n"
    for p in data:
        payload += b"$" + str(len(p)).encode() + b"\r\n" + p + b"\r\n"
    sock.sendall(payload)
    kind = reader.read(1)
    line = reader.readline().rstrip(b"\r\n")
    if kind == b"+":
        return line.decode()
    if kind == b"$":
        n = int(line)
        if n < 0:
            return None
        body = reader.read(n)
        reader.read(2)
        return body.decode()
    if kind == b"-":
        raise RuntimeError(line.decode())
    raise RuntimeError("unexpected redis reply")


def main():
    with open("/run/secrets/redis.json") as f:
        cfg = json.load(f)
    with socket.create_connection((cfg["ip"], int(cfg["port"])), timeout=8) as sock:
        sock.settimeout(8)
        reader = sock.makefile("rb")
        if command(sock, reader, "AUTH", cfg["password"]) != "OK":
            print("redis auth failed")
            return 1
        stamp = command(sock, reader, "HGET", "node:node-jp-01", "updateTime")
        if not stamp:
            print("no heartbeat yet")
            return 1
        tick = datetime.datetime.strptime(stamp, "%Y-%m-%d %H:%M:%S")
        age = (datetime.datetime.now() - tick).total_seconds()
        if age > 100 or age < -120:
            print("heartbeat age %ds too old" % int(age))
            return 1
        return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as exc:
        print("%s: %s" % (type(exc).__name__, exc))
        sys.exit(1)
