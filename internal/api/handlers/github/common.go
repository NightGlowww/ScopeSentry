package github

import (
	"fmt"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	githubsvc "github.com/Autumn-27/ScopeSentry/internal/services/github"
)

// GitHub 情报面 handler 公共件：服务单例、分页校验、ID 归一。

// svc 处理器共用服务实例
func svc() *githubsvc.Service { return githubsvc.DefaultService() }

// parsePageParams 分页参数归一：pageSize 上限 200，超过直接报错（handler 转 400）
func parsePageParams(pageIndex, pageSize int) (int, int, error) {
	if pageIndex <= 0 {
		pageIndex = 1
	}
	if pageSize <= 0 {
		pageSize = models.GithubDefaultPageSize
	}
	if pageSize > models.GithubMaxPageSize {
		return 0, 0, fmt.Errorf("pageSize 不能超过 %d", models.GithubMaxPageSize)
	}
	return pageIndex, pageSize, nil
}

// firstNonEmpty 取第一个非空值
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// normalizeIDs 去空、去重（保持顺序）
func normalizeIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// normalizeStatus 校验研判状态
func normalizeStatus(status string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case models.GithubResultStatusNew:
		return models.GithubResultStatusNew, nil
	case models.GithubResultStatusTriaged:
		return models.GithubResultStatusTriaged, nil
	case models.GithubResultStatusIgnored:
		return models.GithubResultStatusIgnored, nil
	default:
		return "", fmt.Errorf("不支持的研判状态: %s", status)
	}
}
