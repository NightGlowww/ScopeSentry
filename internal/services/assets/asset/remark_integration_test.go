//go:build integration

package asset

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/gin-gonic/gin"
)

// issue #335：备注必须只改 remark 一个字段（不能整文档覆盖冲掉扫描结果），
// 并且去首尾空白、限长、非法 ID 明确报错。
func TestSetRemarkRoundTrip(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	svc := NewService()

	assetDoc := &models.Asset{Host: "remark-test-" + t.Name(), Title: "keep-me", Port: "443"}
	if err := svc.CreateAsset(newGinCtx(), assetDoc); err != nil {
		t.Fatalf("create: %v", err)
	}
	id := assetDoc.ID.Hex()
	t.Cleanup(func() { _ = svc.DeleteAsset(newGinCtx(), id) })

	// 去空白后落库
	if err := svc.SetRemark(newGinCtx(), id, "  手工核对：测试资产  "); err != nil {
		t.Fatalf("set remark: %v", err)
	}
	got, err := svc.GetAssetByID(newGinCtx(), id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Remark != "手工核对：测试资产" {
		t.Fatalf("remark = %q, want trimmed text", got.Remark)
	}
	if got.Title != "keep-me" || got.Port != "443" {
		t.Fatalf("other fields were overwritten: %+v", got)
	}

	// 空串表示清空
	if err := svc.SetRemark(newGinCtx(), id, "   "); err != nil {
		t.Fatalf("clear remark: %v", err)
	}
	got, err = svc.GetAssetByID(newGinCtx(), id)
	if err != nil {
		t.Fatalf("get after clear: %v", err)
	}
	if got.Remark != "" {
		t.Fatalf("remark = %q, want empty", got.Remark)
	}
}

func TestSetRemarkRejectsBadInput(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	svc := NewService()

	if err := svc.SetRemark(newGinCtx(), "not-an-object-id", "x"); !errors.Is(err, ErrInvalidAssetID) {
		t.Fatalf("err = %v, want ErrInvalidAssetID", err)
	}

	tooLong := strings.Repeat("字", MaxRemarkLength+1)
	if err := svc.SetRemark(newGinCtx(), "507f1f77bcf86cd799439011", tooLong); !errors.Is(err, ErrRemarkTooLong) {
		t.Fatalf("err = %v, want ErrRemarkTooLong", err)
	}

	// 正好 500 个字符是允许的（边界不误伤）
	exact := strings.Repeat("a", MaxRemarkLength)
	if err := svc.SetRemark(newGinCtx(), "507f1f77bcf86cd799439011", exact); err != nil && errors.Is(err, ErrRemarkTooLong) {
		t.Fatalf("exact limit rejected: %v", err)
	}
}

func newGinCtx() *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/api/assets/asset/remark", nil)
	return c
}
