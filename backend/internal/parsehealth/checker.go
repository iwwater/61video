// Package parsehealth 周期性地对每个启用的 parse_source 发一次请求，
// 记录"是否能正常响应"作为健康检查。后台 admin 可在 ParseSourcesPage
// 上看到每个源的"绿/红点 + 最近检查时间 + 错误信息"。
package parsehealth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/video-site/backend/internal/catalog"
	"github.com/video-site/backend/internal/safefetch"
)

// 公共测试视频 URL（用最常见的视频站，让所有源都至少能"响应"测试）。
// 注意：这个 URL 本身能不能解析不重要，重要的是源服务器能不能正确响应请求。
const testVideoURL = "https://www.iqiyi.com/v_19rr9agfg0.html"

// 默认健康检查间隔。
const defaultInterval = 5 * time.Minute

// 默认单个源超时。
const defaultTimeout = 8 * time.Second

// Checker 周期健康检查器。
type Checker struct {
	catalog  *catalog.Catalog
	interval time.Duration
	timeout  time.Duration

	mu      sync.Mutex
	cancels []context.CancelFunc
}

// New 创建健康检查器。
func New(cat *catalog.Catalog) *Checker {
	return &Checker{
		catalog:  cat,
		interval: defaultInterval,
		timeout:  defaultTimeout,
	}
}

// SetInterval / SetTimeout 调整间隔/超时（测试用）。
func (c *Checker) SetInterval(d time.Duration)  { c.interval = d }
func (c *Checker) SetTimeout(d time.Duration)  { c.timeout = d }

// Start 启动后台循环。返回的 cancel 用来停止。
func (c *Checker) Start(parent context.Context) (cancel context.CancelFunc) {
	ctx, cancelFn := context.WithCancel(parent)
	c.mu.Lock()
	c.cancels = append(c.cancels, cancelFn)
	c.mu.Unlock()
	go c.loop(ctx)
	return cancelFn
}

// StopAll 停掉所有循环。
func (c *Checker) StopAll() {
	c.mu.Lock()
	for _, fn := range c.cancels {
		fn()
	}
	c.cancels = nil
	c.mu.Unlock()
}

func (c *Checker) loop(ctx context.Context) {
	// 启动后立即跑一次，不等第一个 interval
	c.RunAll(ctx)
	t := time.NewTicker(c.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.RunAll(ctx)
		}
	}
}

// RunAll 对所有 enabled 源跑一次健康检查。供"立即检测"按钮调用。
func (c *Checker) RunAll(ctx context.Context) {
	if c.catalog == nil {
		return
	}
	sources, err := c.catalog.ListParseSources(ctx, true)
	if err != nil {
		log.Printf("[parsehealth] list sources: %v", err)
		return
	}
	log.Printf("[parsehealth] checking %d sources", len(sources))
	for _, s := range sources {
		select {
		case <-ctx.Done():
			return
		default:
		}
		c.checkOne(ctx, s)
	}
}

// checkOne 检查单个源。结果写回 DB。
func (c *Checker) checkOne(parent context.Context, s *catalog.ParseSource) {
	// 构造请求 URL（用 iqiyi 公共视频页做测试）
	template := s.ParseURL
	if template == "" {
		template = s.SearchURL
	}
	if template == "" {
		_ = c.catalog.SetParseSourceHealth(parent, s.ID, "fail",
			"no parse/search url", 0)
		return
	}
	testURL := template
	if contains(template, "{url}") {
		testURL = expandTemplate(template, map[string]string{"url": queryEscape(testVideoURL)})
	} else if contains(template, "{kw}") {
		testURL = expandTemplate(template, map[string]string{"kw": queryEscape("庆余年")})
	}

	// 限速：单个超时
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()

	start := time.Now()
	status, errMsg, httpStatus := c.probe(ctx, testURL)
	elapsed := time.Since(start).Milliseconds()

	if status == "" {
		status = "fail"
	}
	if errMsg == "" && status == "ok" {
		errMsg = fmt.Sprintf("HTTP %d", httpStatus)
	}
	if err := c.catalog.SetParseSourceHealth(parent, s.ID, status, errMsg, elapsed); err != nil {
		log.Printf("[parsehealth] save %s: %v", s.ID, err)
		return
	}
	log.Printf("[parsehealth] %-12s %-4s %4dms %s", s.ID, status, elapsed, errMsg)
}

// probe 发请求。返回 (status, errMsg, httpStatus)。
//   status = "ok" | "fail"
//   httpStatus = 服务端返回的 HTTP code（如果有）
func (c *Checker) probe(ctx context.Context, target string) (string, string, int) {
	// SSRF 防护：scheme 白名单 + IP 黑名单
	if err := safefetch.ValidateURL(target); err != nil {
		return "fail", err.Error(), 0
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "fail", "build request: " + err.Error(), 0
	}
	// 模拟主流 UA（很多源拦爬虫 UA）
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	// safefetch.Client 不跟随重定向，避免跳到内网
	resp, err := safefetch.Client.Do(req)
	if err != nil {
		// 区分 timeout 和连接失败
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "fail", "timeout", 0
		}
		return "fail", err.Error(), 0
	}
	defer resp.Body.Close()
	// 读掉 body（最多 4KB）避免连接不释放
	_, _ = io.CopyN(io.Discard, resp.Body, 4096)

	// 判定"健康"的标准：
	//   2xx 视为 ok；3xx 视为 ok（重定向也算通）；4xx 视为 fail（源返回 404/403）
	//   但有些源（iframe 源）会在 URL 不对时返回 200 + "请输入密码"，需要更智能判断
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return "ok", "", resp.StatusCode
	}
	return "fail", fmt.Sprintf("HTTP %d", resp.StatusCode), resp.StatusCode
}

// 简单 helper：避免再依赖一个 internal 工具包
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func expandTemplate(template string, values map[string]string) string {
	for k, v := range values {
		template = replaceAll(template, "{"+k+"}", v)
	}
	return template
}

func replaceAll(s, old, new string) string {
	out := ""
	for {
		i := indexOf(s, old)
		if i < 0 {
			return out + s
		}
		out += s[:i] + new
		s = s[i+len(old):]
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func queryEscape(s string) string { return url.QueryEscape(s) }
