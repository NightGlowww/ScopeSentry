package models

// AssetAddBatchRequest 批量录入资产（issue #203 扩展）。
//
// 一次最多 500 条：手工导入通常是几十行，设上限避免一个请求跑太久把网关拖超时。
// Project / Tags 作用于所有条目，条目里自己填了则优先用自己的。
type AssetAddBatchRequest struct {
	Items   []AssetAddRequest `json:"items" binding:"required,min=1,max=500"`
	Project string            `json:"project"`
	Tags    []string          `json:"tags"`
}

// AssetAddBatchResult 批量录入结果：成功 / 重复 / 非法分开计数，便于前端给出明确提示。
type AssetAddBatchResult struct {
	Inserted   int      `json:"inserted"`
	Duplicated int      `json:"duplicated"`
	Invalid    int      `json:"invalid"`
	Errors     []string `json:"errors,omitempty"`
}
