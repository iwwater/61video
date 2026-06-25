package catalog

import (
	"context"
	"testing"
	"time"
)

// TestListVideosMediaTypeFilter 验证 ListParams.MediaType 过滤 video / audio。
func TestListVideosMediaTypeFilter(t *testing.T) {
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
	// seed 3 视频 + 2 音频
	for _, v := range []*Video{
		{ID: "v1", DriveID: "drive", FileID: "v1", Title: "视频 1", PublishedAt: now, CreatedAt: now, UpdatedAt: now},
		{ID: "v2", DriveID: "drive", FileID: "v2", Title: "视频 2", PublishedAt: now, CreatedAt: now, UpdatedAt: now},
		{ID: "v3", DriveID: "drive", FileID: "v3", Title: "视频 3", PublishedAt: now, CreatedAt: now, UpdatedAt: now},
		{ID: "a1", DriveID: "drive", FileID: "a1", Title: "音频 1", MediaType: "audio", PublishedAt: now, CreatedAt: now, UpdatedAt: now},
		{ID: "a2", DriveID: "drive", FileID: "a2", Title: "音频 2", MediaType: "audio", PublishedAt: now, CreatedAt: now, UpdatedAt: now},
	} {
		if err := cat.UpsertVideo(ctx, v); err != nil {
			t.Fatalf("seed %s: %v", v.ID, err)
		}
	}

	type tc struct {
		name      string
		mediaType string
		wantIDs   []string // 期望返回的 id 子集(无序)
	}
	cases := []tc{
		{"all", "", []string{"v1", "v2", "v3", "a1", "a2"}},
		{"only_video", "video", []string{"v1", "v2", "v3"}},
		{"only_audio", "audio", []string{"a1", "a2"}},
		{"invalid_falls_through", "garbage", []string{"v1", "v2", "v3", "a1", "a2"}},
		{"case_insensitive", "AUDIO", []string{"a1", "a2"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			items, total, err := cat.ListVideos(ctx, ListParams{
				MediaType: c.mediaType,
				Page:      1,
				PageSize:  100,
			})
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if total != len(c.wantIDs) {
				t.Fatalf("total = %d, want %d", total, len(c.wantIDs))
			}
			got := make(map[string]bool, len(items))
			for _, v := range items {
				got[v.ID] = true
			}
			for _, id := range c.wantIDs {
				if !got[id] {
					t.Errorf("missing id %s in results", id)
				}
			}
			if len(items) != len(c.wantIDs) {
				t.Errorf("items count = %d, want %d", len(items), len(c.wantIDs))
			}
		})
	}
}