package mcp

import "testing"

// filter 键与行字段不同名是真实踩过的坑（2026-11-29）：
// 拿 total 证明不了 rows 被过滤，两个工具的结果也没法按同一个键合并。
func TestAliasAssetRowFieldsAddsFilterKeyAliases(t *testing.T) {
	out := ginH{
		"list": []interface{}{
			map[string]interface{}{"domain": "a.example.com", "status": "200", "port": "443"},
			// 已有 host/statuscode 的行不能被覆盖
			map[string]interface{}{"domain": "b.example.com", "host": "keep.me", "status": "403", "statuscode": "403"},
			// 非 map 元素（脏数据）直接跳过，不能 panic
			"garbage",
		},
	}
	aliasAssetRowFields(out)
	rows := out["list"].([]interface{})
	r0 := rows[0].(map[string]interface{})
	if r0["host"] != "a.example.com" || r0["statuscode"] != "200" {
		t.Fatalf("别名没补上: %v", r0)
	}
	r1 := rows[1].(map[string]interface{})
	if r1["host"] != "keep.me" {
		t.Fatalf("已有 host 被覆盖: %v", r1)
	}
	if rows[2] != "garbage" {
		t.Fatalf("脏数据被改动: %v", rows[2])
	}
}

func TestAliasAssetRowFieldsNoList(t *testing.T) {
	out := ginH{"total": 1}
	aliasAssetRowFields(out) // 不能 panic
	if _, has := out["host"]; has {
		t.Fatalf("没有 list 时不应写入任何字段: %v", out)
	}
}
