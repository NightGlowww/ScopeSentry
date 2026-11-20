#!/usr/bin/env python3
"""把「排队中」的某类任务从节点队列里撤下来并标为暂停（不删除数据）。

场景：外部集成批量派发，队列长期积压，真实任务排不上（2026-11-20 实测）。
用法：
  python3 queue-drain.py --prefix huntdeck-            # 预演，只打印
  python3 queue-drain.py --prefix huntdeck- --apply    # 真正执行
"""
import argparse
import json
import subprocess

PW = "ac6b0cf473fb8d23a0e13ff3"


def redis(*args):
    cmd = ["sg", "docker", "-c",
           "docker exec -i scopesentry-redis redis-cli -a %s --no-auth-warning %s" % (PW, " ".join(args))]
    return subprocess.run(cmd, capture_output=True, text=True).stdout.strip()


def mongo(js):
    open("/tmp/_qd.js", "w").write(js)
    subprocess.run(["sg", "docker", "-c", "docker cp /tmp/_qd.js scopesentry-mongodb:/tmp/_qd.js"], capture_output=True)
    return subprocess.run(["sg", "docker", "-c",
        "docker exec scopesentry-mongodb mongosh -u admin -p a873b84b7830de8b6bc296ee "
        "--authenticationDatabase admin ScopeSentry --quiet --file /tmp/_qd.js"],
        capture_output=True, text=True).stdout.strip()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--prefix", required=True)
    ap.add_argument("--apply", action="store_true")
    args = ap.parse_args()

    # 1) 找出待开始(status=0) 且名字匹配前缀的任务
    js = 'var ids=[]; db.task.find({status:0, name:/^%s/},{_id:1,name:1}).forEach(function(t){ids.push(t._id.toString());}); print(JSON.stringify(ids));' % args.prefix
    raw = mongo(js)
    ids = set(json.loads(raw)) if raw.strip().startswith("[") else set()
    print("pending tasks matching %r: %d" % (args.prefix, len(ids)))
    if not ids:
        return

    # 2) 从各节点队列里摘掉这些任务的消息
    removed = 0
    for key in [k for k in redis("keys", "NodeTask:*").split(chr(10)) if k]:
        items = [x for x in redis("lrange", key, "0", "-1").split(chr(10)) if x]
        for it in items:
            try:
                tid = json.loads(it)["id"]
            except Exception:
                continue
            if tid in ids:
                if args.apply:
                    redis("lrem", key, "1", json.dumps(json.loads(it), separators=(",", ":")))
                removed += 1
    print("queue entries %s: %d" % ("removed" if args.apply else "would remove", removed))

    if args.apply:
        # 3) 标为暂停(status=2)，保留数据；想恢复直接在界面点「开始」
        q = ",".join('ObjectId("%s")' % i for i in ids)
        print(mongo('var r=db.task.updateMany({_id:{$in:[%s]}},{$set:{status:2}}); print("paused:"+r.modifiedCount);' % q))
    else:
        print("(dry run; 加 --apply 才真正执行)")


if __name__ == "__main__":
    main()
