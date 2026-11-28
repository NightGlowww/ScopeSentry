# REST 接口引用索引

> 由 scripts/ci/rest_closure.py --write 生成，勿手改。
> 路由快照来自 SS_ROUTE_DUMP=1 跑 TestSetupRouterInventory（需要真实 Mongo/Redis）。

**这是索引，不是覆盖率裁决。** 两处天然低估：路径可能是拼出来的（脚本按字面量 + 前缀匹配）；
这个项目 POST 大量用于查列表，光看 method 分不出读写，所以读写分类是名字启发式。

两个维度分开看：**测试/契约点名**反映验证面（谁验过这条接口），**UI 引用**反映产品面
（界面会不会真的调到）。只看前者会把「UI 在用但没人写测试」误读成「没人用」。

| 维度 | 数量 |
| --- | --- |
| 路由总数 | 314 |
| 被测试/契约脚本点名 | 186（59%） |
| 读操作 点名/总数 | 75 / 115 |
| 写操作 点名/总数 | 73 / 113 |
| 无法判读 点名/总数 | 38 / 86 |
| UI 引用 | 302（96%） |
| 写操作：测试与 UI 都没碰 | 0 |

## 按模块（未点名 / 总数）

| 模块 | 未点名 | 总数 |
| --- | --- |
| assets | 27 | 81 |
| task | 23 | 30 |
| plugin | 16 | 17 |
| configuration | 9 | 14 |
| system | 9 | 10 |
| node | 7 | 10 |
| poc | 7 | 9 |
| fingerprint | 5 | 6 |
| github | 5 | 26 |
| backup | 4 | 7 |
| mcp | 3 | 10 |
| sensitive | 3 | 5 |
| user | 2 | 8 |
| dictionary | 2 | 19 |
| project | 2 | 6 |
| apikey | 1 | 3 |
| export | 1 | 8 |
| root | 1 | 1 |
| fofa | 1 | 5 |
| console | 0 | 1 |
| dashboard | 0 | 5 |
| icp | 0 | 14 |
| audit | 0 | 1 |
| dict_brute | 0 | 2 |
| dict_cdn | 0 | 2 |
| policy | 0 | 7 |
| project_aggregation | 0 | 7 |

## 写操作里完全没被点名的（优先补测试）

| 方法 | 路径 |
| --- | --- |
| GET | `/api/backup/status` |
| GET | `/api/poc/data/import/status` |
| POST | `/api/backup/create` |
| POST | `/api/backup/upload` |
| POST | `/api/configuration/notification/config/update` |
| POST | `/api/configuration/notification/update` |
| POST | `/api/configuration/rad/save` |
| POST | `/api/configuration/subfinder/save` |
| POST | `/api/configuration/system/save` |
| POST | `/api/dictionary/port/delete` |
| POST | `/api/fingerprint/add` |
| POST | `/api/fingerprint/update` |
| POST | `/api/github/scheduler/stop` |
| POST | `/api/mcp/keys/:id/status` |
| POST | `/api/node/config/update` |
| POST | `/api/node/queue/clear` |
| POST | `/api/plugin/import` |
| POST | `/api/plugin/import/data` |
| POST | `/api/plugin/run` |
| POST | `/api/plugin/save` |
| POST | `/api/plugin/status` |
| POST | `/api/poc/add` |
| POST | `/api/poc/data/import` |
| POST | `/api/poc/data/sync` |
| POST | `/api/poc/update` |
| POST | `/api/sensitive/add` |
| POST | `/api/sensitive/update` |
| POST | `/api/system/intel-feeds/run` |
| POST | `/api/system/intel-feeds/update` |
| POST | `/api/system/storage/slim` |
| POST | `/api/task/assignProject` |
| POST | `/api/task/deleteByFilter` |
| POST | `/api/task/scheduled/add` |
| POST | `/api/task/scheduled/pagemonit/add` |
| POST | `/api/task/scheduled/pagemonit/delete` |
| POST | `/api/task/scheduled/pagemonit/update` |
| POST | `/api/task/scheduled/update` |
| POST | `/api/task/stop` |
| POST | `/api/task/sync` |
| POST | `/api/task/target/resume` |

## 写操作里测试与 UI 都没碰的（死路由候选）

这些要么删掉，要么补上真正的调用方；留着只会让「接口覆盖」看起来比实际好。

| 方法 | 路径 |
| --- | --- |
