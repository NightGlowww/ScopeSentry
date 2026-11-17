package common

import (
	"context"

	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/utils/helper"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
)

const (
	// deleteBySearchBatch 每批先查 _id 再按 _id 删，避免单次 deleteMany 长时间持锁。
	deleteBySearchBatch = 1000
	// DeleteBySearchMaxPerRun 单次请求最多删除多少条：超出就让调用方再点一次，
	// 免得一个 HTTP 请求跑几分钟把网关拖到超时。
	DeleteBySearchMaxPerRun = 50000
)

type deleteCandidate struct {
	ID       primitive.ObjectID `bson:"_id"`
	Hash     string             `bson:"hash"`
	BodyHash string             `bson:"bodyhash"`
}

// DeleteBySearch 按列表页当前的搜索表达式 + 筛选条件批量删除（issue #258 / #191）。
//
// 空筛选等于清空整个集合，必须显式 ConfirmAll 才放行；调用方每次最多删
// DeleteBySearchMaxPerRun 条，返回实际删除条数，剩余的下次继续。
func (s *service) DeleteBySearch(ctx *gin.Context, req *models.DeleteBySearchRequest) (int64, error) {
	if !isValidCollection(req.Index) {
		return 0, ErrInvalidCollection
	}

	searchQuery := models.SearchRequest{
		Index:            req.Index,
		SearchExpression: req.Search,
		Filter:           req.Filter,
	}
	var filter map[string]interface{}
	var err error
	if req.Index == "asset" {
		filter, err = s.assetService.BuildSearchQuery(searchQuery)
	} else {
		filter, err = helper.GetSearchQuery(searchQuery)
	}
	if err != nil {
		return 0, err
	}
	if len(filter) == 0 && !req.ConfirmAll {
		return 0, ErrNeedConfirmAll
	}

	query := bson.M(filter)
	fields := []string{"_id"}
	switch req.Index {
	case "asset":
		fields = append(fields, "bodyhash")
	case "vulnerability":
		fields = append(fields, "hash")
	}

	var deleted int64
	bodyHashes := make([]string, 0)
	vulHashes := make([]string, 0)

	for deleted < DeleteBySearchMaxPerRun {
		var docs []deleteCandidate
		if err := s.repo.Find(ctx.Request.Context(), req.Index, query, deleteBySearchBatch, fields, &docs); err != nil {
			return deleted, err
		}
		if len(docs) == 0 {
			break
		}

		ids := make([]primitive.ObjectID, 0, len(docs))
		for _, doc := range docs {
			ids = append(ids, doc.ID)
			if doc.BodyHash != "" {
				bodyHashes = append(bodyHashes, doc.BodyHash)
			}
			if doc.Hash != "" {
				vulHashes = append(vulHashes, doc.Hash)
			}
		}

		n, err := s.repo.DeleteMany(ctx.Request.Context(), req.Index, bson.M{"_id": bson.M{"$in": ids}})
		if err != nil {
			return deleted, err
		}
		deleted += n
		if n == 0 {
			// 查询到了但一条也没删掉：说明有并发写入/权限问题，直接停下避免死循环
			break
		}
	}

	if len(vulHashes) > 0 {
		go s.deleteVulnerabilityDetails(vulHashes)
	}
	if len(bodyHashes) > 0 {
		go s.cleanupUnusedBodyHashes(bodyHashes)
	}

	logger.Info("delete by search", zap.String("collection", req.Index), zap.Int64("deleted", deleted))
	return deleted, nil
}

// deleteVulnerabilityDetails 删除漏洞正文（vulnerabilityDetail 按 hash 关联）。
func (s *service) deleteVulnerabilityDetails(hashes []string) {
	if len(hashes) == 0 {
		return
	}
	if _, err := s.repo.DeleteMany(context.Background(), "vulnerabilityDetail", bson.M{"hash": bson.M{"$in": hashes}}); err != nil {
		logger.Error("failed to delete vulnerability detail", zap.Error(err))
	}
}

// cleanupUnusedBodyHashes 删除不再被任何资产引用的 HttpBody。
func (s *service) cleanupUnusedBodyHashes(hashes []string) {
	if len(hashes) == 0 {
		return
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"bodyhash": bson.M{"$in": hashes}}}},
		{{Key: "$group", Value: bson.M{"_id": "$bodyhash"}}},
	}
	cursor, err := s.commonRepo.Aggregate(context.Background(), "asset", pipeline)
	if err != nil {
		logger.Error("failed to aggregate bodyhash", zap.Error(err))
		return
	}
	defer cursor.Close(context.Background())

	used := make(map[string]bool)
	for cursor.Next(context.Background()) {
		var row struct {
			ID string `bson:"_id"`
		}
		if err := cursor.Decode(&row); err != nil {
			logger.Error("failed to decode bodyhash", zap.Error(err))
			continue
		}
		if row.ID != "" {
			used[row.ID] = true
		}
	}

	unused := make([]string, 0, len(hashes))
	seen := make(map[string]bool, len(hashes))
	for _, hash := range hashes {
		if hash == "" || used[hash] || seen[hash] {
			continue
		}
		seen[hash] = true
		unused = append(unused, hash)
	}
	if len(unused) == 0 {
		return
	}
	if _, err := s.repo.DeleteMany(context.Background(), "HttpBody", bson.M{"hash": bson.M{"$in": unused}}); err != nil {
		logger.Error("failed to delete HttpBody", zap.Error(err))
		return
	}
	logger.Info("deleted unused HttpBody", zap.Int("count", len(unused)))
}
