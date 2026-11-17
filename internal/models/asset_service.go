package models

// AssetServiceListRequest 服务维度查询（ARL-Next assetService）。
type AssetServiceListRequest struct {
	Keyword   string `json:"keyword" form:"keyword" binding:"max=128"`
	Project   string `json:"project" form:"project" binding:"max=255"`
	PageIndex int    `json:"pageIndex" form:"pageIndex"`
	PageSize  int    `json:"pageSize" form:"pageSize"`
}

// AssetServiceRow 一个服务（如 https/ssh/redis）的资产聚合。
type AssetServiceRow struct {
	Service    string   `json:"service" bson:"_id"`
	AssetCount int64    `json:"assetCount" bson:"assetCount"`
	HostCount  int      `json:"hostCount" bson:"hostCount"`
	PortCount  int      `json:"portCount" bson:"portCount"`
	Samples    []string `json:"samples" bson:"samples"`
	LastSeen   string   `json:"lastSeen" bson:"lastSeen"`
}

// AssetServiceListResult 列表返回。
type AssetServiceListResult struct {
	List  []AssetServiceRow `json:"list"`
	Total int64             `json:"total"`
}
