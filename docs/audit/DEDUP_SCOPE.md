# 资产去重覆盖范围（哪些集合真去重、哪些不去）

> 起因（2026-11-29）：两个「能勾但服务端什么都不做」的开关 —— SensitiveResult 与 PageMonitoring。
> 配置里是 true，去重服务里既不在分组表也不在 switch 分支，静默跳过。用户勾了以为去重了。

## 实际去重的集合

| 集合 | 分组键 | 说明 |
| --- | --- | --- |
| asset（type≠other） | url, statuscode, hashes.body_mmh3 | HTTP 资产 |
| asset（type=other） | host, ip, protocol | 非 HTTP 资产 |
| subdomain | host, type, sorted_ip（先对 ip 排序） | 先后顺序不同也算同一条 |
| UrlScan | output | |
| crawler | url, body | |
| DirScanResult | url, status, msg | |
| SubdomainTakerResult | input, value | |
| vulnerability | url, vulnid, matched | |

## 不去重的两个（界面已禁用并说明）

**SensitiveResult（WIH 敏感命中）** —— 同一条 `(url, sid, match)` 的多行各自带研判状态
`status`（未处理/处理中/忽略/疑似/确认/已处理）。按「保留最新」去重会把研判结果一起丢掉，
那不是去重而是删数据。要做也只能按「同 url+sid+match 且状态相同」再合并，属于产品决策，
先挂在这里等拍板。

**PageMonitoring（页面监控）** —— 每个 URL 只有一条文档，内容与 hash 是数组（按 URL upsert），
不存在重复行。勾了没有意义。

## 行为约定

- 去重服务遇到这两类不再静默：日志里写 `dedup skipped` + 原因；
- 前端这两个开关置灰，悬停给出原因（`configuration.dedupNotApplicable*`）；
- 新增可去重集合必须同时改这里与 `fgk` 表，否则又会出现「勾了没反应」。

## 任务表单的「去重」选项已移除（2026-11-29）

建任务弹窗里原本有一个「去重」单选项（None / 子域名去重，提示写着「历史已经查询到了子域名将会跳过」）。
核实后确认它不生效：

- 选项值只存进 task.duplicates；
- 下发给扫描节点的队列载荷是 *models.ScanTemplate（见 services/task/common.CreateTaskScan），**不含** duplicates；
- 服务端也没有任何代码读这个字段来影响目标列表。

也就是说勾了不会有任何行为差别。已把该区块从表单移除，同时把 duplicates 从 MCP 的
create_scan_task / create_project / create_scheduled_task 入参里去掉 —— 对外不再提供无效参数。
数据库字段保留（历史任务里还有值），只是不再宣传、不再作为旋钮。

真要去重或减量，现在可用的旋钮是：ignore（忽略目标）、scanProfile=recon（跳过 nuclei 与目录扫描）、
以及模板里停用不需要的模块。
