package scanner

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/video-site/backend/internal/catalog"
	"github.com/video-site/backend/internal/drives"
)

func TestRunIgnoresRemoteThumbnailFromDriveEntry(t *testing.T) {
	ctx := context.Background()
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	drv := &scannerFakeDrive{
		entries: []drives.Entry{{
			ID:           "file-1",
			Name:         "clip.mp4",
			Size:         123,
			MimeType:     "video/mp4",
			ModTime:      time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
			ThumbnailURL: "https://thumbnail.example/clip.jpg",
		}},
	}
	sc := New(cat, drv, []string{".mp4"}, nil, nil)

	stats, err := sc.Run(ctx, "")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if stats.Added != 1 {
		t.Fatalf("added = %d, want 1", stats.Added)
	}

	got, err := cat.GetVideo(ctx, "fake-drive-file-1")
	if err != nil {
		t.Fatalf("get video: %v", err)
	}
	if got.ThumbnailURL != "" {
		t.Fatalf("thumbnail = %q, want empty so local thumbnail worker regenerates it", got.ThumbnailURL)
	}
}

func TestRunIgnoresZeroSizeVideoFiles(t *testing.T) {
	ctx := context.Background()
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	drv := &scannerFakeDrive{
		entries: []drives.Entry{{
			ID:       "empty-file",
			Name:     "empty.mp4",
			Size:     0,
			MimeType: "video/mp4",
			ModTime:  time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
		}},
	}
	sc := New(cat, drv, []string{".mp4"}, nil, nil)

	stats, err := sc.Run(ctx, "")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if stats.Added != 0 {
		t.Fatalf("added = %d, want 0", stats.Added)
	}
	if _, err := cat.GetVideo(ctx, "fake-drive-empty-file"); err != sql.ErrNoRows {
		t.Fatalf("get zero-size video error = %v, want sql.ErrNoRows", err)
	}
}

func TestRunScannedCountsOnlyVideoCandidates(t *testing.T) {
	ctx := context.Background()
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	drv := &scannerFakeDrive{
		entries: []drives.Entry{
			{ID: "file-1", Name: "clip.mp4", Size: 123},
			{ID: "file-2", Name: "notes.txt", Size: 123},
			{ID: "file-3", Name: "empty.mp4", Size: 0},
		},
	}
	sc := New(cat, drv, []string{".mp4"}, nil, nil)

	stats, err := sc.Run(ctx, "")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if stats.Scanned != 1 {
		t.Fatalf("scanned = %d, want one non-empty video candidate", stats.Scanned)
	}
	if stats.Added != 1 {
		t.Fatalf("added = %d, want one added video", stats.Added)
	}
}

func TestRunUsesPathSafeVideoIDForUnsafeFileID(t *testing.T) {
	ctx := context.Background()
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	drv := &scannerFakeDrive{
		entries: []drives.Entry{{
			ID:   "fid/with space",
			Name: "clip.mp4",
			Size: 123,
		}},
	}
	sc := New(cat, drv, []string{".mp4"}, nil, nil)

	stats, err := sc.Run(ctx, "")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if stats.Added != 1 {
		t.Fatalf("added = %d, want 1", stats.Added)
	}
	if _, ok := stats.SeenFileIDs["fid/with space"]; !ok {
		t.Fatalf("seen file ids = %#v, want original file id", stats.SeenFileIDs)
	}

	wantID := "fake-drive-b64_ZmlkL3dpdGggc3BhY2U"
	got, err := cat.GetVideo(ctx, wantID)
	if err != nil {
		t.Fatalf("get video %s: %v", wantID, err)
	}
	if strings.Contains(got.ID, "/") {
		t.Fatalf("video id = %q, must not contain slash", got.ID)
	}
	if got.FileID != "fid/with space" {
		t.Fatalf("file id = %q, want original", got.FileID)
	}
}

func TestRunStopsWhenContextCanceledDuringFileLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	drv := &scannerFakeDrive{
		entries: []drives.Entry{
			{ID: "file-1", Name: "one.mp4", Size: 123},
			{ID: "file-2", Name: "two.mp4", Size: 123},
			{ID: "file-3", Name: "three.mp4", Size: 123},
		},
	}
	callbacks := 0
	sc := New(cat, drv, []string{".mp4"}, nil, func(*catalog.Video) {
		callbacks++
		cancel()
	})

	stats, err := sc.Run(ctx, "")

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("scan error = %v, want context.Canceled", err)
	}
	if stats.Added != 1 || callbacks != 1 {
		t.Fatalf("added=%d callbacks=%d, want exactly one video before cancellation", stats.Added, callbacks)
	}
	if _, err := cat.GetVideo(context.Background(), "fake-drive-file-1"); err != nil {
		t.Fatalf("first video should be persisted before cancellation: %v", err)
	}
	if _, err := cat.GetVideo(context.Background(), "fake-drive-file-2"); err != sql.ErrNoRows {
		t.Fatalf("second video lookup error = %v, want sql.ErrNoRows", err)
	}
	if _, err := cat.GetVideo(context.Background(), "fake-drive-file-3"); err != sql.ErrNoRows {
		t.Fatalf("third video lookup error = %v, want sql.ErrNoRows", err)
	}
}

func TestRunSkipsAdminDeletedVideo(t *testing.T) {
	ctx := context.Background()
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	now := time.Now()
	if err := cat.UpsertVideo(ctx, &catalog.Video{
		ID:          "fake-drive-file-1",
		DriveID:     "drive",
		FileID:      "file-1",
		FileName:    "clip.mp4",
		ContentHash: "HASH-1",
		Title:       "Deleted Clip",
		Size:        123,
		PublishedAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("seed video: %v", err)
	}
	if err := cat.DeleteVideoWithTombstone(ctx, "fake-drive-file-1"); err != nil {
		t.Fatalf("delete with tombstone: %v", err)
	}

	drv := &scannerFakeDrive{
		entries: []drives.Entry{{
			ID:       "file-1",
			Name:     "clip.mp4",
			Size:     123,
			Hash:     "hash-1",
			MimeType: "video/mp4",
			ModTime:  now,
		}},
	}
	sc := New(cat, drv, []string{".mp4"}, nil, nil)

	stats, err := sc.Run(ctx, "")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if stats.Added != 0 {
		t.Fatalf("added = %d, want 0", stats.Added)
	}
	if _, err := cat.GetVideo(ctx, "fake-drive-file-1"); err != sql.ErrNoRows {
		t.Fatalf("deleted video was recreated, get error = %v", err)
	}
}

func TestRunDoesNotBackfillRemoteThumbnailForExistingVideo(t *testing.T) {
	ctx := context.Background()
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	now := time.Now()
	if err := cat.UpsertVideo(ctx, &catalog.Video{
		ID:            "fake-drive-file-1",
		DriveID:       "drive",
		FileID:        "file-1",
		Title:         "Clip",
		PreviewStatus: "pending",
		PublishedAt:   now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("seed video: %v", err)
	}

	drv := &scannerFakeDrive{
		entries: []drives.Entry{{
			ID:           "file-1",
			Name:         "clip.mp4",
			Size:         123,
			MimeType:     "video/mp4",
			ModTime:      now,
			ThumbnailURL: "https://thumbnail.example/backfilled.jpg",
		}},
	}
	sc := New(cat, drv, []string{".mp4"}, nil, nil)

	stats, err := sc.Run(ctx, "")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if stats.Added != 0 {
		t.Fatalf("added = %d, want 0", stats.Added)
	}

	got, err := cat.GetVideo(ctx, "fake-drive-file-1")
	if err != nil {
		t.Fatalf("get video: %v", err)
	}
	if got.ThumbnailURL != "" {
		t.Fatalf("thumbnail = %q, want empty so local thumbnail worker regenerates it", got.ThumbnailURL)
	}
}

func TestRunSyncsRenamedExistingVideoMetadata(t *testing.T) {
	ctx := context.Background()
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	now := time.Now()
	if err := cat.UpsertVideo(ctx, &catalog.Video{
		ID:            "fake-drive-file-1",
		DriveID:       "drive",
		FileID:        "file-1",
		FileName:      "old-name - Old Author.mp4",
		Title:         "old-name",
		Author:        "Old Author",
		PreviewStatus: "pending",
		PublishedAt:   now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("seed video: %v", err)
	}

	drv := &scannerFakeDrive{
		entries: []drives.Entry{{
			ID:      "file-1",
			Name:    "[4K] renamed clip.mp4",
			Size:    123,
			ModTime: now,
		}},
	}
	sc := New(cat, drv, []string{".mp4"}, nil, nil)

	stats, err := sc.Run(ctx, "")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if stats.Added != 0 {
		t.Fatalf("added = %d, want existing video to be updated in place", stats.Added)
	}

	got, err := cat.GetVideo(ctx, "fake-drive-file-1")
	if err != nil {
		t.Fatalf("get video: %v", err)
	}
	if got.FileName != "[4K] renamed clip.mp4" {
		t.Fatalf("file_name = %q, want remote name", got.FileName)
	}
	if got.Title != "renamed clip" {
		t.Fatalf("title = %q, want parsed title from remote name", got.Title)
	}
	if got.Author != "" {
		t.Fatalf("author = %q, want cleared author from remote name without author suffix", got.Author)
	}
}

func TestRunReplacesExistingVideoTagsWithFixedFilenameTags(t *testing.T) {
	ctx := context.Background()
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	now := time.Now()
	if err := cat.UpsertVideo(ctx, &catalog.Video{
		ID:            "fake-drive-file-1",
		DriveID:       "drive",
		FileID:        "file-1",
		Title:         "Old",
		Tags:          []string{"sunny", "kenny"},
		PreviewStatus: "pending",
		PublishedAt:   now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("seed video: %v", err)
	}

	drv := &scannerFakeDrive{
		entries: []drives.Entry{{
			ID:      "file-1",
			Name:    "女大后入.mp4",
			Size:    123,
			ModTime: now,
		}},
	}
	sc := New(cat, drv, []string{".mp4"}, nil, nil)

	if _, err := sc.Run(ctx, ""); err != nil {
		t.Fatalf("scan: %v", err)
	}

	got, err := cat.GetVideo(ctx, "fake-drive-file-1")
	if err != nil {
		t.Fatalf("get video: %v", err)
	}
	want := []string{"后入", "女大"}
	if !sameStrings(got.Tags, want) {
		t.Fatalf("tags = %#v, want %#v", got.Tags, want)
	}
}

func TestRunAddsShortCollectionDirectoryAsTag(t *testing.T) {
	ctx := context.Background()
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})
	now := time.Now()
	for _, id := range []string{"existing-1", "existing-2"} {
		if err := cat.UpsertVideo(ctx, &catalog.Video{
			ID:          id,
			DriveID:     "drive",
			FileID:      id,
			Title:       "Existing",
			Category:    "sunny",
			PublishedAt: now,
			CreatedAt:   now,
			UpdatedAt:   now,
		}); err != nil {
			t.Fatalf("seed existing sunny video: %v", err)
		}
	}

	drv := &scannerTreeFakeDrive{
		entries: map[string][]drives.Entry{
			"root": {{
				ID:    "dir-1",
				Name:  "sunny",
				IsDir: true,
			}},
			"dir-1": {{
				ID:       "file-1",
				ParentID: "dir-1",
				Name:     "clip.mp4",
				Size:     123,
				ModTime:  now,
			}},
		},
	}
	sc := New(cat, drv, []string{".mp4"}, nil, nil)

	if _, err := sc.Run(ctx, ""); err != nil {
		t.Fatalf("scan: %v", err)
	}

	got, err := cat.GetVideo(ctx, "fake-drive-file-1")
	if err != nil {
		t.Fatalf("get video: %v", err)
	}
	if !sameStrings(got.Tags, []string{"sunny"}) {
		t.Fatalf("tags = %#v, want sunny", got.Tags)
	}
}

func TestRunDoesNotRecreateDeletedCollectionDirectoryTag(t *testing.T) {
	ctx := context.Background()
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})
	now := time.Now()
	for _, id := range []string{"existing-1", "existing-2"} {
		if err := cat.UpsertVideo(ctx, &catalog.Video{
			ID:          id,
			DriveID:     "drive",
			FileID:      id,
			Title:       "Existing",
			Category:    "sunny",
			PublishedAt: now,
			CreatedAt:   now,
			UpdatedAt:   now,
		}); err != nil {
			t.Fatalf("seed existing sunny video: %v", err)
		}
	}
	if label, ok, err := cat.EnsureCollectionTag(ctx, "sunny"); err != nil || !ok || label != "sunny" {
		t.Fatalf("ensure collection = %q, %v, %v; want sunny true nil", label, ok, err)
	}
	tags, err := cat.ListTags(ctx)
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}
	var tagID int64
	for _, tag := range tags {
		if tag.Label == "sunny" {
			tagID = tag.ID
			break
		}
	}
	if tagID == 0 {
		t.Fatal("sunny tag not found before delete")
	}
	if _, err := cat.DeleteTag(ctx, tagID); err != nil {
		t.Fatalf("delete tag: %v", err)
	}

	drv := &scannerTreeFakeDrive{
		entries: map[string][]drives.Entry{
			"root": {{
				ID:    "dir-1",
				Name:  "sunny",
				IsDir: true,
			}},
			"dir-1": {{
				ID:       "file-1",
				ParentID: "dir-1",
				Name:     "clip.mp4",
				Size:     123,
				ModTime:  now,
			}},
		},
	}
	sc := New(cat, drv, []string{".mp4"}, nil, nil)

	if _, err := sc.Run(ctx, ""); err != nil {
		t.Fatalf("scan: %v", err)
	}

	got, err := cat.GetVideo(ctx, "fake-drive-file-1")
	if err != nil {
		t.Fatalf("get video: %v", err)
	}
	if len(got.Tags) != 0 {
		t.Fatalf("tags = %#v, want none", got.Tags)
	}
	tags, err = cat.ListTags(ctx)
	if err != nil {
		t.Fatalf("list tags after scan: %v", err)
	}
	for _, tag := range tags {
		if tag.Label == "sunny" {
			t.Fatal("deleted collection tag was recreated during scan")
		}
	}
}

func TestRunMapsAVCodeDirectoryToAVTag(t *testing.T) {
	ctx := context.Background()
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})
	now := time.Now()
	for _, id := range []string{"existing-1", "existing-2"} {
		if err := cat.UpsertVideo(ctx, &catalog.Video{
			ID:          id,
			DriveID:     "drive",
			FileID:      id,
			Title:       "Existing",
			Category:    "cc-1750027",
			PublishedAt: now,
			CreatedAt:   now,
			UpdatedAt:   now,
		}); err != nil {
			t.Fatalf("seed existing AV code video: %v", err)
		}
	}

	drv := &scannerTreeFakeDrive{
		entries: map[string][]drives.Entry{
			"root": {{
				ID:    "dir-1",
				Name:  "cc-1750027",
				IsDir: true,
			}},
			"dir-1": {{
				ID:       "file-1",
				ParentID: "dir-1",
				Name:     "clip.mp4",
				Size:     123,
				ModTime:  now,
			}},
		},
	}
	sc := New(cat, drv, []string{".mp4"}, nil, nil)

	if _, err := sc.Run(ctx, ""); err != nil {
		t.Fatalf("scan: %v", err)
	}

	got, err := cat.GetVideo(ctx, "fake-drive-file-1")
	if err != nil {
		t.Fatalf("get video: %v", err)
	}
	if !sameStrings(got.Tags, []string{"AV"}) {
		t.Fatalf("tags = %#v, want AV", got.Tags)
	}
}

func TestRunSkipsDuplicateFileHashes(t *testing.T) {
	ctx := context.Background()
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	now := time.Now()
	drv := &scannerFakeDrive{
		entries: []drives.Entry{
			{
				ID:      "file-1",
				Name:    "first.mp4",
				Size:    123,
				Hash:    "hash-same",
				ModTime: now,
			},
			{
				ID:      "file-2",
				Name:    "second.mp4",
				Size:    123,
				Hash:    "hash-same",
				ModTime: now,
			},
		},
	}
	addedIDs := []string{}
	sc := New(cat, drv, []string{".mp4"}, nil, func(v *catalog.Video) {
		addedIDs = append(addedIDs, v.ID)
	})

	stats, err := sc.Run(ctx, "")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if stats.Added != 1 {
		t.Fatalf("added = %d, want 1", stats.Added)
	}
	if len(addedIDs) != 1 || addedIDs[0] != "fake-drive-file-1" {
		t.Fatalf("on new ids = %#v, want first file only", addedIDs)
	}

	items, total, err := cat.ListVideos(ctx, catalog.ListParams{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list videos: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("visible videos total=%d len=%d, want 1", total, len(items))
	}
	if items[0].FileID != "file-1" {
		t.Fatalf("visible file id = %q, want file-1", items[0].FileID)
	}
}

func TestRunSkipsDuplicateFileNamesWithSameSizeWhenHashesMissing(t *testing.T) {
	ctx := context.Background()
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	now := time.Now()
	drv := &scannerFakeDrive{
		entries: []drives.Entry{
			{
				ID:      "file-1",
				Name:    "same-name.mp4",
				Size:    123,
				ModTime: now,
			},
			{
				ID:      "file-2",
				Name:    "same-name.mp4",
				Size:    123,
				ModTime: now,
			},
			{
				ID:      "file-3",
				Name:    "same-name.mp4",
				Size:    456,
				ModTime: now,
			},
		},
	}
	addedIDs := []string{}
	sc := New(cat, drv, []string{".mp4"}, nil, func(v *catalog.Video) {
		addedIDs = append(addedIDs, v.ID)
	})

	stats, err := sc.Run(ctx, "")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if stats.Added != 2 {
		t.Fatalf("added = %d, want 2", stats.Added)
	}
	wantAdded := []string{"fake-drive-file-1", "fake-drive-file-3"}
	if !sameStrings(addedIDs, wantAdded) {
		t.Fatalf("on new ids = %#v, want %#v", addedIDs, wantAdded)
	}
	if _, err := cat.GetVideo(ctx, "fake-drive-file-2"); err != sql.ErrNoRows {
		t.Fatalf("duplicate video lookup error = %v, want sql.ErrNoRows", err)
	}
}

func TestRunReportsSeenVideoFileIDsAndVisitedDirectories(t *testing.T) {
	ctx := context.Background()
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	drv := &scannerTreeFakeDrive{
		entries: map[string][]drives.Entry{
			"root": {
				{ID: "dir-1", Name: "Folder", IsDir: true},
				{ID: "root-file", Name: "root.mp4", Size: 123},
				{ID: "note", Name: "note.txt", Size: 123},
			},
			"dir-1": {
				{ID: "nested-file", ParentID: "dir-1", Name: "nested.mp4", Size: 456},
				{ID: "empty-video", ParentID: "dir-1", Name: "empty.mp4", Size: 0},
			},
		},
	}
	sc := New(cat, drv, []string{".mp4"}, nil, nil)

	stats, err := sc.Run(ctx, "")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if _, ok := stats.SeenFileIDs["root-file"]; !ok {
		t.Fatalf("seen file ids = %#v, want root-file", stats.SeenFileIDs)
	}
	if _, ok := stats.SeenFileIDs["nested-file"]; !ok {
		t.Fatalf("seen file ids = %#v, want live non-empty videos", stats.SeenFileIDs)
	}
	if _, ok := stats.SeenFileIDs["note"]; ok {
		t.Fatalf("seen file ids = %#v, want non-video entries excluded", stats.SeenFileIDs)
	}
	if _, ok := stats.SeenFileIDs["empty-video"]; ok {
		t.Fatalf("seen file ids = %#v, want zero-size entries excluded", stats.SeenFileIDs)
	}
	if _, ok := stats.VisitedDirIDs["root"]; !ok {
		t.Fatalf("visited dir ids = %#v, want root", stats.VisitedDirIDs)
	}
	if _, ok := stats.VisitedDirIDs["dir-1"]; !ok {
		t.Fatalf("visited dir ids = %#v, want nested dir", stats.VisitedDirIDs)
	}
	if stats.Errors != 0 {
		t.Fatalf("errors = %d, want 0", stats.Errors)
	}
}

func TestRunSkipsConfiguredDirIDsAndDoesNotRecurse(t *testing.T) {
	ctx := context.Background()
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	drv := &scannerTreeFakeDrive{
		kind: "p115",
		id:   "115",
		entries: map[string][]drives.Entry{
			"root": {
				{ID: "skip-dir", Name: "Movies", IsDir: true},
				{ID: "normal-file", Name: "normal.mp4", Size: 123},
			},
			"skip-dir": {
				{ID: "skipped-file", ParentID: "skip-dir", Name: "skipped.mp4", Size: 456},
				{ID: "nested-dir", Name: "Nested", IsDir: true},
			},
			"nested-dir": {
				{ID: "nested-skipped-file", ParentID: "nested-dir", Name: "nested.mp4", Size: 789},
			},
		},
	}
	// 把 skip-dir 加入 SkipDirIDs：scanner 应该完全不进该目录，
	// 也不会递归到其下的 nested-dir。
	sc := New(cat, drv, []string{".mp4"}, []string{"skip-dir"}, nil)

	stats, err := sc.Run(ctx, "")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if stats.Scanned != 1 {
		t.Fatalf("scanned = %d, want only non-skipped file counted", stats.Scanned)
	}
	if stats.Added != 1 {
		t.Fatalf("added = %d, want only non-skipped file added", stats.Added)
	}
	// skip-dir 自身和它下面的目录 / 文件都不应被访问。
	if _, ok := stats.VisitedDirIDs["skip-dir"]; ok {
		t.Fatalf("visited skipped dir, want no recursion into skip-dir")
	}
	if _, ok := stats.VisitedDirIDs["nested-dir"]; ok {
		t.Fatalf("visited nested dir under skipped, want no descent")
	}
	if _, ok := stats.SeenFileIDs["skipped-file"]; ok {
		t.Fatalf("seen skipped file, want skipped")
	}
	if _, err := cat.GetVideo(ctx, "p115-115-skipped-file"); err != sql.ErrNoRows {
		t.Fatalf("skipped direct file get error = %v, want sql.ErrNoRows", err)
	}
	if _, err := cat.GetVideo(ctx, "p115-115-nested-skipped-file"); err != sql.ErrNoRows {
		t.Fatalf("nested skipped file get error = %v, want sql.ErrNoRows", err)
	}
	if _, err := cat.GetVideo(ctx, "p115-115-normal-file"); err != nil {
		t.Fatalf("normal video was not added: %v", err)
	}
}

// TestRunDoesNotEnforceLegacyMaxDepth 校验扫描会一直递归直到没有子目录，
// 不再受旧的 max_depth 上限限制。构造 7 层嵌套（旧 default=5 时第 6+ 层会被截断），
// 确保最深层的视频也能被入库。
func TestRunDoesNotEnforceLegacyMaxDepth(t *testing.T) {
	ctx := context.Background()
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	// scannerTreeFakeDrive.RootID() == "root"。
	// 链接 root → d1 → d2 → ... → d7，叶子放一个视频。
	const depth = 7
	entries := map[string][]drives.Entry{}
	dirs := []string{"root"}
	for i := 1; i <= depth; i++ {
		dirs = append(dirs, fmt.Sprintf("d%d", i))
	}
	for i := 0; i < depth; i++ {
		entries[dirs[i]] = []drives.Entry{
			{ID: dirs[i+1], Name: fmt.Sprintf("L%d", i+1), IsDir: true},
		}
	}
	leaf := dirs[depth]
	entries[leaf] = []drives.Entry{
		{ID: "deep-file", ParentID: leaf, Name: "deep.mp4", Size: 10},
	}
	drv := &scannerTreeFakeDrive{entries: entries}

	sc := New(cat, drv, []string{".mp4"}, nil, nil)
	stats, err := sc.Run(ctx, "")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if stats.Added != 1 {
		t.Fatalf("added = %d, want 1 (deepest-leaf video reached)", stats.Added)
	}
	if _, err := cat.GetVideo(ctx, "fake-drive-deep-file"); err != nil {
		t.Fatalf("deepest video not added (legacy max_depth still enforced?): %v", err)
	}
}

// TestRunConcurrentProducesSameStatsAsSerial 验证并发路径与串行路径产出
// 等价的 stats（Scanned / Added / SeenFileIDs / VisitedDirIDs）。树的形状：
// root → 4 个子目录 → 每个子目录 2 个文件，并发=4 应在多 worker 间分配。
func TestRunConcurrentProducesSameStatsAsSerial(t *testing.T) {
	ctx := context.Background()
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	entries := map[string][]drives.Entry{
		"root": {
			{ID: "d1", Name: "D1", IsDir: true},
			{ID: "d2", Name: "D2", IsDir: true},
			{ID: "d3", Name: "D3", IsDir: true},
			{ID: "d4", Name: "D4", IsDir: true},
		},
	}
	for _, dirID := range []string{"d1", "d2", "d3", "d4"} {
		entries[dirID] = []drives.Entry{
			{ID: dirID + "-f1", ParentID: dirID, Name: dirID + "-a.mp4", Size: 100},
			{ID: dirID + "-f2", ParentID: dirID, Name: dirID + "-b.mp4", Size: 200},
		}
	}
	drv := &scannerTreeFakeDrive{entries: entries}

	sc := New(cat, drv, []string{".mp4"}, nil, nil)
	sc.Concurrency = 4

	stats, err := sc.Run(ctx, "")
	if err != nil {
		t.Fatalf("concurrent scan: %v", err)
	}
	if stats.Added != 8 {
		t.Fatalf("concurrent Added = %d, want 8", stats.Added)
	}
	if stats.Scanned != 8 {
		t.Fatalf("concurrent Scanned = %d, want 8", stats.Scanned)
	}
	if len(stats.VisitedDirIDs) != 5 {
		t.Fatalf("concurrent VisitedDirIDs = %d, want 5 (root + 4 subdirs)", len(stats.VisitedDirIDs))
	}
	if len(stats.SeenFileIDs) != 8 {
		t.Fatalf("concurrent SeenFileIDs = %d, want 8", len(stats.SeenFileIDs))
	}
	for _, id := range []string{"d1-f1", "d1-f2", "d2-f1", "d2-f2", "d3-f1", "d3-f2", "d4-f1", "d4-f2"} {
		if _, err := cat.GetVideo(ctx, "fake-drive-"+id); err != nil {
			t.Fatalf("missing video %s: %v", id, err)
		}
	}
}

// TestRunTreatsSameNameSizeAcrossDrivesAsDistinct 验证扫描器在两个 drive 上
// 看到同名同 size 的不同文件时，不会把第二个当作重复跳过。
// 这是 2026-06-27 修的 race 回归：原 findDuplicateByFileSignature 没带 drive_id，
// 并发扫描不同盘时会把"另一盘的同名文件"误判为重复，导致部分视频不入库。
func TestRunTreatsSameNameSizeAcrossDrivesAsDistinct(t *testing.T) {
	ctx := context.Background()
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	// Drive 1：根 → dir-1 → file.mp4 (size=100)
	// Drive 2：根 → dir-2 → file.mp4 (size=100)
	// 两个文件在不同 drive 上同名同 size，应该是两条独立记录。
	drv1 := &scannerTreeFakeDrive{
		kind: "fake",
		id:   "drive-1",
		entries: map[string][]drives.Entry{
			"root": {{ID: "dir-1", Name: "D1", IsDir: true}},
			"dir-1": {{ID: "f-1", ParentID: "dir-1", Name: "file.mp4", Size: 100}},
		},
	}
	drv2 := &scannerTreeFakeDrive{
		kind: "fake",
		id:   "drive-2",
		entries: map[string][]drives.Entry{
			"root": {{ID: "dir-2", Name: "D2", IsDir: true}},
			"dir-2": {{ID: "f-2", ParentID: "dir-2", Name: "file.mp4", Size: 100}},
		},
	}

	for _, drv := range []*scannerTreeFakeDrive{drv1, drv2} {
		sc := New(cat, drv, []string{".mp4"}, nil, nil)
		sc.Concurrency = 4
		stats, err := sc.Run(ctx, "")
		if err != nil {
			t.Fatalf("scan drive=%s: %v", drv.id, err)
		}
		if stats.Added != 1 {
			t.Fatalf("drive=%s Added = %d, want 1", drv.id, stats.Added)
		}
	}
	if _, err := cat.GetVideo(ctx, "fake-drive-1-f-1"); err != nil {
		t.Fatalf("drive-1 file not found: %v", err)
	}
	if _, err := cat.GetVideo(ctx, "fake-drive-2-f-2"); err != nil {
		t.Fatalf("drive-2 file missing — 被误判为重复跳过了: %v", err)
	}
}

// TestRunConcurrentRespectsCancellation 验证并发模式在 ctx 取消时退出，
// 不会泄漏 worker 或死锁。
func TestRunConcurrentRespectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	// 100 层嵌套，每层 1 个子目录 + 1 个 mp4。并发=4 启动后立即取消，
	// 应在 ctx 取消后有限时间内返回。
	entries := map[string][]drives.Entry{}
	for i := 0; i < 99; i++ {
		parent := fmt.Sprintf("d%d", i)
		child := fmt.Sprintf("d%d", i+1)
		entries[parent] = []drives.Entry{
			{ID: child, Name: child, IsDir: true},
			{ID: parent + "-v", ParentID: parent, Name: "a.mp4", Size: 1},
		}
	}
	entries["d99"] = []drives.Entry{
		{ID: "d99-v", ParentID: "d99", Name: "a.mp4", Size: 1},
	}
	drv := &scannerTreeFakeDrive{entries: entries}

	sc := New(cat, drv, []string{".mp4"}, nil, nil)
	sc.Concurrency = 4

	doneCh := make(chan error, 1)
	go func() {
		_, err := sc.Run(ctx, "")
		doneCh <- err
	}()

	// 给 worker 一点时间启动再取消
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-doneCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("scan err = %v, want nil or Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("concurrent scan did not exit within 3s after cancel")
	}
}

type scannerFakeDrive struct {
	entries []drives.Entry
}

func (d *scannerFakeDrive) Kind() string { return "fake" }
func (d *scannerFakeDrive) ID() string   { return "drive" }
func (d *scannerFakeDrive) Init(context.Context) error {
	return nil
}
func (d *scannerFakeDrive) List(context.Context, string) ([]drives.Entry, error) {
	return d.entries, nil
}
func (d *scannerFakeDrive) Stat(context.Context, string) (*drives.Entry, error) {
	return nil, drives.ErrNotSupported
}
func (d *scannerFakeDrive) StreamURL(context.Context, string) (*drives.StreamLink, error) {
	return &drives.StreamLink{URL: "https://video.example/clip.mp4"}, nil
}
func (d *scannerFakeDrive) Upload(context.Context, string, string, io.Reader, int64) (string, error) {
	return "", drives.ErrNotSupported
}
func (d *scannerFakeDrive) EnsureDir(context.Context, string) (string, error) {
	return "", drives.ErrNotSupported
}
func (d *scannerFakeDrive) RootID() string { return "root" }

type scannerTreeFakeDrive struct {
	kind    string
	id      string
	entries map[string][]drives.Entry
}

func (d *scannerTreeFakeDrive) Kind() string {
	if d.kind != "" {
		return d.kind
	}
	return "fake"
}
func (d *scannerTreeFakeDrive) ID() string {
	if d.id != "" {
		return d.id
	}
	return "drive"
}
func (d *scannerTreeFakeDrive) Init(context.Context) error {
	return nil
}
func (d *scannerTreeFakeDrive) List(_ context.Context, parentID string) ([]drives.Entry, error) {
	return d.entries[parentID], nil
}
func (d *scannerTreeFakeDrive) Stat(context.Context, string) (*drives.Entry, error) {
	return nil, drives.ErrNotSupported
}
func (d *scannerTreeFakeDrive) StreamURL(context.Context, string) (*drives.StreamLink, error) {
	return &drives.StreamLink{URL: "https://video.example/clip.mp4"}, nil
}
func (d *scannerTreeFakeDrive) Upload(context.Context, string, string, io.Reader, int64) (string, error) {
	return "", drives.ErrNotSupported
}
func (d *scannerTreeFakeDrive) EnsureDir(context.Context, string) (string, error) {
	return "", drives.ErrNotSupported
}
func (d *scannerTreeFakeDrive) RootID() string { return "root" }

// captureLog 把 log 包默认输出引到一个 bytes.Buffer，便于断言进度日志被打印；
// 测试结束自动恢复。
func captureLog(t *testing.T) *strings.Builder {
	t.Helper()
	buf := &strings.Builder{}
	originalOutput := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(originalOutput)
		log.SetFlags(originalFlags)
	})
	return buf
}

func TestScannerProgressHeartbeatEmits(t *testing.T) {
	ctx := context.Background()
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() { cat.Close() })

	// 准备 5 个文件，足够触发循环结尾的 progress() 调用至少一次。
	entries := make([]drives.Entry, 5)
	for i := range entries {
		entries[i] = drives.Entry{
			ID:      fmt.Sprintf("file-%d", i),
			Name:    fmt.Sprintf("clip-%d.mp4", i),
			Size:    100,
			ModTime: time.Now(),
		}
	}
	drv := &scannerFakeDrive{entries: entries}

	sc := New(cat, drv, []string{".mp4"}, nil, nil)
	// 极短间隔，确保至少一次 heartbeat 在 walk 内被触发
	sc.ProgressInterval = 1 * time.Microsecond

	buf := captureLog(t)
	if _, err := sc.Run(ctx, ""); err != nil {
		t.Fatalf("scan: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "[scanner] drive=drive progress:") {
		t.Fatalf("expected progress heartbeat in log, got:\n%s", out)
	}
}

func TestScannerProgressHeartbeatDisabled(t *testing.T) {
	ctx := context.Background()
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() { cat.Close() })

	drv := &scannerFakeDrive{entries: []drives.Entry{
		{ID: "f-1", Name: "x.mp4", Size: 1, ModTime: time.Now()},
	}}
	sc := New(cat, drv, []string{".mp4"}, nil, nil)
	sc.ProgressInterval = -1 // 显式关闭

	buf := captureLog(t)
	if _, err := sc.Run(ctx, ""); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if strings.Contains(buf.String(), "progress:") {
		t.Fatalf("progress heartbeat should be silenced when interval < 0, got:\n%s", buf.String())
	}
}

// TestRunIncrementalSkipsUnchanged 验证增量扫快路径：第二次扫同一个盘时，
// 无变化的视频都进 Skipped 计数，不动 catalog（不重置 thumbnail_status），
// 也不触发 ffmpeg 重排。
func TestRunIncrementalSkipsUnchanged(t *testing.T) {
	ctx := context.Background()
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() { cat.Close() })

	entries := map[string][]drives.Entry{
		"root": {{ID: "d1", Name: "D1", IsDir: true}},
		"d1": {
			{ID: "f-a", ParentID: "d1", Name: "a.mp4", Size: 100},
			{ID: "f-b", ParentID: "d1", Name: "b.mp4", Size: 200},
		},
	}
	drv := &scannerTreeFakeDrive{entries: entries}

	// 首次扫盘：增量关闭
	sc1 := New(cat, drv, []string{".mp4"}, nil, nil)
	stats1, err := sc1.Run(ctx, "")
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if stats1.Added != 2 || stats1.Skipped != 0 {
		t.Fatalf("first scan: Added=%d Skipped=%d, want 2 0", stats1.Added, stats1.Skipped)
	}

	// 模拟 thumbnail 已生成的副作用；如果增量扫错误地覆盖了它会触发重排。
	seedTime := time.Now().UnixMilli()
	for _, id := range []string{"fake-drive-f-a", "fake-drive-f-b"} {
		if err := cat.UpdatePreview(ctx, id, "/tmp/fake-"+id+".mp4", "ready"); err != nil {
			t.Fatalf("update preview: %v", err)
		}
	}
	beforeUpdatedAt := map[string]int64{}
	for _, id := range []string{"fake-drive-f-a", "fake-drive-f-b"} {
		v, err := cat.GetVideo(ctx, id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		beforeUpdatedAt[id] = v.UpdatedAt.UnixMilli()
	}
	if beforeUpdatedAt["fake-drive-f-a"] < seedTime {
		t.Fatalf("seed time not applied: %v", beforeUpdatedAt)
	}

	// 第二次扫盘：增量开启 + Concurrency=4（也验证快路径在并发模式下工作）
	sc2 := NewWithOptions(cat, drv, []string{".mp4"}, nil, nil, true)
	sc2.Concurrency = 4
	stats2, err := sc2.Run(ctx, "")
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if stats2.Added != 0 {
		t.Fatalf("incremental Added = %d, want 0 (all unchanged)", stats2.Added)
	}
	if stats2.Skipped != 2 {
		t.Fatalf("incremental Skipped = %d, want 2", stats2.Skipped)
	}
	if stats2.Scanned != 0 {
		t.Fatalf("incremental Scanned = %d, want 0 (fast path bypasses Scanned counter)", stats2.Scanned)
	}
	if len(stats2.SeenFileIDs) != 2 {
		t.Fatalf("incremental SeenFileIDs = %d, want 2", len(stats2.SeenFileIDs))
	}

	// 关键不变量：preview_status / updated_at 都没被触碰。
	for id, ts := range beforeUpdatedAt {
		v, err := cat.GetVideo(ctx, id)
		if err != nil {
			t.Fatalf("get %s after incremental: %v", id, err)
		}
		if v.PreviewStatus != "ready" {
			t.Fatalf("%s preview_status = %q, want ready (fast path should not touch row)", id, v.PreviewStatus)
		}
		if v.UpdatedAt.UnixMilli() != ts {
			t.Fatalf("%s updated_at changed: before=%d after=%d (fast path should not touch row)", id, ts, v.UpdatedAt.UnixMilli())
		}
	}
}

// TestRunIncrementalDetectsRenamedAndResized 验证增量扫能识别 file_name 或
// size 变化：fallthrough 到原有 update 路径而不是错误地跳过。
func TestRunIncrementalDetectsRenamedAndResized(t *testing.T) {
	ctx := context.Background()
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() { cat.Close() })

	entries1 := map[string][]drives.Entry{
		"root": {{ID: "d1", Name: "D1", IsDir: true}},
		"d1":   {{ID: "f-1", ParentID: "d1", Name: "old.mp4", Size: 100}},
	}
	drv1 := &scannerTreeFakeDrive{entries: entries1}

	sc1 := New(cat, drv1, []string{".mp4"}, nil, nil)
	if _, err := sc1.Run(ctx, ""); err != nil {
		t.Fatalf("first scan: %v", err)
	}

	// 用户改了文件名 / size（模拟网盘上文件被覆盖）
	entries2 := map[string][]drives.Entry{
		"root": {{ID: "d1", Name: "D1", IsDir: true}},
		"d1":   {{ID: "f-1", ParentID: "d1", Name: "new.mp4", Size: 200}},
	}
	drv2 := &scannerTreeFakeDrive{entries: entries2}

	sc2 := NewWithOptions(cat, drv2, []string{".mp4"}, nil, nil, true)
	stats, err := sc2.Run(ctx, "")
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if stats.Skipped != 0 {
		t.Fatalf("Skipped = %d, want 0 (file_name/size changed — must fall through)", stats.Skipped)
	}
	if stats.Scanned != 1 {
		t.Fatalf("Scanned = %d, want 1 (changed file should run update path)", stats.Scanned)
	}
	got, err := cat.GetVideo(ctx, "fake-drive-f-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.FileName != "new.mp4" || got.Size != 200 {
		t.Fatalf("file_name/size not updated: got %s/%d, want new.mp4/200", got.FileName, got.Size)
	}
}

// BenchmarkScanFanOut 对比不同 Concurrency 在"50 目录 × 50 文件"扇形树上的扫描耗时。
// 树是 BFS 友好形状（广度大，深度 1），最能体现并发收益。10000 文件是模拟
// "上万份文件" 的目标规模，但 SQLite 写入在 Windows 测试机上较慢，缩小规模
// 让 CI 也能跑完。
//
// 用法：go test -bench BenchmarkScanFanOut -benchmem ./internal/scanner/...
func BenchmarkScanFanOut(b *testing.B) {
	const (
		numDirs    = 50
		filesPerDir = 50
	)
	entries := map[string][]drives.Entry{
		"root": make([]drives.Entry, 0, numDirs),
	}
	for d := 0; d < numDirs; d++ {
		dirID := fmt.Sprintf("dir-%d", d)
		entries["root"] = append(entries["root"], drives.Entry{
			ID: dirID, Name: fmt.Sprintf("D%d", d), IsDir: true,
		})
		files := make([]drives.Entry, 0, filesPerDir)
		for f := 0; f < filesPerDir; f++ {
			files = append(files, drives.Entry{
				ID:       fmt.Sprintf("f-%d-%d", d, f),
				ParentID: dirID,
				Name:     fmt.Sprintf("dir%d-clip-%d.mp4", d, f),
				Size:     int64(100 + f),
			})
		}
		entries[dirID] = files
	}

	for _, concurrency := range []int{1, 4, 8} {
		b.Run(fmt.Sprintf("Concurrency=%d", concurrency), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				cat, err := catalog.Open(b.TempDir() + "/catalog.db")
				if err != nil {
					b.Fatalf("open catalog: %v", err)
				}
				drv := &scannerTreeFakeDrive{entries: entries}
				sc := New(cat, drv, []string{".mp4"}, nil, nil)
				sc.Concurrency = concurrency
				if _, err := sc.Run(context.Background(), ""); err != nil {
					b.Fatalf("scan: %v", err)
				}
				if err := cat.Close(); err != nil {
					b.Fatalf("close: %v", err)
				}
			}
		})
	}
}
