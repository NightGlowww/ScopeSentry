// config-------------------------------------
// @file      : notification.go
// @author    : Autumn
// @contact   : rainy-autumn@outlook.com
// @time      : 2025/12/22 22:08
// -------------------------------------------

package config

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/Autumn-27/ScopeSentry/internal/global"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/utils"
	"go.uber.org/zap"
)

var NotificationApi []models.NotificationApi

// jsonEscape 生成可嵌进 JSON 字符串的正文（去引号）。
// POST 的 data 模板是 {"msg":"*msg*"} 这种形态，正文必须按 JSON 转义。
func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}

// Notification 按配置逐个发送通知。
//
// 两种载体要两种转义，之前统一用 JSON 转义导致 GET 不可用：
// 正文里的空格/中文直接拼进 query 会生成非法 URL（Go 解析直接报错），
// 于是「钉钉/PushPlus 这类 GET 机器人」即使配好了也永远收不到消息。
func Notification(msg string) {
	for _, api := range NotificationApi {
		// URL 里的占位符一律按 query 转义
		uri := strings.Replace(api.Url, "*msg*", url.QueryEscape(msg), -1)
		if api.Method == "GET" {
			if _, err := utils.Requests.HttpGet(uri); err != nil {
				global.Log.Error(msg, zap.Error(err))
			}
			continue
		}
		// body 里的占位符按 JSON 字符串转义
		data := strings.Replace(api.Data, "*msg*", jsonEscape(msg), -1)
		if err, _ := utils.Requests.HttpPost(uri, []byte(data), api.ContentType); err != nil {
			global.Log.Error(msg, zap.Error(err))
		}
	}
}
