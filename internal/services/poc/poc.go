package poc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/services/node"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"archive/zip"
	"io"
	"os"
	"path/filepath"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/repositories/poc"
	"github.com/Autumn-27/ScopeSentry/internal/utils/helper"
	"github.com/Autumn-27/ScopeSentry/internal/utils/random"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrPocNotFound POC 不存在
var ErrPocNotFound = errors.New("poc not found")

// ErrInvalidTemplate POC 模板 YAML 解析失败：调用方参数问题，handler 映射 400。
// 以前这种错会冒成 500 + 原始 yaml 报错文案（2026-11-28 线上实测）。
var ErrInvalidTemplate = errors.New("poc: invalid template")

// Service 定义POC服务接口
type Service interface {
	GetPocList(ctx *gin.Context, req *models.PocListRequest) (*models.PocListResponse, error)
	GetPocDetail(ctx *gin.Context, req *models.PocDetailRequest) (*models.Poc, error)
	UpdatePoc(ctx *gin.Context, req *models.PocUpdateRequest) error
	AddPoc(ctx context.Context, req *models.PocAddRequest) (string, error)
	// DeletePoc 返回真实删除条数（0 表示这些 id 都不存在）
	DeletePoc(ctx *gin.Context, req *models.PocDeleteRequest) (int64, error)
	GetAllPocData(ctx context.Context) ([]models.Poc, error)
	ImportPoc(ctx context.Context, filePath string) (*models.PocImportResponse, error)
	ImportPocWithOptions(ctx context.Context, filePath string, overwrite bool) (*models.PocImportResponse, error)
	SyncFromURL(ctx context.Context, url string) (*models.PocImportResponse, error)
	UpsertPoc(ctx context.Context, content string) (pocID string, action string, err error)
	TemplateIdExists(ctx context.Context, templateId string) (bool, error)
}

type service struct {
	repo        poc.Repository
	nodeService node.Service
}

// NewService 创建新的POC服务实例
func NewService() Service {
	return &service{
		repo:        poc.NewRepository(),
		nodeService: node.NewService(),
	}
}

func (s *service) GetPocList(ctx *gin.Context, req *models.PocListRequest) (*models.PocListResponse, error) {
	// 搜索要覆盖 name / id / tags 三个字段：
	// 用户从 nuclei 模板或扫描命令里复制过来的通常是 **id**（ack-cluster-api-public），
	// 只匹配 name 的话搜不到，界面表现为「库里有但搜索说没有」（2026-11-28 实测，本轮修）。
	// 同时把输入当字面量：未转义的 $regex 遇到 "(" 之类的字符会报错，
	// 也会把用户输入当正则执行（a.*b 之类的写法本来就搜不到东西）。
	search := strings.TrimSpace(req.Search)
	query := bson.M{}
	if search != "" {
		rx := bson.M{"$regex": regexp.QuoteMeta(search), "$options": "i"}
		query["$or"] = []bson.M{{"name": rx}, {"id": rx}, {"tags": rx}}
	}

	if len(req.Filter.Level) > 0 {
		query["level"] = bson.M{"$in": req.Filter.Level}
	}

	// 列表只要 id/name/level/tags/time：content 是整段 YAML，编辑时才用 /poc/detail 取。
	// 不排除它的话，200 条实测 12.4MB / 8.6s，1000 条 13.8MB（2026-11-28 实测），
	// POC 页打开要等近十秒，而界面上根本不显示正文。
	opts := options.Find().
		SetProjection(bson.M{"content": 0}).
		SetSort(bson.D{{Key: "time", Value: -1}}).
		SetSkip(int64((req.PageIndex - 1) * req.PageSize)).
		SetLimit(int64(req.PageSize))

	results, err := s.repo.FindWithPagination(ctx, query, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get poc list: %w", err)
	}

	total, err := s.repo.Count(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to count poc: %w", err)
	}

	return &models.PocListResponse{
		List:  results,
		Total: total,
	}, nil
}

func (s *service) GetPocDetail(ctx *gin.Context, req *models.PocDetailRequest) (*models.Poc, error) {
	// 详情要同时支持两种标识：
	//   1) 库内 _id（ObjectID 十六进制）——列表点进来用的是它；
	//   2) nuclei 模板 id（ack-cluster-api-public）——人从模板/命令行复制过来的是它。
	// 以前非 ObjectID 直接 500「invalid id format」，等于告诉调用方「服务器错了」。
	if id, err := primitive.ObjectIDFromHex(req.ID); err == nil {
		result, ferr := s.repo.FindOne(ctx, bson.M{"_id": id})
		if ferr == nil {
			return result, nil
		}
		if !errors.Is(ferr, poc.ErrPocNotFound) {
			return nil, fmt.Errorf("failed to get poc detail: %w", ferr)
		}
	}

	// 退回按模板 id 查；查不到就是 404，而不是 500
	if strings.TrimSpace(req.ID) != "" {
		result, ferr := s.repo.FindOne(ctx, bson.M{"id": req.ID})
		if ferr == nil {
			return result, nil
		}
		if !errors.Is(ferr, poc.ErrPocNotFound) {
			return nil, fmt.Errorf("failed to get poc detail: %w", ferr)
		}
	}
	return nil, ErrPocNotFound
}

func (s *service) UpdatePoc(ctx *gin.Context, req *models.PocUpdateRequest) error {
	id, err := primitive.ObjectIDFromHex(req.ID)
	if err != nil {
		return fmt.Errorf("invalid id format: %w", err)
	}

	var pt models.PocTemplate
	err = yaml.Unmarshal([]byte(req.Content), &pt)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTemplate, err)
	}
	ptTags := strings.Split(pt.Info.Tags, ",")
	tags := helper.RemoveArrayDuplicates(ptTags)
	update := bson.M{
		"$set": bson.M{
			"name":    pt.Info.Name,
			"content": req.Content,
			"level":   pt.Info.Severity,
			"tags":    tags,
		},
	}

	res, err := s.repo.UpdateOne(ctx, bson.M{"_id": id}, update)
	if err != nil {
		return fmt.Errorf("failed to update poc: %w", err)
	}
	// 匹配 0 条不是成功：改一个不存在的 POC 以前回 200（2026-11-28 线上实测同类问题）
	if res == nil || res.MatchedCount == 0 {
		return ErrPocNotFound
	}
	go func() {
		msg := models.Message{
			Name:    "all",
			Type:    "poc",
			Content: fmt.Sprintf(`add:%v`, req.ID),
		}
		err = s.nodeService.RefreshConfig(ctx, msg)
		if err != nil {
			logger.Error("failed to refresh config", zap.Error(err))
		}
	}()

	return nil
}

func (s *service) AddPoc(ctx context.Context, req *models.PocAddRequest) (string, error) {

	var pt models.PocTemplate
	err := yaml.Unmarshal([]byte(req.Content), &pt)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal poc template: %w", err)
	}
	id := pt.ID
	existing, err := s.repo.FindOne(ctx, bson.M{"id": id})
	if err == nil && existing != nil {
		return "", fmt.Errorf("poc already exists")
	}
	ptTags := strings.Split(pt.Info.Tags, ",")
	tags := helper.RemoveArrayDuplicates(ptTags)
	doc := models.Poc{
		Name:       pt.Info.Name,
		Content:    req.Content,
		Level:      pt.Info.Severity,
		TemplateId: id,
		Tags:       tags,
		Time:       helper.GetNowTimeString(),
	}

	res, err := s.repo.InsertOne(ctx, doc)
	if err != nil {
		return "", fmt.Errorf("failed to add poc: %w", err)
	}
	pocId := res.InsertedID.(primitive.ObjectID).Hex()
	go func() {
		msg := models.Message{
			Name:    "all",
			Type:    "poc",
			Content: fmt.Sprintf(`add:%v`, pocId),
		}
		err = s.nodeService.RefreshConfig(ctx, msg)
		if err != nil {
			logger.Error("failed to refresh config", zap.Error(err))
		}
	}()
	return pocId, nil
}

func (s *service) DeletePoc(ctx *gin.Context, req *models.PocDeleteRequest) (int64, error) {
	var objIDs []primitive.ObjectID
	for _, id := range req.IDs {
		objID, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			return 0, fmt.Errorf("invalid id format: %w", err)
		}
		objIDs = append(objIDs, objID)
	}

	res, err := s.repo.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": objIDs}})
	if err != nil {
		return 0, fmt.Errorf("failed to delete poc: %w", err)
	}
	go func() {
		msg := models.Message{
			Name:    "all",
			Type:    "poc",
			Content: fmt.Sprintf(`delete:%v`, strings.Join(req.IDs, ",")),
		}
		if err := s.nodeService.RefreshConfig(ctx, msg); err != nil {
			logger.Error("failed to refresh config", zap.Error(err))
		}
	}()
	return res.DeletedCount, nil
}

func (s *service) GetAllPocData(ctx context.Context) ([]models.Poc, error) {
	result, err := s.repo.GetAllPocData(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get all poc data: %w", err)
	}
	return result, nil
}

// ImportPoc 导入 POC 文件。
// 默认 overwrite=true：同 template id 内容变化时更新，保留 _id，扫描模板的 VulList 引用不会断。
func (s *service) ImportPoc(ctx context.Context, filePath string) (*models.PocImportResponse, error) {
	return s.ImportPocWithOptions(ctx, filePath, true)
}

// ImportPocWithOptions overwrite=false 时保持旧行为：同 id 已存在即计入 repeat，不更新内容。
func (s *service) ImportPocWithOptions(ctx context.Context, filePath string, overwrite bool) (*models.PocImportResponse, error) {
	logger.Info("POC导入开始")

	extractPath := filepath.Join(os.TempDir(), random.GenerateString(11))
	if err := os.MkdirAll(extractPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create extract directory: %w", err)
	}

	yamlFiles, err := s.extractZipFile(filePath, extractPath)
	if err != nil {
		return nil, fmt.Errorf("failed to extract zip file: %w", err)
	}
	defer s.cleanupTempFiles(filePath, extractPath)

	index, err := s.repo.GetTemplateIndex(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load poc index: %w", err)
	}

	var (
		successNum, updateNum, repeatNum, errorNum int
		newDocs                                    []models.Poc
		changedIDs                                 []string
	)

	logger.Info(fmt.Sprintf("共%d个POC", len(yamlFiles)))
	GlobalImportProgress.Begin(len(yamlFiles))
	defer func() { GlobalImportProgress.Finish("") }()

	for _, yamlFile := range yamlFiles {
		content, readErr := os.ReadFile(yamlFile)
		if readErr != nil {
			logger.Error(fmt.Sprintf("POC导入 读取文件失败: %s", yamlFile), zap.Error(readErr))
			errorNum++
			GlobalImportProgress.Step("failed")
			continue
		}

		var pt models.PocTemplate
		if unmarshalErr := yaml.Unmarshal(content, &pt); unmarshalErr != nil {
			logger.Error(fmt.Sprintf("POC导入 解析YAML失败: %s", yamlFile), zap.Error(unmarshalErr))
			errorNum++
			GlobalImportProgress.Step("failed")
			continue
		}
		if pt.Info.Name == "" || pt.ID == "" {
			errorNum++
			GlobalImportProgress.Step("failed")
			continue
		}

		raw := string(content)
		hash := pocContentHash(raw)

		if entry, exists := index[pt.ID]; exists {
			existingHash := entry.ContentHash
			if existingHash == "" {
				// 历史文档没有 content_hash：只为冲突的这一条补算，不读全量
				if existing, findErr := s.repo.FindOne(ctx, bson.M{"id": pt.ID}); findErr == nil && existing != nil {
					existingHash = pocContentHash(existing.Content)
					index[pt.ID] = models.PocIndexEntry{ID: entry.ID, ContentHash: existingHash}
				}
			}
			if existingHash == hash || !overwrite {
				repeatNum++
				GlobalImportProgress.Step("repeat")
				continue
			}
			update := bson.M{"$set": bson.M{
				"name":         pt.Info.Name,
				"content":      raw,
				"level":        pocLevel(pt.Info.Severity),
				"tags":         pocTags(pt.Info.Tags),
				"content_hash": hash,
			}}
			if _, updateErr := s.repo.UpdateOne(ctx, bson.M{"_id": entry.ID}, update); updateErr != nil {
				logger.Error(fmt.Sprintf("POC导入 更新失败: %s", pt.ID), zap.Error(updateErr))
				errorNum++
				GlobalImportProgress.Step("failed")
				continue
			}
			updateNum++
			GlobalImportProgress.Step("update")
			changedIDs = append(changedIDs, entry.ID.Hex())
			continue
		}

		GlobalImportProgress.Step("success")
		newDocs = append(newDocs, models.Poc{
			Name:        pt.Info.Name,
			Content:     raw,
			TemplateId:  pt.ID,
			Level:       pocLevel(pt.Info.Severity),
			Tags:        pocTags(pt.Info.Tags),
			Time:        helper.GetNowTimeString(),
			ContentHash: hash,
		})
	}

	if len(newDocs) > 0 {
		result, insertErr := s.repo.InsertMany(ctx, newDocs)
		if insertErr != nil {
			return nil, fmt.Errorf("failed to insert poc data: %w", insertErr)
		}
		if result.InsertedIDs != nil {
			successNum = len(result.InsertedIDs)
			for _, id := range result.InsertedIDs {
				if oid, ok := id.(primitive.ObjectID); ok {
					changedIDs = append(changedIDs, oid.Hex())
				}
			}
		}
	}

	if len(changedIDs) > 0 {
		s.notifyPocChanged(changedIDs)
	}

	logger.Info(fmt.Sprintf("POC import completed: %d inserted, %d updated, %d unchanged, %d failed",
		successNum, updateNum, repeatNum, errorNum))

	return &models.PocImportResponse{
		SuccessNum: successNum,
		UpdateNum:  updateNum,
		RepeatNum:  repeatNum,
		ErrorNum:   errorNum,
		Message:    "import success",
	}, nil
}

// UpsertPoc 按 template id 落库：没有就建，内容变了就更新（保留 _id），没变就跳过。
// 供 Nuclei Monitor 这类同步流程使用；action 取 created / updated / unchanged。
func (s *service) UpsertPoc(ctx context.Context, content string) (string, string, error) {
	var pt models.PocTemplate
	if err := yaml.Unmarshal([]byte(content), &pt); err != nil {
		return "", "", fmt.Errorf("failed to unmarshal poc template: %w", err)
	}
	if pt.ID == "" || pt.Info.Name == "" {
		return "", "", fmt.Errorf("poc template missing id or info.name")
	}

	hash := pocContentHash(content)
	existing, err := s.repo.FindOne(ctx, bson.M{"id": pt.ID})
	if err != nil {
		if !errors.Is(err, mongo.ErrNoDocuments) {
			return "", "", fmt.Errorf("failed to find poc: %w", err)
		}
		res, insertErr := s.repo.InsertOne(ctx, models.Poc{
			Name:        pt.Info.Name,
			Content:     content,
			TemplateId:  pt.ID,
			Level:       pocLevel(pt.Info.Severity),
			Tags:        pocTags(pt.Info.Tags),
			Time:        helper.GetNowTimeString(),
			ContentHash: hash,
		})
		if insertErr != nil {
			return "", "", fmt.Errorf("failed to insert poc: %w", insertErr)
		}
		oid, _ := res.InsertedID.(primitive.ObjectID)
		s.notifyPocChanged([]string{oid.Hex()})
		return oid.Hex(), "created", nil
	}

	existingHash := existing.ContentHash
	if existingHash == "" {
		existingHash = pocContentHash(existing.Content)
	}
	if existingHash == hash {
		return existing.ID.Hex(), "unchanged", nil
	}

	update := bson.M{"$set": bson.M{
		"name":         pt.Info.Name,
		"content":      content,
		"level":        pocLevel(pt.Info.Severity),
		"tags":         pocTags(pt.Info.Tags),
		"content_hash": hash,
	}}
	if _, err := s.repo.UpdateOne(ctx, bson.M{"_id": existing.ID}, update); err != nil {
		return "", "", fmt.Errorf("failed to update poc: %w", err)
	}
	s.notifyPocChanged([]string{existing.ID.Hex()})
	return existing.ID.Hex(), "updated", nil
}

// notifyPocChanged 通知节点重新拉取这些 POC（add 对节点语义是「写入/覆盖该模板文件」）。
func (s *service) notifyPocChanged(ids []string) {
	if len(ids) == 0 {
		return
	}
	content := "add:" + strings.Join(ids, ",")
	go func() {
		msg := models.Message{Name: "all", Type: "poc", Content: content}
		if err := s.nodeService.RefreshConfig(context.Background(), msg); err != nil {
			logger.Error("failed to refresh config", zap.Error(err))
		}
	}()
}

// pocLevel 统一 severity 取值（小写，缺省 unknown）
func pocLevel(severity string) string {
	level := strings.ToLower(strings.TrimSpace(severity))
	if level == "" {
		return "unknown"
	}
	return level
}

// pocTags 拆分并去重模板标签
func pocTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	parts := strings.Split(raw, ",")
	tags := make([]string, 0, len(parts))
	for _, tag := range parts {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags = append(tags, tag)
		}
	}
	return helper.RemoveArrayDuplicates(tags)
}

// normalizePocContent 归一化后用于比较：统一换行、去行尾空白、去尾部空行。
// 只影响「是否算变化」的判断，落库内容仍是上游原文。
func normalizePocContent(raw string) string {
	raw = strings.TrimPrefix(raw, "\ufeff")
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// pocContentHash 内容指纹
func pocContentHash(raw string) string {
	sum := sha256.Sum256([]byte(normalizePocContent(raw)))
	return hex.EncodeToString(sum[:])
}

// TemplateIdExists 高性能检查TemplateId是否存在
func (s *service) TemplateIdExists(ctx context.Context, templateId string) (bool, error) {
	return s.repo.TemplateIdExists(ctx, templateId)
}

// extractZipFile 解压ZIP文件并返回YAML文件列表
func (s *service) extractZipFile(zipPath, extractPath string) ([]string, error) {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	var yamlFiles []string

	// 确保解压路径是绝对路径
	absExtractPath, err := filepath.Abs(extractPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	for _, file := range reader.File {
		// 检查文件名是否包含路径穿越字符
		cleanName := filepath.Clean(file.Name)
		if strings.HasPrefix(cleanName, "..") || strings.Contains(cleanName, ".."+string(os.PathSeparator)) {
			logger.Error("Path traversal detected in ZIP file", zap.String("path", file.Name))
			continue
		}

		// 构建安全的文件路径
		filePath := filepath.Join(absExtractPath, file.Name)

		// 规范化路径
		filePath = filepath.Clean(filePath)

		// 安全检查：确保解压路径在目标目录内
		if !strings.HasPrefix(filePath, absExtractPath) {
			logger.Error("Path traversal detected in ZIP file", zap.String("path", file.Name), zap.String("resolved", filePath))
			continue
		}

		// 额外检查：确保路径不包含任何可疑的目录遍历
		if strings.Contains(filePath, "..") {
			logger.Error("Path traversal detected after resolution", zap.String("path", file.Name), zap.String("resolved", filePath))
			continue
		}

		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(filePath, 0755); err != nil {
				logger.Error("Failed to create directory", zap.String("path", filePath), zap.Error(err))
				continue
			}
			continue
		}

		// 创建父目录
		parentDir := filepath.Dir(filePath)
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			logger.Error("Failed to create parent directory", zap.String("path", parentDir), zap.Error(err))
			continue
		}

		// 解压文件
		rc, err := file.Open()
		if err != nil {
			logger.Error("Failed to open file in ZIP", zap.String("path", file.Name), zap.Error(err))
			continue
		}

		dst, err := os.Create(filePath)
		if err != nil {
			rc.Close()
			logger.Error("Failed to create file", zap.String("path", filePath), zap.Error(err))
			continue
		}

		_, err = io.Copy(dst, rc)
		rc.Close()
		dst.Close()

		if err != nil {
			logger.Error("Failed to copy file content", zap.String("path", filePath), zap.Error(err))
			continue
		}

		// 如果是YAML文件，添加到列表
		if strings.HasSuffix(strings.ToLower(file.Name), ".yaml") || strings.HasSuffix(strings.ToLower(file.Name), ".yml") {
			yamlFiles = append(yamlFiles, filePath)
		}
	}

	return yamlFiles, nil
}

// cleanupTempFiles 清理临时文件
func (s *service) cleanupTempFiles(zipPath, extractPath string) {
	// 删除ZIP文件
	if err := os.Remove(zipPath); err != nil {
		logger.Error("删除POC ZIP文件出错", zap.Error(err))
	}

	// 删除解压目录
	if err := os.RemoveAll(extractPath); err != nil {
		logger.Error("删除POC解压目录出错", zap.Error(err))
	}
}

// isSafeFilePath 检查文件路径是否安全
func (s *service) isSafeFilePath(filePath string) bool {
	// 检查路径是否包含危险字符
	if strings.Contains(filePath, "..") {
		return false
	}

	// 规范化路径
	cleanPath := filepath.Clean(filePath)

	// 检查规范化后的路径是否仍然包含危险字符
	if strings.Contains(cleanPath, "..") {
		return false
	}

	// if filepath.IsAbs(cleanPath) {
	//     return false
	// }

	return true
}

// SyncFromURL 从给定 URL 下载 POC 压缩包并导入，返回导入统计。
// 供「立即同步」按钮与每日定时任务共用：URL 为空时返回错误，不做网络请求。
func (s *service) SyncFromURL(ctx context.Context, url string) (*models.PocImportResponse, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, fmt.Errorf("sync url is empty")
	}

	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	client := &http.Client{Timeout: 15 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download status %d", resp.StatusCode)
	}

	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("poc_sync_%d.zip", time.Now().Unix()))
	f, err := os.Create(tmp)
	if err != nil {
		return nil, fmt.Errorf("create temp: %w", err)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return nil, fmt.Errorf("write temp: %w", err)
	}
	f.Close()
	defer os.Remove(tmp)

	return s.ImportPocWithOptions(ctx, tmp, true)
}
