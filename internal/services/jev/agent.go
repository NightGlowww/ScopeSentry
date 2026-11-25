package jev

// agent 后端：把「判定」交给一个外部 agent / harness 进程，而不是单次模型调用。
//
// 为什么需要它（2026-11-26 用户指出）：真正去实施的往往不是一次 chat completion，
// 而是一个会自己发请求、比对响应、多步推理的 agent（dsh / PI / sapper / 自写脚本）。
// 单次模型调用只能「读已有证据下判断」，agent 才能「重新验证一遍再下判断」。
//
// 契约（与模型后端完全一致，所以上层阈值判断不用改）：
//   stdin  : {"state": "<证据文本>", "questions": {...}}
//   stdout : {"is_vuln":0..1,"evidence":0..2,"nq":"none|informational|by_design|false_positive",
//             "nq_veto":0..1,"reason":"...","trace":"可选，agent 做了什么的简述"}
//   退出码 : 0 成功；非 0 视为不可用，回落到模型后端
//
// 配置：
//   AGENT_VERIFIER_CMD     例如 "dsh run --preset verifier --json" 或 "/opt/bin/verify.sh"
//   AGENT_VERIFIER_TIMEOUT 秒，默认 180

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// AgentConfigured 是否配置了 agent 后端
func AgentConfigured() bool {
	return strings.TrimSpace(os.Getenv("AGENT_VERIFIER_CMD")) != ""
}

func agentTimeout() time.Duration {
	if v := os.Getenv("AGENT_VERIFIER_TIMEOUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 180 * time.Second
}

// askAgent 调外部 agent 进程。
func askAgent(ctx context.Context, state string, questions map[string]Question) (*Response, error) {
	cmdline := strings.TrimSpace(os.Getenv("AGENT_VERIFIER_CMD"))
	if cmdline == "" {
		return nil, fmt.Errorf("AGENT_VERIFIER_CMD not set")
	}
	in, _ := json.Marshal(map[string]interface{}{"state": state, "questions": questions})

	cctx, cancel := context.WithTimeout(ctx, agentTimeout())
	defer cancel()
	cmd := exec.CommandContext(cctx, "sh", "-c", cmdline)
	cmd.Stdin = bytes.NewReader(in)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("agent: %v: %s", err, truncate(stderr.String(), 200))
	}
	js := extractJSON(stdout.String())
	if js == "" {
		return nil, fmt.Errorf("agent: no json in stdout: %s", truncate(stdout.String(), 200))
	}
	var v llmVerdict
	if err := json.Unmarshal([]byte(js), &v); err != nil {
		return nil, fmt.Errorf("agent: bad json: %w", err)
	}
	if v.NQ == "" {
		v.NQ = "none"
	}
	resp := toResponse(&v)
	resp.Model = "agent:" + filepath.Base(strings.Fields(cmdline)[0])
	return resp, nil
}
