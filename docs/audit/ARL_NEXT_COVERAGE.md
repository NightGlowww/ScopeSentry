# ARL-Next 路由族覆盖对照

> 由 scripts/ci/arl_parity.py --write 生成，勿手改。
> 上游快照来自 owl234/ARL-Next 仓库 backend/app/routes（58 个文件、302 个端点）。
>
> 判读口径：✅ 同族有接口且数量不少于上游 60%；🟡 有接口但明显少；❌ 一条都没有；❓ 未登记（需要人判断）。
> 族名→路径关键词的映射是手写的：自动语义映射会把「都叫域名」的族混在一起，把缺失记成覆盖。

| 族 | 上游端点 | 我们接口数 | 判定 | 说明 |
| --- | ---: | ---: | --- | --- |
| assetCert.py | 3 | 2 | ✅ 覆盖 |  |
| assetCip.py | 4 | 2 | 🟡 部分 | 列表/详情两条已够；导出走通用 /api/export |
| assetDomain.py | 3 | 1 | 🟡 部分 | 根域名接口是 /assets/root_domain（单条），删除走 common/delete |
| assetFileleak.py | 3 | 2 | ✅ 覆盖 |  |
| assetGroup.py | 4 | 5 | ✅ 覆盖 |  |
| assetIP.py | 5 | 3 | ✅ 覆盖 |  |
| assetNpocService.py | 3 | 2 | ✅ 覆盖 |  |
| assetNucleiResult.py | 3 | 5 | ✅ 覆盖 |  |
| assetOverview.py | 1 | 1 | ✅ 覆盖 |  |
| assetScope.py | 8 | 7 | ✅ 覆盖 |  |
| assetService.py | 3 | 1 | 🟡 部分 | 同上 |
| assetSite.py | 7 | 2 | 🟡 部分 | 删除/导出走 /api/assets/common/delete 与 /api/export，不再逐族开一套 |
| assetStatFinger.py | 3 | 2 | ✅ 覆盖 |  |
| assetUrl.py | 3 | 1 | 🟡 部分 | 同上 |
| assetVuln.py | 3 | 3 | ✅ 覆盖 |  |
| assetWih.py | 3 | 2 | ✅ 覆盖 |  |
| backup.py | 9 | 7 | ✅ 覆盖 |  |
| batchExport.py | 11 | 11 | ✅ 覆盖 |  |
| brute_dict.py | 10 | 21 | ✅ 覆盖 |  |
| cdn_dict.py | 2 | 21 | ✅ 覆盖 |  |
| cert.py | 3 | 2 | ✅ 覆盖 |  |
| cip.py | 3 | 2 | ✅ 覆盖 |  |
| console.py | 1 | 1 | ✅ 覆盖 |  |
| dashboard.py | 5 | 5 | ✅ 覆盖 |  |
| dictionary.py | 10 | 19 | ✅ 覆盖 |  |
| domain.py | 3 | 1 | 🟡 部分 | 同上 |
| export.py | 2 | 11 | ✅ 覆盖 |  |
| fileleak.py | 2 | 2 | ✅ 覆盖 |  |
| fingerprint.py | 6 | 6 | ✅ 覆盖 |  |
| github_monitor_result.py | 1 | 1 | ✅ 覆盖 |  |
| github_result.py | 6 | 6 | ✅ 覆盖 |  |
| github_scheduler.py | 6 | 5 | ✅ 覆盖 |  |
| github_task.py | 3 | 4 | ✅ 覆盖 |  |
| github_threat.py | 14 | 10 | ✅ 覆盖 |  |
| http_status.py | 0 | 1 | ✅ 覆盖 |  |
| icp.py | 20 | 14 | ✅ 覆盖 |  |
| image.py | 1 | 1 | ✅ 覆盖 |  |
| ip.py | 5 | 3 | ✅ 覆盖 |  |
| mcp.py | 16 | 7 | 🟡 部分 | 上游把这些做成 REST（asset_profile/search_assets/create_scan_task…）；我们做成 MCP 工具（get_asset_profile/search_assets/create_scan_task…），REST 只留治理面 |
| npoc_service.py | 3 | 2 | ✅ 覆盖 |  |
| nuclei.py | 19 | 0 | ❌ 缺失 |  |
| nuclei_result.py | 2 | 5 | ✅ 覆盖 |  |
| poc.py | 6 | 9 | ✅ 覆盖 |  |
| policy.py | 4 | 7 | ✅ 覆盖 |  |
| scheduler.py | 12 | 10 | ✅ 覆盖 |  |
| service.py | 3 | 1 | 🟡 部分 | 同上（读模型叫 /assets/service） |
| site.py | 6 | 2 | 🟡 部分 | 同上（读模型叫 /assets/site） |
| stat_finger.py | 2 | 2 | ✅ 覆盖 |  |
| syslog.py | 1 | 1 | ✅ 覆盖 |  |
| system_config.py | 15 | 24 | ✅ 覆盖 | 代理节点/策略、更新检查这些上游能力我们还没有，见候选缺口 |
| task.py | 12 | 46 | ✅ 覆盖 |  |
| taskFofa.py | 2 | 5 | ✅ 覆盖 |  |
| task_schedule.py | 5 | 10 | ✅ 覆盖 |  |
| url.py | 3 | 1 | 🟡 部分 |  |
| user.py | 4 | 5 | ✅ 覆盖 |  |
| vuln.py | 2 | 3 | ✅ 覆盖 |  |
| wih.py | 3 | 2 | ✅ 覆盖 |  |

## 未登记的族（上游有、我们没在 MAP 里表态）

无。

## 逐族明细

### assetCert.py —— ✅ 覆盖

上游：/, /delete/, /export/

我们：/api/assets/cert/detail, /api/assets/cert/list

候选缺口（叶名对不上，需人工判断）：/delete/, /export/

### assetCip.py —— 🟡 部分

上游：/, /delete/, /export/, /ip_domain_detail/

我们：/api/assets/cip/detail, /api/assets/cip/list

候选缺口（叶名对不上，需人工判断）：/delete/, /export/

### assetDomain.py —— 🟡 部分

上游：/, /delete/, /export/

我们：/api/assets/root_domain

候选缺口（叶名对不上，需人工判断）：/delete/, /export/

### assetFileleak.py —— ✅ 覆盖

上游：/, /delete/, /export/

我们：/api/assets/fileleak/delete, /api/assets/fileleak/list

候选缺口（叶名对不上，需人工判断）：/export/

### assetGroup.py —— ✅ 覆盖

上游：/, /delete/, /edit/, /reorder/

我们：/api/assets/asset_group/add, /api/assets/asset_group/delete, /api/assets/asset_group/edit, /api/assets/asset_group/list, /api/assets/asset_group/reorder

### assetIP.py —— ✅ 覆盖

上游：/, /delete/, /export/, /export_domain/, /export_ip/

我们：/api/assets/ip, /api/assets/ip/domains, /api/assets/ip/list

候选缺口（叶名对不上，需人工判断）：/delete/, /export/, /export_domain/

### assetNpocService.py —— ✅ 覆盖

上游：/, /delete/, /export/

我们：/api/assets/npoc_service/delete, /api/assets/npoc_service/list

候选缺口（叶名对不上，需人工判断）：/export/

### assetNucleiResult.py —— ✅ 覆盖

上游：/, /delete/, /export/

我们：/api/assets/nuclei_result/detail, /api/assets/nuclei_result/list, /api/assets/nuclei_result/stats, /api/assets/nuclei_result/status, /api/assets/nuclei_result/status/batch

候选缺口（叶名对不上，需人工判断）：/delete/, /export/

### assetOverview.py —— ✅ 覆盖

上游：/count

我们：/api/assets/asset_overview/count

### assetScope.py —— ✅ 覆盖

上游：/, /add/, /batch_move_group/, /bind_enterprise/, /clear_update_error/, /delete/, /edit/, /update/

我们：/api/assets/asset_scope/add, /api/assets/asset_scope/batch_move_group, /api/assets/asset_scope/bind_enterprise, /api/assets/asset_scope/clear_update_error, /api/assets/asset_scope/delete, /api/assets/asset_scope/list, /api/assets/asset_scope/update

候选缺口（叶名对不上，需人工判断）：/edit/

### assetService.py —— 🟡 部分

上游：/, /delete/, /export/

我们：/api/assets/service/list

候选缺口（叶名对不上，需人工判断）：/delete/, /export/

### assetSite.py —— 🟡 部分

上游：/, /add_tag/, /delete/, /delete_tag/, /export/, /save_result_set/, /subdomain_chain/

我们：/api/assets/site/chain, /api/assets/site/list

候选缺口（叶名对不上，需人工判断）：/add_tag/, /delete/, /delete_tag/, /export/, /save_result_set/

### assetStatFinger.py —— ✅ 覆盖

上游：/, /delete/, /export/

我们：/api/assets/stat_finger/assets, /api/assets/stat_finger/list

候选缺口（叶名对不上，需人工判断）：/delete/, /export/

### assetUrl.py —— 🟡 部分

上游：/, /delete/, /export/

我们：/api/assets/url

候选缺口（叶名对不上，需人工判断）：/delete/, /export/

### assetVuln.py —— ✅ 覆盖

上游：/, /delete/, /export/

我们：/api/assets/vulnerability, /api/assets/vulnerability/detail, /api/assets/vulnerability/llm-verify

候选缺口（叶名对不上，需人工判断）：/delete/, /export/

### assetWih.py —— ✅ 覆盖

上游：/, /delete/, /export/

我们：/api/assets/wih/detail, /api/assets/wih/list

候选缺口（叶名对不上，需人工判断）：/delete/, /export/

### backup.py —— ✅ 覆盖

上游：/create, /delete/<string:filename>, /download/<string:filename>, /download_ticket, /info/<string:filename>, /list, /restore, /status, /upload

我们：/api/backup/:name, /api/backup/create, /api/backup/download/:name, /api/backup/list, /api/backup/restore, /api/backup/status, /api/backup/upload

候选缺口（叶名对不上，需人工判断）：/delete/<string:filename>, /info/<string:filename>

### batchExport.py —— ✅ 覆盖

上游：/asset_domain/, /asset_ip/, /asset_site/, /asset_wih/, /cip/, /domain/, /ip/, /ip_port/, /site/, /url/, /wih/

我们：/api/export, /api/export/batch, /api/export/delete, /api/export/download, /api/export/fields, /api/export/preview, /api/export/records, /api/export/task/:taskId, /api/github/result/export, /api/icp/export, /api/mcp/logs/export

候选缺口（叶名对不上，需人工判断）：/asset_domain/, /asset_ip/, /asset_site/, /asset_wih/, /cip/, /domain/, /ip/, /ip_port/, /site/, /url/, /wih/

### brute_dict.py —— ✅ 覆盖

上游：/append, /create, /delete_entries, /delete_file, /download, /list, /preview, /search, /upload_large, /upload_status

我们：/api/dict_brute/list, /api/dict_brute/save, /api/dictionary/append, /api/dictionary/create, /api/dictionary/delete_entries, /api/dictionary/delete_file, /api/dictionary/download, /api/dictionary/list, /api/dictionary/manage/create, /api/dictionary/manage/delete, /api/dictionary/manage/download, /api/dictionary/manage/list, /api/dictionary/manage/save, /api/dictionary/port/add, /api/dictionary/port/data, /api/dictionary/port/delete, /api/dictionary/port/upgrade, /api/dictionary/preview, /api/dictionary/search, /api/dictionary/upload_large …

### cdn_dict.py —— ✅ 覆盖

上游：/list, /save

我们：/api/dict_cdn/list, /api/dict_cdn/save, /api/dictionary/append, /api/dictionary/create, /api/dictionary/delete_entries, /api/dictionary/delete_file, /api/dictionary/download, /api/dictionary/list, /api/dictionary/manage/create, /api/dictionary/manage/delete, /api/dictionary/manage/download, /api/dictionary/manage/list, /api/dictionary/manage/save, /api/dictionary/port/add, /api/dictionary/port/data, /api/dictionary/port/delete, /api/dictionary/port/upgrade, /api/dictionary/preview, /api/dictionary/search, /api/dictionary/upload_large …

### cert.py —— ✅ 覆盖

上游：/, /delete/, /export/

我们：/api/assets/cert/detail, /api/assets/cert/list

候选缺口（叶名对不上，需人工判断）：/delete/, /export/

### cip.py —— ✅ 覆盖

上游：/, /delete/, /export/

我们：/api/assets/cip/detail, /api/assets/cip/list

候选缺口（叶名对不上，需人工判断）：/delete/, /export/

### console.py —— ✅ 覆盖

上游：/info

我们：/api/console/info

### dashboard.py —— ✅ 覆盖

上游：/logs, /stats, /sysinfo, /trend, /widgets

我们：/api/dashboard/logs, /api/dashboard/stats, /api/dashboard/sysinfo, /api/dashboard/trend, /api/dashboard/widgets

### dictionary.py —— ✅ 覆盖

上游：/append, /create, /delete_entries, /delete_file, /download, /list, /preview, /search, /upload_large, /upload_status

我们：/api/dictionary/append, /api/dictionary/create, /api/dictionary/delete_entries, /api/dictionary/delete_file, /api/dictionary/download, /api/dictionary/list, /api/dictionary/manage/create, /api/dictionary/manage/delete, /api/dictionary/manage/download, /api/dictionary/manage/list, /api/dictionary/manage/save, /api/dictionary/port/add, /api/dictionary/port/data, /api/dictionary/port/delete, /api/dictionary/port/upgrade, /api/dictionary/preview, /api/dictionary/search, /api/dictionary/upload_large, /api/dictionary/upload_status

### domain.py —— 🟡 部分

上游：/, /delete/, /export/

我们：/api/assets/root_domain

候选缺口（叶名对不上，需人工判断）：/delete/, /export/

### export.py —— ✅ 覆盖

上游：/<string:task_id>, /batch/

我们：/api/export, /api/export/batch, /api/export/delete, /api/export/download, /api/export/fields, /api/export/preview, /api/export/records, /api/export/task/:taskId, /api/github/result/export, /api/icp/export, /api/mcp/logs/export

### fileleak.py —— ✅ 覆盖

上游：/, /delete/

我们：/api/assets/fileleak/delete, /api/assets/fileleak/list

### fingerprint.py —— ✅ 覆盖

上游：/, /delete/, /export/, /sync/, /sync_from_json/, /upload/

我们：/api/fingerprint/add, /api/fingerprint/batch-add, /api/fingerprint/data, /api/fingerprint/delete, /api/fingerprint/update, /api/fingerprint/version

候选缺口（叶名对不上，需人工判断）：/export/, /sync/, /sync_from_json/, /upload/

### github_monitor_result.py —— ✅ 覆盖

上游：/

我们：/api/github/monitor_result/list

### github_result.py —— ✅ 覆盖

上游：/, /export/, /ignored_repos/, /overview/, /stats/, /triage/

我们：/api/github/result/export, /api/github/result/ignored_repos, /api/github/result/list, /api/github/result/overview, /api/github/result/stats, /api/github/result/triage

### github_scheduler.py —— ✅ 覆盖

上游：/, /delete/, /recover/, /run/, /stop/, /update/

我们：/api/github/scheduler/list, /api/github/scheduler/recover, /api/github/scheduler/run, /api/github/scheduler/stop, /api/github/scheduler/update

候选缺口（叶名对不上，需人工判断）：/delete/

### github_task.py —— ✅ 覆盖

上游：/, /delete/, /stop/

我们：/api/github/task/create, /api/github/task/delete, /api/github/task/list, /api/github/task/stop

### github_threat.py —— ✅ 覆盖

上游：/cve_config, /cve_history, /cve_history/delete, /cve_run_once, /hackers_config, /hackers_history, /hackers_run_once, /hackers_target, /hackers_target/delete, /token_status, /tools_config, /tools_run_once, /tools_target, /tools_target/delete

我们：/api/github/threat/cve_config, /api/github/threat/cve_history, /api/github/threat/cve_run_once, /api/github/threat/hackers_config, /api/github/threat/hackers_run_once, /api/github/threat/hackers_target, /api/github/threat/token_status, /api/github/threat/tools_config, /api/github/threat/tools_run_once, /api/github/threat/tools_target

候选缺口（叶名对不上，需人工判断）：/cve_history/delete, /hackers_history, /hackers_target/delete, /tools_target/delete

### http_status.py —— ✅ 覆盖

上游：

我们：/api/assets/http_status/list

### icp.py —— ✅ 覆盖

上游：/asset, /backfill_details/<string:task_id>, /batch_export, /batch_stop/, /delete/, /delete/<string:task_id>, /export/<string:task_id>, /restart/, /restart/<string:task_id>, /resume/<string:task_id>, /stop/<string:task_id>, /sync/<string:task_id>, /task, /task/log/<string:task_id>, /task/overview_count, /task/overview_count/, /task/statistics, /task/statistics/, /tyc_check, /tyc_task

我们：/api/icp/asset, /api/icp/backfill_details, /api/icp/export, /api/icp/sync, /api/icp/task/create, /api/icp/task/delete, /api/icp/task/list, /api/icp/task/log, /api/icp/task/overview_count, /api/icp/task/restart, /api/icp/task/resume, /api/icp/task/statistics, /api/icp/task/stop, /api/icp/tyc_check

### image.py —— ✅ 覆盖

上游：/<string:task_id>/<string:file_name>

我们：/api/assets/asset/screenshot

### ip.py —— ✅ 覆盖

上游：/, /delete/, /export/, /export_domain/, /export_ip/

我们：/api/assets/ip, /api/assets/ip/domains, /api/assets/ip/list

候选缺口（叶名对不上，需人工判断）：/delete/, /export/, /export_domain/

### mcp.py —— 🟡 部分

上游：/asset_profile, /config, /create_scan_task, /enterprise_profile, /export_server, /keys, /keys/<string:key_id>, /keys/<string:key_id>/status, /logs, /logs/export, /overview, /search_assets, /stop_scan_task, /task_detail_export, /task_status, /tools

我们：/api/mcp/config, /api/mcp/keys, /api/mcp/keys/:id, /api/mcp/keys/:id/status, /api/mcp/logs, /api/mcp/logs/export, /api/mcp/tools

候选缺口（叶名对不上，需人工判断）：/asset_profile, /create_scan_task, /enterprise_profile, /overview, /search_assets, /stop_scan_task

### npoc_service.py —— ✅ 覆盖

上游：/, /delete/, /export/

我们：/api/assets/npoc_service/delete, /api/assets/npoc_service/list

候选缺口（叶名对不上，需人工判断）：/export/

### nuclei.py —— ❌ 缺失

上游：/blacklist/, /blacklist/<string:template_id>/, /blacklist/delete/, /community/, /community/source/, /community/sync/, /community/sync_offline/, /community/sync_status/, /stats/, /template/, /template/batch_toggle/, /template/create/, /template/delete/, /template/detail/, /template/export/, /template/import/, /template/test/, /template/toggle/, /template/update/

我们：（无对应接口）

候选缺口（叶名对不上，需人工判断）：/blacklist/, /blacklist/<string:template_id>/, /blacklist/delete/, /community/, /community/source/, /community/sync/, /community/sync_offline/, /community/sync_status/, /stats/, /template/, /template/batch_toggle/, /template/create/, /template/delete/, /template/detail/, /template/export/, /template/import/, /template/test/, /template/toggle/, /template/update/

### nuclei_result.py —— ✅ 覆盖

上游：/, /delete/

我们：/api/assets/nuclei_result/detail, /api/assets/nuclei_result/list, /api/assets/nuclei_result/stats, /api/assets/nuclei_result/status, /api/assets/nuclei_result/status/batch

候选缺口（叶名对不上，需人工判断）：/delete/

### poc.py —— ✅ 覆盖

上游：/, /create/, /delete/, /import/, /source/, /sync/

我们：/api/poc, /api/poc/add, /api/poc/data/all, /api/poc/data/import, /api/poc/data/import/status, /api/poc/data/sync, /api/poc/delete, /api/poc/detail, /api/poc/update

候选缺口（叶名对不上，需人工判断）：/create/, /source/

### policy.py —— ✅ 覆盖

上游：/, /add/, /delete/, /edit/

我们：/api/policy/apply, /api/policy/create, /api/policy/delete, /api/policy/detail, /api/policy/list, /api/policy/test, /api/policy/update

候选缺口（叶名对不上，需人工判断）：/add/, /edit/

### scheduler.py —— ✅ 覆盖

上游：/, /add/, /add/site_monitor/, /add/wih_monitor/, /delete/, /one_time_scan/, /recover/, /recover/batch, /run/, /run/batch, /stop/, /stop/batch

我们：/api/task/scheduled, /api/task/scheduled/add, /api/task/scheduled/delete, /api/task/scheduled/detail, /api/task/scheduled/pagemonit/add, /api/task/scheduled/pagemonit/data, /api/task/scheduled/pagemonit/delete, /api/task/scheduled/pagemonit/update, /api/task/scheduled/run, /api/task/scheduled/update

候选缺口（叶名对不上，需人工判断）：/add/site_monitor/, /add/wih_monitor/, /one_time_scan/, /recover/, /recover/batch, /run/batch, /stop/, /stop/batch

### service.py —— 🟡 部分

上游：/, /delete/, /export/

我们：/api/assets/service/list

候选缺口（叶名对不上，需人工判断）：/delete/, /export/

### site.py —— 🟡 部分

上游：/, /add_tag/, /delete/, /delete_tag/, /export/, /save_result_set/

我们：/api/assets/site/chain, /api/assets/site/list

候选缺口（叶名对不上，需人工判断）：/add_tag/, /delete/, /delete_tag/, /export/, /save_result_set/

### stat_finger.py —— ✅ 覆盖

上游：/, /delete/

我们：/api/assets/stat_finger/assets, /api/assets/stat_finger/list

候选缺口（叶名对不上，需人工判断）：/delete/

### syslog.py —— ✅ 覆盖

上游：/

我们：/api/dashboard/logs

### system_config.py —— ✅ 覆盖

上游：/capacity_probe, /check_update, /general, /local_version, /performance, /proxy, /proxy/cooldown/reset, /proxy/nodes, /proxy/nodes/<string:node_id>, /proxy/policies, /proxy/probe, /request_update_token, /running_task_count, /security_policy, /test_push

我们：/api/configuration/deduplication/config, /api/configuration/deduplication/save, /api/configuration/notification/add, /api/configuration/notification/config/data, /api/configuration/notification/config/update, /api/configuration/notification/data, /api/configuration/notification/delete, /api/configuration/notification/update, /api/configuration/rad/data, /api/configuration/rad/save, /api/configuration/subfinder/data, /api/configuration/subfinder/save, /api/configuration/system/data, /api/configuration/system/save, /api/system/config-check, /api/system/intel-feeds, /api/system/intel-feeds/run, /api/system/intel-feeds/update, /api/system/jev-run, /api/system/quality-report …

候选缺口（叶名对不上，需人工判断）：/capacity_probe, /general, /performance, /proxy, /proxy/cooldown/reset, /proxy/nodes, /proxy/nodes/<string:node_id>, /proxy/policies, /proxy/probe, /security_policy, /test_push

### task.py —— ✅ 覆盖

上游：/, /batch_stop/, /delete/, /overview_count, /overview_count/, /policy/, /restart/, /statistics, /statistics/, /stop/<string:task_id>, /sync/, /sync_scope/

我们：/api/export/task/:taskId, /api/fofa/task/list, /api/github/task/create, /api/github/task/delete, /api/github/task/list, /api/github/task/stop, /api/icp/task/create, /api/icp/task/delete, /api/icp/task/list, /api/icp/task/log, /api/icp/task/overview_count, /api/icp/task/restart, /api/icp/task/resume, /api/icp/task/statistics, /api/icp/task/stop, /api/task/, /api/task/add, /api/task/assignProject, /api/task/delete, /api/task/deleteByFilter …

候选缺口（叶名对不上，需人工判断）：/policy/

### taskFofa.py —— ✅ 覆盖

上游：/submit, /test

我们：/api/fofa/config, /api/fofa/search, /api/fofa/status, /api/fofa/task, /api/fofa/task/list

候选缺口（叶名对不上，需人工判断）：/submit, /test

### task_schedule.py —— ✅ 覆盖

上游：/, /delete/, /recover/, /run/, /stop/

我们：/api/task/scheduled, /api/task/scheduled/add, /api/task/scheduled/delete, /api/task/scheduled/detail, /api/task/scheduled/pagemonit/add, /api/task/scheduled/pagemonit/data, /api/task/scheduled/pagemonit/delete, /api/task/scheduled/pagemonit/update, /api/task/scheduled/run, /api/task/scheduled/update

候选缺口（叶名对不上，需人工判断）：/recover/, /stop/

### url.py —— 🟡 部分

上游：/, /delete/, /export/

我们：/api/assets/url

候选缺口（叶名对不上，需人工判断）：/delete/, /export/

### user.py —— ✅ 覆盖

上游：/change_pass, /login, /logout, /refresh_token

我们：/api/user, /api/user/:id, /api/user/changePassword, /api/user/login, /api/user/register

候选缺口（叶名对不上，需人工判断）：/change_pass, /logout, /refresh_token

### vuln.py —— ✅ 覆盖

上游：/, /delete/

我们：/api/assets/vulnerability, /api/assets/vulnerability/detail, /api/assets/vulnerability/llm-verify

候选缺口（叶名对不上，需人工判断）：/delete/

### wih.py —— ✅ 覆盖

上游：/, /delete/, /export/

我们：/api/assets/wih/detail, /api/assets/wih/list

候选缺口（叶名对不上，需人工判断）：/delete/, /export/

