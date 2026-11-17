package icp

import (
	"context"
	"fmt"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// backfill_details：对任务内已有结果补 company / icpNo。
//
// 逐条调用 provider（按 domain 反查），只填「当前为空」的字段，不覆盖已有值；
// 单条失败不中断整批，失败逐条记进 failures（同时计入 stats.failed）。
// 走 queryWithRetry，所以 429/5xx 一样有指数退避。

const (
	// defaultBackfillLimit 一次补全最多扫多少条（避免一次请求跑太久）
	defaultBackfillLimit = 500
	// maxBackfillFailures 响应里最多回传多少条失败明细
	maxBackfillFailures = 50
)

// Backfill 补全任务结果的 company / icpNo / siteName
func (s *Service) Backfill(ctx context.Context, taskID string) (*BackfillSummary, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("%w: taskId 不能为空", ErrInvalidInput)
	}
	task, err := s.tasks.Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	provider := s.providerFor(ctx, task.Provider)
	if available, reason := provider.Status(); !available {
		return nil, fmt.Errorf("%w: provider %s 不可用：%s", ErrProviderUnavailable, provider.Name(), reason)
	}

	items, err := s.results.MissingDetails(ctx, taskID, defaultBackfillLimit)
	if err != nil {
		return nil, err
	}
	summary := &BackfillSummary{
		TaskID:   taskID,
		Scanned:  int64(len(items)),
		Failures: make([]BackfillFailure, 0),
	}

	for _, item := range items {
		records, qErr := s.queryWithRetry(ctx, taskID, provider, models.IcpTaskTypeDomain, item.Domain)
		if qErr != nil {
			summary.Failed++
			if len(summary.Failures) < maxBackfillFailures {
				summary.Failures = append(summary.Failures, BackfillFailure{
					ResultID: item.ID.Hex(),
					Domain:   item.Domain,
					Error:    qErr.Error(),
				})
			}
			continue
		}

		company, icpNo, siteName := pickDetails(item, records)
		if company == "" && icpNo == "" && siteName == "" {
			continue
		}
		if uErr := s.results.UpdateDetails(ctx, item.ID, company, icpNo, siteName); uErr != nil {
			summary.Failed++
			if len(summary.Failures) < maxBackfillFailures {
				summary.Failures = append(summary.Failures, BackfillFailure{
					ResultID: item.ID.Hex(),
					Domain:   item.Domain,
					Error:    uErr.Error(),
				})
			}
			continue
		}
		summary.Updated++
	}

	s.appendLog(ctx, taskID, IcpLogLevelInfo, fmt.Sprintf(
		"补全详情：扫描=%d 更新=%d 失败=%d", summary.Scanned, summary.Updated, summary.Failed))
	return summary, nil
}

// pickDetails 从 provider 记录里挑出能补的字段：
// 只补条目里为空的字段，已有值一律保留（不覆盖用户/历史结果）。
func pickDetails(item models.IcpResult, records []models.ICPRecord) (string, string, string) {
	company := ""
	icpNo := ""
	siteName := ""
	for _, rec := range records {
		rec = models.NormalizeIcpRecord(rec)
		if company == "" && item.Company == "" && rec.Company != "" {
			company = rec.Company
		}
		if icpNo == "" && item.ICPNo == "" && rec.ICPNo != "" {
			icpNo = rec.ICPNo
		}
		if siteName == "" && item.SiteName == "" && rec.SiteName != "" {
			siteName = rec.SiteName
		}
		if company != "" && icpNo != "" && siteName != "" {
			break
		}
	}
	return company, icpNo, siteName
}
