package fofa

import (
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	fofasvc "github.com/Autumn-27/ScopeSentry/internal/services/fofa"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// FOFA Provider 配置（key / baseUrl）。
//
// fofa.key 本来就存在 config 集合里，provider 每次调用现读，改完立即生效；
// 但此前只能去「系统配置」或环境变量里填。这里给 FOFA 页一个专用入口，
// 写入的仍是同一份 config 文档，不引入第二套配置。

type saveFofaConfigRequest struct {
	Key     string `json:"key"`
	BaseURL string `json:"baseUrl"`
}

// SaveConfig 保存或清除 FOFA 配置
// @Summary 保存 FOFA Key
// @Tags FOFA
// @Router /api/fofa/config [post]
func SaveConfig(c *gin.Context) {
	var req saveFofaConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequestWithError(c, "api.bad_request", err)
		return
	}
	key := strings.TrimSpace(req.Key)
	baseURL := strings.TrimSpace(req.BaseURL)

	collection := mongodb.DB.Collection("config")
	ctx := c.Request.Context()

	// 空 key = 清除（回到 none provider，不再消耗额度）
	if key == "" {
		if _, err := collection.DeleteMany(ctx, bson.M{"name": bson.M{"$in": []string{"fofa.key", "fofa.base_url"}}}); err != nil {
			response.InternalServerError(c, "api.error", err)
			return
		}
	} else {
		if len(key) < 16 || len(key) > 128 {
			response.BadRequestWithError(c, "api.bad_request", errInvalidFofaKey)
			return
		}
		if _, err := collection.UpdateOne(ctx,
			bson.M{"name": "fofa.key"},
			bson.M{"$set": bson.M{"type": "fofa", "name": "fofa.key", "value": key}},
			options.Update().SetUpsert(true),
		); err != nil {
			response.InternalServerError(c, "api.error", err)
			return
		}
		if baseURL != "" {
			if _, err := collection.UpdateOne(ctx,
				bson.M{"name": "fofa.base_url"},
				bson.M{"$set": bson.M{"type": "fofa", "name": "fofa.base_url", "value": baseURL}},
				options.Update().SetUpsert(true),
			); err != nil {
				response.InternalServerError(c, "api.error", err)
				return
			}
		}
	}

	status := fofasvc.DefaultService().ProviderStatus(ctx)
	response.Success(c, status, "api.success")
}
