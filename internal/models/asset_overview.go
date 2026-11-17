package models

// AssetOverviewCount 资产族数量总览（对标 ARL-Next 资产总览）。
//
// 物理集合走 EstimatedDocumentCount（元数据 O(1)）；
// cert/cip 是读模型聚合出来的族，没有独立集合，走聚合 count。
type AssetOverviewCount struct {
	Asset         int64 `json:"asset"`
	Subdomain     int64 `json:"subdomain"`
	URL           int64 `json:"url"`
	App           int64 `json:"app"`
	MP            int64 `json:"mp"`
	Vulnerability int64 `json:"vulnerability"`
	Sensitive     int64 `json:"sensitive"`
	Cert          int64 `json:"cert"`
	CIP           int64 `json:"cip"`
}
