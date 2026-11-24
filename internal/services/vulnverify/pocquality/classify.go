// Package pocquality 给 PocList 里的模板打「能否复验」标记。
//
// 背景：漏洞落库只保存最后一条响应，判据依赖 body_2..N 的模板永远无法复验，
// 它们的结论和误报无法区分。2026-11-24 全量统计：437 个模板属于这一类。
// 默认把这类模板标 excluded=true，节点不再加载；运营者可用 excludedOverride=true 恢复。
package pocquality

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/services/vulnverify"
)

// RunOnce 扫一批模板并打标：unverifiable=只能多请求判定，excluded=节点不加载。
// 返回 (checked, blocked)。
func RunOnce(ctx context.Context, limit int64) (checked, blocked int, err error) {
	if mongodb.DB == nil {
		return 0, 0, fmt.Errorf("mongodb not ready")
	}
	dry := os.Getenv("POC_BLOCK_DRY") == "1"
	cur, ferr := mongodb.DB.Collection("PocList").Find(ctx,
		bson.M{"qualityChecked": bson.M{"$exists": false}},
		options.Find().SetLimit(limit).SetProjection(bson.M{"content": 1, "excludedOverride": 1}))
	if ferr != nil {
		return 0, 0, ferr
	}
	defer cur.Close(ctx)
	var docs []struct {
		ID               interface{} `bson:"_id"`
		Content          string      `bson:"content"`
		ExcludedOverride bool        `bson:"excludedOverride"`
	}
	if err = cur.All(ctx, &docs); err != nil {
		return 0, 0, err
	}
	for _, d := range docs {
		checked++
		bad := vulnverify.UnverifiableTemplate(d.Content)
		set := bson.M{"qualityChecked": true, "unverifiable": bad}
		if bad && !d.ExcludedOverride {
			set["excluded"] = true
			blocked++
		}
		if dry {
			continue
		}
		_, _ = mongodb.DB.Collection("PocList").UpdateOne(ctx, bson.M{"_id": d.ID}, bson.M{"$set": set})
	}
	return checked, blocked, nil
}

// Start 启动时跑一次，之后每 6 小时补扫（新导入的模板）。POC_BLOCK=off 关闭。
func Start(ctx context.Context) {
	if os.Getenv("POC_BLOCK") == "off" {
		logger.Info("pocquality: disabled by POC_BLOCK=off")
		return
	}
	go func() {
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for {
			checked, blocked, err := RunOnce(ctx, 500)
			if err != nil {
				logger.Error("pocquality: classify failed: " + err.Error())
			} else if checked > 0 {
				logger.Info(fmt.Sprintf("pocquality: checked=%d excluded=%d", checked, blocked))
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
