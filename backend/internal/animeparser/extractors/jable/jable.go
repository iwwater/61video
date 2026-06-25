// Package jable 提供 jable.tv 视频链接解析。
//
// URL 形式：
//   - https://jable.tv/videos/xxx/
//   - https://jable.tv/xxx/
//
// 流程：
//  1. 拉取页面 HTML
//  2. 从 <source src="...m3u8"> / data-src / hls playlist URL 抽出视频源
//  3. 标题用 og:title，缩略图用 og:image
//
// 视频源 URL 走的是 Jable 的 hls CDN，浏览器直连通常需要 Referer=https://jable.tv
// 才能拿到 200；我们把 Referer 放进 ParseResult.Headers。
package jable

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	netUrl "net/url"
	"regexp"
	"strings"

	"github.com/video-site/backend/internal/animeparser"
	"github.com/video-site/backend/internal/safefetch"
)

func init() {
	animeparser.Register(&Parser{})
}

// Parser jable.tv extractor。
type Parser struct{}

// Name 实现 animeparser.Parser。
func (p *Parser) Name() string { return "jable" }

// Match 判断是否是 jable.tv 链接。
func (p *Parser) Match(rawURL string) bool {
	u := strings.ToLower(strings.TrimSpace(rawURL))
	if u == "" {
		return false
	}
	// 解析 host，host 等于 jable.tv / www.jable.tv 才算 jable 链接，
	// 避免 `example.com/jable.tv/` 这种裸路径被误判。
	if parsed, err := netUrl.Parse(u); err == nil {
		host := strings.ToLower(parsed.Hostname())
		if host == "jable.tv" || host == "www.jable.tv" {
			return true
		}
	}
	// 兜底：不是 URL 或解析失败时只看 host 段（jable.tv 后面跟 /）
	return jableHostPrefixRegex.MatchString(u)
}

var jableHostPrefixRegex = regexp.MustCompile(`(^|://)jable\.tv/`)

// Extract 实现 animeparser.Parser。
func (p *Parser) Extract(ctx context.Context, rawURL string) (*animeparser.ParseResult, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, errors.New("empty url")
	}

	// SSRF 防护：scheme 白名单 + 私网/回环 IP 黑名单
	if err := validateURL(rawURL); err != nil {
		return nil, fmt.Errorf("safefetch: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	animeparser.SetDefaultHeaders(req)
	// Jable CDN 强制校验 Referer 同源
	req.Header.Set("Referer", "https://jable.tv/")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New(http.StatusText(resp.StatusCode))
	}
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20)) // 8 MiB
	if err != nil {
		return nil, err
	}
	html := string(bodyBytes)

	videoURLs := collectVideoURLs(html)
	if len(videoURLs) == 0 {
		return nil, errors.New("no video source found in jable page")
	}

	res := &animeparser.ParseResult{
		Title:     extractTitle(html),
		Thumbnail: extractMeta(html, "og:image"),
		VideoURL:  videoURLs[0],
		VideoURLs: videoURLs,
		Headers: map[string]string{
			"Referer":    "https://jable.tv/",
			"User-Agent": animeparser.DefaultUserAgent,
		},
	}
	return res, nil
}

// ---------- 测试钩子（生产 = safefetch；测试 = 注入 httptest server） ----------

var (
	httpClient   = safefetch.Client
	validateURL  = safefetch.ValidateURL
)

// setHTTPClient 注入自定义 client（仅测试用）。nil 表示恢复默认。
func setHTTPClient(c *http.Client) {
	if c == nil {
		httpClient = safefetch.Client
		return
	}
	httpClient = c
}

// setValidateURL 替换 URL 校验函数（仅测试用）。nil 表示恢复默认。
func setValidateURL(fn func(string) error) {
	if fn == nil {
		validateURL = safefetch.ValidateURL
		return
	}
	validateURL = fn
}

// ---------- 正则 / 抽取工具 ----------

var (
	// Jable 页面里视频源典型形态：
	//   <source src="https://...m3u8" type="application/x-mpegURL">
	//   <source src="https://...mp4"  type="video/mp4">
	//   <video data-src="https://...m3u8">
	//   <video src="https://...m3u8">
	// 一些版本也会把 m3u8 放在 hls.js / videojs 初始化的 JS 字符串里。
	reSourceType = regexp.MustCompile(`(?is)<source[^>]*?\btype\s*=\s*["'][^"']*["'][^>]*?\bsrc\s*=\s*["']([^"']+)["']`)
	reSourceSrc  = regexp.MustCompile(`(?is)<source[^>]*?\bsrc\s*=\s*["']([^"']+)["']`)
	reVideoData  = regexp.MustCompile(`(?is)<video[^>]*?\bdata-src\s*=\s*["']([^"']+)["']`)
	reVideoSrc   = regexp.MustCompile(`(?is)<video[^>]*?\bsrc\s*=\s*["']([^"']+)["']`)
	reTitle      = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	reMeta       = regexp.MustCompile(`(?is)<meta[^>]*?\bproperty\s*=\s*["']([^"']+)["'][^>]*?\bcontent\s*=\s*["']([^"']+)["']`)
	reLooseMedia = regexp.MustCompile(`(?i)(https?://[^\s"'<>\\)]+?\.(?:m3u8|mp4|flv|webm|mov|ts))`)
)

func collectVideoURLs(html string) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(u string) {
		u = strings.TrimSpace(u)
		if u == "" {
			return
		}
		if !looksLikeVideoURL(u) {
			return
		}
		if _, ok := seen[u]; ok {
			return
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	// 1) 显式标签（最可信）
	for _, m := range reSourceType.FindAllStringSubmatch(html, -1) {
		add(m[1])
	}
	for _, m := range reSourceSrc.FindAllStringSubmatch(html, -1) {
		add(m[1])
	}
	for _, m := range reVideoData.FindAllStringSubmatch(html, -1) {
		add(m[1])
	}
	for _, m := range reVideoSrc.FindAllStringSubmatch(html, -1) {
		add(m[1])
	}
	// 2) 兜底：从 JS 字符串里抓 .m3u8 / .mp4
	if len(out) == 0 {
		for _, m := range reLooseMedia.FindAllStringSubmatch(html, -1) {
			add(m[1])
		}
	}
	return out
}

func looksLikeVideoURL(u string) bool {
	lu := strings.ToLower(u)
	if strings.HasPrefix(lu, "data:") || strings.HasPrefix(lu, "blob:") || strings.HasPrefix(lu, "javascript:") {
		return false
	}
	return strings.Contains(lu, ".m3u8") ||
		strings.Contains(lu, ".mp4") ||
		strings.Contains(lu, ".flv") ||
		strings.Contains(lu, ".webm") ||
		strings.Contains(lu, ".mov") ||
		strings.Contains(lu, ".ts")
}

func extractTitle(html string) string {
	if v := extractMeta(html, "og:title"); v != "" {
		return v
	}
	if m := reTitle.FindStringSubmatch(html); len(m) >= 2 {
		return strings.TrimSpace(stripTags(m[1]))
	}
	return ""
}

func extractMeta(html, property string) string {
	for _, m := range reMeta.FindAllStringSubmatch(html, -1) {
		if strings.EqualFold(m[1], property) {
			return strings.TrimSpace(m[2])
		}
	}
	return ""
}

var reTag = regexp.MustCompile(`<[^>]+>`)

func stripTags(s string) string { return reTag.ReplaceAllString(s, "") }

// Compile-time interface check.
var _ animeparser.Parser = (*Parser)(nil)

// resolveBaseURL 把裸路径拼成完整 URL（仅测试用）。
func resolveBaseURL(base, ref string) string {
	b, err := netUrl.Parse(base)
	if err != nil {
		return ref
	}
	r, err := netUrl.Parse(ref)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}
