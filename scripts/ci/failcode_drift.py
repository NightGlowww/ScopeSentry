#!/usr/bin/env python3
"""失败原因 code 四方漂移检查：后端写入方 / 解析器 / 前端映射 / 三语词条必须一致。

背景（2026-11-29）：任务失败原因由巡检写中文句子，另有机器可读的 failCode 供界面按语言渲染。
前端 failReason.ts 的 FAIL_CODES 是手写白名单，后端新增一个 code 而前端没跟上时，
英文/韩文界面就会把中文句子原样显示出来 —— 编译不报、接口不报，只有那个状态的任务出现时才看得见。
实测就是这样漏掉了 stalledNoProgress。

四方：
  1) Go 直接写入：bson map 里的 "failCode": "xxx"
  2) Go 历史回填解析器：internal/failreason 的 code: "xxx"
  3) 前端 FAIL_CODES 数组
  4) 三语词条 task.failCode.<code>

任一方缺项都算失败；多余项（前端或词条里有、后端不写）也算失败，避免死配置堆积。
"""
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[2]


def go_writers():
    codes = set()
    for p in (ROOT / "internal").rglob("*.go"):
        if "_test" in p.name:
            continue
        t = p.read_text(encoding="utf-8", errors="ignore")
        codes |= set(re.findall(r'"failCode"\s*:\s*"([A-Za-z]+)"', t))
    return codes


def go_parser():
    p = ROOT / "internal/failreason/failreason.go"
    t = p.read_text(encoding="utf-8", errors="ignore")
    return set(re.findall(r'code:\s*"([A-Za-z]+)"', t))


def frontend_codes():
    p = ROOT / "frontend/src/utils/failReason.ts"
    t = p.read_text(encoding="utf-8", errors="ignore")
    m = re.search(r"FAIL_CODES\s*=\s*\[([^\]]*)\]", t, re.S)
    if not m:
        return set()
    return set(re.findall(r"'([A-Za-z]+)'", m.group(1)))


def locale_codes(fname):
    p = ROOT / "frontend/src/locales" / fname
    t = p.read_text(encoding="utf-8", errors="ignore")
    m = re.search(r"failCode:\s*\{(.*?)\n\s{4}\}", t, re.S)
    if not m:
        return set()
    return set(re.findall(r"^\s+([A-Za-z]+):", m.group(1), re.M))


def main():
    writers, parser = go_writers(), go_parser()
    front = frontend_codes()
    locales = {f: locale_codes(f) for f in ("zh-CN.ts", "en.ts", "ko-KR.ts")}

    backend = writers | parser
    print("后端写入 %s" % sorted(writers))
    print("回填解析 %s" % sorted(parser))
    print("前端映射 %d 个" % len(front))

    problems = []
    missing_front = backend - front
    extra_front = front - backend
    if missing_front:
        problems.append("前端 FAIL_CODES 缺少：%s（这些 code 会在英/韩界面显示中文原句）" % sorted(missing_front))
    if extra_front:
        problems.append("前端 FAIL_CODES 多出（后端不写）：%s" % sorted(extra_front))
    for fname, codes in locales.items():
        miss = backend - codes
        extra = codes - backend
        if miss:
            problems.append("%s 缺少词条：%s" % (fname, sorted(miss)))
        if extra:
            problems.append("%s 多余词条：%s" % (fname, sorted(extra)))

    for p in problems:
        print("    ! " + p)
    if problems:
        print("FAILCODE_DRIFT=FAIL")
        return 1
    print("FAILCODE_DRIFT=PASS（%d 个 code 四方一致）" % len(backend))
    return 0


if __name__ == "__main__":
    sys.exit(main())
