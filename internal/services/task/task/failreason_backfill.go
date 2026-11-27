package task

import (
	"context"

	"github.com/Autumn-27/ScopeSentry/internal/failreason"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// BackfillFailCodes 给历史失败任务补 failCode/failParams，返回更新条数。
// 句式解析在 internal/failreason（纯函数，有单测）；这里只负责查与写。
// 幂等：只处理 status=4 且 failCode 为空的记录。
func (s *service) BackfillFailCodes(ctx context.Context) (int, error) {
	filter := bson.M{
		"status":     4,
		"failReason": bson.M{"$nin": []interface{}{nil, ""}},
		"$or": []bson.M{
			{"failCode": bson.M{"$exists": false}},
			{"failCode": ""},
		},
	}
	tasks, err := s.taskRepo.Find(ctx, filter, options.Find().SetLimit(200))
	if err != nil {
		return 0, err
	}
	updated := 0
	for _, t := range tasks {
		code, params := failreason.Parse(t.FailReason)
		if code == "" {
			continue
		}
		set := bson.M{"failCode": code}
		if len(params) > 0 {
			set["failParams"] = params
		}
		// 注意：模型里 ID 是 mongo 的 _id，TaskID 才是业务 id 字段
		if uerr := s.taskRepo.UpdateOne(ctx, bson.M{"_id": t.ID}, bson.M{"$set": set}); uerr != nil {
			continue
		}
		updated++
	}
	return updated, nil
}
