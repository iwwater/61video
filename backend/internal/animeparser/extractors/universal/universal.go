// Package universal 提供 animeparser 的兜底解析器。
//
// 借鉴 lux/annie 的 extractors/universal：当没有特定站点的 extractor 命中时，
// 通用解析器拉取 HTML 并从中抽取 <video> / <source> 直链、常见 iframe 嵌入
// 链接、og:title、og:image 等元信息。
package universal

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

// Parser 通用 HTML 解析器。
type Parser struct{}

// Name 实现 animeparser.Parser。
func (p *Parser) Name() string { return "universal" }

// Match 永远返回 true —— 它是兜底解析器，调用方按注册顺序只在没特定解析器
// 命中时才用它。
func (p *Parser) Match(url string) bool { return true }

// Extract 抓取页面并尝试抽取 <video>/<source>/iframe 中的视频源。
func (p *Parser) Extract(ctx context.Context, target string) (*animeparser.ParseResult, error) {
	// SSRF 防护：scheme 白名单 + 私网/回环/云元数据 IP 黑名单
	if err := safefetch.ValidateURL(target); err != nil {
		return nil, fmt.Errorf("safefetch: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	animeparser.SetDefaultHeaders(req)
	// Referer 设为目标 URL 的 origin（不是完整 URL，只到 host 根）。
	// 不少 CDN/防盗链（如 jszy 类的资源站播放页）会校验 Referer 是同源
	// 或父域；空 Referer 会被 403。
	if u, err := netUrl.Parse(target); err == nil && u.Scheme != "" && u.Host != "" {
		req.Header.Set("Referer", u.Scheme+"://"+u.Host+"/")
	}

	// 使用 safefetch.Client：拒绝跟随重定向，避免跳到内网
	resp, err := safefetch.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New(http.StatusText(resp.StatusCode))
	}
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20)) // 上限 8 MiB
	if err != nil {
		return nil, err
	}
	html := string(bodyBytes)

	videoURLs := collectVideoURLs(html)
	if len(videoURLs) == 0 {
		// 尝试从常见 iframe 嵌入 URL 找 .m3u8 / .mp4
		for _, u := range collectIframeSources(html) {
			videoURLs = append(videoURLs, u)
		}
	}
	if len(videoURLs) == 0 {
		return nil, errors.New("no <video>/<source>/iframe 视频源被找到")
	}

	res := &animeparser.ParseResult{
		Title:     extractTitle(html),
		Thumbnail: extractMeta(html, "og:image"),
		VideoURL:  videoURLs[0],
		VideoURLs: videoURLs,
	}
	return res, nil
}

// ---------- 正则 / 抽取工具 ----------

var (
	reVideoSrc   = regexp.MustCompile(`(?is)<video[^>]*?\bsrc\s*=\s*["']([^"']+)["']`)
	reSourceSrc  = regexp.MustCompile(`(?is)<source[^>]*?\bsrc\s*=\s*["']([^"']+)["']`)
	reSourceType = regexp.MustCompile(`(?is)<source[^>]*?\btype\s*=\s*["']video/[^"']+["'][^>]*?\bsrc\s*=\s*["']([^"']+)["']`)
	reIframeSrc  = regexp.MustCompile(`(?is)<iframe[^>]*?\bsrc\s*=\s*["']([^"']+)["']`)
	reTitle      = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	reMeta       = regexp.MustCompile(`(?is)<meta[^>]*?\bproperty\s*=\s*["']([^"']+)["'][^>]*?\bcontent\s*=\s*["']([^"']+)["']`)
	// 动态播放器（DPlayer / videojs / ckplayer 等）把 m3u8 写在 JS 字符串里，
	// 不是 <video> 属性。例如 DPlayer：
	//   new DPlayer({ video: { url: '...m3u8', type: 'hls' } })
	// 这里宽松匹配任何引号/反引号包起来的 http(s) URL，endswith 媒体后缀。
	reLooseMedia = regexp.MustCompile(`(?i)(https?://[^\s"'<>\\)]+?\.(?:m3u8|mp4|flv|webm|mov|ts|m4a|mp3))`)
	// 排除同源静态资源（图标/CSS/JS 文件夹路径）的反例通过 looksLikeVideoURL 完成。
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
	// 1) 显式 <video>/<source> 标签（最高优先级，最可信）
	for _, m := range reSourceType.FindAllStringSubmatch(html, -1) {
		add(m[1])
	}
	for _, m := range reSourceSrc.FindAllStringSubmatch(html, -1) {
		add(m[1])
	}
	for _, m := range reVideoSrc.FindAllStringSubmatch(html, -1) {
		add(m[1])
	}
	// 2) 动态播放器（DPlayer/videojs 等）写在 JS 字符串里的 m3u8/mp4 URL
	//    兜底：抓全文里所有"看起来是媒体 URL"且不在上面已收集集合中的
	if len(out) == 0 {
		for _, m := range reLooseMedia.FindAllStringSubmatch(html, -1) {
			add(m[1])
		}
	}
	return out
}

func collectIframeSources(html string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, m := range reIframeSrc.FindAllStringSubmatch(html, -1) {
		u := strings.TrimSpace(m[1])
		if u == "" {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
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
	if m := reTitle.FindStringSubmatch(html); len(m) >= 2 {
		return strings.TrimSpace(stripTags(m[1]))
	}
	if v := extractMeta(html, "og:title"); v != "" {
		return v
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
