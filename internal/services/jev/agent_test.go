package jev

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// agent 后端的契约测试：stdin 收证据 JSON，stdout 回判定 JSON。
//
// 这条测试存在的理由：真正去实施的往往是一个 agent/harness（会自己重发请求、多步验证），
// 不是单次模型调用。契约一旦漂移，上层阈值判断就会静默失效，所以必须有测试钉住它。
func TestAskAgentContract(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "agent.sh")
	body := "#!/bin/sh\n" +
		"cat > /tmp/agent_in_$$.json\n" + // 吃掉 stdin，顺带证明我们真的传了东西
		"echo '{\"is_vuln\":0.91,\"evidence\":2,\"nq\":\"none\",\"nq_veto\":0.03,\"reason\":\"refetched and confirmed\"}'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_VERIFIER_CMD", "/bin/sh "+script)
	t.Setenv("AGENT_VERIFIER_TIMEOUT", "10")
	if !AgentConfigured() {
		t.Fatal("AgentConfigured 应为 true")
	}

	resp, err := askAgent(context.Background(), "Template: x\nURL: https://example.test", questions())
	if err != nil {
		t.Fatalf("askAgent 失败: %v", err)
	}
	if got := resp.Answers["is_vulnerability"].Noul; got != 0.91 {
		t.Fatalf("isVuln 映射错: %v", got)
	}
	if got := resp.Answers["evidence_strength"].Score; got != 2 {
		t.Fatalf("evidence 映射错: %v", got)
	}
	if got := resp.Answers["nq_reason"].Choice; got != "none" {
		t.Fatalf("nq 映射错: %v", got)
	}
	if resp.Model == "" || resp.Model[:6] != "agent:" {
		t.Fatalf("Model 应标注 agent 后端，得到 %q", resp.Model)
	}
}

// 没配 AGENT_VERIFIER_CMD 时不能假装有 agent，否则 Ask 会一直走失败分支。
func TestAgentNotConfigured(t *testing.T) {
	t.Setenv("AGENT_VERIFIER_CMD", "")
	if AgentConfigured() {
		t.Fatal("未配置时应返回 false")
	}
}
