package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/video-site/backend/internal/drives"
)

func TestServeStreamRedirectsP115WithRequestUserAgent(t *testing.T) {
	reg := NewRegistry()
	drv := &proxyFakeDrive{kind: "p115"}
	reg.Set("115", drv)

	p := New(reg)
	req := httptest.NewRequest(http.MethodGet, "/p/stream/115/file-1", nil)
	req.Header.Set("User-Agent", "Browser-A")
	rr := httptest.NewRecorder()

	p.ServeStream(rr, req, "115", "file-1")

	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusFound)
	}
	if got := rr.Header().Get("Location"); got != "https://cdn.example/file-1?ua=Browser-A" {
		t.Fatalf("Location = %q", got)
	}
	if got := drv.calls[0].ua; got != "Browser-A" {
		t.Fatalf("link UA = %q, want request UA", got)
	}
	if got := rr.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q", got)
	}
}

func TestServeStreamP115CacheIsUserAgentScoped(t *testing.T) {
	reg := NewRegistry()
	drv := &proxyFakeDrive{kind: "p115"}
	reg.Set("115", drv)

	p := New(reg)

	requestP115(t, p, "115", "file-1", "Browser-A")
	requestP115(t, p, "115", "file-1", "Browser-B")
	requestP115(t, p, "115", "file-1", "Browser-A")

	if len(drv.calls) != 2 {
		t.Fatalf("link calls = %d, want 2", len(drv.calls))
	}
	if drv.calls[0].ua != "Browser-A" || drv.calls[1].ua != "Browser-B" {
		t.Fatalf("link UAs = %#v", drv.calls)
	}
}

func requestP115(t *testing.T, p *Proxy, driveID, fileID, ua string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/p/stream/"+driveID+"/"+fileID, nil)
	req.Header.Set("User-Agent", ua)
	rr := httptest.NewRecorder()
	p.ServeStream(rr, req, driveID, fileID)
	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusFound)
	}
}

type proxyFakeDrive struct {
	kind  string
	calls []proxyFakeCall
}

type proxyFakeCall struct {
	fileID string
	ua     string
}

func (d *proxyFakeDrive) Kind() string { return d.kind }
func (d *proxyFakeDrive) ID() string   { return "fake" }
func (d *proxyFakeDrive) Init(context.Context) error {
	return nil
}
func (d *proxyFakeDrive) List(context.Context, string) ([]drives.Entry, error) {
	return nil, drives.ErrNotSupported
}
func (d *proxyFakeDrive) Stat(context.Context, string) (*drives.Entry, error) {
	return nil, drives.ErrNotSupported
}
func (d *proxyFakeDrive) StreamURL(ctx context.Context, fileID string) (*drives.StreamLink, error) {
	return d.StreamURLWithHeader(ctx, fileID, nil)
}
func (d *proxyFakeDrive) StreamURLWithHeader(_ context.Context, fileID string, header http.Header) (*drives.StreamLink, error) {
	ua := header.Get("User-Agent")
	d.calls = append(d.calls, proxyFakeCall{fileID: fileID, ua: ua})
	return &drives.StreamLink{
		URL:     "https://cdn.example/" + fileID + "?ua=" + ua,
		Headers: http.Header{"User-Agent": {ua}},
		Expires: time.Now().Add(time.Minute),
	}, nil
}
func (d *proxyFakeDrive) Upload(context.Context, string, string, io.Reader, int64) (string, error) {
	return "", drives.ErrNotSupported
}
func (d *proxyFakeDrive) EnsureDir(context.Context, string) (string, error) {
	return "", drives.ErrNotSupported
}
func (d *proxyFakeDrive) RootID() string { return "0" }

func TestServeStreamRedirectsPikPakWithoutUserAgentScopedCache(t *testing.T) {
	reg := NewRegistry()
	drv := &proxyFakePikPakDrive{}
	reg.Set("pikpak", drv)

	p := New(reg)

	// 三次请求，两个不同 UA。pikpak 取链不依赖 UA，所以缓存 key 只看 driveID/fileID，
	// 30 秒内只会真正调用 driver 一次。
	requestPikPak(t, p, "pikpak", "file-1", "Browser-A")
	requestPikPak(t, p, "pikpak", "file-1", "Browser-B")
	requestPikPak(t, p, "pikpak", "file-1", "Browser-A")

	if drv.calls != 1 {
		t.Fatalf("link calls = %d, want 1 (cache must not be UA-scoped for pikpak)", drv.calls)
	}
}

func TestServeStreamPikPakSetsRedirectHeaders(t *testing.T) {
	reg := NewRegistry()
	drv := &proxyFakePikPakDrive{}
	reg.Set("pikpak", drv)

	p := New(reg)
	req := httptest.NewRequest(http.MethodGet, "/p/stream/pikpak/file-1", nil)
	req.Header.Set("User-Agent", "Browser-A")
	rr := httptest.NewRecorder()

	p.ServeStream(rr, req, "pikpak", "file-1")

	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusFound)
	}
	if got := rr.Header().Get("Location"); got != "https://cdn.pikpak.example/file-1" {
		t.Fatalf("Location = %q", got)
	}
	if got := rr.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q", got)
	}
}

func TestParseRangeHandlesSingleRange(t *testing.T) {
	cases := []struct {
		in    string
		start int64
		end   int64
		ok    bool
	}{
		{"bytes=0-1023", 0, 1023, true},
		{"bytes=100-", 100, -1, true},
		{"bytes=0-99,200-299", 0, 99, true}, // multi-range → first only
		{"bytes=-500", 0, 0, false},        // suffix not supported
		{"junk", 0, 0, false},
		{"", 0, 0, false},
	}
	for _, c := range cases {
		s, e, ok := parseRange(c.in)
		if ok != c.ok || s != c.start || e != c.end {
			t.Errorf("parseRange(%q) = (%d, %d, %v), want (%d, %d, %v)",
				c.in, s, e, ok, c.start, c.end, c.ok)
		}
	}
}

func TestDiskRangeCachePrepareCommitDiscard(t *testing.T) {
	dir := t.TempDir()
	c, err := newDiskRangeCache(dir, 1<<20) // 1MB cap
	if err != nil {
		t.Fatalf("newDiskRangeCache: %v", err)
	}

	// prepare → write → commit → entryPath 应存在
	partial, err := c.prepareEntry("drive-1|file-A", 0, 99)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if err := os.WriteFile(partial, []byte("0123456789"), 0o644); err != nil {
		t.Fatalf("write partial: %v", err)
	}
	c.commitEntry(partial)

	finalPath := c.entryPath("drive-1|file-A", 0, 99)
	if _, err := os.Stat(finalPath); err != nil {
		t.Fatalf("commit entry missing: %v", err)
	}
	if c.size != 10 {
		t.Fatalf("cache size = %d, want 10", c.size)
	}

	// 不同 (start, end) 是不同 entry
	partial2, err := c.prepareEntry("drive-1|file-A", 100, 199)
	if err != nil {
		t.Fatalf("prepare2: %v", err)
	}
	if err := os.WriteFile(partial2, []byte("abcdefghij"), 0o644); err != nil {
		t.Fatalf("write partial2: %v", err)
	}
	c.commitEntry(partial2)
	if c.size != 20 {
		t.Fatalf("after second commit size = %d, want 20", c.size)
	}

	// discard → 文件应不存在
	partial3, err := c.prepareEntry("drive-1|file-B", 0, 9)
	if err != nil {
		t.Fatalf("prepare3: %v", err)
	}
	if err := os.WriteFile(partial3, []byte("xxxxx"), 0o644); err != nil {
		t.Fatalf("write partial3: %v", err)
	}
	c.discardEntry(partial3)
	if _, err := os.Stat(partial3); !os.IsNotExist(err) {
		t.Fatalf("discarded partial still exists: %v", err)
	}
}

func TestDiskRangeCacheEvictionByMtime(t *testing.T) {
	dir := t.TempDir()
	// 100B cap 让第二个 entry 立即触发淘汰
	c, err := newDiskRangeCache(dir, 100)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	for i := 0; i < 3; i++ {
		partial, err := c.prepareEntry("drive-1|file-"+string(rune('A'+i)), 0, 99)
		if err != nil {
			t.Fatalf("prepare %d: %v", i, err)
		}
		if err := os.WriteFile(partial, make([]byte, 50), 0o644); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		time.Sleep(20 * time.Millisecond)
		c.commitEntry(partial)
	}

	if c.size > 100 {
		t.Fatalf("size after eviction = %d, want ≤ 100", c.size)
	}
	if len(c.entries) > 1 {
		t.Fatalf("entries after eviction = %d, want ≤ 1 (oldest evicted)", len(c.entries))
	}
}

func TestDiskRangeCacheRebuildsSizeFromDisk(t *testing.T) {
	dir := t.TempDir()
	for i, name := range []string{"a.bin", "b.bin"} {
		if err := os.WriteFile(filepath.Join(dir, name), make([]byte, 100+i*50), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	c, err := newDiskRangeCache(dir, 1<<20)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if c.size != 100+150 {
		t.Fatalf("rebuild size = %d, want 250", c.size)
	}
	if len(c.entries) != 2 {
		t.Fatalf("rebuild entries = %d, want 2", len(c.entries))
	}
}

func TestServeStreamRedirectsOneDrive(t *testing.T) {
	reg := NewRegistry()
	drv := &proxyFakeSimpleDrive{
		kind: "onedrive",
		url:  "https://public.onedrive.example/video.mp4",
	}
	reg.Set("onedrive", drv)

	p := New(reg)
	req := httptest.NewRequest(http.MethodGet, "/p/stream/onedrive/file-1", nil)
	rr := httptest.NewRecorder()

	p.ServeStream(rr, req, "onedrive", "file-1")

	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusFound)
	}
	if got := rr.Header().Get("Location"); got != "https://public.onedrive.example/video.mp4" {
		t.Fatalf("Location = %q", got)
	}
	if drv.calls != 1 {
		t.Fatalf("link calls = %d, want 1", drv.calls)
	}
}

func TestServeStreamRedirectsP123(t *testing.T) {
	reg := NewRegistry()
	drv := &proxyFakeSimpleDrive{
		kind: "p123",
		url:  "https://cdn.123pan.example/video.mp4",
	}
	reg.Set("p123", drv)

	p := New(reg)
	req := httptest.NewRequest(http.MethodGet, "/p/stream/p123/file-1", nil)
	rr := httptest.NewRecorder()

	p.ServeStream(rr, req, "p123", "file-1")

	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusFound)
	}
	if got := rr.Header().Get("Location"); got != "https://cdn.123pan.example/video.mp4" {
		t.Fatalf("Location = %q", got)
	}
	if drv.calls != 1 {
		t.Fatalf("link calls = %d, want 1", drv.calls)
	}
}

func TestServeStreamRedirectsWopan(t *testing.T) {
	reg := NewRegistry()
	drv := &proxyFakeSimpleDrive{
		kind: "wopan",
		url:  "https://du.smartont.net:8445/openapi/download?fid=encoded",
	}
	reg.Set("wopan", drv)

	p := New(reg)
	req := httptest.NewRequest(http.MethodGet, "/p/stream/wopan/file-1", nil)
	rr := httptest.NewRecorder()

	p.ServeStream(rr, req, "wopan", "file-1")

	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusFound)
	}
	if got := rr.Header().Get("Location"); got != "https://du.smartont.net:8445/openapi/download?fid=encoded" {
		t.Fatalf("Location = %q", got)
	}
	if drv.calls != 1 {
		t.Fatalf("link calls = %d, want 1", drv.calls)
	}
}

func TestServeStreamRedirectsGuangYaPan(t *testing.T) {
	reg := NewRegistry()
	drv := &proxyFakeSimpleDrive{
		kind: "guangyapan",
		url:  "https://cdn.guangyapan.example/video.mp4?sign=encoded",
	}
	reg.Set("guangyapan", drv)

	p := New(reg)
	req := httptest.NewRequest(http.MethodGet, "/p/stream/guangyapan/file-1", nil)
	rr := httptest.NewRecorder()

	p.ServeStream(rr, req, "guangyapan", "file-1")

	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusFound)
	}
	if got := rr.Header().Get("Location"); got != "https://cdn.guangyapan.example/video.mp4?sign=encoded" {
		t.Fatalf("Location = %q", got)
	}
	if drv.calls != 1 {
		t.Fatalf("link calls = %d, want 1", drv.calls)
	}
}

func TestServeStreamServesLocalFilePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "video.mp4")
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatalf("write local file: %v", err)
	}
	reg := NewRegistry()
	drv := &proxyFakeSimpleDrive{
		kind: "localstorage",
		url:  path,
	}
	reg.Set("local", drv)

	p := New(reg)
	req := httptest.NewRequest(http.MethodGet, "/p/stream/local/file-1", nil)
	req.Header.Set("Range", "bytes=2-5")
	rr := httptest.NewRecorder()

	p.ServeStream(rr, req, "local", "file-1")

	if rr.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusPartialContent)
	}
	if got := rr.Body.String(); got != "2345" {
		t.Fatalf("body = %q, want range bytes", got)
	}
	if drv.calls != 1 {
		t.Fatalf("link calls = %d, want 1", drv.calls)
	}
}

func requestPikPak(t *testing.T, p *Proxy, driveID, fileID, ua string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/p/stream/"+driveID+"/"+fileID, nil)
	req.Header.Set("User-Agent", ua)
	rr := httptest.NewRecorder()
	p.ServeStream(rr, req, driveID, fileID)
	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusFound)
	}
}

// proxyFakePikPakDrive 故意不实现 streamURLWithHeader，
// 用来回归 pikpak 取链不带 UA 作用域、且走 302 的不变量。
type proxyFakePikPakDrive struct {
	calls int
}

func (d *proxyFakePikPakDrive) Kind() string { return "pikpak" }
func (d *proxyFakePikPakDrive) ID() string   { return "pikpak" }
func (d *proxyFakePikPakDrive) Init(context.Context) error {
	return nil
}
func (d *proxyFakePikPakDrive) List(context.Context, string) ([]drives.Entry, error) {
	return nil, drives.ErrNotSupported
}
func (d *proxyFakePikPakDrive) Stat(context.Context, string) (*drives.Entry, error) {
	return nil, drives.ErrNotSupported
}
func (d *proxyFakePikPakDrive) StreamURL(_ context.Context, fileID string) (*drives.StreamLink, error) {
	d.calls++
	return &drives.StreamLink{
		URL:     "https://cdn.pikpak.example/" + fileID,
		Headers: http.Header{},
		Expires: time.Now().Add(10 * time.Minute),
	}, nil
}
func (d *proxyFakePikPakDrive) Upload(context.Context, string, string, io.Reader, int64) (string, error) {
	return "", drives.ErrNotSupported
}
func (d *proxyFakePikPakDrive) EnsureDir(context.Context, string) (string, error) {
	return "", drives.ErrNotSupported
}
func (d *proxyFakePikPakDrive) RootID() string { return "0" }

type proxyFakeSimpleDrive struct {
	kind  string
	url   string
	calls int
}

func (d *proxyFakeSimpleDrive) Kind() string { return d.kind }
func (d *proxyFakeSimpleDrive) ID() string   { return d.kind }
func (d *proxyFakeSimpleDrive) Init(context.Context) error {
	return nil
}
func (d *proxyFakeSimpleDrive) List(context.Context, string) ([]drives.Entry, error) {
	return nil, drives.ErrNotSupported
}
func (d *proxyFakeSimpleDrive) Stat(context.Context, string) (*drives.Entry, error) {
	return nil, drives.ErrNotSupported
}
func (d *proxyFakeSimpleDrive) StreamURL(context.Context, string) (*drives.StreamLink, error) {
	d.calls++
	return &drives.StreamLink{
		URL:     d.url,
		Headers: http.Header{},
		Expires: time.Now().Add(10 * time.Minute),
	}, nil
}
func (d *proxyFakeSimpleDrive) Upload(context.Context, string, string, io.Reader, int64) (string, error) {
	return "", drives.ErrNotSupported
}
func (d *proxyFakeSimpleDrive) EnsureDir(context.Context, string) (string, error) {
	return "", drives.ErrNotSupported
}
func (d *proxyFakeSimpleDrive) RootID() string { return "0" }
