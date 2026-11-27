package intel

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// 外部服务预算：NVD 是「有则更好、没有也不能拖死工具」的依赖。
const (
	defaultBudget   = 8 * time.Second
	defaultMaxTechs = 12
	maxSingleFetch  = 10 * time.Second
)

// Options 对应 MCP 入参。
type Options struct {
	Project   string
	Hosts     []string
	Techs     []string
	SinceDays int
	Limit     int
	// Budget 是整次情报构建的墙钟预算。NVD 是外部服务，不通时每个技术名要等满 HTTP 超时，
	// 不设预算的话全库调用会一直挂到 MCP 客户端超时（2026-11-28 实测 intel_for_assets 直接 REQUEST_TIMEOUT）。
	Budget time.Duration
	// MaxTechs 限制单次最多查几个技术名（按宿主数从多到少）。全库有上百个指纹，
	// 逐个查既慢又没意义——报告里会如实说明截断了多少。
	MaxTechs int
}

// Affected 是「某资产上某技术 × 某 CVE」的判定结果。
type Affected struct {
	Host          string  `json:"host"`
	Tech          string  `json:"tech"`
	Version       *string `json:"version"`
	VersionSource *string `json:"version_source"`
	Applicability string  `json:"applicability"`
	Reason        string  `json:"reason"`
}

// Item 是一条归一化情报。
type Item struct {
	CVE       string     `json:"cve"`
	Title     string     `json:"title"`
	URL       string     `json:"url"`
	Severity  string     `json:"severity"`
	Published string     `json:"published"`
	Source    string     `json:"source"`
	Summary   string     `json:"summary"`
	Affected  []Affected `json:"affected"`
	Unmatched []string   `json:"unmatched_hosts"`
}

// Stats 必须真实：with_version 决定下游能不能用这条情报。
type Stats struct {
	AssetsScanned  int `json:"assets_scanned"`
	WithVersion    int `json:"with_version"`
	WithoutVersion int `json:"without_version"`
	// 下面几项说明「这次结果覆盖了多少」，避免把截断/失败读成「没有 CVE」。
	TechsTotal       int  `json:"techs_total"`
	TechsQueried     int  `json:"techs_queried"`
	TechsSkipped     int  `json:"techs_skipped"`
	DeadlineHit      bool `json:"deadline_hit"`
	UpstreamFailures int  `json:"upstream_failures"`
	ElapsedMS        int  `json:"elapsed_ms"`
}

// Report 是工具出参。
type Report struct {
	Items []Item `json:"items"`
	Stats Stats  `json:"stats"`
}

type assetDoc struct {
	Host         string   `bson:"host"`
	Port         any      `bson:"port"`
	URL          string   `bson:"url"`
	Technologies []string `bson:"technologies"`
	RawHeaders   string   `bson:"rawheaders"`
	Body         string   `bson:"body"`
	WebServer    string   `bson:"webServer"`
	Title        string   `bson:"title"`
	// versionprobe 回填的版本（技术名归一化 → 版本号），TTL 7 天。
	TechVersions map[string]string `bson:"techVersions"`
}

// BuildReport 是工具主逻辑。
func BuildReport(ctx context.Context, opt Options) (*Report, error) {
	if mongodb.DB == nil {
		return nil, fmt.Errorf("数据库未就绪")
	}
	if opt.SinceDays <= 0 {
		opt.SinceDays = 90
	}
	if opt.Limit <= 0 {
		opt.Limit = 20
	}
	if opt.Limit > 100 {
		opt.Limit = 100
	}
	if opt.Budget <= 0 {
		opt.Budget = defaultBudget
	}
	if opt.MaxTechs <= 0 {
		opt.MaxTechs = defaultMaxTechs
	}
	started := time.Now()

	assets, err := loadAssets(ctx, opt)
	if err != nil {
		return nil, err
	}

	report := &Report{Items: []Item{}}
	// host → 是否拿到过版本（同一宿主多技术名只算一次）
	hostWithVersion := map[string]bool{}
	// tech -> host -> 观测到的版本
	obs := map[string]map[string]versionHit{}
	techDisplay := map[string]string{}
	for _, a := range assets {
		for _, t := range a.Technologies {
			if !techWanted(t, opt.Techs) {
				continue
			}
			// 指纹常带版本后缀（"IIS:10.0"）。不归一化的话它会被当成一个独立技术名，
			// 去 NVD 搜 "IIS:10.0" 必然 0 结果，而真正的 "IIS" 那条又收不到这个资产（2026-11-25 实测）。
			key := normalizeTechKey(t)
			if obs[key] == nil {
				obs[key] = map[string]versionHit{}
				techDisplay[key] = key
			}
			obs[key][a.Host] = extractVersion(a, t)
		}
	}

	allNames := make([]string, 0, len(obs))
	for k := range obs {
		allNames = append(allNames, k)
	}
	// 全库指纹上百个，逐个打 NVD 既慢又超出报告价值：按宿主数从多到少取前 N 个，
	// 截断多少写进 stats.techs_skipped，不让调用方把「没查」读成「没有 CVE」。
	sort.Slice(allNames, func(i, j int) bool {
		if len(obs[allNames[i]]) != len(obs[allNames[j]]) {
			return len(obs[allNames[i]]) > len(obs[allNames[j]])
		}
		return allNames[i] < allNames[j]
	})
	report.Stats.TechsTotal = len(allNames)
	names := allNames
	if len(names) > opt.MaxTechs {
		names = names[:opt.MaxTechs]
	}
	names = append([]string(nil), names...)

	deadline := started.Add(opt.Budget)
	upstreamFails := 0

	for _, key := range names {
		// NVD 不通时每个技术名都要等满超时，必须给整次调用设墙钟上限：
		// 宁可返回「已查 N 个、剩下因超时跳过」，也不要让 MCP 客户端等到 REQUEST_TIMEOUT。
		if time.Now().After(deadline) {
			report.Stats.DeadlineHit = true
			break
		}
		hosts := obs[key]
		// 逐技术名累加会把「一台机器命中两个技术名」算成两台，
		// 下游拿这个数推断覆盖率就错了。这里只记「哪些宿主拿到过版本」，最后按宿主去重。
		for h, hit := range hosts {
			if hit.Version != "" {
				hostWithVersion[h] = true
			} else if _, ok := hostWithVersion[h]; !ok {
				hostWithVersion[h] = false
			}
		}

		searchName := strings.Fields(techDisplay[key])
		cveQuery := techDisplay[key]
		if len(searchName) > 0 {
			cveQuery = searchName[0]
		}
		remain := time.Until(deadline)
		if remain <= 0 {
			report.Stats.DeadlineHit = true
			break
		}
		if remain > maxSingleFetch {
			remain = maxSingleFetch
		}
		callCtx, cancel := context.WithTimeout(ctx, remain)
		records, _, err := FetchCVE(callCtx, cveQuery, opt.SinceDays, opt.Limit)
		cancel()
		report.Stats.TechsQueried++
		if err != nil {
			// 单个技术名取不到不该让整个工具失败，但必须留痕——静默 continue 会让人
			// 以为「没有 CVE」，实际是请求失败（2026-11-25 踩过：NVD 404 被当成错误）。
			logger.Error(fmt.Sprintf("intel: fetch cve for %q failed: %v", cveQuery, err))
			report.Stats.UpstreamFailures++
			upstreamFails++
			// 连续两个技术名都失败 = 上游不可达，继续查只是把预算一条条耗在超时上。
			if upstreamFails >= 2 {
				break
			}
			continue
		}
		upstreamFails = 0
		for _, rec := range records {
			if len(report.Items) >= opt.Limit {
				break
			}
			item := Item{
				CVE: rec.CVE, Title: rec.Title, URL: rec.URL, Severity: rec.Severity,
				Published: rec.Published, Source: rec.Source, Summary: rec.Summary,
				Affected: []Affected{}, Unmatched: []string{},
			}
			seenHost := map[string]bool{}
			for host, hit := range hosts {
				if seenHost[host] {
					continue
				}
				seenHost[host] = true
				aff, include := judge(rec, host, techDisplay[key], hit)
				if !include {
					continue
				}
				// unmatched_hosts 只收「有该技术但拿不到版本」的资产。
				if aff.Version == nil {
					item.Unmatched = append(item.Unmatched, host)
				}
				item.Affected = append(item.Affected, aff)
			}
			if len(item.Affected) == 0 {
				// 全部资产都能确定「不受影响」——这条 CVE 对本次范围没有情报价值，不入列表。
				continue
			}
			sort.Slice(item.Affected, func(i, j int) bool { return item.Affected[i].Host < item.Affected[j].Host })
			sort.Strings(item.Unmatched)
			report.Items = append(report.Items, item)
		}
	}
	// 统计口径：按「宿主」而不是「技术名 × 宿主」。
	// 一台机器同时命中 Nginx 和 Tengine 时，逐技术名累加会把它算成两台，
	// 下游拿这个数去推断覆盖率就错了。
	report.Stats.TechsSkipped = report.Stats.TechsTotal - report.Stats.TechsQueried
	report.Stats.ElapsedMS = int(time.Since(started).Milliseconds())
	report.Stats.AssetsScanned = len(hostWithVersion)
	for _, ok := range hostWithVersion {
		if ok {
			report.Stats.WithVersion++
		}
	}
	report.Stats.WithoutVersion = report.Stats.AssetsScanned - report.Stats.WithVersion

	return report, nil
}

type versionHit struct {
	Version string
	Source  string
}

// judge 返回判定结果与「是否收录」。
// 版本明确落在区间之外 = 阴性对照，不是「无法判定」——
// 把它放进 affected 会让调用方误以为有 12 台机器可疑（2026-11-25 实测：
// IIS 10.0 对不上 WhatsUp 的 CVE，却整批出现在 affected 里）。
func judge(rec CVERecord, host, tech string, hit versionHit) (Affected, bool) {
	a := Affected{Host: host, Tech: tech, Applicability: "unknown"}
	if hit.Version == "" {
		a.Reason = "该资产暴露了 " + tech + " 指纹，但响应头/正文/banner 均未给出版本号，无法与受影响范围比对"
		return a, true
	}
	v := hit.Version
	src := hit.Source
	a.Version = &v
	a.VersionSource = &src
	matched := matchRange(rec, tech, v)
	switch matched {
	case rangeHit:
		a.Applicability = "confirmed"
		a.Reason = "实测版本 " + v + "（来源 " + src + "）落在该 CVE 声明的受影响区间内"
	case rangeMiss:
		a.Applicability = "unknown"
		a.Reason = "实测版本 " + v + "（来源 " + src + "）不在该 CVE 声明的受影响区间内；不构成命中"
		return a, false
	default:
		a.Applicability = "likely"
		a.Reason = "实测版本 " + v + "（来源 " + src + "），该 CVE 只给了产品级匹配、未声明可比对的版本区间"
	}
	return a, true
}

type rangeResult int

const (
	rangeUnknown rangeResult = iota
	rangeHit
	rangeMiss
)

func matchRange(rec CVERecord, tech, version string) rangeResult {
	if len(rec.Ranges) == 0 {
		for _, p := range rec.Products {
			if productMatches(p, tech) {
				return rangeUnknown
			}
		}
		return rangeUnknown
	}
	for _, r := range rec.Ranges {
		if !productMatches(r.Product, tech) {
			continue
		}
		if inRange(version, r) {
			return rangeHit
		}
	}
	return rangeMiss
}

func productMatches(product, tech string) bool {
	p := strings.ToLower(strings.TrimSpace(product))
	t := strings.ToLower(strings.TrimSpace(tech))
	if p == "" || t == "" {
		return false
	}
	fields := strings.Fields(t)
	if len(fields) > 0 {
		t = fields[0]
	}
	return strings.Contains(p, t) || strings.Contains(t, p)
}

func inRange(version string, r VersionRange) bool {
	if r.VersionStartIncl != "" && cmpVersion(version, r.VersionStartIncl) < 0 {
		return false
	}
	if r.VersionStartExcl != "" && cmpVersion(version, r.VersionStartExcl) <= 0 {
		return false
	}
	if r.VersionEndIncl != "" && cmpVersion(version, r.VersionEndIncl) > 0 {
		return false
	}
	if r.VersionEndExcl != "" && cmpVersion(version, r.VersionEndExcl) >= 0 {
		return false
	}
	return true
}

// cmpVersion 逐段比较，非数字段按字符串比。
func cmpVersion(a, b string) int {
	pa := splitVer(a)
	pb := splitVer(b)
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		var x, y string
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		xi, xerr := parseInt(x)
		yi, yerr := parseInt(y)
		if xerr == nil && yerr == nil {
			if xi != yi {
				if xi < yi {
					return -1
				}
				return 1
			}
			continue
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// splitVer 按 . - _ + ~ 切段。
// 早先用正则 `[.-_+~]`，其中 `.-_` 被当成**字符区间**（0x2E..0x5F，把数字也圈进去了），
// 结果每个数字都被切开、所有版本号互比都相等——区间判断全线失效（单测当场抓到）。
// 改用 FieldsFunc，不经过正则转义。
func splitVer(s string) []string {
	return strings.FieldsFunc(strings.TrimSpace(s), func(r rune) bool {
		switch r {
		case '.', '-', '_', '+', '~':
			return true
		}
		return false
	})
}

func parseInt(s string) (int, error) {
	n := 0
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	for _, c := range s {
		if c < 0x30 || c > 0x39 {
			return 0, fmt.Errorf("not a number")
		}
		n = n*10 + int(c-0x30)
	}
	return n, nil
}

func techWanted(name string, want []string) bool {
	if len(want) == 0 {
		return true
	}
	n := strings.ToLower(name)
	for _, w := range want {
		wl := strings.ToLower(strings.TrimSpace(w))
		if wl == "" {
			continue
		}
		if strings.Contains(n, wl) || strings.Contains(wl, n) {
			return true
		}
	}
	return false
}

// normalizeTechKey 把 "IIS:10.0" / "nginx/1.2" 归到产品名 "iis" / "nginx"。
func normalizeTechKey(t string) string {
	s := strings.ToLower(strings.TrimSpace(t))
	if i := strings.IndexAny(s, ":/("); i > 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// versionFromTech 从指纹名里取版本（"IIS:10.0" → 10.0）。
// 这是 ScopeSentry 指纹库里最常见的带版本形式，比从响应头抠更可靠。
func versionFromTech(t string) versionHit {
	s := strings.TrimSpace(t)
	i := strings.IndexAny(s, ":/(")
	if i <= 0 || i+1 >= len(s) {
		return versionHit{}
	}
	rest := strings.TrimLeft(s[i+1:], "/ ")
	if rest == "" {
		return versionHit{}
	}
	re := regexp.MustCompile(`^v?([0-9][0-9A-Za-z.\-_]*)`)
	if m := re.FindStringSubmatch(rest); len(m) > 1 {
		return versionHit{Version: cleanVer(m[1]), Source: "fingerprint"}
	}
	return versionHit{}
}

// extractVersion 只做「读到的算数」：header / banner / body / 指纹 哪个给了就是哪个，都没有就是空。
func extractVersion(a assetDoc, tech string) versionHit {
	// 先看主动探测回填的版本（versionprobe，TTL 7 天）——它比存量响应头可靠得多。
	if len(a.TechVersions) > 0 {
		if v, ok := a.TechVersions[normalizeTechKey(tech)]; ok && v != "" {
			return versionHit{Version: v, Source: "probe"}
		}
	}
	if v := versionFromTech(tech); v.Version != "" {
		return v
	}
	base := strings.Fields(tech)
	name := tech
	if len(base) > 0 {
		name = base[0]
	}
	re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(name) + `\s*[/ ]\s*v?([0-9][0-9A-Za-z.\-_]{0,15})`)
	if m := re.FindStringSubmatch(a.RawHeaders); len(m) > 1 {
		return versionHit{Version: cleanVer(m[1]), Source: "header"}
	}
	if m := re.FindStringSubmatch(a.WebServer); len(m) > 1 {
		return versionHit{Version: cleanVer(m[1]), Source: "banner"}
	}
	body := a.Body
	if len(body) > 4000 {
		body = body[:4000]
	}
	if m := re.FindStringSubmatch(body); len(m) > 1 {
		return versionHit{Version: cleanVer(m[1]), Source: "body"}
	}
	return versionHit{}
}

func cleanVer(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimSuffix(v, ".")
	return v
}

func loadAssets(ctx context.Context, opt Options) ([]assetDoc, error) {
	filter := bson.M{}
	or := []bson.M{}
	if len(opt.Hosts) > 0 {
		or = append(or, bson.M{"host": bson.M{"$in": opt.Hosts}})
		or = append(or, bson.M{"domain": bson.M{"$in": opt.Hosts}})
	}
	if opt.Project != "" {
		pid := opt.Project
		var proj struct {
			ID   any    `bson:"_id"`
			Name string `bson:"name"`
		}
		if err := mongodb.DB.Collection("project").FindOne(ctx, bson.M{"name": opt.Project}).Decode(&proj); err == nil {
			if s, ok := proj.ID.(interface{ Hex() string }); ok {
				pid = s.Hex()
			}
		}
		or = append(or, bson.M{"project": pid})
		or = append(or, bson.M{"project": opt.Project})
	}
	if len(or) > 0 {
		filter["$or"] = or
	}
	if len(opt.Techs) > 0 {
		techOr := []bson.M{}
		for _, t := range opt.Techs {
			techOr = append(techOr, bson.M{"technologies": bson.M{"$regex": regexp.QuoteMeta(t), "$options": "i"}})
		}
		filter["$and"] = []bson.M{{"$or": techOr}}
	}
	// 同 versionprobe：`$ne: []` 会把 null 也算作「有技术」，这里只认真有元素的。
	filter["technologies.0"] = bson.M{"$exists": true}

	cur, err := mongodb.DB.Collection("asset").Find(ctx, filter,
		options.Find().SetLimit(5000).SetProjection(bson.M{
			"host": 1, "technologies": 1, "rawheaders": 1, "body": 1,
			"webServer": 1, "title": 1, "url": 1, "port": 1, "techVersions": 1,
		}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []assetDoc
	for cur.Next(ctx) {
		var d assetDoc
		if err := cur.Decode(&d); err != nil {
			continue
		}
		if d.Host == "" {
			continue
		}
		out = append(out, d)
	}
	return out, nil
}
