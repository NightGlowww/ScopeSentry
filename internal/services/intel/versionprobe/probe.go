// Package versionprobe 给「有指纹但没版本」的资产补版本。
//
// 为什么需要它：intel_for_assets 的价值全在 stats.with_version 上。
// 实测 eToro 项目 12 台 Nginx 资产 with_version=0——存量响应头里根本没有版本号，
// 于是每条 CVE 都只能判 unknown，下游（ARTEX planner）会全部驳回。
//
// 做法：对目标发一次 GET，从 Server / X-Powered-By / 正文里按技术名抠版本，
// 结果写回 asset.techVersions（TTL 7 天），intel 读资产时直接用。
package versionprobe

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	// 9480 台待补，50 台/20 分钟要跑 63 小时——太慢。
	// 300 台/5 分钟、并发 24 ≈ 2.6 小时铺完一遍，之后靠 7 天 TTL 滚动。
	batchSize  = 300
	probeConc  = 24
	probeTTL   = 7 * 24 * time.Hour
	bodyLimit  = 16 * 1024
	probeTimeo = 10 * time.Second
)

var httpClient = &http.Client{
	Timeout: probeTimeo,
	Transport: &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
		MaxIdleConnsPerHost: 2,
		DisableKeepAlives:   true,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

type assetRow struct {
	ID           any               `bson:"_id"`
	Host         string            `bson:"host"`
	URL          string            `bson:"url"`
	Technologies []string          `bson:"technologies"`
	TechVersions map[string]string `bson:"techVersions"`
}

// RunOnce 补一批版本，返回（探测台数, 新拿到版本的台数）。
func RunOnce(ctx context.Context) (int, int, error) {
	if mongodb.DB == nil {
		logger.Warn("versionprobe: mongodb not ready, skip")
		return 0, 0, nil
	}
	col := mongodb.DB.Collection("asset")
	cutoff := time.Now().Add(-probeTTL)
	filter := bson.M{
		// 必须用 "technologies.0" exists：`{$ne: []}` 会把 technologies=null 也算进来，
		// 而库里 35629 条正是 null，自然序前 50 条全是它 → 游标拿到一堆空技术名、rows=0。
		// 这条错让 versionprobe 白跑了两轮（matched=9714 但 rows=0）。
		"technologies.0": bson.M{"$exists": true},
		"host":           bson.M{"$exists": true, "$ne": ""},
		"$or": []bson.M{
			{"techVersionsAt": bson.M{"$exists": false}},
			{"techVersionsAt": bson.M{"$lt": cutoff}},
		},
	}
	cur, err := col.Find(ctx, filter, options.Find().
		SetLimit(batchSize).
		SetProjection(bson.M{"host": 1, "url": 1, "technologies": 1, "techVersions": 1}))
	if err != nil {
		return 0, 0, err
	}
	defer cur.Close(ctx)
	// 诊断：连「匹配到几台」「第一条解码错在哪」一起打出来，
	// 否则 rows=0 时分不清是查询没命中还是解码失败（2026-11-25 踩过）。
	if n, cerr := col.CountDocuments(ctx, filter); cerr == nil {
		logger.Info(fmt.Sprintf("versionprobe: matched=%d", n))
	}
	var rows []assetRow
	decodeErr := ""
	for cur.Next(ctx) {
		var r assetRow
		if derr := cur.Decode(&r); derr != nil {
			if decodeErr == "" {
				decodeErr = derr.Error()
			}
			continue
		}
		if len(r.Technologies) > 0 {
			rows = append(rows, r)
		}
	}
	if decodeErr != "" {
		logger.Warn("versionprobe: decode error: " + decodeErr)
	}
	// Next() 返回 false 有两种原因：遍历完 / 游标出错。不查 Err 就只能看到 rows=0，
	// 分不清「没数据」和「查询失败」（2026-11-25：matched=9714 却 rows=0，就是这里）。
	if cerr := cur.Err(); cerr != nil {
		logger.Error("versionprobe: cursor error: " + cerr.Error())
		return 0, 0, cerr
	}
	if len(rows) == 0 {
		logger.Info("versionprobe: tick rows=0")
		return 0, 0, nil
	}
	logger.Info(fmt.Sprintf("versionprobe: tick rows=%d", len(rows)))

	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		probed int
		gotVer int
		sem    = make(chan struct{}, probeConc)
	)
	for _, row := range rows {
		wg.Add(1)
		go func(r assetRow) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			versions := ProbeHost(ctx, r)
			mu.Lock()
			probed++
			if len(versions) > 0 {
				gotVer++
			}
			mu.Unlock()
			_, uerr := col.UpdateOne(ctx, bson.M{"_id": r.ID}, bson.M{"$set": bson.M{
				"techVersions":   versions,
				"techVersionsAt": time.Now(),
			}})
			if uerr != nil {
				logger.Error(fmt.Sprintf("versionprobe: update %s failed: %v", r.Host, uerr))
			}
		}(row)
	}
	wg.Wait()
	logger.Info(fmt.Sprintf("versionprobe: done probed=%d withVersion=%d", probed, gotVer))
	return probed, gotVer, nil
}

// ProbeHost 探测单台资产，返回 {技术名: 版本}。拿不到就是空 map，不编造。
func ProbeHost(ctx context.Context, r assetRow) map[string]string {
	headers, body := fetch(ctx, r)
	if headers == "" && body == "" {
		return map[string]string{}
	}
	out := map[string]string{}
	for _, tech := range r.Technologies {
		key := normalizeKey(tech)
		if key == "" {
			continue
		}
		if v := matchVersion(key, headers, body); v != "" {
			out[key] = v
		}
	}
	return out
}

func fetch(ctx context.Context, r assetRow) (string, string) {
	candidates := []string{}
	if strings.HasPrefix(r.URL, "http") {
		candidates = append(candidates, r.URL)
	}
	if r.Host != "" {
		candidates = append(candidates, "https://"+r.Host, "http://"+r.Host)
	}
	for _, u := range candidates {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 ScopeSentry-versionprobe")
		resp, err := httpClient.Do(req)
		if err != nil {
			continue
		}
		hdr := collectHeaders(resp)
		b, _ := io.ReadAll(io.LimitReader(resp.Body, bodyLimit))
		resp.Body.Close()
		return hdr, string(b)
	}
	return "", ""
}

func collectHeaders(resp *http.Response) string {
	var sb strings.Builder
	for k, vs := range resp.Header {
		sb.WriteString(k)
		sb.WriteString(": ")
		sb.WriteString(strings.Join(vs, ", "))
		sb.WriteString("\n")
	}
	return sb.String()
}

// matchVersion 在头与正文里找 "<tech>/<ver>" 或 "<tech> <ver>"。
func matchVersion(key, headers, body string) string {
	re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(key) + `\s*[/ ]\s*v?([0-9][0-9A-Za-z.\-_]{0,15})`)
	if m := re.FindStringSubmatch(headers); len(m) > 1 {
		return clean(m[1])
	}
	if m := re.FindStringSubmatch(body); len(m) > 1 {
		return clean(m[1])
	}
	return ""
}

func clean(v string) string {
	return strings.TrimSuffix(strings.TrimSpace(v), ".")
}

func normalizeKey(t string) string {
	s := strings.ToLower(strings.TrimSpace(t))
	if i := strings.IndexAny(s, ":/("); i > 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return ""
	}
	return s
}

// Start 每 20 分钟补一批。VERSIONPROBE=off 关闭。
func Start(ctx context.Context, disabled bool) {
	if disabled {
		logger.Info("versionprobe: disabled")
		return
	}
	logger.Info("versionprobe: started (batch=300, every 5m)")
	go func() {
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for {
			if _, _, err := RunOnce(ctx); err != nil {
				logger.Error("versionprobe: " + err.Error())
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}
