//go:build integration

package common

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/gin-gonic/gin"
)

// issue #258 / #191：按筛选批量删除是破坏性操作，空筛选必须显式确认；
// 非法集合直接拒绝。这里只验证“危险情况一定被挡住”，不做真实删除。
func TestDeleteBySearchGuards(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	svc := NewService()

	newCtx := func() *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/api/assets/common/delete_by_search", nil)
		return c
	}

	t.Run("empty filter requires confirmAll", func(t *testing.T) {
		deleted, err := svc.DeleteBySearch(newCtx(), &models.DeleteBySearchRequest{Index: "DirScanResult"})
		if !errors.Is(err, ErrNeedConfirmAll) {
			t.Fatalf("err = %v, want ErrNeedConfirmAll", err)
		}
		if deleted != 0 {
			t.Fatalf("deleted = %d, want 0", deleted)
		}
	})

	t.Run("invalid collection rejected", func(t *testing.T) {
		deleted, err := svc.DeleteBySearch(newCtx(), &models.DeleteBySearchRequest{Index: "not_a_collection", ConfirmAll: true})
		if !errors.Is(err, ErrInvalidCollection) {
			t.Fatalf("err = %v, want ErrInvalidCollection", err)
		}
		if deleted != 0 {
			t.Fatalf("deleted = %d, want 0", deleted)
		}
	})
}
