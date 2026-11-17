package models

// IPListRequest IP 列表查询参数（对标 ARL-Next assetIP 列表）。
type IPListRequest struct {
	// Keyword 关键字，匹配 ip（不区分大小写、子串）
	Keyword string `json:"keyword" form:"keyword" binding:"max=255"`
	// IP 按 IP 前缀过滤（例如 "10.0."），比 keyword 更适合按网段筛
	IP        string `json:"ip" form:"ip" binding:"max=64"`
	PageIndex int    `json:"pageIndex" form:"pageIndex" binding:"omitempty,min=0"`
	PageSize  int    `json:"pageSize" form:"pageSize" binding:"omitempty,min=0,max=1000"`
}

// IPPortBrief IP 上的一个端口/协议组合。
// asset 集合没有独立的 protocol 字段，这里用 service 顶（http/https/...），
// 与 ARL 的「端口 + 协议」展示口径一致。
type IPPortBrief struct {
	Port     string `json:"port" bson:"port"`
	Protocol string `json:"protocol" bson:"protocol"`
}

// IPRow 按 ip 聚合出来的一行。
// Ports/Domains 只带前 20 个，完整数量看 PortCount/DomainCount。
type IPRow struct {
	IP          string        `json:"ip" bson:"ip"`
	PortCount   int           `json:"portCount" bson:"portCount"`
	Ports       []IPPortBrief `json:"ports" bson:"ports"`
	DomainCount int           `json:"domainCount" bson:"domainCount"`
	Domains     []string      `json:"domains" bson:"domains"`
	AssetCount  int64         `json:"assetCount" bson:"assetCount"`
	LastSeen    string        `json:"lastSeen" bson:"lastSeen"`
}

// IPListResult IP 列表返回结构
type IPListResult struct {
	List  []IPRow `json:"list"`
	Total int64   `json:"total"`
}

// IPDomainsRequest 按 IP 反查域名
type IPDomainsRequest struct {
	IP string `json:"ip" form:"ip" binding:"required,max=64"`
}

// IPDomainItem IP 上解析到的一个域名。
// Sources 标明来源：asset（资产表 host）、subdomain（子域表 ip 数组命中）。
type IPDomainItem struct {
	Domain   string   `json:"domain" bson:"domain"`
	Sources  []string `json:"sources" bson:"sources"`
	LastSeen string   `json:"lastSeen" bson:"lastSeen"`
}

// IPDomainsResult 按 IP 反查域名返回结构
type IPDomainsResult struct {
	List  []IPDomainItem `json:"list"`
	Total int64          `json:"total"`
}
