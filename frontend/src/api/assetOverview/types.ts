// 资产族数量总览（对标 ARL-Next 资产总览）。
// 物理集合走 EstimatedDocumentCount，cert/cip 是读模型聚合计数，因此数字口径
// 与证书/主体页面里「全部项目」的列表 total 一致。

export interface AssetOverviewCount {
  asset: number
  subdomain: number
  url: number
  app: number
  mp: number
  vulnerability: number
  sensitive: number
  cert: number
  cip: number
}

export interface AssetOverviewQuery {
  // 可选：传项目名只统计该项目；不传为全库口径
  project?: string
}
