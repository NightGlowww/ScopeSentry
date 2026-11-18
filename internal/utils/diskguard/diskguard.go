// Package diskguard 在启动扫描前检查磁盘水位。
//
// 背景：扫描产物（资产/截图/响应体）写在数据盘上，磁盘写满会把整机拖到
// sshd 无法响应（2026-11-19 生产事故）。阈值之下的新扫描直接拒绝启动。
package diskguard

import (
	"fmt"

	"github.com/Autumn-27/ScopeSentry/internal/utils/hoststat"
)

// MinFreePercent 允许启动扫描的最低磁盘可用百分比。
const MinFreePercent = 3.0

// FreePercent 可注入，便于测试；默认读真实磁盘。
var FreePercent = func(path string) float64 { return hoststat.Stat(path).DiskFreePercent }

// Check 在磁盘可用空间低于阈值时返回错误；拿不到磁盘信息（-1）时不拦。
func Check(path string) error {
	p := FreePercent(path)
	if p >= 0 && p < MinFreePercent {
		return fmt.Errorf("磁盘可用空间仅 %.1f%%（阈值 %.0f%%），已拒绝启动扫描任务；请先清理磁盘", p, MinFreePercent)
	}
	return nil
}
