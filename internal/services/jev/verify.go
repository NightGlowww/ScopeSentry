package jev

// 按需判定：给一条具体的漏洞记录，立刻问一次模型，把结论返回给调用方（并可选落库）。
//
// 与 RunOnce 的分工：
//   RunOnce   = 后台批处理，只挑「机械复验判不了」的候选，自动落库；
//   VerifyNow = 人点一条洞时同步问，返回结构给界面显示，默认不改状态。
// 两者共用同一套状态构造、问题集和阈值，避免「批量判的和手动判的不一样」。

import (
	"context"
	"fmt"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// VerifyResult 单条按需判定的结果
type VerifyResult struct {
	ID       string  `json:"id"`
	URL      string  `json:"url"`
	Vulname  string  `json:"vulname"`
	Decision string  `json:"decision"` // confirmed | rejected | needs_human
	IsVuln   float64 `json:"isVuln"`
	Evidence float64 `json:"evidence"`
	NQ       string  `json:"nq"`
	NQVeto   float64 `json:"nqVeto"`
	Model    string  `json:"model"`
	Applied  bool    `json:"applied"`
	Error    string  `json:"error,omitempty"`
}

// VerifyNow 对指定漏洞 id 逐个判定。apply=true 时按结论落库，
// 但**永不覆盖人工结论**（statusSource=human 的直接跳过并说明原因）。
func VerifyNow(ctx context.Context, ids []string, apply bool) ([]VerifyResult, error) {
	if !Enabled() {
		return nil, fmt.Errorf("LLM 未配置：JEV_API_KEY 为空")
	}
	if mongodb.DB == nil {
		return nil, fmt.Errorf("数据库未初始化")
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("ids 不能为空")
	}
	if len(ids) > 20 {
		return nil, fmt.Errorf("单次最多 20 条，收到 %d", len(ids))
	}

	col := mongodb.DB.Collection("vulnerability")
	out := make([]VerifyResult, 0, len(ids))
	for _, id := range ids {
		res := VerifyResult{ID: id}
		oid, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			res.Error = "id 格式不对"
			out = append(out, res)
			continue
		}
		var d bson.M
		if err = col.FindOne(ctx, bson.M{"_id": oid}).Decode(&d); err != nil {
			res.Error = "记录不存在"
			out = append(out, res)
			continue
		}
		if s, _ := d["url"].(string); s != "" {
			res.URL = s
		}
		if s, _ := d["vulname"].(string); s != "" {
			res.Vulname = s
		}
		if src, _ := d["statusSource"].(string); src == "human" {
			res.Error = "人工已判定，不覆盖"
			out = append(out, res)
			continue
		}

		state := buildState(ctx, d)
		if state == "" {
			res.Error = "没有可判定的证据"
			out = append(out, res)
			continue
		}
		resp, aerr := Ask(ctx, state, questions())
		if aerr != nil {
			res.Error = aerr.Error()
			out = append(out, res)
			continue
		}
		res.Model = resp.Model
		res.IsVuln = resp.Answers["is_vulnerability"].Noul
		res.Evidence = resp.Answers["evidence_strength"].Score
		nq := resp.Answers["nq_reason"]
		res.NQ = nq.Choice
		for k, p := range nq.Probabilities {
			if k != "none" && p > res.NQVeto {
				res.NQVeto = p
			}
		}
		switch {
		case res.IsVuln >= acceptThreshold() && res.Evidence >= 1.5 && res.NQVeto < 0.6:
			res.Decision = "confirmed"
		case res.IsVuln <= rejectThreshold() || (res.NQVeto >= 0.6 && res.NQ != "none"):
			res.Decision = "rejected"
		default:
			res.Decision = "needs_human"
		}

		if apply {
			now := time.Now().Format("2006-01-02 15:04:05")
			jevDoc := bson.M{
				"model": res.Model, "isVuln": res.IsVuln, "evidence": res.Evidence,
				"nq": res.NQ, "nqVeto": res.NQVeto, "judgedAt": now, "decision": res.Decision,
				"mode": "on-demand",
			}
			upd := bson.M{"jev": jevDoc}
			switch res.Decision {
			case "confirmed":
				upd["status"] = 5
				upd["confirmedBy"] = "auto:jev-on-demand"
				upd["confirmedAt"] = now
			case "rejected":
				upd["status"] = 3 // 忽略，但不删除：按需判定只给建议，删证据留给人
				upd["verifyReason"] = fmt.Sprintf("LLM 按需判定：判为非漏洞（nq=%s, veto=%.2f）。证据未删除，可人工复核后恢复。", res.NQ, res.NQVeto)
			}
			if _, uerr := col.UpdateOne(ctx, bson.M{"_id": oid, "statusSource": bson.M{"$ne": "human"}}, bson.M{"$set": upd}); uerr == nil {
				res.Applied = true
			}
		}
		out = append(out, res)
	}
	return out, nil
}
