package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/video-site/backend/internal/drives"
)

type streamURLWithHeader interface {
	StreamURLWithHeader(ctx context.Context, fileID string, header http.Header) (*drives.StreamLink, error)
}

// Registry 管理多个 Drive 实例
type Registry struct {
	mu     sync.RWMutex
	drives map[string]drives.Drive
}

func NewRegistry() *Registry {
	return &Registry{drives: make(map[string]drives.Drive)}
}

func (r *Registry) Set(id string, d drives.Drive) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.drives[id] = d
}

func (r *Registry) Get(id string) (drives.Drive, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.drives[id]
	return d, ok
}

func (r *Registry) All() []drives.Drive {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]drives.Drive, 0, len(r.drives))
	for _, d := range r.drives {
		out = append(out, d)
	}
	return out
}

func (r *Registry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.drives, id)
}

// Proxy 根据 driveID + fileID 反向代理到真实网盘直链
type Proxy struct {
	Registry *Registry
	// linkCache key: driveID + "/" + fileID (+ User-Agent for UA-bound links)
	cacheMu sync.Mutex
	cache   map[string]cachedLink
	http    *http.Client
	// diskCache 字节范围磁盘缓存。未设置时退化为无缓存代理。
	diskCache *diskRangeCache
}

type cachedLink struct {
	link    *drives.StreamLink
	fetched time.Time
}

func New(r *Registry) *Proxy {
	return &Proxy{
		Registry: r,
		cache:    make(map[string]cachedLink),
		// 显式构造 Transport 给流式代理设合理超时：
		//   - IdleConnTimeout 30s: 空闲连接被 server 关掉前主动关
		//   - ResponseHeaderTimeout 10s: 等首字节响应的硬上限，防挂死的网盘源
		//   - ExpectContinueTimeout 1s: 100-continue 头快速失败
		//   - MaxIdleConnsPerHost 4: 同一 drive 复用 4 个空闲连接够了
		// http.Client.Timeout 保持 0：流式 io.Copy 不应被全局超时切断，靠 ctx
		// 和 Transport 各阶段超时分别把关。
		http: &http.Client{
			Transport: &http.Transport{
				IdleConnTimeout:       30 * time.Second,
				ResponseHeaderTimeout: 10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
				MaxIdleConnsPerHost:   4,
			},
		},
	}
}

// EnableDiskCache 启用字节范围磁盘缓存。dir 必须存在；capBytes 是总大小上限。
// 已设置的 cache 会被替换（重启 server 生效）。
func (p *Proxy) EnableDiskCache(dir string, capBytes int64) error {
	c, err := newDiskRangeCache(dir, capBytes)
	if err != nil {
		return err
	}
	p.diskCache = c
	return nil
}

func (p *Proxy) getLink(ctx context.Context, d drives.Drive, driveID, fileID string, header http.Header) (*drives.StreamLink, error) {
	key := linkCacheKey(d, driveID, fileID, header)

	p.cacheMu.Lock()
	if c, ok := p.cache[key]; ok {
		// 缓存 30 秒，且不超过 link.Expires
		if time.Since(c.fetched) < 30*time.Second && time.Now().Before(c.link.Expires) {
			p.cacheMu.Unlock()
			return c.link, nil
		}
	}
	p.cacheMu.Unlock()

	var (
		link *drives.StreamLink
		err  error
	)
	if h, ok := d.(streamURLWithHeader); ok {
		link, err = h.StreamURLWithHeader(ctx, fileID, header)
	} else {
		link, err = d.StreamURL(ctx, fileID)
	}
	if err != nil {
		return nil, err
	}
	p.cacheMu.Lock()
	p.cache[key] = cachedLink{link: link, fetched: time.Now()}
	p.cacheMu.Unlock()
	return link, nil
}

func linkCacheKey(d drives.Drive, driveID, fileID string, header http.Header) string {
	key := driveID + "/" + fileID
	if _, ok := d.(streamURLWithHeader); ok {
		key += "|ua=" + header.Get("User-Agent")
	}
	return key
}

func (p *Proxy) ServeStream(w http.ResponseWriter, r *http.Request, driveID, fileID string) {
	d, ok := p.Registry.Get(driveID)
	if !ok {
		http.Error(w, errDriveNotFound.Error(), errDriveNotFound.Code)
		return
	}

	link, err := p.getLink(r.Context(), d, driveID, fileID, r.Header)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if shouldRedirect(d) {
		redirect(w, r, link)
		return
	}
	p.serve(w, r, link)
}

// shouldRedirect 返回 true 时，/p/stream 不再反代视频字节，
// 而是用 302 让浏览器直连网盘 CDN。
//
// 只把"自己签名 URL 即可下载、不需要持久 Header 鉴权"的网盘放进来：
//   - p115：CDN 签名链接，UA 通过 streamURLWithHeader 在取链时使用，
//     302 之后浏览器用自己的 UA 直连，CDN 仍然认签名
//   - pikpak：与 OpenList 一致，WebContentLink / media link 都是自签 URL，
//     CDN 不校验请求头，直连可获得最佳带宽并避免占用 backend 出站
//   - onedrive：Microsoft Graph 返回的 @microsoft.graph.downloadUrl 是短期
//     免鉴权下载 URL，不需要后端继续代传视频字节
//   - p123：123网盘 download_info 返回的下载页会再跳 CDN；driver 已在后端
//     先解出最终 Location，浏览器可直接 302 到该短期地址
//   - wopan：联通网盘 GetDownloadUrlV2 返回的是短期直链，OpenList 也是直接
//     将该 URL 交给客户端使用；不需要后端持续代传视频字节
//   - guangyapan：光鸭 get_res_download_url 返回 signedURL / downloadUrl，
//     浏览器可直接访问，不需要后端持续代传视频字节
//
// 其余网盘（如夸克等）仍走反代，因为它们的下载
// 链接通常需要随请求带上后端持有的 Cookie / Authorization / Range
// 的特殊处理，浏览器拿不到这些上下文。
func shouldRedirect(d drives.Drive) bool {
	switch d.Kind() {
	case "p115", "pikpak", "onedrive", "p123", "wopan", "guangyapan":
		return true
	}
	return false
}

func redirect(w http.ResponseWriter, r *http.Request, link *drives.StreamLink) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "max-age=0, no-cache, no-store, must-revalidate")
	http.Redirect(w, r, link.URL, http.StatusFound)
}

func (p *Proxy) serve(w http.ResponseWriter, r *http.Request, link *drives.StreamLink) {
	// 构造上游请求
	u, err := url.Parse(link.URL)
	if err != nil {
		http.Error(w, "bad upstream url", http.StatusBadGateway)
		return
	}
	if localPath, ok := localFilePath(u, link.URL); ok {
		w.Header().Set("Cache-Control", "private, max-age=300")
		http.ServeFile(w, r, localPath)
		return
	}

	// 字节范围缓存：相同 (driveID, fileID, range) 的请求复用上次下载的字节块。
	// 减少 115/PikPak/夸克等网盘的限速压力。Range 为空（请求整文件）时不缓存——
	// 整文件动辄几百 MB，命中率低且浪费磁盘，按需重新取更划算。
	if p.diskCache != nil && r.Header.Get("Range") != "" {
		rangeStart, rangeEnd, ok := parseRange(r.Header.Get("Range"))
		key := driveFileFromRequest(r)
		if ok && rangeEnd > 0 && p.tryServeFromCache(w, r, key, rangeStart, rangeEnd) {
			return
		}
	}

	req, err := http.NewRequestWithContext(r.Context(), r.Method, u.String(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// 复制上游请求头
	for k, vs := range link.Headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	// 透传 Range
	if rng := r.Header.Get("Range"); rng != "" {
		req.Header.Set("Range", rng)
	}

	resp, err := p.http.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// 透传响应头
	for _, k := range []string{
		"Content-Type", "Content-Length", "Content-Range",
		"Accept-Ranges", "Last-Modified", "Etag",
	} {
		if v := resp.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.WriteHeader(resp.StatusCode)

	// 决定是否写盘缓存
	var cacheSink io.WriteCloser
	var cachePath string
	if p.diskCache != nil && r.Header.Get("Range") != "" && resp.StatusCode == http.StatusPartialContent {
		if rangeStart, rangeEnd, ok := parseRange(r.Header.Get("Range")); ok && rangeEnd > 0 {
			if rangeEnd-rangeStart+1 <= p.diskCache.maxEntrySize {
				key := driveFileFromRequest(r)
				if path, err := p.diskCache.prepareEntry(key, rangeStart, rangeEnd); err == nil {
					cachePath = path
					f, err := os.Create(cachePath)
					if err == nil {
						cacheSink = f
					}
				}
			}
		}
	}

	// 流式 io.Copy：客户端断开（r.Context().Done()）时让连接复用以释放 socket。
	// 同时把字节流写入缓存文件（如果有），让后续请求复用。
	closed := make(chan struct{})
	go func() {
		var dst io.Writer = w
		if cacheSink != nil {
			dst = io.MultiWriter(w, cacheSink)
		}
		_, _ = io.Copy(dst, resp.Body)
		if cacheSink != nil {
			_ = cacheSink.Close()
			// 标记 entry 完成（即使部分写入也算 LRU 候选，下次再补全）
			p.diskCache.commitEntry(cachePath)
		}
		close(closed)
	}()
	select {
	case <-closed:
	case <-r.Context().Done():
		if cacheSink != nil {
			_ = cacheSink.Close()
			// 客户端断开 → 这段可能不完整。删掉避免下次服务错误内容。
			p.diskCache.discardEntry(cachePath)
		}
	}
}

// tryServeFromCache 尝试从磁盘缓存读出 range；命中且 TTL 未过期时写回 w 并返回 true。
func (p *Proxy) tryServeFromCache(w http.ResponseWriter, r *http.Request, key string, start, end int64) bool {
	if p.diskCache == nil {
		return false
	}
	path := p.diskCache.entryPath(key, start, end)
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		return false
	}
	if time.Since(info.ModTime()) > p.diskCache.ttl {
		_ = os.Remove(path)
		return false
	}
	expected := end - start + 1
	if info.Size() != expected {
		return false
	}
	// 命中：设置 Range 响应头 + 透传文件
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, end+1))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.WriteHeader(http.StatusPartialContent)
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	_, _ = io.Copy(w, f)
	p.diskCache.touch(path)
	return true
}

func driveFileFromRequest(r *http.Request) string {
	driveID := chi.URLParam(r, "driveID")
	fileID := strings.TrimPrefix(r.URL.Path, "/p/stream/"+driveID+"/")
	return driveID + "|" + fileID
}

// driveRangeKey 计算 cache 文件名：sha256(driveID + "\x00" + fileID + "\x00" + start + "\x00" + end)。
// 用 NUL 分隔避免 "drive-A + file-BC" 和 "drive-AB + file-C" 碰撞。
func driveRangeKey(key string, start, end int64) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%d", key, start, end)))
	return hex.EncodeToString(h[:])
}

// parseRange 解析 HTTP Range 头的 "bytes=start-end"。只支持单段。
// 返回 (start, end, ok)。end==-1 表示到文件末尾（如 "bytes=0-"）。
func parseRange(header string) (int64, int64, bool) {
	const prefix = "bytes="
	if !strings.HasPrefix(header, prefix) {
		return 0, 0, false
	}
	spec := strings.TrimPrefix(header, prefix)
	idx := strings.IndexByte(spec, ',')
	if idx >= 0 {
		spec = spec[:idx]
	}
	spec = strings.TrimSpace(spec)
	dash := strings.IndexByte(spec, '-')
	if dash < 0 {
		return 0, 0, false
	}
	startStr := strings.TrimSpace(spec[:dash])
	endStr := strings.TrimSpace(spec[dash+1:])
	var start, end int64
	var err error
	if startStr == "" {
		// suffix range "bytes=-500" → 最后 500 字节。本代理暂不支持。
		return 0, 0, false
	}
	if start, err = strconv.ParseInt(startStr, 10, 64); err != nil {
		return 0, 0, false
	}
	if endStr == "" {
		end = -1
	} else {
		if end, err = strconv.ParseInt(endStr, 10, 64); err != nil {
			return 0, 0, false
		}
		if end < start {
			return 0, 0, false
		}
	}
	return start, end, true
}

// --- 字节范围磁盘缓存 ---

// diskRangeCache 是 /p/stream 代理的字节范围磁盘缓存。命中已下载过的
// (driveID, fileID, start, end) 元组时直接读盘写回，避免重复打上游网盘。
//
// 设计要点：
//   - 写入未完成的 entry 用 .partial 后缀，commit 时改名为正式；客户端断开时删除。
//   - LRU 淘汰按 mtime 升序；总大小超 cap 时循环淘汰。
//   - Range 大小超过 maxEntrySize（默认 16MB）不缓存——常见的大块下载基本是一次性，
//     缓存下来命中率低且浪费磁盘。
type diskRangeCache struct {
	dir          string
	capBytes     int64
	maxEntrySize int64
	ttl          time.Duration

	mu      sync.Mutex
	size    int64
	entries map[string]int64 // path → size
}

func newDiskRangeCache(dir string, capBytes int64) (*diskRangeCache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	c := &diskRangeCache{
		dir:          dir,
		capBytes:     capBytes,
		maxEntrySize: 16 << 20, // 16 MB
		ttl:          24 * time.Hour,
		entries:      make(map[string]int64),
	}
	// 启动时扫描已有文件，重建 size 计数
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".bin") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		c.entries[e.Name()] = info.Size()
		c.size += info.Size()
	}
	return c, nil
}

// entryPath 返回已提交 entry 的文件路径。
func (c *diskRangeCache) entryPath(key string, start, end int64) string {
	return filepath.Join(c.dir, driveRangeKey(key, start, end)+".bin")
}

// prepareEntry 为 (drive, file, range) 分配临时 .partial 文件路径，调用方写入
// 完成后 commitEntry 改名。失败或客户端断开时调用 discardEntry 清理。
func (c *diskRangeCache) prepareEntry(key string, start, end int64) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	target := c.entryPath(key, start, end)
	if _, exists := c.entries[target]; exists {
		// 已存在 → 不重新下载；让上游请求照常打，但不再写新文件。
		return "", errors.New("entry already exists")
	}
	return target + ".partial", nil
}

func (c *diskRangeCache) commitEntry(partialPath string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := os.Stat(partialPath); err != nil {
		return
	}
	final := strings.TrimSuffix(partialPath, ".partial")
	if err := os.Rename(partialPath, final); err != nil {
		_ = os.Remove(partialPath)
		return
	}
	info, err := os.Stat(final)
	if err != nil {
		return
	}
	c.entries[final] = info.Size()
	c.size += info.Size()
	c.evictIfNeeded()
}

func (c *diskRangeCache) discardEntry(partialPath string) {
	_ = os.Remove(partialPath)
}

// touch 把已提交 entry 的 mtime 更新到当前时间，标记最近使用。
func (c *diskRangeCache) touch(path string) {
	now := time.Now()
	_ = os.Chtimes(path, now, now)
}

// evictIfNeeded 在 size 超出 cap 时按 mtime ASC 淘汰，直到 ≤ cap * 0.8
// （避免频繁抖动）。已加锁调用。
func (c *diskRangeCache) evictIfNeeded() {
	if c.size <= c.capBytes {
		return
	}
	target := c.capBytes * 4 / 5
	type cand struct {
		path  string
		mtime time.Time
	}
	var cands []cand
	for path := range c.entries {
		info, err := os.Stat(path)
		if err != nil {
			delete(c.entries, path)
			continue
		}
		cands = append(cands, cand{path: path, mtime: info.ModTime()})
	}
	// 简单选择排序（小数据集，< 几千条；LRU 淘汰不需要堆）
	for i := 0; i < len(cands); i++ {
		for j := i + 1; j < len(cands); j++ {
			if cands[j].mtime.Before(cands[i].mtime) {
				cands[i], cands[j] = cands[j], cands[i]
			}
		}
	}
	for _, cand := range cands {
		if c.size <= target {
			break
		}
		if err := os.Remove(cand.path); err == nil {
			c.size -= c.entries[cand.path]
			delete(c.entries, cand.path)
		}
	}
}

// fmt 占位导入保护（go vet 会抱怨未使用的 fmt）
var _ = fmt.Sprintf

// ServeLocal 服务本地预览视频文件
func (p *Proxy) ServeLocal(w http.ResponseWriter, r *http.Request, path string) {
	http.ServeFile(w, r, path)
}

func localFilePath(u *url.URL, raw string) (string, bool) {
	if filepath.IsAbs(raw) {
		return raw, true
	}
	if u == nil {
		return "", false
	}
	if u.Scheme == "file" && u.Path != "" {
		return u.Path, true
	}
	if u.Scheme == "" && u.Host == "" && filepath.IsAbs(raw) {
		return raw, true
	}
	return "", false
}

var errDriveNotFound = &httpError{Code: http.StatusNotFound, Msg: "drive not found"}

type httpError struct {
	Code int
	Msg  string
}

func (e *httpError) Error() string { return e.Msg }
