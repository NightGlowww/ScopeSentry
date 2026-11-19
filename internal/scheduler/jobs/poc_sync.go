package jobs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	pocsvc "github.com/Autumn-27/ScopeSentry/internal/services/poc"
)

// POCSync 从 POC_SYNC_URL 拉取 POC 压缩包并导入。
//
// 背景：新版本不再内置 POC，只能手动上传 zip 导入；上游模板持续更新，
// 没有自动同步则库会停在最后一次手工导入（2026-11-20 实测：最新记录停在 11-19）。
//
// 未设置 POC_SYNC_URL 时直接返回 nil，不做任何网络请求。
func POCSync() error {
	url := strings.TrimSpace(os.Getenv("POC_SYNC_URL"))
	if url == "" {
		return nil
	}
	logger.Info(fmt.Sprintf("poc_sync: start, url=%s", url))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("poc_sync build request: %w", err)
	}
	client := &http.Client{Timeout: 15 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("poc_sync download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("poc_sync download status %d", resp.StatusCode)
	}

	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("poc_sync_%d.zip", time.Now().Unix()))
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("poc_sync create temp: %w", err)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("poc_sync write temp: %w", err)
	}
	f.Close()
	defer os.Remove(tmp)

	result, err := pocsvc.NewService().ImportPocWithOptions(ctx, tmp, true)
	if err != nil {
		return fmt.Errorf("poc_sync import: %w", err)
	}
	logger.Info(fmt.Sprintf("poc_sync done: %+v", result))
	return nil
}

// EnsurePOCSyncJob 在 POC_SYNC_URL 存在时把 poc_sync 计划任务写进 scheduledTasks，
// 让调度器的 loadJobs 在启动时能加载到它（否则只注册了 handler、没有任务记录）。
func EnsurePOCSyncJob(ctx context.Context) error {
	if strings.TrimSpace(os.Getenv("POC_SYNC_URL")) == "" {
		return nil
	}
	coll := mongodb.DB.Collection("scheduledTasks")
	filter := bson.M{"id": "poc_sync"}
	var existing bson.M
	err := coll.FindOne(ctx, filter).Decode(&existing)
	if err == nil {
		return nil
	}
	_, err = coll.InsertOne(ctx, bson.M{
		"id":       "poc_sync",
		"name":     "POC 每日同步",
		"handler":  "poc_sync",
		"schedule": "30 3 * * *",
		"params":   "",
	})
	if err != nil {
		return fmt.Errorf("insert poc_sync scheduled task: %w", err)
	}
	logger.Info("poc_sync scheduled task created (daily 03:30)")
	return nil
}
