package github

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ThreatRepository github_threat_config 单例 + 黑名单仓库 + 情报发现历史
type ThreatRepository interface {
	GetConfig(ctx context.Context) (*models.GithubThreatConfig, error)
	SaveSource(ctx context.Context, kind string, src models.GithubThreatSource) error
	TouchSourceRun(ctx context.Context, kind string, lastRunAt time.Time, nextRunAt *time.Time, stats string) error
	// AddIgnoredRepo / RemoveIgnoredRepo 黑名单仓库
	AddIgnoredRepo(ctx context.Context, repo, reason string) (bool, error)
	RemoveIgnoredRepo(ctx context.Context, repo string) (bool, error)
	ListIgnoredRepos(ctx context.Context) ([]models.GithubIgnoredRepo, error)
	// UpsertHistory 情报发现入库（kind+key 唯一），返回是否新增
	UpsertHistory(ctx context.Context, item *models.GithubThreatHistory) (bool, error)
	ListHistory(ctx context.Context, kind string, limit int) ([]models.GithubThreatHistory, error)
	CountHistory(ctx context.Context, kind, keyword string) (int64, error)
}

type threatRepository struct{}

// NewThreatRepository 创建威胁情报仓库
func NewThreatRepository() ThreatRepository { return &threatRepository{} }

func (r *threatRepository) coll() (*mongo.Collection, error) {
	if mongodb.DB == nil {
		return nil, errors.New("github_threat_config: database not initialized")
	}
	return mongodb.DB.Collection(CollectionThreat), nil
}

func (r *threatRepository) historyColl() (*mongo.Collection, error) {
	if mongodb.DB == nil {
		return nil, errors.New("github_threat_history: database not initialized")
	}
	return mongodb.DB.Collection(CollectionHistory), nil
}

// DefaultThreatConfig 默认配置：三来源全关，默认 6 小时间隔
func DefaultThreatConfig() *models.GithubThreatConfig {
	src := func() models.GithubThreatSource {
		return models.GithubThreatSource{
			Enabled:       false,
			Keywords:      []string{},
			IntervalHours: models.GithubDefaultIntervalHours,
			Targets:       []string{},
		}
	}
	return &models.GithubThreatConfig{
		ID:           models.GithubThreatConfigID,
		CVE:          src(),
		Hacker:       src(),
		Tools:        src(),
		IgnoredRepos: []models.GithubIgnoredRepo{},
	}
}

func (r *threatRepository) GetConfig(ctx context.Context) (*models.GithubThreatConfig, error) {
	collection, err := r.coll()
	if err != nil {
		return nil, err
	}
	var cfg models.GithubThreatConfig
	err = collection.FindOne(ctx, bson.M{"_id": models.GithubThreatConfigID}).Decode(&cfg)
	if errors.Is(err, mongo.ErrNoDocuments) {
		def := DefaultThreatConfig()
		def.UpdatedAt = time.Now()
		opt := options.Update().SetUpsert(true)
		if _, upErr := collection.UpdateOne(ctx,
			bson.M{"_id": models.GithubThreatConfigID},
			bson.M{"$setOnInsert": def},
			opt,
		); upErr != nil && !mongo.IsDuplicateKeyError(upErr) {
			return nil, fmt.Errorf("github_threat_config: seed default: %w", upErr)
		}
		return def, nil
	}
	if err != nil {
		return nil, fmt.Errorf("github_threat_config: get: %w", err)
	}
	normalizeThreatConfig(&cfg)
	return &cfg, nil
}

// normalizeThreatConfig 老文档缺字段时补齐默认值（读路径只补内存，不写库）
func normalizeThreatConfig(cfg *models.GithubThreatConfig) {
	if cfg == nil {
		return
	}
	cfg.ID = models.GithubThreatConfigID
	fix := func(src *models.GithubThreatSource) {
		if src.IntervalHours <= 0 {
			src.IntervalHours = models.GithubDefaultIntervalHours
		}
		if src.Keywords == nil {
			src.Keywords = []string{}
		}
		if src.Targets == nil {
			src.Targets = []string{}
		}
	}
	fix(&cfg.CVE)
	fix(&cfg.Hacker)
	fix(&cfg.Tools)
	if cfg.IgnoredRepos == nil {
		cfg.IgnoredRepos = []models.GithubIgnoredRepo{}
	}
}

// SourceField 来源对应的文档字段名
func SourceField(kind string) (string, error) {
	switch kind {
	case models.GithubThreatKindCVE:
		return "cve", nil
	case models.GithubThreatKindHacker:
		return "hacker", nil
	case models.GithubThreatKindTools:
		return "tools", nil
	default:
		return "", fmt.Errorf("unknown threat source: %s", kind)
	}
}

func (r *threatRepository) SaveSource(ctx context.Context, kind string, src models.GithubThreatSource) error {
	field, err := SourceField(kind)
	if err != nil {
		return err
	}
	collection, err := r.coll()
	if err != nil {
		return err
	}
	if src.IntervalHours <= 0 {
		src.IntervalHours = models.GithubDefaultIntervalHours
	}
	if src.Keywords == nil {
		src.Keywords = []string{}
	}
	if src.Targets == nil {
		src.Targets = []string{}
	}
	_, err = collection.UpdateOne(ctx,
		bson.M{"_id": models.GithubThreatConfigID},
		bson.M{
			"$set": bson.M{
				field:       src,
				"updatedAt": time.Now(),
			},
		},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("github_threat_config: save %s: %w", kind, err)
	}
	return nil
}

func (r *threatRepository) TouchSourceRun(ctx context.Context, kind string, lastRunAt time.Time, nextRunAt *time.Time, stats string) error {
	field, err := SourceField(kind)
	if err != nil {
		return err
	}
	collection, err := r.coll()
	if err != nil {
		return err
	}
	set := bson.M{
		field + ".lastRunAt":    lastRunAt,
		field + ".lastRunStats": stats,
		"updatedAt":             time.Now(),
	}
	if nextRunAt != nil {
		set[field+".nextRunAt"] = *nextRunAt
	}
	if _, err := collection.UpdateOne(ctx,
		bson.M{"_id": models.GithubThreatConfigID},
		bson.M{"$set": set},
		options.Update().SetUpsert(true),
	); err != nil {
		return fmt.Errorf("github_threat_config: touch %s: %w", kind, err)
	}
	return nil
}

func (r *threatRepository) AddIgnoredRepo(ctx context.Context, repo, reason string) (bool, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return false, errors.New("repo 不能为空")
	}
	collection, err := r.coll()
	if err != nil {
		return false, err
	}
	// 先确保单例存在
	if _, err := r.GetConfig(ctx); err != nil {
		return false, err
	}
	res, err := collection.UpdateOne(ctx,
		bson.M{"_id": models.GithubThreatConfigID, "ignoredRepos.repo": bson.M{"$ne": repo}},
		bson.M{
			"$push": bson.M{"ignoredRepos": models.GithubIgnoredRepo{
				Repo:      repo,
				Reason:    reason,
				CreatedAt: time.Now(),
			}},
			"$set": bson.M{"updatedAt": time.Now()},
		},
	)
	if err != nil {
		return false, fmt.Errorf("github_threat_config: add ignored repo: %w", err)
	}
	return res.ModifiedCount > 0, nil
}

func (r *threatRepository) RemoveIgnoredRepo(ctx context.Context, repo string) (bool, error) {
	repo = strings.TrimSpace(repo)
	collection, err := r.coll()
	if err != nil {
		return false, err
	}
	res, err := collection.UpdateOne(ctx,
		bson.M{"_id": models.GithubThreatConfigID},
		bson.M{
			"$pull": bson.M{"ignoredRepos": bson.M{"repo": repo}},
			"$set":  bson.M{"updatedAt": time.Now()},
		},
	)
	if err != nil {
		return false, fmt.Errorf("github_threat_config: remove ignored repo: %w", err)
	}
	return res.ModifiedCount > 0, nil
}

func (r *threatRepository) ListIgnoredRepos(ctx context.Context) ([]models.GithubIgnoredRepo, error) {
	cfg, err := r.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	if cfg.IgnoredRepos == nil {
		return []models.GithubIgnoredRepo{}, nil
	}
	return cfg.IgnoredRepos, nil
}

func (r *threatRepository) UpsertHistory(ctx context.Context, item *models.GithubThreatHistory) (bool, error) {
	collection, err := r.historyColl()
	if err != nil {
		return false, err
	}
	if item.DiscoveredAt.IsZero() {
		item.DiscoveredAt = time.Now()
	}
	// $set 与 $setOnInsert 不能出现同一路径，因此只在插入时写 kind/key/discoveredAt，
	// 其余可变字段走 $set（重复发现时刷新内容）。
	res, err := collection.UpdateOne(ctx,
		bson.M{"kind": item.Kind, "key": item.Key},
		bson.M{
			"$setOnInsert": bson.M{
				"kind":         item.Kind,
				"key":          item.Key,
				"discoveredAt": item.DiscoveredAt,
			},
			"$set": bson.M{
				"title":       item.Title,
				"url":         item.URL,
				"repo":        item.Repo,
				"description": item.Description,
				"severity":    item.Severity,
				"keyword":     item.Keyword,
			},
		},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return false, nil
		}
		return false, fmt.Errorf("github_threat_history: upsert: %w", err)
	}
	return res.UpsertedCount > 0, nil
}

func (r *threatRepository) ListHistory(ctx context.Context, kind string, limit int) ([]models.GithubThreatHistory, error) {
	collection, err := r.historyColl()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "discoveredAt", Value: -1}}).
		SetLimit(int64(limit))
	cursor, err := collection.Find(ctx, bson.M{"kind": kind}, opts)
	if err != nil {
		return nil, fmt.Errorf("github_threat_history: list: %w", err)
	}
	defer cursor.Close(ctx)
	items := make([]models.GithubThreatHistory, 0, limit)
	if err := cursor.All(ctx, &items); err != nil {
		return nil, fmt.Errorf("github_threat_history: decode: %w", err)
	}
	return items, nil
}

func (r *threatRepository) CountHistory(ctx context.Context, kind, keyword string) (int64, error) {
	collection, err := r.historyColl()
	if err != nil {
		return 0, err
	}
	filter := bson.M{"kind": kind}
	if kw := escapeRegex(keyword); kw != "" {
		re := primitive.Regex{Pattern: kw, Options: "i"}
		filter["$or"] = []bson.M{{"title": re}, {"repo": re}, {"description": re}, {"key": re}}
	}
	count, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("github_threat_history: count: %w", err)
	}
	return count, nil
}
