# REST 接口引用索引

> 由 scripts/ci/rest_closure.py --write 生成，勿手改。
> 路由快照来自 SS_ROUTE_DUMP=1 跑 TestSetupRouterInventory（需要真实 Mongo/Redis）。

**这是索引，不是覆盖率裁决。** 两处天然低估：路径可能是拼出来的（脚本按字面量 + 前缀匹配）；
这个项目 POST 大量用于查列表，光看 method 分不出读写，所以读写分类是名字启发式。

| 维度 | 数量 |
| --- | --- |
| 路由总数 | 314 |
| 被测试/契约脚本点名 | 157（50%） |
| 读操作 点名/总数 | 73 / 115 |
| 写操作 点名/总数 | 47 / 113 |
| 无法判读 点名/总数 | 37 / 86 |

## 按模块（未点名 / 总数）

| 模块 | 未点名 | 总数 |
| --- | --- |
| assets | 37 | 81 |
| task | 28 | 30 |
| plugin | 17 | 17 |
| configuration | 14 | 14 |
| system | 9 | 10 |
| node | 8 | 10 |
| poc | 8 | 9 |
| fingerprint | 6 | 6 |
| github | 5 | 26 |
| sensitive | 5 | 5 |
| backup | 4 | 7 |
| project | 4 | 6 |
| mcp | 3 | 10 |
| user | 2 | 8 |
| apikey | 2 | 3 |
| dictionary | 2 | 19 |
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
| POST | `/api/apikey/delete` |
| POST | `/api/assets/asset_group/edit` |
| POST | `/api/assets/asset_group/reorder` |
| POST | `/api/assets/asset_scope/bind_enterprise` |
| POST | `/api/assets/asset_scope/clear_update_error` |
| POST | `/api/assets/asset_scope/delete` |
| POST | `/api/assets/asset_scope/update` |
| POST | `/api/assets/fileleak/delete` |
| POST | `/api/assets/npoc_service/delete` |
| POST | `/api/assets/nuclei_result/status` |
| POST | `/api/assets/nuclei_result/status/batch` |
| POST | `/api/backup/create` |
| POST | `/api/backup/upload` |
| POST | `/api/configuration/deduplication/save` |
| POST | `/api/configuration/notification/add` |
| POST | `/api/configuration/notification/config/update` |
| POST | `/api/configuration/notification/delete` |
| POST | `/api/configuration/notification/update` |
| POST | `/api/configuration/rad/save` |
| POST | `/api/configuration/subfinder/save` |
| POST | `/api/configuration/system/save` |
| POST | `/api/dictionary/port/delete` |
| POST | `/api/fingerprint/add` |
| POST | `/api/fingerprint/delete` |
| POST | `/api/fingerprint/update` |
| POST | `/api/github/scheduler/stop` |
| POST | `/api/mcp/keys/:id/status` |
| POST | `/api/node/config/update` |
| POST | `/api/node/delete` |
| POST | `/api/node/queue/clear` |
| POST | `/api/plugin/delete` |
| POST | `/api/plugin/import` |
| POST | `/api/plugin/import/data` |
| POST | `/api/plugin/run` |
| POST | `/api/plugin/save` |
| POST | `/api/plugin/status` |
| POST | `/api/poc/add` |
| POST | `/api/poc/data/import` |
| POST | `/api/poc/data/sync` |
| POST | `/api/poc/delete` |
| POST | `/api/poc/update` |
| POST | `/api/project/delete` |
| POST | `/api/project/update` |
| POST | `/api/sensitive/add` |
| POST | `/api/sensitive/delete` |
| POST | `/api/sensitive/update` |
| POST | `/api/sensitive/update/state` |
| POST | `/api/system/intel-feeds/run` |
| POST | `/api/system/intel-feeds/update` |
| POST | `/api/system/storage/slim` |
| POST | `/api/task/add` |
| POST | `/api/task/assignProject` |
| POST | `/api/task/deleteByFilter` |
| POST | `/api/task/scheduled/add` |
| POST | `/api/task/scheduled/delete` |
| POST | `/api/task/scheduled/pagemonit/add` |
| POST | `/api/task/scheduled/pagemonit/delete` |
| POST | `/api/task/scheduled/pagemonit/update` |
| POST | `/api/task/scheduled/update` |
| POST | `/api/task/stop` |
| POST | `/api/task/sync` |
| POST | `/api/task/target/resume` |
| POST | `/api/task/template/delete` |
| POST | `/api/task/template/save` |
