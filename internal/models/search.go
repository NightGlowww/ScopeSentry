// models-------------------------------------
// @file      : search.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2025/5/7 19:27
// -------------------------------------------

package models

// DeleteBySearchRequest 按当前筛选条件批量删除（issue #258 / #191）。
//
// 资产量很大时前端只能一页一页选、一次删 1000 条，删 17 万条要点上百次。
// 这里复用列表页的搜索表达式与筛选条件，由服务端分批删。
type DeleteBySearchRequest struct {
	Index  string                   `json:"index" binding:"required"`
	Search string                   `json:"search"`
	Filter map[string][]interface{} `json:"filter"`
	// ConfirmAll 为空筛选（等于清空整个集合）时必须显式传 true，避免误触把库清空
	ConfirmAll bool `json:"confirmAll"`
}

// SearchRequest 定义了搜索请求的参数结构
type SearchRequest struct {
	PageIndex int `json:"pageIndex"`
	PageSize  int `json:"pageSize"`
	// Index 搜索类型，对应SearchKey中的key
	Index            string `json:"index"`
	SearchExpression string `json:"search"`
	// Filter 过滤条件
	Filter map[string][]interface{} `json:"filter"`
	// FuzzyQuery 模糊查询条件
	FuzzyQuery map[string]string `json:"-"`
	Sort       map[string]string `json:"sort"`
	Sid        string            `json:"sid"`
}
