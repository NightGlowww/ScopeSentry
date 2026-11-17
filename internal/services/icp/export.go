package icp

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// CSV 导出：任务结果按 createdAt 升序流式写出。
// 调用方（handler）负责 UTF-8 BOM 与响应头；本方法只产出表头 + 数据行。

// icpExportHeader 导出列（与 icp_result 字段一一对应）
var icpExportHeader = []string{
	"taskId", "domain", "company", "icpNo", "siteName", "type", "source", "createdAt",
}

// Export 把任务结果写成 CSV，返回数据行数（不含表头）
func (s *Service) Export(ctx context.Context, taskID string, w io.Writer) (int64, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return 0, fmt.Errorf("%w: taskId 不能为空", ErrInvalidInput)
	}
	if _, err := s.tasks.Get(ctx, taskID); err != nil {
		return 0, err
	}

	writer := csv.NewWriter(w)
	if err := writer.Write(icpExportHeader); err != nil {
		return 0, err
	}

	var rows int64
	iterErr := s.results.Iterate(ctx, taskID, func(item *models.IcpResult) error {
		record := []string{
			item.TaskID,
			item.Domain,
			item.Company,
			item.ICPNo,
			item.SiteName,
			item.Type,
			item.Source,
			item.CreatedAt.Format(time.RFC3339),
		}
		if err := writer.Write(record); err != nil {
			return err
		}
		rows++
		return nil
	})
	writer.Flush()
	if iterErr != nil {
		return rows, iterErr
	}
	if err := writer.Error(); err != nil {
		return rows, err
	}
	return rows, nil
}
