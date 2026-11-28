package github

// 测试专用出口：黑名单读取与命中判定是内部实现，集成用例要能从 github_test 包验证
// 「配置真的被读到了」。导出范围只到测试需要的两个函数，不扩大公开 API。

import "context"

func IgnoredRepoPatternsForTest(s *Service) []string {
	return s.ignoredRepoPatterns(context.Background())
}

func MatchIgnoredRepoForTest(patterns []string, repo string) bool {
	return matchIgnoredRepo(patterns, repo)
}
