// Package failreason 把历史的中文失败原因还原成可翻译的 code + params。
//
// 背景：任务失败原因由巡检直接拼中文句子写进 failReason；后来补了 failCode/failParams
// 让英文界面能按语言渲染。老数据只有句子，这里按已知句式做一次还原。
// 认不出的句子原样保留 —— 解析器只做确定性映射，不猜。
package failreason

import (
	"regexp"
	"strconv"
	"strings"
)

var patterns = []struct {
	re     *regexp.Regexp
	code   string
	fields []string
}{
	{
		re:     regexp.MustCompile(`任务标记为运行中，但没有任何节点在跑、也不在队列里，且 (.+?) 没有进度写入`),
		code:   "phantomNoProgress",
		fields: []string{"since"},
	},
	{
		re:     regexp.MustCompile(`节点 (.+?) 心跳停在 (.+?)（超过 (.+?) 未更新`),
		code:   "nodeHeartbeatStale",
		fields: []string{"node", "at", "after"},
	},
	{
		re:     regexp.MustCompile(`超过 (.+?) 无进展，且不在任何节点队列里（最后活动 (.+?)）`),
		code:   "staleNoProgress",
		fields: []string{"after", "last"},
	},
}

var autoResumeRe = regexp.MustCompile(`已自动重投 (\d+) 次`)

// Parse 输入历史 failReason，返回 code 与参数；认不出时 code 为空。
func Parse(reason string) (string, map[string]string) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "", nil
	}
	for _, p := range patterns {
		m := p.re.FindStringSubmatch(reason)
		if m == nil {
			continue
		}
		params := make(map[string]string, len(p.fields)+1)
		for i, name := range p.fields {
			params[name] = strings.TrimSpace(m[i+1])
		}
		if p.code == "staleNoProgress" {
			n := "0"
			if am := autoResumeRe.FindStringSubmatch(reason); am != nil {
				n = am[1]
			}
			if _, err := strconv.Atoi(n); err != nil {
				n = "0"
			}
			params["autoResume"] = n
		}
		return p.code, params
	}
	return "", nil
}
