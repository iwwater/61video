// Package systemtags 提供"系统分类标签"的内存缓存 + 持久化。
//
// system 标签以前硬编码在 internal/fixedtags，扩展和编辑只能改代码重编译。
// 现在通过 settings 表的 system_tags key 持久化，启动时一次性加载到内存，
// 后续 tag 匹配 / 自动归类都从内存读；后台修改后刷新内存缓存。
//
// 持久化格式：
//   key:   "system_tags"
//   value: JSON 数组，每个元素 { "label": "...", "aliases": [...] }
//
// 首次启动时 settings 为空 → 自动用 fixedtags.DefaultSystemTags() 作为种子，
// 写回 settings，保持向后兼容。
package systemtags

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/video-site/backend/internal/fixedtags"
)

const SettingsKey = "system_tags"

type Tag struct {
	Label   string   `json:"label"`
	Aliases []string `json:"aliases"`
}

// Store 内存里的 system 标签集合，进程内单例。
type Store struct {
	mu     sync.RWMutex
	tags   []Tag
	byLabel map[string]Tag
}

func NewStore() *Store {
	defaults := fixedtags.DefaultSystemTags()
	seed := make([]Tag, len(defaults))
	for i, t := range defaults {
		seed[i] = Tag{Label: t.Label, Aliases: t.Aliases}
	}
	return &Store{
		tags:    seed,
		byLabel: indexByLabel(seed),
	}
}

// Load 从 settings 表加载一次。如果 settings 为空，用 fixedtags 种子初始化并写回。
func (s *Store) Load(ctx context.Context, db *sql.DB) error {
	value, err := readSetting(ctx, db, SettingsKey)
	if err != nil {
		return err
	}

	var tags []Tag
	if value == "" {
		defaults := fixedtags.DefaultSystemTags()
		tags = make([]Tag, len(defaults))
		for i, t := range defaults {
			tags[i] = Tag{Label: t.Label, Aliases: t.Aliases}
		}
		// 首次启动：把种子写回 settings，下次直接读
		if err := writeSetting(ctx, db, SettingsKey, tags); err != nil {
			return err
		}
	} else {
		if err := json.Unmarshal([]byte(value), &tags); err != nil {
			return fmt.Errorf("decode %s: %w", SettingsKey, err)
		}
	}

	tags = normalizeTags(tags)
	s.replace(tags)
	return nil
}

// Replace 替换内存中的标签集（来自后台编辑）。不写库，由调用方写。
func (s *Store) Replace(tags []Tag) {
	s.replace(normalizeTags(tags))
}

// Snapshot 返回当前标签集的副本（按 label 排序），用于前后端交互和持久化。
func (s *Store) Snapshot() []Tag {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Tag, len(s.tags))
	for i, t := range s.tags {
		out[i] = Tag{
			Label:   t.Label,
			Aliases: append([]string(nil), t.Aliases...),
		}
	}
	return out
}

// Labels 返回当前所有 system 标签的 label（按字母序排序），与原 fixedtags.Labels 等价。
func (s *Store) Labels() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.tags))
	for _, t := range s.tags {
		out = append(out, t.Label)
	}
	return out
}

// AliasesFor 返回指定标签的别名副本。
func (s *Store) AliasesFor(label string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if t, ok := s.byLabel[label]; ok {
		out := make([]string, len(t.Aliases))
		copy(out, t.Aliases)
		return out
	}
	return nil
}

// MatchFilename 扫描文件名，命中任何 system 标签的别名就返回对应 label。
// 与 fixedtags.MatchFilename 等价，但数据来自可编辑的 settings。
func (s *Store) MatchFilename(name string) []string {
	s.mu.RLock()
	tags := append([]Tag(nil), s.tags...)
	s.mu.RUnlock()

	text := normalizeText(name)
	var out []string
	for _, t := range tags {
		for _, alias := range t.Aliases {
			if text.contains(alias) {
				out = append(out, t.Label)
				break
			}
		}
	}
	return out
}

// Save 写库 + 刷新内存。后台编辑器调用。
func Save(ctx context.Context, db *sql.DB, tags []Tag) error {
	tags = normalizeTags(tags)
	if err := writeSetting(ctx, db, SettingsKey, tags); err != nil {
		return err
	}
	// 调用方持有 Store 引用，自己负责 Replace。
	return nil
}

// ---------- 内部 ----------

func (s *Store) replace(tags []Tag) {
	sort.Slice(tags, func(i, j int) bool { return tags[i].Label < tags[j].Label })
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tags = tags
	s.byLabel = indexByLabel(tags)
}

func indexByLabel(tags []Tag) map[string]Tag {
	m := make(map[string]Tag, len(tags))
	for _, t := range tags {
		m[t.Label] = t
	}
	return m
}

func cloneTags(in []Tag) []Tag {
	out := make([]Tag, len(in))
	for i, t := range in {
		out[i] = Tag{
			Label:   t.Label,
			Aliases: append([]string(nil), t.Aliases...),
		}
	}
	return out
}

// normalizeTags 校验 + 排序 + 去空 label。
func normalizeTags(in []Tag) []Tag {
	seen := make(map[string]bool, len(in))
	out := make([]Tag, 0, len(in))
	for _, t := range in {
		label := strings.TrimSpace(t.Label)
		if label == "" || seen[label] {
			continue
		}
		seen[label] = true
		aliases := make([]string, 0, len(t.Aliases))
		for _, a := range t.Aliases {
			a = strings.TrimSpace(a)
			if a != "" {
				aliases = append(aliases, a)
			}
		}
		out = append(out, Tag{Label: label, Aliases: aliases})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

func readSetting(ctx context.Context, db *sql.DB, key string) (string, error) {
	var value string
	err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func writeSetting(ctx context.Context, db *sql.DB, key string, tags []Tag) error {
	payload, err := json.Marshal(tags)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	_, err = db.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, key, string(payload), now)
	return err
}

// ---------- 文件名匹配（与 fixedtags 同步实现） ----------

type normalizedText struct {
	lower   string
	compact string
	tokens  map[string]struct{}
}

func normalizeText(s string) normalizedText {
	lower := strings.ToLower(s)
	var compact strings.Builder
	var spaced strings.Builder
	for _, r := range lower {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			compact.WriteRune(r)
			spaced.WriteRune(r)
			continue
		}
		spaced.WriteByte(' ')
	}

	tokens := make(map[string]struct{})
	for _, token := range strings.Fields(spaced.String()) {
		tokens[token] = struct{}{}
	}

	return normalizedText{
		lower:   lower,
		compact: compact.String(),
		tokens:  tokens,
	}
}

func (n normalizedText) contains(alias string) bool {
	lowerAlias := strings.ToLower(alias)
	compactAlias := compact(lowerAlias)
	if compactAlias == "" {
		return false
	}
	if isShortASCIIWord(compactAlias) && compactAlias == lowerAlias {
		_, ok := n.tokens[compactAlias]
		return ok
	}
	if strings.Contains(n.lower, lowerAlias) {
		return true
	}
	return strings.Contains(n.compact, compactAlias)
}

func compact(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isShortASCIIWord(s string) bool {
	if len(s) > 3 {
		return false
	}
	for _, r := range s {
		if r > unicode.MaxASCII || (!unicode.IsLetter(r) && !unicode.IsDigit(r)) {
			return false
		}
	}
	return true
}