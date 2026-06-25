package scanner

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"path"
	"strings"
	"time"

	"github.com/video-site/backend/internal/catalog"
	"github.com/video-site/backend/internal/drives"
	"github.com/video-site/backend/internal/mediatype"
)

type Scanner struct {
	Catalog *catalog.Catalog
	Drive   drives.Drive
	Exts    map[string]bool
	// SkipDirIDs 是用户在 admin 后台配置的"扫描跳过目录"集合（drive 侧的目录 fileID）。
	SkipDirIDs map[string]struct{}
	// OnNewVideo 回调：新视频被加入后触发预览视频生成。
	OnNewVideo func(v *catalog.Video)
	// OnNewImageSet 回调：新图集被创建后触发。
	OnNewImageSet func(is *catalog.ImageSet)
	// OnNewNovelSet 回调：新小说被创建后触发。
	OnNewNovelSet func(ns *catalog.NovelSet)
	// OnProgress 在扫描进度变化时触发。
	OnProgress func(stats Stats)
	// ProgressInterval 控制扫描内部 heartbeat 的最小输出间隔。
	ProgressInterval time.Duration
}

const defaultScanProgressInterval = 30 * time.Second

// New 构造一个 Scanner。
func New(cat *catalog.Catalog, drv drives.Drive, exts []string, skipDirIDs []string, onNew func(v *catalog.Video)) *Scanner {
	m := make(map[string]bool, len(exts))
	for _, e := range exts {
		m[strings.ToLower(e)] = true
	}
	skip := make(map[string]struct{}, len(skipDirIDs))
	for _, id := range skipDirIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		skip[id] = struct{}{}
	}
	return &Scanner{
		Catalog:    cat,
		Drive:      drv,
		Exts:       m,
		SkipDirIDs: skip,
		OnNewVideo: onNew,
	}
}

// imageEntry 扫描过程中收集的单张图片信息。
type imageEntry struct {
	ID      string
	Name    string
	Size    int64
	ModTime time.Time
}

type Stats struct {
	Scanned       int
	Added         int
	Errors        int
	SeenFileIDs   map[string]struct{}
	VisitedDirIDs map[string]struct{}
	// ImageGroups 按目录收集图片文件（dirID → 该目录下的图片列表）。
	// walk 完成后由 flushImageGroups 统一创建 ImageSet。
	ImageGroups map[string][]imageEntry
}

// Run 从 Drive.RootID 开始扫描。
func (s *Scanner) Run(ctx context.Context, startDirID string) (Stats, error) {
	if startDirID == "" {
		startDirID = s.Drive.RootID()
	}
	stats := Stats{
		SeenFileIDs:   make(map[string]struct{}),
		VisitedDirIDs: make(map[string]struct{}),
		ImageGroups:   make(map[string][]imageEntry),
	}

	interval := s.ProgressInterval
	if interval == 0 {
		interval = defaultScanProgressInterval
	}
	started := time.Now()
	lastBeat := started
	driveID := ""
	if s.Drive != nil {
		driveID = s.Drive.ID()
	}
	progress := func(currentDir string) {
		if s.OnProgress != nil {
			s.OnProgress(stats)
		}
		if interval < 0 {
			return
		}
		now := time.Now()
		if now.Sub(lastBeat) < interval {
			return
		}
		lastBeat = now
		shown := currentDir
		if shown == "" {
			shown = "(root)"
		}
		log.Printf("[scanner] drive=%s progress: scanned=%d added=%d errors=%d dirs=%d elapsed=%s at=%s",
			driveID, stats.Scanned, stats.Added, stats.Errors, len(stats.VisitedDirIDs),
			now.Sub(started).Round(time.Second), shown)
	}

	if err := s.walk(ctx, startDirID, "", &stats, progress); err != nil {
		return stats, err
	}

	// walk 完成后，统一把按目录收集的图片创建为 ImageSet。
	if err := s.flushImageGroups(ctx, &stats); err != nil {
		return stats, err
	}

	return stats, nil
}

func (s *Scanner) walk(ctx context.Context, dirID, dirName string, stats *Stats, progress func(string)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stats.VisitedDirIDs[dirID] = struct{}{}
	progress(dirName)

	entries, err := s.Drive.List(ctx, dirID)
	if err != nil {
		return fmt.Errorf("list %s: %w", dirID, err)
	}

	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if e.IsDir {
			if strings.EqualFold(e.Name, "previews") {
				continue
			}
			if _, skip := s.SkipDirIDs[e.ID]; skip {
				continue
			}
			if err := s.walk(ctx, e.ID, e.Name, stats, progress); err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return ctxErr
				}
				stats.Errors++
				log.Printf("[scanner] walk %s error: %v", e.Name, err)
			}
			continue
		}

		ext := strings.ToLower(path.Ext(e.Name))
		if !s.Exts[ext] {
			continue
		}
		if e.Size <= 0 {
			continue
		}
		stats.Scanned++
		progress(dirName)
		stats.SeenFileIDs[e.ID] = struct{}{}

		// 按媒体类型分流
		if mediatype.IsImageExtension(ext) {
			stats.ImageGroups[dirID] = append(stats.ImageGroups[dirID], imageEntry{
				ID:      e.ID,
				Name:    e.Name,
				Size:    e.Size,
				ModTime: e.ModTime,
			})
			continue
		}

		if mediatype.IsDocumentExtension(ext) {
			if err := s.handleDocument(ctx, e, ext, dirName, stats); err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return ctxErr
				}
				stats.Errors++
				log.Printf("[scanner] upsert novel %s error: %v", e.Name, err)
			}
			continue
		}

		// 视频/音频：走原有 UpsertVideo 流程
		if err := s.handleVideo(ctx, e, ext, dirName, stats, progress); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			stats.Errors++
			log.Printf("[scanner] upsert video %s error: %v", e.Name, err)
		}
	}
	return nil
}

// handleVideo 处理视频/音频文件（原有逻辑提取为独立方法）。
func (s *Scanner) handleVideo(ctx context.Context, e drives.Entry, ext, dirName string, stats *Stats, progress func(string)) error {
	id := s.Drive.Kind() + "-" + s.Drive.ID() + "-" + videoIDFilePart(e.ID)
	if deleted, err := s.Catalog.IsDeletedVideoCandidate(ctx, id, s.Drive.ID(), e.ID, e.Hash, e.Name, e.Size); err != nil {
		return err
	} else if deleted {
		return nil
	}

	parsed := Parse(e.Name)
	if parsed.Title == "" {
		parsed.Title = strings.TrimSuffix(e.Name, ext)
	}
	tags := parsed.Tags
	if matched, err := s.Catalog.MatchTags(ctx, e.Name+" "+dirName+" "+parsed.Author); err == nil {
		tags = mergeTags(tags, matched)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if label, ok, err := s.Catalog.EnsureCollectionTag(ctx, dirName); err == nil && ok {
		tags = mergeTags(tags, []string{label})
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	existing, _ := s.Catalog.GetVideo(ctx, id)
	if err := ctx.Err(); err != nil {
		return err
	}
	if existing != nil {
		patch := catalog.VideoMetaPatch{}
		if e.Hash != "" && existing.ContentHash == "" {
			patch.ContentHash = e.Hash
			existing.ContentHash = e.Hash
		}
		if e.Name != "" && existing.FileName != e.Name {
			patch.FileName = e.Name
			existing.FileName = e.Name
			patch.Title = parsed.Title
			patch.TitleSet = true
			patch.Author = parsed.Author
			patch.AuthorSet = true
		}
		if existing.Category == "" && dirName != "" {
			patch.Category = dirName
		}
		mediaType := mediatype.FromExtension(ext)
		if existing.MediaType != mediaType {
			patch.MediaType = mediaType
		}
		if patch.Category != "" || patch.ContentHash != "" || patch.FileName != "" || patch.TitleSet || patch.AuthorSet || patch.MediaType != "" {
			_ = s.Catalog.UpdateVideoMeta(ctx, id, patch)
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if dup := s.findDuplicate(ctx, e.Hash, e.Name, e.Size, id); dup != nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !sameTags(existing.Tags, tags) {
			_ = s.Catalog.SetAutoVideoTags(ctx, id, tags)
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		return nil
	}

	if dup := s.findDuplicate(ctx, e.Hash, e.Name, e.Size, id); dup != nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	now := time.Now()
	mediaType := mediatype.FromExtension(ext)
	previewStatus := "pending"
	if mediaType == mediatype.Audio {
		previewStatus = "disabled"
	}
	v := &catalog.Video{
		ID:            id,
		DriveID:       s.Drive.ID(),
		FileID:        e.ID,
		FileName:      e.Name,
		ContentHash:   e.Hash,
		ParentID:      e.ParentID,
		Title:         parsed.Title,
		Author:        parsed.Author,
		Tags:          tags,
		Ext:           strings.TrimPrefix(ext, "."),
		MediaType:     mediaType,
		Quality:       "HD",
		Size:          e.Size,
		PreviewStatus: previewStatus,
		Category:      dirName,
		PublishedAt:   now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.Catalog.UpsertVideo(ctx, v); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	stats.Added++
	progress(dirName)
	if s.OnNewVideo != nil {
		s.OnNewVideo(v)
	}
	progress(dirName)
	return nil
}

// handleDocument 将单个文档文件（PDF/EPUB/TXT）创建为 NovelSet（含一个章节）。
func (s *Scanner) handleDocument(ctx context.Context, e drives.Entry, ext, dirName string, stats *Stats) error {
	fileIDPart := videoIDFilePart(e.ID)
	id := s.Drive.Kind() + "-" + s.Drive.ID() + "-novel-" + fileIDPart
	sourceID := e.ID

	// 检查 tombstone
	if deleted, err := s.Catalog.IsNovelDeleted(ctx, id); err != nil || deleted {
		return nil
	}

	// 检查是否已存在
	if existing, _ := s.Catalog.GetNovelSet(ctx, id); existing != nil {
		return nil
	}

	contentType := "pdf"
	lowerExt := strings.ToLower(strings.TrimPrefix(ext, "."))
	if lowerExt == "txt" {
		contentType = "text"
	}

	title := strings.TrimSuffix(e.Name, ext)
	parsed := Parse(e.Name)
	if parsed.Title != "" {
		title = parsed.Title
	}

	now := time.Now().UnixMilli()
	streamURL := fmt.Sprintf("/p/stream/%s/%s", s.Drive.ID(), e.ID)

	novel := &catalog.NovelSet{
		ID:          id,
		DriveID:     s.Drive.ID(),
		SourceID:    sourceID,
		Title:       title,
		Author:      parsed.Author,
		ContentType: contentType,
		Tags:        parsed.Tags,
		SourceKind:  "scanner",
		PublishedAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
		Chapters: []catalog.NovelChapter{
			{
				Position:    0,
				Title:       title,
				ContentType: contentType,
				PDFURL:      streamURL,
			},
		},
	}

	if err := s.Catalog.UpsertNovelSet(ctx, novel); err != nil {
		return err
	}
	stats.Added++
	if s.OnNewNovelSet != nil {
		s.OnNewNovelSet(novel)
	}
	return nil
}

// flushImageGroups 将 walk 期间按目录收集的图片批量创建为 ImageSet。
func (s *Scanner) flushImageGroups(ctx context.Context, stats *Stats) error {
	for dirID, images := range stats.ImageGroups {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(images) == 0 {
			continue
		}

		fileIDPart := videoIDFilePart(dirID)
		id := s.Drive.Kind() + "-" + s.Drive.ID() + "-imgset-" + fileIDPart

		// 检查是否已存在
		if existing, _ := s.Catalog.GetImageSet(ctx, id); existing != nil {
			continue
		}

		now := time.Now().UnixMilli()
		// 用目录名作为图集标题
		title := dirID
		if title == "" || title == "/" {
			title = s.Drive.ID()
		}

		var items []catalog.ImageSetItem
		var coverURL string
		for i, img := range images {
			url := fmt.Sprintf("/p/stream/%s/%s", s.Drive.ID(), img.ID)
			items = append(items, catalog.ImageSetItem{
				Position: i,
				URL:      url,
				ThumbURL: url, // 图片自身即缩略图
			})
			if i == 0 {
				coverURL = url
			}
		}

		imgSet := &catalog.ImageSet{
			ID:          id,
			DriveID:     s.Drive.ID(),
			SourceID:    dirID,
			Title:       title,
			CoverURL:    coverURL,
			ImageCount:  len(items),
			SourceKind:  "scanner",
			PublishedAt: now,
			CreatedAt:   now,
			UpdatedAt:   now,
			Images:      items,
		}

		if err := s.Catalog.UpsertImageSet(ctx, imgSet); err != nil {
			log.Printf("[scanner] upsert imageset %s error: %v", title, err)
			stats.Errors++
			continue
		}
		stats.Added++
		if s.OnNewImageSet != nil {
			s.OnNewImageSet(imgSet)
		}
	}
	return nil
}

func (s *Scanner) findDuplicate(ctx context.Context, hash, fileName string, size int64, currentID string) *catalog.Video {
	if dup := s.findDuplicateByHash(ctx, hash, currentID); dup != nil {
		return dup
	}
	return s.findDuplicateByFileSignature(ctx, fileName, size, currentID)
}

func (s *Scanner) findDuplicateByHash(ctx context.Context, hash, currentID string) *catalog.Video {
	if hash == "" {
		return nil
	}
	dup, err := s.Catalog.FindVideoByContentHash(ctx, hash)
	if err != nil || dup == nil || dup.ID == currentID {
		return nil
	}
	return dup
}

func (s *Scanner) findDuplicateByFileSignature(ctx context.Context, fileName string, size int64, currentID string) *catalog.Video {
	if fileName == "" || size <= 0 {
		return nil
	}
	dup, err := s.Catalog.FindVideoByFileSignature(ctx, fileName, size)
	if err != nil || dup == nil || dup.ID == currentID {
		return nil
	}
	return dup
}

func sameTags(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func mergeTags(lists ...[]string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, list := range lists {
		for _, tag := range list {
			if tag == "" || seen[tag] {
				continue
			}
			seen[tag] = true
			out = append(out, tag)
		}
	}
	return out
}

func videoIDFilePart(fileID string) string {
	if !strings.ContainsAny(fileID, `/\`+"\x00") {
		return fileID
	}
	return "b64_" + base64.RawURLEncoding.EncodeToString([]byte(fileID))
}
