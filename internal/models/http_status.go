package models

// HTTPStatusListRequest HTTP 状态码视图查询（ARL-Next http_status）。
type HTTPStatusListRequest struct {
	// Status 支持分档 2xx/3xx/4xx/5xx 或精确码（如 404）
	Status    string `json:"status" form:"status"`
	Keyword   string `json:"keyword" form:"keyword" binding:"max=255"`
	Project   string `json:"project" form:"project" binding:"max=255"`
	PageIndex int    `json:"pageIndex" form:"pageIndex"`
	PageSize  int    `json:"pageSize" form:"pageSize"`
}

// HTTPStatusRow 一行资产 HTTP 状态。
type HTTPStatusRow struct {
	ID         string `json:"id" bson:"_id"`
	Host       string `json:"host" bson:"host"`
	Port       string `json:"port" bson:"port"`
	StatusCode int    `json:"statusCode" bson:"statuscode"`
	Title      string `json:"title" bson:"title"`
	URL        string `json:"url" bson:"url"`
	Service    string `json:"service" bson:"service"`
	Project    string `json:"project" bson:"project"`
	Time       string `json:"time" bson:"time"`
}

// HTTPStatusListResult 列表返回。
type HTTPStatusListResult struct {
	List  []HTTPStatusRow `json:"list"`
	Total int64           `json:"total"`
}
