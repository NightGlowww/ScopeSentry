# 发版安全三道闸（2026-11-20）

一天之内在部署链路上踩了三个坑，都已固化成脚本里的硬校验。

## 1. 目标主机必须是「服务端」而不是「节点」

**事故**：`release.sh` 的 `PROD_HOST` 默认值仍指向 `prod-tx-sh-app-02`，
而该机在迁移后已降级为纯扫描节点（没有 `scope-sentry` 容器）。
发版流程"成功"，线上完全没变。

**现状**：
- 默认值改为 `prod-tx-sh-app-01`（8G 主服务器）
- `[0/7] 生产探活` 增加容器身份校验：

```bash
docker ps --format "{{.Names}}" | grep -qx scope-sentry
```

命中不了直接终止，并提示「它很可能只是扫描节点」。

## 2. 远端 /tmp 塞满会导致 scp 静默截断

**事故**：三台机器的 `/tmp` 都是 1.9G 的 tmpfs，历次上传的 164MB 二进制把
它塞到 81%。`scp` 只传了 66–71MB 就"成功"返回，`docker cp` 复制了半个二进制，
节点版本号纹丝不动。

**现状**：
- 上传后立刻比对 `sha256`：

```bash
LOCAL=$(shasum -a 256 dist/... | cut -d' ' -f1)
REMOTE=$(ssh $H "sha256sum /tmp/x | cut -d' ' -f1")
[ "$REMOTE" = "$LOCAL" ] || abort
```

- 部署前清理远端旧包：`rm -f /tmp/n19* /tmp/ss1*`

## 3. 前端组件缺 import 会静默失效

**事故**：`<ElAlert>` 在 `ProgressInfo.vue` 里没有 import，
Vue 把它渲染成未解析的 `<elalert>` 自定义标签——不报错、不白屏，
只是告警条永远不显示。同类问题全站共有 12 个文件。

**现状**：
- `scripts/lint-vue-imports.py`：扫描所有 `.vue`，比对 template 用到的
  `<ElXxx>` 与 `element-plus` 导入名单，缺失即失败
- 接入 `release.sh` 的 `[1/7] 审计门禁`

```
== [1/7] 审计门禁
✓ vue 组件导入检查通过（无未解析的 Element Plus 组件）
PASS: 32 条路由，0 违规
```

## 手工部署时的最小检查清单

1. `ls -la /tmp/<binary>` 字节数与本机一致（不一致 = 截断）
2. `docker inspect <container> --format '{{.Image}}'` 是新 tag
3. `curl /healthz` 的 version 是目标版本
4. 三节点 `/api/node` 的 version 字段一致
