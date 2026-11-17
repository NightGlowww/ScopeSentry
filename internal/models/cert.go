package models

import "time"

// CertListRequest 证书列表查询参数（对标 ARL-Next assetCert 列表）。
//
// 数据源是 asset 集合里的 TLS 字段，每次现算，不依赖任何回填集合。
// pageSize 上限 200：由 binding 直接拒成 400，服务层再做一次兜底归一化。
type CertListRequest struct {
	// Keyword 子串匹配 subject/issuer/serial/指纹/SAN/关联 host，不区分大小写
	Keyword string `json:"keyword" form:"keyword" binding:"max=255"`
	// Project 项目名，精确匹配
	Project string `json:"project" form:"project" binding:"max=255"`
	// Expired 过期过滤：true=只返回已过期，false=只返回未过期，不传=全部
	Expired   *bool `json:"expired" form:"expired"`
	PageIndex int   `json:"pageIndex" form:"pageIndex" binding:"omitempty,min=0"`
	PageSize  int   `json:"pageSize" form:"pageSize" binding:"omitempty,min=0,max=200"`
}

// CertDetailRequest 证书详情请求。id 是列表返回的证书聚合键
// （fp:<fingerprint> / sn:<issuer>|<serial> / sub:<subject>|<issuer>|...）。
type CertDetailRequest struct {
	ID string `json:"id" binding:"required,max=1024"`
	// Project 可选：传入时只返回该项目下的关联 host，与列表口径保持一致；
	// 不传则返回该证书在全部项目里的关联。
	Project string `json:"project" form:"project" binding:"max=255"`
}

// CertHost 证书关联的一个 host+ip+port 三元组。
type CertHost struct {
	Host string `json:"host" bson:"host"`
	IP   string `json:"ip" bson:"ip"`
	Port string `json:"port" bson:"port"`
}

// CertRow 一张证书的聚合行。
//
// 同一张证书（指纹，或 serial+issuer）在多个资产上出现时合并为一行：
// HostCount 是完整的关联数，Hosts 只保留前 50 个（由聚合里的 $slice 截断）。
type CertRow struct {
	ID              string     `json:"id" bson:"_id"`
	Subject         string     `json:"subject" bson:"subject"`
	SubjectCN       string     `json:"subjectCN" bson:"subjectCN"`
	Issuer          string     `json:"issuer" bson:"issuer"`
	IssuerCN        string     `json:"issuerCN" bson:"issuerCN"`
	Serial          string     `json:"serial" bson:"serial"`
	Fingerprint     string     `json:"fingerprint" bson:"fingerprint"`
	FingerprintSHA1 string     `json:"fingerprintSha1" bson:"fingerprintSha1"`
	FingerprintMD5  string     `json:"fingerprintMd5" bson:"fingerprintMd5"`
	NotBefore       *time.Time `json:"notBefore" bson:"notBefore"`
	NotAfter        *time.Time `json:"notAfter" bson:"notAfter"`
	Expired         bool       `json:"expired" bson:"expired"`
	SAN             []string   `json:"san" bson:"san"`
	Hosts           []CertHost `json:"hosts" bson:"hosts"`
	HostCount       int        `json:"hostCount" bson:"hostCount"`
}

// CertListResult 证书列表返回结构
type CertListResult struct {
	List  []CertRow `json:"list"`
	Total int64     `json:"total"`
}
