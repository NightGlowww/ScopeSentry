package autoclean

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"github.com/Autumn-27/ScopeSentry/internal/services/vulnverify"
)

// SweepOnce 复验一批未标记的漏洞记录：证据不成立的隔离后删除，成立的打上 verified。
// 返回 (checked, removed, verified, skipped)。
func SweepOnce(ctx context.Context, limit int64) (checked, removed, verified, skipped int, err error) {
	if mongodb.DB == nil {
		return 0, 0, 0, 0, fmt.Errorf("mongodb not ready")
	}
	dry := os.Getenv("VULN_VERIFY_DRY") == "1"
	cur, ferr := mongodb.DB.Collection("vulnerability").Find(ctx,
		bson.M{"verifyState": bson.M{"$exists": false}},
		options.Find().SetLimit(limit))
	if ferr != nil {
		return 0, 0, 0, 0, ferr
	}
	defer cur.Close(ctx)
	var docs []bson.M
	if err = cur.All(ctx, &docs); err != nil {
		return 0, 0, 0, 0, err
	}
	for _, d := range docs {
		checked++
		hash, _ := d["hash"].(string)
		vulnID, _ := d["vulnid"].(string)
		var detail struct {
			Request  string `bson:"req"`
			Response string `bson:"res"`
		}
		if hash != "" {
			_ = mongodb.DB.Collection("vulnerabilityDetail").FindOne(ctx, bson.M{"hash": hash}).Decode(&detail)
		}
		content := templateContent(ctx, vulnID)
		if content == "" {
			mark(ctx, d, "skipped", "template not found", dry)
			skipped++
			continue
		}
		v := vulnverify.Check(content, detail.Response)
		switch {
		case !v.Evaluable:
			mark(ctx, d, "skipped", v.Reason, dry)
			skipped++
		case v.Weak:
			// 判据只有状态码：成立也不构成证据，标 weak 待人工复核，不删也不当已确认。
			mark(ctx, d, "weak", v.Reason, dry)
			skipped++
		case v.Verified:
			mark(ctx, d, "verified", v.Reason, dry)
			verified++
		default:
			if !dry {
				if err := quarantineAndDelete(ctx, d, v.Reason); err != nil {
					logger.Error("vulnverify: quarantine failed: " + err.Error())
					continue
				}
			}
			removed++
		}
	}
	return checked, removed, verified, skipped, nil
}

func templateContent(ctx context.Context, vulnID string) string {
	if vulnID == "" {
		return ""
	}
	var doc struct {
		Content string `bson:"content"`
	}
	if oid, err := primitive.ObjectIDFromHex(vulnID); err == nil {
		if mongodb.DB.Collection("PocList").FindOne(ctx, bson.M{"_id": oid}).Decode(&doc) == nil && doc.Content != "" {
			return doc.Content
		}
	}
	if mongodb.DB.Collection("PocList").FindOne(ctx, bson.M{"id": vulnID}).Decode(&doc) == nil {
		return doc.Content
	}
	return ""
}

func mark(ctx context.Context, d bson.M, state, reason string, dry bool) {
	if dry {
		return
	}
	_, _ = mongodb.DB.Collection("vulnerability").UpdateOne(ctx, bson.M{"_id": d["_id"]}, bson.M{
		"$set": bson.M{"verifyState": state, "verifyReason": reason, "verifiedAt": time.Now().Format("2006-01-02 15:04:05")},
	})
}

func quarantineAndDelete(ctx context.Context, d bson.M, reason string) error {
	d["quarantinedAt"] = time.Now().Format("2006-01-02 15:04:05")
	d["reason"] = reason
	if _, err := mongodb.DB.Collection("vulnerabilityQuarantine").InsertOne(ctx, d); err != nil {
		return err
	}
	hash, _ := d["hash"].(string)
	if hash != "" {
		_, _ = mongodb.DB.Collection("vulnerabilityDetail").DeleteMany(ctx, bson.M{"hash": hash})
	}
	_, err := mongodb.DB.Collection("vulnerability").DeleteOne(ctx, bson.M{"_id": d["_id"]})
	return err
}

// Start 常驻复验：默认每 5 分钟一批。VULN_VERIFY=off 关闭。
func Start(ctx context.Context) {
	if os.Getenv("VULN_VERIFY") == "off" {
		logger.Info("vulnverify: disabled by VULN_VERIFY=off")
		return
	}
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			checked, removed, verified, skipped, err := SweepOnce(ctx, 200)
			if err != nil {
				logger.Error("vulnverify: sweep failed: " + err.Error())
			} else if checked > 0 {
				logger.Info(fmt.Sprintf("vulnverify: checked=%d removed=%d verified=%d skipped=%d", checked, removed, verified, skipped))
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
