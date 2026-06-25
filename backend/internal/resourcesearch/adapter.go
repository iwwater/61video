// Package resourcesearch 拉取 资源站（影视聚合）的标准 JSON API 并转换为
// 搜索结果项。
//
// 行业标准协议：GET {api_url}，{kw} 替换为 URL 编码后的关键词，期望返回：
//
//	{ "code": 1, "list": [
//	    { "vod_id": "12345", "vod_name": "间谍过家家",
//	      "vod_pic": "https://.../cover.jpg", "vod_remarks": "更新至12集",
//	      "vod_year": "2022", "vod_play_url": "第1集$https://xxx.m3u8#第2集$..." },
//	    ...
//	]}
//
// vod_play_url 形如 "第1集$url#第2集$url"，段间用 # 分隔，名称和 URL 间用 $。
// 我们默认取首段（playUrlMode=first），并通过 URL 后缀识别 m3u8/mp4 直链
// vs 详情页链接。
package resourcesearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/video-site/backend/internal/catalog"
	"github.com/video-site/backend/internal/safefetch"
)

// Result 单条资源站搜索结果。
type Result struct {
	SiteID     string `json:"siteId"`
	SiteName   string `json:"siteName"`
	VodID      string `json:"vodId"`
	Title      string `json:"title"`
	Cover      string `json:"cover,omitempty"`
	Year       string `json:"year,omitempty"`
	Remarks    string `json:"remarks,omitempty"`
	URL        string `json:"url"`              // 决定下游行为：m3u8/mp4 → 直链；其它 → 详情页
	DirectPlay bool   `json:"directPlay"`       // true=直链 m3u8/mp4；false=详情页（走 parse）
}

// resourceSiteResponse 资源站标准 JSON 响应（只解析我们需要的字段，其余忽略）。
type resourceSiteResponse struct {
	Code  int                `json:"code"`
	List  []resourceSiteItem `json:"list"`
	Total int                `json:"total,omitempty"`
}

type resourceSiteItem struct {
	// 部分资源站 vod_id 是数字（如 84270），部分是字符串。用 json.Number 接住两者，
	// 解析时再统一转 string。
	VodID      json.Number `json:"vod_id"`
	VodName    string      `json:"vod_name"`
	VodPic     string      `json:"vod_pic"`
	VodRemarks string      `json:"vod_remarks"`
	VodYear    string      `json:"vod_year"`
	VodPlayURL string      `json:"vod_play_url"`
}

// FetchOptions 控制 fetch 行为。
type FetchOptions struct {
	PerSiteTimeout time.Duration
	MaxConcurrency int
}

// DefaultFetchOptions 默认并发与超时。
func DefaultFetchOptions() FetchOptions {
	return FetchOptions{
		PerSiteTimeout: 6 * time.Second,
		MaxConcurrency: 6,
	}
}

// Search 对所有 enabled 资源站并发执行搜索，聚合结果。失败的源被忽略（不阻断其他源）。
func Search(ctx context.Context, sites []*catalog.ResourceSite, keyword string, opts FetchOptions) []Result {
	if len(sites) == 0 || strings.TrimSpace(keyword) == "" {
		return nil
	}
	if opts.PerSiteTimeout <= 0 {
		opts.PerSiteTimeout = 6 * time.Second
	}
	if opts.MaxConcurrency <= 0 {
		opts.MaxConcurrency = 6
	}
	type pair struct {
		site *catalog.ResourceSite
		rs   []Result
		err  error
	}
	work := make(chan *catalog.ResourceSite)
	out := make(chan pair, len(sites))
	var wg sync.WaitGroup
	for i := 0; i < opts.MaxConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range work {
				rs, err := fetchOne(ctx, s, keyword, opts.PerSiteTimeout)
				out <- pair{site: s, rs: rs, err: err}
			}
		}()
	}
	go func() {
		for _, s := range sites {
			select {
			case <-ctx.Done():
				close(work)
				wg.Wait()
				close(out)
				return
			case work <- s:
			}
		}
		close(work)
		wg.Wait()
		close(out)
	}()

	var all []Result
	for p := range out {
		if p.err != nil {
			log.Printf("[resourcesearch] %s: %v", p.site.ID, p.err)
			continue
		}
		all = append(all, p.rs...)
	}
	return all
}

func fetchOne(parent context.Context, s *catalog.ResourceSite, keyword string, timeout time.Duration) ([]Result, error) {
	apiURL := s.ExpandAPIURL(keyword)
	if err := safefetch.ValidateURL(apiURL); err != nil {
		return nil, fmt.Errorf("safefetch: %w", err)
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	req, err := newRequest(ctx, apiURL)
	if err != nil {
		return nil, err
	}
	resp, err := safefetch.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, string(body))
	}
	// 资源站 JSON 通常 < 2 MiB；上限 4 MiB 保险
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var parsed resourceSiteResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode json: %w", err)
	}
	if parsed.Code != 0 && parsed.Code != 1 {
		// 大多数资源站 code=1 成功；code=0 也可能成功（自定义约定）。仅当明显错误时报错。
		if parsed.Code >= 400 {
			return nil, fmt.Errorf("resource site returned code %d", parsed.Code)
		}
	}
	out := make([]Result, 0, len(parsed.List))
	for _, it := range parsed.List {
		title := strings.TrimSpace(it.VodName)
		if title == "" {
			continue
		}
		vodID := strings.TrimSpace(it.VodID.String())
		if vodID == "" {
			// 没有 vod_id 就没法后续拉详情，跳过
			continue
		}
		playURL := pickPlayURL(it.VodPlayURL, s.PlayURLMode)
		// 列表里 vod_play_url 通常为空（行业惯例，详情接口才给完整 m3u8）。
		// 这里 **不** 过滤空 playURL 的项——保留为 "需要点详情" 的候选，让前端
		// 调 /api/anime/resource/detail 拿真实播放地址。
		direct := playURL != "" && looksLikeDirectMedia(playURL)
		out = append(out, Result{
			SiteID:     s.ID,
			SiteName:   s.Name,
			VodID:      vodID,
			Title:      title,
			Cover:      strings.TrimSpace(it.VodPic),
			Year:       strings.TrimSpace(it.VodYear),
			Remarks:    strings.TrimSpace(it.VodRemarks),
			URL:        playURL, // 空 = 需要走详情
			DirectPlay: direct,
		})
	}
	return out, nil
}

// newRequest 构造带常规浏览器头的 GET 请求。
func newRequest(ctx context.Context, apiURL string) (*http.Request, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	r.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	r.Header.Set("Accept", "application/json,text/plain,*/*")
	r.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	return r, nil
}

// pickPlayURL 根据 playUrlMode 选取 vod_play_url 中的一段 URL。
//
//   - "first"   取首段（默认）
//   - "direct"  总是返回 vod_play_url 整体（视为直链拼接）
//   - "detail"  把整段当作详情页 URL；调用方应按 detail 处理
func pickPlayURL(raw, mode string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if mode == "detail" {
		return raw
	}

	// 行业里 vod_play_url 的格式：
	//   "name1$url1#name2$url2$$$name3$url3#name4$url4"
	// 不同资源站含义不同：
	//   - jszy:   # 分隔多集；$ 分隔名称和 URL
	//   - cj-lzi: $$$ 分隔多播放源（每个源内部 # 分集）；每段的第一段 URL 是
	//             分享 token（会 404 给非浏览器抓取），后面跟一段真 m3u8
	//
	// 策略：枚举所有 "$" 段（按顺序），优先返回第一个 .m3u8/.mp4 之类的媒体
	// URL；都没有时退回到第一个 $ 段（兼容 jszy 这种直接把第一集 URL 放在
	// 第一个 $ 段的格式）。
	if strings.Contains(raw, "$") {
		// 全部 $ 段（含首尾分隔符），逐个尝试
		parts := strings.Split(raw, "$")
		// parts[0] = 集名前缀（可能为空），parts[1] = 第一段 URL，parts[2] = 集名, parts[3] = URL...
		// 收集所有看起来像 URL 的段（scheme:// 开头）
		var urls []string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			// 去尾 # 分割
			if i := strings.Index(p, "#"); i >= 0 {
				p = p[:i]
			}
			if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
				urls = append(urls, p)
			}
		}
		// 优先选看起来是媒体直链的
		for _, u := range urls {
			if looksLikeDirectMedia(u) {
				return u
			}
		}
		// 否则返回第一个 URL（可能是详情页，由前端走 universal 提取 m3u8）
		if len(urls) > 0 {
			return urls[0]
		}
	}
	// 整段没分隔符时整段返回
	return raw
}

// looksLikeDirectMedia 判断 URL 是否是直链视频（m3u8/mp4/mp3/m4a/...）。
func looksLikeDirectMedia(raw string) bool {
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "data:") || strings.HasPrefix(lower, "blob:") {
		return false
	}
	if u, err := url.Parse(raw); err == nil && u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	for _, ext := range []string{".m3u8", ".mp4", ".m4a", ".mp3", ".flv", ".ts", ".webm", ".mov", ".mkv", ".aac", ".ogg", ".opus"} {
		if strings.Contains(lower, ext) {
			return true
		}
	}
	return false
}

// DetailResult 单条详情结果。
type DetailResult struct {
	SiteID     string `json:"siteId"`
	SiteName   string `json:"siteName"`
	Title      string `json:"title"`
	Cover      string `json:"cover,omitempty"`
	PlayURL    string `json:"playUrl"`
	DirectPlay bool   `json:"directPlay"`
}

// FetchDetail 拉取单个资源的详情（拿到 play URL）。
// 用于点击搜索结果后获取可播放链接。详情接口通常形如：
//
//	GET <base>?ac=detail&ids={vod_id}
//
// 即将原 {api_url} 里的 `?ac=list&wd={kw}` 这段 query 整体替换成 `?ac=detail&ids={vod_id}`。
// 用 url.Parse 处理而不是字符串替换，避免 URL 拼接出错。
func FetchDetail(parent context.Context, s *catalog.ResourceSite, vodID string, timeout time.Duration) (*DetailResult, error) {
	if s == nil || vodID == "" {
		return nil, errors.New("site or vod id is required")
	}
	detailURL, err := buildDetailURL(s.APIURL, vodID)
	if err != nil {
		return nil, err
	}
	if err := safefetch.ValidateURL(detailURL); err != nil {
		return nil, fmt.Errorf("safefetch: %w", err)
	}
	if timeout <= 0 {
		timeout = 6 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	req, err := newRequest(ctx, detailURL)
	if err != nil {
		return nil, err
	}
	resp, err := safefetch.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, string(body))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var parsed resourceSiteResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode json: %w", err)
	}
	if len(parsed.List) == 0 {
		return nil, errors.New("detail not found")
	}
	it := parsed.List[0]
	playURL := pickPlayURL(it.VodPlayURL, s.PlayURLMode)
	if playURL == "" {
		return nil, errors.New("no playable url in detail response")
	}
	return &DetailResult{
		SiteID:     s.ID,
		SiteName:   s.Name,
		Title:      strings.TrimSpace(it.VodName),
		Cover:      strings.TrimSpace(it.VodPic),
		PlayURL:    playURL,
		DirectPlay: looksLikeDirectMedia(playURL),
	}, nil
}

// buildDetailURL 把 search URL（?ac=list&wd={kw}）转成 detail URL（?ac=detail&ids={vod_id}）。
//
// 用 url.Parse + 改 query 而不是字符串替换：
//   - 防止 ?ac=list&wd=ac=detail&ids=xxx 这种粘接错误
//   - 防止用户配的是 {kw} 形式（无其他参数）时被错误处理
func buildDetailURL(apiURLTemplate, vodID string) (string, error) {
	// 先把 {kw} 替换成一个安全的占位符（仅用于 url.Parse 解析 query）
	placeholder := "kwplaceholder"
	parsed := strings.Replace(apiURLTemplate, "{kw}", placeholder, 1)
	u, err := url.Parse(parsed)
	if err != nil {
		return "", fmt.Errorf("parse api url: %w", err)
	}
	q := u.Query()
	// 删除 ac / wd / ids 等参数，重置成详情参数
	q.Del("ac")
	q.Del("wd")
	q.Del("ids")
	q.Del("pg")
	q.Set("ac", "detail")
	q.Set("ids", vodID)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// ErrNoResults 没有结果。
var ErrNoResults = errors.New("no results from resource sites")
