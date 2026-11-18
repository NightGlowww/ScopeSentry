package fofa

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// http provider：FOFA 官方 API（默认 https://fofa.info，可用 fofa.base_url / FOFA_BASE_URL 覆盖）。
//
// 用到的两个端点：
//
//	GET /api/v1/search/all?key=KEY&qbase64=BASE64(query)&page=1&size=100&fields=host
//	    200 {"error":false,"total":123,"page":1,"size":100,
//	         "results":[["1.2.3.4"],["https://a.example.com:8443/path"]]}
//	GET /api/v1/info/my?key=KEY
//	    200 {"error":false,"username":"x@example.com","fofa_point":900,
//	         "remain_free_point":10,"remain_api_query":0,"vip_level":2,"is_vip":true}
//
// 业务错误（{"error":true,"errmsg":"..."}）按 400 返回 *ProviderError；
// HTTP 层的 401/429/5xx 保留原状态码。results 行既可能是字符串，也可能是数组
// （fields 多列时是数组），两种都按第一列解码。

// maxProviderResponseBytes 单次响应读取上限（8MB），防止异常服务打爆内存
const maxProviderResponseBytes = 8 << 20

type httpProvider struct {
	baseURL   string
	key       string
	keySource string
	client    *http.Client
}

func newHTTPProvider(cfg ProviderConfig, client *http.Client) *httpProvider {
	if client == nil {
		client = &http.Client{Timeout: DefaultProviderTimeout}
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &httpProvider{
		baseURL:   baseURL,
		key:       strings.TrimSpace(cfg.Key),
		keySource: strings.TrimSpace(cfg.KeySource),
		client:    client,
	}
}

// Name provider 名称
func (p *httpProvider) Name() string { return models.FofaProviderHTTP }

// Status 有 key 才可用：可用时会顺带查一次余额（失败则不可用 + 原因）
func (p *httpProvider) Status(ctx context.Context) ProviderStatus {
	status := ProviderStatus{
		Provider:   models.FofaProviderHTTP,
		Configured: p.key != "",
		KeySource:  p.keySource,
		BaseURL:    p.baseURL,
	}
	if p.key == "" {
		status.KeySource = "none"
		status.Reason = "未配置 fofa.key（可直接在本页下方填写 FOFA Key，或在系统配置/环境变量 FOFA_KEY 中设置）"
		return status
	}
	balance, err := p.fetchBalance(ctx)
	if err != nil {
		status.Reason = err.Error()
		return status
	}
	status.Available = true
	status.Balance = balance
	return status
}

// Search 取第 page 页 host（page 从 1 开始，size 单页上限由调用方保证 ≤ 100）
func (p *httpProvider) Search(ctx context.Context, query string, page, size int) (*SearchPage, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("%w: query 不能为空", ErrProviderUnavailable)
	}
	if p.key == "" {
		return nil, fmt.Errorf("%w: 未配置 fofa.key", ErrProviderUnavailable)
	}
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = models.FofaHostPageSize
	}

	endpoint, err := url.Parse(p.baseURL + "/api/v1/search/all")
	if err != nil {
		return nil, fmt.Errorf("fofa provider: build url: %w", err)
	}
	params := url.Values{}
	params.Set("key", p.key)
	// FOFA 只接受 base64 后的 query（qbase64）
	params.Set("qbase64", base64.StdEncoding.EncodeToString([]byte(query)))
	params.Set("page", strconv.Itoa(page))
	params.Set("size", strconv.Itoa(size))
	params.Set("fields", "host")
	endpoint.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("fofa provider: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fofa provider: request search: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxProviderResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("fofa provider: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &ProviderError{StatusCode: resp.StatusCode, Message: truncateText(string(raw), 300)}
	}

	var payload struct {
		Error          bool              `json:"error"`
		ErrMsg         string            `json:"errmsg"`
		Total          int64             `json:"total"`
		Size           int               `json:"size"`
		Page           int               `json:"page"`
		Results        []json.RawMessage `json:"results"`
		ConsumedFPoint float64           `json:"consumed_fpoint"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("fofa provider: decode response: %w", err)
	}
	if payload.Error {
		return nil, &ProviderError{StatusCode: http.StatusBadRequest, Message: truncateText(payload.ErrMsg, 300)}
	}

	rawHosts := decodeFofaRows(payload.Results)
	hosts, _ := models.NormalizeFofaHosts(rawHosts)
	return &SearchPage{
		Provider:       models.FofaProviderHTTP,
		Query:          query,
		Page:           page,
		Size:           len(hosts),
		Total:          payload.Total,
		Hosts:          hosts,
		ConsumedFPoint: payload.ConsumedFPoint,
	}, nil
}

// fetchBalance 查一次账户余额（status 端点用）
func (p *httpProvider) fetchBalance(ctx context.Context) (*models.FofaBalance, error) {
	endpoint, err := url.Parse(p.baseURL + "/api/v1/info/my")
	if err != nil {
		return nil, fmt.Errorf("fofa provider: build url: %w", err)
	}
	params := url.Values{}
	params.Set("key", p.key)
	endpoint.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("fofa provider: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fofa provider: request info: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxProviderResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("fofa provider: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &ProviderError{StatusCode: resp.StatusCode, Message: truncateText(string(raw), 300)}
	}

	var payload struct {
		Error           bool     `json:"error"`
		ErrMsg          string   `json:"errmsg"`
		Username        string   `json:"username"`
		FofaPoint       *float64 `json:"fofa_point"`
		RemainFreePoint *float64 `json:"remain_free_point"`
		RemainAPIQuery  *float64 `json:"remain_api_query"`
		VIPLevel        int      `json:"vip_level"`
		IsVIP           bool     `json:"is_vip"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("fofa provider: decode info: %w", err)
	}
	if payload.Error {
		return nil, &ProviderError{StatusCode: http.StatusBadRequest, Message: truncateText(payload.ErrMsg, 300)}
	}
	return &models.FofaBalance{
		Username:        strings.TrimSpace(payload.Username),
		FofaPoint:       payload.FofaPoint,
		RemainFreePoint: payload.RemainFreePoint,
		RemainAPIQuery:  payload.RemainAPIQuery,
		VIPLevel:        payload.VIPLevel,
		IsVIP:           payload.IsVIP,
	}, nil
}

// decodeFofaRows results 行兼容两种形状：["host"]（fields 单列）与 "host"
func decodeFofaRows(rows []json.RawMessage) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		var single string
		if err := json.Unmarshal(row, &single); err == nil {
			out = append(out, single)
			continue
		}
		var cells []json.RawMessage
		if err := json.Unmarshal(row, &cells); err != nil || len(cells) == 0 {
			continue
		}
		var first string
		if err := json.Unmarshal(cells[0], &first); err == nil {
			out = append(out, first)
		}
	}
	return out
}

// truncateText 截断错误详情，避免把整页 HTML 塞进 reason / 日志
func truncateText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}
