package export

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	exportrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/export"
	"github.com/Autumn-27/ScopeSentry/internal/services/export/exportfilter"
	"github.com/xuri/excelize/v2"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 批量导出（对标 ARL-Next batchExport.py / export.py）。
//
// 三条硬约束：
//  1. 只有游标 + bufio(csv.Writer)，任何一步都不做 Find().All()/ReadAll()；
//  2. 单次导出行数上限 MaxExportRows，超了在写响应之前就报错（不能写一半再说超限）；
//  3. 字段只能是 spec 白名单里的子集，未知名直接拒绝，缺字段留空、不造数据。

const (
	// MaxExportRows 单次导出的数据行上限（不含表头）。按需求定 50 万。
	MaxExportRows = 500000
	// PreviewRows 预览返回的行数（前端确认字段用）。
	PreviewRows = 20
	// flushEvery 每写这么多行 flush 一次：边查边下发，长导出也能立刻看到首字节。
	flushEvery = 1000
)

// maxExportRows 运行期生效的上限。测试会把它调小来验证超限分支，
// 生产路径永远是 MaxExportRows。
var maxExportRows = MaxExportRows

// 导出失败原因（handler 用 errors.Is 映射成 400/404/500）。
var (
	// ErrInvalidRequest 请求参数本身不合法（时间格式、任务名为空等）。
	ErrInvalidRequest = errors.New("invalid export request")
	// ErrUnknownExportType 不支持的 type。
	ErrUnknownExportType = errors.New("unknown export type")
	// ErrUnknownExportField fields 里有该 type 不认识的字段。
	ErrUnknownExportField = errors.New("unknown export field")
	// ErrTooManyRows 命中行数超过上限，需要缩小范围。
	ErrTooManyRows = errors.New("too many rows to export")
	// ErrTaskNotFound 按任务导出时任务不存在。
	ErrTaskNotFound = errors.New("task not found")
)

// TaskExportTypes 按任务导出时，每个集合出一个 CSV（zip 内一个文件一个集合）。
var TaskExportTypes = []string{
	models.ExportTypeSite,
	models.ExportTypeDomain,
	models.ExportTypeURL,
	models.ExportTypeVulnerability,
	models.ExportTypeWIH,
}

// TaskExportFileName 按任务导出时 zip 内的文件名。
const TaskExportFileName = "task-%s-%s.zip"

// BatchService 批量导出 / 按任务导出 / 预览。
type BatchService interface {
	// Prepare 校验请求并统计命中行数；超限直接返回 ErrTooManyRows（此时还没写任何字节）。
	Prepare(ctx context.Context, req models.BatchExportRequest) (*ExportPlan, error)
	// PrepareTask 按任务导出：先确认任务存在，再按 TaskExportTypes 各准备一个文件。
	PrepareTask(ctx context.Context, taskID string) (*TaskExportPlan, error)
	// Preview 前 N 行 + 命中总数。
	Preview(ctx context.Context, req models.BatchExportRequest) (*models.ExportPreviewResult, error)
	// Explain 导出查询的 explain（queryPlanner），用来取证「导出走索引」。
	Explain(ctx context.Context, req models.BatchExportRequest) (bson.M, error)
}

type batchService struct {
	repo exportrepo.BatchRepository
}

// NewBatchService 创建批量导出服务。
func NewBatchService() BatchService {
	return newBatchService(exportrepo.NewBatchRepository())
}

func newBatchService(repo exportrepo.BatchRepository) *batchService {
	return &batchService{repo: repo}
}

// ExportPlan 一次（单集合）导出的执行计划。
type ExportPlan struct {
	// Type 请求里的 type。
	Type string
	// Collection 实际读的集合。
	Collection string
	// Fields 本次导出的列（顺序即 CSV 列顺序）。
	Fields []string
	// RowCount 命中行数（Prepare 时算好，用于超限判定与响应头）。
	RowCount int64

	spec  exportSpec
	match bson.M
	repo  exportrepo.BatchRepository
}

// TaskExportPlan 按任务的导出计划：一个 zip，内含每个集合一个 CSV。
type TaskExportPlan struct {
	TaskID   string
	TaskName string
	RowCount int64
	Files    []*ExportPlan
}

// Prepare 见接口说明。
func (s *batchService) Prepare(ctx context.Context, req models.BatchExportRequest) (*ExportPlan, error) {
	return s.plan(ctx, req, true)
}

// Preview 见接口说明。
func (s *batchService) Preview(ctx context.Context, req models.BatchExportRequest) (*models.ExportPreviewResult, error) {
	plan, err := s.plan(ctx, req, false)
	if err != nil {
		return nil, err
	}
	rows := make([][]string, 0, PreviewRows)
	err = plan.eachRow(ctx, int64(PreviewRows), func(row []string) error {
		// row 在 eachRow 里复用，必须拷一份再留
		clone := make([]string, len(row))
		copy(clone, row)
		rows = append(rows, clone)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &models.ExportPreviewResult{
		Type:            plan.Type,
		Collection:      plan.Collection,
		Fields:          plan.Fields,
		AvailableFields: plan.spec.Fields,
		Rows:            rows,
		Total:           plan.RowCount,
		Limit:           PreviewRows,
	}, nil
}

// Explain 见接口说明。
func (s *batchService) Explain(ctx context.Context, req models.BatchExportRequest) (bson.M, error) {
	plan, err := s.plan(ctx, req, false)
	if err != nil {
		return nil, err
	}
	return s.repo.Explain(ctx, plan.query(0))
}

// PrepareTask 见接口说明。
func (s *batchService) PrepareTask(ctx context.Context, taskID string) (*TaskExportPlan, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("%w: taskId is required", ErrInvalidRequest)
	}
	task, err := s.repo.FindTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}
	// 资产/漏洞等集合的 taskName 存的是任务名，任务名为空会让过滤条件失效（= 导出全库），
	// 所以这里必须拦住。
	if strings.TrimSpace(task.Name) == "" {
		return nil, fmt.Errorf("%w: task %s has empty name, cannot filter by taskName", ErrInvalidRequest, taskID)
	}

	files := make([]*ExportPlan, 0, len(TaskExportTypes))
	var total int64
	for _, exportType := range TaskExportTypes {
		plan, err := s.plan(ctx, models.BatchExportRequest{
			Type:     exportType,
			TaskName: task.Name,
		}, false)
		if err != nil {
			return nil, err
		}
		total += plan.RowCount
		files = append(files, plan)
	}
	if total > int64(maxExportRows) {
		return nil, fmt.Errorf("%w: task %s matched %d rows in total (limit %d)",
			ErrTooManyRows, taskID, total, maxExportRows)
	}
	return &TaskExportPlan{
		TaskID:   taskID,
		TaskName: task.Name,
		RowCount: total,
		Files:    files,
	}, nil
}

// plan 组装执行计划。enforceCap=false 用于预览/explain：预览要能看超限集合的总数。
func (s *batchService) plan(ctx context.Context, req models.BatchExportRequest, enforceCap bool) (*ExportPlan, error) {
	spec, ok := lookupSpec(req.Type)
	if !ok {
		return nil, fmt.Errorf("%w: %q (supported: %s)", ErrUnknownExportType, req.Type, strings.Join(ExportTypes(), ","))
	}
	fields, err := resolveFields(spec, req.Fields)
	if err != nil {
		return nil, err
	}
	match, err := buildMatch(req, spec.ExtraMatch)
	if err != nil {
		return nil, err
	}
	total, err := s.countRows(ctx, spec, match, enforceCap)
	if err != nil {
		return nil, err
	}
	return &ExportPlan{
		Type:       spec.Type,
		Collection: spec.Collection,
		Fields:     fields,
		RowCount:   total,
		spec:       spec,
		match:      match,
		repo:       s.repo,
	}, nil
}

// countRows 统计命中行数。find 形态用 count(limit)，聚合形态跑一遍「只留行数」的管道：
// 聚合的行数（站点分组数 / 端口摊平后的行数）与文档数不同，不能拿 CountDocuments 顶。
func (s *batchService) countRows(ctx context.Context, spec exportSpec, match bson.M, enforceCap bool) (int64, error) {
	if spec.Kind == kindAggregate {
		total, err := s.repo.CountPipeline(ctx, spec.Collection, spec.Pipeline(match, spec.Fields, true))
		if err != nil {
			return 0, err
		}
		if enforceCap && total > int64(maxExportRows) {
			return 0, tooManyRowsErr(total)
		}
		return total, nil
	}
	limit := int64(0)
	if enforceCap {
		limit = int64(maxExportRows) + 1
	}
	total, err := s.repo.CountDocuments(ctx, spec.Collection, match, limit)
	if err != nil {
		return 0, err
	}
	if enforceCap && total > int64(maxExportRows) {
		return 0, tooManyRowsErr(total)
	}
	return total, nil
}

func tooManyRowsErr(matched int64) error {
	return fmt.Errorf("%w: matched %d rows, limit is %d per export, narrow project/startTime/endTime",
		ErrTooManyRows, matched, maxExportRows)
}

// buildMatch 拼过滤条件：project / taskName 精确匹配 + 时间范围（可只给一边）。
// extra 是 type 级别的固定条件（asset_site 的 type=http）。
func buildMatch(req models.BatchExportRequest, extra bson.M) (bson.M, error) {
	base := bson.M{}
	if req.Project != "" {
		base["project"] = req.Project
	}
	if req.TaskName != "" {
		base["taskName"] = req.TaskName
	}
	filtered, err := exportfilter.ApplyTimeRange(base, req.StartTime, req.EndTime)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	return mergeMatch(bson.M(filtered), extra), nil
}

// mergeMatch 合并两个条件；base 可能已经被 ApplyTimeRange 包成 $and，直接合并会丢条件。
func mergeMatch(base bson.M, extra bson.M) bson.M {
	if len(extra) == 0 {
		return base
	}
	if len(base) == 0 {
		return extra
	}
	return bson.M{"$and": []interface{}{base, extra}}
}

// resolveFields 校验并规整字段子集：空 = 默认全集；未知字段报错；保序去重。
func resolveFields(spec exportSpec, requested []string) ([]string, error) {
	if len(requested) == 0 {
		return spec.Fields, nil
	}
	valid := make(map[string]struct{}, len(spec.Fields))
	for _, f := range spec.Fields {
		valid[f] = struct{}{}
	}
	seen := make(map[string]struct{}, len(requested))
	fields := make([]string, 0, len(requested))
	for _, f := range requested {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if _, ok := valid[f]; !ok {
			return nil, fmt.Errorf("%w: %q is not a field of %s (supported: %s)",
				ErrUnknownExportField, f, spec.Type, strings.Join(spec.Fields, ","))
		}
		if _, dup := seen[f]; dup {
			continue
		}
		seen[f] = struct{}{}
		fields = append(fields, f)
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("%w: fields is empty after trimming", ErrUnknownExportField)
	}
	return fields, nil
}

// query 生成仓库层查询描述。limit>0 时限制本次取多少条（预览用）。
func (p *ExportPlan) query(limit int64) exportrepo.BatchQuery {
	q := exportrepo.BatchQuery{
		Collection: p.spec.Collection,
		Filter:     p.match,
	}
	if p.spec.Kind == kindAggregate {
		q.Mode = exportrepo.ModeAggregate
		pipeline := p.spec.Pipeline(p.match, p.Fields, false)
		if limit > 0 {
			pipeline = append(pipeline, bson.D{{Key: "$limit", Value: limit}})
		}
		q.Pipeline = pipeline
		return q
	}
	q.Mode = exportrepo.ModeFind
	if limit > 0 {
		q.Limit = limit
	}
	q.Sort = p.spec.Sort
	q.Projection = projectStage(p.Fields)
	return q
}

// eachRow 流式取行：游标 -> 逐条 decode -> 按 Fields 取值，交给回调写出去。
// 全程不落地全量结果集。
func (p *ExportPlan) eachRow(ctx context.Context, limit int64, fn func([]string) error) error {
	cursor, err := p.repo.Cursor(ctx, p.query(limit))
	if err != nil {
		return err
	}
	defer func() {
		_ = cursor.Close(ctx)
	}()

	row := make([]string, len(p.Fields))
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			// 单条解码失败不该毁掉整次导出，跳过并记录
			logger.Error(fmt.Sprintf("export %s: decode document failed: %v", p.Type, err))
			continue
		}
		for i, f := range p.Fields {
			row[i] = cellValue(doc[f])
		}
		if err := fn(row); err != nil {
			return err
		}
	}
	return cursor.Err()
}

// utf8BOM Excel 打开 UTF-8 CSV 需要 BOM，否则中文乱码。
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// flusher 由 gin.ResponseWriter / http.Flusher 实现；服务层不依赖 net/http。
type flusher interface {
	Flush()
}

// Stream 把计划写成 CSV（BOM + 表头 + 数据行），返回写入的数据行数。
// 任何写错误立即中断——导出中途失败不能继续往下灌。
func (p *ExportPlan) Stream(ctx context.Context, w io.Writer) (int64, error) {
	if _, err := w.Write(utf8BOM); err != nil {
		return 0, err
	}
	cw := csv.NewWriter(w)
	if err := cw.Write(p.Fields); err != nil {
		return 0, err
	}
	// 表头先下发，客户端立刻能拿到首字节
	cw.Flush()
	if err := cw.Error(); err != nil {
		return 0, err
	}
	if f, ok := w.(flusher); ok {
		f.Flush()
	}

	var written int64
	err := p.eachRow(ctx, 0, func(row []string) error {
		// 数据在 Prepare 之后继续长（扫描在写）时的兜底：宁可截断报错，也不无限下发
		if written >= int64(maxExportRows) {
			return tooManyRowsErr(written + 1)
		}
		if err := cw.Write(row); err != nil {
			return err
		}
		written++
		if written%flushEvery == 0 {
			cw.Flush()
			if err := cw.Error(); err != nil {
				return err
			}
			if f, ok := w.(flusher); ok {
				f.Flush()
			}
		}
		return nil
	})
	cw.Flush()
	if flushErr := cw.Error(); flushErr != nil && err == nil {
		err = flushErr
	}
	if err != nil {
		return written, err
	}
	if written != p.RowCount {
		// 计数与流式读取之间数据变了（扫描在写），只记日志，不给前端制造假失败
		logger.Warn(fmt.Sprintf("export %s: counted %d rows but streamed %d", p.Type, p.RowCount, written))
	}
	return written, nil
}

// Stream 把按任务导出的多个 CSV 打进一个 zip（流式写，不落临时文件），返回数据行数。
func (p *TaskExportPlan) Stream(ctx context.Context, w io.Writer) (int64, error) {
	zw := zip.NewWriter(w)
	var written int64
	for _, file := range p.Files {
		entry, err := zw.CreateHeader(&zip.FileHeader{
			Name:     file.Type + ".csv",
			Method:   zip.Deflate,
			Modified: time.Now(),
		})
		if err != nil {
			_ = zw.Close()
			return written, err
		}
		n, err := file.Stream(ctx, entry)
		written += n
		if err != nil {
			_ = zw.Close()
			return written, err
		}
		// 一个文件写完就 flush，让客户端尽早开始收
		_ = zw.Flush()
	}
	if err := zw.Close(); err != nil {
		return written, err
	}
	return written, nil
}

// cellValue 把游标里的值转成 CSV 单元格文本。
//
// 约定：缺字段/空值 -> ""；标量数组用 ";" 连接（tags/technologies/ips 这类）；
// 嵌套文档、对象数组走 JSON（ports/domains 这类结构）。不补值、不猜值。
func cellValue(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case primitive.ObjectID:
		return t.Hex()
	case primitive.DateTime:
		return t.Time().Format(exportfilter.TimeLayout)
	}
	norm := normalizeValue(v)
	switch t := norm.(type) {
	case nil:
		return ""
	case string:
		return t
	case []interface{}:
		if joined, ok := joinScalars(t); ok {
			return joined
		}
		return toJSON(norm)
	case map[string]interface{}:
		return toJSON(norm)
	default:
		return fmt.Sprint(norm)
	}
}

// joinScalars 标量数组用 ";" 连接；含嵌套结构时返回 ok=false（交给 JSON 编码）。
func joinScalars(items []interface{}) (string, bool) {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		switch item.(type) {
		case map[string]interface{}, []interface{}:
			return "", false
		}
		s := cellValue(item)
		if s == "" {
			continue
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ";"), true
}

func toJSON(v interface{}) string {
	buf, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(buf)
}

// normalizeValue 把 BSON 专有类型摊成 JSON 友好的结构（primitive.D -> map 等）。
func normalizeValue(v interface{}) interface{} {
	switch t := v.(type) {
	case nil, string, bool:
		return v
	case primitive.A:
		out := make([]interface{}, 0, len(t))
		for _, item := range t {
			out = append(out, normalizeValue(item))
		}
		return out
	case []interface{}:
		out := make([]interface{}, 0, len(t))
		for _, item := range t {
			out = append(out, normalizeValue(item))
		}
		return out
	case []string:
		out := make([]interface{}, 0, len(t))
		for _, item := range t {
			out = append(out, item)
		}
		return out
	case primitive.D:
		out := make(map[string]interface{}, len(t))
		for _, e := range t {
			out[e.Key] = normalizeValue(e.Value)
		}
		return out
	case primitive.M:
		out := make(map[string]interface{}, len(t))
		for k, val := range t {
			out[k] = normalizeValue(val)
		}
		return out
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, val := range t {
			out[k] = normalizeValue(val)
		}
		return out
	case primitive.ObjectID:
		return t.Hex()
	case primitive.DateTime:
		return t.Time().Format(exportfilter.TimeLayout)
	case primitive.Binary:
		return fmt.Sprintf("<binary %d bytes>", len(t.Data))
	case primitive.Decimal128:
		return t.String()
	default:
		return v
	}
}

// taskSheetNames 任务导出工作簿里的工作表名（≤31 字符、不含 Excel 禁用字符）。
// 用中文而不是 type：打开文件的人要看的是「资产/子域名/URL/漏洞/敏感信息」。
var taskSheetNames = map[string]string{
	"site":          "资产",
	"domain":        "子域名",
	"url":           "URL",
	"vulnerability": "漏洞",
	"wih":           "敏感信息",
	"ip":            "IP",
	"ip_port":       "IP端口",
	"cip":           "ICP主体",
	"asset_ip":      "资产IP",
	"asset_domain":  "资产域名",
	"asset_site":    "资产站点",
	"asset_wih":     "资产敏感信息",
	"sensitive":     "敏感信息",
	"fileleak":      "文件泄露",
	"npoc_service":  "服务维度",
}

func taskSheetName(exportType string) string {
	if name, ok := taskSheetNames[exportType]; ok {
		return name
	}
	if exportType == "" {
		return "sheet"
	}
	return exportType
}

// StreamXLSX 把按任务导出的多个集合写进**一个**工作簿，每个集合一张工作表。
//
// 与 Stream（zip 内每集合一个 CSV）并存：CSV 适合直接丢进别的工具，
// xlsx 适合人看（对齐 ARL-Next 1.5.2 的多工作表导出）。空集合也会留一张只有表头的表，
// 让「这次任务没产出敏感信息」是可见的，而不是缺一张表让人猜。
func (p *TaskExportPlan) StreamXLSX(ctx context.Context, w io.Writer) (int64, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	var written int64
	for i, plan := range p.Files {
		sheet := taskSheetName(plan.Type)
		if i == 0 {
			if err := f.SetSheetName("Sheet1", sheet); err != nil {
				return written, err
			}
		} else if _, err := f.NewSheet(sheet); err != nil {
			return written, err
		}

		sw, err := f.NewStreamWriter(sheet)
		if err != nil {
			return written, err
		}
		header := make([]interface{}, len(plan.Fields))
		for col, field := range plan.Fields {
			header[col] = field
		}
		if err := sw.SetRow("A1", header); err != nil {
			return written, err
		}

		// RowCount=0 的集合只留表头：不查库，也避免 limit=0 被当成「不限量」。
		if plan.RowCount <= 0 {
			if err := sw.Flush(); err != nil {
				return written, err
			}
			continue
		}
		rowNum := 2
		err = plan.eachRow(ctx, plan.RowCount, func(row []string) error {
			cells := make([]interface{}, len(row))
			for col, v := range row {
				cells[col] = sanitizeExcelValue(v)
			}
			if err := sw.SetRow(getCellName(1, rowNum), cells); err != nil {
				return err
			}
			rowNum++
			return nil
		})
		if err != nil {
			return written, err
		}
		if err := sw.Flush(); err != nil {
			return written, err
		}
		written += plan.RowCount
	}

	if err := f.Write(w); err != nil {
		return written, err
	}
	return written, nil
}
