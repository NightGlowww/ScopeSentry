package intel

import "strings"

// ---------- NVD 原始 JSON 的归一化 ----------

type nvdResponse struct {
	Vulnerabilities []struct {
		CVE struct {
			ID           string `json:"id"`
			Published    string `json:"published"`
			Descriptions []struct {
				Lang  string `json:"lang"`
				Value string `json:"value"`
			} `json:"descriptions"`
			Metrics struct {
				V31 []nvdMetric `json:"cvssMetricV31"`
				V30 []nvdMetric `json:"cvssMetricV30"`
				V2  []nvdMetric `json:"cvssMetricV2"`
			} `json:"metrics"`
			Configurations []struct {
				Nodes []struct {
					CpeMatch []nvdCpeMatch `json:"cpeMatch"`
				} `json:"nodes"`
			} `json:"configurations"`
		} `json:"cve"`
	} `json:"vulnerabilities"`
	TotalResults int `json:"totalResults"`
}

type nvdMetric struct {
	CVSSData struct {
		BaseSeverity string  `json:"baseSeverity"`
		BaseScore    float64 `json:"baseScore"`
	} `json:"cvssData"`
	BaseSeverity string `json:"baseSeverity"`
}

type nvdCpeMatch struct {
	Criteria              string `json:"criteria"`
	VersionStartIncluding string `json:"versionStartIncluding"`
	VersionStartExcluding string `json:"versionStartExcluding"`
	VersionEndIncluding   string `json:"versionEndIncluding"`
	VersionEndExcluding   string `json:"versionEndExcluding"`
}

func normalizeNVD(p nvdResponse) []cacheItem {
	out := make([]cacheItem, 0, len(p.Vulnerabilities))
	for _, v := range p.Vulnerabilities {
		c := v.CVE
		if c.ID == "" {
			continue
		}
		it := cacheItem{CVE: c.ID, Published: c.Published[:minLen(len(c.Published), 10)], Severity: "UNKNOWN"}
		for _, d := range c.Descriptions {
			if d.Lang == "en" {
				it.Summary = truncate(d.Value, 240)
				break
			}
		}
		if it.Summary == "" && len(c.Descriptions) > 0 {
			it.Summary = truncate(c.Descriptions[0].Value, 240)
		}
		it.Title = truncate(it.Summary, 90)
		for _, m := range c.Metrics.V31 {
			if m.CVSSData.BaseSeverity != "" {
				it.Severity = strings.ToUpper(m.CVSSData.BaseSeverity)
				break
			}
		}
		if it.Severity == "UNKNOWN" {
			for _, m := range c.Metrics.V30 {
				if m.CVSSData.BaseSeverity != "" {
					it.Severity = strings.ToUpper(m.CVSSData.BaseSeverity)
					break
				}
			}
		}
		if it.Severity == "UNKNOWN" {
			for _, m := range c.Metrics.V2 {
				if m.BaseSeverity != "" {
					it.Severity = strings.ToUpper(m.BaseSeverity)
					break
				}
			}
		}
		seen := map[string]bool{}
		for _, cfg := range c.Configurations {
			for _, n := range cfg.Nodes {
				for _, cm := range n.CpeMatch {
					prod := cpeProduct(cm.Criteria)
					if prod == "" || seen[prod] {
						continue
					}
					seen[prod] = true
					it.Products = append(it.Products, prod)
					if cm.VersionStartIncluding != "" || cm.VersionStartExcluding != "" ||
						cm.VersionEndIncluding != "" || cm.VersionEndExcluding != "" {
						it.Ranges = append(it.Ranges, VersionRange{
							Product:          prod,
							VersionStartIncl: cm.VersionStartIncluding,
							VersionStartExcl: cm.VersionStartExcluding,
							VersionEndIncl:   cm.VersionEndIncluding,
							VersionEndExcl:   cm.VersionEndExcluding,
						})
					}
				}
			}
		}
		out = append(out, it)
	}
	return out
}

// cpeProduct 从 cpe:2.3:a:vendor:product:version:... 里取 product。
func cpeProduct(criteria string) string {
	parts := strings.Split(criteria, ":")
	if len(parts) < 5 {
		return ""
	}
	return strings.ToLower(parts[4])
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func minLen(a, b int) int {
	if a < b {
		return a
	}
	return b
}
