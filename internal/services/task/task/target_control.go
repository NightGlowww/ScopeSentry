package task

import (
	"errors"
	"fmt"

	"github.com/Autumn-27/ScopeSentry/internal/constants"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

var (
	// ErrTaskInvalid 任务 ID 非法
	ErrTaskInvalid = errors.New("invalid task id")
	// ErrTaskNotFound 任务不存在
	ErrTaskNotFound = errors.New("task not found")
	// ErrTaskFinished 任务已结束，不能再改目标
	ErrTaskFinished = errors.New("task is finished")
)

// PauseTarget 暂停（跳过）任务里的某个目标（issue #277）。
//
// 语义：还没被节点领取的目标从 TaskInfo:<taskId> 队列里摘掉，并把 taskNum 减一，
// 这样进度仍然能正常走到 100%；已经领取/扫完的目标不会被中断，也不会重复扣减。
func (s *service) PauseTarget(ctx *gin.Context, taskID, target string) error {
	oid, err := primitive.ObjectIDFromHex(taskID)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrTaskInvalid, taskID)
	}
	if target == "" {
		return fmt.Errorf("%w: empty target", ErrTaskInvalid)
	}
	t, err := s.taskRepo.FindByID(ctx.Request.Context(), oid)
	if err != nil {
		return err
	}
	if t == nil {
		return ErrTaskNotFound
	}
	if t.Status == constants.TaskStatusFinished {
		return ErrTaskFinished
	}

	// 幂等：已经在暂停列表里就不重复扣 taskNum
	paused, err := s.taskRepo.ListTargetControls(ctx.Request.Context(), taskID)
	if err != nil {
		return err
	}
	for _, p := range paused {
		if p.Target == target {
			return nil
		}
	}

	removed, err := s.taskRepo.RemoveTaskTarget(ctx.Request.Context(), taskID, target)
	if err != nil {
		return err
	}
	if err := s.taskRepo.AddTargetControl(ctx.Request.Context(), taskID, target); err != nil {
		return err
	}
	if removed == 0 {
		// 目标已被节点领走或已完成：记下状态但不扣 taskNum，等它自己跑完
		return nil
	}
	return s.taskRepo.UpdateOne(ctx.Request.Context(),
		bson.M{"_id": oid, "taskNum": bson.M{"$gt": 0}},
		bson.M{"$inc": bson.M{"taskNum": -1}},
	)
}

// ResumeTarget 恢复被暂停的目标：放回队列头部并加回 taskNum。
func (s *service) ResumeTarget(ctx *gin.Context, taskID, target string) error {
	oid, err := primitive.ObjectIDFromHex(taskID)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrTaskInvalid, taskID)
	}
	if target == "" {
		return fmt.Errorf("%w: empty target", ErrTaskInvalid)
	}
	t, err := s.taskRepo.FindByID(ctx.Request.Context(), oid)
	if err != nil {
		return err
	}
	if t == nil {
		return ErrTaskNotFound
	}
	if err := s.taskRepo.RemoveTargetControl(ctx.Request.Context(), taskID, target); err != nil {
		return err
	}
	if t.Status == constants.TaskStatusFinished {
		// 已完成的任务不把目标塞回队列，只清掉暂停记录
		return nil
	}
	if err := s.taskRepo.PrependTaskTarget(ctx.Request.Context(), taskID, target); err != nil {
		return err
	}
	return s.taskRepo.UpdateOne(ctx.Request.Context(), bson.M{"_id": oid}, bson.M{"$inc": bson.M{"taskNum": 1}})
}

// GetPausedTargets 返回某任务被暂停的目标集合（进度详情里标记用）。
func (s *service) GetPausedTargets(ctx *gin.Context, taskID string) (map[string]bool, error) {
	controls, err := s.taskRepo.ListTargetControls(ctx.Request.Context(), taskID)
	if err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(controls))
	for _, c := range controls {
		result[c.Target] = true
	}
	return result, nil
}
