package models

// AssetAddRequest 手工录入资产（issue #203）。
//
// 只暴露人工能确定的字段：host/port 是唯一索引，重复会被拒；
// 其余扫描字段（title/banner/bodyhash 等）留空，等任务复扫或人工补。
type AssetAddRequest struct {
	Host    string   `json:"host" binding:"required"`
	IP      string   `json:"ip"`
	Port    string   `json:"port"`
	URL     string   `json:"url"`
	Service string   `json:"service"`
	Title   string   `json:"title"`
	Project string   `json:"project"`
	Tags    []string `json:"tags"`
}
