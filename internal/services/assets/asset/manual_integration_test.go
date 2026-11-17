//go:build integration

package asset

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// issue #203：手工录入资产要走唯一索引（host+port），重复录入要明确报错而不是 500。
func TestAddAssetManualEntry(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	svc := NewService()
	host := fmt.Sprintf("manual-%d.example.com", time.Now().UnixNano())

	created, err := svc.AddAsset(newGinCtx(), &models.AssetAddRequest{
		Host: host, Port: "8443", URL: "https://" + host + "/", IP: "10.0.0.9",
		Service: "https", Title: "manual", Project: "proj-manual", Tags: []string{"manual"},
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	t.Cleanup(func() { _ = svc.DeleteAsset(newGinCtx(), created.ID.Hex()) })

	if created.Time == "" {
		t.Fatalf("time not filled: %+v", created)
	}
	if created.Type != "https" {
		t.Fatalf("type = %q, want https (derived from url)", created.Type)
	}
	got, err := svc.GetAssetByID(newGinCtx(), created.ID.Hex())
	if err != nil || got == nil {
		t.Fatalf("get: %v", err)
	}
	if got.Host != host || got.Port != "8443" || got.Project != "proj-manual" {
		t.Fatalf("stored asset mismatch: %+v", got)
	}

	// 同 host+port 再录一次：唯一索引冲突要映射成 ErrAssetExists
	_, err = svc.AddAsset(newGinCtx(), &models.AssetAddRequest{Host: host, Port: "8443"})
	if !errors.Is(err, ErrAssetExists) {
		t.Fatalf("duplicate err = %v, want ErrAssetExists", err)
	}
}

func TestAddAssetRejectsBadInput(t *testing.T) {
	if mongodb.DB == nil {
		t.Skip("mongo not initialized")
	}
	svc := NewService()
	if _, err := svc.AddAsset(newGinCtx(), &models.AssetAddRequest{Host: "  "}); !errors.Is(err, ErrAssetInvalid) {
		t.Fatalf("empty host err = %v, want ErrAssetInvalid", err)
	}
	if _, err := svc.AddAsset(newGinCtx(), &models.AssetAddRequest{Host: "h.example.com", URL: "not-a-url"}); !errors.Is(err, ErrAssetInvalid) {
		t.Fatalf("bad url err = %v, want ErrAssetInvalid", err)
	}
}
