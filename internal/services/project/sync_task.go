package project

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/services/task/tasksync"
	"github.com/Autumn-27/ScopeSentry/internal/utils/helper"
	"go.mongodb.org/mongo-driver/mongo"
)

// SyncTaskTargets 任务完成后的自动同步：把目标并入指定项目，并同步重算资产关联。
//
// 语义与 SyncTargets(option=existing) 一致，但只用 context.Context：
// 自动同步发生在任务完成的后台 goroutine 里，没有 gin.Context 可用。
// 返回真正新增的目标数量。
func (s *service) SyncTaskTargets(ctx context.Context, projectID string, targets []string) (int, error) {
	if len(targets) == 0 {
		return 0, fmt.Errorf("任务目标为空，无法同步到项目")
	}
	existing, err := s.projectRepo.FindByID(ctx, projectID)
	if err != nil {
		return 0, err
	}
	if existing == nil {
		return 0, fmt.Errorf("项目不存在: %w", ErrProjectNotFound)
	}

	current, err := s.projectRepo.GetTarget(ctx, projectID)
	if err != nil {
		if !errors.Is(err, mongo.ErrNoDocuments) {
			return 0, err
		}
		current = ""
	}

	merged := tasksync.MergeExisting(current, targets)
	rawTarget := strings.Join(merged, "\n")
	targetList, err := helper.GetTargetList(rawTarget, existing.Ignore)
	if err != nil {
		return 0, err
	}
	rootDomains := BuildRootDomains(targetList)

	updated := &models.UpdateProject{
		ID:             existing.ID.Hex(),
		Name:           existing.Name,
		Tag:            existing.Tag,
		Logo:           existing.Logo,
		Target:         "",
		ScheduledTasks: existing.ScheduledTasks,
		Hour:           existing.Hour,
		AllNode:        existing.AllNode,
		Node:           existing.Node,
		Duplicates:     existing.Duplicates,
		Ignore:         existing.Ignore,
		Template:       existing.Template,
		Tp:             existing.Tp,
		RootDomains:    rootDomains,
		AssetCount:     existing.AssetCount,
	}
	if err := s.projectRepo.UpdateProject(ctx, updated); err != nil {
		return 0, err
	}
	if err := s.projectRepo.UpsertProjectTarget(ctx, projectID, rawTarget); err != nil {
		return 0, err
	}
	if err := s.projectRepo.UpdateAssetsProject(ctx, rootDomains, projectID, true); err != nil {
		return 0, err
	}
	if err := s.recalcAssetCount(ctx, projectID); err != nil {
		return 0, err
	}
	return tasksync.CountNew(current, targets), nil
}
