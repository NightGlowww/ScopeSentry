package mcp

// 计划任务（周期扫描）的写入口。
//
// 之前 MCP 能列、能看详情、能立即跑、能开关，唯独不能建/改/删 ——
// 自动化没法把「每天扫一遍这批目标」编排进去，只能人去界面点。

import (
	"context"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// scheduledTaskInput 是创建/更新共用的字段子集。
// 只暴露「周期扫描」需要的那些：其余字段（统计/进度/失败原因）都是运行期产物。
// 布尔与计数字段用指针：nil = 不改，避免「没传就把开关关掉」。
type scheduledTaskInput struct {
	// name/target 用 omitempty：update 只改一个周期字段时不该被迫把目标整段回传。
	// 必填性由 create 分支自己校验（schema 里标 required 会让 update 也强制带这两个字段）。
	Name           string   `json:"name,omitempty" jsonschema:"计划任务名；创建时必填"`
	Target         string   `json:"target,omitempty" jsonschema:"扫描目标，多行文本；创建时必填（空目标只会产出空任务）"`
	Ignore         string   `json:"ignore,omitempty" jsonschema:"忽略规则（多行文本），可空"`
	Template       string   `json:"template,omitempty" jsonschema:"扫描模板 id；空 = 用默认模板"`
	Node           []string `json:"node,omitempty" jsonschema:"指定节点名列表；配合 allNode=false 使用"`
	AllNode        *bool    `json:"allNode,omitempty" jsonschema:"是否所有节点都可执行；不传按服务端默认"`
	Project        []string `json:"project,omitempty" jsonschema:"资产归属项目 id 列表"`
	BindProject    string   `json:"bindProject,omitempty" jsonschema:"任务归属项目 id（项目页按它列任务）"`
	CycleType      string   `json:"cycleType,omitempty" jsonschema:"周期类型：nhours / ndays / nweeks / nmonths；默认 nhours"`
	Hour           *int     `json:"hour,omitempty" jsonschema:"周期里的小时数（nhours 时即间隔小时）"`
	Minute         *int     `json:"minute,omitempty" jsonschema:"分钟偏移，0-59"`
	Day            *int     `json:"day,omitempty" jsonschema:"天（ndays/nmonths）"`
	Week           *int     `json:"week,omitempty" jsonschema:"周几（nweeks），1-7"`
	ScheduledTasks *bool    `json:"scheduledTasks,omitempty" jsonschema:"是否启用调度；创建时不传按启用处理"`
	ScanProfile    string   `json:"scanProfile,omitempty" jsonschema:"扫描档位：留空按模板；recon = 只做资产测绘"`
	PolicyID       string   `json:"policyId,omitempty" jsonschema:"扫描策略 id，空 = 不套策略"`
	Duplicates     string   `json:"duplicates,omitempty" jsonschema:"去重口径"`
}

// applyScheduledTaskInput 把入参写进 task（创建与更新共用）。
// 更新时必须先读现有文档再调它：服务层对 AllNode / ScheduledTasks / Hour / Minute 是无条件覆盖，
// 直接拿零值请求去更新会把「全节点」「启用」「偏移」悄悄冲掉。
func applyScheduledTaskInput(task *models.Task, in scheduledTaskInput, isCreate bool) {
	if v := strings.TrimSpace(in.Name); v != "" {
		task.Name = v
	}
	if v := strings.TrimSpace(in.Target); v != "" {
		task.Target = v
	}
	if v := strings.TrimSpace(in.Ignore); v != "" {
		task.Ignore = v
	}
	if v := strings.TrimSpace(in.Template); v != "" {
		task.Template = v
	}
	if len(in.Node) > 0 {
		task.Node = in.Node
	}
	if in.AllNode != nil {
		task.AllNode = *in.AllNode
	}
	if len(in.Project) > 0 {
		task.Project = in.Project
	}
	if v := strings.TrimSpace(in.BindProject); v != "" {
		task.BindProject = v
	}
	if v := strings.TrimSpace(in.CycleType); v != "" {
		task.CycleType = v
	}
	if in.Hour != nil {
		task.Hour = *in.Hour
	}
	if in.Minute != nil {
		task.Minute = *in.Minute
	}
	if in.Day != nil {
		task.Day = *in.Day
	}
	if in.Week != nil {
		task.Week = *in.Week
	}
	if in.ScheduledTasks != nil {
		task.ScheduledTasks = *in.ScheduledTasks
	} else if isCreate {
		task.ScheduledTasks = true
	}
	if v := strings.TrimSpace(in.ScanProfile); v != "" {
		task.ScanProfile = v
	}
	if v := strings.TrimSpace(in.PolicyID); v != "" {
		task.PolicyID = v
	}
	if v := strings.TrimSpace(in.Duplicates); v != "" {
		task.Duplicates = v
	}
}

func createScheduledTask(ctx context.Context, _ *mcp.CallToolRequest, input scheduledTaskInput) (*mcp.CallToolResult, any, error) {
	if d.schedulerService == nil {
		return errorResult("计划任务服务未初始化", nil)
	}
	if strings.TrimSpace(input.Name) == "" {
		return errorResult("name 不能为空", nil)
	}
	if strings.TrimSpace(input.Target) == "" {
		// 空目标的计划任务到点只会产出空任务（RunScheduledTaskNow 也拦这个）
		return errorResult("target 不能为空", nil)
	}
	c := ginContext(ctx)
	if exists, err := d.schedulerService.CheckScheduledTaskNameExists(c, strings.TrimSpace(input.Name)); err == nil && exists {
		return errorResult("同名计划任务已存在："+strings.TrimSpace(input.Name), nil)
	}
	task := &models.Task{ID: primitive.NewObjectID(), Type: "scan"}
	applyScheduledTaskInput(task, input, true)
	if err := d.schedulerService.CreateScheduledTask(c, task); err != nil {
		return errorResult("创建计划任务失败", err)
	}
	return jsonToolResult(ginH{
		"id":             task.ID.Hex(),
		"name":           task.Name,
		"cycleType":      task.CycleType,
		"scheduledTasks": task.ScheduledTasks,
	})
}

type updateScheduledTaskInput struct {
	ID string `json:"id" jsonschema:"计划任务 id"`
	scheduledTaskInput
}

func updateScheduledTask(ctx context.Context, _ *mcp.CallToolRequest, input updateScheduledTaskInput) (*mcp.CallToolResult, any, error) {
	if d.schedulerService == nil {
		return errorResult("计划任务服务未初始化", nil)
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		return errorResult("id 不能为空", nil)
	}
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return errorResult("id 不是合法的 ObjectID", nil)
	}
	// 先读现状再合并：服务层对布尔与时间字段是无条件覆盖，拿零值请求直接更新会静默改配置。
	current, err := d.schedulerService.GetScheduledTaskDetail(ginContext(ctx), id)
	if err != nil || current == nil {
		return errorResult("计划任务不存在", err)
	}
	applyScheduledTaskInput(current, input.scheduledTaskInput, false)
	current.ID = oid
	if err := d.schedulerService.UpdateScheduledTask(ginContext(ctx), current); err != nil {
		return errorResult("更新计划任务失败", err)
	}
	return jsonToolResult(ginH{"id": id, "name": current.Name, "scheduledTasks": current.ScheduledTasks})
}

type scheduledTaskIDsInput struct {
	IDs []string `json:"ids" jsonschema:"计划任务 id 列表，至少一个"`
}

func deleteScheduledTasks(ctx context.Context, _ *mcp.CallToolRequest, input scheduledTaskIDsInput) (*mcp.CallToolResult, any, error) {
	if d.schedulerService == nil {
		return errorResult("计划任务服务未初始化", nil)
	}
	if len(input.IDs) == 0 {
		return errorResult("ids 不能为空", nil)
	}
	deleted, err := d.schedulerService.DeleteScheduledTasks(ginContext(ctx), input.IDs)
	if err != nil {
		return errorResult("删除计划任务失败", err)
	}
	if deleted == 0 {
		return errorResult("没有匹配的计划任务（id 不存在或已删除）", nil)
	}
	return jsonToolResult(ginH{"deleted": deleted, "requested": len(input.IDs)})
}

type retestTaskInput struct {
	ID string `json:"id" jsonschema:"任务 id（列表里那条任务，不是目标）"`
}

func retestTask(ctx context.Context, _ *mcp.CallToolRequest, input retestTaskInput) (*mcp.CallToolResult, any, error) {
	if d.taskService == nil {
		return errorResult("任务服务未初始化", nil)
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		return errorResult("id 不能为空", nil)
	}
	if err := d.taskService.RetestTask(ginContext(ctx), id); err != nil {
		return errorResult("重跑任务失败", err)
	}
	// 重跑会按原任务配置新建一条任务，回执带上原 id 便于调用方关联
	return jsonToolResult(ginH{"retestedFrom": id})
}
