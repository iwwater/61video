// Package animeparser 提供动漫视频页面链接解析能力。
//
// 借鉴 lux/annie 的 extractor 注册模式：
//   - 各站点实现 Parser 接口
//   - 通过 init() 在自己的包内调用 Register() 自注册
//   - 调用方用 Parse(ctx, url) 让注册中心按 URL 自动派发到合适的解析器
//   - 没有匹配的特定解析器时，回退到 universal 全能解析器
package animeparser

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ParseResult 解析结果，包含播放所需的全部信息。
type ParseResult struct {
	Title     string            `json:"title"`
	VideoURL  string            `json:"videoUrl"`            // 主视频直链（mp4 / m3u8）
	VideoURLs []string          `json:"videoUrls,omitempty"` // 多分集时返回所有集
	Thumbnail string            `json:"thumbnail,omitempty"`
	Duration  int               `json:"duration,omitempty"` // 秒
	Source    string            `json:"source"`              // 解析器名
	Headers   map[string]string `json:"headers,omitempty"`   // 拉视频需要的请求头
}

// Parser 单个站点的视频链接解析器。
//
// 借鉴 lux/annie extractors/ 的接口形态：
//   - Name() 返回解析器名（如 "bilibili" / "universal"）
//   - Match() 判断该解析器是否处理给定 URL
//   - Extract() 真正抓取并解析
type Parser interface {
	Name() string
	Match(url string) bool
	Extract(ctx context.Context, url string) (*ParseResult, error)
}

// HTTPClient 解析器使用的 HTTP 客户端。允许注入以便测试。
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// ---------- 注册中心 ----------

var (
	registryMu sync.RWMutex
	registry   = make(map[string]Parser)
)

// Register 注册一个解析器。同名后者覆盖前者；通常在 init() 中调用。
func Register(p Parser) {
	if p == nil || p.Name() == "" {
		return
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[p.Name()] = p
}

// UnregisterAll 主要给测试用：清空所有已注册的解析器。
func UnregisterAll() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = make(map[string]Parser)
}

// List 返回当前所有已注册解析器的名字（按注册顺序，含 "universal" 在末尾）。
func List() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	return out
}

// Parse 根据 URL 自动选择最合适的解析器。
//
// 优先按注册顺序逐个调用 Match()，命中即用其解析；
// 所有特定解析器都不命中时，回退到名为 "universal" 的解析器。
// 如果连 universal 也没注册，则返回 ErrNoParser。
func Parse(ctx context.Context, url string) (*ParseResult, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, errors.New("url is required")
	}

	registryMu.RLock()
	candidates := make([]Parser, 0, len(registry))
	for _, p := range registry {
		candidates = append(candidates, p)
	}
	registryMu.RUnlock()

	// 先匹配特定解析器
	for _, p := range candidates {
		if p.Name() == "universal" {
			continue
		}
		if p.Match(url) {
			return runParser(ctx, p, url)
		}
	}

	// 回退到 universal
	for _, p := range candidates {
		if p.Name() == "universal" {
			return runParser(ctx, p, url)
		}
	}

	return nil, ErrNoParser
}

// ErrNoParser 没有可用的解析器（含 universal 未注册）。
var ErrNoParser = errors.New("no parser registered (universal fallback missing)")

func runParser(ctx context.Context, p Parser, url string) (*ParseResult, error) {
	res, err := p.Extract(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("%s parser: %w", p.Name(), err)
	}
	if res == nil {
		return nil, fmt.Errorf("%s parser: nil result", p.Name())
	}
	res.Source = p.Name()
	if res.Headers == nil {
		res.Headers = map[string]string{}
	}
	return res, nil
}

// ---------- 共享 HTTP 工具 ----------

// NewDefaultClient 返回一个带 12s 超时的默认 HTTP 客户端。
func NewDefaultClient() HTTPClient {
	return &http.Client{Timeout: 12 * time.Second}
}

// DefaultUserAgent 解析视频时的默认 UA，伪装常见浏览器。
const DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// SetDefaultHeaders 给 req 套上常规请求头。
func SetDefaultHeaders(req *http.Request) {
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", DefaultUserAgent)
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	}
	if req.Header.Get("Accept-Language") == "" {
		req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	}
}
