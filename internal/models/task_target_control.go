package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// TaskTargetControl 记录任务里被暂停（跳过）的目标（issue #277）。
//
// 语义是「这个目标先不扫」：还没被节点领取的目标会从 TaskInfo:<taskId> 队列里摘掉，
// 同时把 taskNum 减一，这样进度仍然能正常走到 100%；恢复时再放回队列并加回来。
// 已经被节点领走、正在扫的目标只能等节点自己响应 stop_task，这里不会强行中断。
type TaskTargetControl struct {
	ID       primitive.ObjectID `bson:"_id,omitempty" json:"_id"`
	TaskID   string             `bson:"taskId" json:"taskId"`
	Target   string             `bson:"target" json:"target"`
	PausedAt time.Time          `bson:"pausedAt" json:"pausedAt"`
}

// TaskTargetControlRequest 暂停/恢复单个目标的请求体。
type TaskTargetControlRequest struct {
	TaskID string `json:"taskId" binding:"required"`
	Target string `json:"target" binding:"required"`
}
