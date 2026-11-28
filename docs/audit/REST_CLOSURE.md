# REST 接口引用索引

> 由 scripts/ci/rest_closure.py --write 生成，勿手改。
> 路由快照来自 SS_ROUTE_DUMP=1 跑 TestSetupRouterInventory（需要真实 Mongo/Redis）。

**这是索引，不是覆盖率裁决。** 两处天然低估：路径可能是拼出来的（脚本按字面量 + 前缀匹配）；
这个项目 POST 大量用于查列表，光看 method 分不出读写，所以读写分类是名字启发式。

两个维度分开看：**测试/契约点名**反映验证面（谁验过这条接口），**UI 引用**反映产品面
（界面会不会真的调到）。只看前者会把「UI 在用但没人写测试」误读成「没人用」。

| 维度 | 数量 |
| --- | --- |
| 路由总数 | 315 |
| 被测试/契约脚本点名 | 235（75%） |
| 读操作 点名/总数 | 84 / 115 |
| 写操作 点名/总数 | 109 / 114 |
| 无法判读 点名/总数 | 42 / 86 |
| UI 引用 | 303（96%） |
| 写操作：测试与 UI 都没碰 | 0 |

## 按模块（未点名 / 总数）

| 模块 | 未点名 | 总数 |
| --- | --- |
| assets | 26 | 81 |
| plugin | 14 | 17 |
| task | 11 | 31 |
| system | 7 | 10 |
| node | 5 | 10 |
| github | 4 | 26 |
| user | 2 | 8 |
| fingerprint | 2 | 6 |
| poc | 2 | 9 |
| project | 2 | 6 |
| apikey | 1 | 3 |
| dictionary | 1 | 19 |
| export | 1 | 8 |
| root | 1 | 1 |
| fofa | 1 | 5 |
| backup | 0 | 7 |
| mcp | 0 | 10 |
| configuration | 0 | 14 |
| console | 0 | 1 |
| dashboard | 0 | 5 |
| icp | 0 | 14 |
| audit | 0 | 1 |
| dict_brute | 0 | 2 |
| dict_cdn | 0 | 2 |
| policy | 0 | 7 |
| project_aggregation | 0 | 7 |
| sensitive | 0 | 5 |

## 写操作里完全没被点名的（优先补测试）

| 方法 | 路径 |
| --- | --- |
| - | 全部已点名，或已登记例外 |

## 已登记的例外（为什么还没点名）

来源 `scripts/ci/closure_excuses.json`。登记不等于免检：条件变了（有了本地桩、有了测试密钥）就该补用例。

| 方法 | 路径 | 原因 |
| --- | --- | --- |
| POST | `/api/plugin/import` | 导入插件包要先过 PLUGINKEY 校验（仓库里是本地生成、gitignore 的文件），且成功后广播 install_plugin 到节点；测试环境没有节点，验不出真实安装。可用本地桩 key + 假节点补。 |
| POST | `/api/plugin/import/data` | 同上：PLUGINKEY 门禁 + install_plugin 广播，本地断言只能覆盖到参数校验，覆盖不到安装语义。 |
| POST | `/api/plugin/save` | 同上：需要 PLUGINKEY，且 server 型插件会直接注册进全局插件管理器、web 型会广播给节点。要么用桩 key 建一个不参与调度的 type=server 记录，要么在专门插件测试环境里跑。 |
| POST | `/api/poc/data/sync` | 从上游 POC 仓库拉取（外网 + 上游节奏不受控），本地/CI 没有稳定对端；导入链路本身已由 /api/poc/data/import 的往返用例覆盖。上游可镜像后补。 |
| POST | `/api/system/intel-feeds/run` | 立即抓取外部情报源（CISA KEV / Wiz / PortSwigger / 先知 / MSRC / P0），依赖外网与第三方响应；开关与参数的守卫已由 TestNodeAndIntelFeedGuards 覆盖。需要离线 fixture（预存 feed 响应）才能验抓取落库。 |

## 写操作里测试与 UI 都没碰的（死路由候选）

这些要么删掉，要么补上真正的调用方；留着只会让「接口覆盖」看起来比实际好。

| 方法 | 路径 |
| --- | --- |
