package jev

// 内置主动复核 agent：在把证据交给模型之前，先自己把那条请求重打一遍。
//
// 为什么需要它（2026-11-26）：单次模型调用只能读「已经录下来的响应」下判断。
// 但录下来的响应可能是几天前的、可能是被 WAF 拦的、也可能模板匹配的是登录页的 200 ——
// 模型看不出这些，只能顺着文本判，于是 either 放过假阳 or 杀掉真洞。
//
// 它只做三件确定性的事，不猜 payload 语义（模板几千个，猜错就会把真洞判成假）：
//   1. 把 matched（没有就用 url）重放一次，记状态码/长度/响应哈希；
//   2. 打一个对照请求：同 host 的随机路径，看是否同样 200 且长度相近 → catch-all / soft-404；
//   3. 把「原始证据 + 本次复核结果」拼成新 state 再问模型，复核摘要写进 Model 字段留痕。
//
// 启用：JEV_AGENT=builtin（默认 off）。外部 agent 优先（AGENT_VERIFIER_CMD），两者都不配就走纯模型。

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"go.mongodb.org/mongo-driver/bson"
)

// AgentMode 当前生效的复核形态：external | builtin | （空）。
//
// 优先级：AGENT_VERIFIER_CMD（外部 agent）> 环境变量 JEV_AGENT > Mongo 配置 jev.agent。
// 多一条 Mongo 通道是因为容器的环境变量只能在重建容器时改 —— 而实测重建这个容器
// 很容易把网络/依赖搞挂（2026-11-26 一次 force-recreate 让面板断了 6 分钟）。
// 有了它，开关复核就只是一次数据库写入。
func AgentMode() string {
	if AgentConfigured() {
		return "external"
	}
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("JEV_AGENT")))
	if mode == "" {
		mode = cachedAgentConfig()
	}
	switch mode {
	case "builtin", "on", "1", "true":
		return "builtin"
	case "off", "0", "false":
		return ""
	}
	return ""
}

// agentCfg 缓存 Mongo 里读到的开关，避免每条漏洞都查一次库。
var agentCfg struct {
	sync.Mutex
	value   string
	loadedAt time.Time
}

func cachedAgentConfig() string {
	agentCfg.Lock()
	defer agentCfg.Unlock()
	if time.Since(agentCfg.loadedAt) < 60*time.Second {
		return agentCfg.value
	}
	agentCfg.loadedAt = time.Now()
	agentCfg.value = ""
	if mongodb.DB == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var doc struct {
		Value string `bson:"value"`
	}
	if err := mongodb.DB.Collection("config").FindOne(ctx, bson.M{"name": "jev.agent"}).Decode(&doc); err == nil {
		agentCfg.value = strings.ToLower(strings.TrimSpace(doc.Value))
	}
	return agentCfg.value
}

// activeClient 复核专用：直连、短超时、不跟随跳转、不复用 JEV 的出口代理。
// 走代理会把「日本节点能访问」误判成「服务端也能访问」，评估就偏了。
var activeClient = &http.Client{
	Timeout: 15 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        16,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// probeResult 一次探测的结果。
type probeResult struct {
	URL     string
	Code    int
	Len     int
	Hash    string
	Snippet string
	Err     string
}

func (p *probeResult) String() string {
	if p.Err != "" {
		return fmt.Sprintf("%s -> error: %s", p.URL, p.Err)
	}
	return fmt.Sprintf("%s -> HTTP %d, %d bytes, body-sha1 %s, head=%q", p.URL, p.Code, p.Len, p.Hash[:12], p.Snippet)
}

// skipHost 判断是否不该从服务端打（内网/回环/链路本地）。
// 扫描结果里混进内网地址时，重放等于让服务端做内网探测，必须挡掉。
func skipHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return true
	}
	if i := strings.LastIndex(host, ":"); i > 0 && !strings.Contains(host, "]") {
		host = host[:i]
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
	}
	addrs, err := net.LookupIP(host)
	if err != nil || len(addrs) == 0 {
		return true // 解析不了就别打
	}
	for _, ip := range addrs {
		if !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()) {
			return false
		}
	}
	return true
}

// probe 发一次 GET，读前 4KB 做指纹。
func probe(ctx context.Context, raw string) *probeResult {
	r := &probeResult{URL: raw}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		r.Err = "bad url"
		return r
	}
	if skipHost(u.Host) {
		r.Err = "skipped (private/loopback host)"
		return r
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		r.Err = err.Error()
		return r
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ScopeSentry-verify/1.0)")
	resp, err := activeClient.Do(req)
	if err != nil {
		r.Err = truncate(err.Error(), 120)
		return r
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	sum := sha1.Sum(body)
	r.Code = resp.StatusCode
	r.Len = len(body)
	r.Hash = hex.EncodeToString(sum[:])
	one := strings.Join(strings.Fields(string(body)), " ")
	r.Snippet = truncate(one, 120)
	return r
}

// controlURL 造对照地址：同 host、根路径下的随机段。
// 目的是识别 catch-all（任何路径都 200）与重写型 404 —— 这类站点上「模板命中」毫无意义。
func controlURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	u.Path = "/.ss-ctl-" + hex.EncodeToString(b)
	u.RawQuery = ""
	return u.String()
}

// targetFromState 从 state 里挑要重放的目标：优先 Matched，退回 URL。
func targetFromState(state string) string {
	var fallback string
	for _, line := range strings.Split(state, "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "Matched:"); ok {
			if t := strings.TrimSpace(v); t != "" {
				return t // 只取第一个
			}
		}
		if v, ok := strings.CutPrefix(line, "URL:"); ok && fallback == "" {
			fallback = strings.TrimSpace(v)
		}
	}
	return fallback
}

// askBuiltinActive 内置 agent 的一轮：先复核，再问模型。
// 复核失败不阻断判定 —— 只是把「没复核成」写进 state，让模型知道证据没有被现场确认过。
func askBuiltinActive(ctx context.Context, state string, questions map[string]Question) (*Response, error) {
	target := targetFromState(state)
	if target == "" {
		return nil, fmt.Errorf("builtin agent: state 里没有 Matched/URL")
	}
	var lines []string
	sig := "no-probe"
	replay := probe(ctx, target)
	lines = append(lines, "Replay (this run): "+replay.String())
	if replay.Err == "" {
		sig = fmt.Sprintf("replay=%d/%dB", replay.Code, replay.Len)
	}
	if ctl := controlURL(target); ctl != "" {
		cp := probe(ctx, ctl)
		lines = append(lines, "Control (random path, same host): "+cp.String())
		switch {
		case cp.Err != "":
			lines = append(lines, "Control note: 对照请求没打通，无法判定 catch-all。")
		case cp.Code == replay.Code && replay.Len > 0 && similarLen(cp.Len, replay.Len):
			lines = append(lines, "Control verdict: 随机路径同样是 "+fmt.Sprint(cp.Code)+" 且长度接近 → 站点 catch-all，命中极可能是假阳性。")
			sig += fmt.Sprintf(" control=%d(catchall)", cp.Code)
		default:
			lines = append(lines, fmt.Sprintf("Control verdict: 随机路径 %d / %dB，与命中响应不同 → 命中不是通用 200 兜底。", cp.Code, cp.Len))
			sig += fmt.Sprintf(" control=%d", cp.Code)
		}
	}
	aug := state + "\n--- active re-verification (performed by ScopeSentry just now) ---\n" + strings.Join(lines, "\n") + "\n"
	resp, err := askModel(ctx, aug, questions)
	if err != nil {
		return nil, err
	}
	// 把复核摘要挂到 Model 上：界面「LLM 判定」列本来只显示模型名，
	// 这里让它同时说明「这次是真的重打过」以及打出了什么。
	resp.Model = "agent:builtin(" + sig + ") | " + resp.Model
	logger.Info("jev builtin agent: " + target + " " + sig)
	return resp, nil
}

// similarLen 长度接近（±20%）就算同一种响应。
func similarLen(a, b int) bool {
	if a == 0 || b == 0 {
		return false
	}
	d := a - b
	if d < 0 {
		d = -d
	}
	max := a
	if b > max {
		max = b
	}
	return float64(d)/float64(max) <= 0.2
}
