// Package intel 产出「漏洞情报」，供外部 planner（ARTEX）消费。
//
// 与漏洞扫描的区别：这个包**不声明漏洞**。它回答的是「这条 CVE 在不在我们的资产上成立」，
// 而答案允许是「不知道」——拿不到版本就是 unknown，绝不降级成 likely。
package intel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	nvdEndpoint = "https://services.nvd.nist.gov/rest/json/cves/2.0"
	// 无 key 时 NVD 是 5 次/30s。串行 + 6 秒间隔 + 6 小时缓存，正常量级够用。
	nvdMinInterval = 6 * time.Second
	cacheTTL       = 6 * time.Hour
	cacheColl      = "cve_cache"
)

var (
	nvdMu       sync.Mutex
	nvdLastCall time.Time
	httpClient  = &http.Client{Timeout: 30 * time.Second}
)

// CVERecord 是归一化后的单条 CVE，绝不透传 NVD 原始 JSON。
type CVERecord struct {
	CVE       string         `json:"cve"`
	Title     string         `json:"title"`
	URL       string         `json:"url"`
	Severity  string         `json:"severity"`
	Published string         `json:"published"`
	Source    string         `json:"source"`
	Summary   string         `json:"summary"`
	Ranges    []VersionRange `json:"-"`
	Products  []string       `json:"-"`
}

// VersionRange 是 CPE 里声明的受影响版本区间（有界时才有意义）。
type VersionRange struct {
	Product          string `bson:"product" json:"product"`
	VersionStartIncl string `bson:"vsi,omitempty" json:"version_start_including,omitempty"`
	VersionStartExcl string `bson:"vse,omitempty" json:"version_start_excluding,omitempty"`
	VersionEndIncl   string `bson:"vei,omitempty" json:"version_end_including,omitempty"`
	VersionEndExcl   string `bson:"vee,omitempty" json:"version_end_excluding,omitempty"`
}

type cacheDoc struct {
	Tech      string      `bson:"tech"`
	SinceDays int         `bson:"sinceDays"`
	FetchedAt time.Time   `bson:"fetchedAt"`
	Items     []cacheItem `bson:"items"`
}

type cacheItem struct {
	CVE       string         `bson:"cve"`
	Title     string         `bson:"title"`
	Severity  string         `bson:"severity"`
	Published string         `bson:"published"`
	Summary   string         `bson:"summary"`
	Ranges    []VersionRange `bson:"ranges"`
	Products  []string       `bson:"products"`
}

// FetchCVE 按技术名取 CVE：先看缓存，未过期直接返回，否则打 NVD。
func FetchCVE(ctx context.Context, tech string, sinceDays, limit int) ([]CVERecord, bool, error) {
	if mongodb.DB != nil {
		var doc cacheDoc
		err := mongodb.DB.Collection(cacheColl).FindOne(ctx, bson.M{"tech": strings.ToLower(tech), "sinceDays": sinceDays}).Decode(&doc)
		if err == nil && time.Since(doc.FetchedAt) < cacheTTL {
			return toRecords(tech, doc.Items, limit), true, nil
		}
	}
	items, err := fetchFromNVD(ctx, tech, sinceDays, limit)
	if err != nil {
		return nil, false, err
	}
	if mongodb.DB != nil {
		doc := cacheDoc{Tech: strings.ToLower(tech), SinceDays: sinceDays, FetchedAt: time.Now(), Items: items}
		_, _ = mongodb.DB.Collection(cacheColl).ReplaceOne(ctx,
			bson.M{"tech": doc.Tech, "sinceDays": sinceDays}, doc,
			options.Replace().SetUpsert(true))
	}
	return toRecords(tech, items, limit), false, nil
}

func toRecords(tech string, items []cacheItem, limit int) []CVERecord {
	out := make([]CVERecord, 0, len(items))
	for _, it := range items {
		if limit > 0 && len(out) >= limit {
			break
		}
		out = append(out, CVERecord{
			CVE: it.CVE, Title: it.Title,
			URL:      "https://nvd.nist.gov/vuln/detail/" + it.CVE,
			Severity: it.Severity, Published: it.Published,
			Source: "nvd", Summary: it.Summary,
			Ranges: it.Ranges, Products: it.Products,
		})
	}
	if len(tech) > 0 {
		// 记录查询用的技术名，便于排查「为什么这条 CVE 回来了」
		logger.Debug(fmt.Sprintf("intel: tech=%s items=%d", tech, len(out)))
	}
	return out
}

func fetchFromNVD(ctx context.Context, tech string, sinceDays, limit int) ([]cacheItem, error) {
	if sinceDays <= 0 {
		sinceDays = 90
	}
	now := time.Now().UTC()
	out := []cacheItem{}
	// NVD 的 pubStartDate..pubEndDate 窗口**不能超过 120 天**，超过直接 404（实测：
	// nginx + 180 天 → 404 空 body，看起来像「没有数据」，实际是窗口非法）。
	// 所以按 120 天切片，从最近往前翻，攒够 limit 就停。
	remaining := sinceDays
	end := now
	for remaining > 0 && len(out) < limit {
		span := 120
		if remaining < span {
			span = remaining
		}
		start := end.AddDate(0, 0, -span)
		items, err := nvdWindow(ctx, tech, start, end, limit-len(out))
		if err != nil {
			if len(out) > 0 {
				break
			}
			return nil, err
		}
		out = append(out, items...)
		end = start
		remaining -= span
	}
	return out, nil
}

// nvdWindow 取一个合法窗口（≤120 天）内的 CVE。
func nvdWindow(ctx context.Context, tech string, start, end time.Time, limit int) ([]cacheItem, error) {
	q := url.Values{}
	q.Set("keywordSearch", tech)
	q.Set("pubStartDate", start.Format("2006-01-02T15:04:05.000")+"Z")
	q.Set("pubEndDate", end.Format("2006-01-02T15:04:05.000")+"Z")
	q.Set("resultsPerPage", strconv.Itoa(maxInt(limit, 20)))

	nvdMu.Lock()
	if wait := nvdMinInterval - time.Since(nvdLastCall); wait > 0 {
		time.Sleep(wait)
	}
	nvdLastCall = time.Now()
	nvdMu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nvdEndpoint+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ScopeSentry-intel/1.0")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("nvd http %d: %s", resp.StatusCode, string(body))
	}
	var payload nvdResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return normalizeNVD(payload), nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
