// Package taskstats 负责「任务产出快照」的冻结。
//
// 为什么要单独成包：状态转终态的地方不止一处（正常完成、自动重投放弃、
// 巡检判死、对账收尾），冻结逻辑必须只有一份，否则总有一条路径漏掉快照，
// 列表上就出现「终态任务却显示 —」。
package taskstats

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/constants"
	"github.com/Autumn-27/ScopeSentry/internal/database/mongodb"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// statsTimeLayout 与任务表其它时间字段一致（本地时区字符串）。
const statsTimeLayout = "2006-01-02 15:04:05"

// specs 参与统计的集合：key 是返回给前端的字段名，coll 是实际集合。
var specs = []struct{ key, coll string }{
	{"subdomain", "subdomain"},
	{"url", "UrlScan"},
	{"asset", "asset"},
	{"vulnerability", "vulnerability"},
	// 下面两个是 ARL-Next 1.5.2 产出维度（ip / wih）在库里的落点：
	//   ip   → IPAsset（与 ip 读模型同一集合，字段就是 taskName）
	//   wih  → SensitiveResult（wih 是它的派生读模型，没有独立集合；口径写清楚比造假维度好）
	{"ip", "IPAsset"},
	{"sensitive", "SensitiveResult"},
}

// Freeze 统计任务产出，返回可直接并入 $set 的字段（stats + statsFrozenAt）。
// 任务名为空时返回 nil：任务名为空的过滤条件等于「匹配全库」，绝不能拿去统计。
func Freeze(ctx context.Context, taskName string) bson.M {
	name := strings.TrimSpace(taskName)
	if name == "" || mongodb.DB == nil {
		return nil
	}
	stats := bson.M{}
	for _, s := range specs {
		n, err := mongodb.DB.Collection(s.coll).CountDocuments(ctx, bson.M{"taskName": name})
		if err != nil {
			// 统计失败就整条不写：写一半的快照比没有更误导
			return nil
		}
		stats[s.key] = n
	}
	return bson.M{"stats": stats, "statsFrozenAt": time.Now().Format(statsTimeLayout)}
}

// Merge 把 Freeze 的结果并进已有的 $set 文档（nil 安全）。
func Merge(set bson.M, frozen bson.M) bson.M {
	if len(frozen) == 0 {
		return set
	}
	if set == nil {
		set = bson.M{}
	}
	for k, v := range frozen {
		set[k] = v
	}
	return set
}

// IsTerminal 判断状态是否已终态（完成 / 失败）。
func IsTerminal(status int) bool {
	return status == constants.TaskStatusFinished || status == constants.TaskStatusFailed
}

// FreezeByID 给只拿得到任务 id 的调用方：先读任务名，再冻结。
// 读不到任务（已删）时返回 nil，调用方 merge 后等于什么都没加。
func FreezeByID(ctx context.Context, id primitive.ObjectID) bson.M {
	if mongodb.DB == nil {
		return nil
	}
	var row struct {
		Name string `bson:"name"`
	}
	if err := mongodb.DB.Collection("task").FindOne(ctx, bson.M{"_id": id}).Decode(&row); err != nil {
		return nil
	}
	return Freeze(ctx, row.Name)
}

// DimOrder 产出维度（外部名）与 stats 快照键的对应，顺序即默认口径。
//
// nuclei 没有独立口径（nuclei 命中就是 vulnerability 文档），所以不列出来：
// 传了要明确报错，拿 vuln 冒充只会让人以为筛到了 nuclei。
var DimOrder = []struct{ Name, Key string }{
	{"site", "asset"},
	{"domain", "subdomain"},
	{"url", "url"},
	{"ip", "ip"},
	{"vuln", "vulnerability"},
	{"wih", "sensitive"},
}

// ErrUnsupportedDim 未知产出维度
var ErrUnsupportedDim = errors.New("unsupported asset type")

// ResolveDims 把外部维度名翻译成 stats 键；空表示全部支持的维度。
func ResolveDims(types []string) ([]string, error) {
	if len(types) == 0 {
		keys := make([]string, 0, len(DimOrder))
		for _, d := range DimOrder {
			keys = append(keys, d.Key)
		}
		return keys, nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(types))
	for _, t := range types {
		name := strings.ToLower(strings.TrimSpace(t))
		key := ""
		for _, d := range DimOrder {
			if d.Name == name {
				key = d.Key
				break
			}
		}
		if key == "" {
			return nil, fmt.Errorf("%w: %s（支持 site/domain/url/ip/vuln/wih）", ErrUnsupportedDim, t)
		}
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	return out, nil
}
