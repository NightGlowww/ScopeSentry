package models

// NucleiResultListRequest Nuclei 结果列表查询参数（对标 ARL-Next assetNucleiResult）。
//
// 字段映射（vulnerability 集合现成字段 -> ARL 口径）：
//
//	project    -> project（精确）
//	severity   -> level（精确，大小写两种写法都试）
//	templateId -> vulnid（子串，不区分大小写）
//	keyword    -> url / vulnname / rootDomain（子串，不区分大小写）
type NucleiResultListRequest struct {
	Project    string `json:"project" form:"project" binding:"max=255"`
	Severity   string `json:"severity" form:"severity" binding:"max=64"`
	TemplateID string `json:"templateId" form:"templateId" binding:"max=255"`
	Keyword    string `json:"keyword" form:"keyword" binding:"max=255"`
	PageIndex  int    `json:"pageIndex" form:"pageIndex" binding:"omitempty,min=0"`
	PageSize   int    `json:"pageSize" form:"pageSize" binding:"omitempty,min=0,max=200"`
}

// NucleiResultItem 一条 Nuclei 结果（只读视图，不含 request/response 大字段）
type NucleiResultItem struct {
	ID         string   `json:"id" bson:"id"`
	TemplateID string   `json:"templateId" bson:"templateId"`
	Name       string   `json:"name" bson:"name"`
	Severity   string   `json:"severity" bson:"severity"`
	URL        string   `json:"url" bson:"url"`
	Host       string   `json:"host" bson:"host"`
	Matched    string   `json:"matched" bson:"matched"`
	Project    string   `json:"project" bson:"project"`
	RootDomain string   `json:"rootDomain" bson:"rootDomain"`
	TaskName   string   `json:"taskName" bson:"taskName"`
	Tags       []string `json:"tags" bson:"tags"`
	Status     int      `json:"status" bson:"status"`
	MatchedAt  string   `json:"matchedAt" bson:"matchedAt"`
	Hash       string   `json:"hash" bson:"hash"`
}

// NucleiResultListResult 列表返回结构
type NucleiResultListResult struct {
	List  []NucleiResultItem `json:"list"`
	Total int64              `json:"total"`
}

// NucleiResultDetailRequest 详情查询参数
type NucleiResultDetailRequest struct {
	ID string `json:"id" binding:"required,max=64"`
}

// NucleiResultDetail 单条完整记录。
// Request/Response 优先取漏洞主文档，主文档没有时回退到 vulnerabilityDetail（按 hash 关联）。
type NucleiResultDetail struct {
	NucleiResultItem `bson:",inline"`
	Request          string `json:"request" bson:"request"`
	Response         string `json:"response" bson:"response"`
}
