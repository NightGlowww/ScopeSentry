package export

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	svcExport "github.com/Autumn-27/ScopeSentry/internal/services/export"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

// handler 的失败原因映射是纯逻辑，不需要 Mongo：
// 用假 service 把每个哨兵错误喂进 handler，验状态码与提示。

type fakeBatchService struct {
	prepareErr error
	plan       *svcExport.ExportPlan

	taskPlan *svcExport.TaskExportPlan
	taskErr  error

	preview    *models.ExportPreviewResult
	previewErr error
}

func (f *fakeBatchService) Prepare(context.Context, models.BatchExportRequest) (*svcExport.ExportPlan, error) {
	if f.prepareErr != nil {
		return nil, f.prepareErr
	}
	return f.plan, nil
}

func (f *fakeBatchService) PrepareTask(context.Context, string, string, string) (*svcExport.TaskExportPlan, error) {
	if f.taskErr != nil {
		return nil, f.taskErr
	}
	return f.taskPlan, nil
}

func (f *fakeBatchService) Preview(context.Context, models.BatchExportRequest) (*models.ExportPreviewResult, error) {
	if f.previewErr != nil {
		return nil, f.previewErr
	}
	return f.preview, nil
}

func (f *fakeBatchService) Explain(context.Context, models.BatchExportRequest) (bson.M, error) {
	return nil, nil
}

func newExportTestContext(method, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	return c, w
}

func withFakeBatchService(t *testing.T, fake *fakeBatchService) {
	t.Helper()
	old := batchExportService
	batchExportService = fake
	t.Cleanup(func() { batchExportService = old })
}

// TestBatchExportHandlerErrorMapping 未知 type/字段/超限/参数错 -> 400，其它 -> 500。
func TestBatchExportHandlerErrorMapping(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		want    int
		contain string
	}{
		{
			name:    "over row cap",
			err:     fmt.Errorf("%w: matched 500001 rows, limit is 500000", svcExport.ErrTooManyRows),
			want:    http.StatusBadRequest,
			contain: "limit is 500000",
		},
		{
			name:    "unknown type",
			err:     fmt.Errorf("%w: %q (supported: site,domain)", svcExport.ErrUnknownExportType, "nope"),
			want:    http.StatusBadRequest,
			contain: "unknown export type",
		},
		{
			name:    "unknown field",
			err:     fmt.Errorf("%w: %q is not a field of domain", svcExport.ErrUnknownExportField, "nope"),
			want:    http.StatusBadRequest,
			contain: "unknown export field",
		},
		{
			name:    "invalid request",
			err:     fmt.Errorf("%w: invalid startTime", svcExport.ErrInvalidRequest),
			want:    http.StatusBadRequest,
			contain: "invalid export request",
		},
		{
			name: "internal error",
			err:  errors.New("mongo is down"),
			want: http.StatusInternalServerError,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withFakeBatchService(t, &fakeBatchService{prepareErr: tc.err})
			c, w := newExportTestContext(http.MethodPost, "/api/export/batch", `{"type":"site"}`)
			BatchExport(c)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.want, w.Body.String())
			}
			if tc.contain != "" && !strings.Contains(w.Body.String(), tc.contain) {
				t.Errorf("body %s does not contain %q", w.Body.String(), tc.contain)
			}
		})
	}
}

// TestBatchExportHandlerBadBody 缺 type 的请求体要被 binding 拦成 400。
func TestBatchExportHandlerBadBody(t *testing.T) {
	withFakeBatchService(t, &fakeBatchService{})
	c, w := newExportTestContext(http.MethodPost, "/api/export/batch", `{}`)
	BatchExport(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

// TestTaskExportHandler 任务不存在 -> 404；存在 -> 200 且是合法 zip（空文件集合也能出合法包）。
func TestTaskExportHandler(t *testing.T) {
	withFakeBatchService(t, &fakeBatchService{
		taskErr: fmt.Errorf("%w: taskid-x", svcExport.ErrTaskNotFound),
	})
	c, w := newExportTestContext(http.MethodGet, "/api/export/task/taskid-x", "")
	c.Params = gin.Params{{Key: "taskId", Value: "taskid-x"}}
	TaskExport(c)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing task status = %d, want 404: %s", w.Code, w.Body.String())
	}

	withFakeBatchService(t, &fakeBatchService{
		taskPlan: &svcExport.TaskExportPlan{TaskID: "taskid-y", TaskName: "t", RowCount: 0},
	})
	c, w = newExportTestContext(http.MethodGet, "/api/export/task/taskid-y", "")
	c.Params = gin.Params{{Key: "taskId", Value: "taskid-y"}}
	TaskExport(c)
	if w.Code != http.StatusOK {
		t.Fatalf("task export status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "application/zip") {
		t.Errorf("content-type = %q, want application/zip", got)
	}
	disposition := w.Header().Get("Content-Disposition")
	matched, err := regexp.MatchString(`^attachment; filename="task-taskid-y-\d{14}\.zip"$`, disposition)
	if err != nil || !matched {
		t.Errorf("content-disposition = %q, want task-taskid-y-<yyyymmddHHMMSS>.zip", disposition)
	}
	if !strings.HasPrefix(w.Body.String(), "PK") {
		t.Errorf("body is not a zip: %q", w.Body.String())
	}
	if got := w.Header().Get(exportRowsHeader); got != "0" {
		t.Errorf("%s = %q, want 0", exportRowsHeader, got)
	}
}

// TestTaskExportHandlerBadID 空 taskId 直接 400（不落到 404）。
func TestTaskExportHandlerBadID(t *testing.T) {
	withFakeBatchService(t, &fakeBatchService{})
	c, w := newExportTestContext(http.MethodGet, "/api/export/task/", "")
	c.Params = gin.Params{{Key: "taskId", Value: "   "}}
	TaskExport(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

// TestPreviewExportHandler 预览成功返回 total/rows/fields；字段错误 -> 400。
func TestPreviewExportHandler(t *testing.T) {
	withFakeBatchService(t, &fakeBatchService{
		preview: &models.ExportPreviewResult{
			Type:            models.ExportTypeDomain,
			Collection:      "subdomain",
			Fields:          []string{"host", "time"},
			AvailableFields: []string{"host", "time"},
			Rows:            [][]string{{"a.example.com", "2026-11-18 12:00:00"}},
			Total:           3,
			Limit:           20,
		},
	})
	c, w := newExportTestContext(http.MethodPost, "/api/export/preview", `{"type":"domain"}`)
	PreviewExport(c)
	if w.Code != http.StatusOK {
		t.Fatalf("preview status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var payload struct {
		Data models.ExportPreviewResult `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode preview: %v (%s)", err, w.Body.String())
	}
	if payload.Data.Total != 3 || len(payload.Data.Rows) != 1 || payload.Data.Fields[0] != "host" {
		t.Errorf("preview payload = %+v", payload.Data)
	}

	withFakeBatchService(t, &fakeBatchService{previewErr: fmt.Errorf("%w: bad field", svcExport.ErrUnknownExportField)})
	c, w = newExportTestContext(http.MethodPost, "/api/export/preview", `{"type":"domain","fields":["nope"]}`)
	PreviewExport(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("preview bad field status = %d, want 400: %s", w.Code, w.Body.String())
	}
}
