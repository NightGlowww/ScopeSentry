package i18n

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// response.X(c, "api.xxx", nil) 这类调用会把 err 置空，于是 data 为空，
// 前端 axios 拦截器只能显示 message。message 是 i18n.Translate 的结果：
// bundle 里没有这个 key 时 go-i18n 原样返回 key 本身 —— 用户就会看到一串
// "api.no_files_selected"（2026-11-28 全量扫描发现 19 个这样的键）。
//
// 这个测试锁住这一点：**nil 错误分支用到的 api.* 键必须两个语言文件里都有**。
func TestNilErrApiKeysAreTranslated(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	callRe := regexp.MustCompile(`response\.\w+\(\s*c\s*,\s*"(api\.[A-Za-z0-9_.]+)"\s*(?:,\s*(nil|\w+))?\s*\)`)
	used := map[string][]string{} // key -> 调用点
	err = filepath.Walk(filepath.Join(root, "internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		for i, line := range strings.Split(string(body), "\n") {
			for _, m := range callRe.FindAllStringSubmatch(line, -1) {
				key, arg := m[1], m[2]
				if arg == "" || arg == "nil" {
					rel, _ := filepath.Rel(root, path)
					used[key] = append(used[key], rel+":"+itoa(i+1))
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal: %v", err)
	}
	if len(used) == 0 {
		t.Fatal("没扫到任何 nil 错误分支的 api.* 调用，正则或目录结构变了")
	}

	for _, localeFile := range []string{"locales/zh-CN.json", "locales/en-US.json"} {
		data, rerr := os.ReadFile(localeFile)
		if rerr != nil {
			t.Fatalf("read %s: %v", localeFile, rerr)
		}
		var bundle map[string]string
		if jerr := json.Unmarshal(data, &bundle); jerr != nil {
			t.Fatalf("parse %s: %v", localeFile, jerr)
		}
		for key, sites := range used {
			if _, ok := bundle[key]; !ok {
				t.Errorf("%s 缺少 %q（data 为空时用户会看到裸 key 串）；调用点: %s", localeFile, key, strings.Join(sites, ", "))
			}
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}