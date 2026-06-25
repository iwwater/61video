package catalog

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestFTSUpsertSyncsToIndex(t *testing.T) {
	ctx := context.Background()
	cat, err := Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() { _ = cat.Close() })

	now := time.Now()
	v := &Video{
		ID:          "v1",
		DriveID:     "drive",
		FileID:      "f1",
		Title:       "黎明破晓的风景纪录片",
		Description: "记录黄昏到清晨的光影变化",
		Tags:        []string{"风景记录", "延时摄影", "自然生态"},
		PublishedAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := cat.UpsertVideo(ctx, v); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// FTS 应该立刻能搜到——触发器是 AFTER INSERT/AFTER UPDATE。
	// 标题里 4 字短语 "破晓的风景" 一定能被 trigram 命中（中间 trigram 就是 "破晓的"）。
	got, total, err := cat.SearchVideos(ctx, "破晓的风景", SearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("search 破晓的风景: %v", err)
	}
	if total != 1 || len(got) != 1 || got[0].ID != "v1" {
		t.Fatalf("search 破晓的风景 = %d (total %d), want 1 hit on v1", len(got), total)
	}

	// description 也能搜
	got, total, err = cat.SearchVideos(ctx, "黄昏到清晨", SearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("search 黄昏到清晨: %v", err)
	}
	if total != 1 || len(got) != 1 {
		t.Fatalf("search 黄昏到清晨 should hit description, got %d/%d", len(got), total)
	}

	// 标签（折叠 JSON 后）也能搜——用 3+ 字符 tag 名（trigram 限制）
	got, total, err = cat.SearchVideos(ctx, "延时摄影", SearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("search 延时摄影: %v", err)
	}
	if total != 1 || len(got) != 1 {
		t.Fatalf("search 延时摄影 should hit tags JSON, got %d/%d", len(got), total)
	}
}

func TestFTSUpdateSyncsToIndex(t *testing.T) {
	ctx := context.Background()
	cat, err := Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() { _ = cat.Close() })

	now := time.Now()
	if err := cat.UpsertVideo(ctx, &Video{
		ID: "v1", DriveID: "drive", FileID: "f1",
		Title: "原标题 alpha", Description: "原描述",
		PublishedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// 确认 alpha 能搜到
	if _, total, err := cat.SearchVideos(ctx, "alpha", SearchOptions{}); err != nil || total != 1 {
		t.Fatalf("pre-update search alpha: total=%d err=%v", total, err)
	}

	// 改标题（通过 UpsertVideo 的 ON CONFLICT UPDATE 分支）
	if err := cat.UpsertVideo(ctx, &Video{
		ID: "v1", DriveID: "drive", FileID: "f1",
		Title: "新标题 bravo", Description: "新描述 charlie",
		Tags:        []string{"delta"},
		PublishedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	// 老词 alpha / 原描述 应该搜不到了
	if _, total, err := cat.SearchVideos(ctx, "alpha", SearchOptions{}); err != nil || total != 0 {
		t.Fatalf("post-update search alpha should miss, got total=%d err=%v", total, err)
	}
	if _, total, err := cat.SearchVideos(ctx, "原描述", SearchOptions{}); err != nil || total != 0 {
		t.Fatalf("post-update search 原描述 should miss, got total=%d err=%v", total, err)
	}

	// 新词应该能搜到
	if _, total, err := cat.SearchVideos(ctx, "bravo", SearchOptions{}); err != nil || total != 1 {
		t.Fatalf("post-update search bravo: total=%d err=%v", total, err)
	}
	if _, total, err := cat.SearchVideos(ctx, "charlie", SearchOptions{}); err != nil || total != 1 {
		t.Fatalf("post-update search charlie: total=%d err=%v", total, err)
	}
	if _, total, err := cat.SearchVideos(ctx, "delta", SearchOptions{}); err != nil || total != 1 {
		t.Fatalf("post-update search delta tag: total=%d err=%v", total, err)
	}
}

func TestFTSDeleteRemovesFromIndex(t *testing.T) {
	ctx := context.Background()
	cat, err := Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() { _ = cat.Close() })

	now := time.Now()
	if err := cat.UpsertVideo(ctx, &Video{
		ID: "v1", DriveID: "drive", FileID: "f1", Title: "echo 唯一词",
		PublishedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, total, err := cat.SearchVideos(ctx, "echo", SearchOptions{}); err != nil || total != 1 {
		t.Fatalf("pre-delete: total=%d err=%v", total, err)
	}

	if err := cat.DeleteVideo(ctx, "v1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, total, err := cat.SearchVideos(ctx, "echo", SearchOptions{}); err != nil || total != 0 {
		t.Fatalf("post-delete should miss, got total=%d err=%v", total, err)
	}
}

func TestFTSSearchRankingAndPagination(t *testing.T) {
	ctx := context.Background()
	cat, err := Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() { _ = cat.Close() })

	now := time.Now()
	// 5 个文档：都包含 "猫" 相关内容（trigram 要求 3+ 字符，所以用 "猫咪日记"）
	for i := 0; i < 5; i++ {
		title := "猫咪日记系列"
		desc := "记录喵星人日常"
		if i < 2 {
			desc = "猫咪和狗狗一起玩耍"
		}
		if err := cat.UpsertVideo(ctx, &Video{
			ID:          "v" + string(rune('a'+i)),
			DriveID:     "drive",
			FileID:      "f" + string(rune('a'+i)),
			Title:       title,
			Description: desc,
			PublishedAt: now.Add(time.Duration(i) * time.Second),
			CreatedAt:   now,
			UpdatedAt:   now,
		}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	// 搜 "猫咪日记" 应该返回 5 条，total=5
	all, total, err := cat.SearchVideos(ctx, "猫咪日记", SearchOptions{Limit: 100})
	if err != nil {
		t.Fatalf("search 猫咪日记: %v", err)
	}
	if total != 5 || len(all) != 5 {
		t.Fatalf("search 猫咪日记: total=%d items=%d, want 5/5", total, len(all))
	}

	// 搜 "猫咪日记 狗狗一起"（空格当 AND，trigram 每个 token ≥3 字符）应该返回 2 条
	both, total, err := cat.SearchVideos(ctx, "猫咪日记 狗狗一起", SearchOptions{Limit: 100})
	if err != nil {
		t.Fatalf("search 猫咪日记 狗狗一起: %v", err)
	}
	if total != 2 || len(both) != 2 {
		t.Fatalf("search 猫咪日记 狗狗一起: total=%d items=%d, want 2/2", total, len(both))
	}

	// 分页：limit=2, offset=0 → 前 2 条
	page1, total, err := cat.SearchVideos(ctx, "猫咪日记", SearchOptions{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if total != 5 {
		t.Fatalf("page1 total=%d, want 5", total)
	}
	if len(page1) != 2 {
		t.Fatalf("page1 items=%d, want 2", len(page1))
	}

	// limit=2, offset=2 → 中间 2 条
	page2, _, err := cat.SearchVideos(ctx, "猫咪日记", SearchOptions{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("page2 items=%d, want 2", len(page2))
	}

	// 分页结果不应该有重叠
	seen := map[string]struct{}{}
	for _, v := range page1 {
		seen[v.ID] = struct{}{}
	}
	for _, v := range page2 {
		if _, dup := seen[v.ID]; dup {
			t.Fatalf("pagination overlap: %s in page1 and page2", v.ID)
		}
	}
}

func TestFTSSearchMediaTypeAndHidden(t *testing.T) {
	ctx := context.Background()
	cat, err := Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() { _ = cat.Close() })

	now := time.Now()
	if err := cat.UpsertVideo(ctx, &Video{
		ID: "v-video", DriveID: "drive", FileID: "fv", Title: "foxtrot 视频",
		MediaType: "video", PublishedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed v-video: %v", err)
	}
	if err := cat.UpsertVideo(ctx, &Video{
		ID: "v-audio", DriveID: "drive", FileID: "fa", Title: "foxtrot 音频",
		MediaType: "audio", PublishedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed v-audio: %v", err)
	}
	if err := cat.UpsertVideo(ctx, &Video{
		ID: "v-hidden", DriveID: "drive", FileID: "fh", Title: "foxtrot 隐藏",
		PublishedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed v-hidden: %v", err)
	}
	if err := cat.HideVideo(ctx, "v-hidden"); err != nil {
		t.Fatalf("hide: %v", err)
	}

	// 不带 type：2 条命中（hidden 被排除）
	got, total, err := cat.SearchVideos(ctx, "foxtrot", SearchOptions{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if total != 2 || len(got) != 2 {
		t.Fatalf("unfiltered: total=%d items=%d, want 2/2", total, len(got))
	}

	// type=video：只 1 条
	got, total, err = cat.SearchVideos(ctx, "foxtrot", SearchOptions{MediaType: "video"})
	if err != nil {
		t.Fatalf("video filter: %v", err)
	}
	if total != 1 || len(got) != 1 || got[0].ID != "v-video" {
		t.Fatalf("video filter: total=%d items=%d, want only v-video", total, len(got))
	}

	// type=audio：只 1 条
	got, total, err = cat.SearchVideos(ctx, "foxtrot", SearchOptions{MediaType: "audio"})
	if err != nil {
		t.Fatalf("audio filter: %v", err)
	}
	if total != 1 || len(got) != 1 || got[0].ID != "v-audio" {
		t.Fatalf("audio filter: total=%d items=%d, want only v-audio", total, len(got))
	}

	// 非法 type 一律按不限处理
	got, total, err = cat.SearchVideos(ctx, "foxtrot", SearchOptions{MediaType: "garbage"})
	if err != nil {
		t.Fatalf("garbage filter: %v", err)
	}
	if total != 2 || len(got) != 2 {
		t.Fatalf("garbage filter should be unfiltered, got total=%d", total)
	}
}

func TestSanitizeFTSQuery(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"   ", ""},
		{"hello", "hello*"},
		{"hello world", "hello* world*"},
		// FTS5 元字符被替换成空格，多个空格折叠成一个，每个 token 加 *
		{`foo"bar`, "foo* bar*"},
		{"foo:bar", "foo* bar*"},
		{"foo*", "foo*"},
		{"foo(bar)", "foo* bar*"},
		{"多关键词 测试", "多关键词* 测试*"},
		{"　 全角　空白 ", "全角* 空白*"},
	}
	for _, c := range cases {
		got := sanitizeFTSQuery(c.in)
		if got != c.want {
			t.Errorf("sanitizeFTSQuery(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFTSSearchSpecialCharsDoNotError(t *testing.T) {
	ctx := context.Background()
	cat, err := Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() { _ = cat.Close() })

	now := time.Now()
	if err := cat.UpsertVideo(ctx, &Video{
		ID: "v1", DriveID: "drive", FileID: "f1", Title: "normal title",
		PublishedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// 这些查询以前会让 FTS5 解析报错，应该被 sanitize 安全处理
	queries := []string{
		`"unbalanced quote`,
		`foo:bar*`,
		`a (b c) -d +e`,
		`***`,
		`^^^`,
		`中文全角符号：括号（）`,
	}
	for _, q := range queries {
		got, total, err := cat.SearchVideos(ctx, q, SearchOptions{})
		if err != nil {
			t.Errorf("query %q returned error: %v", q, err)
			continue
		}
		_ = got
		_ = total
	}
}

func TestFTSBackfillOnFirstOpen(t *testing.T) {
	// 模拟"已存在的 DB 里 videos 表有数据但 videos_fts 还是空"的情况：
	// 在临时目录先创建一个 catalog，把数据写进去。然后再 Open 一次（重新跑 schema.sql），
	// 确认回填 marker 写好了，搜索能命中旧数据。
	dir := t.TempDir()
	dbPath := dir + "/catalog.db"

	{
		cat, err := Open(dbPath)
		if err != nil {
			t.Fatalf("first open: %v", err)
		}
		ctx := context.Background()
		now := time.Now()
		if err := cat.UpsertVideo(ctx, &Video{
			ID: "v-pre", DriveID: "drive", FileID: "fp",
			Title: "回填测试 golf", Description: "这是已经存在的老数据",
			PublishedAt: now, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
		_ = cat.Close()
	}

	// 第二次打开 → schema.sql 重新跑，但 videos 已经存在。
	// 关键点：videos_fts 是新加的，第一次跑过 migrate 之前没有 marker，
	// 所以 backfillVideosFTS 会把 v-pre 写进 FTS 索引。
	cat, err := Open(dbPath)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	t.Cleanup(func() { _ = cat.Close() })

	got, total, err := cat.SearchVideos(context.Background(), "golf", SearchOptions{})
	if err != nil {
		t.Fatalf("search golf: %v", err)
	}
	if total != 1 || len(got) != 1 || got[0].ID != "v-pre" {
		t.Fatalf("backfill search golf: total=%d items=%d, want v-pre", total, len(got))
	}

	// 老数据 description 也能搜到
	got, total, err = cat.SearchVideos(context.Background(), "老数据", SearchOptions{})
	if err != nil {
		t.Fatalf("search 老数据: %v", err)
	}
	if total != 1 || len(got) != 1 {
		t.Fatalf("backfill description: total=%d, want 1", total)
	}

	// 回填 marker 应该被写好了——再 Open 一次不会重复回填。
	marker, err := cat.GetSetting(context.Background(), ftsBackfillMarkerKey, "")
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if marker != "1" {
		t.Fatalf("backfill marker = %q, want 1", marker)
	}
}

func TestFTSSearchEmptyQueryReturnsNil(t *testing.T) {
	ctx := context.Background()
	cat, err := Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = cat.Close() })

	now := time.Now()
	if err := cat.UpsertVideo(ctx, &Video{
		ID: "v1", DriveID: "drive", FileID: "f1", Title: "hello",
		PublishedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	for _, q := range []string{"", "   ", `***"`} {
		got, total, err := cat.SearchVideos(ctx, q, SearchOptions{})
		if err != nil {
			t.Errorf("query %q err: %v", q, err)
		}
		if got != nil || total != 0 {
			t.Errorf("query %q: got=%v total=%d, want nil/0", q, got, total)
		}
	}
}

func TestFTSSearchBM25Ordering(t *testing.T) {
	ctx := context.Background()
	cat, err := Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = cat.Close() })

	now := time.Now()
	// v-hot：标题/描述里都有 golf，多次命中
	// v-warm：只有标题里有 golf，1 次命中
	// v-cold：只有描述里有 golf，1 次命中
	if err := cat.UpsertVideo(ctx, &Video{
		ID: "v-hot", DriveID: "drive", FileID: "fh",
		Title: "golf 教学 golf 入门", Description: "GOLF 实战指南",
		PublishedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed v-hot: %v", err)
	}
	if err := cat.UpsertVideo(ctx, &Video{
		ID: "v-warm", DriveID: "drive", FileID: "fw",
		Title: "golf 简单入门", Description: "户外运动",
		PublishedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed v-warm: %v", err)
	}
	if err := cat.UpsertVideo(ctx, &Video{
		ID: "v-cold", DriveID: "drive", FileID: "fc",
		Title: "网球入门", Description: "与 golf 类似",
		PublishedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed v-cold: %v", err)
	}

	got, total, err := cat.SearchVideos(ctx, "golf", SearchOptions{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if total != 3 {
		t.Fatalf("total=%d, want 3", total)
	}
	if len(got) < 1 || got[0].ID != "v-hot" {
		var ids []string
		for _, v := range got {
			ids = append(ids, v.ID)
		}
		t.Fatalf("bm25 ranking: v-hot should be first, got order %s", strings.Join(ids, ","))
	}
}
