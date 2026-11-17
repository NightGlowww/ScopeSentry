package models

// WIHListRequest WIH 敏感信息列表查询参数（对标 ARL-Next assetWih）。
//
// 读的是现成的 SensitiveResult 集合，字段映射：
//
//	type    -> sid（敏感规则名，逗号分隔可传多个，精确匹配）
//	project -> project（精确）
//	keyword -> url / sid / rootDomain / match（子串，不区分大小写）
type WIHListRequest struct {
	Type      string `json:"type" form:"type" binding:"max=128"`
	Project   string `json:"project" form:"project" binding:"max=255"`
	Keyword   string `json:"keyword" form:"keyword" binding:"max=255"`
	PageIndex int    `json:"pageIndex" form:"pageIndex" binding:"omitempty,min=0"`
	PageSize  int    `json:"pageSize" form:"pageSize" binding:"omitempty,min=0,max=200"`
}

// WIHRow WIH 一条记录（= 一次敏感规则命中）。
// Type 对应敏感规则名（sid），Match 是命中的内容片段。
// Body 不入列表：正文按 BodyID 走现成的 /api/assets/sensitive/body 接口取，避免列表被大字段拖垮。
type WIHRow struct {
	ID         string   `json:"id" bson:"id"`
	Type       string   `json:"type" bson:"type"`
	URL        string   `json:"url" bson:"url"`
	Host       string   `json:"host" bson:"host"`
	Match      []string `json:"match" bson:"match"`
	Color      string   `json:"color" bson:"color"`
	Tags       []string `json:"tags" bson:"tags"`
	Status     int      `json:"status" bson:"status"`
	Project    string   `json:"project" bson:"project"`
	RootDomain string   `json:"rootDomain" bson:"rootDomain"`
	TaskName   string   `json:"taskName" bson:"taskName"`
	Time       string   `json:"time" bson:"time"`
	BodyID     string   `json:"bodyId" bson:"bodyId"`
}

// WIHListResult 列表返回结构
type WIHListResult struct {
	List  []WIHRow `json:"list"`
	Total int64    `json:"total"`
}

// WIHDetailRequest 详情查询参数
type WIHDetailRequest struct {
	ID string `json:"id" binding:"required,max=64"`
}

// WIHDetail 单条完整记录（含命中片段全量，正文仍按 bodyId 另取）
type WIHDetail struct {
	WIHRow `bson:",inline"`
}
