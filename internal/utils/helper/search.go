// Package helper -----------------------------
// @file      : search.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2025/5/2 14:34
// -------------------------------------------
package helper

import (
	"errors"
	"fmt"
	"github.com/Autumn-27/ScopeSentry/internal/logger"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/models"
)

// indexHasTimeField 哪些索引有 "time"（发现时间）字段——时间范围筛选只对它们生效。
// 只列**实测确实有 time 字段**的集合（2026-11-22 逐集合 countDocuments 核过）：
// DirScanResult / PageMonitoring / app / mp 的 time 全是 0 条，放进来会让时间筛选
// 恒返回空——那种"静默空结果"比不做筛选更糟。
var indexHasTimeField = map[string]bool{
	"subdomain":       true,
	"asset":           true,
	"UrlScan":         true,
	"vulnerability":   true,
	"SensitiveResult": true,
	"crawler":         true,
	"IPAsset":         true,
	"RootDomain":      true,
}

// SearchKey 定义了不同类型数据的字段映射关系
var SearchKey = map[string]map[string]string{
	"SensitiveResult": {
		"url":   "url",
		"sname": "sid",
		"body":  "body",
		"info":  "match",
		"md5":   "md5",
	},
	"DirScanResult": {
		"statuscode": "status",
		"url":        "url",
		"redirect":   "msg",
		"length":     "length",
	},
	"vulnerability": {
		"url":        "url",
		"vulname":    "vulname",
		"matched":    "matched",
		"request":    "request",
		"response":   "response",
		"level":      "level",
		"rootdomain": "rootdomain",
		"rootDomain": "rootDomain",
	},
	"subdomain": {
		"domain": "host",
		"ip":     "ip",
		"type":   "type",
		"value":  "value",
	},
	"asset": {
		"app":        "technologies",
		"body":       "body",
		"header":     "rawheaders",
		"title":      "title",
		"statuscode": "statuscode",
		"icon":       "faviconmmh3",
		"ip":         "ip",
		"domain":     "host",
		"port":       "port",
		"service":    "service",
		"banner":     "metadata",
		"type":       "type",
	},
	"SubdomainTakerResult": {
		"domain":   "input",
		"value":    "value",
		"type":     "cname",
		"response": "response",
	},
	"UrlScan": {
		"url":      "output",
		"input":    "input",
		"source":   "source",
		"resultId": "resultId",
		"type":     "outputtype",
	},
	"PageMonitoring": {
		"url":      "url",
		"hash":     "hash",
		"diff":     "diff",
		"response": "response",
	},
	"crawler": {
		"url":      "url",
		"method":   "method",
		"body":     "body",
		"resultId": "resultId",
	},
	"RootDomain": {
		"domain":  "domain",
		"icp":     "icp",
		"company": "company",
	},
	"app": {
		"name":        "name",
		"icp":         "icp",
		"company":     "company",
		"category":    "category",
		"description": "description",
		"url":         "url",
		"apk":         "apk",
	},
	"mp": {
		"name":        "name",
		"icp":         "icp",
		"company":     "company",
		"category":    "category",
		"description": "description",
		"url":         "url",
	},
	"IPAsset": {
		"ip":        "ip",
		"domain":    "ports.server.domain",
		"port":      "ports.port",
		"service":   "ports.server.service",
		"webServer": "ports.server.webServer",
		"app":       "ports.server.technologies",
	},
}

// 缓存常用映射，避免重复创建
var (
	filterKeyCache = map[string]string{
		"app":        "technologies",
		"color":      "color",
		"status":     "status",
		"level":      "level",
		"type":       "type",
		"project":    "project",
		"port":       "port",
		"service":    "service",
		"icon":       "faviconmmh3",
		"statuscode": "statuscode",
		"sname":      "sid",
		"task":       "taskName",
		"tags":       "tags",
	}

	fuzzyQueryKeyCache = map[string]string{
		"sub_host":        "host",
		"sub_value":       "value",
		"sub_ip":          "ip",
		"port_port":       "port",
		"port_domain":     "domain,host",
		"port_ip":         "ip,host",
		"port_protocol":   "service",
		"service_service": "type,webServer,protocol",
		"service_domain":  "domain,host",
		"service_port":    "port",
		"service_ip":      "ip,host",
	}

	// 预编译正则表达式
	regexCache = struct {
		sync.RWMutex
		m map[string]*regexp.Regexp
	}{
		m: make(map[string]*regexp.Regexp),
	}
)

// getRegex 获取或创建正则表达式，使用缓存
func getRegex(pattern string) (*regexp.Regexp, error) {
	regexCache.RLock()
	if re, ok := regexCache.m[pattern]; ok {
		regexCache.RUnlock()
		return re, nil
	}
	regexCache.RUnlock()

	regexCache.Lock()
	defer regexCache.Unlock()

	// 双重检查
	if re, ok := regexCache.m[pattern]; ok {
		return re, nil
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	regexCache.m[pattern] = re
	return re, nil
}

// StringToPostfix 将中缀表达式转换为后缀表达式
func StringToPostfix(expression string) ([]string, error) {
	if expression == "" {
		return nil, nil
	}

	// 预分配足够的容量，减少扩容
	operandsStack := make([]string, 0, 16)
	expressionStack := make([]string, 0, 32)
	startChar := 0
	skipFlag := false
	expFlag := false

	// 使用rune来处理Unicode字符
	runes := []rune(expression)
	for i := 0; i < len(runes); i++ {
		char := runes[i]
		if skipFlag {
			skipFlag = false
			continue
		}

		if i < len(runes)-1 {
			if char == '|' && runes[i+1] == '|' {
				skipFlag = true
				operandsStack = append(operandsStack, "||")
				key := string(runes[startChar:i])
				if key != "" {
					expressionStack = append(expressionStack, key)
				}
				startChar = i + 2
				i++
			} else if char == '&' && runes[i+1] == '&' {
				skipFlag = true
				operandsStack = append(operandsStack, "&&")
				key := string(runes[startChar:i])
				if key != "" {
					expressionStack = append(expressionStack, key)
				}
				startChar = i + 2
				i++
			}
		}

		if char == '(' && (i == 0 || runes[i-1] != '\\') && !expFlag {
			startChar = i + 1
			operandsStack = append(operandsStack, "(")
		} else if char == ')' && (i == 0 || runes[i-1] != '\\') && !expFlag {
			key := string(runes[startChar:i])
			if key != "" {
				expressionStack = append(expressionStack, key)
			}
			startChar = i + 1

			for len(operandsStack) > 0 {
				popped := operandsStack[len(operandsStack)-1]
				operandsStack = operandsStack[:len(operandsStack)-1]
				if popped == "(" {
					break
				}
				if popped != "" {
					expressionStack = append(expressionStack, popped)
				}
			}
		} else if char == '"' && (i == 0 || runes[i-1] != '\\') {
			if !expFlag {
				expFlag = true
			} else {
				if i == len(runes)-1 {
					expFlag = false
					continue
				}
				tmp := strings.ReplaceAll(string(runes[i:]), " ", "")
				if strings.HasPrefix(tmp, "\"||") ||
					(strings.HasPrefix(tmp, "\")") && len(tmp) == 3) ||
					strings.HasPrefix(tmp, "\"&&") ||
					strings.HasPrefix(tmp, "\")||") ||
					strings.HasPrefix(tmp, "\")&&") ||
					(strings.HasPrefix(tmp, "\")") && len(tmp) == 2) {
					expFlag = false
				}
			}
		}
	}

	if startChar < len(runes) {
		key := string(runes[startChar:])
		if key != "" {
			expressionStack = append(expressionStack, key)
		}
	}

	for len(operandsStack) > 0 {
		popped := operandsStack[len(operandsStack)-1]
		operandsStack = operandsStack[:len(operandsStack)-1]
		if popped != "" {
			expressionStack = append(expressionStack, popped)
		}
	}

	// 预分配结果切片
	result := make([]string, 0, len(expressionStack))
	for _, key := range expressionStack {
		if key != "" && key != " " {
			cleaned := strings.TrimSpace(key)
			cleaned = strings.ReplaceAll(cleaned, `\(`, "(")
			cleaned = strings.ReplaceAll(cleaned, `\)`, ")")
			cleaned = strings.ReplaceAll(cleaned, `\|\|`, "||")
			cleaned = strings.ReplaceAll(cleaned, `\&\&`, "&&")
			result = append(result, cleaned)
		}
	}

	return result, nil
}

// cmpToMongo 把 DSL 里的比较符映射成 Mongo 操作符。
var cmpToMongo = map[string]string{
	">=": "$gte",
	"<=": "$lte",
	">":  "$gt",
	"<":  "$lt",
}

// stringNumericFields：库里以字符串保存、但语义是数字的字段。
// 这些字段做区间比较必须先用 $convert 转数字，否则类型不匹配永远命中 0 条。
var stringNumericFields = map[string]bool{
	"port": true,
}

// splitComparison 识别 "key>=100" 这类比较表达式。
// 只在「没有 == / != 」时调用，避免把 "a==b" 误判成 "a>"。
func splitComparison(expr string) (op, key, value string, ok bool) {
	for _, candidate := range []string{">=", "<=", ">", "<"} {
		if idx := strings.Index(expr, candidate); idx > 0 {
			k := strings.TrimSpace(expr[:idx])
			// key 里不能再出现 = ! < >：title=<script> 里的 "<" 是值的一部分，不是比较符。
			if strings.ContainsAny(k, "=!<>") {
				return "", "", "", false
			}
			v := strings.TrimSpace(expr[idx+len(candidate):])
			v = strings.Trim(v, "\"")
			if k == "" || v == "" {
				return "", "", "", false
			}
			return candidate, k, v, true
		}
	}
	return "", "", "", false
}

// SearchToMongoDB 将搜索表达式转换为MongoDB查询
func SearchToMongoDB(expressionRaw string, keyword map[string]string) ([]map[string]interface{}, error) {
	if expressionRaw == "" {
		return []map[string]interface{}{{}}, nil
	}

	keyword["task"] = "taskName"
	keyword["rootDomain"] = "rootDomain"
	keyword["tag"] = "tags"

	expression, err := StringToPostfix(expressionRaw)
	if err != nil {
		return nil, err
	}

	// 预分配栈空间
	stack := make([]map[string]interface{}, 0, len(expression))

	for _, expr := range expression {
		if expr == "&&" {
			if len(stack) < 2 {
				return nil, errors.New("invalid expression: insufficient operands for &&")
			}
			right := stack[len(stack)-1]
			left := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			stack = append(stack, map[string]interface{}{
				"$and": []map[string]interface{}{left, right},
			})
		} else if expr == "||" {
			if len(stack) < 2 {
				return nil, errors.New("invalid expression: insufficient operands for ||")
			}
			right := stack[len(stack)-1]
			left := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			stack = append(stack, map[string]interface{}{
				"$or": []map[string]interface{}{left, right},
			})
		} else if cmpOp, cmpKey, cmpVal, isCmp := splitComparison(expr); isCmp {
			// 数值区间比较：ARL-Next 1.5.2 修的就是这块（高级数值比较）。
			// 之前 >= / <= / > / < 没有任何分支：port>=1000 落入 "=" 分支后 key 变成 "port>"，
			// 关键字表查不到，条件被静默丢掉 —— 结果返回全量 45363 条，看起来像「筛了但没用」。
			if mongoKey, ok := keyword[cmpKey]; ok {
				// 时间字段走字符串比较：库里存的就是 "2006-01-02 15:04:05" 这种可排序格式，
				// 这样「发现时间 >= X」在前端可以写成搜索条件，不用给每个列表接口加参数
				// （ARL-Next bc22a10 的「12 维发现时间筛选」正是这个口径）。
				if isTimeField(mongoKey) {
					if !looksLikeTime(cmpVal) {
						return nil, fmt.Errorf("%s %s %q：时间比较值格式应为 YYYY-MM-DD HH:MM:SS", cmpKey, cmpOp, cmpVal)
					}
					stack = append(stack, map[string]interface{}{
						mongoKey: map[string]interface{}{cmpToMongo[cmpOp]: cmpVal},
					})
					continue
				}
				num, numErr := strconv.Atoi(cmpVal)
				if numErr != nil {
					return nil, fmt.Errorf("%s %s %q：比较值必须是数字", cmpKey, cmpOp, cmpVal)
				}
				mongoOp := cmpToMongo[cmpOp]
				if stringNumericFields[mongoKey] {
					// port 在库里是字符串（"443"），直接 $gte 数字永远匹配不到。
					// 用 $expr + $convert 显式转数字；onError/onNull 给 null，
					// 这样 "https"、"80,443" 这类脏值只是不参与比较，不会让整个查询报错。
					stack = append(stack, map[string]interface{}{
						"$expr": map[string]interface{}{
							mongoOp: []interface{}{
								map[string]interface{}{"$convert": map[string]interface{}{
									"input": "$" + mongoKey, "to": "int", "onError": nil, "onNull": nil,
								}},
								num,
							},
						},
					})
				} else {
					stack = append(stack, map[string]interface{}{
						mongoKey: map[string]interface{}{mongoOp: num},
					})
				}
			} else {
				// 未知字段 + 比较运算符 = 无法解释的条件。以前静默丢弃会退化成全量查询，
				// 用户以为筛过了，实际什么都没筛（2026-11-28 实测 port>=1000 → 45363 条）。
				return nil, fmt.Errorf("不支持的比较字段：%s（可用字段见搜索帮助）", cmpKey)
			}
		} else if strings.Contains(expr, "!=") {
			parts := strings.SplitN(expr, "!=", 2)
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			value = strings.Trim(value, "\"")

			if mongoKey, ok := keyword[key]; ok {
				if strings.HasSuffix(key, "code") || key == "length" {
					numValue := 0
					if _, err := fmt.Sscanf(value, "%d", &numValue); err == nil {
						stack = append(stack, map[string]interface{}{
							mongoKey: map[string]interface{}{
								"$ne": numValue,
							},
						})
					}
				} else {
					if value == "" {
						stack = append(stack, map[string]interface{}{
							"$and": []map[string]interface{}{
								{mongoKey: map[string]interface{}{"$ne": ""}},
								{mongoKey: map[string]interface{}{"$ne": nil}},
								{mongoKey: map[string]interface{}{"$not": map[string]interface{}{"$size": 0}}},
							},
						})
					} else {
						stack = append(stack, map[string]interface{}{
							mongoKey: map[string]interface{}{
								"$not": map[string]interface{}{
									"$regex":   value,
									"$options": "i",
								},
							},
						})
					}
				}
			}
		} else if strings.Contains(expr, "==") {
			parts := strings.SplitN(expr, "==", 2)
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			value = strings.Trim(value, "\"")

			if mongoKey, ok := keyword[key]; ok {
				if strings.HasSuffix(key, "code") || key == "length" {
					numValue := 0
					if _, err := fmt.Sscanf(value, "%d", &numValue); err == nil {
						stack = append(stack, map[string]interface{}{
							mongoKey: map[string]interface{}{
								"$eq": numValue,
							},
						})
					}
				} else {
					stack = append(stack, map[string]interface{}{
						mongoKey: map[string]interface{}{
							"$eq": value,
						},
					})
				}
			}
		} else if strings.Contains(expr, "=") {
			parts := strings.SplitN(expr, "=", 2)
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			value = strings.Trim(value, "\"")

			if mongoKey, ok := keyword[key]; ok {
				stack = append(stack, map[string]interface{}{
					mongoKey: map[string]interface{}{
						"$regex":   value,
						"$options": "i",
					},
				})
			}
		} else {
			// 裸词（例如直接输入一个域名/标题）：在可检索的文本字段上做模糊 OR。
			// 之前这里直接丢弃，导致 "example.com" 返回全量数据，
			// 而 "example.com && ip=1.1.1.1" 会报 "insufficient operands for &&"。
			if or := expandBareTerm(expr, keyword); or != nil {
				stack = append(stack, or)
			}
		}
	}

	// 没有运算符的连接（如 "a=1 b=2"）会让栈里留下多个元素，
	// 之前只取 query[0] 静默丢掉后面的条件，这里显式报错。
	if len(stack) > 1 {
		return nil, errors.New("invalid expression: missing operator (&& or ||) between conditions")
	}

	return stack, nil
}

// bareTermFields 裸词展开时参与匹配的字段（按 mongo 字段名）：
// 只收「身份类」字段，排除 body/rawheaders/metadata/request/response 这类大文本，
// 否则一个裸词会触发全集合大字段正则扫描。
var bareTermFields = map[string]bool{
	"host":         true,
	"rootDomain":   true,
	"ip":           true,
	"url":          true,
	"title":        true,
	"vulname":      true,
	"service":      true,
	"technologies": true,
	"tags":         true,
	"type":         true,
	"taskName":     true,
}

// expandBareTerm 把没有操作符的裸词展开成跨文本字段的模糊 OR。
// 例：asset 索引下 "example.com" → {"$or":[{"host":{...}},{"title":{...}}, ...]}
// 词内的正则元字符会被转义，避免用户输入 ".*" 之类意外匹配全部数据。
func expandBareTerm(term string, keyword map[string]string) map[string]interface{} {
	term = strings.TrimSpace(strings.Trim(term, `"`))
	if term == "" {
		return nil
	}
	quoted := regexp.QuoteMeta(term)

	// 同一 mongo 字段可能被多个 key 指向（如 domain/host），去重但保持稳定顺序
	seen := make(map[string]bool, len(keyword))
	clauses := make([]map[string]interface{}, 0, len(keyword))
	for _, mongoKey := range keyword {
		if !bareTermFields[mongoKey] || seen[mongoKey] {
			continue
		}
		seen[mongoKey] = true
		clauses = append(clauses, map[string]interface{}{
			mongoKey: map[string]interface{}{
				"$regex":   quoted,
				"$options": "i",
			},
		})
	}
	if len(clauses) == 0 {
		return nil
	}
	// map 遍历顺序随机，按 mongoKey 排序保证输出稳定（便于测试与排查）
	sort.Slice(clauses, func(i, j int) bool {
		return fmt.Sprint(clauses[i]) < fmt.Sprint(clauses[j])
	})
	return map[string]interface{}{"$or": clauses}
}

// GetSearchQuery 构建完整的搜索查询
// numericFilterFields 这些过滤字段在库里是数字；字符串形态会导致等值/IN 恒不匹配。
var numericFilterFields = map[string]bool{
	"statuscode": true,
	"status":     true,
	"port":       true,
	"length":     true,
}

// buildFilterForms 返回过滤值的候选形态。
//
// 背景（2026-11-19 实测）：同一个"数值字段"在不同集合里类型不一致——
// asset.statuscode 是 number，但 asset.port 是 string（'443'）。
// 单方向归一（字符串→数字）会让 string 存法的过滤器静默空；
// 因此对数值歧义字段同时下发字符串与数字两种形态，哪种存法都能命中。
func buildFilterForms(field string, v interface{}) []interface{} {
	if !numericFilterFields[field] {
		return []interface{}{v}
	}
	s, ok := v.(string)
	if !ok {
		return []interface{}{v}
	}
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && strconv.Itoa(n) == strings.TrimSpace(s) {
		return []interface{}{s, n}
	}
	return []interface{}{s}
}

func GetSearchQuery(req models.SearchRequest) (map[string]interface{}, error) {
	logger.Logger.Info(fmt.Sprintf("search query: %v", req))
	if req.Index == "" {
		return nil, errors.New("search type is required")
	}

	keyword := SearchKey[req.Index]
	if keyword == nil {
		return nil, errors.New("invalid search type")
	}

	query, err := SearchToMongoDB(req.SearchExpression, keyword)
	if err != nil {
		return nil, err
	}

	if len(query) == 0 {
		return map[string]interface{}{}, nil
	}

	result := query[0]

	// 处理过滤条件
	if len(req.Filter) > 0 {
		if _, ok := result["$and"]; !ok {
			result["$and"] = make([]map[string]interface{}, 0, len(req.Filter))
		}

		for f, values := range req.Filter {
			// 按索引的字段别名：UrlScan/DirScanResult 的状态字段叫 status，
			// 而 asset 叫 statuscode。全局 filterKeyCache 只认 statuscode，
			// 这两个索引下过滤器会查一个不存在的字段、静默返回空（2026-11-19 实测）。
			if req.Index == "UrlScan" || req.Index == "DirScanResult" {
				if f == "statuscode" {
					f = "status"
				}
			}
			// IPAsset 的 port/service/app 在文档里是 ports[] 数组里的嵌套字段，
			// filterKeyCache 只有顶级字段名 → 过滤器查 ports.port 之外的字段永远不命中，
			// 表现为 filter.port=443 返回 0 条（而库里明明有一堆 443）。2026-11-29 实测。
			// 这里直接给出嵌套路径，并让下面的 hasNestedFieldQuery 把查询切到聚合管道。
			nestedKey := ""
			if req.Index == "IPAsset" {
				switch f {
				case "port":
					nestedKey = "ports.port"
				case "service":
					nestedKey = "ports.server.service"
				case "app":
					nestedKey = "ports.server.technologies"
				}
			}
			if nestedKey != "" {
				tmpOr := make([]map[string]interface{}, 0, len(values))
				for _, v := range values {
					if v == "" {
						continue
					}
					for _, form := range buildFilterForms(f, v) {
						tmpOr = append(tmpOr, map[string]interface{}{nestedKey: form})
					}
				}
				if len(tmpOr) > 0 {
					result["$and"] = append(result["$and"].([]map[string]interface{}), map[string]interface{}{
						"$or": tmpOr,
					})
				}
				continue
			}
			if mongoKey, ok := filterKeyCache[f]; ok {
				tmpOr := make([]map[string]interface{}, 0, len(values))
				for _, v := range values {
					if v != "" {
						for _, form := range buildFilterForms(f, v) {
							if strings.Contains(mongoKey, ",") {
								keys := strings.Split(mongoKey, ",")
								for _, key := range keys {
									tmpOr = append(tmpOr, map[string]interface{}{
										key: form,
									})
								}
							} else {
								tmpOr = append(tmpOr, map[string]interface{}{
									mongoKey: form,
								})
							}
						}
					}
				}
				if len(tmpOr) > 0 {
					result["$and"] = append(result["$and"].([]map[string]interface{}), map[string]interface{}{
						"$or": tmpOr,
					})
				}
			}
		}
	}

	// 发现时间范围：只对确实有时间字段的索引生效，避免给没有该字段的集合加条件导致恒空。
	// time 存的是 "2006-01-02 15:04:05" 字符串，字典序等于时间序，直接 $gte/$lte。
	//
	// 但必须先校验格式：任何人传 timeFrom=all / today / 7（前端的时间段标记）过来，
	// 字典序比较 "2026-…" < "all" 会让整个条件恒假 → 返回 0 条，看起来像「数据没了」。
	// 实测 timeFrom=all 时 /api/assets/asset 返回 0 行（2026-11-27）。
	from := normalizeTimeBound(req.TimeFrom, false)
	to := normalizeTimeBound(req.TimeTo, true)
	if (from != "" || to != "") && indexHasTimeField[req.Index] {
		rng := map[string]interface{}{}
		if from != "" {
			rng["$gte"] = from
		}
		if to != "" {
			rng["$lte"] = to
		}
		if _, ok := result["$and"]; !ok {
			result["$and"] = make([]map[string]interface{}, 0, 2)
		}
		result["$and"] = append(result["$and"].([]map[string]interface{}), map[string]interface{}{
			"time": rng,
		})
	}

	// 处理模糊查询
	if len(req.FuzzyQuery) > 0 {
		if _, ok := result["$and"]; !ok {
			result["$and"] = make([]map[string]interface{}, 0, len(req.FuzzyQuery))
		}

		for q, value := range req.FuzzyQuery {
			if value != "" {
				tmpFq := make([]map[string]interface{}, 0, 3) // 预分配空间
				if mongoKey, ok := fuzzyQueryKeyCache[q]; ok {
					if strings.Contains(mongoKey, ",") {
						keys := strings.Split(mongoKey, ",")
						for _, key := range keys {
							tmpFq = append(tmpFq, map[string]interface{}{
								key: map[string]interface{}{
									"$regex":   value,
									"$options": "i",
								},
							})
						}
					} else {
						tmpFq = append(tmpFq, map[string]interface{}{
							mongoKey: map[string]interface{}{
								"$regex":   value,
								"$options": "i",
							},
						})
					}
				}
				if len(tmpFq) > 0 {
					result["$and"] = append(result["$and"].([]map[string]interface{}), map[string]interface{}{
						"$or": tmpFq,
					})
				}
			}
		}
	}

	if and, ok := result["$and"].([]map[string]interface{}); ok && len(and) == 0 {
		delete(result, "$and")
	}

	return result, nil
}

// TimeSortDir 返回按 time 排序的方向：默认 -1（新→旧），
// 请求里 sort.time == "asc" 时返回 1（旧→新）——对齐 ARL-Next v1.5.1 的排序切换。
func TimeSortDir(req models.SearchRequest) int {
	if req.Sort != nil {
		if v, ok := req.Sort["time"]; ok {
			if strings.EqualFold(strings.TrimSpace(v), "asc") {
				return 1
			}
		}
	}
	return -1
}

// normalizeTimeBound 校验时间上/下界，只接受 "YYYY-MM-DD" 或 "YYYY-MM-DD HH:MM:SS"。
// 无法解析的值一律当成「没有这个条件」，而不是拿它去做字典序比较——
// 传 timeFrom=all / today / 7（前端的时间段标记）时，
// "2026-…" < "all" 会让条件恒假，接口静默返回 0 条，看起来像数据丢了（2026-11-27 实测）。
// end=true 时把纯日期补成当天 23:59:59，否则 "2026-11-27" 作为上界会漏掉当天所有记录。
func normalizeTimeBound(v string, end bool) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	layouts := []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02"}
	for _, layout := range layouts {
		t, err := time.ParseInLocation(layout, v, time.Local)
		if err != nil {
			continue
		}
		if layout == "2006-01-02" {
			if end {
				return t.Format("2006-01-02") + " 23:59:59"
			}
			return t.Format("2006-01-02") + " 00:00:00"
		}
		return t.Format("2006-01-02 15:04:05")
	}
	return ""
}

// timeFields：库里以 "2006-01-02 15:04:05" 字符串保存的时间字段。
// 同格式下字典序即时间序，所以直接做字符串范围比较即可。
var timeFields = map[string]bool{
	"time":         true,
	"lastScanTime": true,
	"creatTime":    true,
	"endTime":      true,
	"updateTime":   true,
}

func isTimeField(mongoKey string) bool { return timeFields[mongoKey] }

// looksLikeTime 只放行 "YYYY-MM-DD HH:MM:SS"（也接受纯日期，补成当天 0 点由调用方负责）。
// 不做宽松解析：把 "abc" 当成时间比较会静默查不到数据。
func looksLikeTime(v string) bool {
	if _, err := time.Parse("2006-01-02 15:04:05", v); err == nil {
		return true
	}
	_, err := time.Parse("2006-01-02", v)
	return err == nil
}

// init 把 time 关键字挂到「确实有 time 字段」的索引上。
//
// 之前 time 根本不在 SearchKey 里，于是 time>="2026-11-21 00:00:00" 会被当成未知字段——
// 而访问这一能力（按发现时间筛）本来就是各个列表需要的维度（ARL-Next bc22a10 的 12 维口径）。
// 只挂 indexHasTimeField 里为 true 的索引：没有该字段的集合挂上会「恒返回空」，比不筛更糟。
func init() {
	for idx := range indexHasTimeField {
		m, ok := SearchKey[idx]
		if !ok {
			continue
		}
		if _, exists := m["time"]; !exists {
			m["time"] = "time"
		}
	}
}
