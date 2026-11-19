// helper-------------------------------------
// @file      : task.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2025/6/2 21:08
// -------------------------------------------

package helper

import (
	"encoding/binary"
	"fmt"
	"net"
	"regexp"
	"strings"
)

// GenerateIPRange 生成 IP 范围
// MaxIPRangeSize 单次 IP 段展开上限（对应一个 /16）。
// 没有上限时 "1.0.0.0-255.255.255.255" 会直接吃掉数 GB 内存。
const MaxIPRangeSize = 1 << 16

func GenerateIPRange(startIP, endIP string) ([]string, error) {
	start := net.ParseIP(startIP).To4()
	end := net.ParseIP(endIP).To4()
	if start == nil || end == nil {
		return nil, fmt.Errorf("invalid IP range")
	}
	s := binary.BigEndian.Uint32(start)
	e := binary.BigEndian.Uint32(end)
	// 起始大于结束时必须报错：旧实现用 !ip.Equal(end) 循环，IPv4 回绕后
	// 要跑约 40 亿次才能命中，实际表现是任务创建卡死 + 内存爆掉。
	if e < s {
		return nil, fmt.Errorf("invalid IP range: end %s is before start %s", endIP, startIP)
	}
	if uint64(e)-uint64(s)+1 > MaxIPRangeSize {
		return nil, fmt.Errorf("IP range too large: %d addresses (limit %d)", uint64(e)-uint64(s)+1, MaxIPRangeSize)
	}
	out := make([]string, 0, e-s+1)
	buf := make(net.IP, 4)
	for v := s; ; v++ {
		binary.BigEndian.PutUint32(buf, v)
		out = append(out, buf.String())
		if v == e {
			break
		}
	}
	return out, nil
}

// NextIP 生成下一个 IP（仅支持 IPv4）
func NextIP(ip net.IP) net.IP {
	ip = ip.To4()
	result := make(net.IP, len(ip))
	copy(result, ip)
	for i := len(result) - 1; i >= 0; i-- {
		result[i]++
		if result[i] != 0 {
			break
		}
	}
	return result
}

// GenerateTarget 解析 target，可以是单个 IP、IP 段、CIDR 或 URL
func GenerateTarget(target string) []string {
	target = strings.TrimSpace(target)
	if strings.Contains(target, "://") {
		return []string{target}
	}
	if strings.Contains(target, "-") {
		parts := strings.SplitN(target, "-", 2)
		if len(parts) != 2 {
			return []string{target}
		}
		ipRange, err := GenerateIPRange(parts[0], parts[1])
		if err != nil {
			return []string{target}
		}
		return ipRange
	} else if strings.Contains(target, "/") {
		_, network, err := net.ParseCIDR(target)
		if err != nil {
			return []string{target}
		}
		var result []string
		for ip := network.IP.Mask(network.Mask); network.Contains(ip); ip = NextIP(ip) {
			result = append(result, ip.String())
		}
		// 移除网络地址与广播地址；但 /31（RFC 3021，两端都可用）与 /32（单主机）
		// 不能剥，否则 /31 会被剥空 —— 任务看似下发，实际一个目标都没有。
		if len(result) > 2 {
			result = result[1 : len(result)-1]
		}
		return result
	}
	return []string{target}
}

// GenerateIgnore 解析忽略列表，返回精确忽略 IP 和正则忽略列表
func GenerateIgnore(ignore string) ([]string, []string) {
	if ignore == "" {
		return []string{}, []string{}
	}
	var ignoreList []string
	var regexList []string

	lines := strings.Split(ignore, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(line, "http://", ""), "https://", ""))
		if line == "" {
			continue
		}
		if !strings.Contains(line, "*") {
			targets := GenerateTarget(line)
			ignoreList = append(ignoreList, targets...)
		} else {
			escaped := regexp.QuoteMeta(line)
			regexList = append(regexList, strings.ReplaceAll(escaped, `\*`, ".*"))
		}
	}
	return ignoreList, regexList
}

// GetTargetList 根据原始 target 列表和忽略列表，生成去重且保序的目标列表
func GetTargetList(rawTarget, ignore string) ([]string, error) {
	ignoreList, regexList := GenerateIgnore(ignore)
	ignoreSet := make(map[string]struct{})
	for _, ip := range ignoreList {
		ignoreSet[ip] = struct{}{}
	}

	seen := make(map[string]struct{})
	var finalList []string

	targetLines := strings.Split(rawTarget, "\n")
	for _, line := range targetLines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		targets := GenerateTarget(line)
		for _, t := range targets {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			if _, found := ignoreSet[t]; found {
				continue
			}
			shouldIgnore := false
			for _, pattern := range regexList {
				matched, _ := regexp.MatchString(pattern, t)
				if matched {
					shouldIgnore = true
					break
				}
			}
			if !shouldIgnore {
				if _, exists := seen[t]; !exists {
					seen[t] = struct{}{}
					finalList = append(finalList, t)
				}
			}
		}
	}
	return finalList, nil
}
