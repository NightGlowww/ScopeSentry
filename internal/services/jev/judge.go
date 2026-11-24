package jev

// 第二裁判：把「机械复验判不了」的候选交给 Jev，按概率落业务状态。
//
// 分工：
//   vulnverify  = 证据是否成立（matcher 在真实响应上是否命中）→ 机械、可解释；
//   Jev         = 这算不算一个漏洞（影响、是否 NQ）→ 语义、给概率。
// 两者都不越过人工：人工改过的状态（statusSource=human）永远不覆盖。

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"go.mongodb.org/mongo-driver/bson"
)

const maxPerRun = 20 // 每次最多问 20 条，控制成本与时延

func acceptThreshold() float64 {
	if v := os.Getenv("JEV_MIN"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return 0.75
}

func rejectThreshold() float64 {
	if v := os.Getenv("JEV_REJECT"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return 0.25
}

// truncate 截断响应，保留头部与尾部——大多数证据落在其一。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	half := n / 2
	return s[:half] + "\n...[truncated]...\n" + s[len(s)-half:]
}

func questions() map[string]Question {
	return map[string]Question{
		"is_vulnerability": {
			Type:         "noul",
			Instructions: "The recorded evidence demonstrates a real, security-relevant vulnerability at the given URL, not merely a string that matches a pattern",
		},
		"evidence_strength": {
			Type:         "score",
			Instructions: "How directly the recorded evidence proves the claim",
			Criteria:     []string{"No supporting evidence", "Suggestive but indirect", "Direct proof in the response"},
		},
		"nq_reason": {
			Type:         "choice",
			Instructions: "If a triager rejected this report, what would be the reason",
			Criteria: map[string]string{
				"none":           "No reason to reject",
				"informational":  "Informational only, no security impact",
				"by_design":      "Intended behaviour of the software",
				"false_positive": "Evidence does not actually show the claimed issue",
			},
		},
	}
}

// RunOnce 处理一批「机械复验判不了」的候选（skipped / weak / 未复验）。
func RunOnce(ctx context.Context) (confirmed, rejected, kept int, err error) {
	if !Enabled() || mongodb.DB == nil {
		return 0, 0, 0, nil
	}
	cur, ferr := mongodb.DB.Collection("vulnerability").Find(ctx, bson.M{
		"statusSource": bson.M{"$ne": "human"},
		"$or": []bson.M{
			{"status": 1}, {"status": bson.M{"$exists": false}},
		},
		"jev": bson.M{"$exists": false},
	})
	if ferr != nil {
		return 0, 0, 0, ferr
	}
	var docs []bson.M
	if err = cur.All(ctx, &docs); err != nil {
		return 0, 0, 0, err
	}
	_ = cur.Close(ctx)
	if len(docs) > maxPerRun {
		docs = docs[:maxPerRun]
	}

	for _, d := range docs {
		state := buildState(ctx, d)
		if state == "" {
			continue
		}
		resp, aerr := Ask(ctx, state, questions())
		if aerr != nil {
			logger.Error("jev: ask failed: " + aerr.Error())
			return confirmed, rejected, kept, aerr
		}
		isVuln := resp.Answers["is_vulnerability"].Noul
		evidence := resp.Answers["evidence_strength"].Score
		nq := resp.Answers["nq_reason"]
		veto := 0.0
		for k, p := range nq.Probabilities {
			if k != "none" && p > veto {
				veto = p
			}
		}

		jevDoc := bson.M{
			"model":    resp.Model,
			"isVuln":   isVuln,
			"evidence": evidence,
			"nq":       nq.Choice,
			"nqVeto":   veto,
			"judgedAt": time.Now().Format("2006-01-02 15:04:05"),
		}
		sel := bson.M{"_id": d["_id"], "statusSource": bson.M{"$ne": "human"},
			"$or": []bson.M{{"status": 1}, {"status": bson.M{"$exists": false}}}}

		switch {
		case isVuln >= acceptThreshold() && evidence >= 1.5 && veto < 0.6:
			jevDoc["decision"] = "confirmed"
			_, _ = mongodb.DB.Collection("vulnerability").UpdateOne(ctx, sel, bson.M{"$set": bson.M{
				"status": 5, "confirmedBy": "auto:jev",
				"confirmedAt": time.Now().Format("2006-01-02 15:04:05"), "jev": jevDoc,
			}})
			confirmed++
		case isVuln <= rejectThreshold() || (veto >= 0.6 && nq.Choice != "none"):
			// 不直接删：挪进隔离区留证，理由写清是模型判的。
			jevDoc["decision"] = "rejected"
			d["jev"] = jevDoc
			d["quarantinedAt"] = time.Now().Format("2006-01-02 15:04:05")
			d["reason"] = "jev: " + nq.Choice
			if _, ierr := mongodb.DB.Collection("vulnerabilityQuarantine").InsertOne(ctx, d); ierr == nil {
				hash, _ := d["hash"].(string)
				if hash != "" {
					_, _ = mongodb.DB.Collection("vulnerabilityDetail").DeleteMany(ctx, bson.M{"hash": hash})
				}
				_, _ = mongodb.DB.Collection("vulnerability").DeleteOne(ctx, bson.M{"_id": d["_id"]})
				rejected++
			}
		default:
			jevDoc["decision"] = "needs_human"
			_, _ = mongodb.DB.Collection("vulnerability").UpdateOne(ctx, sel, bson.M{"$set": bson.M{"jev": jevDoc}})
			kept++
		}
	}
	return confirmed, rejected, kept, nil
}

// buildState 把一条候选整理成模型要判断的文本。
// 关键：只给它看得到的证据，不给结论——否则它会顺着我们的措辞判。
func buildState(ctx context.Context, d bson.M) string {
	get := func(k string) string {
		if v, ok := d[k].(string); ok {
			return v
		}
		return ""
	}
	var b strings.Builder
	b.WriteString("Template: " + get("vulname") + "\n")
	b.WriteString("Severity: " + get("level") + "\n")
	b.WriteString("URL: " + get("url") + "\n")
	if m := get("matched"); m != "" {
		b.WriteString("Matched: " + m + "\n")
	}
	hash := get("hash")
	if hash != "" {
		var det struct {
			Request  string `bson:"request"`
			Response string `bson:"response"`
		}
		if mongodb.DB.Collection("vulnerabilityDetail").FindOne(ctx, bson.M{"hash": hash}).Decode(&det) == nil {
			if det.Request != "" {
				b.WriteString("Recorded request:\n" + truncate(det.Request, 800) + "\n")
			}
			if det.Response != "" {
				b.WriteString("Recorded response:\n" + truncate(det.Response, 1600) + "\n")
			}
		}
	}
	if b.Len() < 40 {
		return "" // 没有可用证据，别让模型凭空判
	}
	return b.String()
}

// Start 每 10 分钟审一批。JEV=off 或没配 key 时不动。
func Start(ctx context.Context) {
	if !Enabled() {
		logger.Info("jev: disabled (JEV=off or JEV_API_KEY unset)")
		return
	}
	go func() {
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		for {
			c, r, k, err := RunOnce(ctx)
			if err != nil {
				logger.Error("jev: run failed: " + err.Error())
			} else if c+r+k > 0 {
				logger.Info(fmt.Sprintf("jev: confirmed=%d rejected=%d needs_human=%d", c, r, k))
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}
