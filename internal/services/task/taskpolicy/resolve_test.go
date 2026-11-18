package taskpolicy

import (
	"context"
	"errors"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

type fakeLoader struct {
	policy *models.ScanPolicy
	err    error
	calls  int
	gotID  string
}

func (f *fakeLoader) Load(_ context.Context, id string) (*models.ScanPolicy, error) {
	f.calls++
	f.gotID = id
	return f.policy, f.err
}

func TestConfigReturnsEnabledPolicyConfig(t *testing.T) {
	loader := &fakeLoader{policy: &models.ScanPolicy{
		Name:    "外网常规",
		Enabled: true,
		Config:  models.ScanPolicyConfig{TemplateID: "tpl-b"},
	}}
	svc := NewService(WithLoader(loader))
	cfg, err := svc.Config(context.Background(), "  pid-1  ")
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if cfg.TemplateID != "tpl-b" {
		t.Fatalf("TemplateID = %q, want tpl-b", cfg.TemplateID)
	}
	if loader.gotID != "pid-1" {
		t.Fatalf("loader got id %q, want trim 后的 pid-1", loader.gotID)
	}
}

func TestConfigRejectsDisabledPolicy(t *testing.T) {
	loader := &fakeLoader{policy: &models.ScanPolicy{Name: "停用策略", Enabled: false}}
	svc := NewService(WithLoader(loader))
	if _, err := svc.Config(context.Background(), "pid-1"); !errors.Is(err, ErrPolicyDisabled) {
		t.Fatalf("err = %v, want ErrPolicyDisabled", err)
	}
}

func TestLoadRejectsEmptyID(t *testing.T) {
	loader := &fakeLoader{}
	svc := NewService(WithLoader(loader))
	if _, err := svc.Config(context.Background(), "   "); !errors.Is(err, ErrMissingPolicyID) {
		t.Fatalf("err = %v, want ErrMissingPolicyID", err)
	}
	if loader.calls != 0 {
		t.Fatalf("loader 不应被调用，实际 %d 次", loader.calls)
	}
}

func TestLoadNotFoundWhenLoaderReturnsNil(t *testing.T) {
	svc := NewService(WithLoader(&fakeLoader{}))
	if _, err := svc.Config(context.Background(), "pid-1"); !errors.Is(err, ErrPolicyNotFound) {
		t.Fatalf("err = %v, want ErrPolicyNotFound", err)
	}
}

func TestLoadPropagatesLoaderError(t *testing.T) {
	want := errors.New("db down")
	svc := NewService(WithLoader(&fakeLoader{err: want}))
	if _, err := svc.Config(context.Background(), "pid-1"); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

// 默认构造不连库（仓库层在调用期解析 DB），只断言可构造。
func TestNewServiceWithoutLoader(t *testing.T) {
	if svc := NewService(); svc == nil || svc.loader == nil {
		t.Fatalf("NewService() 应装配默认 loader")
	}
}
