package bilibili

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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
