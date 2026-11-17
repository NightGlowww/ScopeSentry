package tasksync

import (
	"strings"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestValidateRejectsEmptyIDs(t *testing.T) {
	err := Request{Option: OptionExisting, Project: "p1"}.Validate()
	if err == nil || !strings.Contains(err.Error(), "任务") {
		t.Fatalf("Validate() = %v, want an error about missing task ids", err)
	}
}

func TestValidateRejectsUnknownOption(t *testing.T) {
	err := Request{IDS: []string{"t1"}, Option: "merge"}.Validate()
	if err == nil || !strings.Contains(err.Error(), "不支持的同步方式") {
		t.Fatalf("Validate() = %v, want an error about the unsupported option", err)
	}
}

func TestValidateExistingRequiresProject(t *testing.T) {
	err := Request{IDS: []string{"t1"}, Option: OptionExisting, Project: "  "}.Validate()
	if err == nil || !strings.Contains(err.Error(), "项目") {
		t.Fatalf("Validate() = %v, want an error about the missing project", err)
	}
}

func TestValidateNewRequiresName(t *testing.T) {
	err := Request{IDS: []string{"t1"}, Option: OptionNew, Name: ""}.Validate()
	if err == nil || !strings.Contains(err.Error(), "名称") {
		t.Fatalf("Validate() = %v, want an error about the missing project name", err)
	}
}

func TestValidateAcceptsBothOptions(t *testing.T) {
	if err := (Request{IDS: []string{"t1"}, Option: OptionExisting, Project: "p1"}).Validate(); err != nil {
		t.Fatalf("existing option rejected: %v", err)
	}
	if err := (Request{IDS: []string{"t1"}, Option: OptionNew, Name: "demo"}).Validate(); err != nil {
		t.Fatalf("new option rejected: %v", err)
	}
}

func TestExtractTargetsDedupesAcrossTasks(t *testing.T) {
	tasks := []models.Task{
		{Target: "a.com\nb.com\n\na.com"},
		{Target: " b.com \nc.com "},
	}
	got, err := ExtractTargets(tasks)
	if err != nil {
		t.Fatalf("ExtractTargets returned error: %v", err)
	}
	want := []string{"a.com", "b.com", "c.com"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ExtractTargets = %v, want %v", got, want)
	}
}

func TestExtractTargetsEmptyTargetsReturnsError(t *testing.T) {
	if _, err := ExtractTargets([]models.Task{{Target: ""}, {Target: "   \n  "}}); err == nil {
		t.Fatal("ExtractTargets accepted empty targets, want error")
	}
	if _, err := ExtractTargets(nil); err == nil {
		t.Fatal("ExtractTargets accepted no tasks, want error")
	}
}

func TestMergeExistingKeepsOrderAndDedupes(t *testing.T) {
	got := MergeExisting("a.com\nb.com", []string{"b.com", "c.com", "a.com"})
	want := []string{"a.com", "b.com", "c.com"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("MergeExisting = %v, want %v", got, want)
	}
}

func TestMergeExistingHandlesMissingExistingTarget(t *testing.T) {
	got := MergeExisting("", []string{"x.com", "x.com", " y.com "})
	want := []string{"x.com", "y.com"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("MergeExisting = %v, want %v", got, want)
	}
}

func TestCountNew(t *testing.T) {
	if got := CountNew("a.com", []string{"a.com", "b.com"}); got != 1 {
		t.Fatalf("CountNew = %d, want 1", got)
	}
	if got := CountNew("a.com", []string{"a.com"}); got != 0 {
		t.Fatalf("CountNew = %d, want 0", got)
	}
}

func TestAutoBindRequest(t *testing.T) {
	validProject := "507f1f77bcf86cd799439011"
	task := models.Task{}
	task.ID = primitive.NewObjectID()
	task.BindProject = validProject

	req, ok := AutoBindRequest(task)
	if !ok {
		t.Fatal("expected auto bind request")
	}
	if req.Option != OptionExisting || req.Project != validProject {
		t.Fatalf("unexpected request: %+v", req)
	}
	if len(req.IDS) != 1 || req.IDS[0] != task.ID.Hex() {
		t.Fatalf("ids = %v, want [%s]", req.IDS, task.ID.Hex())
	}

	// 没有 BindProject / 不是合法 ObjectID：不自动同步
	if _, ok := AutoBindRequest(models.Task{}); ok {
		t.Fatal("empty bind project should not auto sync")
	}
	bad := models.Task{}
	bad.ID = primitive.NewObjectID()
	bad.BindProject = "not-an-object-id"
	if _, ok := AutoBindRequest(bad); ok {
		t.Fatal("invalid bind project should not auto sync")
	}

	// ID 为空时回退到字符串 TaskID
	fallback := models.Task{TaskID: "task-string-id"}
	fallback.BindProject = validProject
	req, ok = AutoBindRequest(fallback)
	if !ok || req.IDS[0] != "task-string-id" {
		t.Fatalf("fallback request = %+v, ok=%v", req, ok)
	}
}
