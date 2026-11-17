package icp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	assetgroup "github.com/Autumn-27/ScopeSentry/internal/services/assets/assetgroup"
)

// 结果回填到资产组（地契）。
//
// 语义（与任务要求一致）：
//   - 只写 asset_scope.domain_array 与 scope_array；
//   - 去重：$addToSet 保证已有条目不重复、新条目追加；
//   - 不覆盖：绝不删除/替换已有条目，ip_array 等其它字段一个字节都不动。
//
// 目标地契按「groupId + 任务名」定位：存在就并进去，不存在就用任务名新建一个
// domain 类型地契（复用 asset_scope 服务，group 非法/不存在时由它返回参数错误）。

// ScopeMergeResult 一次地契合入的结果
type ScopeMergeResult struct {
	ScopeID   string
	ScopeName string
	GroupID   string
	Added     int64
	Total     int64
}

// ScopeWriter 资产组写入抽象（默认实现走 asset_scope 服务；测试可替换）
type ScopeWriter interface {
	MergeDomains(ctx context.Context, groupID, name string, domains []string) (ScopeMergeResult, error)
}

type assetScopeWriter struct {
	service assetgroup.Service
}

// NewAssetScopeWriter 默认实现：复用 asset_scope 服务
func NewAssetScopeWriter() ScopeWriter {
	return &assetScopeWriter{service: assetgroup.NewService()}
}

func (w *assetScopeWriter) MergeDomains(ctx context.Context, groupID, name string, domains []string) (ScopeMergeResult, error) {
	groupID = strings.TrimSpace(groupID)
	name = strings.TrimSpace(name)
	if len(domains) == 0 {
		return ScopeMergeResult{ScopeName: name, GroupID: groupID}, nil
	}

	scope, err := w.service.FindScopeByName(ctx, groupID, name)
	if err != nil {
		return ScopeMergeResult{}, err
	}
	if scope == nil {
		id, err := w.service.CreateScope(ctx, &models.AssetScopeAddRequest{
			Name:      name,
			Scope:     strings.Join(domains, "\n"),
			ScopeType: models.IcpScopeTypeDomain,
			GroupID:   groupID,
		})
		if err != nil {
			return ScopeMergeResult{}, err
		}
		return ScopeMergeResult{
			ScopeID:   id,
			ScopeName: name,
			GroupID:   groupID,
			Added:     int64(len(domains)),
			Total:     int64(len(domains)),
		}, nil
	}

	added, total, err := w.service.MergeScopeDomains(ctx, scope.ID.Hex(), domains)
	if err != nil {
		return ScopeMergeResult{}, err
	}
	return ScopeMergeResult{
		ScopeID:   scope.ID.Hex(),
		ScopeName: scope.Name,
		GroupID:   scope.GroupID,
		Added:     added,
		Total:     total,
	}, nil
}

// Sync 把任务结果里的域名并入资产组地契（幂等：重复调用 added=0）
func (s *Service) Sync(ctx context.Context, taskID, groupID string) (*SyncSummary, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("%w: taskId 不能为空", ErrInvalidInput)
	}
	task, err := s.tasks.Get(ctx, taskID)
	if err != nil {
		return nil, err
	}

	domains, err := s.results.Domains(ctx, taskID)
	if err != nil {
		return nil, err
	}

	scopeName := strings.TrimSpace(task.Name)
	if scopeName == "" {
		scopeName = "icp-" + taskID
	}
	summary := &SyncSummary{
		TaskID:    taskID,
		ScopeName: scopeName,
		GroupID:   strings.TrimSpace(groupID),
		Domains:   int64(len(domains)),
	}
	if len(domains) == 0 {
		s.appendLog(ctx, taskID, IcpLogLevelWarn, "同步跳过：任务没有可用域名")
		return summary, nil
	}

	merged, err := s.scopes.MergeDomains(ctx, groupID, scopeName, domains)
	if err != nil {
		s.appendLog(ctx, taskID, IcpLogLevelError, fmt.Sprintf("同步到资产组失败：%v", err))
		// 集团不存在 / 参数非法属于调用方问题，统一收敛成 400
		if errors.Is(err, assetgroup.ErrInvalidInput) ||
			errors.Is(err, assetgroup.ErrGroupNotFound) ||
			errors.Is(err, assetgroup.ErrScopeNotFound) {
			return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		return nil, fmt.Errorf("icp sync: %w", err)
	}

	summary.ScopeID = merged.ScopeID
	summary.ScopeName = merged.ScopeName
	summary.GroupID = merged.GroupID
	summary.Added = merged.Added
	summary.Total = merged.Total
	s.appendLog(ctx, taskID, IcpLogLevelInfo, fmt.Sprintf(
		"同步到资产组：scope=%s 域名=%d 新增=%d 当前合计=%d",
		merged.ScopeName, len(domains), merged.Added, merged.Total))
	return summary, nil
}
