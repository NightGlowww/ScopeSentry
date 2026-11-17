package assetgroup

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	repo "github.com/Autumn-27/ScopeSentry/internal/repositories/assets/assetgroup"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

var (
	// ErrGroupExists 集团名称重复
	ErrGroupExists = errors.New("asset group name already exists")
	// ErrGroupNotFound 集团不存在
	ErrGroupNotFound = errors.New("asset group not found")
	// ErrScopeNotFound 地契不存在
	ErrScopeNotFound = errors.New("asset scope not found")
	// ErrInvalidInput 参数非法
	ErrInvalidInput = errors.New("invalid input")
)

// 允许的资产范围类型，空值表示自动推断
const (
	ScopeTypeDomain = "domain"
	ScopeTypeIP     = "ip"
	ScopeTypeMixed  = "mixed"
)

var domainPattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)+$`)

// Service 资产集团 / 地契业务层
type Service interface {
	ListGroups(ctx context.Context) ([]models.AssetGroup, error)
	CreateGroup(ctx context.Context, req *models.AssetGroupAddRequest) (string, error)
	UpdateGroup(ctx context.Context, req *models.AssetGroupEditRequest) error
	DeleteGroup(ctx context.Context, req *models.AssetGroupDeleteRequest) error
	ReorderGroups(ctx context.Context, ids []string) error

	ListScopes(ctx context.Context, groupID, search string, pageIndex, pageSize int) ([]models.AssetScope, int64, error)
	CreateScope(ctx context.Context, req *models.AssetScopeAddRequest) (string, error)
	UpdateScope(ctx context.Context, req *models.AssetScopeUpdateRequest) error
	DeleteScopes(ctx context.Context, ids []string) error
	MoveScopes(ctx context.Context, ids []string, groupID string) error
	ClearUpdateErrors(ctx context.Context, ids []string) error
	BindEnterprise(ctx context.Context, id, enterprise string) error
}

type service struct {
	repo repo.Repository
}

// NewService 创建服务实例
func NewService() Service {
	return &service{repo: repo.NewRepository()}
}

// ListGroups 返回集团列表并填充 scopeCount
func (s *service) ListGroups(ctx context.Context) ([]models.AssetGroup, error) {
	groups, err := s.repo.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	counts, err := s.repo.GroupScopeCounts(ctx)
	if err != nil {
		return nil, err
	}
	for i := range groups {
		groups[i].ScopeCount = counts[groups[i].ID.Hex()]
	}
	return groups, nil
}

// CreateGroup 新建集团（名称全局唯一）
func (s *service) CreateGroup(ctx context.Context, req *models.AssetGroupAddRequest) (string, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > 64 {
		return "", fmt.Errorf("%w: group name must be 1-64 characters", ErrInvalidInput)
	}
	existing, err := s.repo.FindGroupByName(ctx, name)
	if err != nil {
		return "", err
	}
	if existing != nil {
		return "", ErrGroupExists
	}
	return s.repo.CreateGroup(ctx, &models.AssetGroup{
		Name:        name,
		Description: strings.TrimSpace(req.Description),
		SortOrder:   req.SortOrder,
	})
}

// UpdateGroup 编辑集团
func (s *service) UpdateGroup(ctx context.Context, req *models.AssetGroupEditRequest) error {
	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > 64 {
		return fmt.Errorf("%w: group name must be 1-64 characters", ErrInvalidInput)
	}
	current, err := s.repo.GetGroup(ctx, req.ID)
	if err != nil {
		return err
	}
	if current == nil {
		return ErrGroupNotFound
	}
	if current.Name != name {
		same, err := s.repo.FindGroupByName(ctx, name)
		if err != nil {
			return err
		}
		if same != nil {
			return ErrGroupExists
		}
	}
	current.Name = name
	current.Description = strings.TrimSpace(req.Description)
	current.SortOrder = req.SortOrder
	return s.repo.UpdateGroup(ctx, current)
}

// DeleteGroup 删除集团：dissolve 移入未分组，transfer 划转到目标集团
func (s *service) DeleteGroup(ctx context.Context, req *models.AssetGroupDeleteRequest) error {
	group, err := s.repo.GetGroup(ctx, req.ID)
	if err != nil {
		return err
	}
	if group == nil {
		return ErrGroupNotFound
	}
	action := strings.TrimSpace(req.Action)
	if action == "" {
		action = "dissolve"
	}
	switch action {
	case "dissolve":
		if err := s.repo.DissolveGroup(ctx, req.ID); err != nil {
			return err
		}
	case "transfer":
		if req.TargetGroupID == "" || req.TargetGroupID == req.ID {
			return fmt.Errorf("%w: target group is required for transfer", ErrInvalidInput)
		}
		target, err := s.repo.GetGroup(ctx, req.TargetGroupID)
		if err != nil {
			return err
		}
		if target == nil {
			return ErrGroupNotFound
		}
		if err := s.repo.TransferGroup(ctx, req.ID, req.TargetGroupID); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: unsupported action %q", ErrInvalidInput, action)
	}
	return s.repo.DeleteGroup(ctx, req.ID)
}

// ReorderGroups 重排
func (s *service) ReorderGroups(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return fmt.Errorf("%w: orderIds is required", ErrInvalidInput)
	}
	return s.repo.ReorderGroups(ctx, ids)
}

// ListScopes 分页查询；groupID 支持 unassigned
func (s *service) ListScopes(ctx context.Context, groupID, search string, pageIndex, pageSize int) ([]models.AssetScope, int64, error) {
	filter := bson.M{}
	switch strings.TrimSpace(groupID) {
	case "":
	case "unassigned":
		filter["group_id"] = bson.M{"$in": []string{"", "0"}}
	default:
		filter["group_id"] = groupID
	}
	if search = strings.TrimSpace(search); search != "" {
		filter["$or"] = []bson.M{
			{"name": bson.M{"$regex": regexp.QuoteMeta(search), "$options": "i"}},
			{"scope": bson.M{"$regex": regexp.QuoteMeta(search), "$options": "i"}},
		}
	}
	return s.repo.ListScopes(ctx, filter, pageIndex, pageSize)
}

// CreateScope 新建地契并解析范围
func (s *service) CreateScope(ctx context.Context, req *models.AssetScopeAddRequest) (string, error) {
	scope, err := s.buildScope(req.Name, req.Scope, req.ScopeType, req.GroupID)
	if err != nil {
		return "", err
	}
	if err := s.ensureGroup(ctx, req.GroupID); err != nil {
		return "", err
	}
	return s.repo.CreateScope(ctx, scope)
}

// UpdateScope 编辑地契
func (s *service) UpdateScope(ctx context.Context, req *models.AssetScopeUpdateRequest) error {
	current, err := s.repo.GetScope(ctx, req.ID)
	if err != nil {
		return err
	}
	if current == nil {
		return ErrScopeNotFound
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = current.Name
	}
	scopeText := req.Scope
	if strings.TrimSpace(scopeText) == "" {
		scopeText = current.Scope
	}
	groupID := req.GroupID
	if groupID == "" {
		groupID = current.GroupID
	}
	updated, err := s.buildScope(name, scopeText, req.ScopeType, groupID)
	if err != nil {
		return err
	}
	if err := s.ensureGroup(ctx, groupID); err != nil {
		return err
	}
	updated.ID = current.ID
	return s.repo.UpdateScope(ctx, updated)
}

func (s *service) DeleteScopes(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return fmt.Errorf("%w: ids is required", ErrInvalidInput)
	}
	return s.repo.DeleteScopes(ctx, ids)
}

func (s *service) MoveScopes(ctx context.Context, ids []string, groupID string) error {
	if len(ids) == 0 {
		return fmt.Errorf("%w: ids is required", ErrInvalidInput)
	}
	if err := s.ensureGroup(ctx, groupID); err != nil {
		return err
	}
	return s.repo.MoveScopes(ctx, ids, groupID)
}

func (s *service) ClearUpdateErrors(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return fmt.Errorf("%w: ids is required", ErrInvalidInput)
	}
	return s.repo.ClearUpdateErrors(ctx, ids)
}

func (s *service) BindEnterprise(ctx context.Context, id, enterprise string) error {
	current, err := s.repo.GetScope(ctx, id)
	if err != nil {
		return err
	}
	if current == nil {
		return ErrScopeNotFound
	}
	return s.repo.BindEnterprise(ctx, id, strings.TrimSpace(enterprise))
}

func (s *service) ensureGroup(ctx context.Context, groupID string) error {
	if strings.TrimSpace(groupID) == "" {
		return nil
	}
	if _, err := primitive.ObjectIDFromHex(groupID); err != nil {
		return fmt.Errorf("%w: invalid group id", ErrInvalidInput)
	}
	group, err := s.repo.GetGroup(ctx, groupID)
	if err != nil {
		return err
	}
	if group == nil {
		return ErrGroupNotFound
	}
	return nil
}

func (s *service) buildScope(name, scopeText, scopeType, groupID string) (*models.AssetScope, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 128 {
		return nil, fmt.Errorf("%w: scope name must be 1-128 characters", ErrInvalidInput)
	}
	entries := splitScopeText(scopeText)
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: scope must contain at least one domain or ip", ErrInvalidInput)
	}
	domains := make([]string, 0, len(entries))
	ips := make([]string, 0, len(entries))
	for _, e := range entries {
		if net.ParseIP(e) != nil {
			ips = append(ips, e)
			continue
		}
		if domainPattern.MatchString(e) {
			domains = append(domains, strings.ToLower(e))
			continue
		}
		return nil, fmt.Errorf("%w: %q is not a valid domain or ip", ErrInvalidInput, e)
	}
	inferred := ScopeTypeMixed
	switch {
	case len(domains) > 0 && len(ips) == 0:
		inferred = ScopeTypeDomain
	case len(domains) == 0 && len(ips) > 0:
		inferred = ScopeTypeIP
	}
	if t := strings.TrimSpace(scopeType); t != "" {
		switch t {
		case ScopeTypeDomain, ScopeTypeIP, ScopeTypeMixed:
			inferred = t
		default:
			return nil, fmt.Errorf("%w: unsupported scopeType %q", ErrInvalidInput, t)
		}
	}
	return &models.AssetScope{
		Name:        name,
		Scope:       strings.TrimSpace(scopeText),
		ScopeType:   inferred,
		GroupID:     strings.TrimSpace(groupID),
		ScopeArray:  append(append([]string{}, domains...), ips...),
		DomainArray: domains,
		IPArray:     ips,
	}, nil
}

func splitScopeText(text string) []string {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return r == '\n' || r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\r'
	})
	seen := make(map[string]struct{}, len(fields))
	result := make([]string, 0, len(fields))
	for _, f := range fields {
		v := strings.TrimSpace(strings.TrimSuffix(f, "."))
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		result = append(result, v)
	}
	return result
}
