package models

// StatFingerListRequest 指纹统计查询参数（对标 ARL-Next assetStatFinger 列表）。
//
// pageSize 上限 200：统计行数可能上万，超过上限由 binding 直接拒成 400，
// 服务层再做一次兜底归一化（兜底只防漏，不改变对外语义）。
type StatFingerListRequest struct {
	// Keyword 技术名子串，不区分大小写（在 $unwind 之后过滤，只看技术名本身）
	Keyword string `json:"keyword" form:"keyword" binding:"max=255"`
	// Project 项目名，精确匹配
	Project   string `json:"project" form:"project" binding:"max=255"`
	PageIndex int    `json:"pageIndex" form:"pageIndex" binding:"omitempty,min=0"`
	PageSize  int    `json:"pageSize" form:"pageSize" binding:"omitempty,min=0,max=200"`
}

// StatFingerRow 一项指纹统计（一个技术名一行）。
//
// Count 是出现次数（同一资产里重复写同一个技术会累加），
// AssetCount 是涉及资产数（同一资产只算一次），
// Samples 是字典序最小的前 5 个 host 样例（由聚合里的 $topN 截断，不在内存里排序）。
type StatFingerRow struct {
	Name       string   `json:"name" bson:"name"`
	Count      int64    `json:"count" bson:"count"`
	AssetCount int64    `json:"assetCount" bson:"assetCount"`
	LastSeen   string   `json:"lastSeen" bson:"lastSeen"`
	Samples    []string `json:"samples" bson:"samples"`
}

// StatFingerListResult 指纹统计返回结构
type StatFingerListResult struct {
	List  []StatFingerRow `json:"list"`
	Total int64           `json:"total"`
}

// StatFingerAssetsRequest 按技术名反查资产。
// Technology 是精确匹配（asset.technologies 数组元素），Project 可选收窄。
type StatFingerAssetsRequest struct {
	Technology string `json:"technology" form:"technology" binding:"required,max=128"`
	Project    string `json:"project" form:"project" binding:"max=255"`
	PageIndex  int    `json:"pageIndex" form:"pageIndex" binding:"omitempty,min=0"`
	PageSize   int    `json:"pageSize" form:"pageSize" binding:"omitempty,min=0,max=200"`
}

// StatFingerAssetRow 使用某技术的资产行（只读摘要，不带 body/screenshot 等大字段）
type StatFingerAssetRow struct {
	ID         string `json:"id" bson:"id"`
	Host       string `json:"host" bson:"host"`
	Port       string `json:"port" bson:"port"`
	IP         string `json:"ip" bson:"ip"`
	URL        string `json:"url" bson:"url"`
	Title      string `json:"title" bson:"title"`
	StatusCode int    `json:"statusCode" bson:"statusCode"`
	Service    string `json:"service" bson:"service"`
	Project    string `json:"project" bson:"project"`
	RootDomain string `json:"rootDomain" bson:"rootDomain"`
	Time       string `json:"time" bson:"time"`
}

// StatFingerAssetsResult 按技术反查资产返回结构
type StatFingerAssetsResult struct {
	List  []StatFingerAssetRow `json:"list"`
	Total int64                `json:"total"`
}
