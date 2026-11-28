package github

// 黑名单仓库（ignoredRepos）真正生效的地方。
//
// 之前这个功能只存不用：接口能加、能列，采集时却从不查 —— 加了「忽略」照样往结果里灌
// 同一批噪声仓库（2026-11-29 审出）。这里把判定做成纯函数，采集链路与单元测试共用。

import (
	"context"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"go.uber.org/zap"
)

// matchIgnoredRepo 判定 repo 是否命中黑名单。
//
// 两种写法，够用且不引入通配引擎的复杂度：
//
//	owner/repo   精确匹配（大小写不敏感，GitHub 仓库名本身不区分大小写）
//	owner/*      该 owner 下全部仓库
//
// 其它形式（正则、中间通配）不认：认了就得解释「为什么这条没被忽略」，代价更高。
func matchIgnoredRepo(patterns []string, repo string) bool {
	if len(patterns) == 0 || repo == "" {
		return false
	}
	target := strings.ToLower(strings.TrimSpace(repo))
	for _, p := range patterns {
		pat := strings.ToLower(strings.TrimSpace(p))
		if pat == "" {
			continue
		}
		if pat == target {
			return true
		}
		if strings.HasSuffix(pat, "/*") {
			owner := strings.TrimSuffix(pat, "/*")
			if owner != "" && strings.HasPrefix(target, owner+"/") {
				return true
			}
		}
	}
	return false
}

// ignoredRepoPatterns 读取当前黑名单；读失败只记日志并按「没有黑名单」继续 ——
// 采集不该因为配置读取失败整轮停摆。
func (s *Service) ignoredRepoPatterns(ctx context.Context) []string {
	if s.threats == nil {
		return nil
	}
	items, err := s.threats.ListIgnoredRepos(ctx)
	if err != nil {
		logger.Warn("github intel: load ignored repos failed", zap.Error(err))
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		if v := strings.TrimSpace(it.Repo); v != "" {
			out = append(out, v)
		}
	}
	return out
}
