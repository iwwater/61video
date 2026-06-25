package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// NovelSet 一本小说/漫画/PDF 元数据 + 章节列表。
type NovelSet struct {
	ID           string         `json:"id"`
	DriveID      string         `json:"driveId"`
	SourceID     string         `json:"sourceId"`
	Title        string         `json:"title"`
	Author       string         `json:"author"`
	CoverURL     string         `json:"coverUrl"`
	ContentType  string         `json:"contentType"` // text | pdf
	ChapterCount int            `json:"chapterCount"`
	Tags         []string       `json:"tags"`
	Description  string         `json:"description"`
	Hidden       bool           `json:"hidden"`
	SourceKind   string         `json:"sourceKind"`
	PublishedAt  int64          `json:"publishedAt"`
	CreatedAt    int64          `json:"createdAt"`
	UpdatedAt    int64          `json:"updatedAt"`
	Chapters     []NovelChapter `json:"chapters,omitempty"` // 仅 GetNovelSet 时填充
}

// NovelChapter 小说中的一章/一节/PDF 文件。
type NovelChapter struct {
	ID          int64             `json:"id"`
	Position    int               `json:"position"`
	Title       string            `json:"title"`
	ContentType string            `json:"contentType"` // text | pdf
	Body        string            `json:"body,omitempty"`
	PDFURL      string            `json:"pdfUrl,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

// UpsertNovelSet 整体替换小说（id 必填；存在则更新元数据 + 清空并重建章节列表）。
func (c *Catalog) UpsertNovelSet(ctx context.Context, s *NovelSet) error {
	if s.ID == "" {
		return errors.New("novel set id is required")
	}
	if strings.TrimSpace(s.Title) == "" {
		return errors.New("novel set title is required")
	}
	if s.ContentType == "" {
		s.ContentType = "text"
	}
	if s.ContentType != "text" && s.ContentType != "pdf" {
		return fmt.Errorf("contentType must be 'text' or 'pdf', got %q", s.ContentType)
	}
	now := time.Now().UnixMilli()
	if s.CreatedAt == 0 {
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	if s.SourceKind == "" {
		s.SourceKind = "crawler"
	}
	if len(s.Tags) == 0 {
		s.Tags = []string{}
	}
	tagsJSON, _ := json.Marshal(s.Tags)

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO novel_sets (id, drive_id, source_id, title, author, cover_url,
		                        content_type, chapter_count, tags, description, hidden, source_kind,
		                        created_at, updated_at, published_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			drive_id      = excluded.drive_id,
			source_id     = excluded.source_id,
			title         = excluded.title,
			author        = excluded.author,
			cover_url     = excluded.cover_url,
			content_type  = excluded.content_type,
			chapter_count = excluded.chapter_count,
			tags          = excluded.tags,
			description   = excluded.description,
			hidden        = excluded.hidden,
			source_kind   = excluded.source_kind,
			updated_at    = excluded.updated_at,
			published_at  = excluded.published_at
	`, s.ID, s.DriveID, s.SourceID, s.Title, s.Author, s.CoverURL,
		s.ContentType, len(s.Chapters), string(tagsJSON), s.Description, boolToInt(s.Hidden), s.SourceKind,
		s.CreatedAt, s.UpdatedAt, s.PublishedAt,
	)
	if err != nil {
		return err
	}

	// 整体替换章节列表
	if _, err := tx.ExecContext(ctx, `DELETE FROM novel_chapters WHERE novel_id = ?`, s.ID); err != nil {
		return err
	}
	for i, ch := range s.Chapters {
		ct := ch.ContentType
		if ct == "" {
			ct = s.ContentType
		}
		headersJSON := "{}"
		if len(ch.Headers) > 0 {
			b, _ := json.Marshal(ch.Headers)
			headersJSON = string(b)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO novel_chapters (novel_id, position, title, content_type, body, pdf_url, headers)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, s.ID, i, ch.Title, ct, ch.Body, ch.PDFURL, headersJSON); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetNovelSet 返回小说元数据 + 章节列表。
func (c *Catalog) GetNovelSet(ctx context.Context, id string) (*NovelSet, error) {
	row := c.db.QueryRowContext(ctx, `
		SELECT id, drive_id, source_id, title, author, cover_url,
		       content_type, chapter_count, tags, description, hidden, source_kind,
		       published_at, created_at, updated_at
		FROM novel_sets WHERE id = ?
	`, id)
	s, err := scanNovelSet(row)
	if err != nil {
		return nil, err
	}
	rows, err := c.db.QueryContext(ctx, `
		SELECT id, position, title, content_type, body, pdf_url, headers
		FROM novel_chapters
		WHERE novel_id = ?
		ORDER BY position ASC
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ch NovelChapter
		var body, pdfURL, headersJSON sql.NullString
		if err := rows.Scan(&ch.ID, &ch.Position, &ch.Title, &ch.ContentType, &body, &pdfURL, &headersJSON); err != nil {
			return nil, err
		}
		if body.Valid {
			ch.Body = body.String
		}
		if pdfURL.Valid {
			ch.PDFURL = pdfURL.String
		}
		if headersJSON.Valid && headersJSON.String != "" && headersJSON.String != "{}" {
			_ = json.Unmarshal([]byte(headersJSON.String), &ch.Headers)
		}
		s.Chapters = append(s.Chapters, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s, nil
}

// GetNovelChapter 返回单章内容（包含正文 body 或 pdf_url）。
func (c *Catalog) GetNovelChapter(ctx context.Context, novelID string, position int) (*NovelChapter, error) {
	row := c.db.QueryRowContext(ctx, `
		SELECT id, position, title, content_type, body, pdf_url, headers
		FROM novel_chapters
		WHERE novel_id = ? AND position = ?
	`, novelID, position)
	var ch NovelChapter
	var body, pdfURL, headersJSON sql.NullString
	if err := row.Scan(&ch.ID, &ch.Position, &ch.Title, &ch.ContentType, &body, &pdfURL, &headersJSON); err != nil {
		return nil, err
	}
	if body.Valid {
		ch.Body = body.String
	}
	if pdfURL.Valid {
		ch.PDFURL = pdfURL.String
	}
	if headersJSON.Valid && headersJSON.String != "" && headersJSON.String != "{}" {
		_ = json.Unmarshal([]byte(headersJSON.String), &ch.Headers)
	}
	return &ch, nil
}

// ListNovelSetsParams 列表过滤/分页参数。
type ListNovelSetsParams struct {
	Page          int
	PageSize      int
	Sort          string // latest | oldest
	Tag           string
	ContentType   string // text | pdf | 空
	Keyword       string // 标题模糊搜索（空=不过滤）
	IncludeHidden bool   // 管理后台用，包含已隐藏的小说
}

// ListNovelSets 返回小说列表（不包含章节，前台用）。
func (c *Catalog) ListNovelSets(ctx context.Context, p ListNovelSetsParams) ([]*NovelSet, int, error) {
	if p.PageSize <= 0 {
		p.PageSize = 24
	}
	if p.Page <= 0 {
		p.Page = 1
	}

	var where []string
	var args []any
	if !p.IncludeHidden {
		where = append(where, "hidden = 0")
	}
	if p.Tag != "" {
		where = append(where, "tags LIKE ?")
		args = append(args, "%\""+p.Tag+"\"%")
	}
	if p.ContentType != "" {
		where = append(where, "content_type = ?")
		args = append(args, p.ContentType)
	}
	if p.Keyword != "" {
		where = append(where, "title LIKE ?")
		args = append(args, "%"+p.Keyword+"%")
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM novel_sets"+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	orderBy := " ORDER BY published_at DESC"
	if p.Sort == "oldest" {
		orderBy = " ORDER BY published_at ASC"
	}

	offset := (p.Page - 1) * p.PageSize
	rows, err := c.db.QueryContext(ctx, `
		SELECT id, drive_id, source_id, title, author, cover_url,
		       content_type, chapter_count, tags, description, hidden, source_kind,
		       published_at, created_at, updated_at
		FROM novel_sets`+whereSQL+orderBy+` LIMIT ? OFFSET ?
	`, append(args, p.PageSize, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []*NovelSet
	for rows.Next() {
		s, err := scanNovelSet(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, s)
	}
	return out, total, rows.Err()
}

// HideNovelSet 软删除小说（hidden=1）。
func (c *Catalog) HideNovelSet(ctx context.Context, id string) error {
	now := time.Now().UnixMilli()
	res, err := c.db.ExecContext(ctx,
		`UPDATE novel_sets SET hidden = 1, updated_at = ? WHERE id = ?`,
		now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteNovelSet 物理删除小说（同时级联删除章节）。
func (c *Catalog) DeleteNovelSet(ctx context.Context, id string) error {
	_, err := c.db.ExecContext(ctx, `DELETE FROM novel_sets WHERE id = ?`, id)
	return err
}

// ---------- crawler helpers ----------

// IsNovelDeleted 检查小说是否已被拉黑删除。
func (c *Catalog) IsNovelDeleted(ctx context.Context, id string) (bool, error) {
	var count int
	if err := c.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM novel_tombstones WHERE novel_id = ?`, id,
	).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// ListCrawlerNovelSourceIDs 返回指定爬虫已入库的所有 source_id（用于去重 seen 文件）。
func (c *Catalog) ListCrawlerNovelSourceIDs(ctx context.Context, sourceKind, driveID string) ([]string, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT source_id FROM novel_sets
		WHERE source_kind = ? AND drive_id = ?
		UNION
		SELECT source_id FROM novel_crawler_sources
		WHERE source_kind = ? AND drive_id = ?
	`, sourceKind, driveID, sourceKind, driveID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		id = strings.TrimSpace(id)
		if id != "" {
			out = append(out, id)
		}
	}
	return out, rows.Err()
}

// MarkCrawlerNovelSourceSeen 记录爬虫已处理过的 source_id。
func (c *Catalog) MarkCrawlerNovelSourceSeen(ctx context.Context, sourceKind, driveID, sourceID, status, novelID string) error {
	now := time.Now().UnixMilli()
	novelID = strings.TrimSpace(novelID)
	_, err := c.db.ExecContext(ctx, `
		INSERT INTO novel_crawler_sources (source_kind, drive_id, source_id, status, novel_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_kind, drive_id, source_id) DO UPDATE SET
			status = excluded.status,
			novel_id = excluded.novel_id,
			updated_at = excluded.updated_at
	`, sourceKind, driveID, sourceID, status, novelID, now, now)
	return err
}

// ---------- helpers ----------

func scanNovelSet(row rowScanner) (*NovelSet, error) {
	var s NovelSet
	var tagsJSON string
	var hidden int
	err := row.Scan(&s.ID, &s.DriveID, &s.SourceID, &s.Title, &s.Author, &s.CoverURL,
		&s.ContentType, &s.ChapterCount, &tagsJSON, &s.Description, &hidden, &s.SourceKind,
		&s.PublishedAt, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	s.Hidden = hidden != 0
	if tagsJSON != "" {
		_ = json.Unmarshal([]byte(tagsJSON), &s.Tags)
	}
	if s.Tags == nil {
		s.Tags = []string{}
	}
	return &s, nil
}

// ListNovelSetsByDriveID 列出指定 drive 下所有未隐藏的小说（轻量版，不含章节）。
func (c *Catalog) ListNovelSetsByDriveID(ctx context.Context, driveID string) ([]*NovelSet, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT id, drive_id, source_id, title, author, cover_url,
		       content_type, chapter_count, tags, description, hidden, source_kind,
		       published_at, created_at, updated_at
		FROM novel_sets WHERE drive_id = ? AND hidden = 0
		ORDER BY created_at ASC, id ASC
	`, driveID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*NovelSet
	for rows.Next() {
		s, err := scanNovelSet(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ValidateNovelSet 校验入库的小说(给爬虫 adapter 用)。
func ValidateNovelSet(s *NovelSet) error {
	if s == nil {
		return fmt.Errorf("nil novel set")
	}
	if s.ID == "" {
		return fmt.Errorf("id required")
	}
	if strings.TrimSpace(s.Title) == "" {
		return fmt.Errorf("title required")
	}
	if len(s.Chapters) == 0 {
		return fmt.Errorf("chapters required")
	}
	for i, ch := range s.Chapters {
		ct := ch.ContentType
		if ct == "" {
			ct = s.ContentType
		}
		if ct == "text" && strings.TrimSpace(ch.Body) == "" {
			return fmt.Errorf("chapters[%d].body required for text content", i)
		}
		if ct == "pdf" && strings.TrimSpace(ch.PDFURL) == "" {
			return fmt.Errorf("chapters[%d].pdfUrl required for pdf content", i)
		}
	}
	return nil
}

// SortChapters 按 position 排序(入库前调用,允许调用方乱序传入)。
func SortChapters(items []NovelChapter) {
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Position < items[j].Position
	})
}
