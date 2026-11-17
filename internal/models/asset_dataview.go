package models

// FileLeakRow 文件泄露结果（对标 ARL-Next asset_fileleak，字段沿用其 bson 名）。
type FileLeakRow struct {
	ID            string `bson:"_id" json:"id"`
	URL           string `bson:"url" json:"url"`
	Site          string `bson:"site" json:"site"`
	Title         string `bson:"title" json:"title"`
	StatusCode    int    `bson:"status_code" json:"statusCode"`
	ContentLength int64  `bson:"content_length" json:"contentLength"`
	InsertTime    string `bson:"insert_time" json:"insertTime"`
	UpdateDate    string `bson:"update_date" json:"updateDate"`
}

// FileLeakListRequest 文件泄露列表查询。
type FileLeakListRequest struct {
	Keyword    string `json:"keyword" form:"keyword" binding:"max=255"`
	StatusCode int    `json:"statusCode" form:"statusCode"`
	PageIndex  int    `json:"pageIndex" form:"pageIndex"`
	PageSize   int    `json:"pageSize" form:"pageSize"`
}

// NpocServiceRow NPoC 服务结果（对标 ARL-Next asset_npoc_service）。
type NpocServiceRow struct {
	ID         string `bson:"_id" json:"id"`
	Scheme     string `bson:"scheme" json:"scheme"`
	Host       string `bson:"host" json:"host"`
	Port       string `bson:"port" json:"port"`
	Target     string `bson:"target" json:"target"`
	InsertTime string `bson:"insert_time" json:"insertTime"`
}

// NpocServiceListRequest NPoC 服务列表查询。
type NpocServiceListRequest struct {
	Keyword   string `json:"keyword" form:"keyword" binding:"max=255"`
	Scheme    string `json:"scheme" form:"scheme" binding:"max=64"`
	Host      string `json:"host" form:"host" binding:"max=255"`
	PageIndex int    `json:"pageIndex" form:"pageIndex"`
	PageSize  int    `json:"pageSize" form:"pageSize"`
}

// DataViewDeleteRequest 两个视图共用的批量删除请求。
type DataViewDeleteRequest struct {
	IDs []string `json:"ids" binding:"required"`
}
