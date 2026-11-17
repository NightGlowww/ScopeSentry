package models

// SiteListRequest 站点列表查询参数（对标 ARL-Next assetSite 列表）。
// GET 走 query、POST 走 JSON，所以 json/form 两套 tag 都打上。
type SiteListRequest struct {
	// Keyword 关键字，匹配 host / ip / title / url（不区分大小写）
	Keyword string `json:"keyword" form:"keyword" binding:"max=255"`
	// Project 项目名，精确匹配
	Project   string `json:"project" form:"project" binding:"max=255"`
	PageIndex int    `json:"pageIndex" form:"pageIndex" binding:"omitempty,min=0"`
	PageSize  int    `json:"pageSize" form:"pageSize" binding:"omitempty,min=0,max=1000"`
}

// SiteRow 站点行：asset 集合按 host+port 聚合出来的一个「站点」。
// 除 assetCount/lastSeen 外，展示字段取该 host+port 下 time 最新的一条资产。
type SiteRow struct {
	Host       string `json:"host" bson:"host"`
	Port       string `json:"port" bson:"port"`
	IP         string `json:"ip" bson:"ip"`
	Title      string `json:"title" bson:"title"`
	StatusCode int    `json:"statusCode" bson:"statusCode"`
	Project    string `json:"project" bson:"project"`
	URL        string `json:"url" bson:"url"`
	AssetCount int64  `json:"assetCount" bson:"assetCount"`
	LastSeen   string `json:"lastSeen" bson:"lastSeen"`
}

// SiteListResult 站点列表返回结构
type SiteListResult struct {
	List  []SiteRow `json:"list"`
	Total int64     `json:"total"`
}

// SiteChainRequest 子域链路查询参数
type SiteChainRequest struct {
	Host string `json:"host" form:"host" binding:"required,max=255"`
}

// SiteChainResult 子域链路返回结构。
// Limit 是本次查询生效的上限（ARL 侧是 500），前端据此判断是否被截断。
type SiteChainResult struct {
	List  []Subdomain `json:"list"`
	Total int64       `json:"total"`
	Limit int         `json:"limit"`
}
