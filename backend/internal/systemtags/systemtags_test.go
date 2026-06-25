package systemtags

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// applySchema 手动建 settings 表（避免依赖 catalog 的完整 schema）。
func applySchema(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at INTEGER NOT NULL
	)`)
	if err != nil {
		t.Fatalf("create settings: %v", err)
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	applySchema(t, db)
	return db
}

func TestNewStoreSeedsFromFixedtags(t *testing.T) {
	store := NewStore()
	labels := store.Labels()
	if len(labels) == 0 {
		t.Fatal("NewStore should seed from fixedtags")
	}
}

func TestNormalizeTagsDedupesAndSorts(t *testing.T) {
	in := []Tag{
		{Label: "  后入  ", Aliases: []string{"a", "", "b"}},
		{Label: "奶子", Aliases: []string{}},
		{Label: "后入", Aliases: []string{"x"}}, // duplicate label
	}
	out := normalizeTags(in)
	if len(out) != 2 {
		t.Fatalf("dedupe failed: got %d, want 2: %#v", len(out), out)
	}
	if out[0].Label != "后入" || out[1].Label != "奶子" {
		t.Fatalf("sort failed: %#v", out)
	}
	// alias trim: 空字符串应被过滤
	if len(out[0].Aliases) != 2 || out[0].Aliases[0] != "a" || out[0].Aliases[1] != "b" {
		t.Fatalf("alias trim failed: %#v", out[0].Aliases)
	}
}

func TestLoadSaveRoundTrip(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	ctx := context.Background()

	store := NewStore()
	if err := store.Load(ctx, db); err != nil {
		t.Fatalf("first Load: %v", err)
	}
	if len(store.Snapshot()) == 0 {
		t.Fatal("first snapshot should have seed tags")
	}

	updated := []Tag{
		{Label: "萝莉", Aliases: []string{"loli", "ロリ"}},
		{Label: "清纯", Aliases: []string{"pure"}},
	}
	if err := Save(ctx, db, updated); err != nil {
		t.Fatalf("Save: %v", err)
	}
	store.Replace(updated)

	store2 := NewStore()
	if err := store2.Load(ctx, db); err != nil {
		t.Fatalf("second Load: %v", err)
	}
	got := store2.Snapshot()
	if len(got) != 2 {
		t.Fatalf("got %d tags, want 2", len(got))
	}
	// normalizeTags 按 label 排序："清纯" < "萝莉"（按 codepoint）
	if got[0].Label != "清纯" || got[0].Aliases[0] != "pure" {
		t.Fatalf("data mismatch[0]: %#v", got[0])
	}
	if got[1].Label != "萝莉" || got[1].Aliases[0] != "loli" {
		t.Fatalf("data mismatch[1]: %#v", got[1])
	}

	// 验证 settings 表里存的是 JSON 数组
	var value string
	if err := db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", SettingsKey).Scan(&value); err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var raw []map[string]any
	if err := json.Unmarshal([]byte(value), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(raw) != 2 {
		t.Fatalf("settings should have 2 entries, got %d", len(raw))
	}
}

func TestMatchFilenameUsesReplacedTags(t *testing.T) {
	store := NewStore()
	store.Replace([]Tag{
		{Label: "户外", Aliases: []string{"outdoor", "野外"}},
	})
	matched := store.MatchFilename("Outdoor野外拍摄.mp4")
	if len(matched) != 1 || matched[0] != "户外" {
		t.Fatalf("expected [户外], got %#v", matched)
	}
}