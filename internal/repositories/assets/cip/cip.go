package cip

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// 主体列表里每个 company 保留的样例域名数。
const sampleDomainLimit = 10

// Repository ICP/主体读模型仓库（对标 ARL-Next assetCip）。
//
// 数据源是 RootDomain 集合的 company/icp/domain 字段，只读，不写任何集合。
type Repository interface {
	// List 按 company 分组统计域名数
	List(ctx context.Context, req models.CIPListRequest, pageIndex, pageSize int) ([]models.CIPRow, int64, error)
	// FindRootDomain 按域名取一条根域名记录（找不到返回 nil, nil）
	FindRootDomain(ctx context.Context, domain string) (*models.RootDomain, error)
	// ListDomainsByCompany 取某主体下的域名（分页）；project 非空时只看该项目
	ListDomainsByCompany(ctx context.Context, company, project string, pageIndex, pageSize int) ([]models.CIPDomain, int64, error)
	// ICPsByCompany 取某主体下的 ICP 号集合；project 非空时只看该项目
	ICPsByCompany(ctx context.Context, company, project string) ([]string, error)
	// CountSubjects 主体总数（去重后的 company 数）
	CountSubjects(ctx context.Context) (int64, error)
}

type repository struct {
	coll *mongo.Collection
}

// NewRepository 创建 ICP/主体读模型仓库实例
func NewRepository() Repository {
	return &repository{coll: mongodb.DB.Collection("RootDomain")}
}

// List 按 company 分组统计域名数。
func (r *repository) List(ctx context.Context, req models.CIPListRequest, pageIndex, pageSize int) ([]models.CIPRow, int64, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: buildListMatch(req)}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$company"},
			{Key: "domainCount", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "icps", Value: bson.D{{Key: "$addToSet", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$icp", ""}}}}}},
			{Key: "samples", Value: bson.D{{Key: "$addToSet", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$domain", ""}}}}}},
			{Key: "lastTime", Value: bson.D{{Key: "$max", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$time", ""}}}}}},
		}}},
		// 第一段先把 ICP 集合排序去空；第二段再取首个 ICP + 截断样例域名
		{{Key: "$project", Value: bson.D{
			{Key: "_id", Value: 1},
			{Key: "domainCount", Value: 1},
			{Key: "lastTime", Value: 1},
			{Key: "icps", Value: bson.D{{Key: "$filter", Value: bson.D{
				{Key: "input", Value: bson.D{{Key: "$sortArray", Value: bson.D{{Key: "input", Value: "$icps"}, {Key: "sortBy", Value: 1}}}}},
				{Key: "as", Value: "i"},
				{Key: "cond", Value: bson.D{{Key: "$ne", Value: bson.A{"$$i", ""}}}},
			}}}},
			{Key: "samples", Value: bson.D{{Key: "$slice", Value: bson.A{
				bson.D{{Key: "$sortArray", Value: bson.D{{Key: "input", Value: "$samples"}, {Key: "sortBy", Value: 1}}}},
				sampleDomainLimit,
			}}}},
		}}},
		{{Key: "$project", Value: bson.D{
			{Key: "_id", Value: 1},
			{Key: "domainCount", Value: 1},
			{Key: "lastTime", Value: 1},
			{Key: "icps", Value: 1},
			{Key: "samples", Value: 1},
			{Key: "icp", Value: bson.D{{Key: "$ifNull", Value: bson.A{
				bson.D{{Key: "$arrayElemAt", Value: bson.A{"$icps", 0}}},
				"",
			}}}},
		}}},
		{{Key: "$facet", Value: bson.M{
			"total": mongo.Pipeline{
				{{Key: "$count", Value: "count"}},
			},
			"data": mongo.Pipeline{
				// 域名数倒序，同数量按主体名升序兜底，保证分页稳定
				{{Key: "$sort", Value: bson.D{{Key: "domainCount", Value: -1}, {Key: "_id", Value: 1}}}},
				{{Key: "$skip", Value: int64((pageIndex - 1) * pageSize)}},
				{{Key: "$limit", Value: int64(pageSize)}},
			},
		}}},
	}

	var facets []struct {
		Total []struct {
			Count int64 `bson:"count"`
		} `bson:"total"`
		Data []models.CIPRow `bson:"data"`
	}
	if err := r.aggregate(ctx, pipeline, &facets); err != nil {
		return nil, 0, err
	}
	if len(facets) == 0 {
		return []models.CIPRow{}, 0, nil
	}
	rows := facets[0].Data
	if rows == nil {
		rows = []models.CIPRow{}
	}
	var total int64
	if len(facets[0].Total) > 0 {
		total = facets[0].Total[0].Count
	}
	return rows, total, nil
}

// FindRootDomain 按域名取根域名记录。域名大小写不敏感：先按原样查，
// 未命中再按小写查（库里的 domain 通常是扫描端写入的小写形态）。
func (r *repository) FindRootDomain(ctx context.Context, domain string) (*models.RootDomain, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return nil, nil
	}
	candidates := []string{domain}
	if lower := strings.ToLower(domain); lower != domain {
		candidates = append(candidates, lower)
	}

	for _, candidate := range candidates {
		var doc models.RootDomain
		err := r.coll.FindOne(ctx, bson.M{"domain": candidate}).Decode(&doc)
		if err == nil {
			return &doc, nil
		}
		if err != mongo.ErrNoDocuments {
			return nil, fmt.Errorf("find root domain %q: %w", candidate, err)
		}
	}
	return nil, nil
}

// ListDomainsByCompany 取某主体下的域名（按 time 倒序分页）。
func (r *repository) ListDomainsByCompany(ctx context.Context, company, project string, pageIndex, pageSize int) ([]models.CIPDomain, int64, error) {
	filter := bson.M{"company": company}
	if project = strings.TrimSpace(project); project != "" {
		filter["project"] = project
	}
	total, err := r.coll.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count domains of company %q: %w", company, err)
	}
	if total == 0 {
		return []models.CIPDomain{}, 0, nil
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "time", Value: -1}}).
		SetSkip(int64((pageIndex - 1) * pageSize)).
		SetLimit(int64(pageSize))
	cursor, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("find domains of company %q: %w", company, err)
	}
	defer cursor.Close(ctx)

	var rows []models.CIPDomain
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, 0, fmt.Errorf("decode domains of company %q: %w", company, err)
	}
	if rows == nil {
		rows = []models.CIPDomain{}
	}
	return rows, total, nil
}

// ICPsByCompany 取某主体下的 ICP 号集合（去空、排序）。
func (r *repository) ICPsByCompany(ctx context.Context, company, project string) ([]string, error) {
	filter := bson.M{"company": company}
	if project = strings.TrimSpace(project); project != "" {
		filter["project"] = project
	}
	values, err := r.coll.Distinct(ctx, "icp", filter)
	if err != nil {
		return nil, fmt.Errorf("distinct icp of company %q: %w", company, err)
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if s, ok := value.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out, nil
}

// CountSubjects 主体总数（去重后的 company 数）。
func (r *repository) CountSubjects(ctx context.Context) (int64, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: nonEmptyCompanyCond()}},
		{{Key: "$group", Value: bson.D{{Key: "_id", Value: "$company"}}}},
		{{Key: "$count", Value: "count"}},
	}
	var rows []struct {
		Count int64 `bson:"count"`
	}
	if err := r.aggregate(ctx, pipeline, &rows); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return rows[0].Count, nil
}

func (r *repository) aggregate(ctx context.Context, pipeline mongo.Pipeline, out interface{}) error {
	opts := options.Aggregate().SetAllowDiskUse(true)
	cursor, err := r.coll.Aggregate(ctx, pipeline, opts)
	if err != nil {
		return fmt.Errorf("failed to aggregate cip: %w", err)
	}
	defer cursor.Close(ctx)
	if err := cursor.All(ctx, out); err != nil {
		return fmt.Errorf("failed to decode cip aggregation: %w", err)
	}
	return nil
}

// nonEmptyCompanyCond 只统计有主体名的记录：空 company 不是一个可展示的主体。
func nonEmptyCompanyCond() bson.M {
	return bson.M{"company": bson.M{"$exists": true, "$nin": bson.A{nil, ""}}}
}

// buildListMatch 拼主体列表的过滤条件。
func buildListMatch(req models.CIPListRequest) bson.M {
	conds := bson.A{nonEmptyCompanyCond()}
	if project := strings.TrimSpace(req.Project); project != "" {
		conds = append(conds, bson.M{"project": project})
	}
	if company := strings.TrimSpace(req.Company); company != "" {
		conds = append(conds, bson.M{"company": company})
	}
	if kw := strings.TrimSpace(req.Keyword); kw != "" {
		conds = append(conds, keywordMatch(kw))
	}
	return composeAnd(conds)
}

// keywordMatch 在 company/icp/domain 上做不区分大小写的子串匹配。
func keywordMatch(keyword string) bson.M {
	re := primitive.Regex{Pattern: regexp.QuoteMeta(keyword), Options: "i"}
	return bson.M{"$or": bson.A{
		bson.M{"company": re},
		bson.M{"icp": re},
		bson.M{"domain": re},
	}}
}

// composeAnd 组合条件：空列表返回空文档（match all），单个条件直接返回。
func composeAnd(conds bson.A) bson.M {
	switch len(conds) {
	case 0:
		return bson.M{}
	case 1:
		if m, ok := conds[0].(bson.M); ok {
			return m
		}
		return bson.M{"$and": conds}
	default:
		return bson.M{"$and": conds}
	}
}
