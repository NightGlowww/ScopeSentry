// models-------------------------------------
// @file      : backup.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2026/11/18 01:10
// -------------------------------------------

package models

// BackupCreateRequest 创建备份请求
type BackupCreateRequest struct {
	Note string `json:"note"`
}

// BackupRestoreRequest 恢复请求
type BackupRestoreRequest struct {
	Name    string `json:"name" binding:"required"`
	Confirm string `json:"confirm" binding:"required"`
}

// BackupCreateResponse 创建/上传备份后的返回
type BackupCreateResponse struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// BackupInfo 备份包描述，同时作为备份目录里的 *.meta.json 落盘结构
type BackupInfo struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	CreatedAt string `json:"createdAt"`
	SHA256    string `json:"sha256"`
	Note      string `json:"note"`
}

// BackupStatus 备份/恢复任务状态
type BackupStatus struct {
	Running   bool   `json:"running"`
	Current   string `json:"current"`
	Progress  int    `json:"progress"`
	LastError string `json:"lastError"`
}

// BackupManifest 备份包内的 manifest.json
type BackupManifest struct {
	Version     int                     `json:"version"`
	CreatedAt   string                  `json:"createdAt"`
	SHA256      string                  `json:"sha256"`
	Collections []BackupCollectionEntry `json:"collections"`
	Uploads     BackupDirSummary        `json:"uploads"`
	Files       BackupDirSummary        `json:"files"`
}

// BackupCollectionEntry 单个集合的清单项
type BackupCollectionEntry struct {
	Name      string `json:"name"`
	File      string `json:"file"`
	Documents int64  `json:"documents"`
	SHA256    string `json:"sha256"`
}

// BackupDirSummary uploads/ files/ 目录的文件数与字节数
type BackupDirSummary struct {
	Files int   `json:"files"`
	Bytes int64 `json:"bytes"`
}

// BackupRestoreResult 恢复结果
type BackupRestoreResult struct {
	Restored    string `json:"restored"`
	PreRestore  string `json:"preRestore"`
	Collections int    `json:"collections"`
	Documents   int64  `json:"documents"`
	Files       int    `json:"files"`
}
