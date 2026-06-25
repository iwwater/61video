package api

import (
	"context"
	crand "crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/video-site/backend/internal/animeparser"
	"github.com/video-site/backend/internal/auth"
	"github.com/video-site/backend/internal/catalog"
	"github.com/video-site/backend/internal/drives/localstorage"
	"github.com/video-site/backend/internal/drives/localupload"
	"github.com/video-site/backend/internal/drives/spider91"
	"github.com/video-site/backend/internal/mediaasset"
	"github.com/video-site/backend/internal/mediatype"
	"github.com/video-site/backend/internal/proxy"
	"github.com/video-site/backend/internal/resourcesearch"
	"github.com/video-site/backend/internal/safefetch"
)

const localUploadDriveID = localupload.DriveID

var allowedUploadExtensions = map[string]struct{}{
	".aac":  {},
	".avi":  {},
	".flac": {},
	".mkv":  {},
	".m4a":  {},
	".mov":  {},
	".mp3":  {},
	".mp4":  {},
	".ogg":  {},
	".opus": {},
	".wav":  {},
	".webm": {},
}

var allowedUploadTags = map[string]struct{}{
	"奶子": {},
	"臀":  {},
	"口交": {},
	"女大": {},
	"人妻": {},
	"AV": {},
}

type Server struct {
	Catalog         *catalog.Catalog
	Proxy           *proxy.Proxy
	LocalDir        string
	UploadDir       string
	OnVideoUploaded func(*catalog.Video)
	// OnHideVideo 处理前台「不再展示」。隐藏机制已废弃，改走拉黑逻辑：
	// 删除库中记录 + 本地封面/预览，保留网盘源文件，并写黑名单墓碑
	// （扫盘不再入库）。未注入时回退为旧的 hidden 标记。
	OnHideVideo func(ctx context.Context, videoID string) error

	tagCacheMu    sync.Mutex
	tagCacheUntil time.Time
	tagCache      []TagDTO

	// GetTheme 返回当前生效的主题（"dark" | "pink" | "sky"）。前台 /api/settings/theme 用，
	// 不需要登录。无注入时返回 "dark"。
	GetTheme func() string
}

const (
	homePageSize = 12
)

// VideoDTO 是返回给前端的视频对象，字段名跟前端 VideoItem 对齐
type VideoDTO struct {
	ID              string `json:"id"`
	MediaType       string `json:"mediaType"`
	Href            string `json:"href"`
	Title           string `json:"title"`
	Thumbnail       string `json:"thumbnail"`
	PreviewSrc      string `json:"previewSrc"`
	PreviewDuration int    `json:"previewDuration"`
	PreviewStrategy string `json:"previewStrategy"`
	Duration        string `json:"duration"`
	// DurationSeconds：原始秒数（前端用于算进度条比例）。
	DurationSeconds int `json:"durationSeconds"`
	// ProgressSeconds：客户端最后一次上报的 currentTime（0=未看；>=duration-30 视为看完）。
	ProgressSeconds float64  `json:"progressSeconds"`
	Badges          []string `json:"badges"`
	Quality         string   `json:"quality,omitempty"`
	SourceLabel     string   `json:"sourceLabel,omitempty"`
	Author          string   `json:"author"`
	Views           int      `json:"views"`
	Favorites       int      `json:"favorites"`
	Comments        int      `json:"comments"`
	Likes           int      `json:"likes"`
	Dislikes        int      `json:"dislikes"`
	PublishedAt     string   `json:"publishedAt"`
	Tags            []string `json:"tags,omitempty"`
	Category        string   `json:"category,omitempty"`
}

type TagDTO struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

type VideoDetailDTO struct {
	VideoDTO
	MediaSrc      string        `json:"mediaSrc"`
	VideoSrc      string        `json:"videoSrc"`
	Poster        string        `json:"poster"`
	Description   string        `json:"description"`
	EmbedURL      string        `json:"embedUrl"`
	Points        int           `json:"points,omitempty"`
	AuthorProfile AuthorProfile `json:"authorProfile"`
	RelatedVideos []VideoDTO    `json:"relatedVideos"`
	CommentsList  []Comment     `json:"commentsList"`
}

type AuthorProfile struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Href   string   `json:"href"`
	Badges []string `json:"badges"`
}

type Comment struct {
	ID        string `json:"id"`
	Author    string `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
	Likes     int    `json:"likes,omitempty"`
}

// RegisterRoutes 挂载前台 REST 路由。前台接口需要登录态。
func (s *Server) RegisterRoutes(r chi.Router, a *auth.Authenticator) {
	// 公开端点：拿当前生效的主题。登录页本身要在挂前就能读，所以单独挂在
	// 鉴权组之外。只暴露 theme 一个字段，避免泄露其他设置。
	r.Get("/api/settings/theme", s.handleGetTheme)

	r.Group(func(r chi.Router) {
		r.Use(a.Required)
		r.Get("/api/home", s.handleHome)
		r.Get("/api/list", s.handleList)
		r.Get("/api/video/{id}", s.handleVideoDetail)
		r.Put("/api/video/{id}/tags", s.handleUpdateVideoTags)
		r.Post("/api/video/{id}/like", s.handleLike)
		r.Delete("/api/video/{id}/like", s.handleUnlike)
		r.Post("/api/video/{id}/view", s.handleView)
		r.Post("/api/video/{id}/progress", s.handleProgress)
		r.Get("/api/home/continue-watching", s.handleContinueWatching)
		r.Get("/api/search", s.handleUnifiedSearch)
		r.Post("/api/video/{id}/hide", s.handleHideVideo)
		r.Post("/api/upload", s.handleUploadVideo)
		r.Get("/api/tags", s.handleTags)
		r.Post("/api/shorts/next", s.handleShortsNext)
		r.Get("/api/galleries", s.handleGalleries)
		r.Get("/api/gallery/{id}", s.handleGalleryDetail)
		r.Get("/api/novels", s.handleNovels)
		r.Get("/api/novel/{id}", s.handleNovelDetail)
		r.Get("/api/novel/{id}/chapter/{position}", s.handleNovelChapter)
		r.Post("/api/novels", s.handleCreateNovel)
		r.Delete("/api/novel/{id}", s.handleDeleteNovel)
		r.Get("/api/anime/search", s.handleAnimeSearch)
		r.Get("/api/anime/sources", s.handleAnimeSources)
		r.Get("/api/anime/resource/detail", s.handleAnimeResourceDetail)
		r.Post("/api/anime/parse", s.handleAnimeParse)
		r.Post("/api/anime/iframe", s.handleAnimeIframe)

		// 代理路由同样需要鉴权，防止绕过
		r.Get("/p/stream/{driveID}/*", s.handleStream)
		r.Get("/p/upload/{videoID}", s.handleUploadedVideo)
		r.Get("/p/spider91/{videoID}", s.handleSpider91Video)
		r.Get("/p/preview/{videoID}", s.handlePreview)
		r.Get("/p/thumb/{videoID}", s.handleThumb)
	})
}

// handleGetTheme 返回当前生效的主题。无需登录。响应永远是
// {"theme": "dark" | "pink" | "sky"}，便于前端无脑解析。
func (s *Server) handleGetTheme(w http.ResponseWriter, r *http.Request) {
	theme := "dark"
	if s.GetTheme != nil {
		if v := s.GetTheme(); v == "pink" || v == "dark" || v == "sky" {
			theme = v
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"theme": theme})
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	// 首页优先从全量已有封面的视频里随机抽取，避免只在最近一小段候选中反复出现。
	excludeIDs := parseVideoIDQuery(r, "exclude", 120)
	items, err := s.Catalog.RandomVideosWithReadyThumbnailsExcluding(r.Context(), excludeIDs, homePageSize)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if len(items) < homePageSize {
		fallbackExclude := append([]string{}, excludeIDs...)
		for _, item := range items {
			if item != nil {
				fallbackExclude = append(fallbackExclude, item.ID)
			}
		}
		fallback, err := s.Catalog.RandomVideosExcluding(r.Context(), fallbackExclude, homePageSize-len(items))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		items = appendUniqueVideos(items, fallback, homePageSize)
	}
	if len(items) < homePageSize && len(excludeIDs) > 0 {
		// The browser keeps a recent-video exclude list so normal refreshes do not
		// repeat too quickly. On small libraries that list can cover every visible
		// video; when that happens, start a new random round instead of returning
		// an empty home section.
		roundExclude := videoIDs(items)
		fallback, err := s.Catalog.RandomVideosWithReadyThumbnailsExcluding(r.Context(), roundExclude, homePageSize-len(items))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		items = appendUniqueVideos(items, fallback, homePageSize)
	}
	if len(items) < homePageSize && len(excludeIDs) > 0 {
		fallback, err := s.Catalog.RandomVideosExcluding(r.Context(), videoIDs(items), homePageSize-len(items))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		items = appendUniqueVideos(items, fallback, homePageSize)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, mapVideos(items))
}

func parseVideoIDQuery(r *http.Request, key string, limit int) []string {
	if r == nil {
		return nil
	}
	values := r.URL.Query()[key]
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		for _, id := range strings.Split(value, ",") {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
			if limit > 0 && len(out) >= limit {
				return out
			}
		}
	}
	return out
}

func appendUniqueVideos(dst []*catalog.Video, candidates []*catalog.Video, limit int) []*catalog.Video {
	if len(dst) >= limit {
		return dst[:limit]
	}
	seen := make(map[string]struct{}, len(dst))
	for _, v := range dst {
		if v != nil {
			seen[v.ID] = struct{}{}
		}
	}
	for _, v := range candidates {
		if v == nil {
			continue
		}
		if _, ok := seen[v.ID]; ok {
			continue
		}
		dst = append(dst, v)
		seen[v.ID] = struct{}{}
		if len(dst) >= limit {
			return dst
		}
	}
	return dst
}

func videoIDs(items []*catalog.Video) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item != nil && item.ID != "" {
			out = append(out, item.ID)
		}
	}
	return out
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("size"))
	if size <= 0 {
		size = 24
	}
	sort := q.Get("sort")
	params := catalog.ListParams{
		Keyword:   q.Get("q"),
		Tag:       q.Get("tag"),
		Category:  q.Get("cat"),
		MediaType: q.Get("media_type"),
		Sort:      sort,
		Page:      page,
		PageSize:  size,
		SkipTotal: strings.EqualFold(q.Get("count"), "false"),
	}
	if sort == "" || sort == "latest" {
		params.PreferReadyThumbnails = true
	}
	items, total, err := s.Catalog.ListVideos(r.Context(), params)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": mapVideos(items),
		"total": total,
		"page":  params.Page,
		"size":  params.PageSize,
	})
}

// handleUnifiedSearch 是顶栏搜索的入口。同时返回本地视频 + 资源站结果。
//
// Query params:
//
//	q=<keyword>                       必填
//	include=local,external            默认两个都返回
//	type=video|audio                  可选，按媒体类型过滤本地视频
//	limit=<n>                         本地视频条数（默认 6，最大 100）
//	offset=<n>                        本地视频偏移（默认 0）
//
// Response shape（向后兼容，老字段 local/remote 保留用于统计面板）：
//
//	items     []unifiedSearchItem
//	total     int                     本地命中总数 + 外部命中条数
//	took_ms   int                     服务端处理耗时（毫秒）
//	query     string
//	local     int                     本地命中条数（<= len(items) 上限）
//	remote    int                     外部命中条数
//
// 本地视频搜索由 SQLite FTS5 索引驱动（videos_fts），外部源走 runAnimeSearch。
func (s *Server) handleUnifiedSearch(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("q is required"))
		return
	}
	if utf8.RuneCountInString(q) > 50 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("q too long (max 50 chars)"))
		return
	}
	include := r.URL.Query().Get("include")
	wantLocal := include == "" || strings.Contains(include, "local")
	wantExternal := include == "" || strings.Contains(include, "external")

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 6
	}
	if limit > 100 {
		limit = 100
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	mediaType := r.URL.Query().Get("type")

	ctx := r.Context()
	items := []unifiedSearchItem{}
	var localCount, remoteCount, localTotal int

	if wantLocal && s.Catalog != nil {
		// 本地视频走 FTS5 索引（videos_fts），按 bm25 排序。
		// SearchVideos 内部已经过滤 hidden=0；count 用独立的 COUNT(*) 拿真实总数。
		videos, total, err := s.Catalog.SearchVideos(ctx, q, catalog.SearchOptions{
			MediaType: mediaType,
			Limit:     limit,
			Offset:    offset,
		})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		localTotal = total
		for _, v := range videos {
			if v == nil {
				continue
			}
			items = append(items, unifiedSearchItem{
				Type:            "video",
				ID:              v.ID,
				Title:           v.Title,
				Subtitle:        v.Author,
				Cover:           v.ThumbnailURL,
				Href:            "/video/" + pathSegment(v.ID),
				Source:          "local",
				ProgressSeconds: v.ProgressSeconds,
			})
			localCount++
		}
		// 本地小说（FTS 不覆盖小说，这里沿用 LIKE 模糊匹配；体量小不影响性能）
		if mediaType == "" { // type 过滤对小说没意义，跳过
			novels, _, _ := s.Catalog.ListNovelSets(ctx, catalog.ListNovelSetsParams{
				Page: 1, PageSize: 6, Sort: "latest", Tag: "",
			})
			for _, n := range novels {
				if n == nil {
					continue
				}
				titleLow := strings.ToLower(n.Title + n.Author)
				if !strings.Contains(titleLow, strings.ToLower(q)) {
					continue
				}
				items = append(items, unifiedSearchItem{
					Type:     "novel",
					ID:       n.ID,
					Title:    n.Title,
					Subtitle: n.Author,
					Cover:    n.CoverURL,
					Href:     "/novel/" + n.ID,
					Source:   "local",
				})
				localCount++
			}
		}
	}

	if wantExternal && s.Catalog != nil {
		// 复用 /api/anime/search 的同款逻辑（资源站 + parse sources）
		// 这里用 SearchAnimeSearch 的实现，避免重复代码
		animeItems := s.runAnimeSearch(ctx, q)
		items = append(items, animeItems...)
		remoteCount += len(animeItems)
	}

	took := time.Since(start).Milliseconds()
	writeJSON(w, http.StatusOK, map[string]any{
		"items":   items,
		"total":   localTotal + remoteCount,
		"took_ms": took,
		"query":   q,
		"local":   localCount,
		"remote":  remoteCount,
	})
}

// runAnimeSearch 抽出 /api/anime/search 的核心逻辑（本地 + 资源站）。
// 返回 unifiedSearchItem 列表。
func (s *Server) runAnimeSearch(ctx context.Context, kw string) []unifiedSearchItem {
	out := []unifiedSearchItem{}
	if s.Catalog == nil {
		return out
	}
	// parse sources (source) - 跳到外站搜索结果页
	sources, err := s.Catalog.ListParseSources(ctx, true)
	if err == nil {
		for _, src := range sources {
			if !src.SupportsSearch() {
				continue
			}
			out = append(out, unifiedSearchItem{
				Type:     "source",
				ID:       src.ID,
				Title:    src.Name,
				Subtitle: "在新标签页打开搜索结果 · " + kw,
				URL:      src.ExpandSearchURL(kw),
				Source:   src.Name,
			})
		}
	}
	// 资源站
	rsSites, err := s.Catalog.ListResourceSites(ctx, true)
	if err == nil && len(rsSites) > 0 {
		rsResults := resourcesearch.Search(ctx, rsSites, kw, resourcesearch.DefaultFetchOptions())
		for _, rs := range rsResults {
			subtitle := rs.SiteName
			if rs.Year != "" {
				subtitle += " · " + rs.Year
			}
			if rs.Remarks != "" {
				subtitle += " · " + rs.Remarks
			}
			if rs.DirectPlay {
				if subtitle != "" {
					subtitle += " · "
				}
				subtitle += "直链可播"
			}
			out = append(out, unifiedSearchItem{
				Type:       "resource",
				ID:         rs.SiteID + ":" + rs.VodID,
				Title:      rs.Title,
				Subtitle:   subtitle,
				Cover:      rs.Cover,
				URL:        rs.URL,
				Source:     rs.SiteName,
				DirectPlay: rs.DirectPlay,
				SiteID:     rs.SiteID,
				VodID:      rs.VodID,
			})
		}
	}
	return out
}

type unifiedSearchItem struct {
	Type            string  `json:"type"` // video | novel | source | resource
	ID              string  `json:"id"`
	Title           string  `json:"title"`
	Subtitle        string  `json:"subtitle,omitempty"`
	Cover           string  `json:"cover,omitempty"`
	Href            string  `json:"href,omitempty"`
	URL             string  `json:"url,omitempty"`
	Source          string  `json:"source,omitempty"`
	DirectPlay      bool    `json:"directPlay,omitempty"`
	SiteID          string  `json:"siteId,omitempty"`
	VodID           string  `json:"vodId,omitempty"`
	ProgressSeconds float64 `json:"progressSeconds,omitempty"`
}

func (s *Server) handleVideoDetail(w http.ResponseWriter, r *http.Request) {
	id := routeParam(r, "id")
	v, err := s.Catalog.GetVideo(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if v.Hidden {
		writeErr(w, http.StatusNotFound, sql.ErrNoRows)
		return
	}
	if v.DriveID != localUploadDriveID {
		if _, err := s.Catalog.GetDrive(r.Context(), v.DriveID); err != nil {
			drives, listErr := s.Catalog.ListDrives(r.Context())
			if listErr != nil || len(drives) > 0 {
				writeErr(w, http.StatusNotFound, sql.ErrNoRows)
				return
			}
		}
	}
	related := s.pickRelatedVideos(r.Context(), v, 6)
	dto := mapVideo(v)
	if d, err := s.Catalog.GetDrive(r.Context(), v.DriveID); err == nil {
		dto.SourceLabel = driveKindLabel(d.Kind)
	}

	detail := VideoDetailDTO{
		VideoDTO:    dto,
		MediaSrc:    s.videoSource(v),
		VideoSrc:    s.videoSource(v),
		Poster:      displayPosterURL(v),
		Description: v.Description,
		EmbedURL:    fmt.Sprintf(`<iframe src="/embed/%s" width="640" height="360" frameborder="0" allowfullscreen></iframe>`, pathSegment(v.ID)),
		AuthorProfile: AuthorProfile{
			ID:     "author-" + v.Author,
			Name:   v.Author,
			Href:   "/author/" + v.Author,
			Badges: []string{},
		},
		RelatedVideos: mapVideos(related),
		CommentsList:  []Comment{},
	}
	// 推荐每次随机生成，禁止浏览器和中间层缓存详情响应
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, detail)
}

// pickRelatedVideos 选 total 个推荐视频。
// 一半来自同标签命中，剩下用全库随机补齐；两段都优先取已有封面的视频，
// 不够时再回退到未生成封面的候选。结果不会重复，也不会包含当前视频。
// mediaType 限制候选池的媒体类型（视频详情只推视频，音频详情只推音频）。
func (s *Server) pickRelatedVideos(ctx context.Context, current *catalog.Video, total int) []*catalog.Video {
	if total <= 0 || current == nil {
		return nil
	}
	mt := mediatype.Normalize(current.MediaType)
	tagQuota := total / 2
	if tagQuota <= 0 && len(current.Tags) > 0 {
		tagQuota = 1
	}

	picked := make([]*catalog.Video, 0, total)
	seen := map[string]struct{}{current.ID: {}}

	// 1) 同标签候选：先取已有封面的候选，数量不够再从全部候选里补。
	if tagQuota > 0 && len(current.Tags) > 0 {
		picked = appendRandomRelated(
			picked,
			s.relatedTagPool(ctx, current.Tags, seen, true, mt),
			tagQuota,
			seen,
		)
		if len(picked) < tagQuota {
			picked = appendRandomRelated(
				picked,
				s.relatedTagPool(ctx, current.Tags, seen, false, mt),
				tagQuota,
				seen,
			)
		}
	}

	// 2) 随机补齐：同样优先已有封面的全库候选，不够再回退。
	if len(picked) < total {
		picked = appendRandomRelated(
			picked,
			s.relatedListPool(ctx, seen, true, 200, mt),
			total,
			seen,
		)
	}
	if len(picked) < total {
		picked = appendRandomRelated(
			picked,
			s.relatedListPool(ctx, seen, false, 200, mt),
			total,
			seen,
		)
	}

	return picked
}

func (s *Server) relatedTagPool(ctx context.Context, tags []string, seen map[string]struct{}, readyOnly bool, mediaType string) []*catalog.Video {
	var pool []*catalog.Video
	poolSeen := make(map[string]struct{})
	for _, tag := range tags {
		if tag == "" {
			continue
		}
		items, _, err := s.Catalog.ListVideos(ctx, catalog.ListParams{
			Tag:                   tag,
			Sort:                  "latest",
			Page:                  1,
			PageSize:              30,
			ThumbnailReadyOnly:    readyOnly,
			PreferReadyThumbnails: !readyOnly,
			MediaType:             mediaType,
		})
		if err != nil {
			continue
		}
		for _, v := range items {
			if v == nil {
				continue
			}
			if _, ok := seen[v.ID]; ok {
				continue
			}
			if _, ok := poolSeen[v.ID]; ok {
				continue
			}
			poolSeen[v.ID] = struct{}{}
			pool = append(pool, v)
		}
	}
	return pool
}

func (s *Server) relatedListPool(ctx context.Context, seen map[string]struct{}, readyOnly bool, pageSize int, mediaType string) []*catalog.Video {
	items, _, err := s.Catalog.ListVideos(ctx, catalog.ListParams{
		Sort:                  "latest",
		Page:                  1,
		PageSize:              pageSize,
		ThumbnailReadyOnly:    readyOnly,
		PreferReadyThumbnails: !readyOnly,
		MediaType:             mediaType,
	})
	if err != nil {
		return nil
	}
	pool := make([]*catalog.Video, 0, len(items))
	for _, v := range items {
		if v == nil {
			continue
		}
		if _, ok := seen[v.ID]; ok {
			continue
		}
		pool = append(pool, v)
	}
	return pool
}

func appendRandomRelated(picked []*catalog.Video, pool []*catalog.Video, targetLen int, seen map[string]struct{}) []*catalog.Video {
	if len(picked) >= targetLen || len(pool) == 0 {
		return picked
	}
	rand.Shuffle(len(pool), func(i, j int) {
		pool[i], pool[j] = pool[j], pool[i]
	})
	for _, v := range pool {
		if len(picked) >= targetLen {
			break
		}
		if v == nil {
			continue
		}
		if _, ok := seen[v.ID]; ok {
			continue
		}
		seen[v.ID] = struct{}{}
		picked = append(picked, v)
	}
	return picked
}

func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	s.tagCacheMu.Lock()
	if s.tagCache != nil && now.Before(s.tagCacheUntil) {
		out := append([]TagDTO(nil), s.tagCache...)
		s.tagCacheMu.Unlock()
		w.Header().Set("Cache-Control", "private, max-age=15")
		writeJSON(w, http.StatusOK, out)
		return
	}
	s.tagCacheMu.Unlock()

	stats, err := s.Catalog.ListTags(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]TagDTO, 0, len(stats))
	for _, stat := range stats {
		out = append(out, TagDTO{ID: stat.Label, Label: stat.Label, Count: stat.Count})
	}
	s.tagCacheMu.Lock()
	s.tagCache = append([]TagDTO(nil), out...)
	s.tagCacheUntil = now.Add(30 * time.Second)
	s.tagCacheMu.Unlock()

	w.Header().Set("Cache-Control", "private, max-age=15")
	writeJSON(w, http.StatusOK, out)
}

// shortsNextReq 客户端把当前轮已看过的 video id 列表传上来。
type shortsNextReq struct {
	SeenIDs []string `json:"seenIds"`
	Count   int      `json:"count"`
}

// ShortsItemDTO 是短视频流单条的精简结构。比 VideoDTO 多 videoSrc / poster，
// 方便前端直接喂给 <video>，不必再为每条请求 /api/video/:id。
type ShortsItemDTO struct {
	VideoDTO
	VideoSrc string `json:"videoSrc"`
	Poster   string `json:"poster"`
}

// handleShortsNext 为短视频模式提供"不重复随机视频"接口。
//
// 行为：
//   - 入参 seenIds 为客户端当前轮已看过的视频 id（来自 localStorage）
//   - 服务器从未在 seenIds 中的可见视频里随机抽至多 count 条返回
//   - 当返回数量 < count 且小于全库可见总数时，说明本轮即将结束，
//     返回 roundComplete=true，前端应在用户看完返回的这些后清空本地已看记录开新一轮
//   - 当 seenIds 真实覆盖当前全部可见视频时，本接口直接返回新一轮的随机一批
//     （不能仅看 seenIds 长度，里面可能有隐藏、删除或历史脏 ID）
func (s *Server) handleShortsNext(w http.ResponseWriter, r *http.Request) {
	var body shortsNextReq
	// seenIds 是客户端 localStorage 拼出来的 id 列表，单条 ~32B × 几百个条也才几 KB；
	// 设 16KB 上限防恶意/异常客户端塞巨型 body 让服务端无脑分配。
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	count := body.Count
	if count <= 0 {
		count = 5
	}
	if count > 20 {
		count = 20
	}

	total, err := s.Catalog.CountVisibleVideos(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	items, err := s.Catalog.RandomVideosExcluding(r.Context(), body.SeenIDs, count)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if total > 0 && len(items) == 0 && len(body.SeenIDs) > 0 {
		items, err = s.Catalog.RandomVideosExcluding(r.Context(), nil, count)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}

	// 注入 sourceLabel 以便前端展示来源网盘
	driveLabels := make(map[string]string)
	out := make([]ShortsItemDTO, 0, len(items))
	for _, v := range items {
		dto := mapVideo(v)
		if label, ok := driveLabels[v.DriveID]; ok {
			dto.SourceLabel = label
		} else if d, err := s.Catalog.GetDrive(r.Context(), v.DriveID); err == nil {
			label := driveKindLabel(d.Kind)
			driveLabels[v.DriveID] = label
			dto.SourceLabel = label
		}
		out = append(out, ShortsItemDTO{
			VideoDTO: dto,
			VideoSrc: s.videoSource(v),
			Poster:   thumbnailURL(v),
		})
	}

	// roundComplete: 服务端能给出的视频数小于 count，说明剩余可选已耗尽，
	// 前端把这批播完后应该清空本地 seenIds 开新一轮。
	roundComplete := len(out) < count

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"items":         out,
		"total":         total,
		"roundComplete": roundComplete,
	})
}

type updateVideoTagsReq struct {
	Tags []string `json:"tags"`
}

func (s *Server) handleUpdateVideoTags(w http.ResponseWriter, r *http.Request) {
	id := routeParam(r, "id")
	var body updateVideoTagsReq
	// tags 列表：单 tag 几十字符 × 上百个条也才几 KB；1MB 足够且防巨型 body。
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.Catalog.SetManualVideoTags(r.Context(), id, body.Tags); err != nil {
		if errors.Is(err, catalog.ErrUnknownTag) {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	v, err := s.Catalog.GetVideo(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, mapVideo(v))
}

func (s *Server) handleLike(w http.ResponseWriter, r *http.Request) {
	id := routeParam(r, "id")
	likes, err := s.Catalog.IncrementLike(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"likes": likes})
}

// handleUnlike 取消点赞：likes - 1（保底 0）。
// 短视频模式中爱心按钮点击切换状态时使用。
func (s *Server) handleUnlike(w http.ResponseWriter, r *http.Request) {
	id := routeParam(r, "id")
	likes, err := s.Catalog.DecrementLike(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"likes": likes})
}

func (s *Server) handleView(w http.ResponseWriter, r *http.Request) {
	id := routeParam(r, "id")
	views, err := s.Catalog.IncrementView(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"views": views})
}

// handleProgress 接收客户端定时上报的播放进度（秒）。可重复调用，仅写 DB。
func (s *Server) handleProgress(w http.ResponseWriter, r *http.Request) {
	id := routeParam(r, "id")
	var req struct {
		Seconds float64 `json:"seconds"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid json: %w", err))
		return
	}
	if req.Seconds < 0 {
		req.Seconds = 0
	}
	if err := s.Catalog.UpdateProgress(r.Context(), id, req.Seconds); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"seconds": req.Seconds})
}

// handleContinueWatching 返回"看了一半"的最近 10 部视频。
func (s *Server) handleContinueWatching(w http.ResponseWriter, r *http.Request) {
	if s.Catalog == nil {
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("catalog not ready"))
		return
	}
	items, err := s.Catalog.ListContinueWatching(r.Context(), 10)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleHideVideo(w http.ResponseWriter, r *http.Request) {
	id := routeParam(r, "id")
	var err error
	if s.OnHideVideo != nil {
		// 走拉黑逻辑：删记录 + 删本地封面/预览 + 写墓碑，保留网盘源文件。
		err = s.OnHideVideo(r.Context(), id)
	} else {
		err = s.Catalog.HideVideo(r.Context(), id)
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleUploadVideo(w http.ResponseWriter, r *http.Request) {
	if s.LocalDir == "" {
		writeErr(w, http.StatusInternalServerError, errors.New("local storage is not configured"))
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("media file is required"))
		return
	}
	defer file.Close()

	originalName := filepath.Base(strings.TrimSpace(header.Filename))
	ext := strings.ToLower(filepath.Ext(originalName))
	if _, ok := allowedUploadExtensions[ext]; !ok {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("unsupported media extension: %s", ext))
		return
	}
	mediaType := mediatype.FromExtension(ext)

	tags, err := parseUploadTags(uploadTagValues(r))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	now := time.Now()
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		title = uploadTitleFromFileName(originalName)
	}

	uploadID, err := newUploadID(now)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	storedName := uploadID + ext
	dst, err := s.localUploadFilePath(storedName)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	size, copyErr := io.Copy(out, file)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(dst)
		writeErr(w, http.StatusInternalServerError, copyErr)
		return
	}
	if closeErr != nil {
		_ = os.Remove(dst)
		writeErr(w, http.StatusInternalServerError, closeErr)
		return
	}
	if size <= 0 {
		_ = os.Remove(dst)
		writeErr(w, http.StatusBadRequest, errors.New("uploaded file is empty"))
		return
	}

	previewStatus := "pending"
	if mediaType == mediatype.Audio {
		previewStatus = "disabled"
	}

	video := &catalog.Video{
		ID:            localUploadDriveID + "-" + uploadID,
		DriveID:       localUploadDriveID,
		FileID:        storedName,
		FileName:      originalName,
		Title:         title,
		Author:        "用户上传",
		Tags:          tags,
		Size:          size,
		Ext:           strings.TrimPrefix(ext, "."),
		MediaType:     mediaType,
		PreviewStatus: previewStatus,
		PublishedAt:   now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.Catalog.UpsertVideo(r.Context(), video); err != nil {
		_ = os.Remove(dst)
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if s.OnVideoUploaded != nil {
		s.OnVideoUploaded(video)
	}
	writeJSON(w, http.StatusCreated, mapVideo(video))
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	driveID := routeParam(r, "driveID")
	fileID := routeWildcardParam(r, "*")
	s.Proxy.ServeStream(w, r, driveID, fileID)
}
func (s *Server) handleUploadedVideo(w http.ResponseWriter, r *http.Request) {
	videoID := routeParam(r, "videoID")
	v, err := s.Catalog.GetVideo(r.Context(), videoID)
	if err != nil || v.Hidden || v.DriveID != localUploadDriveID {
		http.NotFound(w, r)
		return
	}
	path, err := s.localUploadFilePath(v.FileID)
	if err != nil {
		http.Error(w, "invalid upload file", http.StatusForbidden)
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() == 0 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	http.ServeFile(w, r, path)
}

// handleSpider91Video 服务 spider91 drive 下载到本地的视频文件。
// 路径形如 /p/spider91/<videoID>，videoID = "spider91-<driveID>-<sourceID>"。
// 通过 catalog 拿到 file_id（"<sourceID>.mp4"），再让 driver 解析到绝对路径并 ServeFile。
func (s *Server) handleSpider91Video(w http.ResponseWriter, r *http.Request) {
	videoID := routeParam(r, "videoID")
	v, err := s.Catalog.GetVideo(r.Context(), videoID)
	if err != nil || v.Hidden {
		http.NotFound(w, r)
		return
	}
	if s.Proxy == nil || s.Proxy.Registry == nil {
		http.NotFound(w, r)
		return
	}
	d, ok := s.Proxy.Registry.Get(v.DriveID)
	if !ok || d.Kind() != spider91.Kind {
		http.NotFound(w, r)
		return
	}
	sd, ok := d.(*spider91.Driver)
	if !ok {
		http.NotFound(w, r)
		return
	}
	path, err := sd.VideoPath(v.FileID)
	if err != nil {
		http.Error(w, "invalid video id", http.StatusForbidden)
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() == 0 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	http.ServeFile(w, r, path)
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	videoID := routeParam(r, "videoID")
	v, err := s.Catalog.GetVideo(r.Context(), videoID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if v.PreviewStatus != "ready" {
		http.Error(w, "preview not ready", http.StatusNotFound)
		return
	}
	if v.PreviewLocal != "" {
		if !strings.HasPrefix(filepath.Clean(v.PreviewLocal), filepath.Clean(s.LocalDir)) {
			http.Error(w, "invalid local path", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		s.Proxy.ServeLocal(w, r, v.PreviewLocal)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handleThumb(w http.ResponseWriter, r *http.Request) {
	videoID := routeParam(r, "videoID")
	var clean string
	for _, path := range mediaasset.ThumbnailPathCandidates(s.LocalDir, videoID) {
		candidate := filepath.Clean(path)
		if !strings.HasPrefix(candidate, filepath.Clean(s.LocalDir)) {
			http.Error(w, "invalid path", http.StatusForbidden)
			return
		}
		if _, err := os.Stat(candidate); err == nil {
			clean = candidate
			break
		}
	}
	if clean == "" {
		w.Header().Set("Cache-Control", "no-store")
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	s.Proxy.ServeLocal(w, r, clean)
}

// ---------- helpers ----------

func mapVideo(v *catalog.Video) VideoDTO {
	badges := v.Badges
	if badges == nil {
		badges = []string{}
	}
	tags := v.Tags
	if tags == nil {
		tags = []string{}
	}
	return VideoDTO{
		ID:              v.ID,
		MediaType:       mediatype.Normalize(v.MediaType),
		Href:            "/video/" + pathSegment(v.ID),
		Title:           v.Title,
		Thumbnail:       displayThumbnailURL(v),
		PreviewSrc:      previewMediaURL(v),
		PreviewDuration: 12,
		PreviewStrategy: "teaser-file",
		Duration:        formatDuration(v.DurationSeconds),
		DurationSeconds: v.DurationSeconds,
		ProgressSeconds: v.ProgressSeconds,
		Badges:          badges,
		Quality:         v.Quality,
		Author:          v.Author,
		Views:           v.Views,
		Favorites:       v.Favorites,
		Comments:        v.Comments,
		Likes:           v.Likes,
		Dislikes:        v.Dislikes,
		PublishedAt:     v.PublishedAt.Format("2006-01-02"),
		Tags:            tags,
		Category:        v.Category,
	}
}

func previewURL(v *catalog.Video) string {
	base := "/p/preview/" + pathSegment(v.ID)
	if v.UpdatedAt.IsZero() {
		return base
	}
	return base + "?v=" + strconv.FormatInt(v.UpdatedAt.UnixMilli(), 10)
}

func previewMediaURL(v *catalog.Video) string {
	if mediatype.Normalize(v.MediaType) != mediatype.Video {
		return ""
	}
	return previewURL(v)
}

func thumbnailURL(v *catalog.Video) string {
	base := "/p/thumb/" + pathSegment(v.ID)
	if v.ThumbnailURL != "" {
		base = v.ThumbnailURL
		if thumbnailURLMatchesVideoID(base, v.ID) {
			base = "/p/thumb/" + pathSegment(v.ID)
		}
	}
	if !strings.HasPrefix(base, "/p/thumb/") || v.UpdatedAt.IsZero() {
		return base
	}
	return base + "?v=" + strconv.FormatInt(v.UpdatedAt.UnixMilli(), 10)
}

func displayThumbnailURL(v *catalog.Video) string {
	if mediatype.Normalize(v.MediaType) == mediatype.Audio && strings.TrimSpace(v.ThumbnailURL) == "" {
		return ""
	}
	return thumbnailURL(v)
}

func displayPosterURL(v *catalog.Video) string {
	return displayThumbnailURL(v)
}

// transcodedSource 在视频有就绪的浏览器兼容性转码产物时返回产物的播放地址。
// 产物和原始文件在同一个 drive 上，走同一条 /p/stream 代理/302 链路。
func transcodedSource(v *catalog.Video) (string, bool) {
	if v.TranscodeStatus == "ready" && v.TranscodedFileID != "" && v.DriveID != localUploadDriveID {
		return fmt.Sprintf("/p/stream/%s/%s", pathSegment(v.DriveID), pathSegment(v.TranscodedFileID)), true
	}
	return "", false
}

func (s *Server) videoSource(v *catalog.Video) string {
	if v.DriveID == localUploadDriveID {
		return "/p/upload/" + pathSegment(v.ID)
	}
	if s.Proxy != nil && s.Proxy.Registry != nil {
		if d, ok := s.Proxy.Registry.Get(v.DriveID); ok {
			switch d.Kind() {
			case spider91.Kind:
				return "/p/spider91/" + pathSegment(v.ID)
			}
		}
	}
	if src, ok := transcodedSource(v); ok {
		return src
	}
	return fmt.Sprintf("/p/stream/%s/%s", pathSegment(v.DriveID), pathSegment(v.FileID))
}

// videoSource 兼容旧调用点，没有 server context 时按之前逻辑回退到 /p/stream。
// 内部新增的代码请使用 (*Server).videoSource。
func videoSource(v *catalog.Video) string {
	if v.DriveID == localUploadDriveID {
		return "/p/upload/" + pathSegment(v.ID)
	}
	if src, ok := transcodedSource(v); ok {
		return src
	}
	return fmt.Sprintf("/p/stream/%s/%s", pathSegment(v.DriveID), pathSegment(v.FileID))
}

func pathSegment(value string) string {
	return url.PathEscape(value)
}

func routeParam(r *http.Request, key string) string {
	value := chi.URLParam(r, key)
	if value == "" {
		return ""
	}
	if decoded, err := url.PathUnescape(value); err == nil {
		return decoded
	}
	return value
}

func routeWildcardParam(r *http.Request, key string) string {
	value := chi.URLParam(r, key)
	if value == "" {
		return ""
	}
	value = strings.TrimPrefix(value, "/")
	if decoded, err := url.PathUnescape(value); err == nil {
		return decoded
	}
	return value
}

func thumbnailURLMatchesVideoID(value, videoID string) bool {
	if !strings.HasPrefix(value, "/p/thumb/") {
		return false
	}
	tail := strings.TrimPrefix(value, "/p/thumb/")
	if idx := strings.IndexByte(tail, '?'); idx >= 0 {
		tail = tail[:idx]
	}
	if tail == videoID {
		return true
	}
	decoded, err := url.PathUnescape(tail)
	return err == nil && decoded == videoID
}

func driveKindLabel(kind string) string {
	switch kind {
	case "quark":
		return "夸克网盘"
	case "p115":
		return "115 网盘"
	case "p123":
		return "123网盘"
	case "pikpak":
		return "PikPak"
	case "wopan":
		return "联通网盘"
	case "guangyapan":
		return "光鸭网盘"
	case "onedrive":
		return "OneDrive"
	case "googledrive":
		return "Google Drive"
	case localstorage.Kind:
		return "本地存储"
	case spider91.Kind:
		return "61 爬虫"
	default:
		return kind
	}
}

func (s *Server) localUploadFilePath(fileID string) (string, error) {
	if strings.TrimSpace(fileID) == "" || filepath.Base(fileID) != fileID {
		return "", errors.New("invalid upload file id")
	}
	root := s.localUploadDir()
	if root == "" {
		return "", errors.New("local upload storage is not configured")
	}
	path := filepath.Join(root, fileID)
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	cleanPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if cleanPath != cleanRoot && !strings.HasPrefix(cleanPath, cleanRoot+string(os.PathSeparator)) {
		return "", errors.New("invalid upload file id")
	}
	return cleanPath, nil
}

func (s *Server) localUploadDir() string {
	if s.UploadDir != "" {
		return s.UploadDir
	}
	if s.LocalDir == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(s.LocalDir), "uploads")
}

func uploadTagValues(r *http.Request) []string {
	if r.MultipartForm == nil {
		return nil
	}
	values := append([]string{}, r.MultipartForm.Value["tags"]...)
	values = append(values, r.MultipartForm.Value["tag"]...)
	return values
}

func uploadTitleFromFileName(fileName string) string {
	name := strings.TrimSpace(filepath.Base(fileName))
	ext := filepath.Ext(name)
	if ext != "" {
		if trimmed := strings.TrimSuffix(name, ext); strings.TrimSpace(trimmed) != "" {
			return trimmed
		}
	}
	if name != "" {
		return name
	}
	return "upload-" + time.Now().Format("20060102150405")
}

func parseUploadTags(values []string) ([]string, error) {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(values))
	for _, value := range values {
		for _, label := range splitUploadTags(value) {
			if _, ok := allowedUploadTags[label]; !ok {
				return nil, fmt.Errorf("unsupported upload tag: %s", label)
			}
			if _, ok := seen[label]; ok {
				continue
			}
			seen[label] = struct{}{}
			out = append(out, label)
		}
	}
	return out, nil
}

func splitUploadTags(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		switch r {
		case ',', '，', ';', '；', '\n', '\r', '\t', ' ':
			return true
		default:
			return false
		}
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if label := strings.TrimSpace(field); label != "" {
			out = append(out, label)
		}
	}
	return out
}

func newUploadID(now time.Time) (string, error) {
	var suffix [6]byte
	if _, err := crand.Read(suffix[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("upload-%d-%s", now.UnixNano(), hex.EncodeToString(suffix[:])), nil
}

func mapVideos(vs []*catalog.Video) []VideoDTO {
	out := make([]VideoDTO, 0, len(vs))
	for _, v := range vs {
		out = append(out, mapVideo(v))
	}
	return out
}

func formatDuration(sec int) string {
	if sec <= 0 {
		return "00:00"
	}
	m := sec / 60
	s := sec % 60
	return fmt.Sprintf("%02d:%02d", m, s)
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

// ---------- gallery handlers ----------

// ImageSetDTO 是返回给前端的图集对象。
type ImageSetDTO struct {
	ID          string   `json:"id"`
	DriveID     string   `json:"driveId"`
	SourceID    string   `json:"sourceId"`
	Title       string   `json:"title"`
	Author      string   `json:"author"`
	CoverURL    string   `json:"coverUrl"`
	ImageCount  int      `json:"imageCount"`
	Tags        []string `json:"tags"`
	Description string   `json:"description"`
	Hidden      bool     `json:"hidden"`
	SourceKind  string   `json:"sourceKind"`
	PublishedAt int64    `json:"publishedAt"`
	CreatedAt   int64    `json:"createdAt"`
	UpdatedAt   int64    `json:"updatedAt"`
}

// ImageSetDetailDTO 是返回给前端的图集详情（含图片列表）。
type ImageSetDetailDTO struct {
	ImageSetDTO
	Images []ImageSetItemDTO `json:"images"`
}

// ImageSetItemDTO 图集中单张图片。
type ImageSetItemDTO struct {
	Position int               `json:"position"`
	URL      string            `json:"url"`
	ThumbURL string            `json:"thumbUrl,omitempty"`
	Width    int               `json:"width,omitempty"`
	Height   int               `json:"height,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
}

func (s *Server) handleGalleries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("size"))
	if size <= 0 {
		size = 24
	}
	sort := q.Get("sort")
	tag := q.Get("tag")
	items, total, err := s.Catalog.ListImageSets(r.Context(), catalog.ListImageSetsParams{
		Page:     page,
		PageSize: size,
		Sort:     sort,
		Tag:      tag,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": mapImageSets(items),
		"total": total,
		"page":  page,
		"size":  size,
	})
}

func (s *Server) handleGalleryDetail(w http.ResponseWriter, r *http.Request) {
	id := routeParam(r, "id")
	gs, err := s.Catalog.GetImageSet(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if gs.Hidden {
		writeErr(w, http.StatusNotFound, fmt.Errorf("not found"))
		return
	}
	detail := ImageSetDetailDTO{
		ImageSetDTO: mapImageSet(gs),
		Images:      mapImageSetItems(gs.Images),
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, detail)
}

func mapImageSet(s *catalog.ImageSet) ImageSetDTO {
	tags := s.Tags
	if tags == nil {
		tags = []string{}
	}
	author := s.Author
	if author == "" {
		author = "未知"
	}
	return ImageSetDTO{
		ID:          s.ID,
		DriveID:     s.DriveID,
		SourceID:    s.SourceID,
		Title:       s.Title,
		Author:      author,
		CoverURL:    s.CoverURL,
		ImageCount:  s.ImageCount,
		Tags:        tags,
		Description: s.Description,
		Hidden:      s.Hidden,
		SourceKind:  s.SourceKind,
		PublishedAt: s.PublishedAt,
		CreatedAt:   s.CreatedAt,
		UpdatedAt:   s.UpdatedAt,
	}
}

func mapImageSets(sets []*catalog.ImageSet) []ImageSetDTO {
	out := make([]ImageSetDTO, 0, len(sets))
	for _, s := range sets {
		out = append(out, mapImageSet(s))
	}
	return out
}

func mapImageSetItems(items []catalog.ImageSetItem) []ImageSetItemDTO {
	out := make([]ImageSetItemDTO, 0, len(items))
	for _, item := range items {
		out = append(out, ImageSetItemDTO{
			Position: item.Position,
			URL:      item.URL,
			ThumbURL: item.ThumbURL,
			Width:    item.Width,
			Height:   item.Height,
			Headers:  item.Headers,
		})
	}
	return out
}

// ---------- 小说/PDF 阅读器 ----------

// NovelSetDTO 是返回给前端的小说对象。
type NovelSetDTO struct {
	ID           string   `json:"id"`
	DriveID      string   `json:"driveId"`
	SourceID     string   `json:"sourceId"`
	Title        string   `json:"title"`
	Author       string   `json:"author"`
	CoverURL     string   `json:"coverUrl"`
	ContentType  string   `json:"contentType"`
	ChapterCount int      `json:"chapterCount"`
	Tags         []string `json:"tags"`
	Description  string   `json:"description"`
	Hidden       bool     `json:"hidden"`
	SourceKind   string   `json:"sourceKind"`
	PublishedAt  int64    `json:"publishedAt"`
	CreatedAt    int64    `json:"createdAt"`
	UpdatedAt    int64    `json:"updatedAt"`
}

// NovelSetDetailDTO 是返回给前端的小说详情（含章节列表）。
type NovelSetDetailDTO struct {
	NovelSetDTO
	Chapters []NovelChapterDTO `json:"chapters"`
}

// NovelChapterDTO 小说中单章/单文件。
type NovelChapterDTO struct {
	ID          int64             `json:"id"`
	Position    int               `json:"position"`
	Title       string            `json:"title"`
	ContentType string            `json:"contentType"`
	Body        string            `json:"body,omitempty"`
	PDFURL      string            `json:"pdfUrl,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

func (s *Server) handleNovels(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("size"))
	if size <= 0 {
		size = 24
	}
	sortKey := q.Get("sort")
	tag := q.Get("tag")
	contentType := q.Get("contentType")
	items, total, err := s.Catalog.ListNovelSets(r.Context(), catalog.ListNovelSetsParams{
		Page:        page,
		PageSize:    size,
		Sort:        sortKey,
		Tag:         tag,
		ContentType: contentType,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": mapNovelSets(items),
		"total": total,
		"page":  page,
		"size":  size,
	})
}

func (s *Server) handleNovelDetail(w http.ResponseWriter, r *http.Request) {
	id := routeParam(r, "id")
	ns, err := s.Catalog.GetNovelSet(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if ns.Hidden {
		writeErr(w, http.StatusNotFound, fmt.Errorf("not found"))
		return
	}
	detail := NovelSetDetailDTO{
		NovelSetDTO: mapNovelSet(ns),
		Chapters:    mapNovelChapters(ns.Chapters),
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleNovelChapter(w http.ResponseWriter, r *http.Request) {
	id := routeParam(r, "id")
	posStr := routeParam(r, "position")
	pos, err := strconv.Atoi(posStr)
	if err != nil || pos < 0 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid position"))
		return
	}
	ns, err := s.Catalog.GetNovelSet(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if ns.Hidden {
		writeErr(w, http.StatusNotFound, fmt.Errorf("not found"))
		return
	}
	ch, err := s.Catalog.GetNovelChapter(r.Context(), id, pos)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, mapNovelChapter(*ch))
}

func mapNovelSet(s *catalog.NovelSet) NovelSetDTO {
	tags := s.Tags
	if tags == nil {
		tags = []string{}
	}
	author := s.Author
	if author == "" {
		author = "未知"
	}
	ct := s.ContentType
	if ct == "" {
		ct = "text"
	}
	return NovelSetDTO{
		ID:           s.ID,
		DriveID:      s.DriveID,
		SourceID:     s.SourceID,
		Title:        s.Title,
		Author:       author,
		CoverURL:     s.CoverURL,
		ContentType:  ct,
		ChapterCount: s.ChapterCount,
		Tags:         tags,
		Description:  s.Description,
		Hidden:       s.Hidden,
		SourceKind:   s.SourceKind,
		PublishedAt:  s.PublishedAt,
		CreatedAt:    s.CreatedAt,
		UpdatedAt:    s.UpdatedAt,
	}
}

func mapNovelSets(sets []*catalog.NovelSet) []NovelSetDTO {
	out := make([]NovelSetDTO, 0, len(sets))
	for _, s := range sets {
		out = append(out, mapNovelSet(s))
	}
	return out
}

func mapNovelChapter(ch catalog.NovelChapter) NovelChapterDTO {
	ct := ch.ContentType
	if ct == "" {
		ct = "text"
	}
	return NovelChapterDTO{
		ID:          ch.ID,
		Position:    ch.Position,
		Title:       ch.Title,
		ContentType: ct,
		Body:        ch.Body,
		PDFURL:      ch.PDFURL,
		Headers:     ch.Headers,
	}
}

func mapNovelChapters(chapters []catalog.NovelChapter) []NovelChapterDTO {
	out := make([]NovelChapterDTO, 0, len(chapters))
	for _, ch := range chapters {
		out = append(out, mapNovelChapter(ch))
	}
	return out
}

// createNovelReq 是 POST /api/novels 的入参。
type createNovelReq struct {
	ID          string             `json:"id"`
	Title       string             `json:"title"`
	Author      string             `json:"author"`
	CoverURL    string             `json:"coverUrl"`
	ContentType string             `json:"contentType"` // text | pdf
	Tags        []string           `json:"tags"`
	Description string             `json:"description"`
	Chapters    []createNovelChReq `json:"chapters"`
}

type createNovelChReq struct {
	Position    int               `json:"position"`
	Title       string            `json:"title"`
	ContentType string            `json:"contentType"`
	Body        string            `json:"body"`
	PDFURL      string            `json:"pdfUrl"`
	Headers     map[string]string `json:"headers"`
}

func (s *Server) handleCreateNovel(w http.ResponseWriter, r *http.Request) {
	var req createNovelReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid json: %w", err))
		return
	}
	if strings.TrimSpace(req.ID) == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("id is required"))
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("title is required"))
		return
	}
	if s.Catalog == nil {
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("catalog not ready"))
		return
	}
	ct := strings.TrimSpace(req.ContentType)
	if ct == "" {
		ct = "text"
	}
	if ct != "text" && ct != "pdf" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("contentType must be text or pdf"))
		return
	}
	now := time.Now().UnixMilli()
	tags := req.Tags
	if tags == nil {
		tags = []string{}
	}
	chapters := make([]catalog.NovelChapter, 0, len(req.Chapters))
	for i, c := range req.Chapters {
		chapterCT := strings.TrimSpace(c.ContentType)
		if chapterCT == "" {
			chapterCT = ct
		}
		position := c.Position
		if position == 0 {
			position = i
		}
		chapters = append(chapters, catalog.NovelChapter{
			Position:    position,
			Title:       c.Title,
			ContentType: chapterCT,
			Body:        c.Body,
			PDFURL:      c.PDFURL,
			Headers:     c.Headers,
		})
	}
	ns := &catalog.NovelSet{
		ID:          strings.TrimSpace(req.ID),
		Title:       strings.TrimSpace(req.Title),
		Author:      strings.TrimSpace(req.Author),
		CoverURL:    req.CoverURL,
		ContentType: ct,
		Tags:        tags,
		Description: req.Description,
		SourceKind:  "manual",
		CreatedAt:   now,
		UpdatedAt:   now,
		PublishedAt: now,
		Chapters:    chapters,
	}
	if err := s.Catalog.UpsertNovelSet(r.Context(), ns); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	got, err := s.Catalog.GetNovelSet(r.Context(), ns.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, NovelSetDetailDTO{
		NovelSetDTO: mapNovelSet(got),
		Chapters:    mapNovelChapters(got.Chapters),
	})
}

func (s *Server) handleDeleteNovel(w http.ResponseWriter, r *http.Request) {
	if s.Catalog == nil {
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("catalog not ready"))
		return
	}
	id := routeParam(r, "id")
	if err := s.Catalog.DeleteNovelSet(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- 动漫链接解析 ----------

type animeParseRequest struct {
	URL string `json:"url"`
}

type animeParseResponse struct {
	animeparser.ParseResult
	AvailableParsers []string `json:"availableParsers"`
}

func (s *Server) handleAnimeParse(w http.ResponseWriter, r *http.Request) {
	var req animeParseRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid json: %w", err))
		return
	}
	url := strings.TrimSpace(req.URL)
	if url == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("url is required"))
		return
	}
	// SSRF 防护：scheme 白名单 + IP 黑名单（私网/回环/云元数据）。
	// 比单纯检查 http(s) 前缀更严；错误信息更直观。
	if err := safefetch.ValidateURL(url); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	result, err := animeparser.Parse(r.Context(), url)
	if err != nil {
		// 没有匹配解析器时返回 422（语义上对：请求合法但无法处理）
		if errors.Is(err, animeparser.ErrNoParser) {
			writeErr(w, http.StatusUnprocessableEntity, err)
			return
		}
		writeErr(w, http.StatusBadGateway, err)
		return
	}

	writeJSON(w, http.StatusOK, animeParseResponse{
		ParseResult:      *result,
		AvailableParsers: animeparser.List(),
	})
}

// animeIframeRequest 是 POST /api/anime/iframe 的入参。
type animeIframeRequest struct {
	SourceID string `json:"sourceId"`
	URL      string `json:"url"`
}

// handleAnimeIframe 给 iframe 模式解析源构造完整的 iframe URL。
// 第三方解析站（如 jx.xmflv.com）通常返回带 player 的 HTML 页面，需要前端用
// <iframe> 嵌入。后端不做实际解析，只把 {url} 模板展开。
func (s *Server) handleAnimeIframe(w http.ResponseWriter, r *http.Request) {
	if s.Catalog == nil {
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("catalog not ready"))
		return
	}
	var req animeIframeRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid json: %w", err))
		return
	}
	sourceID := strings.TrimSpace(req.SourceID)
	videoURL := strings.TrimSpace(req.URL)
	if sourceID == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("sourceId is required"))
		return
	}
	if !strings.HasPrefix(videoURL, "http://") && !strings.HasPrefix(videoURL, "https://") {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("url must be http(s)"))
		return
	}
	src, err := s.Catalog.GetParseSource(r.Context(), sourceID)
	if err != nil {
		writeErr(w, http.StatusNotFound, fmt.Errorf("source not found: %s", sourceID))
		return
	}
	if !src.Enabled {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("source is disabled"))
		return
	}
	if !src.IsIframe() {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("source is not iframe mode; use /api/anime/parse instead"))
		return
	}
	iframeURL := src.ExpandParseURL(videoURL)
	writeJSON(w, http.StatusOK, map[string]any{
		"url":    iframeURL,
		"source": src.ID,
		"name":   src.Name,
	})
}

// ---------- 影视搜索 ----------

type animeSearchItem struct {
	Type       string `json:"type"` // "video" | "novel" | "source" | "resource"
	ID         string `json:"id"`   // 视频/小说 ID，或 源 ID
	Title      string `json:"title"`
	Subtitle   string `json:"subtitle,omitempty"` // 作者/集数/资源站
	Cover      string `json:"cover,omitempty"`
	Href       string `json:"href,omitempty"`       // 前台点击跳转的 URL（视频/小说详情页）
	URL        string `json:"url,omitempty"`        // 外部源：需要后续 parse 的 URL
	Source     string `json:"source,omitempty"`     // local / 源名
	DirectPlay bool   `json:"directPlay,omitempty"` // resource 专用：true=直链 m3u8/mp4
	SiteID     string `json:"siteId,omitempty"`     // resource 专用：资源站 ID
	VodID      string `json:"vodId,omitempty"`      // resource 专用：资源站侧 ID（用于拉详情）
}

type animeSearchResponse struct {
	Items  []animeSearchItem `json:"items"`
	Total  int               `json:"total"`
	Query  string            `json:"query"`
	Local  int               `json:"localCount"`
	Remote int               `json:"remoteCount"`
}

// handleAnimeSearch 综合搜索：本地视频 + 本地小说 + 已配置外部源。
func (s *Server) handleAnimeSearch(w http.ResponseWriter, r *http.Request) {
	kw := strings.TrimSpace(r.URL.Query().Get("kw"))
	if kw == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("kw is required"))
		return
	}
	if utf8.RuneCountInString(kw) > 50 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("kw too long (max 50 chars)"))
		return
	}

	ctx := r.Context()
	items := []animeSearchItem{}

	// 1) 本地视频（按标题/作者模糊匹配）
	if s.Catalog != nil {
		videos, _, err := s.Catalog.ListVideos(ctx, catalog.ListParams{
			Keyword:  kw,
			Sort:     "latest",
			Page:     1,
			PageSize: 12,
		})
		if err == nil {
			for _, v := range videos {
				if v == nil {
					continue
				}
				items = append(items, animeSearchItem{
					Type:     "video",
					ID:       v.ID,
					Title:    v.Title,
					Subtitle: v.Author,
					Cover:    v.ThumbnailURL,
					Href:     "/video/" + pathSegment(v.ID),
					Source:   "local",
				})
			}
		}

		// 2) 本地小说
		novels, _, err := s.Catalog.ListNovelSets(ctx, catalog.ListNovelSetsParams{
			Page: 1, PageSize: 12, Sort: "latest", Tag: "",
		})
		if err == nil {
			for _, n := range novels {
				if n == nil || !strings.Contains(strings.ToLower(n.Title+n.Author), strings.ToLower(kw)) {
					continue
				}
				items = append(items, animeSearchItem{
					Type:     "novel",
					ID:       n.ID,
					Title:    n.Title,
					Subtitle: n.Author + " · " + n.ContentType + " · " + strconv.Itoa(n.ChapterCount) + " 章",
					Cover:    n.CoverURL,
					Href:     "/novel/" + n.ID,
					Source:   "local",
				})
			}
		}
	}
	localCount := len(items)

	// 3) 已配置的外部解析源（搜索关键词）
	// 真实实现：每张卡片是一个"外站源"的入口，点击后**新标签页**打开该源的搜索结果页。
	// 用户从外站自己拿详情页 URL，再回到本页面粘到"解析链接" tab 走 parse。
	// 不在服务端做"搜索结果页 HTML 抓取 + 详情页 URL 启发式提取"——那等于把通用提取器
	// 变成多站点爬虫，且质量上限极低（外站结构各异）。
	remoteCount := 0
	if s.Catalog != nil {
		sources, err := s.Catalog.ListParseSources(ctx, true)
		if err == nil {
			for _, src := range sources {
				if !src.SupportsSearch() {
					continue
				}
				items = append(items, animeSearchItem{
					Type:     "source",
					ID:       src.ID,
					Title:    src.Name,
					Subtitle: "在新标签页打开搜索结果 · " + kw,
					URL:      src.ExpandSearchURL(kw),
					Source:   src.Name,
				})
				remoteCount++
			}
		}

		// 4) 资源站（行业标准 JSON API）— 拉每个 enabled 站的标准 JSON，聚合结果。
		// 这是 /anime 搜索的核心：用户搜剧名，资源站直接返回匹配的视频列表（含 m3u8 直链
		// 或详情页 URL），用户点哪个就直接播放或走 parse 流程。
		rsSites, err := s.Catalog.ListResourceSites(ctx, true)
		if err == nil && len(rsSites) > 0 {
			rsResults := resourcesearch.Search(ctx, rsSites, kw, resourcesearch.DefaultFetchOptions())
			for _, rs := range rsResults {
				subtitle := rs.SiteName
				if rs.Year != "" {
					subtitle += " · " + rs.Year
				}
				if rs.Remarks != "" {
					subtitle += " · " + rs.Remarks
				}
				if rs.DirectPlay {
					if subtitle != "" {
						subtitle += " · "
					}
					subtitle += "直链可播"
				}
				items = append(items, animeSearchItem{
					Type:       "resource",
					ID:         rs.SiteID + ":" + rs.VodID,
					Title:      rs.Title,
					Subtitle:   subtitle,
					Cover:      rs.Cover,
					URL:        rs.URL,
					Source:     rs.SiteName,
					DirectPlay: rs.DirectPlay,
					SiteID:     rs.SiteID,
					VodID:      rs.VodID,
				})
				remoteCount++
			}
		}
	}

	writeJSON(w, http.StatusOK, animeSearchResponse{
		Items:  items,
		Total:  len(items),
		Query:  kw,
		Local:  localCount,
		Remote: remoteCount,
	})
}

// handleAnimeResourceDetail 处理 GET /api/anime/resource/detail?site=<id>&vod=<vod_id>
// 拉取资源站详情接口，返回首个 vod_play_url 中的首段 URL。
// 用于点击搜索结果后获取可播放链接。
func (s *Server) handleAnimeResourceDetail(w http.ResponseWriter, r *http.Request) {
	if s.Catalog == nil {
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("catalog not ready"))
		return
	}
	q := r.URL.Query()
	siteID := strings.TrimSpace(q.Get("site"))
	vodID := strings.TrimSpace(q.Get("vod"))
	if siteID == "" || vodID == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("site and vod are required"))
		return
	}
	site, err := s.Catalog.GetResourceSite(r.Context(), siteID)
	if err != nil {
		writeErr(w, http.StatusNotFound, fmt.Errorf("site not found"))
		return
	}
	if !site.Enabled {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("site is disabled"))
		return
	}
	res, err := resourcesearch.FetchDetail(r.Context(), site, vodID, 6*time.Second)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleAnimeSources 返回前台可用的解析源列表（仅 enabled）。
func (s *Server) handleAnimeSources(w http.ResponseWriter, r *http.Request) {
	if s.Catalog == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	sources, err := s.Catalog.ListParseSources(r.Context(), true)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	type item struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Kind      string `json:"kind"`
		CanSearch bool   `json:"canSearch"`
		CanParse  bool   `json:"canParse"`
		IsIframe  bool   `json:"isIframe"`
		SearchURL string `json:"searchUrl,omitempty"`
		ParseURL  string `json:"parseUrl,omitempty"`
		Note      string `json:"note"`
	}
	out := make([]item, 0, len(sources)+1)
	out = append(out, item{
		ID: "universal", Name: "通用（HTML 兜底）", Kind: "parse",
		CanSearch: false, CanParse: true, Note: "从 <video>/<source>/<iframe> 抽取",
	})
	for _, s := range sources {
		out = append(out, item{
			ID: s.ID, Name: s.Name, Kind: s.Kind,
			CanSearch: s.SupportsSearch(), CanParse: s.SupportsParse(),
			IsIframe:  s.IsIframe(),
			SearchURL: s.SearchURL, ParseURL: s.ParseURL,
			Note: s.Note,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}
