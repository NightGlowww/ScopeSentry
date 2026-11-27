package dictionary

import (
	"context"
	"errors"
	"testing"
)

// 空名字/空值必须在写库前挡住：这两个分支不碰仓库，所以不需要 Mongo。
func TestPortAddRejectsBlankNameAndValue(t *testing.T) {
	// 直接构造零值：空名字/空值在碰仓库之前就返回，不需要 Mongo（NewPortService 会去连库）。
	svc := &portService{}
	ctx := context.Background()

	if err := svc.Add(ctx, "   ", "80"); !errors.Is(err, ErrPortNameRequired) {
		t.Fatalf("blank name => %v, want ErrPortNameRequired", err)
	}
	if err := svc.Add(ctx, "http", "  "); !errors.Is(err, ErrPortValueRequired) {
		t.Fatalf("blank value => %v, want ErrPortValueRequired", err)
	}
	if err := svc.Update(ctx, "id", "", "80"); !errors.Is(err, ErrPortNameRequired) {
		t.Fatalf("update blank name => %v, want ErrPortNameRequired", err)
	}
	if err := svc.Update(ctx, "id", "http", ""); !errors.Is(err, ErrPortValueRequired) {
		t.Fatalf("update blank value => %v, want ErrPortValueRequired", err)
	}
}
