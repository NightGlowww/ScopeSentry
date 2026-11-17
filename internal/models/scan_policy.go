package models

import (
	"encoding/json"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 扫描策略（对标 ARL-Next backend/app/routes/policy.py）：
// 一组可复用的扫描参数，建任务时可选策略覆盖模板默认值。
//
// ARL-Next 的 policy 按阶段切块（domain_config / ip_config / site_config / poc_config），
// 本仓库的扫描模板则是「分阶段 + 插件 hash + CLI 参数串」。这里不改模板存储，
// 只收敛出任务真正会用到的 6 个旋钮（模板 / 插件 / 并发 / 速率 / 超时 / 字典引用）
// 外加一个自由 options 槽位：策略只做参数预设，不参与执行链路。

const (
	// ScanPolicyMaxPageSize 列表分页上限，超过直接 400（与 ICP / GitHub / MCP 治理面同一口径）
	ScanPolicyMaxPageSize = 200
	// ScanPolicyDefaultPageSize 未传 pageSize 时的默认值
	ScanPolicyDefaultPageSize = 20

	// ScanPolicyMinThreadNum / ScanPolicyMaxThreadNum 并发区间
	ScanPolicyMinThreadNum = 1
	ScanPolicyMaxThreadNum = 200

	// ScanPolicyMinRateLimit / ScanPolicyMaxRateLimit 速率区间（0 表示不限速）
	ScanPolicyMinRateLimit = 0
	ScanPolicyMaxRateLimit = 1000

	// ScanPolicyMinTimeoutSeconds / ScanPolicyMaxTimeoutSeconds 单任务超时区间（秒）
	ScanPolicyMinTimeoutSeconds = 10
	ScanPolicyMaxTimeoutSeconds = 3600

	// ScanPolicyMaxNameRunes 策略名长度上限
	ScanPolicyMaxNameRunes = 64
	// ScanPolicyMaxDescriptionRunes 描述长度上限
	ScanPolicyMaxDescriptionRunes = 512
	// ScanPolicyMaxPlugins 一条策略最多携带多少插件引用
	ScanPolicyMaxPlugins = 512
	// ScanPolicyMaxOptions options 最大条目数
	ScanPolicyMaxOptions = 64
	// ScanPolicyMaxDictRefRunes 单个字典引用长度上限
	ScanPolicyMaxDictRefRunes = 128
	// ScanPolicyMaxOptionKeyRunes option key 长度上限
	ScanPolicyMaxOptionKeyRunes = 64
	// ScanPolicyMaxOptionValueRunes option value 长度上限
	ScanPolicyMaxOptionValueRunes = 1024
)

// ScanPolicyDicts 字典引用（三处可分别覆盖；nil = 不覆盖，指向空串 = 显式清空）
type ScanPolicyDicts struct {
	Dir       *string `bson:"dir,omitempty" json:"dir,omitempty"`
	Subdomain *string `bson:"subdomain,omitempty" json:"subdomain,omitempty"`
	Brute     *string `bson:"brute,omitempty" json:"brute,omitempty"`
}

// ScanPolicyConfig 策略参数（也是 apply 的合并单元 / test 的校验单元）
//
// 指针字段用于区分「未设置」与「显式 0」：
//   - rateLimit=0 是合法值（不限速），不能用零值表示「未设置」；
//   - threadNum / timeoutSeconds 范围不含 0，但同样用指针保持三者的写法一致。
type ScanPolicyConfig struct {
	TemplateID     string            `bson:"templateId,omitempty" json:"templateId,omitempty"`
	Plugins        []string          `bson:"plugins,omitempty" json:"plugins,omitempty"`
	ThreadNum      *int              `bson:"threadNum,omitempty" json:"threadNum"`
	RateLimit      *int              `bson:"rateLimit,omitempty" json:"rateLimit"`
	TimeoutSeconds *int              `bson:"timeoutSeconds,omitempty" json:"timeoutSeconds"`
	Dicts          *ScanPolicyDicts  `bson:"dicts,omitempty" json:"dicts,omitempty"`
	Options        map[string]string `bson:"options,omitempty" json:"options,omitempty"`
}

// ScanPolicy 策略文档（集合 scan_policy，name 唯一）
type ScanPolicy struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Name        string             `bson:"name" json:"name"`
	Description string             `bson:"description" json:"description"`
	Config      ScanPolicyConfig   `bson:"config" json:"config"`
	Enabled     bool               `bson:"enabled" json:"enabled"`
	CreatedAt   time.Time          `bson:"createdAt" json:"createdAt"`
	UpdatedAt   time.Time          `bson:"updatedAt" json:"updatedAt"`
}

// PolicyUsage 引用说明（detail 返回）
//
// 任务表没有保存 policyId，无法统计「有多少任务用了这条策略」；
// 这里只如实说明该事实，避免前端把 detail 的 usage 当成真实引用计数。
type PolicyUsage struct {
	Tracked        bool   `json:"tracked"`
	ReferenceCount int    `json:"referenceCount"`
	Note           string `json:"note"`
}

// ScanPolicyDetail detail 端点响应：策略本体 + 引用说明
type ScanPolicyDetail struct {
	ScanPolicy
	Usage PolicyUsage `json:"usage"`
}

// PolicyWarning 合并冲突提示（apply 返回，不阻断合并）
type PolicyWarning struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// PolicyMergeResult apply 的纯合并结果
type PolicyMergeResult struct {
	Config        ScanPolicyConfig `json:"config"`
	Warnings      []PolicyWarning  `json:"warnings"`
	AppliedFields []string         `json:"appliedFields"`
}

// PolicyIssue 校验问题（test 返回 / create、update 的 400 详情）
type PolicyIssue struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// PolicyValidation test 端点响应
type PolicyValidation struct {
	Valid    bool             `json:"valid"`
	Config   ScanPolicyConfig `json:"config"`
	Errors   []PolicyIssue    `json:"errors"`
	Warnings []PolicyIssue    `json:"warnings"`
}

// ScanTemplatePhases 扫描模板里的分阶段插件清单（只取 baseline 需要的字段）
type ScanTemplatePhases struct {
	TargetHandler       []string `bson:"TargetHandler"`
	SubdomainScan       []string `bson:"SubdomainScan"`
	SubdomainSecurity   []string `bson:"SubdomainSecurity"`
	PortScanPreparation []string `bson:"PortScanPreparation"`
	PortScan            []string `bson:"PortScan"`
	PortFingerprint     []string `bson:"PortFingerprint"`
	AssetMapping        []string `bson:"AssetMapping"`
	AssetHandle         []string `bson:"AssetHandle"`
	URLScan             []string `bson:"URLScan"`
	WebCrawler          []string `bson:"WebCrawler"`
	URLSecurity         []string `bson:"URLSecurity"`
	DirScan             []string `bson:"DirScan"`
	VulnerabilityScan   []string `bson:"VulnerabilityScan"`
	PassiveScan         []string `bson:"PassiveScan"`
}

// ScanPolicyCreateRequest 新建策略
type ScanPolicyCreateRequest struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Config      ScanPolicyConfig `json:"config"`
	Enabled     *bool            `json:"enabled"`
}

// ScanPolicyUpdateRequest 更新策略（指针 = 未传不改；config 传了就整体替换）
type ScanPolicyUpdateRequest struct {
	ID          string            `json:"id"`
	PolicyID    string            `json:"policyId"`
	Name        *string           `json:"name"`
	Description *string           `json:"description"`
	Config      *ScanPolicyConfig `json:"config"`
	Enabled     *bool             `json:"enabled"`
}

// IDValue 取更新目标 ID（id > policyId）
func (r ScanPolicyUpdateRequest) IDValue() string {
	for _, v := range []string{r.ID, r.PolicyID} {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ScanPolicyIDRequest 单条 / 批量策略操作的统一入参
type ScanPolicyIDRequest struct {
	ID         string   `json:"id"`
	PolicyID   string   `json:"policyId"`
	Underscore string   `json:"_id"`
	IDs        []string `json:"ids"`
}

// Single 取单个 ID（id > policyId > _id）
func (r ScanPolicyIDRequest) Single() string {
	for _, v := range []string{r.ID, r.PolicyID, r.Underscore} {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// All 取全部 ID（单个 ID + ids，去空去重保序）
func (r ScanPolicyIDRequest) All() []string {
	raw := make([]string, 0, len(r.IDs)+1)
	if single := r.Single(); single != "" {
		raw = append(raw, single)
	}
	raw = append(raw, r.IDs...)
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, id := range raw {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// ScanPolicyApplyRequest apply 入参
//
// TaskTemplate 允许两种形态：
//   - 对象：直接作为「模板默认值」参与合并（前端把当前任务表单原样带上来）；
//   - 字符串：扫描模板 ID，服务端读模板分阶段插件清单生成 baseline（templateId + plugins）。
type ScanPolicyApplyRequest struct {
	PolicyID     string          `json:"policyId"`
	TaskTemplate json.RawMessage `json:"taskTemplate"`
}

// ScanPolicyApplyResult apply 响应（纯预览，不写库）
type ScanPolicyApplyResult struct {
	PolicyID           string           `json:"policyId"`
	PolicyName         string           `json:"policyName"`
	TaskTemplateSource string           `json:"taskTemplateSource"`
	Config             ScanPolicyConfig `json:"config"`
	Warnings           []PolicyWarning  `json:"warnings"`
	AppliedFields      []string         `json:"appliedFields"`
}

// ScanPolicyListQuery 列表查询
type ScanPolicyListQuery struct {
	Keyword   string
	Enabled   *bool
	PageIndex int
	PageSize  int
}
