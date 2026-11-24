package vulnverify

import (
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// multiResponseRef 命中 body_2..N / status_code_2..N：这类判据依赖前面请求的响应，
// 而落库只保存最后一条响应，物理上无法复验。
var multiResponseRef = regexp.MustCompile(`(body|status_code)_([2-9]|[1-9][0-9])`)

// UnverifiableTemplate 判断模板是否「只能靠多请求响应判定」。
// 只有所有带 matcher 的请求块都依赖 body_N/status_code_N 时才返回 true；
// 只要有一个块能在单响应上判定，就仍然可复验，不能挡。
func UnverifiableTemplate(content string) bool {
	var t template
	if err := yaml.Unmarshal([]byte(content), &t); err != nil {
		return false
	}
	blocks := t.blocks()
	if len(blocks) == 0 {
		// 解析不出请求块不等于「不可复验」：宁可放过，也不能把能跑的模板挡在门外。
		return false
	}
	decidableBlocks := 0
	blocksWithMatchers := 0
	for _, blk := range blocks {
		if len(blk.Matchers) == 0 {
			continue
		}
		blocksWithMatchers++
		if !blockNeedsMultiResponse(blk.Matchers) {
			decidableBlocks++
		}
	}
	if blocksWithMatchers == 0 {
		// 没有任何 matcher（只有 extractor/flow）的模板不是「不可复验」，是「没有判据」。
		// 2026-11-24 实测：不加这条会把 Backdrop/Memos 这类纯 extractor 模板误排 203 个。
		return false
	}
	return decidableBlocks == 0
}

func blockNeedsMultiResponse(ms []rawMatcher) bool {
	hasDSL := false
	for _, m := range ms {
		if strings.EqualFold(m.Type, "dsl") {
			hasDSL = true
			for _, e := range m.DSL {
				if !multiResponseRef.MatchString(e) {
					return false
				}
			}
		}
	}
	// 含非 dsl 判据（word/regex/status）的块，本身就能在单响应上判定。
	return hasDSL
}
