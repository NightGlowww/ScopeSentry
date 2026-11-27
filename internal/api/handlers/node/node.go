// Package node -----------------------------
// @file      : node.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2025/4/30 15:36
// -------------------------------------------
package node

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/services/node"
	"github.com/gin-gonic/gin"
)

var nodeService node.Service

// GetNode @Summary      获取节点数据
// @Description  获取所有在线节点的数据
// @Tags         node
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Success      200  {object}  response.SuccessResponse{data=object{list=[]models.NodeData}}
// @Failure      401  {object}  response.UnauthorizedResponse
// @Failure      500  {object}  response.InternalServerErrorResponse
// @Router       /node [get]
func GetNode(c *gin.Context) {
	result, err := nodeService.GetNodeData(c.Request.Context(), false)
	if err != nil {
		response.InternalServerError(c, "api.node.get_data.failed", err)
		return
	}

	response.Success(c, response.DataResponse{List: result}, "")
}

// GetNodeOnline @Summary      获取在线节点
// @Description  获取所有在线节点的数据
// @Tags         node
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Success      200  {object}  response.SuccessResponse{data=object{list=[]string}}
// @Failure      401  {object}  response.UnauthorizedResponse
// @Failure      500  {object}  response.InternalServerErrorResponse
// @Router       /node/online [get]
func GetNodeOnline(c *gin.Context) {
	result, err := nodeService.GetNodeData(c.Request.Context(), true)
	if err != nil {
		response.InternalServerError(c, "api.node.get_data.failed", err)
		return
	}

	// 仅返回 name 字段
	names := make([]string, 0, len(result))
	for _, item := range result {
		names = append(names, item.Name)
	}

	response.Success(c, response.DataResponse{List: names}, "")
}

// ClearNodeQueue 清空某节点的待领取任务队列。
// @Summary      清空节点队列
// @Tags         Node
// @Accept       json
// @Produce      json
// @Router       /node/queue/clear [post]
func ClearNodeQueue(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	n, err := nodeService.ClearNodeQueue(c.Request.Context(), req.Name)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, map[string]interface{}{"cleared": n}, "api.success")
}

// NodeConfigUpdateRequest 节点配置更新请求
type NodeConfigUpdateRequest struct {
	Name          string `json:"name" binding:"required"`
	OldName       string `json:"oldName" binding:"omitempty"`
	ModulesConfig string `json:"ModulesConfig" binding:"required"`
	State         bool   `json:"state" binding:"required"`
}

// ConfigUpdate 更新节点配置
// @Summary 更新节点配置
// @Tags node
// @Accept json
// @Produce json
// @Param data body NodeConfigUpdateRequest true "节点配置"
// @Success 200 {object} response.Response
// @Failure 400 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/node/config/update [post]
func ConfigUpdate(c *gin.Context) {
	var req NodeConfigUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	old := req.OldName
	if strings.TrimSpace(old) == "" {
		old = req.Name
	}
	msg := models.Message{
		Name:    old,
		Type:    "nodeConfig",
		Content: req.Name + "[*]" + fmt.Sprintf("%v", req.State) + "[*]" + req.ModulesConfig,
	}
	if err := nodeService.RefreshConfig(c, msg); err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, nil, "api.success")
}

// NodeDeleteRequest 删除节点请求
type NodeDeleteRequest struct {
	Names []string `json:"names" binding:"required"`
}

// Delete 删除节点
// @Summary 删除节点
// @Tags node
// @Accept json
// @Produce json
// @Param data body NodeDeleteRequest true "节点名称列表"
// @Success 200 {object} response.Response
// @Failure 400 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/node/delete [post]
func Delete(c *gin.Context) {
	var req NodeDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if len(req.Names) == 0 {
		response.BadRequest(c, "api.bad_request", nil)
		return
	}
	ctx := context.Background()
	if err := nodeService.DeleteNodes(ctx, req.Names); err != nil {
		// 节点不存在是调用方参数问题（名字打错/节点已下线），不是服务端故障
		response.NotFound(c, "api.error", err)
		return
	}
	response.Success(c, map[string]int{"deleted": len(req.Names)}, "api.success")
}

// NodeLogRequest 节点日志请求
type NodeLogRequest struct {
	Name string `json:"name" binding:"required"`
}

// GetLogs 获取节点日志
// @Summary 获取节点日志
// @Tags node
// @Accept json
// @Produce json
// @Param data body NodeLogRequest true "节点名称"
// @Success 200 {object} response.Response{data=object{logs=string}}
// @Failure 400 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/node/log [post]
func GetLogs(c *gin.Context) {
	var req NodeLogRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	ctx := context.Background()
	logs, err := nodeService.GetNodeLogs(ctx, req.Name)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, gin.H{"logs": logs}, "")
}

// GetNodePlugin 获取节点插件信息
// @Summary 获取节点插件信息
// @Tags node
// @Accept json
// @Produce json
// @Param data body NodeLogRequest true "节点名称"
// @Success 200 {object} response.Response{data=object{list=[]models.NodePluginInfo}}
// @Failure 400 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/node/plugin [post]
func GetNodePlugin(c *gin.Context) {
	var req NodeLogRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	ctx := context.Background()
	plugins, err := nodeService.GetNodePlugin(ctx, req.Name)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, response.DataResponse{List: plugins}, "")
}

// RestartNode 重启节点
// @Summary 重启节点
// @Tags node
// @Accept json
// @Produce json
// @Param data body NodeLogRequest true "节点名称"
// @Success 200 {object} response.Response
// @Failure 400 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/node/restart [post]
func RestartNode(c *gin.Context) {
	var req NodeLogRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	ctx := context.Background()
	if err := nodeService.RestartNode(ctx, req.Name); err != nil {
		response.NotFound(c, "api.error", err)
		return
	}
	response.Success(c, map[string]string{"node": req.Name}, "api.success")
}

func init() {
	nodeService = node.NewService()
}

// RebalanceQueue 把待开始任务从积压节点迁到空闲节点。
// 只迁 status=0 的任务：运行中的任务已在原节点上跑，换节点会丢进度。
func RebalanceQueue(c *gin.Context) {
	var req struct {
		From  string `json:"from" binding:"required"`
		To    string `json:"to" binding:"required"`
		Limit int    `json:"limit"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	moved, err := nodeService.MoveQueuedTasks(c.Request.Context(), req.From, req.To, req.Limit)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, map[string]interface{}{"moved": moved}, "api.success")
}

// MemPeaks 返回每个节点最近一小时的内存峰值/均值（用于看压力趋势）。
func MemPeaks(c *gin.Context) {
	out := map[string]map[string]float64{}
	// highMinutes：连续高压分钟数，用来区分「刚冲高」和「已经饱和 45 分钟」
	high := nodeService.MemHighMinutes(c.Request.Context())
	for name, v := range nodeService.MemPeaks(c.Request.Context()) {
		out[name] = map[string]float64{
			"max":         math.Round(v[0]*10) / 10,
			"avg":         math.Round(v[1]*10) / 10,
			"highMinutes": float64(high[name]),
		}
	}
	response.Success(c, out, "api.success")
}
