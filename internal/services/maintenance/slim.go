package maintenance

// 大字段瘦身：把 SensitiveBody 这类「一条响应体近 1MB」的集合按字节截断。
//
// 背景（2026-11-28 生产实测）：SensitiveBody 18,274MB / 19,122 条，平均 1,002,003 字节，
// 最大 5.9MB；全库 19.1GB 里它占 95.6%，备份 3.3GB/9min 的账基本都记在它头上。
// 写入方是扫描节点（本仓库只读），所以只能做「存量瘦身」，写入侧的封顶要在扫描端改。
//
// 设计取舍：
//  1. 默认 dry-run，只统计不写；真正执行要显式 confirm 串（与备份恢复的 RESTORE 同款门禁）；
//  2. 单次有上限（默认 500 条 / 2GB 读取），可重复执行，避免一次请求把库和 IO 拖死；
//  3. 只处理未截断过的文档（bodyTruncated != true），可断点续跑；
//  4. 截断后写回 bodyLen（原始字节数）与 bodyTruncated，读侧据此提示「已截断」，
//     而不是让用户以为响应体本来就这么短。

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrConfirmRequired 未提供确认串
var ErrConfirmRequired = errors.New("confirm 不匹配：真正执行需要传 confirm=SLIM")

// SlimOptions 瘦身参数
type SlimOptions struct {
	Collection string
	KeepBytes  int
	Limit      int
	DryRun     bool
	Confirm    string
}

// SlimResult 执行结果（dry-run 时 Updated 为 0，WouldTrim/可回收字节为估算）
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
	DurationMs    int64  `json:"durationMs"`
}

const (
	defaultKeepBytes = 64 * 1024
	defaultLimit     = 500
	maxLimit         = 2000
)

// SlimBodies 按字节截断大字段（只读 + 定点更新，不动其它字段）。
func SlimBodies(ctx context.Context, opt SlimOptions) (*SlimResult, error) {
	if mongodb.DB == nil {
		return nil, errors.New("数据库未就绪")
	}
	coll := opt.Collection
	if coll == "" {
		coll = "SensitiveBody"
	}
	// 只允许明确的几个集合：这是个不可逆操作，不能拿任意集合试
	if coll != "SensitiveBody" && coll != "HttpBody" {
		return nil, fmt.Errorf("不支持瘦身集合 %q（仅 SensitiveBody / HttpBody）", coll)
	}
	keep := opt.KeepBytes
	if keep <= 0 {
		keep = defaultKeepBytes
	}
	limit := opt.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if !opt.DryRun && opt.Confirm != "SLIM" {
		return nil, ErrConfirmRequired
	}

	start := time.Now()
	c := mongodb.DB.Collection(coll)
	filter := bson.M{
		"bodyTruncated": bson.M{"$ne": true},
		"body":          bson.M{"$exists": true},
	}
	// 只挑「长度超过阈值」的：$expr + $strLenBytes，避免把所有文档拉回来算
	filter["$expr"] = bson.M{"$gt": bson.A{bson.M{"$strLenBytes": "$body"}, keep}}

	cur, err := c.Find(ctx, filter, options.Find().
		SetProjection(bson.M{"_id": 1, "body": 1}).
		SetLimit(int64(limit)).
		SetSort(bson.M{"_id": 1}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	res := &SlimResult{Collection: coll, KeepBytes: keep, DryRun: opt.DryRun}
	type doc struct {
		ID   interface{} `bson:"_id"`
		Body string      `bson:"body"`
	}
	writes := make([]interface{}, 0, limit)
	for cur.Next(ctx) {
		var d doc
		if err := cur.Decode(&d); err != nil {
			return nil, err
		}
		res.Scanned++
		res.WouldTrim++
		res.BytesBefore += int64(len(d.Body))
		res.BytesAfter += int64(keep)
		if opt.DryRun {
			continue
		}
		// 按字节截断：多字节字符可能被切断，但响应体是 HTML/JSON 文本，
		// 截断后写入 bodyTruncated 标记，读侧会提示原文长度，不会假装完整。
		trimmed := d.Body
		if len(trimmed) > keep {
			trimmed = trimmed[:keep]
		}
		writes = append(writes, bson.M{"id": d.ID, "body": trimmed, "len": int64(len(d.Body))})
	}
	if err := cur.Err(); err != nil {
		return nil, err
	}

	if !opt.DryRun {
		for _, w := range writes {
			m, _ := w.(bson.M)
			_, uerr := c.UpdateOne(ctx, bson.M{"_id": m["id"]}, bson.M{"$set": bson.M{
				"body":          m["body"],
				"bodyLen":       m["len"],
				"bodyTruncated": true,
			}})
			if uerr != nil {
				return res, uerr
			}
			res.Updated++
		}
	}

	res.ReclaimBytes = res.BytesBefore - res.BytesAfter
	if res.ReclaimBytes < 0 {
		res.ReclaimBytes = 0
	}
	if left, cerr := c.CountDocuments(ctx, filter); cerr == nil {
		res.RemainingDocs = left
		if !opt.DryRun && res.Updated > 0 {
			res.RemainingDocs -= int64(res.Updated)
			if res.RemainingDocs < 0 {
				res.RemainingDocs = 0
			}
		}
	}
	res.DurationMs = time.Since(start).Milliseconds()
	return res, nil
}
