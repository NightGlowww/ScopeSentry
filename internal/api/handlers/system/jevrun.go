package system

// 手动触发一轮 LLM 批量判定。
//
// 为什么需要：jev.Start 是 10 分钟一次的定时器，调参/验证时没法立刻看到效果；
// 运维上也需要一个「现在就判一轮」的入口。
import (
	"context"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/services/jev"
	"github.com/gin-gonic/gin"
)

// JevRunOnce 立即执行一轮批量判定。
func JevRunOnce(c *gin.Context) {
	if !jev.Enabled() {
		response.BadRequest(c, "api.bad_request", nil)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 180*time.Second)
	defer cancel()
	confirmed, rejected, kept, err := jev.RunOnce(ctx)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, gin.H{"confirmed": confirmed, "rejected": rejected, "needsHuman": kept}, "api.success")
}
