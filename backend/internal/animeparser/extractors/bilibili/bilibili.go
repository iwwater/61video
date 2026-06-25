// Package bilibili 提供 B 站视频链接解析。
//
// 支持的 URL 形式：
//   - https://www.bilibili.com/video/BVxxxxxxxxxx  （BV 号）
//   - https://www.bilibili.com/video/av12345       （AV 号，已不推荐但兼容）
//   - https://b23.tv/xxxxxxx                       （短链，会先 resolve 到完整 URL）
//   - https://www.bilibili.com/bangumi/play/epxxx   （番剧）
//
// 流程：
//  1. 从 URL 提取 BV/AV 号（或 resolve 短链）
//  2. 调用 web-interface/view 拿到 cid / title / pic / duration
//  3. 调用 player/playurl 拿视频直链（默认 80=1080P 高码率，能拿到就拿，拿不到降级）
//
// 注意：B 站 dash 直链通常需要 Referer=https://www.bilibili.com 才能播放，
// 我们把 Referer 放进 ParseResult.Headers 让前端播放时带上。
// 已登录用户如果在配置里加了 SESSDATA cookie（暂未提供入口），可以拿到更高清晰度。
package bilibili

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/video-site/backend/internal/animeparser"
	"github.com/video-site/backend/internal/safefetch"
)

func init() {
	animeparser.Register(&Parser{})
}

// Parser B 站 extractor。
type Parser struct{}

// Name 实现 animeparser.Parser。
func (p *Parser) Name() string { return "bilibili" }

// Match 判断是否是 B 站链接。
func (p *Parser) Match(rawURL string) bool {
	u := strings.ToLower(strings.TrimSpace(rawURL))
	if u == "" {
		return false
	}
	return strings.Contains(u, "bilibili.com/video/") ||
		strings.Contains(u, "bilibili.com/bangumi/play/") ||
		strings.Contains(u, "b23.tv/")
}

var (
	bvRegex = regexp.MustCompile(`(?i)/video/([Bb][Vv][0-9A-Za-z]+)`)
	avRegex = regexp.MustCompile(`(?i)/video/[Aa][Vv](\d+)`)
	epRegex = regexp.MustCompile(`(?i)/bangumi/play/(ep\d+)`)
	b23Regex = regexp.MustCompile(`(?i)b23\.tv/([0-9A-Za-z]+)`)
)

// Extract 实现 animeparser.Parser。
func (p *Parser) Extract(ctx context.Context, rawURL string) (*animeparser.ParseResult, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, errors.New("empty url")
	}

	// SSRF 防护：scheme 白名单 + 私网 IP 黑名单
	if err := safefetch.ValidateURL(rawURL); err != nil {
		return nil, fmt.Errorf("safefetch: %w", err)
	}

	bvid, aid, err := p.resolveID(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	view, err := p.fetchView(ctx, bvid, aid)
	if err != nil {
		return nil, fmt.Errorf("view: %w", err)
	}

	playURL, duration, err := p.fetchPlayURL(ctx, bvid, aid, view.CID)
	if err != nil {
		return nil, fmt.Errorf("playurl: %w", err)
	}
	if duration > 0 {
		view.Duration = duration
	}

	headers := map[string]string{
		"Referer":    "https://www.bilibili.com",
		"User-Agent": animeparser.DefaultUserAgent,
	}

	return &animeparser.ParseResult{
		Title:     view.Title,
		VideoURL:  playURL,
		Thumbnail: view.Pic,
		Duration:  view.Duration,
		Source:    "bilibili",
		Headers:   headers,
	}, nil
}

// resolveID 从 URL 提取 BV / AV 号；短链先展开。
func (p *Parser) resolveID(ctx context.Context, rawURL string) (bvid string, aid int64, err error) {
	if m := b23Regex.FindStringSubmatch(rawURL); len(m) == 2 {
		expanded, err := p.expandShortLink(ctx, rawURL)
		if err != nil {
			return "", 0, fmt.Errorf("expand short link: %w", err)
		}
		rawURL = expanded
	}

	if m := bvRegex.FindStringSubmatch(rawURL); len(m) == 2 {
		bvid = strings.ToUpper(strings.TrimPrefix(m[1], "Bv"))
		bvid = "BV" + bvid[2:] // 统一为 BV1xxx...
	}
	if m := avRegex.FindStringSubmatch(rawURL); len(m) == 2 {
		n, perr := strconv.ParseInt(m[1], 10, 64)
		if perr == nil {
			aid = n
		}
	}
	if bvid == "" && aid == 0 {
		// 番剧 URL：暂不支持
		if epRegex.MatchString(rawURL) {
			return "", 0, errors.New("bangumi play url is not supported yet")
		}
		return "", 0, errors.New("无法从 URL 中识别 BV / AV 号")
	}
	return bvid, aid, nil
}

// expandShortLink 跟踪 b23.tv 短链重定向。safefetch.Client 默认不跟 redirect，所以手动处理。
// 入参已经在 extract() 入口做过 safefetch.ValidateURL 校验，这里直接发请求不再二次校验，
// 否则单元测试里 httptest.NewServer 的 127.0.0.1 会被当作 loopback 拦掉。
func (p *Parser) expandShortLink(ctx context.Context, shortURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, shortURL, nil)
	if err != nil {
		return "", err
	}
	animeparser.SetDefaultHeaders(req)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // 不自动跟 redirect
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))

	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", fmt.Errorf("no Location header (status=%d)", resp.StatusCode)
	}
	if !strings.HasPrefix(strings.ToLower(loc), "http") {
		base, err := url.Parse(shortURL)
		if err != nil {
			return "", err
		}
		ref, err := url.Parse(loc)
		if err != nil {
			return "", err
		}
		loc = base.ResolveReference(ref).String()
	}
	return loc, nil
}

// viewData web-interface/view 接口的 data 字段。
type viewData struct {
	BVID     string `json:"bvid"`
	AID      int64  `json:"aid"`
	CID      int64  `json:"cid"`
	Title    string `json:"title"`
	Pic      string `json:"pic"`
	Duration int    `json:"duration"` // 秒
}

type viewResp struct {
	Code    int       `json:"code"`
	Message string    `json:"message"`
	Data    viewData  `json:"data"`
}

func (p *Parser) fetchView(ctx context.Context, bvid string, aid int64) (*viewData, error) {
	apiURL := "https://api.bilibili.com/x/web-interface/view"
	q := url.Values{}
	if bvid != "" {
		q.Set("bvid", bvid)
	} else {
		q.Set("aid", strconv.FormatInt(aid, 10))
	}
	apiURL += "?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	animeparser.SetDefaultHeaders(req)
	req.Header.Set("Referer", "https://www.bilibili.com")

	resp, err := safefetch.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}

	var view viewResp
	if err := json.Unmarshal(bodyBytes, &view); err != nil {
		return nil, fmt.Errorf("decode view: %w", err)
	}
	if view.Code != 0 {
		return nil, fmt.Errorf("view api code=%d msg=%s", view.Code, view.Message)
	}
	if view.Data.CID == 0 {
		return nil, errors.New("view api: cid is empty")
	}
	return &view.Data, nil
}

// playURLResp player/playurl 接口的部分字段。
type playURLResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Duration int `json:"timelength"` // 毫秒
		DURL    []struct {
			URL string `json:"url"`
		} `json:"durl"`
		DASH struct {
			Video []struct {
				BaseURL string `json:"baseUrl"`
			} `json:"video"`
		} `json:"dash"`
	} `json:"data"`
}

func (p *Parser) fetchPlayURL(ctx context.Context, bvid string, aid int64, cid int64) (string, int, error) {
	// 优先尝试 mp4 (qn=80=1080P, fnval=1=mp4)；拿不到再试 dash (fnval=16)
	for _, fnval := range []string{"1", "16"} {
		apiURL := "https://api.bilibili.com/x/player/playurl"
		q := url.Values{}
		if bvid != "" {
			q.Set("bvid", bvid)
		} else {
			q.Set("aid", strconv.FormatInt(aid, 10))
		}
		q.Set("cid", strconv.FormatInt(cid, 10))
		q.Set("qn", "80")
		q.Set("fnval", fnval)
		q.Set("fnver", "0")
		q.Set("fourk", "1")
		q.Set("platform", "html5")
		apiURL += "?" + q.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
		if err != nil {
			continue
		}
		animeparser.SetDefaultHeaders(req)
		req.Header.Set("Referer", "https://www.bilibili.com")

		resp, err := safefetch.Client.Do(req)
		if err != nil {
			continue
		}
		bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		resp.Body.Close()
		if err != nil {
			continue
		}

		var pl playURLResp
		if err := json.Unmarshal(bodyBytes, &pl); err != nil {
			continue
		}
		if pl.Code != 0 {
			continue
		}

		// mp4 模式：durl[0].url
		if len(pl.Data.DURL) > 0 && pl.Data.DURL[0].URL != "" {
			durationSec := pl.Data.Duration / 1000
			return pl.Data.DURL[0].URL, durationSec, nil
		}
		// dash 模式：dash.video[0].baseUrl
		if len(pl.Data.DASH.Video) > 0 && pl.Data.DASH.Video[0].BaseURL != "" {
			durationSec := pl.Data.Duration / 1000
			return pl.Data.DASH.Video[0].BaseURL, durationSec, nil
		}
	}

	return "", 0, errors.New("playurl: no video url found (login required for higher quality?)")
}
