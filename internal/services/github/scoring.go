package github

import "strings"

// severityRule 严重度判定规则：命中越靠前越严重，全部规则都不命中时按路径特征降级。
type severityRule struct {
	severity string
	tokens   []string
}

var severityRules = []severityRule{
	{
		severity: "critical",
		tokens: []string{
			"aws_secret_access_key", "private_key", "begin rsa private", "begin openssh private",
			"xoxb-", "xoxp-", "ghp_", "github_pat_", "sk_live_",
		},
	},
	{
		severity: "high",
		tokens: []string{
			"password", "passwd", "pwd=", "secret", "api_key", "apikey", "access_key",
			"secret_key", "authorization:", "bearer ", "mongodb://", "mysql://",
			"postgres://", "redis://", "jdbc:", "smtp_pass", "private token",
		},
	},
	{
		severity: "medium",
		tokens: []string{
			"token", "internal", "staging", "admin", ".env", "config", "credential", "auth",
		},
	},
}

// ScoreSeverity 按 repo/path/snippet 的文本特征给出确定性的严重度（critical/high/medium/low）。
//
// 纯函数：同样输入永远同样输出，便于断言与回放。
func ScoreSeverity(repo, path, snippet string) string {
	haystack := strings.ToLower(strings.Join([]string{repo, path, snippet}, "\n"))
	for _, rule := range severityRules {
		for _, token := range rule.tokens {
			if strings.Contains(haystack, token) {
				return rule.severity
			}
		}
	}
	lowerPath := strings.ToLower(path)
	if strings.HasSuffix(lowerPath, ".env") || strings.HasSuffix(lowerPath, ".pem") ||
		strings.HasSuffix(lowerPath, ".key") || strings.HasSuffix(lowerPath, ".yml") ||
		strings.HasSuffix(lowerPath, ".yaml") || strings.HasSuffix(lowerPath, ".json") {
		return "medium"
	}
	return "low"
}
