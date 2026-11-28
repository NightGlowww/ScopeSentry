# 前端类型检查基线（2026-11-28）

`pnpm ts:check`（vue-tsc）在本仓库**从未绿过**，而 `build:pro` 走 vite、不做类型检查，
所以类型错误只能靠人工发现。这轮第一次把它跑起来，记为基线，便于后续逐轮收敛。

## 当前状态

| 指标 | 数值 |
| --- | --- |
| 总错误 | 143 → **136**（本轮修了 7 类真问题） |
| 其中 `TS6133`（未使用声明） | 73 |
| 其余为类型不匹配/缺属性等 | 63 |

## 本轮修掉的（都是会影响运行时的）

| 位置 | 问题 | 后果 |
| --- | --- | --- |
| `views/Asset/components/assetInfo.vue` | `sessionStorage.setItem(...).search` | `setItem` 返回 undefined，一点搜索就 TypeError |
| `views/Setting/Subfinder.vue` | 用分页参数调 `getNodeDataApi()`（该接口不收参、不返回 total） | 表格 total 恒为 undefined |
| `views/Asset/export/exportData.vue` | 模板用了未导入的 `ElTooltip` | 组件解析失败，提示框不渲染 |
| `views/Asset/components/{IP,RootDomain}.vue` | `typeof getTotal === 'function'` 但 `getTotal` 根本不存在 | 类型检查报错（运行时被 typeof 兜住，属于死代码） |
| `locales/{zh-CN,en}.ts` | `queuedHint` 定义了两次 | 后定义覆盖前定义，用户看到的是较短的那条 |
| `api/task/index.ts` + 后端 `DeleteByFilter` | 前端传 node、后端不认 | **批量删除跨节点多删**（详见下） |

## 批量删除那条要单独记

界面按「名字 + 状态 + 节点」筛选后点「删除筛选结果」，实际删除只按「名字 + 状态」：
节点条件在 api 函数签名那里就被截断了，后端也不认。结果是**删掉的范围比界面上看到的大**。

修完的验证（不动数据）：

```bash
# 无条件 → 400（防全量删除，守卫已把 node 算进去）
curl -s -X POST $BASE/api/task/deleteByFilter -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' -d '{"pageIndex":1,"pageSize":10}'
# {"code":400,"data":"need search, status or node to avoid full delete"}

# 只有 node + 不存在的名字 → deleted 0，任务总数仍是 235
```

## 后续怎么收敛

1. 先按错误码分组清理：`TS6133`（未使用）→ 删除声明；`TS2322/2345`（类型不匹配）→ 补类型；
2. 每修一类跑一次 `pnpm ts:check | grep -c 'error TS'` 看总数下降；
3. 总数降到 0 后，把 `ts:check` 接进 `build:test` 已有链路并在 CI 里跑，之后新增错误就是硬红灯。

> `build:test` 脚本本来就写成 `pnpm run ts:check && vite build`，也就是说这条链路现在是断的；
> 在归零之前别指望它。