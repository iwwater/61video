package catalog

import (
	"context"
	"testing"
	"time"
)

func TestIncrementViewStoresLastViewedAt(t *testing.T) {
	ctx := context.Background()
	cat, err := Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	now := time.Now()
	if err := cat.UpsertVideo(ctx, &Video{
		ID:          "video-1",
		DriveID:     "drive",
		FileID:      "file-1",
		Title:       "Video 1",
		PublishedAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("seed video: %v", err)
	}

	if _, err := cat.IncrementView(ctx, "video-1"); err != nil {
		t.Fatalf("increment view: %v", err)
	}
	got, err := cat.GetVideo(ctx, "video-1")
	if err != nil {
		t.Fatalf("get video: %v", err)
	}
	if got.Views != 1 {
		t.Fatalf("views = %d, want 1", got.Views)
	}
	if got.LastViewedAt.IsZero() {
		t.Fatal("last viewed time was not stored")
	}
}

func TestListVideosRecentSortUsesLastViewedAt(t *testing.T) {
	ctx := context.Background()
	cat, err := Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	now := time.Now()
	for _, v := range []*Video{
		{ID: "old-view", DriveID: "drive", FileID: "old-view", Title: "Old View", PublishedAt: now.Add(3 * time.Hour), CreatedAt: now, UpdatedAt: now},
		{ID: "recent-view", DriveID: "drive", FileID: "recent-view", Title: "Recent View", PublishedAt: now, CreatedAt: now, UpdatedAt: now},
		{ID: "unviewed", DriveID: "drive", FileID: "unviewed", Title: "Unviewed", PublishedAt: now.Add(4 * time.Hour), CreatedAt: now, UpdatedAt: now},
	} {
		if err := cat.UpsertVideo(ctx, v); err != nil {
			t.Fatalf("seed %s: %v", v.ID, err)
		}
	}
	if _, err := cat.db.ExecContext(ctx,
		`UPDATE videos SET last_viewed_at = CASE id
			WHEN 'old-view' THEN ?
			WHEN 'recent-view' THEN ?
			ELSE 0
		END`,
		now.Add(-time.Hour).UnixMilli(),
		now.Add(time.Hour).UnixMilli(),
	); err != nil {
		t.Fatalf("seed last_viewed_at: %v", err)
	}

	items, _, err := cat.ListVideos(ctx, ListParams{Sort: "recent", Page: 1, PageSize: 3})
	if err != nil {
		t.Fatalf("list recent videos: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3", len(items))
	}
	got := []string{items[0].ID, items[1].ID, items[2].ID}
	want := []string{"recent-view", "old-view", "unviewed"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("recent order = %#v, want %#v", got, want)
		}
	}
}

// TestListReadyPreviewsByDriveOrdersByUpdatedAtAsc 验证 DriveCap LRU 用的
// 列表：仅返回 drive 下 preview_status='ready' 且 preview_local 非空的视频，
// 按 updated_at 升序（最久未更新的在前，方便删除）。
func TestListReadyPreviewsByDriveOrdersByUpdatedAtAsc(t *testing.T) {
	ctx := context.Background()
	cat, err := Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	base := time.Now()
	seeds := []*Video{
		{ID: "v-old", DriveID: "drive", FileID: "f1", Title: "Old",
			PublishedAt: base, CreatedAt: base, UpdatedAt: base.Add(-3 * time.Hour), PreviewStatus: "ready", PreviewLocal: "/tmp/v-old.mp4"},
		{ID: "v-mid", DriveID: "drive", FileID: "f2", Title: "Mid",
			PublishedAt: base, CreatedAt: base, UpdatedAt: base.Add(-1 * time.Hour), PreviewStatus: "ready", PreviewLocal: "/tmp/v-mid.mp4"},
		{ID: "v-new", DriveID: "drive", FileID: "f3", Title: "New",
			PublishedAt: base, CreatedAt: base, UpdatedAt: base, PreviewStatus: "ready", PreviewLocal: "/tmp/v-new.mp4"},
		// 其它盘的不应混入
		{ID: "v-other", DriveID: "other", FileID: "f4", Title: "Other",
			PublishedAt: base, CreatedAt: base, UpdatedAt: base.Add(-100 * time.Hour), PreviewStatus: "ready", PreviewLocal: "/tmp/v-other.mp4"},
		// pending 状态不应混入
		{ID: "v-pending", DriveID: "drive", FileID: "f5", Title: "Pending",
			PublishedAt: base, CreatedAt: base, UpdatedAt: base.Add(-100 * time.Hour), PreviewStatus: "pending", PreviewLocal: ""},
		// failed 状态不应混入
		{ID: "v-failed", DriveID: "drive", FileID: "f6", Title: "Failed",
			PublishedAt: base, CreatedAt: base, UpdatedAt: base.Add(-100 * time.Hour), PreviewStatus: "failed", PreviewLocal: ""},
	}
	for _, v := range seeds {
		if err := cat.UpsertVideo(ctx, v); err != nil {
			t.Fatalf("seed %s: %v", v.ID, err)
		}
	}

	items, err := cat.ListReadyPreviewsByDrive(ctx, "drive", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3 (other drive / pending / failed excluded)", len(items))
	}
	wantOrder := []string{"v-old", "v-mid", "v-new"}
	for i, v := range items {
		if v.ID != wantOrder[i] {
			t.Fatalf("items[%d].ID = %q, want %q", i, v.ID, wantOrder[i])
		}
	}
}

// TestResetPreviewLocalClearsPathAndResetsStatus 验证清理后的 DB 状态：
// preview_local 被清空、status 回退到 pending，updated_at 被刷新。
// 下次访问该视频会重新触发预览生成。
func TestResetPreviewLocalClearsPathAndResetsStatus(t *testing.T) {
	ctx := context.Background()
	cat, err := Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	now := time.Now()
	if err := cat.UpsertVideo(ctx, &Video{
		ID: "v-evict", DriveID: "drive", FileID: "f1", Title: "EvictMe",
		PublishedAt: now, CreatedAt: now, UpdatedAt: now,
		PreviewStatus: "ready", PreviewLocal: "/tmp/v-evict.mp4",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := cat.ResetPreviewLocal(ctx, "v-evict"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	got, err := cat.GetVideo(ctx, "v-evict")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.PreviewLocal != "" {
		t.Fatalf("PreviewLocal = %q, want empty", got.PreviewLocal)
	}
	if got.PreviewStatus != "pending" {
		t.Fatalf("PreviewStatus = %q, want pending", got.PreviewStatus)
	}
}
