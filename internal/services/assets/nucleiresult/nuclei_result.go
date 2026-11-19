package nucleiresult

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	nucleirepo "github.com/Autumn-27/ScopeSentry/internal/repositories/assets/nucleiresult"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 分页上限：默认 20 条，单页最多 200（handler 的 binding 会把 >200 拒成 400）
const (
	defaultPageSize = 20
	maxPageSize     = 200
)

// 详情查询的哨兵错误：handler 据此区分 400（id 格式错）和 404（记录不存在）
var (
	ErrInvalidID = errors.New("nuclei result: invalid id")
	ErrNotFound  = errors.New("nuclei result: not found")
)

// Service Nuclei 结果只读视图服务接口（对标 ARL-Next assetNucleiResult）
type Service interface {
	List(ctx context.Context, req models.NucleiResultListRequest) (*models.NucleiResultListResult, error)
	GetDetail(ctx context.Context, id string) (*models.NucleiResultDetail, error)
	// UpdateStatus 更新处置状态
	UpdateStatus(ctx context.Context, id string, status int) error
}

type service struct {
	repo nucleirepo.Repository
}

// NewService 创建 Nuclei 结果读模型服务实例
func NewService() Service {
	return &service{repo: nucleirepo.NewRepository()}
}

// List 列表：project/severity/templateId/keyword 过滤 + time 倒序分页。
func (s *service) List(ctx context.Context, req models.NucleiResultListRequest) (*models.NucleiResultListResult, error) {
	pageIndex, pageSize := normalizePage(req.PageIndex, req.PageSize)

	items, total, err := s.repo.List(ctx, BuildListFilter(req), pageIndex, pageSize)
	if err != nil {
		return nil, fmt.Errorf("list nuclei results: %w", err)
	}
	return &models.NucleiResultListResult{List: items, Total: total}, nil
}

// GetDetail 单条完整记录。
func (s *service) GetDetail(ctx context.Context, id string) (*models.NucleiResultDetail, error) {
	oid, err := primitive.ObjectIDFromHex(strings.TrimSpace(id))
	if err != nil {
		return nil, ErrInvalidID
	}

	detail, err := s.repo.FindByID(ctx, oid)
	if err != nil {
		return nil, fmt.Errorf("get nuclei result %s: %w", id, err)
	}
	if detail == nil {
		return nil, ErrNotFound
	}
	return detail, nil
}

// BuildListFilter 拼列表过滤条件。
//
// 条件都落在 vulnerability 的现成字段上：
//   - project    精确
//   - severity   -> level，逗号分隔可传多个；原样值和小写值都用（扫描侧写的是小写，
//     前端偶尔传首字母大写），$in 仍然是索引等值查找
//   - templateId -> vulnid 子串（不区分大小写）；子串正则本身用不上索引边界，
//     所以它总是和 project/severity 这类索引条件一起出现时效果最好
//   - keyword    -> url / vulnname / rootDomain 子串（不区分大小写）
func BuildListFilter(req models.NucleiResultListRequest) bson.M {
	conds := bson.A{}

	if project := strings.TrimSpace(req.Project); project != "" {
		conds = append(conds, bson.M{"project": project})
	}

	if severity := strings.TrimSpace(req.Severity); severity != "" {
		if values := caseVariants(severity); len(values) > 0 {
			conds = append(conds, bson.M{"level": bson.M{"$in": values}})
		}
	}

	// 处置状态过滤：漏报/误报分流的入口
	if req.Status > 0 {
		conds = append(conds, bson.M{"status": req.Status})
	}

	if templateID := strings.TrimSpace(req.TemplateID); templateID != "" {
		conds = append(conds, bson.M{"vulnid": primitive.Regex{
			Pattern: regexp.QuoteMeta(templateID),
			Options: "i",
		}})
	}

	if keyword := strings.TrimSpace(req.Keyword); keyword != "" {
		pattern := primitive.Regex{Pattern: regexp.QuoteMeta(keyword), Options: "i"}
		conds = append(conds, bson.M{"$or": bson.A{
			bson.M{"url": pattern},
			bson.M{"vulnname": pattern},
			// 扫描侧落库是 "rootdomain"（Go 字段名小写），老数据是 "rootDomain"，
			// 关键词搜索两种拼写都要覆盖，否则按根域名搜不到新数据。
			bson.M{"rootdomain": pattern},
			bson.M{"rootDomain": pattern},
		}})
	}

	return composeAnd(conds)
}

// caseVariants 把逗号分隔的取值展开成「原样 + 小写」两种写法并去重。
// 例："High,critical" -> ["High","high","critical"]。
func caseVariants(raw string) bson.A {
	seen := make(map[string]struct{})
	values := bson.A{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		for _, v := range []string{part, strings.ToLower(part)} {
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			values = append(values, v)
		}
	}
	return values
}

// composeAnd 组合条件：空列表返回空文档（match all），单个条件直接返回，
// 多个条件才包 $and，避免生成 Mongo 不接受的空 $and 数组。
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

// normalizePage 归一化分页参数
func normalizePage(pageIndex, pageSize int) (int, int) {
	if pageIndex < 1 {
		pageIndex = 1
	}
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return pageIndex, pageSize
}

// UpdateStatus 更新一条漏洞结果的处置状态。
func (s *service) UpdateStatus(ctx context.Context, id string, status int) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("invalid id: %w", err)
	}
	if status < 1 || status > 6 {
		return fmt.Errorf("invalid status: %d", status)
	}
	return s.repo.UpdateStatus(ctx, oid, status)
}
