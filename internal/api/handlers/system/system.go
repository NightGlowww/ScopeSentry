// Package system -----------------------------
// @file      : system.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2025/5/2 23:01
// -------------------------------------------
package system

import (
	"encoding/json"
	"fmt"
	"github.com/Autumn-27/ScopeSentry/internal/constants"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"net/http"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/database/redis"
	"github.com/gin-gonic/gin"
	"github.com/valyala/fasthttp"
)

type VersionResponse struct {
	Code int `json:"code"`
	Data struct {
		List []VersionInfo `json:"list"`
	} `json:"data"`
}

type VersionInfo struct {
	Name     string `json:"name"`
	CVersion string `json:"cversion"`
	LVersion string `json:"lversion"`
	Msg      string `json:"msg"`
}

type RemoteVersion struct {
	Server    string `json:"server"`
	ServerMsg string `json:"server_msg"`
	Scan      string `json:"scan"`
	ScanMsg   string `json:"scan_msg"`
}

// VersionSourceURL 版本检查来源（本仓库）。发布新版本后只需更新本仓库 version.json。
const VersionSourceURL = "https://raw.githubusercontent.com/NightGlowww/ScopeSentry/main/version.json"

// GetSystemVersion @Summary      获取系统及节点版本信息
// @Description  获取 ScopeSentry 服务端及所有节点的当前版本信息
// @Tags         System
// @Accept       json
// @Produce      json
// @Success      200  {object}  response.SuccessResponse{data=VersionListResponse}
// @Failure      400  {object}  response.BadRequestResponse
// @Failure      401  {object}  response.UnauthorizedResponse
// @Failure      404  {object}  response.NotFoundResponse
// @Failure      500  {object}  response.InternalServerErrorResponse
// @Router       /system/version [get]
func GetSystemVersion(c *gin.Context) {
	var (
		serverLVersion string
		serverMsg      string
		scanLVersion   string
		scanMsg        string
	)

	client := &fasthttp.Client{
		ReadTimeout: 10 * time.Second,
	}

	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	// 版本检查只读**本仓库**的 version.json：此前读上游仓库，
	// 导致我们发到 2.0.x 了界面还显示「最新 1.9.4」。本项目与上游无运行时关系。
	req.SetRequestURI(VersionSourceURL)
	req.Header.SetMethod("GET")

	err := client.Do(req, resp)
	if err == nil && resp.StatusCode() == http.StatusOK {
		var version RemoteVersion
		if err := json.Unmarshal(resp.Body(), &version); err == nil {
			serverLVersion = version.Server
			serverMsg = version.ServerMsg
			scanLVersion = version.Scan
			scanMsg = version.ScanMsg
		}
	}

	resultList := []VersionInfo{
		{
			Name:     "ScopeSentry-Server",
			CVersion: constants.Version,
			LVersion: serverLVersion,
			Msg:      serverMsg,
		},
	}

	ctx := c.Request.Context()
	keys, err := redis.Client.Keys(ctx, "node:*").Result()
	if err != nil {
		response.InternalServerError(c, "api.system.version.redis_error", err)
		return
	}
	for _, key := range keys {
		name := key[5:]
		// node:<name> 是 hash，但同一个前缀下还有别的类型：
		// 例如 node:<name>:currentTask 是 string（扫描端声明「当前在跑哪个任务」）。
		// 以前不分类型直接 HGetAll，遇到它整页 500：「WRONGTYPE ... wrong kind of value」。
		if strings.Contains(name, ":") {
			continue
		}
		hashData, err := redis.Client.HGetAll(ctx, key).Result()
		if err != nil {
			// 单个节点的键类型异常不该让整个版本接口失败。
			logger.Error(fmt.Sprintf("system version: skip key %s: %v", key, err))
			continue
		}
		resultList = append(resultList, VersionInfo{
			Name:     name,
			CVersion: hashData["version"],
			LVersion: scanLVersion,
			Msg:      scanMsg,
		})
	}

	response.Success(c, VersionListResponse{
		List: resultList,
	}, "api.system.version.success")
}

// VersionListResponse 版本列表响应结构
// 用于统一响应格式
// @Description 版本信息列表
// @Author Autumn
// @Date 2025-05-02
// @Contact rainy-autumn@outlook.com
type VersionListResponse struct {
	List []VersionInfo `json:"list"`
}
