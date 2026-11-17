package cert

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// 单条证书最多保留的关联 host 数；完整计数由 hostCount 单独给出。
const hostSampleLimit = 50

// TLS 字段路径：当前扫描端（ScopeSentry-Scan）把 tlsx 的证书响应写在
// asset.tls 下，且因为 tlsx 结构没有 bson tag，嵌入的 CertificateResponse
// 被驱动编码成 tls.certificateresponse 子文档（key 为 Go 字段名小写、无下划线）。
// 这里同时兼容：
//   - tls.certificateresponse.*：扫描端真实写入的 BSON 形状；
//   - tls.*：若某版本驱动把嵌入结构内联后的形状；
//   - tlsdata.*：服务端模型 legacy 的 map[string]string 形状（下划线 key）。
var (
	subjectDNPaths = []string{
		"$tls.certificateresponse.subjectdn", "$tls.subjectdn",
		"$tlsdata.subject_dn", "$tlsdata.subject",
	}
	subjectCNPaths = []string{
		"$tls.certificateresponse.subjectcn", "$tls.subjectcn",
		"$tlsdata.subject_cn", "$tlsdata.subject",
	}
	issuerDNPaths = []string{
		"$tls.certificateresponse.issuerdn", "$tls.issuerdn",
		"$tlsdata.issuer_dn", "$tlsdata.issuer",
	}
	issuerCNPaths = []string{
		"$tls.certificateresponse.issuercn", "$tls.issuercn",
		"$tlsdata.issuer_cn", "$tlsdata.issuer",
	}
	serialPaths = []string{
		"$tls.certificateresponse.serial", "$tls.serial", "$tlsdata.serial",
	}
	fingerprintPaths = []string{
		"$tls.certificateresponse.fingerprinthash.sha256", "$tls.fingerprinthash.sha256",
		"$tlsdata.fingerprint_sha256", "$tlsdata.sha256", "$tlsdata.fingerprint",
		"$tls.certificateresponse.fingerprinthash.sha1", "$tls.fingerprinthash.sha1",
		"$tlsdata.fingerprint_sha1", "$tlsdata.sha1",
		"$tls.certificateresponse.fingerprinthash.md5", "$tls.fingerprinthash.md5",
		"$tlsdata.fingerprint_md5", "$tlsdata.md5",
	}
	sha1Paths = []string{
		"$tls.certificateresponse.fingerprinthash.sha1", "$tls.fingerprinthash.sha1",
		"$tlsdata.fingerprint_sha1", "$tlsdata.sha1",
	}
	md5Paths = []string{
		"$tls.certificateresponse.fingerprinthash.md5", "$tls.fingerprinthash.md5",
		"$tlsdata.fingerprint_md5", "$tlsdata.md5",
	}
	notBeforePaths = []string{
		"$tls.certificateresponse.notbefore", "$tls.notbefore", "$tlsdata.not_before",
	}
	notAfterPaths = []string{
		"$tls.certificateresponse.notafter", "$tls.notafter", "$tlsdata.not_after",
	}
	sanPaths = []string{
		"$tls.certificateresponse.subjectan", "$tls.subjectan",
		"$tlsdata.subject_an", "$tlsdata.san",
	}
)

// certExistsCond 先按「可能存在证书」收窄：三条路径任一条存在即进入投影，
// 投影之后再按身份字段（subject/serial/issuer/指纹/notAfter）过滤掉空的 TLS 探测。
var certExistsCond = bson.M{"$or": bson.A{
	bson.M{"tls.certificateresponse": bson.M{"$exists": true}},
	bson.M{"tls.subjectdn": bson.M{"$exists": true}},
	bson.M{"tlsdata": bson.M{"$exists": true}},
}}

// Repository 证书读模型仓库（对标 ARL-Next assetCert）。
//
// 数据源是 asset 集合的 TLS 字段，只读，不写任何集合。
type Repository interface {
	// List 聚合证书列表（按 notAfter 升序分页）
	List(ctx context.Context, req models.CertListRequest, pageIndex, pageSize int, now time.Time) ([]models.CertRow, int64, error)
	// GetByID 按聚合键取单张证书（找不到返回 nil, nil）。
	// project 非空时只统计该项目下的关联 host，与列表口径一致。
	GetByID(ctx context.Context, id, project string, now time.Time) (*models.CertRow, error)
	// Count 证书总数（去重后）
	Count(ctx context.Context) (int64, error)
}

type repository struct {
	coll *mongo.Collection
}

// NewRepository 创建证书读模型仓库实例
func NewRepository() Repository {
	return &repository{coll: mongodb.DB.Collection("asset")}
}

// List 聚合证书列表。
func (r *repository) List(ctx context.Context, req models.CertListRequest, pageIndex, pageSize int, now time.Time) ([]models.CertRow, int64, error) {
	pipeline := buildBasePipeline(buildPreMatch(req.Project, ""), now)
	pipeline = append(pipeline, bson.D{{Key: "$match", Value: buildListMatch(req, now)}})
	pipeline = append(pipeline, bson.D{{Key: "$facet", Value: bson.M{
		"total": mongo.Pipeline{
			{{Key: "$count", Value: "count"}},
		},
		"data": mongo.Pipeline{
			// notAfter 升序：最快到期的排前面；同时间按聚合键兜底，保证分页稳定
			{{Key: "$sort", Value: bson.D{{Key: "notAfter", Value: 1}, {Key: "_id", Value: 1}}}},
			{{Key: "$skip", Value: int64((pageIndex - 1) * pageSize)}},
			{{Key: "$limit", Value: int64(pageSize)}},
		},
	}}})

	var facets []struct {
		Total []struct {
			Count int64 `bson:"count"`
		} `bson:"total"`
		Data []models.CertRow `bson:"data"`
	}
	if err := r.aggregate(ctx, pipeline, &facets); err != nil {
		return nil, 0, err
	}
	if len(facets) == 0 {
		return []models.CertRow{}, 0, nil
	}
	rows := facets[0].Data
	if rows == nil {
		rows = []models.CertRow{}
	}
	var total int64
	if len(facets[0].Total) > 0 {
		total = facets[0].Total[0].Count
	}
	return rows, total, nil
}

// GetByID 按聚合键取单张证书。
// 指纹/序列号形态先在 $match 里收窄，避免为一张证书扫描全集合。
func (r *repository) GetByID(ctx context.Context, id, project string, now time.Time) (*models.CertRow, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}
	pipeline := buildBasePipeline(buildPreMatch(project, id), now)
	pipeline = append(pipeline,
		bson.D{{Key: "$match", Value: bson.M{"_id": id}}},
		bson.D{{Key: "$limit", Value: 1}},
	)

	var rows []models.CertRow
	if err := r.aggregate(ctx, pipeline, &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// Count 证书总数（去重后）。
func (r *repository) Count(ctx context.Context) (int64, error) {
	pipeline := buildBasePipeline(certExistsCond, time.Now().UTC())
	pipeline = append(pipeline, bson.D{{Key: "$count", Value: "count"}})

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
		return fmt.Errorf("failed to aggregate certificates: %w", err)
	}
	defer cursor.Close(ctx)
	if err := cursor.All(ctx, out); err != nil {
		return fmt.Errorf("failed to decode certificate aggregation: %w", err)
	}
	return nil
}

// buildPreMatch 拼 $project 之前的过滤条件。
// detailID 非空时按聚合键前缀追加身份过滤（fp:/sn:）。
func buildPreMatch(project, detailID string) bson.M {
	conds := bson.A{certExistsCond}
	if project = strings.TrimSpace(project); project != "" {
		conds = append(conds, bson.M{"project": project})
	}
	if id := strings.TrimSpace(detailID); id != "" {
		if cond := detailIdentityCond(id); cond != nil {
			conds = append(conds, cond)
		}
	}
	return composeAnd(conds)
}

// detailIdentityCond 把聚合键还原成可索引的字段等值条件。
// sub: 形态（无指纹、无 serial+issuer）无法安全还原，返回 nil 走全量聚合。
func detailIdentityCond(id string) bson.M {
	switch {
	case strings.HasPrefix(id, "fp:"):
		value := strings.TrimPrefix(id, "fp:")
		if value == "" {
			return nil
		}
		return anyEquals(append(append([]string{}, fingerprintPaths...), sha1Paths...), value)
	case strings.HasPrefix(id, "sn:"):
		parts := strings.SplitN(strings.TrimPrefix(id, "sn:"), "|", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil
		}
		return bson.M{"$and": bson.A{
			anyEquals(issuerDNPaths, parts[0]),
			anyEquals(serialPaths, parts[1]),
		}}
	default:
		return nil
	}
}

// anyEquals 生成「任一字段等于 value」的 $or，用于跨新旧两种字段形状收窄。
func anyEquals(paths []string, value string) bson.M {
	or := make(bson.A, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		field := strings.TrimPrefix(path, "$")
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		or = append(or, bson.M{field: value})
	}
	return bson.M{"$or": or}
}

// buildBasePipeline 投影出统一字段 -> 过滤空记录 -> 按证书身份分组 -> 计算展示字段。
func buildBasePipeline(preMatch bson.M, now time.Time) mongo.Pipeline {
	return mongo.Pipeline{
		{{Key: "$match", Value: preMatch}},
		{{Key: "$project", Value: canonicalProjection()}},
		{{Key: "$match", Value: identityMatch()}},
		groupStage(),
		{{Key: "$project", Value: postGroupProjection(now)}},
	}
}

// canonicalProjection 把三种 TLS 字段形状投影成同一组字段。
func canonicalProjection() bson.D {
	return bson.D{
		{Key: "_id", Value: 0},
		{Key: "subject", Value: asString(firstOf(subjectDNPaths, ""))},
		{Key: "subjectCN", Value: asString(firstOf(subjectCNPaths, ""))},
		{Key: "issuer", Value: asString(firstOf(issuerDNPaths, ""))},
		{Key: "issuerCN", Value: asString(firstOf(issuerCNPaths, ""))},
		{Key: "serial", Value: asString(firstOf(serialPaths, ""))},
		{Key: "fingerprint", Value: asString(firstOf(fingerprintPaths, ""))},
		{Key: "fingerprintSha1", Value: asString(firstOf(sha1Paths, ""))},
		{Key: "fingerprintMd5", Value: asString(firstOf(md5Paths, ""))},
		// 日期统一成 BSON Date：tls 里是 Date，tlsdata legacy 里可能是 RFC3339 字符串
		{Key: "notBefore", Value: asDate(firstOf(notBeforePaths, nil))},
		{Key: "notAfter", Value: asDate(firstOf(notAfterPaths, nil))},
		{Key: "san", Value: asStringArray(firstOf(sanPaths, nil))},
		{Key: "host", Value: asString("$host")},
		{Key: "ip", Value: asString("$ip")},
		{Key: "port", Value: asString("$port")},
	}
}

// identityMatch 丢掉 TLS 探测失败/没有证书身份信息的记录，不造空证书。
func identityMatch() bson.M {
	return bson.M{"$or": bson.A{
		bson.M{"subject": bson.M{"$ne": ""}},
		bson.M{"serial": bson.M{"$ne": ""}},
		bson.M{"issuer": bson.M{"$ne": ""}},
		bson.M{"fingerprint": bson.M{"$ne": ""}},
		bson.M{"notAfter": bson.M{"$ne": nil}},
	}}
}

// groupStage 按证书身份合并：指纹优先，其次 serial+issuer，最后 subject+issuer+有效期兜底。
func groupStage() bson.D {
	return bson.D{{Key: "$group", Value: bson.D{
		{Key: "_id", Value: certKeyExpr()},
		{Key: "subject", Value: bson.D{{Key: "$max", Value: "$subject"}}},
		{Key: "subjectCN", Value: bson.D{{Key: "$max", Value: "$subjectCN"}}},
		{Key: "issuer", Value: bson.D{{Key: "$max", Value: "$issuer"}}},
		{Key: "issuerCN", Value: bson.D{{Key: "$max", Value: "$issuerCN"}}},
		{Key: "serial", Value: bson.D{{Key: "$max", Value: "$serial"}}},
		{Key: "fingerprint", Value: bson.D{{Key: "$max", Value: "$fingerprint"}}},
		{Key: "fingerprintSha1", Value: bson.D{{Key: "$max", Value: "$fingerprintSha1"}}},
		{Key: "fingerprintMd5", Value: bson.D{{Key: "$max", Value: "$fingerprintMd5"}}},
		// $min/$max 会忽略 null：同一证书的 doc 有缺失日期时仍能取到有效值
		{Key: "notBefore", Value: bson.D{{Key: "$min", Value: "$notBefore"}}},
		{Key: "notAfter", Value: bson.D{{Key: "$max", Value: "$notAfter"}}},
		{Key: "sanAll", Value: bson.D{{Key: "$addToSet", Value: "$san"}}},
		{Key: "hosts", Value: bson.D{{Key: "$addToSet", Value: bson.D{
			{Key: "host", Value: "$host"},
			{Key: "ip", Value: "$ip"},
			{Key: "port", Value: "$port"},
		}}}},
	}}}
}

// certKeyExpr 生成稳定的证书聚合键。
func certKeyExpr() interface{} {
	nonEmpty := func(field string) bson.D {
		return bson.D{{Key: "$ne", Value: bson.A{"$" + field, ""}}}
	}
	return bson.D{{Key: "$cond", Value: bson.A{
		nonEmpty("fingerprint"),
		bson.D{{Key: "$concat", Value: bson.A{"fp:", "$fingerprint"}}},
		bson.D{{Key: "$cond", Value: bson.A{
			bson.D{{Key: "$and", Value: bson.A{nonEmpty("serial"), nonEmpty("issuer")}}},
			bson.D{{Key: "$concat", Value: bson.A{"sn:", "$issuer", "|", "$serial"}}},
			bson.D{{Key: "$concat", Value: bson.A{
				"sub:", "$subject", "|", "$issuer", "|",
				asString("$notBefore"), "|", asString("$notAfter"),
			}}},
		}}},
	}}}
}

// postGroupProjection 计算过期标记、SAN 去重、host 计数与样例截断。
func postGroupProjection(now time.Time) bson.D {
	return bson.D{
		{Key: "_id", Value: 1},
		{Key: "subject", Value: 1},
		{Key: "subjectCN", Value: 1},
		{Key: "issuer", Value: 1},
		{Key: "issuerCN", Value: 1},
		{Key: "serial", Value: 1},
		{Key: "fingerprint", Value: 1},
		{Key: "fingerprintSha1", Value: 1},
		{Key: "fingerprintMd5", Value: 1},
		{Key: "notBefore", Value: 1},
		{Key: "notAfter", Value: 1},
		{Key: "expired", Value: bson.D{{Key: "$and", Value: bson.A{
			bson.D{{Key: "$ne", Value: bson.A{"$notAfter", nil}}},
			bson.D{{Key: "$lt", Value: bson.A{"$notAfter", now}}},
		}}}},
		{Key: "san", Value: bson.D{{Key: "$sortArray", Value: bson.D{
			{Key: "input", Value: bson.D{{Key: "$reduce", Value: bson.D{
				{Key: "input", Value: "$sanAll"},
				{Key: "initialValue", Value: bson.A{}},
				{Key: "in", Value: bson.D{{Key: "$setUnion", Value: bson.A{"$$value", "$$this"}}}},
			}}}},
			{Key: "sortBy", Value: 1},
		}}}},
		{Key: "hosts", Value: bson.D{{Key: "$slice", Value: bson.A{
			bson.D{{Key: "$sortArray", Value: bson.D{
				{Key: "input", Value: "$hosts"},
				{Key: "sortBy", Value: bson.D{{Key: "host", Value: 1}, {Key: "port", Value: 1}, {Key: "ip", Value: 1}}},
			}}},
			hostSampleLimit,
		}}}},
		// 计数用截断前的完整 host 集合
		{Key: "hostCount", Value: bson.D{{Key: "$size", Value: "$hosts"}}},
	}
}

// buildListMatch 拼列表的 keyword / expired 过滤（分组之后，落在证书标量上）。
func buildListMatch(req models.CertListRequest, now time.Time) bson.M {
	conds := bson.A{}
	if req.Expired != nil {
		if *req.Expired {
			conds = append(conds, bson.M{"notAfter": bson.M{"$ne": nil, "$lt": now}})
		} else {
			conds = append(conds, bson.M{"notAfter": bson.M{"$ne": nil, "$gte": now}})
		}
	}
	if kw := strings.TrimSpace(req.Keyword); kw != "" {
		conds = append(conds, keywordMatch(kw))
	}
	return composeAnd(conds)
}

// keywordMatch 在证书标量与关联 host 上做不区分大小写的子串匹配。
func keywordMatch(keyword string) bson.M {
	re := primitive.Regex{Pattern: regexp.QuoteMeta(keyword), Options: "i"}
	return bson.M{"$or": bson.A{
		bson.M{"subject": re},
		bson.M{"subjectCN": re},
		bson.M{"issuer": re},
		bson.M{"issuerCN": re},
		bson.M{"serial": re},
		bson.M{"fingerprint": re},
		bson.M{"fingerprintSha1": re},
		bson.M{"fingerprintMd5": re},
		bson.M{"san": re},
		bson.M{"hosts.host": re},
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

// firstOf 依次取第一个非 null 的字段，全部缺失时返回 fallback。
func firstOf(paths []string, fallback interface{}) interface{} {
	var expr interface{} = fallback
	for i := len(paths) - 1; i >= 0; i-- {
		expr = bson.D{{Key: "$ifNull", Value: bson.A{paths[i], expr}}}
	}
	return expr
}

// asString 把任意标量安全转成 string（null/转换失败 -> ""）。
func asString(expr interface{}) interface{} {
	return bson.D{{Key: "$convert", Value: bson.D{
		{Key: "input", Value: expr},
		{Key: "to", Value: "string"},
		{Key: "onError", Value: ""},
		{Key: "onNull", Value: ""},
	}}}
}

// asDate 把 Date 或 RFC3339 字符串安全转成 Date（null/失败 -> null）。
func asDate(expr interface{}) interface{} {
	return bson.D{{Key: "$convert", Value: bson.D{
		{Key: "input", Value: expr},
		{Key: "to", Value: "date"},
		{Key: "onError", Value: nil},
		{Key: "onNull", Value: nil},
	}}}
}

// asStringArray 把数组或逗号分隔字符串统一成去空、去重前的字符串数组。
func asStringArray(expr interface{}) interface{} {
	asArray := bson.D{{Key: "$map", Value: bson.D{
		{Key: "input", Value: expr},
		{Key: "as", Value: "s"},
		{Key: "in", Value: bson.D{{Key: "$trim", Value: bson.D{{Key: "input", Value: asString("$$s")}}}}},
	}}}
	asSplit := bson.D{{Key: "$map", Value: bson.D{
		{Key: "input", Value: bson.D{{Key: "$split", Value: bson.A{asString(expr), ","}}}},
		{Key: "as", Value: "s"},
		{Key: "in", Value: bson.D{{Key: "$trim", Value: bson.D{{Key: "input", Value: asString("$$s")}}}}},
	}}}
	chosen := bson.D{{Key: "$cond", Value: bson.A{
		bson.D{{Key: "$isArray", Value: expr}},
		asArray,
		bson.D{{Key: "$cond", Value: bson.A{
			bson.D{{Key: "$eq", Value: bson.A{asString(expr), ""}}},
			bson.A{},
			asSplit,
		}}},
	}}}
	return bson.D{{Key: "$filter", Value: bson.D{
		{Key: "input", Value: chosen},
		{Key: "as", Value: "s"},
		{Key: "cond", Value: bson.D{{Key: "$ne", Value: bson.A{"$$s", ""}}}},
	}}}
}
