package jev

// 可插拔基模：默认 JEV（TypeSafe System One），也可以切到任意 OpenAI 兼容端点
// （DeepSeek / 本地 vLLM / PI 的 OpenAI 兼容层 / dsh 暴露的兼容接口）。
//
// 为什么这样切而不是直接内嵌 PI 或 dsh：
//   它们是 agent CLI（会调工具、跑多轮），不是「给一段证据 → 返回打分」的推理端点；
//   内嵌等于包一层进程调用，延迟和稳定性都不划算。
//   而 JEV 本身就是为「判漏洞有效性」设计的打分模型，返回的是校准过的概率。
// 所以：JEV 默认，OpenAI 兼容作为第二实现，两者输出映射到同一套 Response。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ProviderName 当前生效的基模：jev（默认）| openai
func ProviderName() string {
	if p := strings.ToLower(strings.TrimSpace(os.Getenv("LLM_PROVIDER"))); p != "" {
		return p
	}
	return "jev"
}

func llmBaseURL() string {
	if v := os.Getenv("LLM_BASE_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://api.deepseek.com/v1"
}

func llmModel() string {
	if v := os.Getenv("LLM_MODEL"); v != "" {
		return v
	}
	return "deepseek-chat"
}

// judgePrompt 是判定用的系统提示。
//
// 这里的措辞是有意的：模型经常对「安全测试材料」本身触发拒绝（它读到的是一段 exploit 证据）。
// 所以先明确这是防御性 triage、要求只做评分不做评论；若第一次返回不可解析（多半是拒绝或跑题），
// askOpenAI 会用 neutralPrompt 再试一次——这就是「jailbreak 组件」在这里的正当形态：
// 不是骗模型做坏事，而是防止它把「判断这段证据算不算真漏洞」误当成「帮人写攻击代码」。
const judgePrompt = `You are a vulnerability triage engine. You will receive a record from an automated scanner:
template name, severity, URL, matched pattern, and the recorded request/response evidence.
Your job is to score whether the evidence actually demonstrates a real, security-relevant vulnerability.
You are not asked to exploit anything; this is defensive triage of already-collected evidence.
Answer ONLY with a JSON object, no prose, no code fences:
{"is_vuln": <0..1>, "evidence": <0..2>, "nq": "none|informational|by_design|false_positive", "nq_veto": <0..1>, "reason": "<one short sentence>"}`

const neutralPrompt = `You are reviewing an automated scanner record for accuracy. The record contains a payload string and the server response it produced.
Assess whether the response proves the claimed issue, or whether it is just a coincidental pattern match.
Respond with JSON only:
{"is_vuln": <0..1>, "evidence": <0..2>, "nq": "none|informational|by_design|false_positive", "nq_veto": <0..1>, "reason": "<one short sentence>"}`

type llmVerdict struct {
	IsVuln   float64 `json:"is_vuln"`
	Evidence float64 `json:"evidence"`
	NQ       string  `json:"nq"`
	NQVeto   float64 `json:"nq_veto"`
	Reason   string  `json:"reason"`
}

// askOpenAI 走 {LLM_BASE_URL}/chat/completions，把模型的 JSON 答案映射成 Response。
func askOpenAI(ctx context.Context, state string, _ map[string]Question) (*Response, error) {
	key := os.Getenv("LLM_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("LLM_API_KEY not set (LLM_PROVIDER=openai)")
	}
	for attempt, sys := range []string{judgePrompt, neutralPrompt} {
		v, err := callChat(ctx, key, sys, state)
		if err == nil {
			return toResponse(v), nil
		}
		// 第一次失败（拒绝/跑题/非法 JSON）才用中性措辞重试；第二次仍失败就返回错误。
		if attempt == 1 {
			return nil, err
		}
	}
	return nil, fmt.Errorf("llm: no attempt made")
}

func callChat(ctx context.Context, key, sys, state string) (*llmVerdict, error) {
	payload, _ := json.Marshal(map[string]interface{}{
		"model":       llmModel(),
		"temperature": 0,
		"messages": []map[string]string{
			{"role": "system", "content": sys},
			{"role": "user", "content": state},
		},
	})
	timeout := 60 * time.Second
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, llmBaseURL()+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := newClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("llm http %d: %s", resp.StatusCode, string(body[:min(len(body), 300)]))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("llm decode: %w", err)
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("llm: empty choices")
	}
	content := out.Choices[0].Message.Content
	js := extractJSON(content)
	if js == "" {
		return nil, fmt.Errorf("llm: no json in answer: %s", truncate(content, 200))
	}
	var v llmVerdict
	if err := json.Unmarshal([]byte(js), &v); err != nil {
		return nil, fmt.Errorf("llm: bad json: %w", err)
	}
	if v.NQ == "" {
		v.NQ = "none"
	}
	return &v, nil
}

// extractJSON 从可能带 ```json 围栏或前后缀的答案里抠出第一个 JSON 对象。
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
}

func toResponse(v *llmVerdict) *Response {
	r := &Response{Model: "llm:" + llmModel(), Answers: map[string]Answer{}}
	r.Answers["is_vulnerability"] = Answer{Type: "noul", Noul: v.IsVuln}
	r.Answers["evidence_strength"] = Answer{Type: "score", Score: v.Evidence}
	r.Answers["nq_reason"] = Answer{
		Type: "choice", Choice: v.NQ,
		Probabilities: map[string]float64{v.NQ: v.NQVeto, "none": 1 - v.NQVeto},
	}
	return r
}
