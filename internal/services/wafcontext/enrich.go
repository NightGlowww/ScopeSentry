// Package wafcontext 把 WAF 画像回填到漏洞记录上。
//
// 闭环的最后一环：扫描端 wafscan 模块测出「这台主机挂了什么 WAF、哪些载体不被检查」，
// 写进 waf_profile；这里在服务端把该画像贴到同一主机上的漏洞记录上，
// 让报告能写清「这个洞是在 WAF 存在的前提下、通过 X 载体打到的」，而不是只写「发现漏洞」。
//
// 放在服务端而不是扫描端：一个洞可能由多个节点、多次任务产出，画像却是按 host 的全局事实。
// 服务端只算一次，而且历史漏洞也能补上。
package wafcontext

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"go.mongodb.org/mongo-driver/bson"
)

// Profile waf_profile 集合里的一条记录（只取这里要用的字段）。
type Profile struct {
	Host     string   `bson:"host"`
	Vendor   string   `bson:"vendor"`
	Bypasses []string `bson:"bypasses"`
}

func disabled() bool {
	v := strings.ToLower(os.Getenv("WAF_CONTEXT"))
	return v == "off" || v == "0" || v == "false"
}

// hostOf 从 URL 取 host，与扫描端 wafscan 的 hostOf 保持一致。
func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	s := raw
	for _, p := range []string{"https://", "http://"} {
		if strings.HasPrefix(s, p) {
			s = s[len(p):]
		}
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	return s
}

// RunOnce 给还没有 waf 字段的漏洞补上所属主机的画像。返回处理条数。
func RunOnce(ctx context.Context) (int, error) {
	if disabled() || mongodb.Client == nil {
		return 0, nil
	}

	type vulnRow struct {
		Hash string `bson:"hash"`
		URL  string `bson:"url"`
	}
	var rows []vulnRow
	if err := mongodb.FindMany("vulnerability", bson.M{"waf": bson.M{"$exists": false}}, &rows); err != nil {
		return 0, fmt.Errorf("find vulnerabilities: %w", err)
	}
	if len(rows) == 0 {
		return 0, nil
	}
	if len(rows) > 5000 {
		rows = rows[:5000]
	}

	hostSet := map[string]struct{}{}
	for _, r := range rows {
		if h := hostOf(r.URL); h != "" {
			hostSet[h] = struct{}{}
		}
	}
	if len(hostSet) == 0 {
		return 0, nil
	}
	hosts := make([]string, 0, len(hostSet))
	for h := range hostSet {
		hosts = append(hosts, h)
	}

	var profs []Profile
	if err := mongodb.FindMany("waf_profile", bson.M{"host": bson.M{"$in": hosts}}, &profs); err != nil {
		return 0, fmt.Errorf("find waf_profile: %w", err)
	}
	byHost := map[string]Profile{}
	for _, p := range profs {
		if p.Host != "" {
			byHost[p.Host] = p
		}
	}

	n := 0
	for _, r := range rows {
		h := hostOf(r.URL)
		p, ok := byHost[h]
		if !ok {
			// 没有画像不是错误：该主机可能没跑过 wafscan（非 HTTP 资产、或模块关闭）。
			continue
		}
		_, err := mongodb.UpdateOne("vulnerability", bson.M{"hash": r.Hash}, bson.M{"$set": bson.M{
			"waf": bson.M{
				"vendor":     p.Vendor,
				"bypasses":   p.Bypasses,
				"bypassable": len(p.Bypasses) > 0,
			},
		}})
		if err == nil {
			n++
		}
	}
	return n, nil
}

// Start 每 10 分钟补一次。
func Start(ctx context.Context) {
	if disabled() {
		logger.Info("wafcontext: disabled by WAF_CONTEXT")
		return
	}
	go func() {
		if n, err := RunOnce(ctx); err == nil && n > 0 {
			logger.Info(fmt.Sprintf("wafcontext: stamped %d vulnerabilities with host WAF profile", n))
		}
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				n, err := RunOnce(ctx)
				if err != nil {
					logger.Error("wafcontext: " + err.Error())
					continue
				}
				if n > 0 {
					logger.Info(fmt.Sprintf("wafcontext: stamped %d vulnerabilities with host WAF profile", n))
				}
			}
		}
	}()
}
