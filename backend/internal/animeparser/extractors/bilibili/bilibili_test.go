package bilibili

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestParserName(t *testing.T) {
	if got := (&Parser{}).Name(); got != "bilibili" {
		t.Fatalf("Name = %q, want bilibili", got)
	}
}

func TestMatch(t *testing.T) {
	p := &Parser{}
	cases := []struct {
		url  string
		want bool
	}{
		{"https://www.bilibili.com/video/BV1xx411c7mD", true},
		{"https://www.bilibili.com/video/av170001", true},
		{"https://www.bilibili.com/video/bv1xx411c7md", true},
		{"https://b23.tv/abcdefg", true},
		{"https://www.bilibili.com/bangumi/play/ep12345", true},
		{"https://example.com/something", false},
		{"https://www.youtube.com/watch?v=xxx", false},
		{"", false},
	}
	for _, c := range cases {
		if got := p.Match(c.url); got != c.want {
			t.Errorf("Match(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}

func TestExtractSSRFGuard(t *testing.T) {
	p := &Parser{}
	// 私网 IP 应被 safefetch 拦掉
	_, err := p.Extract(context.Background(), "http://127.0.0.1/video/BV123")
	if err == nil || !strings.Contains(err.Error(), "safefetch") {
		t.Fatalf("expected safefetch error, got %v", err)
	}
}

func TestExpandShortLink(t *testing.T) {
	// 模拟 b23.tv 重定向到完整 bilibili 链接
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://www.bilibili.com/video/BV1xx411c7mD")
		w.WriteHeader(http.StatusFound)
	}))
	defer upstream.Close()

	p := &Parser{}
	got, err := p.expandShortLink(context.Background(), upstream.URL)
	if err != nil {
		t.Fatalf("expandShortLink: %v", err)
	}
	if !strings.HasPrefix(got, "https://www.bilibili.com/video/BV") {
		t.Fatalf("expanded = %q, want bilibili video URL", got)
	}
}

func TestExpandShortLinkMissingLocation(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	p := &Parser{}
	_, err := p.expandShortLink(context.Background(), upstream.URL)
	if err == nil {
		t.Fatal("expected error when no Location header")
	}
}

func TestExtractBangumiRejected(t *testing.T) {
	p := &Parser{}
	_, err := p.Extract(context.Background(), "https://www.bilibili.com/bangumi/play/ep12345")
	if err == nil || !strings.Contains(err.Error(), "bangumi") {
		t.Fatalf("expected bangumi error, got %v", err)
	}
}

func TestExtractNonBVRejected(t *testing.T) {
	p := &Parser{}
	// Match 应该返回 false，Parse 不会派发到 bilibili；但直接调用 Extract 会走到 resolveID
	_, err := p.Extract(context.Background(), "https://www.bilibili.com/video/")
	if err == nil || !strings.Contains(err.Error(), "BV") {
		t.Fatalf("expected BV detection error, got %v", err)
	}
}

// TestApplyCookie 覆盖 SESSDATA 注入逻辑的最小单元。
func TestApplyCookie(t *testing.T) {
	p := &Parser{SESSData: "abc%2C123"}
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	p.applyCookie(req)
	if got := req.Header.Get("Cookie"); got != "SESSDATA=abc%2C123" {
		t.Fatalf("with SESSData, Cookie header = %q, want %q", got, "SESSDATA=abc%2C123")
	}

	// 空 SESSData：不应该写入 Cookie
	empty := &Parser{}
	req2, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	empty.applyCookie(req2)
	if got := req2.Header.Get("Cookie"); got != "" {
		t.Fatalf("with empty SESSData, Cookie header = %q, want empty", got)
	}

	// 前后空白被 trim
	ws := &Parser{SESSData: "  xyz  "}
	req3, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	ws.applyCookie(req3)
	if got := req3.Header.Get("Cookie"); got != "SESSDATA=xyz" {
		t.Fatalf("with whitespace SESSData, Cookie header = %q, want %q", got, "SESSDATA=xyz")
	}
}

// TestFetchViewSESSDataInjected 验证 SESSDATA 会被加到 web-interface/view 请求里。
func TestFetchViewSESSDataInjected(t *testing.T) {
	var gotCookie atomic.Value // string
	gotCookie.Store("")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie.Store(r.Header.Get("Cookie"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"0","data":{"bvid":"BV1xx","aid":170001,"cid":12345,"title":"mock","pic":"http://i0.hdslb.com/bfs/cover/abc.jpg","duration":100}}`))
	}))
	defer upstream.Close()

	prevClient := httpClient
	prevBase := apiBaseURL
	setHTTPClient(upstream.Client())
	setAPIBaseURL(upstream.URL)
	t.Cleanup(func() {
		setHTTPClient(prevClient)
		setAPIBaseURL(prevBase)
	})

	p := &Parser{SESSData: "session-token-xyz"}
	vd, err := p.fetchView(context.Background(), "BV1xx", 0)
	if err != nil {
		t.Fatalf("fetchView: %v", err)
	}
	if vd.CID != 12345 {
		t.Fatalf("cid = %d, want 12345", vd.CID)
	}
	if got := gotCookie.Load().(string); got != "SESSDATA=session-token-xyz" {
		t.Fatalf("Cookie header on /view = %q, want SESSDATA injection", got)
	}

	// 空 SESSData：不应该带 cookie
	p2 := &Parser{}
	gotCookie.Store("__reset__")
	if _, err := p2.fetchView(context.Background(), "BV1xx", 0); err != nil {
		t.Fatalf("fetchView (no sessdata): %v", err)
	}
	if got := gotCookie.Load().(string); got != "" {
		t.Fatalf("Cookie header on /view (no sessdata) = %q, want empty", got)
	}
}

// TestFetchPlayURLSESSDataInjected 验证 SESSDATA 会被加到 player/playurl 请求里。
func TestFetchPlayURLSESSDataInjected(t *testing.T) {
	var gotCookie atomic.Value
	gotCookie.Store("")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie.Store(r.Header.Get("Cookie"))
		// 只匹配 player/playurl 路径；其它路径直接 404 即可。
		if !strings.Contains(r.URL.Path, "playurl") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"0","data":{"timelength":120000,"durl":[{"url":"https://cdn.example.com/video.mp4"}]}}`))
	}))
	defer upstream.Close()

	prevClient := httpClient
	prevBase := apiBaseURL
	setHTTPClient(upstream.Client())
	setAPIBaseURL(upstream.URL)
	t.Cleanup(func() {
		setHTTPClient(prevClient)
		setAPIBaseURL(prevBase)
	})

	p := &Parser{SESSData: "session-token-xyz"}
	url, dur, err := p.fetchPlayURL(context.Background(), "BV1xx", 0, 12345)
	if err != nil {
		t.Fatalf("fetchPlayURL: %v", err)
	}
	if !strings.HasSuffix(url, ".mp4") {
		t.Fatalf("video url = %q, want .mp4", url)
	}
	if dur != 120 {
		t.Fatalf("duration = %d, want 120", dur)
	}
	if got := gotCookie.Load().(string); got != "SESSDATA=session-token-xyz" {
		t.Fatalf("Cookie header on /playurl = %q, want SESSDATA injection", got)
	}
}
