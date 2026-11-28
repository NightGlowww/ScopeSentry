package github

// 噪声仓库黑名单的服务层入口：REST handler 与 MCP 工具共用同一段逻辑。
// 之前这段只有 handler 里有，MCP 想调只能复制一遍 —— 复制出来的第二份迟早和第一份不一致。

import (
	"context"
	"errors"
	"strings"
)

// ErrInvalidAction 黑名单操作名不合法
var ErrInvalidAction = errors.New("invalid action")

// IgnoredRepos 黑名单管理：action = list | add | remove。
// purge 只对 add 有意义：把已入库的同仓库结果一起清掉（不可恢复，调用方需显式要求）。
func (s *Service) IgnoredRepos(ctx context.Context, action, repo, reason string, purge bool) (map[string]any, error) {
	action = strings.TrimSpace(strings.ToLower(action))
	repo = strings.TrimSpace(repo)
	if action == "" {
		action = "list"
	}
	switch action {
	case "list":
		items, err := s.threats.ListIgnoredRepos(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": items, "total": len(items)}, nil
	case "add":
		if repo == "" {
			return nil, errors.New("repo is required")
		}
		added, err := s.threats.AddIgnoredRepo(ctx, repo, reason)
		if err != nil {
			return nil, err
		}
		var removed int64
		if purge {
			removed, err = s.results.DeleteByRepo(ctx, repo)
			if err != nil {
				return nil, err
			}
		}
		return map[string]any{"repo": repo, "added": added, "removed": removed, "purged": purge}, nil
	case "remove":
		if repo == "" {
			return nil, errors.New("repo is required")
		}
		removed, err := s.threats.RemoveIgnoredRepo(ctx, repo)
		if err != nil {
			return nil, err
		}
		return map[string]any{"repo": repo, "removed": removed}, nil
	default:
		return nil, ErrInvalidAction
	}
}
