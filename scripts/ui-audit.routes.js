/**
 * 审计覆盖的路由清单（与侧边栏 / Tab 对齐）。
 * 注意：资产页的「子域名 / URL / 爬虫」等是 #/asset-information/index 里的 Tab，
 * 不是独立路由，审计只能覆盖到 Tab 容器本身。
 */
module.exports = [
  '#/dashboard/index',
  '#/asset-information/index',
  '#/asset-information/asset-group',
  '#/asset-information/stat-finger',
  '#/asset-information/nuclei-result',
  '#/asset-information/wih',
  '#/asset-information/http-status',
  '#/asset-information/service',
  '#/asset-information/fileleak',
  '#/asset-information/npoc-service',
  '#/asset-information/cert',
  '#/asset-information/cip',
  '#/asset-information/icp-task',
  '#/asset-information/fofa',
  '#/asset-information/policy',
  '#/task-management/ScanTask',
  '#/task-management/ScheduledTask',
  '#/task-management/ScanTemplate',
  '#/plugin-management/index',
  '#/node-management/index',
  '#/project-management/index',
  '#/poc-management/index',
  '#/fingerprint-management/index',
  '#/sensitive-information-rules/index',
  '#/dictionary-management/manage',
  '#/dictionary-management/port',
  '#/configuration/system',
  '#/configuration/subfinder',
  '#/configuration/rad',
  '#/configuration/apikey',
  '#/configuration/syslog',
  '#/about/index'
]
