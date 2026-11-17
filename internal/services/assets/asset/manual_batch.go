package asset

import (
	"errors"
	"fmt"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/gin-gonic/gin"
)

// maxBatchErrors 结果里最多回传多少条失败原因，避免响应体过大
const maxBatchErrors = 20

// AddAssets 批量录入资产：逐条复用 AddAsset 的校验与唯一索引处理，
// 把「成功 / 重复 / 非法」分开计数，前端才能给出「导入 8 条，重复 2 条」这种提示。
func (s *service) AddAssets(ctx *gin.Context, req *models.AssetAddBatchRequest) (*models.AssetAddBatchResult, error) {
	if len(req.Items) == 0 {
		return nil, fmt.Errorf("%w: items is required", ErrAssetInvalid)
	}
	result := &models.AssetAddBatchResult{}
	for i, item := range req.Items {
		if item.Project == "" {
			item.Project = req.Project
		}
		if len(item.Tags) == 0 {
			item.Tags = req.Tags
		}
		if _, err := s.AddAsset(ctx, &item); err != nil {
			switch {
			case errors.Is(err, ErrAssetExists):
				result.Duplicated++
			case errors.Is(err, ErrAssetInvalid):
				result.Invalid++
				if len(result.Errors) < maxBatchErrors {
					result.Errors = append(result.Errors, fmt.Sprintf("item %d: %v", i, err))
				}
			default:
				return result, fmt.Errorf("failed to add item %d: %w", i, err)
			}
			continue
		}
		result.Inserted++
	}
	return result, nil
}
