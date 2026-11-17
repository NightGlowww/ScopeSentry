// models-------------------------------------
// @file      : export_batch.go
// @time      : 2026/11/18 03:20
// -------------------------------------------

package models

// 批量导出支持的 type（对标 ARL-Next batchExport 的 type 面：
// site/domain/ip/url/ip_port/cip/wih/asset_ip/asset_domain/asset_site/asset_wih，
// 另按本仓库现有集合补 vulnerability / sensitive 两个）。
const (
	ExportTypeSite          = "site"
	ExportTypeDomain        = "domain"
	ExportTypeIP            = "ip"
	ExportTypeURL           = "url"
	ExportTypeIPPort        = "ip_port"
	ExportTypeCIP           = "cip"
	ExportTypeWIH           = "wih"
	ExportTypeAssetIP       = "asset_ip"
	ExportTypeAssetDomain   = "asset_domain"
	ExportTypeAssetSite     = "asset_site"
	ExportTypeAssetWIH      = "asset_wih"
	ExportTypeVulnerability = "vulnerability"
	ExportTypeSensitive     = "sensitive"
	ExportTypeFileLeak      = "fileleak"
	ExportTypeNpocService   = "npoc_service"
)

// BatchExportRequest 批量导出 / 预览请求。
//
// Project / TaskName 是精确匹配（asset.taskName 是数组，等值查询照样命中数组元素），
// StartTime / EndTime 格式 "YYYY-MM-DD HH:MM:SS"，可只给一边。
type BatchExportRequest struct {
	Type      string   `json:"type" binding:"required,max=32"`
	Project   string   `json:"project" binding:"max=255"`
	TaskName  string   `json:"taskName" binding:"max=255"`
	StartTime string   `json:"startTime" binding:"max=32"`
	EndTime   string   `json:"endTime" binding:"max=32"`
	Fields    []string `json:"fields" binding:"max=64,dive,max=64"`
}

// ExportPreviewResult 预览结果：前 N 行 + 命中总数，供前端确认字段。
// Rows 与 Fields 一一对应（列顺序即请求 fields 顺序）。
type ExportPreviewResult struct {
	Type            string     `json:"type"`
	Collection      string     `json:"collection"`
	Fields          []string   `json:"fields"`
	AvailableFields []string   `json:"availableFields"`
	Rows            [][]string `json:"rows"`
	Total           int64      `json:"total"`
	Limit           int        `json:"limit"`
}
