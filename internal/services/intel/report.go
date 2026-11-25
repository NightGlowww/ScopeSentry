package intel

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Options 对应 MCP 入参。
type Options struct {
	Project   string
	Hosts     []string
	Techs     []string
	SinceDays int
	Limit     int
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

	assets, err := loadAssets(ctx, opt)
	if err != nil {
		return nil, err
	}

	report := &Report{Items: []Item{}}
	// tech -> host -> 观测到的版本
	obs := map[string]map[string]versionHit{}
	techDisplay := map[string]string{}
	for _, a := range assets {
		for _, t := range a.Technologies {
			if !techWanted(t, opt.Techs) {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(t))
			if obs[key] == nil {
				obs[key] = map[string]versionHit{}
				techDisplay[key] = t
			}
			obs[key][a.Host] = extractVersion(a, t)
		}
	}

	names := make([]string, 0, len(obs))
	for k := range obs {
		names = append(names, k)
	}
	sort.Strings(names)

	for _, key := range names {
		hosts := obs[key]
		withV, withoutV := 0, 0
		for _, h := range hosts {
			if h.Version != "" {
				withV++
			} else {
				withoutV++
			}
		}
		report.Stats.AssetsScanned += len(hosts)
		report.Stats.WithVersion += withV
		report.Stats.WithoutVersion += withoutV

		searchName := strings.Fields(techDisplay[key])
		cveQuery := techDisplay[key]
		if len(searchName) > 0 {
			cveQuery = searchName[0]
		}
		records, _, err := FetchCVE(ctx, cveQuery, opt.SinceDays, opt.Limit)
		if err != nil {
			// 单个技术名取不到不该让整个工具失败，但要如实记进日志
			continue
		}
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
				aff := judge(rec, host, techDisplay[key], hit)
				if aff.Applicability == "unknown" {
					item.Unmatched = append(item.Unmatched, host)
				}
				item.Affected = append(item.Affected, aff)
			}
			sort.Slice(item.Affected, func(i, j int) bool { return item.Affected[i].Host < item.Affected[j].Host })
			sort.Strings(item.Unmatched)
			report.Items = append(report.Items, item)
		}
	}
	return report, nil
}

type versionHit struct {
	Version string
	Source  string
}

func judge(rec CVERecord, host, tech string, hit versionHit) Affected {
	a := Affected{Host: host, Tech: tech, Applicability: "unknown"}
	if hit.Version == "" {
		a.Reason = "该资产暴露了 " + tech + " 指纹，但响应头/正文/banner 均未给出版本号，无法与受影响范围比对"
		return a
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
	default:
		a.Applicability = "likely"
		a.Reason = "实测版本 " + v + "（来源 " + src + "），该 CVE 只给了产品级匹配、未声明可比对的版本区间"
	}
	return a
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

func splitVer(s string) []string {
	return regexp.MustCompile(`[.-_+~]`).Split(strings.TrimSpace(s), -1)
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

// extractVersion 只做「读到的算数」：header / banner / body 哪个给了就是哪个，都没有就是空。
func extractVersion(a assetDoc, tech string) versionHit {
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
	filter["technologies"] = bson.M{"$exists": true, "$ne": []string{}}

	cur, err := mongodb.DB.Collection("asset").Find(ctx, filter,
		options.Find().SetLimit(5000).SetProjection(bson.M{
			"host": 1, "technologies": 1, "rawheaders": 1, "body": 1,
			"webServer": 1, "title": 1, "url": 1, "port": 1,
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
