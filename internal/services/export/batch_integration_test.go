//go:build integration

package export

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	exportrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/export"
	"go.mongodb.org/mongo-driver/bson"
)

// 批量导出的集成测试：真连 Mongo，按 type 各 seed 3 条，验
// 预览/CSV 行数与列序/中文字段/未知 type 与字段/行数上限/任务导出/explain 走索引。

// batchSeed 一次 seed 的上下文。
type batchSeed struct {
	Project  string
	Prefix   string
	TaskName string
}

// seedBatchData 每个 type 依赖的集合各插 3 条（project/taskName 都指向本次运行的唯一值），
// 清理按 project 删——IPAsset.project 是数组，等值查询同样命中数组元素。
func seedBatchData(t *testing.T) batchSeed {
	t.Helper()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	seed := batchSeed{
		Project:  "proj-export-" + suffix,
		Prefix:   "bx-" + suffix,
		TaskName: "task-export-" + suffix,
	}

	assetDocs := make([]interface{}, 0, 3)
	subdomainDocs := make([]interface{}, 0, 3)
	urlDocs := make([]interface{}, 0, 3)
	ipAssetDocs := make([]interface{}, 0, 3)
	rootDomainDocs := make([]interface{}, 0, 3)
	sensitiveDocs := make([]interface{}, 0, 3)
	vulnDocs := make([]interface{}, 0, 3)
	fileLeakDocs := make([]interface{}, 0, 3)
	npocDocs := make([]interface{}, 0, 3)

	for i := 0; i < 3; i++ {
		host := fmt.Sprintf("%s-%d.example.com", seed.Prefix, i)
		assetDocs = append(assetDocs, bson.M{
			"host":         host,
			"port":         []string{"443", "80", "8443"}[i],
			"ip":           fmt.Sprintf("198.18.32.%d", i+1),
			"title":        fmt.Sprintf("中文站点-%d", i),
			"statuscode":   200 + i,
			"url":          "https://" + host,
			"type":         "http",
			"service":      "https",
			"webServer":    "nginx",
			"technologies": []string{"nginx", "php"},
			"project":      seed.Project,
			"taskName":     []string{seed.TaskName},
			"rootDomain":   seed.Prefix + ".example.com",
			"time":         fmt.Sprintf("2026-11-18 12:00:0%d", i+1),
			"lastScanTime": fmt.Sprintf("2026-11-18 12:00:1%d", i+1),
		})
		subdomainDocs = append(subdomainDocs, bson.M{
			"host":       host,
			"type":       "A",
			"value":      []string{fmt.Sprintf("198.18.32.%d", i+1)},
			"ip":         []string{fmt.Sprintf("198.18.32.%d", i+1)},
			"time":       fmt.Sprintf("2026-11-18 12:00:0%d", i+1),
			"project":    seed.Project,
			"taskName":   seed.TaskName,
			"rootDomain": seed.Prefix + ".example.com",
			"tags":       []string{"tag-a", "tag-b"},
		})
		urlDocs = append(urlDocs, bson.M{
			"input":      "https://" + host + "/",
			"source":     "crawler",
			"outputtype": "url",
			"output":     "https://" + host + "/admin",
			"status":     200,
			"length":     1024 + i,
			"time":       fmt.Sprintf("2026-11-18 12:00:0%d", i+1),
			"project":    seed.Project,
			"taskName":   seed.TaskName,
			"rootDomain": seed.Prefix + ".example.com",
			"tags":       []string{"url-tag"},
		})
		ipAssetDocs = append(ipAssetDocs, bson.M{
			"ip": fmt.Sprintf("198.18.33.%d", i+1),
			"ports": []interface{}{
				bson.M{
					"port": []string{"443", "80", "8080"}[i],
					"server": []interface{}{
						bson.M{
							"domain":       host,
							"service":      "https",
							"webServer":    "nginx",
							"technologies": []string{"nginx"},
						},
					},
				},
			},
			"project":    []string{seed.Project},
			"taskName":   []string{seed.TaskName},
			"rootDomain": []string{seed.Prefix + ".example.com"},
			"time":       fmt.Sprintf("2026-11-18 12:00:0%d", i+1),
		})
		rootDomainDocs = append(rootDomainDocs, bson.M{
			"domain":  fmt.Sprintf("%s-%d.example.com", seed.Prefix, i),
			"company": fmt.Sprintf("中文企业-%d", i),
			"icp":     fmt.Sprintf("京ICP备%06d号", i+1),
			"project": seed.Project,
			"tags":    []string{"root-tag"},
			"time":    fmt.Sprintf("2026-11-18 12:00:0%d", i+1),
		})
		sensitiveDocs = append(sensitiveDocs, bson.M{
			"url":        "https://" + host + "/app.js",
			"sid":        []string{"apikey", "token", "idcard"}[i],
			"match":      []string{fmt.Sprintf("中文命中-%d", i), "sk-live-" + suffix},
			"color":      "danger",
			"status":     1,
			"project":    seed.Project,
			"rootDomain": seed.Prefix + ".example.com",
			"taskName":   seed.TaskName,
			"time":       fmt.Sprintf("2026-11-18 12:00:0%d", i+1),
			"md5":        fmt.Sprintf("%s-md5-%d", seed.Prefix, i),
		})
		fileLeakDocs = append(fileLeakDocs, bson.M{
			"url":            "https://" + host + "/backup.zip",
			"site":           host,
			"title":          fmt.Sprintf("中文泄露-%d", i),
			"status_code":    200 + i,
			"content_length": 1024 + i,
			"insert_time":    fmt.Sprintf("2026-11-18 12:00:0%d", i+1),
			"update_date":    fmt.Sprintf("2026-11-18 12:10:0%d", i+1),
			"project":        seed.Project,
		})
		npocDocs = append(npocDocs, bson.M{
			"scheme":      []string{"redis", "ssh", "mysql"}[i],
			"host":        host,
			"port":        []string{"6379", "22", "3306"}[i],
			"target":      host,
			"insert_time": fmt.Sprintf("2026-11-18 12:00:0%d", i+1),
			"project":     seed.Project,
		})
		vulnDocs = append(vulnDocs, bson.M{
			"url":        "https://" + host + "/admin",
			"vulnid":     fmt.Sprintf("cve-2026-%04d", i),
			"vulnname":   fmt.Sprintf("中文漏洞-%d", i),
			"matched":    "payload-" + suffix,
			"project":    seed.Project,
			"level":      []string{"critical", "high", "medium"}[i],
			"time":       fmt.Sprintf("2026-11-18 12:00:0%d", i+1),
			"taskName":   seed.TaskName,
			"rootDomain": seed.Prefix + ".example.com",
			"tags":       []string{"nuclei"},
			"status":     1,
			"hash":       fmt.Sprintf("%s-hash-%d", seed.Prefix, i),
		})
	}

	collections := map[string][]interface{}{
		"asset":              assetDocs,
		"subdomain":          subdomainDocs,
		"UrlScan":            urlDocs,
		"IPAsset":            ipAssetDocs,
		"RootDomain":         rootDomainDocs,
		"SensitiveResult":    sensitiveDocs,
		"vulnerability":      vulnDocs,
		"asset_fileleak":     fileLeakDocs,
		"asset_npoc_service": npocDocs,
	}
	ctx := context.Background()
	for coll, docs := range collections {
		if _, err := mongodb.DB.Collection(coll).InsertMany(ctx, docs); err != nil {
			t.Fatalf("seed %s: %v", coll, err)
		}
	}
	t.Cleanup(func() {
		for coll := range collections {
			_, _ = mongodb.DB.Collection(coll).DeleteMany(ctx, bson.M{"project": seed.Project})
		}
		mongodb.DB.Collection("task").DeleteMany(ctx, bson.M{"id": "taskid-" + suffix})
	})
	return seed
}

func newTestBatchService() *batchService {
	return newBatchService(exportrepo.NewBatchRepository())
}

// parseCSV 去掉 BOM 后按 csv 解析。
func parseCSV(t *testing.T, raw []byte) [][]string {
	t.Helper()
	if !bytes.HasPrefix(raw, utf8BOM) {
		head := raw
		if len(head) > 4 {
			head = head[:4]
		}
		t.Fatalf("csv missing utf-8 BOM, first bytes: % x", head)
	}
	reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(raw, utf8BOM)))
	rows, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	return rows
}

// TestBatchPreviewPerType 每个 type：命中 3 条、行数 3、列宽与字段一致。
func TestBatchPreviewPerType(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	seed := seedBatchData(t)
	svc := newTestBatchService()
	ctx := context.Background()

	for _, exportType := range ExportTypes() {
		t.Run(exportType, func(t *testing.T) {
			spec, ok := lookupSpec(exportType)
			if !ok {
				t.Fatalf("type %s missing spec", exportType)
			}
			result, err := svc.Preview(ctx, models.BatchExportRequest{Type: exportType, Project: seed.Project})
			if err != nil {
				t.Fatalf("Preview(%s): %v", exportType, err)
			}
			if result.Collection != spec.Collection {
				t.Errorf("collection = %s, want %s", result.Collection, spec.Collection)
			}
			if result.Total != 3 {
				t.Errorf("total = %d, want 3", result.Total)
			}
			if len(result.Rows) != 3 {
				t.Fatalf("rows = %d, want 3", len(result.Rows))
			}
			if len(result.Fields) != len(spec.Fields) {
				t.Errorf("fields = %v, want default %v", result.Fields, spec.Fields)
			}
			if result.Limit != PreviewRows {
				t.Errorf("limit = %d, want %d", result.Limit, PreviewRows)
			}
			for i, row := range result.Rows {
				if len(row) != len(result.Fields) {
					t.Fatalf("row %d width = %d, want %d (fields %v)", i, len(row), len(result.Fields), result.Fields)
				}
			}
			// 预览只取前 20 行：字段可用性也要一起返回，前端靠它做字段勾选
			if len(result.AvailableFields) != len(spec.Fields) {
				t.Errorf("availableFields = %v, want %v", result.AvailableFields, spec.Fields)
			}
		})
	}
}

// TestBatchExportCSVPerType 每个 type：CSV = 表头 + 3 行，列顺序与字段顺序一致。
func TestBatchExportCSVPerType(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	seed := seedBatchData(t)
	svc := newTestBatchService()
	ctx := context.Background()

	for _, exportType := range ExportTypes() {
		t.Run(exportType, func(t *testing.T) {
			plan, err := svc.Prepare(ctx, models.BatchExportRequest{Type: exportType, Project: seed.Project})
			if err != nil {
				t.Fatalf("Prepare(%s): %v", exportType, err)
			}
			want := lookupFields(t, exportType)
			if strings.Join(plan.Fields, ",") != strings.Join(want, ",") {
				t.Fatalf("fields = %v, want %v", plan.Fields, want)
			}

			var buf bytes.Buffer
			written, err := plan.Stream(ctx, &buf)
			if err != nil {
				t.Fatalf("Stream(%s): %v", exportType, err)
			}
			if written != 3 {
				t.Errorf("written = %d, want 3", written)
			}
			rows := parseCSV(t, buf.Bytes())
			if len(rows) != 4 {
				t.Fatalf("csv lines = %d, want header+3", len(rows))
			}
			if strings.Join(rows[0], ",") != strings.Join(want, ",") {
				t.Errorf("header = %v, want %v", rows[0], want)
			}
			for i, row := range rows[1:] {
				if len(row) != len(want) {
					t.Fatalf("line %d width = %d, want %d", i+2, len(row), len(want))
				}
			}
		})
	}
}

func lookupFields(t *testing.T, exportType string) []string {
	t.Helper()
	spec, ok := lookupSpec(exportType)
	if !ok {
		t.Fatalf("type %s missing spec", exportType)
	}
	return spec.Fields
}

// TestBatchExportChinese 中文必须原样落盘（BOM + UTF-8）：站点 title / 漏洞名 / 敏感命中。
func TestBatchExportChinese(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	seed := seedBatchData(t)
	svc := newTestBatchService()
	ctx := context.Background()

	cases := []struct {
		exportType string
		field      string
		want       string
	}{
		{models.ExportTypeSite, "title", "中文站点-0"},
		{models.ExportTypeVulnerability, "vulnname", "中文漏洞-0"},
		{models.ExportTypeWIH, "match", "中文命中-0"},
		{models.ExportTypeCIP, "company", "中文企业-0"},
	}
	for _, tc := range cases {
		t.Run(tc.exportType, func(t *testing.T) {
			plan, err := svc.Prepare(ctx, models.BatchExportRequest{Type: tc.exportType, Project: seed.Project})
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			var buf bytes.Buffer
			if _, err := plan.Stream(ctx, &buf); err != nil {
				t.Fatalf("Stream: %v", err)
			}
			if !bytes.HasPrefix(buf.Bytes(), utf8BOM) {
				t.Fatalf("missing BOM")
			}
			if !strings.Contains(buf.String(), tc.want) {
				t.Fatalf("%s does not contain %q: %s", tc.field, tc.want, buf.String())
			}
			rows := parseCSV(t, buf.Bytes())
			idx := -1
			for i, f := range rows[0] {
				if f == tc.field {
					idx = i
				}
			}
			if idx < 0 {
				t.Fatalf("header missing field %s: %v", tc.field, rows[0])
			}
			found := false
			for _, row := range rows[1:] {
				if strings.Contains(row[idx], tc.want) {
					found = true
				}
			}
			if !found {
				t.Fatalf("no row has %s=%q: %v", tc.field, tc.want, rows[1:])
			}
		})
	}
}

// TestBatchExportFieldSubset 字段子集：只出请求的列，顺序按请求顺序；跨 type 的字段名要拒绝。
func TestBatchExportFieldSubset(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	seed := seedBatchData(t)
	svc := newTestBatchService()
	ctx := context.Background()

	// port 是 asset_site 的字段，但不是 domain 的字段 -> 必须按未知字段拒绝
	if _, err := svc.Prepare(ctx, models.BatchExportRequest{
		Type:   models.ExportTypeDomain,
		Fields: []string{"port"},
	}); !errors.Is(err, ErrUnknownExportField) {
		t.Fatalf("domain.port err = %v, want ErrUnknownExportField", err)
	}

	plan, err := svc.Prepare(ctx, models.BatchExportRequest{
		Type:    models.ExportTypeAssetSite,
		Project: seed.Project,
		Fields:  []string{"port", "host"},
	})
	if err != nil {
		t.Fatalf("Prepare(asset_site): %v", err)
	}
	if strings.Join(plan.Fields, ",") != "port,host" {
		t.Fatalf("fields = %v, want [port host]（按请求顺序）", plan.Fields)
	}
	var buf bytes.Buffer
	if _, err := plan.Stream(ctx, &buf); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	rows := parseCSV(t, buf.Bytes())
	if len(rows) != 4 {
		t.Fatalf("lines = %d, want 4", len(rows))
	}
	if strings.Join(rows[0], ",") != "port,host" {
		t.Fatalf("header = %v, want [port host]", rows[0])
	}
	for _, row := range rows[1:] {
		if len(row) != 2 {
			t.Fatalf("width = %d, want 2", len(row))
		}
		if !strings.HasPrefix(row[1], seed.Prefix) {
			t.Errorf("host col = %q, want prefix %s", row[1], seed.Prefix)
		}
	}
}

// TestBatchExportValidation 未知 type / 未知字段 / 时间格式错 -> 哨兵错误（handler 映射 400）。
func TestBatchExportValidation(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	svc := newTestBatchService()
	ctx := context.Background()

	if _, err := svc.Prepare(ctx, models.BatchExportRequest{Type: "nope"}); !errors.Is(err, ErrUnknownExportType) {
		t.Fatalf("unknown type err = %v, want ErrUnknownExportType", err)
	} else if !strings.Contains(err.Error(), models.ExportTypeSite) {
		t.Errorf("error should list supported types: %v", err)
	}
	if _, err := svc.Prepare(ctx, models.BatchExportRequest{Type: models.ExportTypeDomain, Fields: []string{"nope"}}); !errors.Is(err, ErrUnknownExportField) {
		t.Fatalf("unknown field err = %v, want ErrUnknownExportField", err)
	}
	if _, err := svc.Prepare(ctx, models.BatchExportRequest{Type: models.ExportTypeDomain, StartTime: "2026/11/18"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("bad time err = %v, want ErrInvalidRequest", err)
	}
	if _, err := svc.Prepare(ctx, models.BatchExportRequest{
		Type:      models.ExportTypeDomain,
		StartTime: "2026-11-19 00:00:00",
		EndTime:   "2026-11-18 00:00:00",
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("start>end err = %v, want ErrInvalidRequest", err)
	}
}

// TestBatchExportRowCap 行数上限：超过即报错（写响应之前），预览不受影响。
func TestBatchExportRowCap(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	seed := seedBatchData(t)
	svc := newTestBatchService()
	ctx := context.Background()

	oldCap := maxExportRows
	maxExportRows = 2
	defer func() { maxExportRows = oldCap }()

	// find 口径
	if _, err := svc.Prepare(ctx, models.BatchExportRequest{Type: models.ExportTypeDomain, Project: seed.Project}); !errors.Is(err, ErrTooManyRows) {
		t.Fatalf("domain over cap err = %v, want ErrTooManyRows", err)
	}
	// 聚合口径（站点分组数也按同一上限判定）
	if _, err := svc.Prepare(ctx, models.BatchExportRequest{Type: models.ExportTypeSite, Project: seed.Project}); !errors.Is(err, ErrTooManyRows) {
		t.Fatalf("site over cap err = %v, want ErrTooManyRows", err)
	}
	// ip_port 是 unwind 后的行数（3 条 IPAsset -> 3 行），同样受上限约束
	if _, err := svc.Prepare(ctx, models.BatchExportRequest{Type: models.ExportTypeIPPort, Project: seed.Project}); !errors.Is(err, ErrTooManyRows) {
		t.Fatalf("ip_port over cap err = %v, want ErrTooManyRows", err)
	}
	// 上限内正常：时间收窄到 1 条
	plan, err := svc.Prepare(ctx, models.BatchExportRequest{
		Type:      models.ExportTypeDomain,
		Project:   seed.Project,
		StartTime: "2026-11-18 12:00:02",
		EndTime:   "2026-11-18 12:00:02",
	})
	if err != nil {
		t.Fatalf("Prepare within cap: %v", err)
	}
	if plan.RowCount != 1 {
		t.Fatalf("rowCount = %d, want 1", plan.RowCount)
	}
	// 预览照常能看总数（不受导出上限影响）
	result, err := svc.Preview(ctx, models.BatchExportRequest{Type: models.ExportTypeDomain, Project: seed.Project})
	if err != nil {
		t.Fatalf("Preview over cap: %v", err)
	}
	if result.Total != 3 || len(result.Rows) != 3 {
		t.Fatalf("preview total/rows = %d/%d, want 3/3", result.Total, len(result.Rows))
	}
}

// TestTaskExportZip 按任务导出：zip 内每集合一个 CSV，行数/表头正确；任务不存在 -> 哨兵错误。
func TestTaskExportZip(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	seed := seedBatchData(t)
	ctx := context.Background()

	taskID := "taskid-" + strings.TrimPrefix(seed.Project, "proj-export-")
	if _, err := mongodb.DB.Collection("task").InsertOne(ctx, bson.M{
		"id":   taskID,
		"name": seed.TaskName,
	}); err != nil {
		t.Fatalf("seed task: %v", err)
	}

	svc := newTestBatchService()
	plan, err := svc.PrepareTask(ctx, taskID)
	if err != nil {
		t.Fatalf("PrepareTask: %v", err)
	}
	if len(plan.Files) != len(TaskExportTypes) {
		t.Fatalf("files = %d, want %d", len(plan.Files), len(TaskExportTypes))
	}
	if plan.RowCount != int64(3*len(TaskExportTypes)) {
		t.Errorf("rowCount = %d, want %d", plan.RowCount, 3*len(TaskExportTypes))
	}

	var buf bytes.Buffer
	written, err := plan.Stream(ctx, &buf)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if written != plan.RowCount {
		t.Errorf("written = %d, want %d", written, plan.RowCount)
	}

	reader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	if len(reader.File) != len(TaskExportTypes) {
		t.Fatalf("zip entries = %d, want %d", len(reader.File), len(TaskExportTypes))
	}
	seen := map[string]bool{}
	for _, file := range reader.File {
		seen[file.Name] = true
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open %s: %v", file.Name, err)
		}
		raw, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", file.Name, err)
		}
		rows := parseCSV(t, raw)
		if len(rows) != 4 {
			t.Errorf("%s lines = %d, want header+3", file.Name, len(rows))
		}
		exportType := strings.TrimSuffix(file.Name, ".csv")
		want := lookupFields(t, exportType)
		if len(rows) > 0 && strings.Join(rows[0], ",") != strings.Join(want, ",") {
			t.Errorf("%s header = %v, want %v", file.Name, rows[0], want)
		}
	}
	for _, exportType := range TaskExportTypes {
		if !seen[exportType+".csv"] {
			t.Errorf("zip missing %s.csv (got %v)", exportType, seen)
		}
	}

	// 任务不存在 -> ErrTaskNotFound（handler 映射 404）
	if _, err := svc.PrepareTask(ctx, "taskid-does-not-exist"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("missing task err = %v, want ErrTaskNotFound", err)
	}
}

// winningPlanJSON 只取 queryPlanner.winningPlan（聚合取 $cursor 里的那份）。
// 不能拿整份 explain 判 SORT：rejectedPlans 里本来就会躺着带 SORT 的落选方案，
// 那是淘汰掉的计划，不是实际执行的计划。
func winningPlanJSON(t *testing.T, explain bson.M) string {
	t.Helper()
	norm, ok := normalizeValue(explain).(map[string]interface{})
	if !ok {
		t.Fatalf("explain is not a document: %T", explain)
	}
	planner, ok := norm["queryPlanner"].(map[string]interface{})
	if !ok {
		stages, _ := norm["stages"].([]interface{})
		if len(stages) == 0 {
			t.Fatalf("explain has neither queryPlanner nor stages")
		}
		first, _ := stages[0].(map[string]interface{})
		cursor, _ := first["$cursor"].(map[string]interface{})
		planner, ok = cursor["queryPlanner"].(map[string]interface{})
		if !ok {
			t.Fatalf("aggregate explain has no $cursor.queryPlanner: %v", stages[0])
		}
	}
	winning, ok := planner["winningPlan"]
	if !ok {
		t.Fatalf("queryPlanner has no winningPlan")
	}
	buf, err := json.Marshal(winning)
	if err != nil {
		t.Fatalf("marshal winningPlan: %v", err)
	}
	return string(buf)
}

// TestBatchExportUsesIndex explain 取证：导出查询（project + 时间范围 + time 倒序）
// 必须走 {project,time:-1} 索引，且没有阻塞式 SORT / COLLSCAN。
func TestBatchExportUsesIndex(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	seed := seedBatchData(t)
	// 生产启动路径会建索引；测试库可能还没建，先补一次，避免「没索引 -> COLLSCAN」的假阴性
	if err := mongodb.EnsureIndexes(mongodb.DB); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	svc := newTestBatchService()
	ctx := context.Background()

	cases := []struct {
		exportType string
		indexName  string
	}{
		{models.ExportTypeURL, "project_1_time_-1"},
		{models.ExportTypeDomain, "project_1_time_-1"},
		{models.ExportTypeVulnerability, "project_1_time_-1"},
		{models.ExportTypeWIH, "project_1_time_-1"},
		{models.ExportTypeAssetSite, "project_1_time_-1"},
		{models.ExportTypeAssetIP, "project_1_time_-1"},
		{models.ExportTypeCIP, "project_1_time_-1"},
		// 聚合口径：$match 也必须落到索引上
		{models.ExportTypeSite, "project_1_time_-1"},
		{models.ExportTypeIPPort, "project_1_time_-1"},
	}
	for _, tc := range cases {
		t.Run(tc.exportType, func(t *testing.T) {
			explain, err := svc.Explain(ctx, models.BatchExportRequest{
				Type:      tc.exportType,
				Project:   seed.Project,
				StartTime: "2026-11-18 00:00:00",
				EndTime:   "2026-11-19 00:00:00",
			})
			if err != nil {
				t.Fatalf("Explain: %v", err)
			}
			plan := winningPlanJSON(t, explain)
			if !strings.Contains(plan, "IXSCAN") {
				t.Fatalf("plan has no IXSCAN: %s", plan)
			}
			if strings.Contains(plan, "COLLSCAN") {
				t.Fatalf("plan uses COLLSCAN: %s", plan)
			}
			if strings.Contains(plan, "\"SORT\"") || strings.Contains(plan, "SORT_MERGE") {
				t.Fatalf("plan has blocking SORT: %s", plan)
			}
			if !strings.Contains(plan, tc.indexName) {
				t.Fatalf("plan does not use index %s: %s", tc.indexName, plan)
			}
		})
	}
}

// TestBatchExportTaskNameIndex taskName 过滤 + time 倒序也要走索引（按任务导出的热路径）。
func TestBatchExportTaskNameIndex(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	seed := seedBatchData(t)
	if err := mongodb.EnsureIndexes(mongodb.DB); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	svc := newTestBatchService()
	ctx := context.Background()

	for _, exportType := range []string{models.ExportTypeDomain, models.ExportTypeURL, models.ExportTypeVulnerability, models.ExportTypeWIH} {
		t.Run(exportType, func(t *testing.T) {
			explain, err := svc.Explain(ctx, models.BatchExportRequest{Type: exportType, TaskName: seed.TaskName})
			if err != nil {
				t.Fatalf("Explain: %v", err)
			}
			plan := winningPlanJSON(t, explain)
			if !strings.Contains(plan, "taskName_1_time_-1") {
				t.Fatalf("plan does not use taskName_1_time_-1: %s", plan)
			}
			if strings.Contains(plan, "\"SORT\"") {
				t.Fatalf("plan has blocking SORT: %s", plan)
			}
		})
	}
}

var _ = io.Discard
