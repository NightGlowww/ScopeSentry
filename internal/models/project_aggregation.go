package models

// ProjectAggregationRequest 是项目详情页 4 个 Tab 的统一查询请求。
//
// 历史：Python 版 api/project_aggregation.py 有 7 个接口，Go 重写时整体丢失，
// 导致前端 /api/project_aggregation/* 全部 404（docs/audit/ISLANDS.md 第 4.1 节）。
// 这里按当前 Vue3 前端实际发送的字段建模：{search, filter, fq} + 分页 + 可选时间范围。
type ProjectAggregationRequest struct {
	// ID 项目 ObjectID（十六进制）。与 filter.project 等价，优先使用本字段。
	ID string `json:"id"`
	// PageIndex 页码，从 1 开始；缺省 1
	PageIndex int `json:"pageIndex"`
	// PageSize 每页条数；缺省 20，上限 200（超过返回 400，见 docs/ARL_NEXT_PARITY.md 性能约束 #4）
	PageSize int `json:"pageSize"`
	// Search 搜索表达式字符串（复用 helper.GetSearchQuery 的 DSL）
	Search string `json:"search"`
	// Filter 精确过滤，key 走 helper.filterKeyCache（project / type / level / tags ...）
	Filter map[string][]interface{} `json:"filter"`
	// Fq 表头模糊搜索，key 由本包按 Tab 映射（sub_host / port_port / service_service ...）
	Fq map[string]string `json:"fq"`
	// StartTime 时间范围下界，"YYYY-MM-DD" 或 "YYYY-MM-DD HH:MM:SS"
	StartTime string `json:"startTime"`
	// EndTime 时间范围上界，同上
	EndTime string `json:"endTime"`
}

// ProjectAggregationInfo 项目基础信息（/project/info 响应）
type ProjectAggregationInfo struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Tag            string   `json:"tag"`
	ScheduledTasks bool     `json:"scheduledTasks"`
	Hour           int      `json:"hour"`
	AssetCount     int      `json:"AssetCount"`
	RootDomains    []string `json:"root_domains"`
	// NextTime 定时任务的下次执行时间；没有关联定时任务时为空串
	NextTime string `json:"next_time"`
}

// ProjectAggregationAssetCount 项目资产数量汇总（/project/asset/count 响应）
type ProjectAggregationAssetCount struct {
	AssetCount     int64 `json:"assetCount"`
	SubdomainCount int64 `json:"subdomainCount"`
	VulCount       int64 `json:"vulCount"`
	URLCount       int64 `json:"urlCount"`
	SensitiveCount int64 `json:"sensitiveCount"`
	DirScanCount   int64 `json:"dirScanCount"`
}

// ProjectAggregationLevelCount 漏洞等级分布（/project/vul/statistics 单行）
// 前端直接用 _id / count 两个字段（Dashboard.vue levelMap 映射）。
type ProjectAggregationLevelCount struct {
	Level string `bson:"_id" json:"_id"`
	Count int64  `bson:"count" json:"count"`
}

// ProjectAggregationVulRow 漏洞列表行（/project/vul/data 单行）
type ProjectAggregationVulRow struct {
	ID      string `bson:"id" json:"id"`
	URL     string `bson:"url" json:"url"`
	VulName string `bson:"vulname" json:"vulname"`
	Level   string `bson:"level" json:"level"`
	Time    string `bson:"time" json:"time"`
	Matched string `bson:"matched" json:"matched"`
}

// ProjectAggregationSubdomainRow 子域名行（/project/subdomain/data 单行）
// value / ip 必须是数组：前端 Subdomain.vue 的 formatter 直接对它 forEach。
type ProjectAggregationSubdomainRow struct {
	ID         string   `bson:"id" json:"id"`
	Host       string   `bson:"host" json:"host"`
	Type       string   `bson:"type" json:"type"`
	Value      []string `bson:"value" json:"value"`
	IP         []string `bson:"ip" json:"ip"`
	RootDomain string   `bson:"rootDomain" json:"rootDomain"`
	Time       string   `bson:"time" json:"time"`
}

// ProjectAggregationPortRow 端口聚合行（/project/port/data 单行）
// count = 该端口下资产数；host/ip/service 取该端口最近一条资产作样例。
type ProjectAggregationPortRow struct {
	ID      string `bson:"id" json:"id"`
	Port    string `bson:"port" json:"port"`
	Host    string `bson:"host" json:"host"`
	IP      string `bson:"ip" json:"ip"`
	Service string `bson:"service" json:"service"`
	Count   int64  `bson:"count" json:"count"`
	Time    string `bson:"time" json:"time"`
}

// ProjectAggregationServiceRow 服务聚合行（/project/service/data 单行）
type ProjectAggregationServiceRow struct {
	ID      string `bson:"id" json:"id"`
	Service string `bson:"service" json:"service"`
	Host    string `bson:"host" json:"host"`
	IP      string `bson:"ip" json:"ip"`
	Port    string `bson:"port" json:"port"`
	Count   int64  `bson:"count" json:"count"`
	Time    string `bson:"time" json:"time"`
}

// ProjectAggregationPage 分页响应：total + 当前页数据
type ProjectAggregationPage struct {
	List      interface{} `json:"list"`
	Total     int64       `json:"total"`
	PageIndex int         `json:"pageIndex"`
	PageSize  int         `json:"pageSize"`
}
