package system

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

// 自检端点必须自己先是对的：模板的真实结构是 Parameters.<模块> = {插件hash: 配置}，
// 早先按顶层数组判定，导致「7 个模板全部不完整」这种假警报。
func TestModuleConfigured(t *testing.T) {
	params := bson.M{
		"SubdomainScan":     bson.M{"abc": bson.M{"dict": "x"}},
		"PortScan":          bson.M{},
		"DirScan":           bson.A{"hash1"},
		"VulnerabilityScan": nil,
	}
	doc := bson.M{"Parameters": params, "WebCrawler": bson.A{"hash2"}}

	cases := []struct {
		module string
		want   bool
	}{
		{"SubdomainScan", true},      // Parameters 下非空映射
		{"DirScan", true},            // Parameters 下非空数组
		{"WebCrawler", true},         // 兼容顶层数组（老文档）
		{"PortScan", false},          // 空映射 = 没配
		{"VulnerabilityScan", false}, // nil
		{"AssetHandle", false},       // 根本不存在
	}
	for _, c := range cases {
		if got := moduleConfigured(doc, params, c.module); got != c.want {
			t.Fatalf("moduleConfigured(%s) = %v, want %v", c.module, got, c.want)
		}
	}

	// Parameters 缺失时只看顶层，不能因此把所有模板判成不完整
	if !moduleConfigured(bson.M{"URLScan": bson.A{"h"}}, nil, "URLScan") {
		t.Fatal("没有 Parameters 时应回退到顶层字段")
	}
}
