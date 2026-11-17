package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GitHub API 客户端（对标 ARL-Next 的 github_task.py 采集面）。
//
// 三条硬约束：
//  1. baseURL 必须可注入 —— 单测用 httptest 假服务，绝不发真实请求；
//     生产走 https://api.github.com，GitHub Enterprise 用 GITHUB_API_BASE_URL 覆盖。
//  2. 限速必须显式处理 —— 读 X-RateLimit-Remaining / X-RateLimit-Reset，
//     剩余为 0 时立刻返回 RateLimitedError，由上层停下本任务并写 nextRunAt。
//  3. 403 / 429 必须退避 —— 带 Retry-After 或 rate-limit 头时按 reset 退避，
//     无头信息时按指数退避（1s / 2s / 4s，最多 3 次）。

const (
	// DefaultBaseURL 公共 GitHub API
	DefaultBaseURL = "https://api.github.com"
	// EnvBaseURL 覆盖 API 基址（GitHub Enterprise / 测试假服务）
	EnvBaseURL = "GITHUB_API_BASE_URL"
	// EnvToken 从环境读取 token
	EnvToken = "GITHUB_TOKEN"
	// defaultUserAgent GitHub 要求带 UA
	defaultUserAgent = "ScopeSentry-GithubIntel"
	// defaultTimeout 单请求超时
	defaultTimeout = 30 * time.Second
	// defaultRateFloor 剩余额度低于该值即视为「不足」，停止继续翻页
	defaultRateFloor = 1
)

// RateLimit 一次响应带回的限流状态
type RateLimit struct {
	Resource  string    `json:"resource"`
	Limit     int       `json:"limit"`
	Remaining int       `json:"remaining"`
	Reset     time.Time `json:"reset"`
}

// RateLimitedError 限流错误：Reset 是额度恢复时间，上层据此写 nextRunAt
type RateLimitedError struct {
	Resource   string
	Reset      time.Time
	StatusCode int
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("github api rate limited (resource=%s, reset=%s, status=%d)",
		e.Resource, e.Reset.Format(time.RFC3339), e.StatusCode)
}

// ErrUnauthorized token 无效
var ErrUnauthorized = errors.New("github api unauthorized")

// ClientOptions 客户端构造参数
type ClientOptions struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
	UserAgent  string
	RateFloor  int
}

// Client GitHub REST 客户端（并发安全）
type Client struct {
	baseURL   string
	token     string
	ua        string
	http      *http.Client
	rateFloor int

	mu   sync.Mutex
	rate map[string]RateLimit
}

// NewClient 构造客户端；BaseURL / Token 留空则回落到环境变量与默认值
func NewClient(opts ClientOptions) *Client {
	base := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if base == "" {
		base = strings.TrimRight(os.Getenv(EnvBaseURL), "/")
	}
	if base == "" {
		base = DefaultBaseURL
	}
	token := strings.TrimSpace(opts.Token)
	if token == "" {
		token = strings.TrimSpace(os.Getenv(EnvToken))
	}
	ua := opts.UserAgent
	if ua == "" {
		ua = defaultUserAgent
	}
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: defaultTimeout}
	}
	floor := opts.RateFloor
	if floor <= 0 {
		floor = defaultRateFloor
	}
	return &Client{
		baseURL:   base,
		token:     token,
		ua:        ua,
		http:      hc,
		rateFloor: floor,
		rate:      make(map[string]RateLimit),
	}
}

// BaseURL 当前基址（测试断言用）
func (c *Client) BaseURL() string { return c.baseURL }

// RateState 读取最近一次观察到的限流状态
func (c *Client) RateState(resource string) RateLimit {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rate[resource]
}

func (c *Client) setRate(rl RateLimit) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if rl.Resource == "" {
		rl.Resource = "core"
	}
	c.rate[rl.Resource] = rl
}

// Exhausted 额度是否已经耗尽（未到 reset 时间）
func (c *Client) Exhausted(resource string, now time.Time) (RateLimit, bool) {
	rl := c.RateState(resource)
	if rl.Reset.IsZero() {
		return rl, false
	}
	if rl.Remaining < c.rateFloor && now.Before(rl.Reset) {
		return rl, true
	}
	return rl, false
}

// do 发请求 + 统一限流处理
func (c *Client) do(ctx context.Context, method, path string, query url.Values, out interface{}) (*http.Response, error) {
	if _, ok := c.Exhausted(guessResource(path), time.Now()); ok {
		rl := c.RateState(guessResource(path))
		return nil, &RateLimitedError{Resource: rl.Resource, Reset: rl.Reset}
	}

	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			// 无头信息可依时按指数退避
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(1<<uint(attempt-1)) * time.Second):
			}
		}

		req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("github api: build request: %w", err)
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		req.Header.Set("User-Agent", c.ua)
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("github api: %s %s: %w", method, path, err)
			continue
		}

		rl := parseRateLimit(resp.Header, guessResource(path))
		if rl.Limit > 0 || rl.Remaining > 0 || !rl.Reset.IsZero() {
			c.setRate(rl)
		}

		switch {
		case resp.StatusCode == http.StatusUnauthorized:
			_ = resp.Body.Close()
			return resp, ErrUnauthorized
		case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			if rl.Remaining == 0 || resp.Header.Get("Retry-After") != "" || isRateLimitBody(body) {
				return resp, &RateLimitedError{Resource: rl.Resource, Reset: resetTime(rl, resp.Header), StatusCode: resp.StatusCode}
			}
			lastErr = fmt.Errorf("github api: %s %s: status %d: %s", method, path, resp.StatusCode, truncate(string(body), 200))
			continue
		case resp.StatusCode >= 500:
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("github api: %s %s: status %d", method, path, resp.StatusCode)
			continue
		case resp.StatusCode >= 400:
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			return resp, fmt.Errorf("github api: %s %s: status %d: %s", method, path, resp.StatusCode, truncate(string(body), 200))
		}

		if out != nil {
			if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out); err != nil {
				_ = resp.Body.Close()
				return resp, fmt.Errorf("github api: decode %s: %w", path, err)
			}
		}
		_ = resp.Body.Close()
		return resp, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("github api: %s %s: retries exhausted", method, path)
	}
	return nil, lastErr
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func isRateLimitBody(body []byte) bool {
	lower := strings.ToLower(string(body))
	return strings.Contains(lower, "rate limit") || strings.Contains(lower, "secondary rate")
}

// guessResource 按路径推断限流资源池（search 与 core 是两个独立配额）
func guessResource(path string) string {
	if strings.HasPrefix(path, "/search") {
		return "search"
	}
	return "core"
}

func parseRateLimit(h http.Header, resource string) RateLimit {
	rl := RateLimit{Resource: resource}
	rl.Limit = atoi(h.Get("X-RateLimit-Limit"))
	rl.Remaining = atoi(h.Get("X-RateLimit-Remaining"))
	if res := h.Get("X-RateLimit-Resource"); res != "" {
		rl.Resource = res
	}
	if reset := atoi64(h.Get("X-RateLimit-Reset")); reset > 0 {
		rl.Reset = time.Unix(reset, 0)
	}
	return rl
}

// resetTime 综合 reset / Retry-After，得出该退避到什么时候
func resetTime(rl RateLimit, h http.Header) time.Time {
	now := time.Now()
	candidate := rl.Reset
	if retryAfter := h.Get("Retry-After"); retryAfter != "" {
		if secs, err := strconv.Atoi(retryAfter); err == nil && secs > 0 {
			at := now.Add(time.Duration(secs) * time.Second)
			if candidate.IsZero() || at.After(candidate) {
				candidate = at
			}
		}
	}
	if candidate.IsZero() || !candidate.After(now) {
		// 没有可用 reset 时给一个保守的退避窗口
		candidate = now.Add(time.Hour)
	}
	return candidate
}

func atoi(s string) int {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return v
}

func atoi64(s string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// ---------- Search API ----------

// SearchItem 归一后的搜索结果条目
type SearchItem struct {
	Repo    string
	Path    string
	URL     string
	Commit  string
	Title   string
	Snippet string
}

// SearchResult 一次 Search API 调用的结果
type SearchResult struct {
	TotalCount int
	Incomplete bool
	Items      []SearchItem
}

type searchResponse struct {
	TotalCount        int  `json:"total_count"`
	IncompleteResults bool `json:"incomplete_results"`
	Items             []struct {
		ID         int64  `json:"id"`
		Number     int    `json:"number"`
		Title      string `json:"title"`
		Name       string `json:"name"`
		Body       string `json:"body"`
		HTMLURL    string `json:"html_url"`
		Path       string `json:"path"`
		SHA        string `json:"sha"`
		UpdatedAt  string `json:"updated_at"`
		Repository struct {
			FullName string `json:"full_name"`
			HTMLURL  string `json:"html_url"`
			Owner    struct {
				Login string `json:"login"`
			} `json:"owner"`
		} `json:"repository"`
		TextMatches []struct {
			Fragment string `json:"fragment"`
		} `json:"text_matches"`
	} `json:"items"`
}

// SearchKind 支持的 Search API 类型
const (
	SearchKindCode         = "code"
	SearchKindIssues       = "issues"
	SearchKindRepositories = "repositories"
)

// Search 调 Search API（code / issues / repositories）。
//
// 归一规则（去重键里的 commit 必须稳定，否则重跑会重复计数）：
//   - code：path + blob sha（同内容同 sha），snippet 取 text_matches[0]
//   - issues：path = "#<number>"，commit 取 issue id（议题本身没有 commit 概念）
//   - repositories：path = 仓库名，commit 取 updated_at（仓库更新即视为新状态）
func (c *Client) Search(ctx context.Context, kind, query string, page, perPage int) (*SearchResult, error) {
	switch kind {
	case SearchKindCode, SearchKindIssues, SearchKindRepositories:
	default:
		return nil, fmt.Errorf("github api: unsupported search kind %q", kind)
	}
	if page <= 0 {
		page = 1
	}
	if perPage <= 0 || perPage > 100 {
		perPage = 100
	}
	q := url.Values{}
	q.Set("q", query)
	q.Set("page", strconv.Itoa(page))
	q.Set("per_page", strconv.Itoa(perPage))

	var raw searchResponse
	if _, err := c.do(ctx, http.MethodGet, "/search/"+kind, q, &raw); err != nil {
		return nil, err
	}

	result := &SearchResult{TotalCount: raw.TotalCount, Incomplete: raw.IncompleteResults}
	result.Items = make([]SearchItem, 0, len(raw.Items))
	for _, item := range raw.Items {
		repo := item.Repository.FullName
		path := item.Path
		commit := item.SHA
		title := item.Title
		snippet := ""
		if len(item.TextMatches) > 0 {
			snippet = item.TextMatches[0].Fragment
		}

		switch kind {
		case SearchKindIssues:
			path = fmt.Sprintf("#%d", item.Number)
			commit = strconv.FormatInt(item.ID, 10)
			if title == "" {
				title = item.Title
			}
			if snippet == "" {
				snippet = item.Body
			}
		case SearchKindRepositories:
			path = repo
			commit = item.UpdatedAt
			if title == "" {
				title = item.Name
			}
			if snippet == "" {
				snippet = item.Body
			}
		case SearchKindCode:
			if title == "" {
				title = baseName(path)
			}
		}
		if commit == "" {
			commit = "HEAD"
		}
		result.Items = append(result.Items, SearchItem{
			Repo:    repo,
			Path:    path,
			URL:     item.HTMLURL,
			Commit:  commit,
			Title:   title,
			Snippet: snippet,
		})
	}
	return result, nil
}

func baseName(p string) string {
	if idx := strings.LastIndex(p, "/"); idx >= 0 && idx+1 < len(p) {
		return p[idx+1:]
	}
	return p
}

// ---------- 仓库 / Release ----------

// Release 仓库最新 release
type Release struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Body        string `json:"body"`
}

// LatestRelease 取 owner/repo 的最新 release（404 视为无 release，返回 nil 而非错误）
func (c *Client) LatestRelease(ctx context.Context, ownerRepo string) (*Release, error) {
	path := "/repos/" + strings.Trim(strings.TrimSpace(ownerRepo), "/") + "/releases/latest"
	var rel Release
	resp, err := c.do(ctx, http.MethodGet, path, nil, &rel)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &rel, nil
}

// RateLimitSnapshot /rate_limit 的归一结果
type RateLimitSnapshot struct {
	Core   RateLimit
	Search RateLimit
}

// GetRateLimit 查询当前 token 的额度（token_status 端点用）
func (c *Client) GetRateLimit(ctx context.Context) (*RateLimitSnapshot, error) {
	var raw struct {
		Resources struct {
			Core struct {
				Limit     int   `json:"limit"`
				Remaining int   `json:"remaining"`
				Reset     int64 `json:"reset"`
			} `json:"core"`
			Search struct {
				Limit     int   `json:"limit"`
				Remaining int   `json:"remaining"`
				Reset     int64 `json:"reset"`
			} `json:"search"`
		} `json:"resources"`
	}
	if _, err := c.do(ctx, http.MethodGet, "/rate_limit", nil, &raw); err != nil {
		return nil, err
	}
	snap := &RateLimitSnapshot{
		Core: RateLimit{
			Resource:  "core",
			Limit:     raw.Resources.Core.Limit,
			Remaining: raw.Resources.Core.Remaining,
			Reset:     time.Unix(raw.Resources.Core.Reset, 0),
		},
		Search: RateLimit{
			Resource:  "search",
			Limit:     raw.Resources.Search.Limit,
			Remaining: raw.Resources.Search.Remaining,
			Reset:     time.Unix(raw.Resources.Search.Reset, 0),
		},
	}
	c.setRate(snap.Core)
	c.setRate(snap.Search)
	return snap, nil
}

// HasToken 是否带 token（token_status 用来区分 missing）
func (c *Client) HasToken() bool { return c.token != "" }
