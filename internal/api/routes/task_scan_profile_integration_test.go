//go:build integration

package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"go.mongodb.org/mongo-driver/bson"
)

// 扫描档位（scanProfile=recon）：任务入库时要把档位归一化并落一条可见提示，
// 否则用户只看到「任务跑完了但没有漏洞/目录结果」，又会当成 bug 来查。
//
// 这里只验 REST 可见的部分（落库值 + policyWarnings）；模板裁剪本身在
// services/task/scanprofile 的单元测试里覆盖（纯函数，不依赖 DB）。
func TestTaskScanProfileReconPersisted(t *testing.T) {
	router := SetupRouter()
	token := seedUserAndLogin(t, router)

	do := func(method, path string, body interface{}) *httptest.ResponseRecorder {
		t.Helper()
		payload, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)
		return w
	}

	// 造一个最小模板（nuclei 与目录扫描都在，方便对比「档位裁剪前后的差异」）
	tplBody := map[string]interface{}{
		"name":              "it-scan-profile-" + strings.ReplaceAll(t.Name(), "/", "-"),
		"SubdomainScan":     []string{"sub"},
		"PortScan":          []string{"port"},
		"PortFingerprint":   []string{"fp"},
		"AssetMapping":      []string{"map"},
		"AssetHandle":       []string{"handle"},
		"URLScan":           []string{"url"},
		"WebCrawler":        []string{"crawler"},
		"URLSecurity":       []string{"sec"},
		"DirScan":           []string{"dir"},
		"VulnerabilityScan": []string{"vul"},
	}
	tw := do(http.MethodPost, "/api/task/template/save", map[string]interface{}{"id": "", "result": tplBody})
	if tw.Code != http.StatusOK {
		t.Fatalf("建模板 = %d: %s", tw.Code, tw.Body.String())
	}
	var tpls struct {
		List []struct {
			ID   string `bson:"id" json:"id"`
			Name string `bson:"name" json:"name"`
		} `json:"list"`
	}
	_ = tpls
	tl := do(http.MethodPost, "/api/task/template", map[string]interface{}{"pageIndex": 1, "pageSize": 50})
	var tplList struct {
		Data struct {
			List []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(tl.Body.Bytes(), &tplList); err != nil {
		t.Fatalf("decode template list: %v", err)
	}
	name := tplBody["name"].(string)
	tplID := ""
	for _, it := range tplList.Data.List {
		if it.Name == name {
			tplID = it.ID
		}
	}
	if tplID == "" {
		t.Fatalf("模板没建出来: %s", tl.Body.String())
	}
	t.Cleanup(func() {
		do(http.MethodPost, "/api/task/template/delete", map[string]interface{}{"ids": []string{tplID}})
	})

	taskName := "it-scan-profile-task-" + strings.ReplaceAll(t.Name(), "/", "-")
	aw := do(http.MethodPost, "/api/task/add", map[string]interface{}{
		"name":         taskName,
		"target":       "example.invalid",
		"node":         []string{"node-it"},
		"template":     tplID,
		"targetSource": "general",
		"scanProfile":  "recon",
	})
	if aw.Code != http.StatusOK {
		t.Fatalf("建任务 = %d: %s", aw.Code, aw.Body.String())
	}

	tasks := mongodb.DB.Collection("task")
	t.Cleanup(func() { _, _ = tasks.DeleteMany(t.Context(), bson.M{"name": taskName}) })
	var doc struct {
		ScanProfile    string   `bson:"scanProfile"`
		PolicyWarnings []string `bson:"policyWarnings"`
	}
	if err := tasks.FindOne(t.Context(), bson.M{"name": taskName}).Decode(&doc); err != nil {
		t.Fatalf("读任务失败: %v", err)
	}
	if doc.ScanProfile != "recon" {
		t.Fatalf("scanProfile = %q, want recon", doc.ScanProfile)
	}
	joined := strings.Join(doc.PolicyWarnings, " | ")
	if !strings.Contains(joined, "scan_profile=recon") || !strings.Contains(joined, "DirScan") {
		t.Fatalf("policyWarnings 缺少档位说明: %v", doc.PolicyWarnings)
	}
}
