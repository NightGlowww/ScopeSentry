package task

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// AssignProject 把已有任务归到某个项目下（projectID 传空 = 解除归属）。
//
// 为什么需要它：bindProject 是 2026-11-27 才引入的字段，此前 229 条历史任务全是空值，
// 结果「项目」页面除了新建的任务什么都看不到，项目就是一个空壳。
// 有了补录通道，用户可以把历史任务挂到对应项目下，项目才真正能回答「这个项目跑过什么」。
func (s *service) AssignProject(ctx *gin.Context, ids []string, projectID string) (int, error) {
	if len(ids) == 0 {
		return 0, fmt.Errorf("未选择任务")
	}
	if len(ids) > 500 {
		return 0, fmt.Errorf("单次最多补录 500 条（收到 %d 条）", len(ids))
	}
	if projectID != "" {
		p, err := s.projectService.FindByID(ctx.Request.Context(), projectID)
		if err != nil {
			return 0, fmt.Errorf("查询项目失败: %v", err)
		}
		if p == nil {
			return 0, fmt.Errorf("项目不存在: %s", projectID)
		}
	}
	oids := make([]primitive.ObjectID, 0, len(ids))
	for _, id := range ids {
		oid, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			continue
		}
		oids = append(oids, oid)
	}
	if len(oids) == 0 {
		return 0, fmt.Errorf("没有合法的任务 ID")
	}
	filter := bson.M{"_id": bson.M{"$in": oids}}
	if projectID == "" {
		res, err := s.taskRepo.UpdateMany(ctx, filter, bson.M{"$unset": bson.M{"bindProject": ""}})
		if err != nil {
			return 0, err
		}
		return int(res.ModifiedCount), nil
	}
	res, err := s.taskRepo.UpdateMany(ctx, filter, bson.M{"$set": bson.M{"bindProject": projectID}})
	if err != nil {
		return 0, err
	}
	return int(res.ModifiedCount), nil
}
