# MCP 工具闭环矩阵

> 由 scripts/ci/mcp_closure.py --write 生成，勿手改。
> 读 = mcp_sweep.py 用真实 id 调通；拒绝分支 = 喂不存在/非法参数必须报错；
> 写往返 = 合法参数下真的改到库、复核、再还原。

| 工具 | scope | 验证 | 说明 |
| --- | --- | --- | --- |
| `add_asset_tag` | `asset:write` | 拒绝分支 |  |
| `append_dictionary_lines` | `asset:write` | 拒绝分支 |  |
| `apply_scan_policy` | `task:read` | 读 / 拒绝分支 | 策略试算（不写库、不建任务）；需要至少一个真实策略样本，线上无策略时该分支跳过 |
| `asset_distribution` | `asset:read` | 读 / 拒绝分支 |  |
| `assign_tasks_project` | `task:write` | 拒绝分支 / 写往返 | 绑定任务到项目 → REST 复核 bindProject → 传空 projectId 解除 → 回到原值 |
| `count_assets` | `asset:read` | 读 / 拒绝分支 |  |
| `create_asset_group` | `asset:write` | 拒绝分支 |  |
| `create_asset_scope` | `asset:write` | 拒绝分支 |  |
| `create_dictionary` | `asset:write` | 拒绝分支 |  |
| `create_export_task` | `asset:write` | 拒绝分支 | 会落文件并占磁盘；成功路径由 verify_contract.sh 的 SS_EXPORT_ROUNDTRIP 单独开关跑 |
| `create_icp_task` | `icp:write` | 拒绝分支 | 会对外部 ICP 数据源发起查询并起后台 goroutine；只验非法入参 |
| `create_project` | `project:write` | 拒绝分支 |  |
| `create_scan_policy` | `task:write` | 拒绝分支 / 写往返 | 建 ci-rt 临时策略 → REST policy/list 可见 → 删除 → 列表归零 |
| `create_scan_task` | `task:write` | 拒绝分支 | 会真的建任务并触发扫描；sweep 只用非法/缺参验 schema，不制造扫描 |
| `create_scan_template` | `template:write` | 拒绝分支 |  |
| `delete_asset_groups` | `asset:write` | 拒绝分支 |  |
| `delete_dictionary` | `asset:write` | 拒绝分支 |  |
| `delete_dictionary_entries` | `asset:write` | 拒绝分支 |  |
| `delete_export_files` | `asset:write` | 拒绝分支 |  |
| `delete_nodes` | `node:write` | 拒绝分支 | 节点是扫描执行体，误删影响扫面；只验不存在的名字 |
| `delete_port_dict_entries` | `asset:write` | 拒绝分支 |  |
| `delete_scan_policies` | `task:write` | 拒绝分支 |  |
| `delete_task` | `task:write` | 拒绝分支 |  |
| `export_task_workbook` | `asset:write` | 拒绝分支 | 会生成 Excel 文件；只验非法入参 |
| `get_asset_detail` | `asset:read` | 读 / 拒绝分支 |  |
| `get_asset_profile` | `asset:read` | 读 |  |
| `get_console_info` | `asset:read` | 读 |  |
| `get_dashboard_stats` | `asset:read` | 读 |  |
| `get_enterprise_profile` | `asset:read` | 读 |  |
| `get_export_fields` | `asset:read` | 读 |  |
| `get_node_logs` | `node:read` | 读 |  |
| `get_poc` | `asset:read` | 读 / 拒绝分支 |  |
| `get_project` | `project:read` | 读 / 拒绝分支 |  |
| `get_scan_policy` | `task:read` | 读 / 拒绝分支 |  |
| `get_scan_template` | `template:read` | 读 / 拒绝分支 |  |
| `get_scheduled_task` | `task:read` | 读 / 拒绝分支 |  |
| `get_storage_report` | `system:read` | 读 |  |
| `get_system_info` | `node:read` | 读 |  |
| `get_task` | `task:read` | 读 / 拒绝分支 |  |
| `get_task_progress` | `task:read` | 读 |  |
| `intel_for_assets` | `asset:read` | 读 |  |
| `list_asset_groups` | `asset:read` | 读 |  |
| `list_asset_scopes` | `asset:read` | 读 |  |
| `list_assets` | `asset:read` | 读 / 拒绝分支 |  |
| `list_audit_logs` | `asset:read` | 读 |  |
| `list_backups` | `system:read` | 读 |  |
| `list_dictionaries` | `asset:read` | 读 |  |
| `list_export_records` | `asset:read` | 读 |  |
| `list_fingerprints` | `asset:read` | 读 |  |
| `list_github_results` | `asset:read` | 读 |  |
| `list_icp_tasks` | `asset:read` | 读 |  |
| `list_node_plugins` | `node:read` | 读 |  |
| `list_nodes` | `node:read` | 读 |  |
| `list_nuclei_results` | `asset:read` | 读 |  |
| `list_plugin_modules` | `plugin:read` | 读 |  |
| `list_plugins` | `plugin:read` | 读 |  |
| `list_pocs` | `asset:read` | 读 |  |
| `list_port_dict` | `asset:read` | 读 |  |
| `list_projects` | `project:read` | 读 |  |
| `list_projects_data` | `project:read` | 读 |  |
| `list_scan_policies` | `task:read` | 读 |  |
| `list_scan_templates` | `template:read` | 读 |  |
| `list_scheduled_tasks` | `task:read` | 读 |  |
| `list_sensitive_rules` | `asset:read` | 读 |  |
| `list_stat_finger` | `asset:read` | 读 |  |
| `list_tasks` | `task:read` | 读 / 拒绝分支 |  |
| `list_wih_results` | `asset:read` | 读 |  |
| `pause_task_targets` | `task:write` | 拒绝分支 | 需要「运行中且目标尚未全部领取」的任务；线上没有这种样本时只验拒绝分支 |
| `preview_dictionary` | `asset:read` | 读 |  |
| `recheck_plugin` | `plugin:write` | 拒绝分支 |  |
| `reinstall_plugin` | `plugin:write` | 拒绝分支 |  |
| `restart_node` | `node:write` | 拒绝分支 |  |
| `resume_task_targets` | `task:write` | 拒绝分支 | 与 pause 成对，同一前置条件 |
| `search_assets` | `asset:read` | 读 |  |
| `search_dictionary_lines` | `asset:read` | 拒绝分支 |  |
| `set_port_dict_entry` | `asset:write` | 拒绝分支 / 写往返 | 新增 ci-rt 条目 → 条数 3→4 且值回读 12345 → 按 id 删除 → 回到 3 条 |
| `set_scheduled_task_state` | `task:write` | 拒绝分支 / 写往返 | 计划任务 enabled False→True→False，cycleType 与其余字段不变 |
| `set_sensitive_rule_state` | `asset:write` | 拒绝分支 / 写往返 | 规则 state True→False→True，列表回读每一步都对得上 |
| `start_task` | `task:write` | 拒绝分支 |  |
| `stop_icp_tasks` | `icp:write` | 拒绝分支 |  |
| `stop_task` | `task:write` | 拒绝分支 |  |
| `uninstall_plugin` | `plugin:write` | 拒绝分支 |  |

## 汇总

| 维度 | 数量 |
| --- | --- |
| 工具总数 | 82 |
| 读路径验证 | 48 |
| 拒绝分支验证 | 46 |
| 写往返验证 | 5 |
| 仅前条件说明 | 0 |
| 未归类 | 0 |
