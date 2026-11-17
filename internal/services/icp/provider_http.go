package icp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// http provider：外部 ICP / 天眼查类服务骨架。
//
// 契约（baseURL 来自系统配置 icp.provider_url，token 来自 icp.token）：
//
//	POST {baseURL}/icp/domain   {"domain":"example.com"}
//	POST {baseURL}/icp/company  {"company":"某某科技有限公司"}
//	200 {"records":[{"domain":"example.com","company":"某某公司","icpNo":"京ICP备12345678号","siteName":"官网","type":"web"}]}
//
// 兼容两种常见包装：{"data":{"records":[...]}} 与 {"data":[...]}；
// 业务码放在 body 里（{"code":500,...} + HTTP 200）时同样按 ProviderError 处理。
// 非 2xx 一律返回 *ProviderError：429 / 5xx 由 service 层指数退避重试。

// maxProviderResponseBytes 单次响应读取上限（8MB），防止异常服务打爆内存
const maxProviderResponseBytes = 8 << 20

type httpProvider struct {
	baseURL string
	token   string
	client  *http.Client
}

func newHTTPProvider(cfg ProviderConfig, client *http.Client) *httpProvider {
	if client == nil {
		client = &http.Client{Timeout: DefaultProviderHTTPTimeout}
	}
	return &httpProvider{
		baseURL: strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		token:   strings.TrimSpace(cfg.Token),
		client:  client,
	}
}

// Name provider 名称
func (p *httpProvider) Name() string { return models.IcpProviderHTTP }

// Status 有 provider_url 即可用（token 可选）
func (p *httpProvider) Status() (bool, string) {
	if p.baseURL == "" {
		return false, "未配置 icp.provider_url（在系统配置里填写 ICP 服务地址后自动启用）"
	}
	return true, ""
}

// QueryDomain 按域名查备案主体
func (p *httpProvider) QueryDomain(ctx context.Context, domain string) ([]models.ICPRecord, error) {
	return p.post(ctx, "/icp/domain", map[string]string{"domain": domain})
}

// QueryCompany 按企业名查备案域名
func (p *httpProvider) QueryCompany(ctx context.Context, company string) ([]models.ICPRecord, error) {
	return p.post(ctx, "/icp/company", map[string]string{"company": company})
}

func (p *httpProvider) post(ctx context.Context, path string, payload map[string]string) ([]models.ICPRecord, error) {
	if p.baseURL == "" {
		return nil, fmt.Errorf("%w: 未配置 icp.provider_url", ErrProviderUnavailable)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("icp provider: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("icp provider: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("icp provider: request %s: %w", path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxProviderResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("icp provider: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &ProviderError{StatusCode: resp.StatusCode, Message: truncateText(string(raw), 300)}
	}
	return decodeICPRecords(raw)
}

// decodeICPRecords 兼容裸数组 / {"records":[]} / {"data":{"records":[]}} / {"data":[]}
func decodeICPRecords(raw []byte) ([]models.ICPRecord, error) {
	var direct []models.ICPRecord
	if err := json.Unmarshal(raw, &direct); err == nil {
		return direct, nil
	}
	var payload struct {
		Records []models.ICPRecord `json:"records"`
		Data    json.RawMessage    `json:"data"`
		Code    int                `json:"code"`
		Message string             `json:"message"`
		Msg     string             `json:"msg"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("icp provider: decode response: %w", err)
	}
	records := payload.Records
	if len(records) == 0 && len(payload.Data) > 0 {
		if err := json.Unmarshal(payload.Data, &direct); err == nil {
			records = direct
		} else {
			var wrapped struct {
				Records []models.ICPRecord `json:"records"`
			}
			if err := json.Unmarshal(payload.Data, &wrapped); err == nil {
				records = wrapped.Records
			}
		}
	}
	if payload.Code != 0 && payload.Code != http.StatusOK && len(records) == 0 {
		message := payload.Message
		if strings.TrimSpace(message) == "" {
			message = payload.Msg
		}
		return nil, &ProviderError{StatusCode: payload.Code, Message: message}
	}
	return records, nil
}

// truncateText 截断错误详情，避免把整页 HTML 塞进 lastError / 日志
func truncateText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}
