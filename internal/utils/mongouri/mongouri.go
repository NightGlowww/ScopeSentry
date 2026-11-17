// Package mongouri 负责把配置拼成 MongoDB 连接串与客户端选项（issue #137）。
//
// 之前只支持「用户名 + 密码」，连 Atlas、开了 authSource 的集群、或者要求 TLS 的实例时
// 根本连不上。这里把 authSource / authMechanism / TLS / TLS CA 都纳入，并保证密码里的
// 特殊字符按 URL 规则正确转义（旧实现先 QueryEscape 再拼 URL，属于双重转义）。
package mongouri

import (
	"fmt"
	"net"
	"net/url"
	"strconv"

	"go.mongodb.org/mongo-driver/mongo/options"
)

// Options 连接 MongoDB 需要的全部参数
type Options struct {
	Host          string
	Port          int
	Username      string
	Password      string
	AuthSource    string
	AuthMechanism string
	TLS           bool
	TLSCAFile     string
}

// Build 生成连接 URI 与客户端选项。
// 返回 URI 是为了让调用方/测试能拿到最终字符串（ClientOptions 不保留原文）。
func Build(o Options) (string, *options.ClientOptions, error) {
	if o.Host == "" {
		return "", nil, fmt.Errorf("mongodb host is required")
	}
	port := o.Port
	if port == 0 {
		port = 27017
	}

	u := &url.URL{
		Scheme: "mongodb",
		Host:   net.JoinHostPort(o.Host, strconv.Itoa(port)),
	}
	if o.Username != "" || o.Password != "" {
		u.User = url.UserPassword(o.Username, o.Password)
	}

	q := url.Values{}
	if o.AuthSource != "" {
		q.Set("authSource", o.AuthSource)
	}
	if o.AuthMechanism != "" {
		q.Set("authMechanism", o.AuthMechanism)
	}
	if o.TLS {
		q.Set("tls", "true")
	}
	if o.TLSCAFile != "" {
		q.Set("tlsCAFile", o.TLSCAFile)
	}
	u.RawQuery = q.Encode()
	uri := u.String()
	return uri, options.Client().ApplyURI(uri), nil
}

// URI 返回不含密码的展示用地址（日志/报错里只能出现这个）。
func URI(o Options) string {
	port := o.Port
	if port == 0 {
		port = 27017
	}
	s := net.JoinHostPort(o.Host, strconv.Itoa(port))
	if o.Username != "" {
		return fmt.Sprintf("mongodb://%s@%s", o.Username, s)
	}
	return "mongodb://" + s
}
