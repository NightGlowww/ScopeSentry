//go:build integration

package taskstats_test

// 终态通知端到端：起一个真实 HTTP 接收端，确认消息真的发出去了、内容对得上。
//
// 这条用例存在的理由：config.Notification 一直没有任何调用方 —— 通知配置能被添加、
// 能被加载进内存，却永远不发。只测 FormatTerminalMessage（纯函数）证明不了「发出去」。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/ScopeSentry/internal/config"
	"github.com/Autumn-27/ScopeSentry/internal/constants"
	"github.com/Autumn-27/ScopeSentry/internal/models"
	"github.com/Autumn-27/ScopeSentry/internal/services/task/taskstats"
	"go.mongodb.org/mongo-driver/bson"
)

func TestNotifyTerminalReachesWebhook(t *testing.T) {
	got := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.URL.Query().Get("msg")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	orig := config.NotificationApi
	t.Cleanup(func() { config.NotificationApi = orig })
	config.NotificationApi = []models.NotificationApi{{
		Method: "GET",
		Url:    srv.URL + "/?msg=*msg*",
		State:  true,
	}}

	set := bson.M{
		"status": constants.TaskStatusFinished,
		"stats":  bson.M{"subdomain": int64(7), "vulnerability": int64(2)},
	}
	taskstats.NotifyTerminal("it-notify-task", set)

	select {
	case msg := <-got:
		if !strings.Contains(msg, "it-notify-task") || !strings.Contains(msg, "子域 7") || !strings.Contains(msg, "漏洞 2") {
			t.Fatalf("通知内容不对: %q", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("3 秒内没有收到通知请求：通知链路没接上")
	}

	// 非终态不该发：保证这条链路不会被进度轮询刷屏
	taskstats.NotifyTerminal("it-notify-running", bson.M{"status": constants.TaskStatusRunning})
	select {
	case msg := <-got:
		t.Fatalf("运行中不应发通知，却收到: %q", msg)
	case <-time.After(300 * time.Millisecond):
		// 预期：什么都没发
	}
}

// POST + JSON 模板：body 里的占位符必须按 JSON 转义（正文带引号/换行时不破坏 JSON）。
// GET 走 query 转义、POST 走 JSON 转义 —— 两种载体混用会让其中一种永远发不出去。
func TestNotifyTerminalPostJSONBody(t *testing.T) {
	type body struct {
		Msg string `json:"msg"`
	}
	got := make(chan body, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b body
		_ = json.NewDecoder(r.Body).Decode(&b)
		got <- b
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	orig := config.NotificationApi
	t.Cleanup(func() { config.NotificationApi = orig })
	config.NotificationApi = []models.NotificationApi{{
		Method:      "POST",
		Url:         srv.URL + "/hook",
		ContentType: "application/json",
		Data:        `{"msg":"*msg*"}`,
		State:       true,
	}}

	config.Notification("带\"引号\"与\n换行")
	select {
	case b := <-got:
		if !strings.Contains(b.Msg, "带\"引号\"") || !strings.Contains(b.Msg, "换行") {
			t.Fatalf("POST 正文被转义弄坏了: %q", b.Msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("POST 通知没到")
	}
}
