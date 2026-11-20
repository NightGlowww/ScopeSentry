package mcp

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// 资产画像 / 企业画像 / 跨库检索：对齐 ARL-Next 的 MCP 能力面（2026-11-21）。
// 我们原有 58 个工具偏「管理面」（节点/字典/模板），缺「问一个目标它到底是什么」的聚合入口。

type assetProfileInput struct {
	Target string `json:"target" jsonschema:"资产标的：域名（test.com）、IP（1.2.3.4）或站点 URL（https://test.com:8443）"`
	Limit  int    `json:"limit,omitempty" jsonschema:"每类样本条数，默认 5，上限 20"`
}

func escapeRegex(s string) string {
	return regexp.QuoteMeta(strings.TrimSpace(s))
}

// normalizeTarget 从 URL / IP / 域名里取出可匹配的主机名
func normalizeTarget(raw string) string {
	t := strings.TrimSpace(raw)
	if t == "" {
		return ""
	}
	if i := strings.Index(t, "://"); i >= 0 {
		t = t[i+3:]
	}
	if i := strings.IndexAny(t, "/?#"); i >= 0 {
		t = t[:i]
	}
	if i := strings.LastIndex(t, ":"); i > 0 && !strings.Contains(t[i:], "]") {
		t = t[:i]
	}
	return strings.Trim(strings.ToLower(t), ".")
}

func clampLimit(n int) int64 {
	if n <= 0 {
		return 5
	}
	if n > 20 {
		return 20
	}
	return int64(n)
}

// hostFilter 同时匹配「主机等于 target」与「主机以 .target 结尾」
func hostFilter(field, host string) bson.M {
	re := "^(.+\\.)?" + escapeRegex(host) + "$"
	return bson.M{field: bson.M{"$regex": re, "$options": "i"}}
}

func getAssetProfile(ctx context.Context, _ *mcp.CallToolRequest, input assetProfileInput) (*mcp.CallToolResult, any, error) {
	host := normalizeTarget(input.Target)
	if host == "" {
		return errorResult("target 不能为空", nil)
	}
	if mongodb.DB == nil {
		return errorResult("数据库未就绪", nil)
	}
	limit := clampLimit(input.Limit)
	db := mongodb.DB
	out := map[string]interface{}{"target": host}

	// 子域名：数量 + 样本
	subFilter := hostFilter("host", host)
	if n, err := db.Collection("subdomain").CountDocuments(ctx, subFilter); err == nil {
		out["subdomainCount"] = n
	}
	if cur, err := db.Collection("subdomain").Find(ctx, subFilter, options.Find().SetLimit(limit)); err == nil {
		var rows []bson.M
		_ = cur.All(ctx, &rows)
		out["subdomains"] = rows
	}

	// 端口/指纹：host 或 ip 命中
	assetFilter := bson.M{"$or": []bson.M{hostFilter("host", host), hostFilter("url", host), {"ip": host}}}
	if n, err := db.Collection("asset").CountDocuments(ctx, assetFilter); err == nil {
		out["assetCount"] = n
	}
	if cur, err := db.Collection("asset").Find(ctx, assetFilter, options.Find().SetLimit(limit).SetProjection(bson.M{
		"host": 1, "ip": 1, "port": 1, "service": 1, "title": 1, "technologies": 1, "_id": 0,
	})); err == nil {
		var rows []bson.M
		_ = cur.All(ctx, &rows)
		out["assets"] = rows
	}

	// URL
	urlFilter := hostFilter("output", host)
	if n, err := db.Collection("UrlScan").CountDocuments(ctx, urlFilter); err == nil {
		out["urlCount"] = n
	}

	// 漏洞
	vulnFilter := bson.M{"$or": []bson.M{hostFilter("url", host), {"matched": bson.M{"$regex": escapeRegex(host), "$options": "i"}}}}
	if n, err := db.Collection("vulnerability").CountDocuments(ctx, vulnFilter); err == nil {
		out["vulnerabilityCount"] = n
	}
	if cur, err := db.Collection("vulnerability").Find(ctx, vulnFilter, options.Find().SetLimit(limit).SetProjection(bson.M{
		"vulname": 1, "level": 1, "url": 1, "_id": 0,
	})); err == nil {
		var rows []bson.M
		_ = cur.All(ctx, &rows)
		out["vulnerabilities"] = rows
	}

	// 敏感信息
	sensFilter := hostFilter("url", host)
	if n, err := db.Collection("SensitiveResult").CountDocuments(ctx, sensFilter); err == nil {
		out["sensitiveCount"] = n
	}

	return jsonToolResult(out)
}

type searchAssetsInput struct {
	Query string `json:"query" jsonschema:"检索词：域名片段、URL 片段或漏洞关键字"`
	Limit int    `json:"limit,omitempty" jsonschema:"每类样本条数，默认 5，上限 20"`
}

func searchAssets(ctx context.Context, _ *mcp.CallToolRequest, input searchAssetsInput) (*mcp.CallToolResult, any, error) {
	q := strings.TrimSpace(input.Query)
	if q == "" {
		return errorResult("query 不能为空", nil)
	}
	if mongodb.DB == nil {
		return errorResult("数据库未就绪", nil)
	}
	limit := clampLimit(input.Limit)
	db := mongodb.DB
	like := bson.M{"$regex": escapeRegex(q), "$options": "i"}
	out := map[string]interface{}{"query": q}

	specs := []struct {
		key   string
		coll  string
		field string
		proj  bson.M
	}{
		{"subdomains", "subdomain", "host", bson.M{"host": 1, "ip": 1, "_id": 0}},
		{"assets", "asset", "host", bson.M{"host": 1, "ip": 1, "port": 1, "title": 1, "_id": 0}},
		{"urls", "UrlScan", "output", bson.M{"output": 1, "status": 1, "_id": 0}},
		{"vulnerabilities", "vulnerability", "vulname", bson.M{"vulname": 1, "level": 1, "url": 1, "_id": 0}},
		{"sensitive", "SensitiveResult", "url", bson.M{"url": 1, "sid": 1, "_id": 0}},
	}
	for _, sp := range specs {
		filter := bson.M{sp.field: like}
		if n, err := db.Collection(sp.coll).CountDocuments(ctx, filter); err == nil {
			out[sp.key+"Count"] = n
			if n > 0 {
				cur, cerr := db.Collection(sp.coll).Find(ctx, filter, options.Find().SetLimit(limit).SetProjection(sp.proj))
				if cerr == nil {
					var rows []bson.M
					_ = cur.All(ctx, &rows)
					out[sp.key] = rows
				}
			}
		}
	}
	return jsonToolResult(out)
}

var _ = fmt.Sprintf
