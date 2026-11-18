# 版本与发布

本项目是 ScopeSentry 的自维护分支（upstream: `Autumn-27/ScopeSentry`）。从 **v2.0.0** 起使用自己的版本号，
语义化版本：

- **MAJOR**：不兼容的接口/数据模型变更、大版本重构；
- **MINOR**：向后兼容的新功能（如新增资产族、新的读模型、新的页面）；
- **PATCH**：向后兼容的修复（bug、样式、性能）。

`version.json` 的 `server` 字段是服务端版本；构建脚本会把该值注入 `internal/constants.Version`，
`/api/console/info` 与启动日志会显示它。`scan` 字段保留扫描节点的上游版本。

## 发布流程

```bash
# 1. 功能 PR 合并到 main（CI 三道闸 + 冒烟）
# 2. 发布版本（自动 bump version.json → PR → 合并 → tag → GitHub Release）
./scripts/release.sh 2.0.1
# 或带发布说明
./scripts/release.sh 2.1.0 "新增 xxx 资产族与页面"
```

发布完成后：

- `git tag` 与 GitHub Releases 对应同一个 commit；
- 部署使用该 tag 构建的镜像（`fork-<sha>`），部署记录写进 release notes；
- 破坏性变更必须在 release notes 里单列。
