package intelfeed

// 多源情报订阅：把「安全公告 / 研究文章 / 在野利用清单」统一收进 ScopeSentry。
//
// 设计取舍：
//   · 只做两类解析：通用 RSS/Atom（覆盖绝大多数博客与公告）和 CISA KEV 的 JSON。
//     不追求万能：每加一个特殊格式就多一处会坏的地方。
//   · 每条源自己带 intervalHours，跑完写 lastRunAt/lastError，出问题在列表里能看见。
//   · 去重键 = sha1(source + link|title)，同一篇文章永远只入库一次。
//   · 每次最多取 maxItemsPerFetch 条（MSRC 的 feed 有 5776 条，不限流会一次灌爆）。

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	collFeeds      = "intel_feeds"
	collItems      = "intel_items"
	maxItems       = 120
	fetchAttempts  = 3
)

// Feed 一条订阅源
type Feed struct {
	ID            string     `bson:"_id" json:"id"`
	Name          string     `bson:"name" json:"name"`
	URL           string     `bson:"url" json:"url"`
	Kind          string     `bson:"kind" json:"kind"` // rss | kev
	Enabled       bool       `bson:"enabled" json:"enabled"`
	IntervalHours int        `bson:"intervalHours" json:"intervalHours"`
	LastRunAt     *time.Time `bson:"lastRunAt,omitempty" json:"lastRunAt,omitempty"`
	LastError     string     `bson:"lastError,omitempty" json:"lastError,omitempty"`
	ItemCount     int        `bson:"itemCount" json:"itemCount"`
}

// Item 一条情报
type Item struct {
	Source    string `bson:"source" json:"source"`
	Title     string `bson:"title" json:"title"`
	URL       string `bson:"url" json:"url"`
	Summary   string `bson:"summary,omitempty" json:"summary,omitempty"`
	Published string `bson:"published,omitempty" json:"published,omitempty"`
	Hash      string `bson:"hash" json:"hash"`
	AddedAt   string `bson:"addedAt" json:"addedAt"`
}

// defaultFeeds 首次启动时种入；都是 2026-11-26 实测可达的源。
var defaultFeeds = []Feed{
	// 用 CISA 官方仓库的镜像：www.cisa.gov 由 Akamai 前置，对云主机 IP 间歇性 403
	// （2026-11-26 实测：同一台机器 curl 有时 200 有时 403），GitHub raw 稳定且内容一致（同 1,898,186 字节）。
	{ID: "cisa-kev", Name: "CISA KEV（在野利用）", URL: "https://raw.githubusercontent.com/cisagov/kev-data/main/known_exploited_vulnerabilities.json", Kind: "kev", Enabled: true, IntervalHours: 12},
	{ID: "wiz", Name: "Wiz Research", URL: "https://www.wiz.io/feed/rss.xml", Kind: "rss", Enabled: true, IntervalHours: 6},
	{ID: "portswigger", Name: "PortSwigger Research", URL: "https://portswigger.net/research/rss", Kind: "rss", Enabled: true, IntervalHours: 6},
	{ID: "xz-aliyun", Name: "先知社区", URL: "https://xz.aliyun.com/feed", Kind: "rss", Enabled: true, IntervalHours: 6},
	{ID: "msrc", Name: "Microsoft MSRC", URL: "https://api.msrc.microsoft.com/update-guide/rss", Kind: "rss", Enabled: true, IntervalHours: 12},
	// P0 走 Google 域名，境内服务器不可达（2026-11-26 实测 i/o timeout）。
	// 保留条目但默认关闭：换个出口（比如东京节点）再打开即可。
	{ID: "p0", Name: "Google Project Zero（境内不可达）", URL: "https://projectzero.google/feed.xml", Kind: "rss", Enabled: false, IntervalHours: 12},
	// 直接用 feedburner 最终地址：thehackernews.com 会 302 过去，多一跳更容易超时。
	{ID: "thn", Name: "The Hacker News", URL: "https://feeds.feedburner.com/TheHackersNews", Kind: "rss", Enabled: true, IntervalHours: 6},
	{ID: "helpnet", Name: "Help Net Security", URL: "https://www.helpnetsecurity.com/feed/", Kind: "rss", Enabled: true, IntervalHours: 12},
	{ID: "krebs", Name: "Krebs on Security", URL: "https://krebsonsecurity.com/feed/", Kind: "rss", Enabled: true, IntervalHours: 12},
	// 以下 7 个源是 2026-11-26 从 prod 出口逐个实测 200 + 合法 RSS 后加进来的。
	// 挑的都是「有具体技术细节、能反推出攻击面」的源，纯新闻聚合站不收。
	{ID: "bleepingcomputer", Name: "BleepingComputer", URL: "https://www.bleepingcomputer.com/feed/", Kind: "rss", Enabled: true, IntervalHours: 6},
	{ID: "securityweek", Name: "SecurityWeek", URL: "https://www.securityweek.com/feed/", Kind: "rss", Enabled: true, IntervalHours: 12},
	{ID: "unit42", Name: "Palo Alto Unit 42", URL: "https://unit42.paloaltonetworks.com/feed/", Kind: "rss", Enabled: true, IntervalHours: 12},
	{ID: "talos", Name: "Cisco Talos", URL: "https://blog.talosintelligence.com/rss/", Kind: "rss", Enabled: true, IntervalHours: 12},
	{ID: "checkpoint", Name: "Check Point Research", URL: "https://research.checkpoint.com/feed/", Kind: "rss", Enabled: true, IntervalHours: 12},
	{ID: "securelist", Name: "Kaspersky Securelist", URL: "https://securelist.com/feed/", Kind: "rss", Enabled: true, IntervalHours: 12},
	{ID: "darkreading", Name: "Dark Reading", URL: "https://www.darkreading.com/rss.xml", Kind: "rss", Enabled: true, IntervalHours: 12},
}

var httpClient = &http.Client{Timeout: 40 * time.Second}

// Start 每 30 分钟扫描一次到期的源。INTELFEED=off 关闭。
func Start(ctx context.Context) {
	go func() {
		seed(ctx)
		t := time.NewTicker(30 * time.Minute)
		defer t.Stop()
		for {
			runDue(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

func seed(ctx context.Context) {
	if mongodb.DB == nil {
		return
	}
	coll := mongodb.DB.Collection(collFeeds)
	for _, f := range defaultFeeds {
		// url/name/kind 用 $set：源地址换了（比如 CISA 被 Akamai 挡、P0 换域名）要能自动纠正，
		// 而 enabled/intervalHours 交给用户，不能被种子覆盖。
		// 注意：同一个字段不能同时出现在 $set 和 $setOnInsert 里，Mongo 会整条更新报错
		// （2026-11-26 踩过：seed 静默失败，源的 url 一直没被纠正）。
		_, _ = coll.UpdateOne(ctx, bson.M{"_id": f.ID}, bson.M{
			"$set": bson.M{"url": f.URL, "name": f.Name, "kind": f.Kind},
			"$setOnInsert": bson.M{
				"enabled":       f.Enabled,
				"intervalHours": f.IntervalHours,
			},
		}, options.Update().SetUpsert(true))
	}
}

func runDue(ctx context.Context) {
	if mongodb.DB == nil || strings.EqualFold(strings.TrimSpace(os.Getenv("INTELFEED")), "off") {
		return
	}
	coll := mongodb.DB.Collection(collFeeds)
	cur, err := coll.Find(ctx, bson.M{"enabled": true})
	if err != nil {
		return
	}
	var feeds []Feed
	_ = cur.All(ctx, &feeds)
	_ = cur.Close(ctx)
	for _, f := range feeds {
		if f.LastRunAt != nil && time.Since(*f.LastRunAt) < time.Duration(f.IntervalHours)*time.Hour {
			continue
		}
		runOne(ctx, coll, f)
	}
}

// runOne 抓一个源、入库、把结果写回源文档。返回写入后的状态。
func runOne(ctx context.Context, coll *mongo.Collection, f Feed) bson.M {
	items, ferr := fetch(ctx, f)
	now := time.Now()
	upd := bson.M{"lastRunAt": now}
	res := bson.M{"id": f.ID, "name": f.Name}
	if ferr != nil {
		upd["lastError"] = ferr.Error()
		res["error"] = ferr.Error()
		logger.Warn("intelfeed: " + f.Name + " failed: " + ferr.Error())
	} else {
		upd["lastError"] = ""
		n := ingest(ctx, f, items)
		upd["itemCount"] = n
		res["added"] = n
		res["fetched"] = len(items)
		if n > 0 {
			logger.Info(fmt.Sprintf("intelfeed: %s +%d items", f.Name, n))
		}
	}
	_, _ = coll.UpdateOne(ctx, bson.M{"_id": f.ID}, bson.M{"$set": upd})
	return res
}

// RunNow 无视 intervalHours 立即跑一轮：id 为空跑全部启用的源，否则只跑指定源。
// 给运维和前端「立即刷新」按钮用；不做并发，源之间互相限速是好事。
func RunNow(ctx context.Context, id string) ([]bson.M, error) {
	if mongodb.DB == nil {
		return nil, fmt.Errorf("mongo not ready")
	}
	coll := mongodb.DB.Collection(collFeeds)
	filter := bson.M{"enabled": true}
	if strings.TrimSpace(id) != "" {
		filter = bson.M{"_id": strings.TrimSpace(id)}
	}
	cur, err := coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	var feeds []Feed
	_ = cur.All(ctx, &feeds)
	_ = cur.Close(ctx)
	if len(feeds) == 0 {
		return nil, fmt.Errorf("no matching feed")
	}
	out := make([]bson.M, 0, len(feeds))
	for _, f := range feeds {
		out = append(out, runOne(ctx, coll, f))
	}
	return out, nil
}

// ingest 写入并返回新增条数。
func ingest(ctx context.Context, f Feed, items []Item) int {
	if len(items) == 0 {
		return 0
	}
	coll := mongodb.DB.Collection(collItems)
	now := time.Now().Format("2006-01-02 15:04:05")
	newCount := 0
	for _, it := range items {
		it.Source = f.ID
		it.AddedAt = now
		it.Hash = hashKey(f.ID, it.URL, it.Title)
		res, err := coll.UpdateOne(ctx, bson.M{"hash": it.Hash}, bson.M{"$setOnInsert": it}, options.Update().SetUpsert(true))
		if err == nil && res.UpsertedCount > 0 {
			newCount++
		}
	}
	return newCount
}

func hashKey(parts ...string) string {
	h := sha1.Sum([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(h[:])
}

// fetch 按 kind 抓取并归一成 Item 列表。
//
// 重试是必需的，不是保险：2026-11-26 实测同一台机器同一秒内，
// feedburner（Google IP）会 dial timeout、helpnetsecurity 会返 202 排队页，
// 隔几秒再打就 200。单次失败直接标红会让列表长期挂着假故障。
func fetch(ctx context.Context, f Feed) ([]Item, error) {
	var lastErr error
	for attempt := 1; attempt <= fetchAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt-1) * 2 * time.Second):
			}
		}
		items, err := fetchOnce(ctx, f)
		if err == nil {
			return items, nil
		}
		lastErr = err
		if !retryable(err) {
			break
		}
	}
	// Go 的 TLS 指纹被 Cloudflare 挡（bleepingcomputer / securityweek 恒定 403，
	// helpnetsecurity 走 CloudFront 恒定 202 挑战页），换 HTTP 版本、补全浏览器头都无效
	// （2026-11-26 实测 h1/h2 双栈 + 完整 sec-ch-ua 全套头都是 403）。
	// curl 的 JA3 在它们的白名单里，实测同样 UA 头 200。用 curl 兜底一次。
	if items, err := fetchViaCurl(ctx, f); err == nil {
		return items, nil
	}
	return nil, lastErr
}

// curlBin 走系统 curl 兜底。镜像里自带（Debian 12 /usr/bin/curl）。
var curlBin = "curl"

// fetchViaCurl 用 curl 抓一次再按 kind 解析。INTELFEED_CURL=off 可关掉。
func fetchViaCurl(ctx context.Context, f Feed) ([]Item, error) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("INTELFEED_CURL")), "off") {
		return nil, fmt.Errorf("curl fallback disabled")
	}
	cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, curlBin,
		"-sS", "-L", "--max-time", "35", "--compressed",
		"-A", browserUA,
		"-H", "Accept: application/rss+xml, application/atom+xml, application/xml;q=0.9, */*;q=0.8",
		"-H", "Accept-Language: en-US,en;q=0.9",
		"-w", "\n%{http_code}",
		f.URL,
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil && out.Len() == 0 {
		return nil, err
	}
	raw := out.Bytes()
	// 末尾是 -w 打的 http_code
	idx := bytes.LastIndexByte(raw, '\n')
	if idx < 0 {
		return nil, fmt.Errorf("curl: empty output")
	}
	codeStr := strings.TrimSpace(string(raw[idx+1:]))
	body := raw[:idx]
	if codeStr != "200" {
		return nil, fmt.Errorf("curl http %s", codeStr)
	}
	switch f.Kind {
	case "kev":
		return parseKEV(body)
	default:
		return parseRSS(body)
	}
}

// browserUA 与 Go 请求共用同一个 UA，避免两个通道表现不一致。
const browserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// errHTTP 带状态码，供 retryable 判断。
type errHTTP struct {
	Code int
	URL  string
}

func (e *errHTTP) Error() string { return fmt.Sprintf("http %d", e.Code) }

// retryable 只对「可能是抖动」的错误重试：网络错误、超时、5xx、202/429。
// 404/403 这类确定性拒绝重试也没用，直接返回。
func retryable(err error) bool {
	if err == nil {
		return false
	}
	var he *errHTTP
	if errors.As(err, &he) {
		return he.Code == http.StatusAccepted || he.Code == http.StatusTooManyRequests || he.Code >= 500
	}
	return true // 网络层错误（dial timeout / TLS handshake / EOF）都值得再试
}

func fetchOnce(ctx context.Context, f Feed) ([]Item, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.URL, nil)
	if err != nil {
		return nil, err
	}
	// 用浏览器 UA：CISA 对自定义 UA 直接 403（实测 2026-11-26，curl 默认 UA 反而 200）。
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml;q=0.9, */*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &errHTTP{Code: resp.StatusCode, URL: f.URL}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	switch f.Kind {
	case "kev":
		return parseKEV(body)
	default:
		return parseRSS(body)
	}
}

// ---------- RSS / Atom ----------

type rssDoc struct {
	Channel struct {
		Items []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			Description string `xml:"description"`
			PubDate     string `xml:"pubDate"`
		} `xml:"item"`
	} `xml:"channel"`
	Entries []struct {
		Title   string `xml:"title"`
		Summary string `xml:"summary"`
		Pub     string `xml:"published"`
		Updated string `xml:"updated"`
		Links   []struct {
			Href string `xml:"href,attr"`
			Rel  string `xml:"rel,attr"`
		} `xml:"link"`
	} `xml:"entry"`
}

func parseRSS(body []byte) ([]Item, error) {
	var d rssDoc
	if err := xml.Unmarshal(body, &d); err != nil {
		return nil, err
	}
	out := make([]Item, 0, maxItems)
	for _, it := range d.Channel.Items {
		if len(out) >= maxItems {
			break
		}
		out = append(out, Item{Title: clean(it.Title), URL: clean(it.Link), Summary: clean(it.Description), Published: clean(it.PubDate)})
	}
	for _, e := range d.Entries {
		if len(out) >= maxItems {
			break
		}
		link := ""
		for _, l := range e.Links {
			if l.Rel == "" || l.Rel == "alternate" {
				link = l.Href
				break
			}
		}
		pub := e.Pub
		if pub == "" {
			pub = e.Updated
		}
		out = append(out, Item{Title: clean(e.Title), URL: clean(link), Summary: clean(e.Summary), Published: clean(pub)})
	}
	return out, nil
}

// ---------- CISA KEV ----------

type kevDoc struct {
	Vulnerabilities []struct {
		CVE         string `json:"cveID"`
		Vendor      string `json:"vendorProject"`
		Product     string `json:"product"`
		Name        string `json:"vulnerabilityName"`
		DateAdded   string `json:"dateAdded"`
		Description string `json:"shortDescription"`
	} `json:"vulnerabilities"`
}

func parseKEV(body []byte) ([]Item, error) {
	var d kevDoc
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, err
	}
	// KEV 是「全量清单」，按 dateAdded 倒序取新增的；不排序的话早期 CVE 会占满配额
	vs := d.Vulnerabilities
	for i := 0; i < len(vs); i++ {
		for j := i + 1; j < len(vs); j++ {
			if vs[j].DateAdded > vs[i].DateAdded {
				vs[i], vs[j] = vs[j], vs[i]
			}
		}
	}
	out := make([]Item, 0, maxItems)
	for _, v := range vs {
		if len(out) >= maxItems {
			break
		}
		out = append(out, Item{
			Title:     fmt.Sprintf("%s %s %s（%s）", v.Vendor, v.Product, v.Name, v.CVE),
			URL:       "https://nvd.nist.gov/vuln/detail/" + v.CVE,
			Summary:   clean(v.Description),
			Published: v.DateAdded,
		})
	}
	return out, nil
}

func clean(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "<![CDATA[", "")
	s = strings.ReplaceAll(s, "]]>", "")
	if len(s) > 500 {
		s = s[:500]
	}
	return s
}
