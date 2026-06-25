package jable

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParserName(t *testing.T) {
	if got := (&Parser{}).Name(); got != "jable" {
		t.Fatalf("Name = %q, want jable", got)
	}
}

func TestMatch(t *testing.T) {
	p := &Parser{}
	cases := []struct {
		url  string
		want bool
	}{
		{"https://jable.tv/videos/abc-123/", true},
		{"https://jable.tv/some-slug/", true},
		{"https://www.jable.tv/videos/abc/", true},
		{"https://example.com/jable.tv/", false}, // 必须 host 段
		{"https://example.com/", false},
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
	_, err := p.Extract(context.Background(), "http://127.0.0.1/videos/abc")
	if err == nil || !strings.Contains(err.Error(), "safefetch") {
		t.Fatalf("expected safefetch error, got %v", err)
	}
}

func TestExtractPullsVideoURLAndMeta(t *testing.T) {
	const m3u8 = "https://cdn.jable.tv/contents/videos/abc/abc.m3u8"
	const thumb = "https://cdn.jable.tv/contents/videos/abc/abc.jpg"

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证 Referer 已经被设成 jable 同源
		if got := r.Header.Get("Referer"); got != "https://jable.tv/" {
			t.Errorf("Referer = %q, want https://jable.tv/", got)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head>
<title>Jable Title</title>
<meta property="og:title" content="Mock Jable Video">
<meta property="og:image" content="` + thumb + `">
</head><body>
<video>
  <source src="` + m3u8 + `" type="application/x-mpegURL">
</video>
</body></html>`))
	}))
	defer upstream.Close()

	// 注入 httptest client + 绕过 safefetch 的 IP 校验（127.0.0.1 会被默认
	// safefetch 当作 loopback 拦掉）
	setHTTPClient(upstream.Client())
	setValidateURL(func(string) error { return nil })
	t.Cleanup(func() {
		setHTTPClient(nil)
		setValidateURL(nil)
	})

	p := &Parser{}
	res, err := p.Extract(context.Background(), upstream.URL+"/videos/abc/")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if res.Source != "" {
		// Source 由 animeparser.runParser 在注册中心派发时设置，
		// 直接调用 Extract 拿到的是空字符串。
		t.Errorf("Source = %q, want empty (set by registry)", res.Source)
	}
	if res.VideoURL != m3u8 {
		t.Errorf("VideoURL = %q, want %q", res.VideoURL, m3u8)
	}
	if res.Title != "Mock Jable Video" {
		t.Errorf("Title = %q, want og:title", res.Title)
	}
	if res.Thumbnail != thumb {
		t.Errorf("Thumbnail = %q, want og:image", res.Thumbnail)
	}
	if res.Headers["Referer"] != "https://jable.tv/" {
		t.Errorf("Headers[Referer] = %q, want https://jable.tv/", res.Headers["Referer"])
	}
}

func TestExtractFallsBackToDataSrc(t *testing.T) {
	const m3u8 = "https://cdn.jable.tv/contents/videos/def/def.m3u8"

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body>
<video data-src="` + m3u8 + `"></video>
</body></html>`))
	}))
	defer upstream.Close()

	setHTTPClient(upstream.Client())
	setValidateURL(func(string) error { return nil })
	t.Cleanup(func() {
		setHTTPClient(nil)
		setValidateURL(nil)
	})

	p := &Parser{}
	res, err := p.Extract(context.Background(), upstream.URL+"/videos/def/")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if res.VideoURL != m3u8 {
		t.Errorf("VideoURL = %q, want %q", res.VideoURL, m3u8)
	}
}

func TestExtractNoVideoReturnsError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body>no video here</body></html>`))
	}))
	defer upstream.Close()

	setHTTPClient(upstream.Client())
	setValidateURL(func(string) error { return nil })
	t.Cleanup(func() {
		setHTTPClient(nil)
		setValidateURL(nil)
	})

	p := &Parser{}
	_, err := p.Extract(context.Background(), upstream.URL+"/videos/empty/")
	if err == nil {
		t.Fatal("expected error when no video source in page")
	}
}

func TestCollectVideoURLsFiltersNonMedia(t *testing.T) {
	html := `
<source src="https://cdn.jable.tv/video.m3u8" type="application/x-mpegURL">
<source src="https://cdn.jable.tv/icon.png" type="image/png">
<source src="data:video/mp4;base64,AAA" type="video/mp4">
<source src="https://cdn.jable.tv/poster.jpg" type="image/jpeg">
`
	got := collectVideoURLs(html)
	if len(got) != 1 {
		t.Fatalf("got %d urls, want 1: %#v", len(got), got)
	}
	if !strings.HasSuffix(got[0], ".m3u8") {
		t.Errorf("survived url = %q, want .m3u8", got[0])
	}
}
