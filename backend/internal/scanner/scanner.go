package scanner

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"path"
	"strings"
	"sync"
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
	// Concurrency 控制目录并发数。0/1 = 单 goroutine 深度优先（默认行为，兼容旧
	// 调用方）；>1 = worker pool + 广度优先，多个 Drive.List 可并行触发。
	// 对自带 rate limit 的网盘（115 / PikPak）收益小但不会变慢；对本地盘
	// 和慢速 list API 会显著加速。硬上限 16 防止误配。
	Concurrency int

	// enableIncrement 由 NewWithOptions 设置。true 时 Run() 启动预加载
	// drive 的现有 videos 进 snapshot；walk 阶段无变化文件直接跳过，
	// 不调 GetVideo / UpsertVideo，节省 IO。
	enableIncrement bool

	// snapshot 是增量扫用的 file_id → *Video 缓存。Run 启动时一次性预加载。
	// 单 drive 1 万视频约几 MB，无压力。
	snapshot map[string]*catalog.Video
}

const defaultScanProgressInterval = 30 * time.Second

// dirJob 是 runConcurrent worker pool 调度的最小单元：处理一个目录。
type dirJob struct {
	ID   string
	Name string
}

// New 构造一个 Scanner。
func New(cat *catalog.Catalog, drv drives.Drive, exts []string, skipDirIDs []string, onNew func(v *catalog.Video)) *Scanner {
	return NewWithOptions(cat, drv, exts, skipDirIDs, onNew, false)
}

// NewWithOptions 是 New 的扩展版，允许启用增量扫。EnableIncrement=true 时
// scanner 启动时一次性预加载 drive 上现有 videos 进内存 map；walk 阶段对
// file_id 已存在且 (file_name, size_bytes) 一致的文件直接跳过 catalog 写入，
// 大幅减少日常扫盘的 IO 和 ffmpeg 重排。
//
// 适用：定时扫盘 / 增量更新场景；首次导入新盘仍由 EnableIncrement=false 走
// 旧路径（避免首次扫错过已有文件）。
func NewWithOptions(cat *catalog.Catalog, drv drives.Drive, exts []string, skipDirIDs []string, onNew func(v *catalog.Video), enableIncrement bool) *Scanner {
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
		Catalog:         cat,
		Drive:           drv,
		Exts:            m,
		SkipDirIDs:      skip,
		OnNewVideo:      onNew,
		enableIncrement: enableIncrement,
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
	// Skipped 是增量扫里"file_id 已存在且 file_name/size 一致、跳过 catalog 写入"
	// 的文件数。用来在 admin 后台展示"本次扫盘其实只更新了 X 个"，
	// 避免看到 Scanned=0 误以为没扫到东西。EnableIncrement=false 时永远是 0。
	Skipped       int
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

	// 增量扫预加载：把 drive 上现有 videos 一次拉进内存，walk 时跳过无变化。
	// 加载失败时降级为全量扫，不阻塞整体流程。
	if s.enableIncrement && s.Catalog != nil && s.Drive != nil {
		driveID := s.Drive.ID()
		existing, err := s.Catalog.ListVideosByDrive(ctx, driveID)
		if err != nil {
			log.Printf("[scanner] drive=%s increment preload failed (fallback to full scan): %v", driveID, err)
		} else {
			s.snapshot = make(map[string]*catalog.Video, len(existing))
			for _, v := range existing {
				if v != nil && v.FileID != "" {
					s.snapshot[v.FileID] = v
				}
			}
			log.Printf("[scanner] drive=%s increment preload loaded %d videos", driveID, len(s.snapshot))
		}
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

	if s.Concurrency > 1 {
		if err := s.runConcurrent(ctx, startDirID, &stats, progress); err != nil {
			return stats, err
		}
	} else {
		if err := s.walk(ctx, startDirID, "", &stats, progress); err != nil {
			return stats, err
		}
	}

	// walk/runConcurrent 完成后，统一把按目录收集的图片创建为 ImageSet。
	if err := s.flushImageGroups(ctx, &stats); err != nil {
		return stats, err
	}

	return stats, nil
}

// runConcurrent 用 worker pool + 广度优先并发扫描。处理 N 个目录并行（受
// Scanner.Concurrency 控制，硬上限 16）。
//
// 与 walk() 共享的逻辑（handleVideo / handleDocument / flushImageGroups）
// 没改；并发模式下 stats 上的可变字段（map + int）用 statsMu 串行化。
//
// 不变量：
//   - 同一目录内的文件串行处理（顺序与 walk 一致）；
//   - 子目录按发现顺序入队到 dirCh，先入先出；
//   - OnProgress 回调拿 stats 快照，避免锁外读到撕裂值；
//   - ctx 取消后 drain 已入队任务不强制等待，但每个 worker 周期性检查 ctx。
func (s *Scanner) runConcurrent(ctx context.Context, startDirID string, stats *Stats, progress func(string)) error {
	n := s.Concurrency
	if n < 1 {
		n = 1
	}
	if n > 16 {
		n = 16
	}

	dirCh := make(chan dirJob, n*4)
	var (
		statsMu sync.Mutex
		wg      sync.WaitGroup
		// pending tracks unfinished dir jobs（包括当前正在处理的）。
		// 归零时关 dirCh，所有 worker 自然退出。
		pendingMu sync.Mutex
		pending   int
	)
	queueDir := func(j dirJob) {
		pendingMu.Lock()
		pending++
		pendingMu.Unlock()
		select {
		case dirCh <- j:
		case <-ctx.Done():
		}
	}
	finishDir := func() {
		pendingMu.Lock()
		pending--
		if pending == 0 {
			close(dirCh)
		}
		pendingMu.Unlock()
	}

	progressLocked := func(currentDir string) {
		statsMu.Lock()
		snap := Stats{
			Scanned:       stats.Scanned,
			Added:          stats.Added,
			Errors:         stats.Errors,
			SeenFileIDs:   stats.SeenFileIDs,
			VisitedDirIDs: stats.VisitedDirIDs,
			ImageGroups:   stats.ImageGroups,
		}
		statsMu.Unlock()
		if s.OnProgress != nil {
			s.OnProgress(snap)
		}
		_ = currentDir
	}

	worker := func() {
		defer wg.Done()
		for job := range dirCh {
			if err := ctx.Err(); err != nil {
				finishDir()
				continue
			}
			s.processDirConcurrent(ctx, job, stats, &statsMu, queueDir, progressLocked)
			finishDir()
		}
	}

	for i := 0; i < n; i++ {
		wg.Add(1)
		go worker()
	}

	// seed root
	pendingMu.Lock()
	pending = 1
	pendingMu.Unlock()
	dirCh <- dirJob{ID: startDirID, Name: ""}

	wg.Wait()
	return nil
}

// processDirConcurrent 处理单个目录：列目录、子目录入队、文件分发到 handle*。
//
// 并发模式下，stats 的可变字段（int 计数 + map）必须 statsMu 串行化；本函数
// 在持有 statsMu 的窗口内做 stats.Scanned++ / SeenFileIDs 写入 / ImageGroups
// append。对外的 OnNewVideo / OnNewNovelSet 回调放在锁外调用，避免用户实现
// 在锁内又回调 Catalog 造成死锁。
//
// 与 walk() 的等价性：单目录内 entries 顺序处理（与 walk 一致）；子目录调度
// 顺序也按发现顺序入队到 channel。
func (s *Scanner) processDirConcurrent(
	ctx context.Context,
	job dirJob,
	stats *Stats,
	statsMu *sync.Mutex,
	queueDir func(dirJob),
	progress func(string),
) {
	if err := ctx.Err(); err != nil {
		return
	}
	entries, err := s.Drive.List(ctx, job.ID)
	if err != nil {
		statsMu.Lock()
		stats.Errors++
		statsMu.Unlock()
		log.Printf("[scanner] list %s error: %v", job.ID, err)
		return
	}

	statsMu.Lock()
	stats.VisitedDirIDs[job.ID] = struct{}{}
	statsMu.Unlock()
	progress(job.Name)

	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return
		}
		if e.IsDir {
			if strings.EqualFold(e.Name, "previews") {
				continue
			}
			if _, skip := s.SkipDirIDs[e.ID]; skip {
				continue
			}
			queueDir(dirJob{ID: e.ID, Name: e.Name})
			continue
		}

		ext := strings.ToLower(path.Ext(e.Name))
		if !s.Exts[ext] {
			continue
		}
		if e.Size <= 0 {
			continue
		}

		// 增量扫快路径：snapshot 命中 + 无变化 → 写 SeenFileIDs（让清理逻辑
		// 认为这条仍存在）+ Skipped++ + 跳过整个 catalog 写入和文件类型分发。
		if s.isUnchangedSnapshotHit(e) {
			statsMu.Lock()
			stats.SeenFileIDs[e.ID] = struct{}{}
			stats.Skipped++
			statsMu.Unlock()
			continue
		}

		statsMu.Lock()
		stats.Scanned++
		stats.SeenFileIDs[e.ID] = struct{}{}
		statsMu.Unlock()
		progress(job.Name)

		if mediatype.IsImageExtension(ext) {
			statsMu.Lock()
			stats.ImageGroups[job.ID] = append(stats.ImageGroups[job.ID], imageEntry{
				ID:      e.ID,
				Name:    e.Name,
				Size:    e.Size,
				ModTime: e.ModTime,
			})
			statsMu.Unlock()
			continue
		}

		if mediatype.IsDocumentExtension(ext) {
			added, err := s.handleDocument(ctx, e, ext, job.Name)
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return
				}
				statsMu.Lock()
				stats.Errors++
				statsMu.Unlock()
				log.Printf("[scanner] upsert novel %s error: %v", e.Name, err)
				continue
			}
			if added {
				statsMu.Lock()
				stats.Added++
				statsMu.Unlock()
			}
			continue
		}

		added, err := s.handleVideo(ctx, e, ext, job.Name, progress)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return
			}
			statsMu.Lock()
			stats.Errors++
			statsMu.Unlock()
			log.Printf("[scanner] upsert video %s error: %v", e.Name, err)
			continue
		}
		if added {
			statsMu.Lock()
			stats.Added++
			statsMu.Unlock()
		}
	}
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

		// 增量扫快路径：snapshot 命中 + 无变化 → 仅记录 SeenFileIDs 和 Skipped，
		// 不走 handleVideo/handleDocument，不调 GetVideo/UpsertVideo。
		if s.isUnchangedSnapshotHit(e) {
			stats.SeenFileIDs[e.ID] = struct{}{}
			stats.Skipped++
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
			added, err := s.handleDocument(ctx, e, ext, dirName)
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return ctxErr
				}
				stats.Errors++
				log.Printf("[scanner] upsert novel %s error: %v", e.Name, err)
			}
			if added {
				stats.Added++
			}
			continue
		}

		// 视频/音频：走原有 UpsertVideo 流程
		added, err := s.handleVideo(ctx, e, ext, dirName, progress)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			stats.Errors++
			log.Printf("[scanner] upsert video %s error: %v", e.Name, err)
		}
		if added {
			stats.Added++
		}
	}
	return nil
}

// isUnchangedSnapshotHit 是增量扫快路径判定：snapshot 里能找到同 file_id 且
// (file_name, size_bytes) 一致时返回 true。调用方据此跳过整个 catalog 写入路径
// ——不动 status、不重置 thumbnail/preview、不触发 ffmpeg。
//
// 仅在 enableIncrement=true 且 snapshot 已预加载时有效。
func (s *Scanner) isUnchangedSnapshotHit(e drives.Entry) bool {
	if s.snapshot == nil {
		return false
	}
	existing, ok := s.snapshot[e.ID]
	if !ok || existing == nil {
		return false
	}
	return existing.FileName == e.Name && existing.Size == e.Size
}

// handleVideo 处理视频/音频文件（原有逻辑提取为独立方法）。
// 返回 added=true 表示新建了一条 video 记录，调用方负责 stats.Added++。
func (s *Scanner) handleVideo(ctx context.Context, e drives.Entry, ext, dirName string, progress func(string)) (bool, error) {
	id := s.Drive.Kind() + "-" + s.Drive.ID() + "-" + videoIDFilePart(e.ID)
	if deleted, err := s.Catalog.IsDeletedVideoCandidate(ctx, id, s.Drive.ID(), e.ID, e.Hash, e.Name, e.Size); err != nil {
		return false, err
	} else if deleted {
		return false, nil
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
		return false, err
	}
	if label, ok, err := s.Catalog.EnsureCollectionTag(ctx, dirName); err == nil && ok {
		tags = mergeTags(tags, []string{label})
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}

	existing, _ := s.Catalog.GetVideo(ctx, id)
	if err := ctx.Err(); err != nil {
		return false, err
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
		// 增量扫里 size 变化是常见信号（网盘文件被覆盖），需要写回 DB。
		// 原代码漏了这个字段——增量扫路径下用户改了文件大小 DB 不会更新。
		if e.Size > 0 && existing.Size != e.Size {
			patch.Size = e.Size
			existing.Size = e.Size
		}
		if existing.Category == "" && dirName != "" {
			patch.Category = dirName
		}
		mediaType := mediatype.FromExtension(ext)
		if existing.MediaType != mediaType {
			patch.MediaType = mediaType
		}
		if patch.Category != "" || patch.ContentHash != "" || patch.FileName != "" || patch.Size > 0 || patch.TitleSet || patch.AuthorSet || patch.MediaType != "" {
			_ = s.Catalog.UpdateVideoMeta(ctx, id, patch)
			if err := ctx.Err(); err != nil {
				return false, err
			}
		}
		if dup := s.findDuplicate(ctx, e.Hash, e.Name, e.Size, id); dup != nil {
			return false, nil
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if !sameTags(existing.Tags, tags) {
			_ = s.Catalog.SetAutoVideoTags(ctx, id, tags)
			if err := ctx.Err(); err != nil {
				return false, err
			}
		}
		return false, nil
	}

	if dup := s.findDuplicate(ctx, e.Hash, e.Name, e.Size, id); dup != nil {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
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
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	progress(dirName)
	if s.OnNewVideo != nil {
		s.OnNewVideo(v)
	}
	progress(dirName)
	return true, nil
}

// handleDocument 将单个文档文件（PDF/EPUB/TXT）创建为 NovelSet（含一个章节）。
// 返回 added=true 表示新建了一条 novel 记录，调用方负责 stats.Added++。
func (s *Scanner) handleDocument(ctx context.Context, e drives.Entry, ext, dirName string) (bool, error) {
	fileIDPart := videoIDFilePart(e.ID)
	id := s.Drive.Kind() + "-" + s.Drive.ID() + "-novel-" + fileIDPart
	sourceID := e.ID

	// 检查 tombstone
	if deleted, err := s.Catalog.IsNovelDeleted(ctx, id); err != nil || deleted {
		return false, nil
	}

	// 检查是否已存在
	if existing, _ := s.Catalog.GetNovelSet(ctx, id); existing != nil {
		return false, nil
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
		return false, err
	}
	if s.OnNewNovelSet != nil {
		s.OnNewNovelSet(novel)
	}
	return true, nil
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
	driveID := ""
	if s.Drive != nil {
		driveID = s.Drive.ID()
	}
	dup, err := s.Catalog.FindVideoByFileSignature(ctx, driveID, fileName, size)
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
