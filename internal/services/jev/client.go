// Package jev 接 TypeSafe 的 Jev（System One）模型做「第二裁判」。
//
// 为什么需要它：机械复验（vulnverify）只能回答「模板的 matcher 在真实响应上是否成立」，
// 回答不了「这算不算一个漏洞」。像 "Next.js Public ENV Exposure" 这种，matcher 百分百成立，
// 但三分类器一眼就该判 NQ。反过来，某些模板的 matcher 是 DSL/多响应，机械复验只能 skipped，
// 可它们恰恰是真洞。
//
// Jev 是「给结构化判断」的模型：发 state + 带类型的问题，回 0..1 的概率和分布，不用解析文本。
// 所以本包只做一件事：把一条候选漏洞的证据整理成 state，问三个问题，把答案回写成业务状态。
package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/logger"
)

const endpoint = "https://api.typesafe.ai/v1/systemone"

// Question 是一个带类型的问题。type 取 noul / score / choice。
type Question struct {
	Type         string      `json:"type"`
	Instructions string      `json:"instructions"`
	Criteria     interface{} `json:"criteria,omitempty"`
}

// Answer 是模型回的结构化答案。三种问题各自的字段都在这。
type Answer struct {
	Type          string             `json:"type"`
	Noul          float64            `json:"noul"`
	Score         float64            `json:"score"`
	Choice        string             `json:"choice"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
}

// Response 是整次调用的返回。
type Response struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Enabled 是否配置可用；JEAV=off 强制关闭。
func Enabled() bool {
	if os.Getenv("JEV") == "off" {
		return false
	}
	// 切到 OpenAI 兼容端点时，用 LLM_API_KEY 判可用性。
	if ProviderName() == "openai" {
		return os.Getenv("LLM_API_KEY") != ""
	}
	return os.Getenv("JEV_API_KEY") != ""
}

func model() string {
	if m := os.Getenv("JEV_MODEL"); m != "" {
		return m
	}
	return "jev-latest"
}

// TypeSafe 对部分地区返回 451，所以允许配一个出口代理（JEV_PROXY）。
// 形如 http://user:pass@host:port 或 socks5://host:port。
var client = newClient()

func newClient() *http.Client {
	tr := &http.Transport{
		MaxIdleConns:        4,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 15 * time.Second,
	}
	if p := os.Getenv("JEV_PROXY"); p != "" {
		if u, err := url.Parse(p); err == nil {
			tr.Proxy = http.ProxyURL(u)
		}
	}
	return &http.Client{Timeout: 45 * time.Second, Transport: tr}
}

// Ask 发一次 System One 请求。state 是要判断的内容，questions 是问题集合。
func Ask(ctx context.Context, state string, questions map[string]Question) (*Response, error) {
	// 判定后端优先级：
	//   1) agent / harness（能自己重发请求、多步验证）—— 配了 AGENT_VERIFIER_CMD 就走它
	//   2) LLM（JEV 或任意 OpenAI 兼容端点）—— 只能读已有证据下判断
	// 三者输出都映射成同一套 Response，上层阈值判断不变。
	if AgentConfigured() {
		if resp, aerr := askAgent(ctx, state, questions); aerr == nil {
			return resp, nil
		} else {
			// agent 失败不静默：记录后回落到 LLM，避免整条链路因为 agent 挂了而停摆。
			logger.Warn("agent verifier failed, falling back to llm: " + aerr.Error())
		}
	} else if AgentMode() == "builtin" {
		// 内置复核：先自己重打一次请求（含 catch-all 对照），再问模型。
		if resp, aerr := askBuiltinActive(ctx, state, questions); aerr == nil {
			return resp, nil
		} else {
			logger.Warn("builtin agent failed, falling back to llm: " + aerr.Error())
		}
	}
	return askModel(ctx, state, questions)
}

// askModel 纯模型判定（JEV 或任意 OpenAI 兼容端点），不带任何主动复核。
// 单独拆出来是为了让内置 agent 能复用「问模型」这一步而不触发递归。
func askModel(ctx context.Context, state string, questions map[string]Question) (*Response, error) {
	if ProviderName() == "openai" {
		return askOpenAI(ctx, state, questions)
	}
	key := os.Getenv("JEV_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("JEV_API_KEY not set")
	}
	payload, err := json.Marshal(map[string]interface{}{
		"state":     state,
		"model":     model(),
		"questions": questions,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jev http %d: %s", resp.StatusCode, string(body[:min(len(body), 300)]))
	}
	var out Response
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("jev decode: %w", err)
	}
	return &out, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
