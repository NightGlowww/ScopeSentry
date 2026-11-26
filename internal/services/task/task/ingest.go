package task

// 项目优先接入（project-first ingest）。
//
// 背景（2026-11-27 用户提出）：HuntDeck 推过来的只是一条条任务，但它背后其实是一个项目，
// 一个项目下会陆续有多批资产/多轮扫描。现在只能靠「先建任务、事后同步到项目」倒着补，
// 结果项目页像个摆设、任务一盘散沙。
//
// 这个接口把方向反过来：调用方只说「往哪个项目里加这批目标」，
//   · 项目不存在 → 建项目；
//   · 项目已存在 → 把新目标并进项目范围；
//   · 两种情况都建一条绑定到该项目的任务（bindProject），下次 HuntDeck 有新资产直接再来一次即可。
// 幂等点在项目侧：同一批目标重复提交时 SyncTargets 只报新增数，不会重复写范围。

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/services/task/tasksync"
	"github.com/Autumn-27/ScopeSentry/internal/utils/helper"
	"github.com/gin-gonic/gin"
)

// IngestRequest 项目优先接入请求。
type IngestRequest struct {
	ProjectID   string   `json:"projectId"`   // 已有项目 ID（优先）
	ProjectName string   `json:"projectName"` // 项目名；不存在则按它新建
	Tag         string   `json:"tag"`         // 新建项目时的标签
	Targets     string   `json:"targets"`     // 一行一个目标（域名/IP/CIDR/URL）
	Template    string   `json:"template"`    // 扫描模板 id，留空用 default
	Node        []string `json:"node"`        // 目标节点；留空=全部节点
	AutoStart   bool     `json:"autoStart"`   // 建完立即开跑
}

// IngestResult 返回给调用方的事实，够它判断「这次是新建还是追加」。
type IngestResult struct {
	ProjectID      string `json:"projectId"`
	ProjectName    string `json:"projectName"`
	ProjectCreated bool   `json:"projectCreated"`
	TaskID         string `json:"taskId"`
	TaskName       string `json:"taskName"`
	TargetCount    int    `json:"targetCount"`
	AddedTargets   int    `json:"addedTargets"`
	Started        bool   `json:"started"`
	StartError     string `json:"startError,omitempty"`
}

// IngestToProject 把一批目标接进指定项目，并建一条绑定该项目的任务。
func (s *service) IngestToProject(ctx *gin.Context, req IngestRequest) (*IngestResult, error) {
	raw := strings.TrimSpace(req.Targets)
	if raw == "" {
		return nil, fmt.Errorf("targets 不能为空")
	}
	list, err := helper.GetTargetList(raw, "")
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("targets 解析后为空")
	}

	projectID := strings.TrimSpace(req.ProjectID)
	name := strings.TrimSpace(req.ProjectName)
	created := false
	option := tasksync.OptionExisting

	if projectID == "" {
		if name == "" {
			return nil, fmt.Errorf("projectId 或 projectName 必须给一个")
		}
		existing, ferr := s.projectService.FindByName(ctx.Request.Context(), name)
		if ferr != nil {
			return nil, ferr
		}
		if existing == nil {
			option = tasksync.OptionNew
			created = true
		} else {
			projectID = existing.ID.Hex()
		}
	} else {
		// 传了 projectId 时要把项目名查回来：任务名是按项目名拼的，
		// 查不到就会拼出「6afb1e2e…-20261127-0225」这种一串 ObjectID 的任务名（2026-11-27 实测）。
		p, ferr := s.projectService.FindByID(ctx.Request.Context(), projectID)
		if ferr != nil {
			return nil, ferr
		}
		if p == nil {
			return nil, fmt.Errorf("项目不存在: %s", projectID)
		}
		name = p.Name
	}

	// 项目侧：不存在就建，存在就把新目标并进范围。
	pid, added, serr := s.projectService.SyncTargets(ctx, option, projectID, name, req.Tag, list)
	if serr != nil {
		return nil, serr
	}
	projectID = pid

	// 任务侧：一条任务 = 这一批新目标，绑定到项目，后续在项目页里能看到整串历史。
	base := fmt.Sprintf("%s-%s", name, time.Now().Format("20060102-1504"))
	taskName := base
	for i := 2; i < 50; i++ {
		exists, cerr := s.CheckTaskNameExists(ctx, taskName)
		if cerr != nil {
			return nil, cerr
		}
		if !exists {
			break
		}
		taskName = fmt.Sprintf("%s-%d", base, i)
	}

	t := &models.Task{
		Name:         taskName,
		Node:         req.Node,
		AllNode:      len(req.Node) == 0,
		Target:       strings.Join(list, "\n"),
		Template:     req.Template,
		BindProject:  projectID,
		TargetSource: "general", // 必须显式给：为空时 Insert 走不到任何分支，目标会是空串（踩过）
	}
	id, ierr := s.commonService.Insert(context.Background(), t)
	if ierr != nil {
		return nil, ierr
	}

	res := &IngestResult{
		ProjectID:      projectID,
		ProjectName:    name,
		ProjectCreated: created,
		TaskID:         id,
		TaskName:       taskName,
		TargetCount:    len(list),
		AddedTargets:   added,
	}
	if req.AutoStart {
		if serr := s.startOne(ctx, id); serr != nil {
			// 建任务成功但没跑起来：把原因带回去，别让调用方以为在跑
			res.StartError = serr.Error()
		} else {
			res.Started = true
		}
	}
	return res, nil
}
