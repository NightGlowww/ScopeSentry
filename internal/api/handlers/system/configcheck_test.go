package system

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

// 自检端点必须自己先是对的：模板的真实结构是 Parameters.<模块> = {插件hash: 配置}，
// 扫描载荷也只从 Parameters 取。早先按顶层数组判定，得出「7 个模板全部不完整」的假警报；
// 后来加过「回退到顶层字段」，又会把「顶层镜像有值、Parameters 为空」判成已配置 ——
// 反向误报比误报更糟，所以现在只看 Parameters。
func TestModuleConfigured(t *testing.T) {
	params := bson.M{
		"SubdomainScan":     bson.M{"abc": bson.M{"dict": "x"}},
		"PortScan":          bson.M{},
		"DirScan":           bson.A{"hash1"},
		"VulnerabilityScan": nil,
	}

	cases := []struct {
		module string
		want   bool
	}{
		{"SubdomainScan", true},      // 非空映射（{插件hash: 配置}）
		{"DirScan", true},            // 非空数组
		{"PortScan", false},          // 空映射 = 没配
		{"VulnerabilityScan", false}, // nil
		{"AssetHandle", false},       // 根本不存在
	}
	for _, c := range cases {
		if got := moduleConfigured(params, c.module); got != c.want {
			t.Fatalf("moduleConfigured(%s) = %v, want %v", c.module, got, c.want)
		}
	}

	// Parameters 缺失时一律判没配：不能凭顶层字段说「已配置」
	if moduleConfigured(nil, "URLScan") {
		t.Fatal("没有 Parameters 时不应判定已配置")
	}
}
