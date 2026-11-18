// Package projectaggregation 实现项目详情页（Overview / 子域名 / 端口 / 服务 四个 Tab）
// 的只读聚合服务。
//
// 契约来源：Python 版 api/project_aggregation.py（已随 Go 重写丢失）+
// 当前 Vue3 前端 frontend/src/api/ProjectAggregation/index.ts 实际发送的字段。
// Go 版新增可选时间范围（startTime/endTime），其余语义保持一致。
package projectaggregation

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	repo "github.com/Autumn-27/ScopeSentry/internal/repositories/projectaggregation"
	"github.com/Autumn-27/ScopeSentry/internal/utils/helper"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	// DefaultPageSize 默认每页条数
	DefaultPageSize = 20
	// MaxPageSize 每页条数上限（超过返回 400）
	MaxPageSize = 200
)

var (
	// ErrInvalidProjectID 项目 id 不是合法 ObjectID 十六进制串
	ErrInvalidProjectID = errors.New("invalid project id")
	// ErrProjectNotFound 项目不存在
	ErrProjectNotFound = errors.New("project not found")
	// ErrPageSizeTooLarge pageSize 超过 MaxPageSize
	ErrPageSizeTooLarge = fmt.Errorf("pageSize must be <= %d", MaxPageSize)
	// ErrInvalidTimeRange 时间范围格式不对
	ErrInvalidTimeRange = errors.New("invalid time range, expect YYYY-MM-DD or YYYY-MM-DD HH:MM:SS")
)

// 表头模糊搜索的字段映射（每个 Tab 只接受自己的 key，未知 key 忽略）
var (
	fqSubdomainFields = map[string]string{"sub_host": "host", "sub_value": "value", "sub_ip": "ip"}
	fqPortFields      = map[string]string{"port_port": "port", "port_domain": "host", "port_ip": "ip", "port_protocol": "service"}
	fqServiceFields   = map[string]string{"service_service": "service", "service_domain": "host", "service_ip": "ip", "service_port": "port"}
)

// Service 项目详情页聚合服务
type Service interface {
	Info(ctx context.Context, req models.ProjectAggregationRequest) (*models.ProjectAggregationInfo, error)
	AssetCount(ctx context.Context, req models.ProjectAggregationRequest) (*models.ProjectAggregationAssetCount, error)
	VulStatistics(ctx context.Context, req models.ProjectAggregationRequest) ([]models.ProjectAggregationLevelCount, error)
	VulData(ctx context.Context, req models.ProjectAggregationRequest) (*models.ProjectAggregationPage, error)
	SubdomainData(ctx context.Context, req models.ProjectAggregationRequest) (*models.ProjectAggregationPage, error)
	PortData(ctx context.Context, req models.ProjectAggregationRequest) (*models.ProjectAggregationPage, error)
	ServiceData(ctx context.Context, req models.ProjectAggregationRequest) (*models.ProjectAggregationPage, error)
}

type service struct {
	repo repo.Repository
}

// NewService 创建聚合服务
func NewService() Service { return &service{repo: repo.NewRepository()} }

// Info 项目基础信息 + 定时任务下次执行时间
func (s *service) Info(ctx context.Context, req models.ProjectAggregationRequest) (*models.ProjectAggregationInfo, error) {
	hex, oid, err := normalizeProjectID(req)
	if err != nil {
		return nil, err
	}
	info, err := s.repo.FindProject(ctx, oid)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, ErrProjectNotFound
	}
	// 定时任务：库里 scheduled._id 就是项目 id（没有则留空，前端按空串展示）
	if next, nerr := s.repo.NextRunTime(ctx, hex); nerr == nil {
		info.NextTime = next
	}
	return info, nil
}

// AssetCount 项目资产族数量汇总
func (s *service) AssetCount(ctx context.Context, req models.ProjectAggregationRequest) (*models.ProjectAggregationAssetCount, error) {
	hex, oid, err := normalizeProjectID(req)
	if err != nil {
		return nil, err
	}
	if err := s.ensureProject(ctx, oid); err != nil {
		return nil, err
	}
	match := projectMatch(hex, oid)

	out := &models.ProjectAggregationAssetCount{}
	pairs := []struct {
		coll string
		dst  *int64
	}{
		{repo.CollAsset, &out.AssetCount},
		{repo.CollSubdomain, &out.SubdomainCount},
		{repo.CollVuln, &out.VulCount},
		{repo.CollURLScan, &out.URLCount},
		{repo.CollSensitive, &out.SensitiveCount},
		{repo.CollDirScan, &out.DirScanCount},
	}
	for _, p := range pairs {
		n, cerr := s.repo.Count(ctx, p.coll, match)
		if cerr != nil {
			return nil, cerr
		}
		*p.dst = n
	}
	return out, nil
}

// VulStatistics 漏洞等级分布（前端饼图）
func (s *service) VulStatistics(ctx context.Context, req models.ProjectAggregationRequest) ([]models.ProjectAggregationLevelCount, error) {
	match, err := s.baseMatch(ctx, req, "vulnerability", nil, nil)
	if err != nil {
		return nil, err
	}
	return s.repo.VulLevelStats(ctx, match)
}

// VulData 漏洞列表（前端 Dashboard 右侧滚动表格）
func (s *service) VulData(ctx context.Context, req models.ProjectAggregationRequest) (*models.ProjectAggregationPage, error) {
	pageIndex, pageSize, err := normalizePage(req.PageIndex, req.PageSize)
	if err != nil {
		return nil, err
	}
	match, err := s.baseMatch(ctx, req, "vulnerability", nil, nil)
	if err != nil {
		return nil, err
	}
	rows, total, err := s.repo.VulList(ctx, match, pageIndex, pageSize)
	if err != nil {
		return nil, err
	}
	return page(rows, total, pageIndex, pageSize), nil
}

// SubdomainData 子域名列表（分页）
func (s *service) SubdomainData(ctx context.Context, req models.ProjectAggregationRequest) (*models.ProjectAggregationPage, error) {
	pageIndex, pageSize, err := normalizePage(req.PageIndex, req.PageSize)
	if err != nil {
		return nil, err
	}
	match, err := s.baseMatch(ctx, req, "subdomain", fqSubdomainFields, nil)
	if err != nil {
		return nil, err
	}
	rows, total, err := s.repo.SubdomainList(ctx, match, pageIndex, pageSize)
	if err != nil {
		return nil, err
	}
	return page(rows, total, pageIndex, pageSize), nil
}

// PortData 端口聚合（按 port 分组，count = 该端口资产数）
func (s *service) PortData(ctx context.Context, req models.ProjectAggregationRequest) (*models.ProjectAggregationPage, error) {
	pageIndex, pageSize, err := normalizePage(req.PageIndex, req.PageSize)
	if err != nil {
		return nil, err
	}
	// asset 里的端口是字符串，空端口不进聚合
	extra := []bson.M{{"port": bson.M{"$nin": []interface{}{"", nil}}}}
	match, err := s.baseMatch(ctx, req, "asset", fqPortFields, extra)
	if err != nil {
		return nil, err
	}
	rows, total, err := s.repo.PortGroups(ctx, match, pageIndex, pageSize)
	if err != nil {
		return nil, err
	}
	return page(rows, total, pageIndex, pageSize), nil
}

// ServiceData 服务聚合（按 service 分组，count = 该服务资产数）
func (s *service) ServiceData(ctx context.Context, req models.ProjectAggregationRequest) (*models.ProjectAggregationPage, error) {
	pageIndex, pageSize, err := normalizePage(req.PageIndex, req.PageSize)
	if err != nil {
		return nil, err
	}
	match, err := s.baseMatch(ctx, req, "asset", fqServiceFields, nil)
	if err != nil {
		return nil, err
	}
	rows, total, err := s.repo.ServiceGroups(ctx, match, pageIndex, pageSize)
	if err != nil {
		return nil, err
	}
	return page(rows, total, pageIndex, pageSize), nil
}

// ---------------------------------------------------------------------------
// 内部工具
// ---------------------------------------------------------------------------

func page(list interface{}, total int64, pageIndex, pageSize int) *models.ProjectAggregationPage {
	return &models.ProjectAggregationPage{List: list, Total: total, PageIndex: pageIndex, PageSize: pageSize}
}

// baseMatch 组装 {项目 + 搜索表达式 + 精确过滤 + 表头模糊搜索 + 时间范围}
func (s *service) baseMatch(ctx context.Context, req models.ProjectAggregationRequest, index string, fqFields map[string]string, extra []bson.M) (bson.M, error) {
	hex, oid, err := normalizeProjectID(req)
	if err != nil {
		return nil, err
	}
	if err := s.ensureProject(ctx, oid); err != nil {
		return nil, err
	}

	clauses := []interface{}{projectMatch(hex, oid)}

	// 搜索表达式 + 精确过滤复用统一解析器（project/type/level/tags ... 走 filterKeyCache）
	expr, err := helper.GetSearchQuery(models.SearchRequest{
		Index:            index,
		SearchExpression: req.Search,
		Filter:           req.Filter,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to parse search expression: %w", err)
	}
	if len(expr) > 0 {
		clauses = append(clauses, expr)
	}

	if fq := buildFq(req.Fq, fqFields); len(fq) > 0 {
		clauses = append(clauses, fq)
	}

	timeMatch, err := buildTimeMatch(req.StartTime, req.EndTime)
	if err != nil {
		return nil, err
	}
	if len(timeMatch) > 0 {
		clauses = append(clauses, timeMatch)
	}

	for _, m := range extra {
		if len(m) > 0 {
			clauses = append(clauses, m)
		}
	}

	// 统一成 $and 结构：单条件 / 多条件走同一条路径，explain 结果也更稳定
	return bson.M{"$and": clauses}, nil
}

// ensureProject 项目不存在时返回 ErrProjectNotFound，避免前端拿到"空 = 无数据"的错觉
func (s *service) ensureProject(ctx context.Context, oid primitive.ObjectID) error {
	info, err := s.repo.FindProject(ctx, oid)
	if err != nil {
		return err
	}
	if info == nil {
		return ErrProjectNotFound
	}
	return nil
}

// normalizeProjectID 从 id 或 filter.project 取项目 id 并归一成 (十六进制串, ObjectID)。
// 库里 project 字段是字符串（IPAsset 是字符串数组），因此需要十六进制串形态。
func normalizeProjectID(req models.ProjectAggregationRequest) (string, primitive.ObjectID, error) {
	raw := strings.TrimSpace(req.ID)
	if raw == "" {
		for _, scope := range []string{"project", "_id"} {
			values, ok := req.Filter[scope]
			if !ok {
				continue
			}
			for _, v := range values {
				switch vv := v.(type) {
				case string:
					if strings.TrimSpace(vv) != "" {
						raw = strings.TrimSpace(vv)
					}
				case primitive.ObjectID:
					raw = vv.Hex()
				}
				if raw != "" {
					break
				}
			}
			if raw != "" {
				break
			}
		}
	}
	if raw == "" {
		return "", primitive.NilObjectID, ErrInvalidProjectID
	}
	oid, err := primitive.ObjectIDFromHex(raw)
	if err != nil {
		return "", primitive.NilObjectID, ErrInvalidProjectID
	}
	return oid.Hex(), oid, nil
}

// projectMatch 同时匹配字符串与 ObjectID 两种写入形态（历史节点写法不一致）
func projectMatch(hex string, oid primitive.ObjectID) bson.M {
	return bson.M{"project": bson.M{"$in": bson.A{hex, oid}}}
}

// buildFq 把表头模糊搜索拼成 $and，未知 key 直接忽略
func buildFq(fq map[string]string, mapping map[string]string) bson.M {
	if len(fq) == 0 || len(mapping) == 0 {
		return nil
	}
	keys := make([]string, 0, len(fq))
	for k := range fq {
		keys = append(keys, k)
	}
	sort.Strings(keys) // 固定顺序，便于测试与 explain 对比

	conds := make([]interface{}, 0, len(keys))
	for _, k := range keys {
		value := strings.TrimSpace(fq[k])
		if value == "" {
			continue
		}
		field, ok := mapping[k]
		if !ok {
			continue
		}
		conds = append(conds, bson.M{field: bson.M{
			"$regex":   regexp.QuoteMeta(value),
			"$options": "i",
		}})
	}
	if len(conds) == 0 {
		return nil
	}
	return bson.M{"$and": conds}
}

// buildTimeMatch 时间范围。库里 time 是 "YYYY-MM-DD HH:MM:SS" 字符串，可直接字典序比较。
func buildTimeMatch(startRaw, endRaw string) (bson.M, error) {
	start, err := normalizeTime(startRaw, false)
	if err != nil {
		return nil, err
	}
	end, err := normalizeTime(endRaw, true)
	if err != nil {
		return nil, err
	}
	if start == "" && end == "" {
		return nil, nil
	}
	rng := bson.M{}
	if start != "" {
		rng["$gte"] = start
	}
	if end != "" {
		rng["$lte"] = end
	}
	return bson.M{"time": rng}, nil
}

// normalizeTime 接受 "YYYY-MM-DD" 与 "YYYY-MM-DD HH:MM:SS"（也容忍 T 分隔），
// 日期形态按整天边界补齐（start=00:00:00 / end=23:59:59）。
func normalizeTime(value string, endOfDay bool) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", value, time.Local); err == nil {
		return t.Format("2006-01-02 15:04:05"), nil
	}
	if t, err := time.ParseInLocation("2006-01-02T15:04:05", value, time.Local); err == nil {
		return t.Format("2006-01-02 15:04:05"), nil
	}
	if t, err := time.ParseInLocation("2006-01-02", value, time.Local); err == nil {
		if endOfDay {
			return t.Format("2006-01-02") + " 23:59:59", nil
		}
		return t.Format("2006-01-02") + " 00:00:00", nil
	}
	return "", ErrInvalidTimeRange
}

// normalizePage 分页归一：pageSize > MaxPageSize 直接报错（不允许一次拉全库）
func normalizePage(pageIndex, pageSize int) (int, int, error) {
	if pageSize > MaxPageSize {
		return 0, 0, ErrPageSizeTooLarge
	}
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	if pageIndex <= 0 {
		pageIndex = 1
	}
	return pageIndex, pageSize, nil
}
