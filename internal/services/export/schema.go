package export

import (
	"sort"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// exportKind 导出口径。
type exportKind int

const (
	// kindFind 逐条读集合（cursor + projection）。
	kindFind exportKind = iota
	// kindAggregate 聚合出读模型行（$group/$unwind），管道必须 SetAllowDiskUse(true)。
	kindAggregate
)

// exportSpec 描述一种 type 的导出口径。
//
// 字段命名约定（不造数据：集合里没有的值一律留空）：
//   - find 口径：字段名 = 集合里的 bson 名，投影取不到就是空串；
//   - aggregate 口径：字段名 = 读模型输出名，与本仓库 /api/assets/site|ip 的列表口径一致。
type exportSpec struct {
	// Type 请求里的 type 值。
	Type string
	// Collection 读的集合。
	Collection string
	// Kind 导出口径。
	Kind exportKind
	// Fields 可导出字段全集；顺序即默认 CSV 列顺序，也是 fields 子集的白名单。
	Fields []string
	// Sort find 口径的排序：统一 time 倒序，配合 {project|taskName, time:-1} 索引，
	// explain 里必须是 IXSCAN 且没有阻塞式 SORT（大集合上内存排序直接超 32MB）。
	Sort bson.D
	// ExtraMatch find 口径的固定附加条件（例如 asset_site 只导 type=http 的站点资产）。
	ExtraMatch bson.M
	// Pipeline 聚合口径的管道；counting=true 时只算行数（末尾接 $count），
	// 供导出前的行数上限判定使用——不能等写了一半响应才发现超限。
	Pipeline func(match bson.M, fields []string, counting bool) mongo.Pipeline
}

// 各 type 的字段清单。
var (
	siteFields = []string{
		"host", "port", "ip", "title", "statusCode", "project", "url", "assetCount", "lastSeen",
	}
	domainFields = []string{
		"host", "type", "value", "ip", "time", "project", "taskName", "rootDomain", "tags",
	}
	ipFields = []string{
		"ip", "portCount", "domainCount", "assetCount", "lastSeen", "ports", "domains",
	}
	urlFields = []string{
		"input", "source", "outputtype", "output", "status", "length", "time",
		"project", "taskName", "rootDomain", "tags",
	}
	ipPortFields = []string{
		"ip", "port", "domain", "service", "webServer", "technologies",
		"project", "taskName", "rootDomain", "time",
	}
	cipFields = []string{"domain", "company", "icp", "project", "tags", "time"}
	// wih / asset_wih / sensitive 同源：SensitiveResult 就是敏感规则命中结果表，
	// WIH 只是它的读模型口径（sid -> 类型名）。字段直接用集合里的 bson 名。
	wihFields = []string{
		"url", "sid", "match", "color", "status", "project", "rootDomain", "taskName", "time", "md5",
	}
	assetIPFields     = []string{"ip", "ports", "project", "taskName", "rootDomain", "time"}
	assetDomainFields = []string{
		"host", "ips", "ports", "projects", "rootDomains", "assetCount", "lastSeen",
	}
	assetSiteFields = []string{
		"host", "port", "ip", "url", "title", "statuscode", "service", "webServer",
		"technologies", "project", "taskName", "rootDomain", "time", "lastScanTime",
	}
	vulnerabilityFields = []string{
		"url", "vulnid", "vulnname", "matched", "project", "level", "time",
		"taskName", "rootDomain", "tags", "status", "hash",
	}
)

// timeSortDesc 所有 find 口径共用：time 倒序（time 是 "2006-01-02 15:04:05"，
// 字典序即时间序），索引 {project|taskName, time:-1} 可直接提供顺序。
var timeSortDesc = bson.D{{Key: "time", Value: -1}}

// exportSpecs type -> 口径。键必须覆盖 models.ExportType* 全集。
var exportSpecs = map[string]exportSpec{
	models.ExportTypeSite: {
		Type:       models.ExportTypeSite,
		Collection: "asset",
		Kind:       kindAggregate,
		Fields:     siteFields,
		Pipeline:   sitePipeline,
	},
	models.ExportTypeDomain: {
		Type:       models.ExportTypeDomain,
		Collection: "subdomain",
		Kind:       kindFind,
		Fields:     domainFields,
		Sort:       timeSortDesc,
	},
	models.ExportTypeIP: {
		Type:       models.ExportTypeIP,
		Collection: "asset",
		Kind:       kindAggregate,
		Fields:     ipFields,
		Pipeline:   ipPipeline,
	},
	models.ExportTypeURL: {
		Type:       models.ExportTypeURL,
		Collection: "UrlScan",
		Kind:       kindFind,
		Fields:     urlFields,
		Sort:       timeSortDesc,
	},
	models.ExportTypeIPPort: {
		Type:       models.ExportTypeIPPort,
		Collection: "IPAsset",
		Kind:       kindAggregate,
		Fields:     ipPortFields,
		Pipeline:   ipPortPipeline,
	},
	// cip（企业 ICP）本仓库没有独立集合：company/icp 落在 RootDomain 上，
	// 所以按 RootDomain 导，缺的字段（例如主体统一社会信用代码）留空不补。
	models.ExportTypeCIP: {
		Type:       models.ExportTypeCIP,
		Collection: "RootDomain",
		Kind:       kindFind,
		Fields:     cipFields,
		Sort:       timeSortDesc,
	},
	models.ExportTypeWIH: {
		Type:       models.ExportTypeWIH,
		Collection: "SensitiveResult",
		Kind:       kindFind,
		Fields:     wihFields,
		Sort:       timeSortDesc,
	},
	// asset_ip：IPAsset 集合的原始文档（节点回填好的 ip + ports 聚合），
	// 与 ip（从 asset 集合现算的读模型）是两种口径，见 schema 注释。
	models.ExportTypeAssetIP: {
		Type:       models.ExportTypeAssetIP,
		Collection: "IPAsset",
		Kind:       kindFind,
		Fields:     assetIPFields,
		Sort:       timeSortDesc,
	},
	models.ExportTypeAssetDomain: {
		Type:       models.ExportTypeAssetDomain,
		Collection: "asset",
		Kind:       kindAggregate,
		Fields:     assetDomainFields,
		Pipeline:   assetDomainPipeline,
	},
	models.ExportTypeAssetSite: {
		Type:       models.ExportTypeAssetSite,
		Collection: "asset",
		Kind:       kindFind,
		Fields:     assetSiteFields,
		Sort:       timeSortDesc,
		ExtraMatch: bson.M{"type": "http"},
	},
	models.ExportTypeAssetWIH: {
		Type:       models.ExportTypeAssetWIH,
		Collection: "SensitiveResult",
		Kind:       kindFind,
		Fields:     wihFields,
		Sort:       timeSortDesc,
	},
	models.ExportTypeVulnerability: {
		Type:       models.ExportTypeVulnerability,
		Collection: "vulnerability",
		Kind:       kindFind,
		Fields:     vulnerabilityFields,
		Sort:       timeSortDesc,
	},
	models.ExportTypeSensitive: {
		Type:       models.ExportTypeSensitive,
		Collection: "SensitiveResult",
		Kind:       kindFind,
		Fields:     wihFields,
		Sort:       timeSortDesc,
	},
	// 文件泄露 / NPoC 服务：集合由节点侧写入，服务端只读导出（字段沿用 ARL-Next bson 名）
	models.ExportTypeFileLeak: {
		Type:       models.ExportTypeFileLeak,
		Collection: "asset_fileleak",
		Kind:       kindFind,
		Fields:     []string{"url", "site", "title", "status_code", "content_length", "insert_time", "update_date"},
		Sort:       bson.D{{Key: "insert_time", Value: -1}},
	},
	models.ExportTypeNpocService: {
		Type:       models.ExportTypeNpocService,
		Collection: "asset_npoc_service",
		Kind:       kindFind,
		Fields:     []string{"scheme", "host", "port", "target", "insert_time"},
		Sort:       bson.D{{Key: "insert_time", Value: -1}},
	},
}

// ExportTypes 返回全部支持的 type（排序后，用于错误提示）。
func ExportTypes() []string {
	types := make([]string, 0, len(exportSpecs))
	for name := range exportSpecs {
		types = append(types, name)
	}
	sort.Strings(types)
	return types
}

// lookupSpec 取口径，未知名返回 ok=false。
func lookupSpec(name string) (exportSpec, bool) {
	spec, ok := exportSpecs[name]
	return spec, ok
}

// countStage 聚合管道的收尾：只留一个行数。
var countStage = bson.D{{Key: "$count", Value: "n"}}

// ifNull $ifNull 简写（聚合里缺失字段兜底成空串，不造数）。
func ifNull(expr interface{}, fallback interface{}) bson.D {
	return bson.D{{Key: "$ifNull", Value: bson.A{expr, fallback}}}
}

// projectStage 字段子集投影：{_id:0, field:1...}。
func projectStage(fields []string) bson.D {
	stage := bson.D{{Key: "_id", Value: 0}}
	for _, f := range fields {
		stage = append(stage, bson.E{Key: f, Value: 1})
	}
	return stage
}

// sortedStrings 数组去空 + 升序（$sortArray 需要 MongoDB 5.2+，站点/IP 读模型已在用）。
func sortedStrings(expr string) bson.D {
	return bson.D{{Key: "$sortArray", Value: bson.D{
		{Key: "input", Value: bson.D{{Key: "$filter", Value: bson.D{
			{Key: "input", Value: expr},
			{Key: "as", Value: "v"},
			{Key: "cond", Value: bson.D{{Key: "$and", Value: bson.A{
				bson.D{{Key: "$ne", Value: bson.A{"$$v", ""}}},
				bson.D{{Key: "$ne", Value: bson.A{"$$v", nil}}},
			}}}},
		}}}},
		{Key: "sortBy", Value: 1},
	}}}
}

// sitePipeline 站点口径：asset 按 host+port 聚合，展示字段取 time 最新一条
// （与 /api/assets/site/list 的 $group + $top 完全同口径，导出不加 facet/分页）。
func sitePipeline(match bson.M, fields []string, counting bool) mongo.Pipeline {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: match}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: bson.D{
				{Key: "host", Value: "$host"},
				{Key: "port", Value: "$port"},
			}},
			{Key: "assetCount", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "lastSeen", Value: bson.D{{Key: "$max", Value: "$time"}}},
			{Key: "latest", Value: bson.D{{Key: "$top", Value: bson.D{
				{Key: "sortBy", Value: bson.D{{Key: "time", Value: -1}}},
				{Key: "output", Value: bson.D{
					{Key: "ip", Value: "$ip"},
					{Key: "title", Value: "$title"},
					{Key: "statuscode", Value: "$statuscode"},
					{Key: "project", Value: "$project"},
					{Key: "url", Value: "$url"},
				}},
			}}}},
		}}},
	}
	if counting {
		return append(pipeline, countStage)
	}
	pipeline = append(pipeline,
		bson.D{{Key: "$project", Value: bson.D{
			{Key: "_id", Value: 0},
			{Key: "host", Value: ifNull("$_id.host", "")},
			{Key: "port", Value: ifNull("$_id.port", "")},
			{Key: "ip", Value: ifNull("$latest.ip", "")},
			{Key: "title", Value: ifNull("$latest.title", "")},
			// 缺失的 statuscode 留空（不补 0）：导出不做"看起来有值"的填充
			{Key: "statusCode", Value: ifNull("$latest.statuscode", "")},
			{Key: "project", Value: ifNull("$latest.project", "")},
			{Key: "url", Value: ifNull("$latest.url", "")},
			{Key: "assetCount", Value: 1},
			{Key: "lastSeen", Value: ifNull("$lastSeen", "")},
		}}},
		bson.D{{Key: "$sort", Value: bson.D{
			{Key: "lastSeen", Value: -1},
			{Key: "host", Value: 1},
			{Key: "port", Value: 1},
		}}},
		bson.D{{Key: "$project", Value: projectStage(fields)}},
	)
	return pipeline
}

// ipPipeline IP 口径：asset 按 ip 聚合，(port, protocol) 与域名去重成数组，
// 与 /api/assets/ip/list 同口径；导出不截断数组（读模型列表才 slice 20）。
func ipPipeline(match bson.M, fields []string, counting bool) mongo.Pipeline {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: match}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$ip"},
			{Key: "ports", Value: bson.D{{Key: "$addToSet", Value: bson.D{
				{Key: "port", Value: "$port"},
				{Key: "protocol", Value: "$service"},
			}}}},
			{Key: "hosts", Value: bson.D{{Key: "$addToSet", Value: "$host"}}},
			{Key: "assetCount", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "lastSeen", Value: bson.D{{Key: "$max", Value: "$time"}}},
		}}},
	}
	if counting {
		return append(pipeline, countStage)
	}
	pipeline = append(pipeline,
		bson.D{{Key: "$project", Value: bson.D{
			{Key: "_id", Value: 0},
			{Key: "ip", Value: ifNull("$_id", "")},
			{Key: "assetCount", Value: 1},
			{Key: "lastSeen", Value: ifNull("$lastSeen", "")},
			{Key: "ports", Value: bson.D{{Key: "$sortArray", Value: bson.D{
				{Key: "input", Value: "$ports"},
				{Key: "sortBy", Value: bson.D{{Key: "port", Value: 1}}},
			}}}},
			{Key: "domains", Value: sortedStrings("$hosts")},
		}}},
		bson.D{{Key: "$project", Value: bson.D{
			{Key: "ip", Value: 1},
			{Key: "assetCount", Value: 1},
			{Key: "lastSeen", Value: 1},
			{Key: "portCount", Value: bson.D{{Key: "$size", Value: "$ports"}}},
			{Key: "domainCount", Value: bson.D{{Key: "$size", Value: "$domains"}}},
			{Key: "ports", Value: 1},
			{Key: "domains", Value: 1},
		}}},
		bson.D{{Key: "$sort", Value: bson.D{
			{Key: "lastSeen", Value: -1},
			{Key: "ip", Value: 1},
		}}},
		bson.D{{Key: "$project", Value: projectStage(fields)}},
	)
	return pipeline
}

// ipPortPipeline 端口口径：IPAsset 的 ports[].server[] 摊平成「一行一个 ip+port+server」。
func ipPortPipeline(match bson.M, fields []string, counting bool) mongo.Pipeline {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: match}},
		{{Key: "$unwind", Value: bson.D{{Key: "path", Value: "$ports"}}}},
		{{Key: "$unwind", Value: bson.D{
			{Key: "path", Value: "$ports.server"},
			{Key: "preserveNullAndEmptyArrays", Value: true},
		}}},
	}
	if counting {
		return append(pipeline, countStage)
	}
	pipeline = append(pipeline,
		bson.D{{Key: "$project", Value: bson.D{
			{Key: "_id", Value: 0},
			{Key: "ip", Value: "$ip"},
			{Key: "port", Value: "$ports.port"},
			{Key: "domain", Value: "$ports.server.domain"},
			{Key: "service", Value: "$ports.server.service"},
			{Key: "webServer", Value: "$ports.server.webServer"},
			{Key: "technologies", Value: "$ports.server.technologies"},
			{Key: "project", Value: 1},
			{Key: "taskName", Value: 1},
			{Key: "rootDomain", Value: 1},
			{Key: "time", Value: 1},
		}}},
		bson.D{{Key: "$sort", Value: bson.D{
			{Key: "ip", Value: 1},
			{Key: "port", Value: 1},
		}}},
		bson.D{{Key: "$project", Value: projectStage(fields)}},
	)
	return pipeline
}

// assetDomainPipeline 资产表的域名维度：asset 按 host 聚合出端口/IP/项目集合。
// 这里所有展示字段都是集合/聚合算出来的，没有来源的字段不进字段表。
func assetDomainPipeline(match bson.M, fields []string, counting bool) mongo.Pipeline {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: match}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$host"},
			{Key: "ips", Value: bson.D{{Key: "$addToSet", Value: "$ip"}}},
			{Key: "ports", Value: bson.D{{Key: "$addToSet", Value: "$port"}}},
			{Key: "projects", Value: bson.D{{Key: "$addToSet", Value: "$project"}}},
			{Key: "rootDomains", Value: bson.D{{Key: "$addToSet", Value: "$rootDomain"}}},
			{Key: "assetCount", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "lastSeen", Value: bson.D{{Key: "$max", Value: "$time"}}},
		}}},
	}
	if counting {
		return append(pipeline, countStage)
	}
	pipeline = append(pipeline,
		bson.D{{Key: "$project", Value: bson.D{
			{Key: "_id", Value: 0},
			{Key: "host", Value: ifNull("$_id", "")},
			{Key: "ips", Value: sortedStrings("$ips")},
			{Key: "ports", Value: sortedStrings("$ports")},
			{Key: "projects", Value: sortedStrings("$projects")},
			{Key: "rootDomains", Value: sortedStrings("$rootDomains")},
			{Key: "assetCount", Value: 1},
			{Key: "lastSeen", Value: ifNull("$lastSeen", "")},
		}}},
		bson.D{{Key: "$sort", Value: bson.D{
			{Key: "lastSeen", Value: -1},
			{Key: "host", Value: 1},
		}}},
		bson.D{{Key: "$project", Value: projectStage(fields)}},
	)
	return pipeline
}
