package fofa

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	taskrepo "github.com/Autumn-27/ScopeSentry/internal/repositories/task/task"
	commontask "github.com/Autumn-27/ScopeSentry/internal/services/task/common"
)

// 建任务复用现有任务链路。
//
// 唯一实现 commonTaskCreator 只是把「FOFA 的 host 列表」翻译成 models.Task，
// 然后调用 commonTask.Service.Insert —— 目标解析（IP 段/CIDR 展开、去重）、
// 任务入库、TaskNum/Status/CreatTime 落值、节点派发全部沿用现有逻辑，
// 这里不复制任何一行。

// ErrTaskNameExists 任务名已存在（逐条失败，不中断整批）
var ErrTaskNameExists = errors.New("task name already exists")

// CreateTaskSpec 建一个扫描任务的输入
type CreateTaskSpec struct {
	// Name 任务名（调用方保证唯一；重复会让本 host 记失败）
	Name string
	// Targets 目标列表（每个元素一个 host[:port]）
	Targets []string
	// Project 绑定项目 ObjectID（可选）
	Project string
	// TemplateID 扫描模板 ObjectID（可选）
	TemplateID string
}

// TaskCreator 任务创建抽象（集成测试可注入 fake，避免真的落 task 集合）
type TaskCreator interface {
	Create(ctx context.Context, spec CreateTaskSpec) (string, error)
}

type commonTaskCreator struct {
	tasks  taskrepo.Repository
	common commontask.Service
}

// NewTaskCreator 默认实现：复用现有任务 service
func NewTaskCreator() TaskCreator {
	return &commonTaskCreator{
		tasks:  taskrepo.NewRepository(),
		common: commontask.NewService(),
	}
}

// Create 建任务并返回任务 ID
func (c *commonTaskCreator) Create(ctx context.Context, spec CreateTaskSpec) (string, error) {
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return "", fmt.Errorf("%w: taskName 不能为空", ErrInvalidInput)
	}
	targets := normalizeTaskTargets(spec.Targets)
	if len(targets) == 0 {
		return "", fmt.Errorf("%w: 任务没有任何目标", ErrInvalidInput)
	}

	exists, err := c.tasks.CheckTaskNameExists(ctx, name)
	if err != nil {
		return "", err
	}
	if exists {
		return "", fmt.Errorf("%w: %s", ErrTaskNameExists, name)
	}

	task := &models.Task{
		Name: name,
		// 多目标用换行分隔：common.Insert 里按行解析成 TaskNum
		Target:       strings.Join(targets, "\n"),
		TargetSource: "general",
		TargetNumber: len(targets),
		// 给了模板才自动加入全部在线节点：FOFA 导入出来的任务应该直接可跑
		// （与 ARL 提交后 celery 立刻执行的语义对齐）；没给模板时只落任务不派发，
		// 避免把空模板推给节点（与 /api/task/add 的行为一致，但更保守）。
		AllNode:  strings.TrimSpace(spec.TemplateID) != "",
		Template: strings.TrimSpace(spec.TemplateID),
	}
	if project := strings.TrimSpace(spec.Project); project != "" {
		task.Project = []string{project}
		task.BindProject = project
	}
	return c.common.Insert(ctx, task)
}

// normalizeTaskTargets 去空、去重保序（Insert 内部还会再做一次 IP 段/CIDR 归一）
func normalizeTaskTargets(targets []string) []string {
	out := make([]string, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		out = append(out, target)
	}
	return out
}
