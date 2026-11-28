package project

import (
	"errors"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/api/response"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/services/project"
	"github.com/gin-gonic/gin"
)

var projectService project.Service

func init() {
	projectService = project.NewService()
}

// GetProjectsByTag godoc
// @Summary 获取按标签分组的项目列表
// @Description 获取所有项目并按标签分组
// @Tags 项目
// @Accept json
// @Produce json
// @Success 200 {object} response.Response{data=[]project.TagGroup}
// @Router /api/projects/all [get]
func GetProjectsByTag(c *gin.Context) {
	result, err := projectService.GetProjectsByTag(c)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}

	response.Success(c, gin.H{
		"list": result,
	}, "")
}

// GetProjectsData 获取项目数据（含标签聚合）
// @Summary 获取项目数据
// @Description 返回每个标签下的分页项目列表与标签计数
// @Tags 项目
// @Accept json
// @Produce json
// @Param data body models.ProjectListRequest true "查询参数"
// @Success 200 {object} response.Response{data=models.ProjectListResponse}
// @Failure 400 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/project/data [post]
func GetProjectsData(c *gin.Context) {
	var req models.ProjectListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	if req.PageIndex <= 0 {
		req.PageIndex = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 10
	}
	data, err := projectService.GetProjectsData(c, req.Search, req.PageIndex, req.PageSize)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, data, "")
}

// GetProjectContent 获取项目内容
// @Summary 获取项目目标与基础信息
// @Tags 项目
// @Accept json
// @Produce json
// @Param data body models.ProjectContentRequest true "项目ID"
// @Success 200 {object} response.Response{data=models.ProjectContentResponse}
// @Failure 400 {object} response.Response
// @Failure 404 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/project/content [post]
func GetProjectContent(c *gin.Context) {
	var req models.ProjectContentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	data, err := projectService.GetProjectContent(c, req.ID)
	if err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	if data == nil {
		response.NotFound(c, "api.not_found", nil)
		return
	}
	response.Success(c, data, "")
}

func AddProject(c *gin.Context) {
	var req models.Project
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	// 名称是项目的唯一标识：空名字会建出一张没有标题的卡片（页面上就是一块空白），
	// 而 models.Project 没有 binding 校验 —— 以前 POST {} 能直接建成功（2026-11-28 实测）。
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		response.BadRequest(c, "api.required.name", nil)
		return
	}
	err := projectService.AddProject(c, &req)
	if err != nil {
		// 重名是用户输入问题，不是服务端故障：以前一律 500「api.error」。
		if errors.Is(err, project.ErrProjectNameRequired) {
			response.BadRequest(c, "api.required.name", nil)
			return
		}
		if errors.Is(err, project.ErrProjectNameTaken) {
			response.BadRequest(c, "api.project.name_exists", nil)
			return
		}
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, nil, "api.success")
	return
}

func UpdatePorject(c *gin.Context) {
	var req models.UpdateProject
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	// 改名同样不能清空：空名字在列表里就是一张没有标题的卡片。
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		response.BadRequest(c, "api.required.name", nil)
		return
	}
	err := projectService.UpdateProject(c, &req)
	if err != nil {
		if errors.Is(err, project.ErrProjectNameRequired) {
			response.BadRequest(c, "api.required.name", nil)
			return
		}
		if errors.Is(err, project.ErrProjectNameTaken) {
			response.BadRequest(c, "api.project.name_exists", nil)
			return
		}
		if errors.Is(err, project.ErrProjectNotFound) {
			// 改一个不存在的项目以前回 200 success（repo 吞掉了 MatchedCount=0）
			response.NotFound(c, "api.not_found", nil)
			return
		}
		response.InternalServerError(c, "api.error", err)
		return
	}
	response.Success(c, nil, "api.success")
	return
}

func DeleteProject(c *gin.Context) {
	var req models.DeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "api.bad_request", err)
		return
	}
	// 先确认项目存在：这个 handler 以前**什么都不回**（200 空 body），
	// 前端 axios 拿到空响应也当成功，删错 id 完全没有反馈（2026-11-28 实测）。
	existing := make([]string, 0, len(req.IDs))
	for _, id := range req.IDs {
		if p, err := projectService.GetProjectContent(c, id); err == nil && p != nil {
			existing = append(existing, id)
		}
	}
	if len(existing) == 0 {
		response.NotFound(c, "api.project.not_found", nil)
		return
	}
	if err := projectService.DeleteProjects(c, existing, req.DeleateAssets); err != nil {
		response.InternalServerError(c, "api.error", err)
		return
	}
	data := map[string]any{"deleted": len(existing), "deleteAssets": req.DeleateAssets}
	if len(existing) < len(req.IDs) {
		data["requested"] = len(req.IDs)
	}
	response.Success(c, data, "api.success")
}
