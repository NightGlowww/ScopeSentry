# 发版安全闸门（2026-11-20 定稿）

一天之内在部署链路上踩了 8 个坑，全部固化成脚本里的硬校验。本文档是这套闸门的
单一说明源——不要再靠记忆部署。

## 一、服务端：scripts/release.sh

| 步骤 | 闸门 | 拦住的事故 |
| --- | --- | --- |
| [0/7] | /healthz 可达 | 主机宕机时继续构建上传，留下半截产物 |
| [0/7] | 容器 scope-sentry 存在 | 目标主机是纯扫描节点（默认值指向已降级的 02 机，发版"成功"但线上没变） |
| [1/7] | lint-vue-imports.py | 组件缺 import 渲染成未解析标签（ElAlert 变 elalert，界面静默失效） |
| [1/7] | UI 审计基线 | 32 条路由的历史违规回归 |
| [3/7] | 构建产物存在 | 构建失败仍继续上传 |
| [4/7] | 产物 sha256 | 本地产物损坏 |
| [5/7] | 上传后远端 sha256 | scp 静默截断（tmpfs 塞满时只传 66MB 却返回成功） |
| [5/7] | 先 mkdir -p 部署目录 | 换机后目录不存在，scp: No such file |
| [6/7] | 记录运行中镜像 tag 到 .prev_image | 无回滚基准 |
| [6/7] | 同时更新两个 compose 文件 | fork 覆盖单机文件的 image，拉起旧镜像（实测 fork 钉着 v2.0.148） |
| [7/7] | healthz.version 等于目标版本 | compose tag 未更新时的假成功 |
| [7/7] | 两个 compose 的 image tag 一致 | fork 覆盖导致的版本漂移 |
| [7/7] | 节点在线 在线/总数 | 发布后节点全掉仍报成功 |
| 失败 | 自动回滚到 .prev_image | 新版本起不来时手工救 |

## 二、节点：scripts/deploy-node.sh（ScopeSentry-Scan）

| 步骤 | 闸门 |
| --- | --- |
| 构建 | golang:1.24 容器内交叉编译，版本从 internal/config/config.go 读 |
| 上传前 | 远端 rm -f /tmp/n19* /tmp/ss-node*（tmpfs 空间有限） |
| 上传 | 逐台 sha256 比对，不一致跳过该机 |
| 部署前 | docker cp 导出容器内旧二进制到 /tmp/ss-node.prev |
| 部署 | docker cp 新二进制 → commit 新 tag → restart |
| 验证 | 轮询 60 秒等 Register Success，并解析 version 与目标比对 |
| 失败 | 自动回滚（cp 回旧二进制 + restart + 再验证） |
| 收尾 | 从服务端视角断言全部节点在线且版本一致（scripts/node_assert.py） |
| 预演 | --dry-run |
| 手工 | --rollback |

## 三、写脚本的硬规则（都是血泪）

1. 不要在 JS 模板字符串里拼含 ${...} 的 shell——会被当成插值（本轮因此报错 4 次）。
2. 超过 3 行的解析逻辑不要内联在 shell 里，写成独立 .py 文件由 shell 调用。
   内联 python3 -c "..." 在双引号里嵌套会被 shell 先吃掉引号（本轮报错 2 次）。
3. 验证脚本必须区分"空"和"失败"——曾把 401 响应当成"节点 0 台"。
4. tmpfs 上的大文件上传必须校验大小/哈希，scp 返回 0 不代表文件完整。

## 四、手工部署的最小检查清单

1. ls -la /tmp/<binary> 字节数与本地一致
2. docker inspect <container> --format {{.Image}} 是新 tag
3. curl /healthz 的 version 是目标版本
4. 两个 compose 文件的 image: 一致
5. /api/node 全部 state=1 且 version 一致

## 五、当前生产拓扑（2026-11-20）

```
1.15.46.212    prod-tx-sh-app-01 (4C/8G)
  ├── scope-sentry        服务端 2.0.206
  ├── scopesentry-mongodb 1.2G 上限
  ├── scopesentry-redis   256M
  └── scopesentry-scan    node-sh-01   1.9.18  2.5G 上限

111.229.44.240 prod-tx-sh-app-02 (4C/4G)
  └── scopesentry-scan    node-tx-sh-02 1.9.18  2G 上限

43.142.188.143 dev-tx-sh-app-02 (4C/4G)
  └── scopesentry-scan    node-dev-02   1.9.18  1.2G 上限
```

内存预算：mongo 1.2G + redis 256M + server 1.5G + scan 2.5G ≈ 5.5G / 7.68G。
8G 机器上给 scan 5G 会把整机压到假死（ping 通但 SSH/HTTP 全挂，需重启实例）。
## 六、已知缺口：i18n key 缺失靠人工发现（2026-11-20）

现状：`common.confirm` 这类 key 不存在时，界面直接显示原始 key，构建不报错。
本轮尝试写自动检查脚本（解析 locale 的 TS 对象字面量），但 locale 里有内联嵌套对象
（`system: { version: { success: ... } }`）和多行字符串，简单缩进解析会产出 2000+ 假阳性，
**已放弃该脚本**——留着比没有更糟。

当前做法：
1. 涉及弹窗/二次确认的文案，改完必须在浏览器里真实打开一次看按钮文字
2. 已对六个主页面做全量扫描（task / node / poc / asset / project / dict），无裸 key 泄漏
3. 若要做自动检查，正确路线是把 locale 编译成 JSON（走 vite 产物）再做 key 集合比对，
   而不是正则解析源码

命名对照：`common.confirmed` 是状态标签「确认」，确认按钮统一用 `confirmButtonText: OK`。
