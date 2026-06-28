package scriptcrawler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/video-site/backend/internal/catalog"
	"github.com/video-site/backend/internal/fingerprint"
)

const (
	scriptCrawlerDuplicateBytes = "duplicate-video-bytes"
	scriptCrawlerUniqueBytes    = "unique-video-bytes"
)

func writeScriptCrawlerFFprobeStub(t *testing.T, dir string, ok bool) string {
	t.Helper()
	name := "ffprobe-ok"
	unixBody := "echo video\nexit 0\n"
	windowsBody := "echo video\r\nexit /b 0\r\n"
	if !ok {
		name = "ffprobe-fail"
		unixBody = "echo moov atom not found 1>&2\nexit 1\n"
		windowsBody = "echo moov atom not found 1>&2\r\nexit /b 1\r\n"
	}
	return writePlatformScript(t, dir, name, unixBody, windowsBody)
}

func writeScriptCrawlerFFmpegStub(t *testing.T, dir string) string {
	t.Helper()
	return writePlatformScript(
		t,
		dir,
		"ffmpeg-hls",
		"if [ -n \"$GO_SCRIPTCRAWLER_FFMPEG_ARGS_FILE\" ]; then printf '%s\\n' \"$@\" > \"$GO_SCRIPTCRAWLER_FFMPEG_ARGS_FILE\"; fi\nout=\"\"\nfor arg do out=\"$arg\"; done\nprintf 'hls-video-bytes' > \"$out\"\n",
		"if not \"%GO_SCRIPTCRAWLER_FFMPEG_ARGS_FILE%\"==\"\" (\r\n  > \"%GO_SCRIPTCRAWLER_FFMPEG_ARGS_FILE%\" echo -protocol_whitelist\r\n  >> \"%GO_SCRIPTCRAWLER_FFMPEG_ARGS_FILE%\" echo http,https,tcp,tls,crypto\r\n  >> \"%GO_SCRIPTCRAWLER_FFMPEG_ARGS_FILE%\" echo -allowed_extensions\r\n  >> \"%GO_SCRIPTCRAWLER_FFMPEG_ARGS_FILE%\" echo ALL\r\n  >> \"%GO_SCRIPTCRAWLER_FFMPEG_ARGS_FILE%\" echo -allowed_segment_extensions\r\n  >> \"%GO_SCRIPTCRAWLER_FFMPEG_ARGS_FILE%\" echo ALL\r\n  >> \"%GO_SCRIPTCRAWLER_FFMPEG_ARGS_FILE%\" echo -extension_picky\r\n  >> \"%GO_SCRIPTCRAWLER_FFMPEG_ARGS_FILE%\" echo 0\r\n)\r\nif \"%GO_SCRIPTCRAWLER_FFMPEG_OUTFILE%\"==\"\" exit /b 2\r\npowershell -NoLogo -NoProfile -Command \"$p = $env:GO_SCRIPTCRAWLER_FFMPEG_OUTFILE; Set-Content -LiteralPath $p -NoNewline -Value 'hls-video-bytes'\"\r\n",
	)
}

func TestCrawlerRunOnceImportsLocalFileAndSkipsExisting(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cat, err := catalog.Open(filepath.Join(tmp, "catalog.db"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})
	drv := New(Config{ID: "demo", RootDir: filepath.Join(tmp, "crawler")})
	if err := drv.Init(ctx); err != nil {
		t.Fatalf("driver init: %v", err)
	}
	wrapper := writeHelperWrapperScript(t, tmp)

	t.Setenv("GO_WANT_SCRIPTCRAWLER_HELPER", "1")
	c := NewCrawler(CrawlerConfig{
		Driver:      drv,
		Catalog:     cat,
		CrawlerName: "Demo Crawler",
		FFprobePath: writeScriptCrawlerFFprobeStub(t, tmp, true),
		ScriptPath:  wrapper,
	})
	res, err := c.RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if res.NewVideos != 1 || res.Skipped != 0 || res.Failed != 0 {
		t.Fatalf("result = new:%d skipped:%d failed:%d, want 1/0/0", res.NewVideos, res.Skipped, res.Failed)
	}
	v, err := cat.GetVideo(ctx, BuildVideoID("demo", "abc-123"))
	if err != nil {
		t.Fatalf("get video: %v", err)
	}
	if v.Title != "Imported From Helper" || v.FileID != "abc-123.mp4" || v.Size == 0 {
		t.Fatalf("video = title:%q file:%q size:%d", v.Title, v.FileID, v.Size)
	}
	if !hasString(v.Tags, "Demo Crawler") {
		t.Fatalf("video tags = %#v, want crawler name tag", v.Tags)
	}
	if _, err := os.Stat(filepath.Join(drv.VideosDir(), "abc-123.mp4")); err != nil {
		t.Fatalf("video file not copied: %v", err)
	}

	res, err = c.RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if res.NewVideos != 0 || res.Skipped != 1 {
		t.Fatalf("second result = new:%d skipped:%d, want 0/1", res.NewVideos, res.Skipped)
	}
	if res.SeenSnapshot != 1 {
		t.Fatalf("seen snapshot = %d, want 1", res.SeenSnapshot)
	}
}

func TestCrawlerRunOnceMarksPreviewDisabledWhenConfigured(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cat, err := catalog.Open(filepath.Join(tmp, "catalog.db"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})
	drv := New(Config{ID: "demo", RootDir: filepath.Join(tmp, "crawler")})
	if err := drv.Init(ctx); err != nil {
		t.Fatalf("driver init: %v", err)
	}
	wrapper := writeHelperWrapperScript(t, tmp)

	t.Setenv("GO_WANT_SCRIPTCRAWLER_HELPER", "1")
	c := NewCrawler(CrawlerConfig{
		Driver:         drv,
		Catalog:        cat,
		FFprobePath:    writeScriptCrawlerFFprobeStub(t, tmp, true),
		ScriptPath:     wrapper,
		DisablePreview: true,
	})
	res, err := c.RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if res.NewVideos != 1 || res.Failed != 0 {
		t.Fatalf("result = new:%d failed:%d, want 1/0", res.NewVideos, res.Failed)
	}
	v, err := cat.GetVideo(ctx, BuildVideoID("demo", "abc-123"))
	if err != nil {
		t.Fatalf("get video: %v", err)
	}
	if v.PreviewStatus != "disabled" {
		t.Fatalf("preview status = %q, want disabled", v.PreviewStatus)
	}
	if v.FingerprintStatus != "ready" || v.SampledSHA256 == "" {
		t.Fatalf("fingerprint status=%q sampled=%q, want ready and sampled hash", v.FingerprintStatus, v.SampledSHA256)
	}
	pending, err := cat.ListVideosByPreviewStatus(ctx, "demo", "pending", 0)
	if err != nil {
		t.Fatalf("list pending previews: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending previews = %d, want 0", len(pending))
	}
}

func TestCrawlerRunOnceUsesCurrentDrivePreviewSwitch(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cat, err := catalog.Open(filepath.Join(tmp, "catalog.db"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})
	drv := New(Config{ID: "demo", RootDir: filepath.Join(tmp, "crawler")})
	if err := drv.Init(ctx); err != nil {
		t.Fatalf("driver init: %v", err)
	}
	if err := cat.UpsertDrive(ctx, &catalog.Drive{
		ID:            drv.ID(),
		Kind:          Kind,
		Name:          "Demo",
		RootID:        "/",
		Credentials:   map[string]string{"script_path": "/tmp/crawler.py"},
		TeaserEnabled: true,
	}); err != nil {
		t.Fatalf("seed drive: %v", err)
	}
	wrapper := writeHelperWrapperScript(t, tmp)

	t.Setenv("GO_WANT_SCRIPTCRAWLER_HELPER", "1")
	c := NewCrawler(CrawlerConfig{
		Driver:         drv,
		Catalog:        cat,
		FFprobePath:    writeScriptCrawlerFFprobeStub(t, tmp, true),
		ScriptPath:     wrapper,
		DisablePreview: true,
	})
	res, err := c.RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if res.NewVideos != 1 || res.Failed != 0 {
		t.Fatalf("result = new:%d failed:%d, want 1/0", res.NewVideos, res.Failed)
	}
	v, err := cat.GetVideo(ctx, BuildVideoID("demo", "abc-123"))
	if err != nil {
		t.Fatalf("get video: %v", err)
	}
	if v.PreviewStatus != "pending" {
		t.Fatalf("preview status = %q, want pending from current drive switch", v.PreviewStatus)
	}
}

func TestCrawlerRunOnceUsesSourceKindNamespace(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cat, err := catalog.Open(filepath.Join(tmp, "catalog.db"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})
	drv := New(Config{ID: "demo", RootDir: filepath.Join(tmp, "crawler")})
	if err := drv.Init(ctx); err != nil {
		t.Fatalf("driver init: %v", err)
	}
	wrapper := writeHelperWrapperScript(t, tmp)

	t.Setenv("GO_WANT_SCRIPTCRAWLER_HELPER", "1")
	c := NewCrawler(CrawlerConfig{
		Driver:      drv,
		Catalog:     cat,
		SourceKind:  "spider91",
		FFprobePath: writeScriptCrawlerFFprobeStub(t, tmp, true),
		ScriptPath:  wrapper,
	})
	res, err := c.RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if res.NewVideos != 1 || res.SeenSnapshot != 0 {
		t.Fatalf("result = new:%d seen:%d, want 1/0", res.NewVideos, res.SeenSnapshot)
	}
	videoID := BuildVideoIDForKind("spider91", "demo", "abc-123")
	if _, err := cat.GetVideo(ctx, videoID); err != nil {
		t.Fatalf("get source-kind video: %v", err)
	}
	if _, err := cat.GetVideo(ctx, BuildVideoID("demo", "abc-123")); err == nil {
		t.Fatalf("default namespace video unexpectedly exists")
	}

	res, err = c.RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if res.NewVideos != 0 || res.Skipped != 1 || res.SeenSnapshot != 1 {
		t.Fatalf("second result = new:%d skipped:%d seen:%d, want 0/1/1", res.NewVideos, res.Skipped, res.SeenSnapshot)
	}
}

func TestCrawlerRunOncePassesAbsoluteJobPathsWhenWorkDirDiffers(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	t.Chdir(tmp)
	cat, err := catalog.Open(filepath.Join(tmp, "catalog.db"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})
	drv := New(Config{ID: "demo", RootDir: filepath.Join("data", "crawler")})
	if err := drv.Init(ctx); err != nil {
		t.Fatalf("driver init: %v", err)
	}
	scriptDir := filepath.Join(tmp, "scripts")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatalf("mkdir script dir: %v", err)
	}
	wrapper := writeHelperWrapperScript(t, scriptDir)

	t.Setenv("GO_WANT_SCRIPTCRAWLER_HELPER", "1")
	t.Setenv("GO_WANT_SCRIPTCRAWLER_ASSERT_ABS", "1")
	c := NewCrawler(CrawlerConfig{
		Driver:      drv,
		Catalog:     cat,
		FFprobePath: writeScriptCrawlerFFprobeStub(t, tmp, true),
		ScriptPath:  wrapper,
		WorkDir:     scriptDir,
	})
	res, err := c.RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if res.NewVideos != 1 || res.Skipped != 0 || res.Failed != 0 {
		t.Fatalf("result = new:%d skipped:%d failed:%d, want 1/0/0", res.NewVideos, res.Skipped, res.Failed)
	}
	if !filepath.IsAbs(res.JobFile) || !filepath.IsAbs(res.SeenFile) {
		t.Fatalf("result paths should be absolute: job=%q seen=%q", res.JobFile, res.SeenFile)
	}
}

func TestCrawlerRunOnceImportsSimpleMediaURLWithoutSourceID(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cat, err := catalog.Open(filepath.Join(tmp, "catalog.db"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/video.mp4" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("simple-video-bytes"))
	}))
	defer srv.Close()

	drv := New(Config{ID: "demo", RootDir: filepath.Join(tmp, "crawler")})
	if err := drv.Init(ctx); err != nil {
		t.Fatalf("driver init: %v", err)
	}
	wrapper := writeHelperWrapperScript(t, tmp)

	t.Setenv("GO_WANT_SCRIPTCRAWLER_HELPER", "1")
	t.Setenv("GO_WANT_SCRIPTCRAWLER_SIMPLE", "1")
	t.Setenv("GO_SCRIPTCRAWLER_MEDIA_URL", srv.URL+"/video.mp4?token=first")
	c := NewCrawler(CrawlerConfig{
		Driver:      drv,
		Catalog:     cat,
		FFprobePath: writeScriptCrawlerFFprobeStub(t, tmp, true),
		ScriptPath:  wrapper,
		HTTPClient:  srv.Client(),
	})
	res, err := c.RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if res.NewVideos != 1 || res.Skipped != 0 || res.Failed != 0 {
		t.Fatalf("result = new:%d skipped:%d failed:%d, want 1/0/0", res.NewVideos, res.Skipped, res.Failed)
	}
	videos, err := cat.ListVideosByDrive(ctx, "demo")
	if err != nil {
		t.Fatalf("list videos: %v", err)
	}
	if len(videos) != 1 {
		t.Fatalf("videos = %d, want 1", len(videos))
	}
	v := videos[0]
	if !strings.HasPrefix(v.ID, BuildVideoID("demo", "auto-")) {
		t.Fatalf("video id = %q, want generated auto source id", v.ID)
	}
	if v.Title != "Simple Protocol Video" || v.Ext != "mp4" || v.ThumbnailURL != "" || v.Size == 0 {
		t.Fatalf("video = title:%q ext:%q thumb:%q size:%d", v.Title, v.Ext, v.ThumbnailURL, v.Size)
	}
	if _, err := os.Stat(filepath.Join(drv.VideosDir(), v.FileID)); err != nil {
		t.Fatalf("video file not downloaded: %v", err)
	}

	t.Setenv("GO_SCRIPTCRAWLER_MEDIA_URL", srv.URL+"/video.mp4?token=second")
	res, err = c.RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if res.NewVideos != 0 || res.Skipped != 1 {
		t.Fatalf("second result = new:%d skipped:%d, want 0/1", res.NewVideos, res.Skipped)
	}
}

func TestCrawlerRunOnceSkipsFingerprintDuplicateAndContinues(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cat, err := catalog.Open(filepath.Join(tmp, "catalog.db"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})
	drv := New(Config{ID: "demo", RootDir: filepath.Join(tmp, "crawler")})
	if err := drv.Init(ctx); err != nil {
		t.Fatalf("driver init: %v", err)
	}

	seedFile := "seed-canonical.mp4"
	if err := os.WriteFile(filepath.Join(drv.VideosDir(), seedFile), []byte(scriptCrawlerDuplicateBytes), 0o644); err != nil {
		t.Fatalf("write seed video: %v", err)
	}
	seed := &catalog.Video{
		ID:          "seed-for-hash",
		DriveID:     drv.ID(),
		FileID:      seedFile,
		Title:       "Seed",
		Size:        int64(len(scriptCrawlerDuplicateBytes)),
		PublishedAt: time.Now(),
	}
	sampled, err := fingerprint.Compute(ctx, drv, seed, fingerprint.Config{}, nil)
	if err != nil {
		t.Fatalf("compute seed fingerprint: %v", err)
	}
	_ = os.Remove(filepath.Join(drv.VideosDir(), seedFile))

	now := time.Now()
	if err := cat.UpsertVideo(ctx, &catalog.Video{
		ID:                "existing-canonical",
		DriveID:           "other-drive",
		FileID:            "existing.mp4",
		FileName:          "existing.mp4",
		Title:             "Existing Canonical",
		Size:              int64(len(scriptCrawlerDuplicateBytes)),
		Ext:               "mp4",
		SampledSHA256:     sampled,
		FingerprintStatus: "ready",
		PublishedAt:       now,
		CreatedAt:         now,
		UpdatedAt:         now,
	}); err != nil {
		t.Fatalf("seed canonical video: %v", err)
	}

	wrapper := writeHelperWrapperScript(t, tmp)

	t.Setenv("GO_WANT_SCRIPTCRAWLER_HELPER", "1")
	t.Setenv("GO_WANT_SCRIPTCRAWLER_DUP_UNIQUE", "1")
	c := NewCrawler(CrawlerConfig{
		Driver:      drv,
		Catalog:     cat,
		FFprobePath: writeScriptCrawlerFFprobeStub(t, tmp, true),
		ScriptPath:  wrapper,
	})
	res, err := c.RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if res.NewVideos != 1 || res.Skipped != 1 || res.Failed != 0 || res.TotalEntries != 2 {
		t.Fatalf("result = total:%d new:%d skipped:%d failed:%d, want 2/1/1/0", res.TotalEntries, res.NewVideos, res.Skipped, res.Failed)
	}
	if res.CandidateBudget <= res.TargetNew {
		t.Fatalf("candidate budget = %d, target = %d; want expanded budget", res.CandidateBudget, res.TargetNew)
	}
	if _, err := cat.GetVideo(ctx, BuildVideoID("demo", "dup-source")); err == nil {
		t.Fatal("duplicate candidate should not be imported")
	}
	if _, err := os.Stat(filepath.Join(drv.VideosDir(), "dup-source.mp4")); !os.IsNotExist(err) {
		t.Fatalf("duplicate local file stat = %v, want removed", err)
	}
	v, err := cat.GetVideo(ctx, BuildVideoID("demo", "unique-source"))
	if err != nil {
		t.Fatalf("unique video should be imported: %v", err)
	}
	if v.SampledSHA256 == "" || v.FingerprintStatus != "ready" {
		t.Fatalf("unique fingerprint = %q status=%q, want ready sampled fingerprint", v.SampledSHA256, v.FingerprintStatus)
	}
	seen, err := cat.ListCrawlerSourceIDs(ctx, Kind, "demo")
	if err != nil {
		t.Fatalf("list seen source ids: %v", err)
	}
	seenSet := map[string]bool{}
	for _, id := range seen {
		seenSet[id] = true
	}
	if !seenSet["dup-source"] || !seenSet["unique-source"] {
		t.Fatalf("seen ids = %#v, want duplicate and imported source ids", seen)
	}
}

func TestCrawlerRunOnceRejectsInvalidDownloadedVideo(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cat, err := catalog.Open(filepath.Join(tmp, "catalog.db"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})
	drv := New(Config{ID: "demo", RootDir: filepath.Join(tmp, "crawler")})
	if err := drv.Init(ctx); err != nil {
		t.Fatalf("driver init: %v", err)
	}
	wrapper := writeHelperWrapperScript(t, tmp)

	t.Setenv("GO_WANT_SCRIPTCRAWLER_HELPER", "1")
	c := NewCrawler(CrawlerConfig{
		Driver:      drv,
		Catalog:     cat,
		CrawlerName: "Demo Crawler",
		FFprobePath: writeScriptCrawlerFFprobeStub(t, tmp, false),
		ScriptPath:  wrapper,
	})
	res, err := c.RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if res.NewVideos != 0 || res.Skipped != 0 || res.Failed != 1 || res.TotalEntries != 1 {
		t.Fatalf("result = total:%d new:%d skipped:%d failed:%d, want 1/0/0/1", res.TotalEntries, res.NewVideos, res.Skipped, res.Failed)
	}
	if _, err := cat.GetVideo(ctx, BuildVideoID("demo", "abc-123")); err == nil {
		t.Fatal("invalid video should not be imported")
	}
	if _, err := os.Stat(filepath.Join(drv.VideosDir(), "abc-123.mp4")); !os.IsNotExist(err) {
		t.Fatalf("invalid local video stat = %v, want removed", err)
	}
	seen, err := cat.ListCrawlerSourceIDs(ctx, Kind, "demo")
	if err != nil {
		t.Fatalf("list seen source ids: %v", err)
	}
	if len(seen) != 0 {
		t.Fatalf("seen ids = %#v, want none for invalid video", seen)
	}
}

func TestCrawlerRunOnceDownloadsHLSMediaURL(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cat, err := catalog.Open(filepath.Join(tmp, "catalog.db"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})
	drv := New(Config{ID: "demo", RootDir: filepath.Join(tmp, "crawler")})
	if err := drv.Init(ctx); err != nil {
		t.Fatalf("driver init: %v", err)
	}
	wrapper := writeHelperWrapperScript(t, tmp)

	t.Setenv("GO_WANT_SCRIPTCRAWLER_HELPER", "1")
	t.Setenv("GO_WANT_SCRIPTCRAWLER_HLS", "1")
	ffmpegArgsFile := filepath.Join(tmp, "ffmpeg-args.txt")
	ffmpegOutFile := filepath.Join(drv.VideosDir(), "hls-source.mp4.part")
	t.Setenv("GO_SCRIPTCRAWLER_FFMPEG_ARGS_FILE", ffmpegArgsFile)
	t.Setenv("GO_SCRIPTCRAWLER_FFMPEG_OUTFILE", ffmpegOutFile)
	c := NewCrawler(CrawlerConfig{
		Driver:      drv,
		Catalog:     cat,
		CrawlerName: "Demo Crawler",
		FFmpegPath:  writeScriptCrawlerFFmpegStub(t, tmp),
		FFprobePath: writeScriptCrawlerFFprobeStub(t, tmp, true),
		ScriptPath:  wrapper,
	})
	res, err := c.RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if res.NewVideos != 1 || res.Skipped != 0 || res.Failed != 0 {
		t.Fatalf("result = new:%d skipped:%d failed:%d, want 1/0/0", res.NewVideos, res.Skipped, res.Failed)
	}
	v, err := cat.GetVideo(ctx, BuildVideoID("demo", "hls-source"))
	if err != nil {
		t.Fatalf("get hls video: %v", err)
	}
	if v.FileID != "hls-source.mp4" || v.Size != int64(len("hls-video-bytes")) {
		t.Fatalf("video file=%q size=%d, want hls-source.mp4 size %d", v.FileID, v.Size, len("hls-video-bytes"))
	}
	data, err := os.ReadFile(filepath.Join(drv.VideosDir(), "hls-source.mp4"))
	if err != nil {
		t.Fatalf("read hls output: %v", err)
	}
	if string(data) != "hls-video-bytes" {
		t.Fatalf("hls output = %q", string(data))
	}
	argsData, err := os.ReadFile(ffmpegArgsFile)
	if err != nil {
		t.Fatalf("read ffmpeg args: %v", err)
	}
	argsText := "\n" + strings.ReplaceAll(string(argsData), "\r\n", "\n") + "\n"
	for _, want := range []string{
		"\n-protocol_whitelist\nhttp,https,tcp,tls,crypto\n",
		"\n-allowed_extensions\nALL\n",
		"\n-allowed_segment_extensions\nALL\n",
		"\n-extension_picky\n0\n",
	} {
		if !strings.Contains(argsText, want) {
			t.Fatalf("ffmpeg args missing %q in:\n%s", strings.TrimSpace(want), string(argsData))
		}
	}
}

func TestScriptCrawlerHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_SCRIPTCRAWLER_HELPER") != "1" {
		return
	}
	args := os.Args
	jobPath := ""
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--job" {
			jobPath = args[i+1]
			break
		}
	}
	if jobPath == "" {
		fmt.Fprintln(os.Stderr, "missing --job")
		os.Exit(2)
	}
	data, err := os.ReadFile(jobPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if os.Getenv("GO_WANT_SCRIPTCRAWLER_ASSERT_ABS") == "1" {
		if !filepath.IsAbs(jobPath) || !filepath.IsAbs(job.SeenSourceIDsFile) || !filepath.IsAbs(job.OutputDir) {
			fmt.Fprintf(os.Stderr, "expected absolute paths, got job=%q seen=%q output=%q\n", jobPath, job.SeenSourceIDsFile, job.OutputDir)
			os.Exit(2)
		}
	}
	if os.Getenv("GO_WANT_SCRIPTCRAWLER_SIMPLE") == "1" {
		event := map[string]any{
			"title":     "Simple Protocol Video",
			"media_url": os.Getenv("GO_SCRIPTCRAWLER_MEDIA_URL"),
		}
		_ = json.NewEncoder(os.Stdout).Encode(event)
		os.Exit(0)
	}
	if os.Getenv("GO_WANT_SCRIPTCRAWLER_HLS") == "1" {
		event := Event{
			Type: "item",
			Item: Item{
				SourceID: "hls-source",
				Title:    "HLS Protocol Video",
				Author:   "helper",
				Media: MediaRef{
					URL: "https://media.example.test/video.m3u8",
					Headers: map[string]string{
						"Referer": "https://example.test/",
					},
				},
			},
		}
		_ = json.NewEncoder(os.Stdout).Encode(event)
		os.Exit(0)
	}
	if os.Getenv("GO_WANT_SCRIPTCRAWLER_DUP_UNIQUE") == "1" {
		duplicateFile := filepath.Join(job.OutputDir, "duplicate.mp4")
		if err := os.WriteFile(duplicateFile, []byte(scriptCrawlerDuplicateBytes), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		uniqueFile := filepath.Join(job.OutputDir, "unique.mp4")
		if err := os.WriteFile(uniqueFile, []byte(scriptCrawlerUniqueBytes), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		for _, event := range []Event{
			{
				Type: "item",
				Item: Item{
					SourceID: "dup-source",
					Title:    "Duplicate Candidate",
					Author:   "helper",
					Media:    MediaRef{LocalFile: duplicateFile},
				},
			},
			{
				Type: "item",
				Item: Item{
					SourceID: "unique-source",
					Title:    "Unique Candidate",
					Author:   "helper",
					Media:    MediaRef{LocalFile: uniqueFile},
				},
			},
		} {
			_ = json.NewEncoder(os.Stdout).Encode(event)
		}
		os.Exit(0)
	}
	localFile := filepath.Join(job.OutputDir, "helper.mp4")
	if err := os.WriteFile(localFile, []byte("helper-video"), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	event := Event{
		Type: "item",
		Item: Item{
			SourceID: "abc-123",
			Title:    "Imported From Helper",
			Author:   "helper",
			Media:    MediaRef{LocalFile: localFile},
		},
	}
	_ = json.NewEncoder(os.Stdout).Encode(event)
	os.Exit(0)
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
