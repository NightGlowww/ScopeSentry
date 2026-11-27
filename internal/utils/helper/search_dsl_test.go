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

// 数值区间比较（ARL-Next 1.5.2 对齐）：
//
//	field>=n / <=n / >n / <n  -> {"$gte|$lte|$gt|$lt": n}
//	port 在库里是字符串，必须走 $expr + $convert，否则永远命中 0 条
//	未知字段 + 比较符 -> 报错（以前静默丢弃 = 退化成全量查询）
func TestSearchToMongoDBNumericComparison(t *testing.T) {
	kw := map[string]string{"port": "port", "statuscode": "statuscode"}

	// statuscode 是数值字段：直接 $gte
	got, err := SearchToMongoDB("statuscode>=400", kw)
	if err != nil {
		t.Fatalf("statuscode>=400: %v", err)
	}
	b, _ := json.Marshal(got)
	if want := `[{"statuscode":{"$gte":400}}]`; string(b) != want {
		t.Fatalf("statuscode>=400:\n got %s\nwant %s", b, want)
	}

	// port 是字符串字段：必须是 $expr + $convert，且带 onError/onNull
	got, err = SearchToMongoDB("port>=1000", kw)
	if err != nil {
		t.Fatalf("port>=1000: %v", err)
	}
	b, _ = json.Marshal(got)
	if want := `[{"$expr":{"$gte":[{"$convert":{"input":"$port","onError":null,"onNull":null,"to":"int"}},1000]}}]`; string(b) != want {
		t.Fatalf("port>=1000:\n got %s\nwant %s", b, want)
	}

	// 未知字段：报错，而不是静默返回全量
	if _, err := SearchToMongoDB("nope>=1", kw); err == nil {
		t.Fatalf("unknown field comparison should fail")
	}

	// 值里带 < > 的普通等值查询不能被误判成比较：title=<script> 走 = 分支
	kw2 := map[string]string{"title": "title"}
	got, err = SearchToMongoDB("title=<script>", kw2)
	if err != nil {
		t.Fatalf("title=<script>: %v", err)
	}
	b, _ = json.Marshal(got)
	// encoding/json 会把 < > 转义成 \u003c / \u003e
	if want := "[{\"title\":{\"$options\":\"i\",\"$regex\":\"\\u003cscript\\u003e\"}}]"; string(b) != want {
		t.Fatalf("title=<script>:\n got %s\nwant %s", b, want)
	}
}

// 时间字段的范围比较走字符串（同格式字典序即时间序）。
// 这么做的意义：前端把「发现时间 >= X」写进搜索条件就能筛，
// 不用给每个列表接口单独加 timeFrom 参数（ARL-Next bc22a10 的 12 维口径）。
func TestSearchToMongoDBTimeComparison(t *testing.T) {
	kw := map[string]string{"time": "time"}

	got, err := SearchToMongoDB(`time>="2026-11-21 00:00:00"`, kw)
	if err != nil {
		t.Fatalf("time>=: %v", err)
	}
	b, _ := json.Marshal(got)
	if want := `[{"time":{"$gte":"2026-11-21 00:00:00"}}]`; string(b) != want {
		t.Fatalf("time>=\n got %s\nwant %s", b, want)
	}

	got, err = SearchToMongoDB(`time<="2026-11-27"`, kw)
	if err != nil {
		t.Fatalf("time<=: %v", err)
	}
	b, _ = json.Marshal(got)
	if want := `[{"time":{"$lte":"2026-11-27"}}]`; string(b) != want {
		t.Fatalf("time<=\n got %s\nwant %s", b, want)
	}

	// 非时间格式必须报错：把 abc 当时间比较会静默查不到数据
	if _, err := SearchToMongoDB(`time>="abc"`, kw); err == nil {
		t.Fatal("invalid time value should fail")
	}
}
