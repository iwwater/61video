// Package safefetch 在请求用户配置的目标 URL 前做最小化 SSRF 防护：
//  - scheme 白名单（仅 http/https）
//  - 解析 host，拒绝解析到私网/回环/链路本地/云元数据 IP
//  - 默认 8 秒超时
//  - 不跟随重定向（防止跳到内网）
//
// 本包是最低实现：不防 DNS rebinding 攻击（攻击者把域名解析到公网 IP 通过校验后
// 立刻改成内网 IP）。要彻底防需要在 dial 时再次解析并比对。在个人项目场景下，
// 一次性解析 + IP 黑名单已能挡掉绝大多数 SSRF 滥用。
package safefetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultTimeout 单次请求的默认超时。
	DefaultTimeout = 8 * time.Second
	// DefaultMaxBytes 默认 body 上限（8 MiB）。
	DefaultMaxBytes int64 = 8 << 20
)

// ErrBlocked 目标 URL 被 SSRF 防护拦截。
type ErrBlocked struct {
	Reason string
}

func (e *ErrBlocked) Error() string {
	return "url blocked: " + e.Reason
}

// IsBlocked 判断错误是否由 SSRF 防护产生。
func IsBlocked(err error) bool {
	var b *ErrBlocked
	return errors.As(err, &b)
}

// Options 控制 fetch 行为。
type Options struct {
	Timeout  time.Duration
	MaxBytes int64
	// Headers 额外加到请求上（如 UA / Accept / Accept-Language）。
	Headers http.Header
	// Method，缺省 GET。
	Method string
}

func (o Options) timeout() time.Duration {
	if o.Timeout > 0 {
		return o.Timeout
	}
	return DefaultTimeout
}

func (o Options) maxBytes() int64 {
	if o.MaxBytes > 0 {
		return o.MaxBytes
	}
	return DefaultMaxBytes
}

// Client 包内复用的 http.Client（不跟随重定向）。
var Client = &http.Client{
	Timeout: DefaultTimeout,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return errors.New("redirects are not allowed")
	},
}

// Get 执行一次受限的 HTTP GET。返回响应对象，body 已被限制在 MaxBytes 以内。
//
// 调用方需在用完 resp 后调用 resp.Body.Close()。如果 err != nil，resp 可能为 nil。
func Get(ctx context.Context, rawURL string, opts Options) (*http.Response, error) {
	if err := ValidateURL(rawURL); err != nil {
		return nil, err
	}
	method := opts.Method
	if method == "" {
		method = http.MethodGet
	}
	ctx, cancel := context.WithTimeout(ctx, opts.timeout())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	for k, vs := range opts.Headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := Client.Do(req)
	if err != nil {
		return nil, err
	}
	if opts.maxBytes() > 0 {
		resp.Body = io.NopCloser(io.LimitReader(resp.Body, opts.maxBytes()))
	}
	return resp, nil
}

// ValidateURL 校验 URL 是否符合 SSRF 防护策略。仅在请求前调用，避免拉了内容才拒。
func ValidateURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return &ErrBlocked{Reason: "invalid url: " + err.Error()}
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return &ErrBlocked{Reason: "unsupported scheme: " + u.Scheme}
	}
	host := u.Hostname()
	if host == "" {
		return &ErrBlocked{Reason: "empty host"}
	}
	// 如果 host 是字面量 IP，直接校验；否则解析一次。
	if ip := net.ParseIP(host); ip != nil {
		if blocked, reason := ipBlocked(ip); blocked {
			return &ErrBlocked{Reason: reason}
		}
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return &ErrBlocked{Reason: "dns lookup failed: " + err.Error()}
	}
	if len(ips) == 0 {
		return &ErrBlocked{Reason: "no dns record"}
	}
	for _, ip := range ips {
		if blocked, reason := ipBlocked(ip); blocked {
			return &ErrBlocked{Reason: reason}
		}
	}
	return nil
}

// ipBlocked 判断单个 IP 是否命中黑名单。
func ipBlocked(ip net.IP) (bool, string) {
	if ip == nil {
		return true, "nil ip"
	}
	v4 := ip.To4()
	if v4 != nil {
		switch {
		case v4[0] == 127:
			return true, "loopback"
		case v4[0] == 10:
			return true, "private (10.0.0.0/8)"
		case v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31:
			return true, "private (172.16.0.0/12)"
		case v4[0] == 192 && v4[1] == 168:
			return true, "private (192.168.0.0/16)"
		case v4[0] == 169 && v4[1] == 254:
			return true, "link-local (incl. cloud metadata 169.254.169.254)"
		case v4[0] == 0:
			return true, "wildcard (0.0.0.0)"
		case v4[0] >= 224:
			return true, "multicast/broadcast"
		}
		return false, ""
	}
	// IPv6 校验
	if ip.IsLoopback() {
		return true, "ipv6 loopback (::1)"
	}
	if ip.IsLinkLocalUnicast() {
		return true, "ipv6 link-local (fe80::/10)"
	}
	if ip.IsLinkLocalMulticast() {
		return true, "ipv6 link-local multicast"
	}
	if ip.IsMulticast() {
		return true, "ipv6 multicast"
	}
	if ip.IsUnspecified() {
		return true, "ipv6 unspecified (::)"
	}
	// Unique local addresses fc00::/7
	if ip[0]&0xfe == 0xfc {
		return true, "ipv6 unique-local (fc00::/7)"
	}
	// IPv4-mapped IPv6 (::ffff:0:0/96) 单独走 v4 校验
	if v4 := ip.To4(); v4 != nil {
		return ipBlocked(v4)
	}
	return false, ""
}
