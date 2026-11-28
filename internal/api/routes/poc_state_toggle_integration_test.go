//go:build integration

package routes

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 模板停用/启用：以前只有「删」没有「停」。停用必须真的影响扫描载荷（见
// services/task/common 的用例），这里先钉住 REST 这一层：落库、读回、拒绝分支。
func TestPocStateToggleRoundTrip(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	pocs := mongodb.DB.Collection("PocList")
	tplID := fmt.Sprintf("it-poc-state-%d", time.Now().UnixNano())
	res, err := pocs.InsertOne(t.Context(), bson.M{
		"name": "it poc state", "id": tplID, "level": "info", "content": "id: " + tplID, "time": "2026-11-29 00:00:00",
	})
	if err != nil {
		t.Fatalf("seed poc: %v", err)
	}
	id := res.InsertedID.(primitive.ObjectID).Hex()
	t.Cleanup(func() { _, _ = pocs.DeleteMany(t.Context(), bson.M{"id": tplID}) })

	readDisabled := func() bool {
		w := postJSON(t, router, token, "/api/poc", map[string]interface{}{"search": tplID, "pageIndex": 1, "pageSize": 5})
		if w.Code != http.StatusOK {
			t.Fatalf("poc list = %d: %s", w.Code, w.Body.String())
		}
		var data struct {
			List []struct {
				ID       string `json:"id"`
				Disabled bool   `json:"disabled"`
			} `json:"list"`
		}
		decodeData(t, w, &data)
		for _, row := range data.List {
			if row.ID == id {
				return row.Disabled
			}
		}
		t.Fatalf("列表里找不到刚插入的模板 %s", id)
		return false
	}
	if readDisabled() {
		t.Fatalf("新建模板默认应为启用")
	}

	w := postJSON(t, router, token, "/api/poc/state", map[string]interface{}{"ids": []string{id}, "disabled": true})
	if w.Code != http.StatusOK {
		t.Fatalf("停用 = %d: %s", w.Code, w.Body.String())
	}
	var st struct {
		Updated int64 `json:"updated"`
	}
	decodeData(t, w, &st)
	if st.Updated != 1 {
		t.Fatalf("停用 updated = %d, want 1", st.Updated)
	}
	if !readDisabled() {
		t.Fatalf("停用没落库")
	}

	w = postJSON(t, router, token, "/api/poc/state", map[string]interface{}{"ids": []string{id}, "disabled": false})
	if w.Code != http.StatusOK {
		t.Fatalf("启用 = %d: %s", w.Code, w.Body.String())
	}
	if readDisabled() {
		t.Fatalf("启用没落库")
	}

	// 拒绝分支：空 ids / 非法 id / 不存在的 id
	w = postJSON(t, router, token, "/api/poc/state", map[string]interface{}{"ids": []string{}, "disabled": true})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("空 ids = %d，期望 400", w.Code)
	}
	w = postJSON(t, router, token, "/api/poc/state", map[string]interface{}{"ids": []string{"zzz"}, "disabled": true})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法 id = %d，期望 400: %s", w.Code, w.Body.String())
	}
	w = postJSON(t, router, token, "/api/poc/state", map[string]interface{}{"ids": []string{"0123456789abcdef01234567"}, "disabled": true})
	if w.Code != http.StatusNotFound {
		t.Fatalf("不存在的 id = %d，期望 404: %s", w.Code, w.Body.String())
	}
}