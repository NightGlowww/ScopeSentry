//go:build integration

package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"go.mongodb.org/mongo-driver/bson"
)

// 系统配置 / 通知配置走的是「name-value 全量覆盖写」：
// 假成功的形态不是报错，而是**悄悄只 insert 不 update**——老值还在，新值读不到，
// 或者反过来留下两条同名记录，界面上永远显示第一次写的那条。
// 这里用测试专用 key 做两轮写入，验证是真的 upsert。
func TestSystemConfigUpsertRoundTrip(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	do := func(method, path string, body interface{}) *httptest.ResponseRecorder {
		var payload []byte
		if body != nil {
			payload, _ = json.Marshal(body)
		}
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)
		return w
	}
	read := func() map[string]interface{} {
		w := do(http.MethodGet, "/api/configuration/system/data", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("system data = %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Data map[string]interface{} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode system data: %v", err)
		}
		return resp.Data
	}

	const key = "it_probe_round84"
	ctx := context.Background()
	cleanup := func() {
		if mongodb.DB != nil {
			_, _ = mongodb.DB.Collection("config").DeleteMany(ctx, bson.M{"type": "system", "name": key})
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	for _, want := range []string{"v1", "v2"} {
		w := do(http.MethodPost, "/api/configuration/system/save", map[string]interface{}{key: want})
		if w.Code != http.StatusOK {
			t.Fatalf("save system(%s) = %d: %s", want, w.Code, w.Body.String())
		}
		got, _ := read()[key].(string)
		if got != want {
			t.Fatalf("写入 %q 后读回 %q（不是 upsert，或根本没落库）", want, got)
		}
	}

	// 同名记录只能有一条：两条的话界面会随机显示其中一条
	if mongodb.DB != nil {
		n, err := mongodb.DB.Collection("config").CountDocuments(ctx, bson.M{"type": "system", "name": key})
		if err != nil {
			t.Fatalf("count: %v", err)
		}
		if n != 1 {
			t.Fatalf("同名配置有 %d 条，应为 1（upsert 写成了 insert）", n)
		}
	}
}
