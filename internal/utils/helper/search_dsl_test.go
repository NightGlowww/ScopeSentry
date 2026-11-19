package helper

import (
	"encoding/json"
	"testing"
)

// 搜索 DSL 是 UI 搜索框与 MCP list_assets 共用的入口，此前没有测试。
// 语义（实测钉住）：
//
//	field=value      -> {"field":{"$regex":value,"$options":"i"}}（模糊、忽略大小写）
//	field=="value"   -> {"field":{"$eq":value}}（精确；数字串转数字）
//	field!=value     -> {"field":{"$not":{"$regex":value,"$options":"i"}}}
//	a && b           -> [{"$and":[a,b]}]
//	a || b           -> [{"$or":[a,b]}]
func TestSearchToMongoDBSemantics(t *testing.T) {
	cases := []struct {
		expr string
		want string
	}{
		{"domain=example", `[{"host":{"$options":"i","$regex":"example"}}]`},
		{"domain==\"example.com\"", `[{"host":{"$eq":"example.com"}}]`},
		{"domain!=a.com", `[{"host":{"$not":{"$options":"i","$regex":"a.com"}}}]`},
		{"domain=a && statuscode==200", `[{"$and":[{"host":{"$options":"i","$regex":"a"}},{"statuscode":{"$eq":200}}]}]`},
		{"domain=a || domain=b", `[{"$or":[{"host":{"$options":"i","$regex":"a"}},{"host":{"$options":"i","$regex":"b"}}]}]`},
	}
	for _, c := range cases {
		// 真实调用方（SearchKey[index]）会把 DSL 字段名映射到库字段；未映射的字段会被忽略
		kw := map[string]string{"domain": "host", "statuscode": "statuscode"}
		got, err := SearchToMongoDB(c.expr, kw)
		if err != nil {
			t.Fatalf("%s: error %v", c.expr, err)
		}
		b, _ := json.Marshal(got)
		if string(b) != c.want {
			t.Fatalf("%s:\n got %s\nwant %s", c.expr, b, c.want)
		}
	}
}

func TestSearchToMongoDBEmpty(t *testing.T) {
	got, err := SearchToMongoDB("", map[string]string{})
	if err != nil || len(got) != 1 || len(got[0]) != 0 {
		t.Fatalf("empty expr => %v, %v; want [{}]", got, err)
	}
}
