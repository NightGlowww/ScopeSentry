package models

// CIPListRequest ICP/主体列表查询参数（对标 ARL-Next assetCip 列表）。
//
// 数据源是 RootDomain 集合的 company/icp/domain 字段，按 company 分组现算。
type CIPListRequest struct {
	// Keyword 子串匹配 company/icp/domain，不区分大小写
	Keyword string `json:"keyword" form:"keyword" binding:"max=255"`
	// Project 项目名，精确匹配
	Project string `json:"project" form:"project" binding:"max=255"`
	// Company 主体名，精确匹配（可选）
	Company   string `json:"company" form:"company" binding:"max=255"`
	PageIndex int    `json:"pageIndex" form:"pageIndex" binding:"omitempty,min=0"`
	PageSize  int    `json:"pageSize" form:"pageSize" binding:"omitempty,min=0,max=200"`
}

// CIPRow 一个 ICP 主体（按 company 分组）一行。
type CIPRow struct {
	Company     string   `json:"company" bson:"_id"`
	ICP         string   `json:"icp" bson:"icp"`
	ICPs        []string `json:"icps" bson:"icps"`
	DomainCount int64    `json:"domainCount" bson:"domainCount"`
	Samples     []string `json:"samples" bson:"samples"`
	LastTime    string   `json:"lastTime" bson:"lastTime"`
}

// CIPListResult 主体列表返回结构
type CIPListResult struct {
	List  []CIPRow `json:"list"`
	Total int64    `json:"total"`
}

// CIPDetailRequest 主体详情请求：按一个根域名反查它所属的 company，
// 再返回该主体下的域名列表（分页）。
type CIPDetailRequest struct {
	Domain string `json:"domain" binding:"required,max=255"`
	// Project 可选：传入时只返回该项目下的域名，与列表口径保持一致；
	// 不传则返回该主体在全部项目里的域名。
	Project   string `json:"project" form:"project" binding:"max=255"`
	PageIndex int    `json:"pageIndex" form:"pageIndex" binding:"omitempty,min=0"`
	PageSize  int    `json:"pageSize" form:"pageSize" binding:"omitempty,min=0,max=200"`
}

// CIPDomain 主体下的一个根域名。
type CIPDomain struct {
	Domain  string   `json:"domain" bson:"domain"`
	ICP     string   `json:"icp" bson:"icp"`
	Project string   `json:"project" bson:"project"`
	Time    string   `json:"time" bson:"time"`
	Tags    []string `json:"tags" bson:"tags"`
}

// CIPDetailResult 主体详情返回结构
type CIPDetailResult struct {
	Domain    string      `json:"domain"`
	Company   string      `json:"company"`
	ICP       string      `json:"icp"`
	ICPs      []string    `json:"icps"`
	Total     int64       `json:"total"`
	List      []CIPDomain `json:"list"`
	PageIndex int         `json:"pageIndex"`
	PageSize  int         `json:"pageSize"`
}
