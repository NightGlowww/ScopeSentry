package maintenance

// 大字段瘦身：把 SensitiveBody 这类「一条响应体近 1MB」的集合按字节截断。
//
// 背景（2026-11-28 生产实测）：SensitiveBody 18,274MB / 19,122 条，平均 1,002,003 字节，
// 最大 5.9MB；全库 19.1GB 里它占 95.6%，备份 3.3GB/9min 的账基本都记在它头上。
// 写入方是扫描节点（本仓库只读），所以只能做「存量瘦身」，写入侧的封顶要在扫描端改。
//
// 设计取舍：
//  1. 默认 dry-run 只统计；真正执行要显式 confirm=SLIM（与备份恢复的 RESTORE 同款门禁）；
//  2. 只处理未截断过的文档（bodyTruncated != true），可断点续跑；
//  3. 截断后写回 bodyLen（原始字节数）与 bodyTruncated，读侧据此提示「已截断」；
//  4. 性能：筛选条件用 $expr + $strLenBytes，索引用不上，等于每次全扫。
//     因此分批时必须带续跑游标（nextAfterId），否则 38 批就是 38 次全表扫；
//     落库用 BulkWrite 每 20 条一批，把往返次数压下来。
//  5. 干跑分两种：limit>0 只看这一批；limit=0 走一次聚合统计全量（可回收多少、还差多少条）。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrConfirmRequired 未提供确认串
var ErrConfirmRequired = errors.New("confirm 不匹配：真正执行需要传 confirm=SLIM")

// ErrBadCollection 集合不在白名单
var ErrBadCollection = errors.New("collection not allowed")

// SlimOptions 瘦身参数
type SlimOptions struct {
	Collection string
	KeepBytes  int
	Limit      int
	DryRun     bool
	Confirm    string
	// AfterID 续跑游标：上一批返回的 nextAfterId。
	// 不带它每次都从头扫（$expr 用不上索引），38 批会变成 38 次全表扫。
	AfterID string
}

// SlimResult 执行结果
type SlimResult struct {
	Collection    string `json:"collection"`
	KeepBytes     int    `json:"keepBytes"`
	DryRun        bool   `json:"dryRun"`
	Scanned       int    `json:"scanned"`
	WouldTrim     int    `json:"wouldTrim"`
	Updated       int    `json:"updated"`
	BytesBefore   int64  `json:"bytesBefore"`
	BytesAfter    int64  `json:"bytesAfter"`
	ReclaimBytes  int64  `json:"reclaimBytes"`
	RemainingDocs int64  `json:"remainingDocs"`
	// NextAfterID 下一批的续跑游标；HasMore=false 表示已扫到尾
	NextAfterID string `json:"nextAfterId,omitempty"`
	HasMore     bool   `json:"hasMore"`
	DurationMs  int64  `json:"durationMs"`
}

const (
	defaultKeepBytes = 64 * 1024
	// 单批默认 200、上限 500：一批要把正文读进内存，500 × 1MB 就是 500MB，
	// 默认值按「内存安全」而不是「次数最少」来定。
	defaultLimit = 200
	maxLimit     = 500
	// bulkFlush 每多少条落一次 BulkWrite：压往返次数，同时限制内存占用
	bulkFlush = 20
)

// truncateUTF8 按字节上限截断，并回退到完整 UTF-8 字符边界。
//
// 直接 s[:n] 可能把一个多字节字符切成两半，产生非法 UTF-8（实测截 65536 字节后
// mongosh 读到的字符数只有 64296，就是末尾字符被切开）。最多回退 3 字节。
func truncateUTF8(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && cut > limit-4 {
		if utf8.RuneStart(s[cut]) {
			return s[:cut]
		}
		cut--
	}
	return s[:cut]
}

func slimFilter(keep int, afterID *primitive.ObjectID) bson.M {
	f := bson.M{
		"bodyTruncated": bson.M{"$ne": true},
		"body":          bson.M{"$exists": true},
		"$expr":         bson.M{"$gt": bson.A{bson.M{"$strLenBytes": "$body"}, keep}},
	}
	if afterID != nil {
		f["_id"] = bson.M{"$gt": *afterID}
	}
	return f
}

// SlimBodies 按字节截断大字段。
func SlimBodies(ctx context.Context, opt SlimOptions) (*SlimResult, error) {
	if mongodb.DB == nil {
		return nil, errors.New("数据库未就绪")
	}
	coll := opt.Collection
	if coll == "" {
		coll = "SensitiveBody"
	}
	if coll != "SensitiveBody" && coll != "HttpBody" {
		return nil, fmt.Errorf("%w: %q（仅 SensitiveBody / HttpBody）", ErrBadCollection, coll)
	}
	keep := opt.KeepBytes
	if keep <= 0 {
		keep = defaultKeepBytes
	}
	limit := opt.Limit
	if limit < 0 {
		limit = 0
	}
	if limit == 0 && !opt.DryRun {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if !opt.DryRun && opt.Confirm != "SLIM" {
		return nil, ErrConfirmRequired
	}
	var afterID *primitive.ObjectID
	if s := strings.TrimSpace(opt.AfterID); s != "" {
		if oid, err := primitive.ObjectIDFromHex(s); err == nil {
			afterID = &oid
		}
	}

	start := time.Now()
	c := mongodb.DB.Collection(coll)
	res := &SlimResult{Collection: coll, KeepBytes: keep, DryRun: opt.DryRun}

	// limit=0 且 dry-run：一次聚合统计全量（比逐批扫快得多，也只扫一遍）
	if limit == 0 && opt.DryRun {
		pipe := mongo.Pipeline{
			{{"$match", bson.M{"bodyTruncated": bson.M{"$ne": true}, "body": bson.M{"$exists": true}}}},
			{{"$project", bson.M{"len": bson.M{"$strLenBytes": "$body"}}}},
			{{"$match", bson.M{"len": bson.M{"$gt": keep}}}},
			{{"$group", bson.M{"_id": nil, "n": bson.M{"$sum": 1}, "bytes": bson.M{"$sum": "$len"}}}},
		}
		cur, err := c.Aggregate(ctx, pipe)
		if err != nil {
			return nil, err
		}
		defer cur.Close(ctx)
		var agg struct {
			N     int64 `bson:"n"`
			Bytes int64 `bson:"bytes"`
		}
		if cur.Next(ctx) {
			if err := cur.Decode(&agg); err != nil {
				return nil, err
			}
		}
		res.Scanned = int(agg.N)
		res.WouldTrim = int(agg.N)
		res.BytesBefore = agg.Bytes
		res.BytesAfter = agg.N * int64(keep)
		res.RemainingDocs = agg.N
		res.DurationMs = time.Since(start).Milliseconds()
		finalizeReclaim(res)
		return res, nil
	}

	cur, err := c.Find(ctx, slimFilter(keep, afterID), options.Find().
		SetProjection(bson.M{"_id": 1, "body": 1}).
		SetLimit(int64(limit)).
		SetSort(bson.M{"_id": 1}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	type doc struct {
		ID   primitive.ObjectID `bson:"_id"`
		Body string             `bson:"body"`
	}
	models := make([]mongo.WriteModel, 0, bulkFlush)
	flush := func() error {
		if opt.DryRun || len(models) == 0 {
			models = models[:0]
			return nil
		}
		if _, werr := c.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false)); werr != nil {
			return werr
		}
		res.Updated += len(models)
		models = models[:0]
		return nil
	}

	var lastID primitive.ObjectID
	for cur.Next(ctx) {
		var d doc
		if err := cur.Decode(&d); err != nil {
			return res, err
		}
		res.Scanned++
		res.WouldTrim++
		res.BytesBefore += int64(len(d.Body))
		trimmed := truncateUTF8(d.Body, keep)
		res.BytesAfter += int64(len(trimmed))
		lastID = d.ID
		models = append(models, mongo.NewUpdateOneModel().
			SetFilter(bson.M{"_id": d.ID}).
			SetUpdate(bson.M{"$set": bson.M{
				"body":          trimmed,
				"bodyLen":       int64(len(d.Body)),
				"bodyTruncated": true,
			}}))
		if len(models) >= bulkFlush {
			if err := flush(); err != nil {
				return res, err
			}
		}
	}
	if err := cur.Err(); err != nil {
		return res, err
	}
	if err := flush(); err != nil {
		return res, err
	}
	if !lastID.IsZero() {
		res.NextAfterID = lastID.Hex()
	}
	res.HasMore = res.Scanned >= limit

	if left, cerr := c.CountDocuments(ctx, slimFilter(keep, &lastID)); cerr == nil {
		if opt.DryRun {
			res.RemainingDocs = left
		} else {
			// 写完之后还剩多少（不含本批已被截断的）
			res.RemainingDocs = left
		}
	}
	res.DurationMs = time.Since(start).Milliseconds()
	finalizeReclaim(res)
	return res, nil
}

func finalizeReclaim(res *SlimResult) {
	res.ReclaimBytes = res.BytesBefore - res.BytesAfter
	if res.ReclaimBytes < 0 {
		res.ReclaimBytes = 0
	}
}
