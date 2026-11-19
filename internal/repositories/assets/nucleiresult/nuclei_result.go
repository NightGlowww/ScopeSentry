package nucleiresult

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Repository Nuclei 结果读模型仓库（对标 ARL-Next assetNucleiResult）。
//
// 只读 vulnerability 集合，不改写入链路：扫描侧怎么写，这里就怎么读。
// vulnerability 没有独立的 nuclei 表，ARL 的 asset_nuclei_result 在本项目里
// 落到「漏洞」这条已有链路上（templateId->vulnid、severity->level、matchedAt->time）。
type Repository interface {
	// List 分页列表（按 time 倒序），返回列表与命中总数
	List(ctx context.Context, filter bson.M, pageIndex, pageSize int) ([]models.NucleiResultItem, int64, error)
	// FindByID 单条完整记录；不存在时返回 (nil, nil)
	FindByID(ctx context.Context, id primitive.ObjectID) (*models.NucleiResultDetail, error)
	// UpdateStatus 更新处置状态
	UpdateStatus(ctx context.Context, id primitive.ObjectID, status int) error
	// UpdateStatusMany 批量更新处置状态
	UpdateStatusMany(ctx context.Context, ids []primitive.ObjectID, status int) (int64, error)
	// ExplainList 列表查询的 explain（queryPlanner），用于验证走索引
	ExplainList(ctx context.Context, filter bson.M, pageIndex, pageSize int) (bson.M, error)
}

type repository struct {
	db        *mongo.Database
	coll      *mongo.Collection
	detailCol *mongo.Collection
}

// NewRepository 创建 Nuclei 结果读模型仓库实例
func NewRepository() Repository {
	return &repository{
		db:        mongodb.DB,
		coll:      mongodb.DB.Collection("vulnerability"),
		detailCol: mongodb.DB.Collection("vulnerabilityDetail"),
	}
}

// nucleiDoc vulnerability 集合的解码结构（字段名与 models.Vulnerability 一致）
type nucleiDoc struct {
	ID     primitive.ObjectID `bson:"_id"`
	VulnID string             `bson:"vulnid"`
	// 扫描侧落库是 "vulname"；"vulnname" 是本仓库读模型此前的错拼写（老数据/回归数据里可能有），
	// 两个都解码，列表才不会出现「有结果但名字为空」。
	VulName       string `bson:"vulname"`
	VulNameLegacy string `bson:"vulnname"`
	Level         string `bson:"level"`
	URL           string `bson:"url"`
	Matched       string `bson:"matched"`
	Project       string `bson:"project"`
	// 扫描侧（ScopeSentry-Scan 的 types.VulnResult）该字段只打了 yaml tag，
	// bson 编码按 Go 字段名小写落库成 "rootdomain"；老数据/手写数据里是 "rootDomain"。
	// 两个拼写都解码，读模型才不会因为大小写把根域名丢成空串。
	RootDomain       string   `bson:"rootdomain"`
	RootDomainLegacy string   `bson:"rootDomain"`
	TaskName         string   `bson:"taskName"`
	Tags             []string `bson:"tags"`
	Status           int      `bson:"status"`
	Time             string   `bson:"time"`
	Hash             string   `bson:"hash"`
	Request          string   `bson:"request"`
	Response         string   `bson:"response"`
}

// listProjection 列表只取展示字段：request/response 是大字段，只在详情端点取。
var listProjection = bson.D{
	{Key: "vulnid", Value: 1},
	{Key: "vulname", Value: 1},
	{Key: "vulnname", Value: 1},
	{Key: "level", Value: 1},
	{Key: "url", Value: 1},
	{Key: "matched", Value: 1},
	{Key: "project", Value: 1},
	{Key: "rootdomain", Value: 1},
	{Key: "rootDomain", Value: 1},
	{Key: "taskName", Value: 1},
	{Key: "tags", Value: 1},
	{Key: "status", Value: 1},
	{Key: "time", Value: 1},
	{Key: "hash", Value: 1},
}

// listSort 按 time 倒序：vulnerability 上已有 {time:-1} 以及
// {project:1,time:-1} / {level:1,time:-1} / {project:1,level:1,time:-1}，
// 排序能在索引里完成，不会退化成 blocking sort。
var listSort = bson.D{{Key: "time", Value: -1}}

// effectiveVulName 取漏洞名：优先扫描侧实际落库的 "vulname"，回退到历史 "vulnname"。
func (d *nucleiDoc) effectiveVulName() string {
	if d.VulName != "" {
		return d.VulName
	}
	return d.VulNameLegacy
}

// effectiveRootDomain 取根域名：优先扫描侧实际落库的小写键，回退到老数据的 camelCase。
func (d *nucleiDoc) effectiveRootDomain() string {
	if d.RootDomain != "" {
		return d.RootDomain
	}
	return d.RootDomainLegacy
}

func listFindOptions(pageIndex, pageSize int) *options.FindOptions {
	return options.Find().
		SetProjection(listProjection).
		SetSort(listSort).
		SetSkip(int64((pageIndex - 1) * pageSize)).
		SetLimit(int64(pageSize))
}

// List 分页列表。
func (r *repository) List(ctx context.Context, filter bson.M, pageIndex, pageSize int) ([]models.NucleiResultItem, int64, error) {
	total, err := r.coll.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count nuclei results: %w", err)
	}
	if total == 0 {
		return []models.NucleiResultItem{}, 0, nil
	}

	cursor, err := r.coll.Find(ctx, filter, listFindOptions(pageIndex, pageSize))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to find nuclei results: %w", err)
	}
	defer cursor.Close(ctx)

	var docs []nucleiDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, 0, fmt.Errorf("failed to decode nuclei results: %w", err)
	}

	items := make([]models.NucleiResultItem, 0, len(docs))
	for _, d := range docs {
		items = append(items, models.NucleiResultItem{
			ID:         d.ID.Hex(),
			TemplateID: d.VulnID,
			Name:       d.effectiveVulName(),
			Severity:   d.Level,
			URL:        d.URL,
			Host:       hostFromURL(d.URL),
			Matched:    d.Matched,
			Project:    d.Project,
			RootDomain: d.effectiveRootDomain(),
			TaskName:   d.TaskName,
			Tags:       normalizeTags(d.Tags),
			Status:     d.Status,
			MatchedAt:  d.Time,
			Hash:       d.Hash,
		})
	}
	return items, total, nil
}

// FindByID 取单条完整记录。
//
// request/response 优先用漏洞主文档上的字段；主文档没有（历史数据 / 只有 detail 记录）
// 时按 hash 回退到 vulnerabilityDetail 集合，避免详情页开出来是空的。
func (r *repository) FindByID(ctx context.Context, id primitive.ObjectID) (*models.NucleiResultDetail, error) {
	var doc nucleiDoc
	err := r.coll.FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find nuclei result %s: %w", id.Hex(), err)
	}

	req, resp := doc.Request, doc.Response
	if (req == "" || resp == "") && doc.Hash != "" {
		var detail struct {
			Request  string `bson:"req"`
			Response string `bson:"res"`
		}
		if err := r.detailCol.FindOne(ctx, bson.M{"hash": doc.Hash}).Decode(&detail); err == nil {
			if req == "" {
				req = detail.Request
			}
			if resp == "" {
				resp = detail.Response
			}
		} else if err != mongo.ErrNoDocuments {
			return nil, fmt.Errorf("failed to find vulnerability detail for hash %s: %w", doc.Hash, err)
		}
	}

	return &models.NucleiResultDetail{
		NucleiResultItem: models.NucleiResultItem{
			ID:         doc.ID.Hex(),
			TemplateID: doc.VulnID,
			Name:       doc.effectiveVulName(),
			Severity:   doc.Level,
			URL:        doc.URL,
			Host:       hostFromURL(doc.URL),
			Matched:    doc.Matched,
			Project:    doc.Project,
			RootDomain: doc.effectiveRootDomain(),
			TaskName:   doc.TaskName,
			Tags:       normalizeTags(doc.Tags),
			Status:     doc.Status,
			MatchedAt:  doc.Time,
			Hash:       doc.Hash,
		},
		Request:  req,
		Response: resp,
	}, nil
}

// ExplainList 列表查询的 explain。
func (r *repository) ExplainList(ctx context.Context, filter bson.M, pageIndex, pageSize int) (bson.M, error) {
	cmd := bson.D{
		{Key: "explain", Value: bson.D{
			{Key: "find", Value: "vulnerability"},
			{Key: "filter", Value: filter},
			{Key: "sort", Value: listSort},
			{Key: "skip", Value: int64((pageIndex - 1) * pageSize)},
			{Key: "limit", Value: int64(pageSize)},
			{Key: "projection", Value: listProjection},
		}},
		{Key: "verbosity", Value: "queryPlanner"},
	}
	var out bson.M
	if err := r.db.RunCommand(ctx, cmd).Decode(&out); err != nil {
		return nil, fmt.Errorf("failed to explain nuclei list: %w", err)
	}
	return out, nil
}

// normalizeTags 空 tags 统一成空数组，避免前端拿到 null 还要判空
func normalizeTags(tags []string) []string {
	if tags == nil {
		return []string{}
	}
	return tags
}

// hostFromURL 从命中的 URL 里取 host 供列表展示/分组；
// vulnerability 没有独立的 host 字段，解析失败就返回空串，不影响其它字段。
func hostFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Host
}

// UpdateStatus 更新一条漏洞结果的处置状态（1未处理 2处理中 3忽略 4疑似 5确认 6已处理）。
// 误报请用 3（忽略），确认存在用 5。
func (r *repository) UpdateStatus(ctx context.Context, id primitive.ObjectID, status int) error {
	_, err := r.coll.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"status": status}})
	if err != nil {
		return fmt.Errorf("failed to update nuclei result status: %w", err)
	}
	return nil
}

// UpdateStatusMany 批量更新处置状态，返回实际影响条数。
func (r *repository) UpdateStatusMany(ctx context.Context, ids []primitive.ObjectID, status int) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res, err := r.coll.UpdateMany(ctx, bson.M{"_id": bson.M{"$in": ids}}, bson.M{"$set": bson.M{"status": status}})
	if err != nil {
		return 0, fmt.Errorf("failed to batch update nuclei result status: %w", err)
	}
	return res.ModifiedCount, nil
}
