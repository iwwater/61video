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

// ImageSet 一个图集元数据 + 图片列表。
type ImageSet struct {
	ID          string         `json:"id"`
	DriveID     string         `json:"driveId"`
	SourceID    string         `json:"sourceId"`
	Title       string         `json:"title"`
	Author      string         `json:"author"`
	CoverURL    string         `json:"coverUrl"`
	ImageCount  int            `json:"imageCount"`
	Tags        []string       `json:"tags"`
	Description string         `json:"description"`
	Hidden      bool           `json:"hidden"`
	SourceKind  string         `json:"sourceKind"`
	PublishedAt int64          `json:"publishedAt"`
	CreatedAt   int64          `json:"createdAt"`
	UpdatedAt   int64          `json:"updatedAt"`
	Images      []ImageSetItem `json:"images,omitempty"` // 仅 GetImageSet 时填充
}

// ImageSetItem 图集中的单张图片。
type ImageSetItem struct {
	Position int             `json:"position"`
	URL      string          `json:"url"`
	ThumbURL string          `json:"thumbUrl,omitempty"`
	Width    int             `json:"width,omitempty"`
	Height   int             `json:"height,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
}

// UpsertImageSet 整体替换图集（id 必填；存在则更新元数据 + 清空并重建图片列表）。
func (c *Catalog) UpsertImageSet(ctx context.Context, s *ImageSet) error {
	if s.ID == "" {
		return errors.New("image set id is required")
	}
	if strings.TrimSpace(s.Title) == "" {
		return errors.New("image set title is required")
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
		INSERT INTO image_sets (id, drive_id, source_id, title, author, cover_url,
		                       image_count, tags, description, hidden, source_kind,
		                       created_at, updated_at, published_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			drive_id     = excluded.drive_id,
			source_id    = excluded.source_id,
			title        = excluded.title,
			author       = excluded.author,
			cover_url    = excluded.cover_url,
			image_count  = excluded.image_count,
			tags         = excluded.tags,
			description  = excluded.description,
			hidden       = excluded.hidden,
			source_kind  = excluded.source_kind,
			updated_at   = excluded.updated_at,
			published_at = excluded.published_at
	`, s.ID, s.DriveID, s.SourceID, s.Title, s.Author, s.CoverURL,
		len(s.Images), string(tagsJSON), s.Description, boolToInt(s.Hidden), s.SourceKind,
		s.CreatedAt, s.UpdatedAt, s.PublishedAt,
	)
	if err != nil {
		return err
	}

	// 整体替换图片列表
	if _, err := tx.ExecContext(ctx, `DELETE FROM image_set_images WHERE set_id = ?`, s.ID); err != nil {
		return err
	}
	for i, img := range s.Images {
		headersJSON := "{}"
		if len(img.Headers) > 0 {
			b, _ := json.Marshal(img.Headers)
			headersJSON = string(b)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO image_set_images (set_id, position, url, thumb_url, width, height, headers)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, s.ID, i, img.URL, img.ThumbURL, img.Width, img.Height, headersJSON); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetImageSet 返回图集元数据 + 图片列表。
func (c *Catalog) GetImageSet(ctx context.Context, id string) (*ImageSet, error) {
	row := c.db.QueryRowContext(ctx, `
		SELECT id, drive_id, source_id, title, author, cover_url,
		       image_count, tags, description, hidden, source_kind,
		       published_at, created_at, updated_at
		FROM image_sets WHERE id = ?
	`, id)
	s, err := scanImageSet(row)
	if err != nil {
		return nil, err
	}
	rows, err := c.db.QueryContext(ctx, `
		SELECT position, url, thumb_url, width, height, headers
		FROM image_set_images
		WHERE set_id = ?
		ORDER BY position ASC
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item ImageSetItem
		var headersJSON string
		if err := rows.Scan(&item.Position, &item.URL, &item.ThumbURL, &item.Width, &item.Height, &headersJSON); err != nil {
			return nil, err
		}
		if headersJSON != "" && headersJSON != "{}" {
			_ = json.Unmarshal([]byte(headersJSON), &item.Headers)
		}
		s.Images = append(s.Images, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s, nil
}

// ListImageSetsParams 列表过滤/分页参数。
type ListImageSetsParams struct {
	Page          int
	PageSize      int
	Sort          string // latest | oldest
	Tag           string
	Keyword       string // 标题模糊搜索（空=不过滤）
	IncludeHidden bool   // 管理后台用，包含已隐藏的图集
}

// ListImageSets 返回图集列表（不包含图片列表，前台用）。
func (c *Catalog) ListImageSets(ctx context.Context, p ListImageSetsParams) ([]*ImageSet, int, error) {
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
	if p.Keyword != "" {
		where = append(where, "title LIKE ?")
		args = append(args, "%"+p.Keyword+"%")
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM image_sets"+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	orderBy := " ORDER BY published_at DESC"
	if p.Sort == "oldest" {
		orderBy = " ORDER BY published_at ASC"
	}

	offset := (p.Page - 1) * p.PageSize
	rows, err := c.db.QueryContext(ctx, `
		SELECT id, drive_id, source_id, title, author, cover_url,
		       image_count, tags, description, hidden, source_kind,
		       published_at, created_at, updated_at
		FROM image_sets`+whereSQL+orderBy+` LIMIT ? OFFSET ?
	`, append(args, p.PageSize, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []*ImageSet
	for rows.Next() {
		s, err := scanImageSet(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, s)
	}
	return out, total, rows.Err()
}

// HideImageSet 软删除图集（hidden=1）。
func (c *Catalog) HideImageSet(ctx context.Context, id string) error {
	now := time.Now().UnixMilli()
	res, err := c.db.ExecContext(ctx,
		`UPDATE image_sets SET hidden = 1, updated_at = ? WHERE id = ?`,
		now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteImageSet 物理删除图集（同时级联删除图片）。
func (c *Catalog) DeleteImageSet(ctx context.Context, id string) error {
	_, err := c.db.ExecContext(ctx, `DELETE FROM image_sets WHERE id = ?`, id)
	return err
}

// ---------- crawler helpers ----------

// IsImageSetDeleted 检查图集是否已被拉黑删除。
func (c *Catalog) IsImageSetDeleted(ctx context.Context, id string) (bool, error) {
	var count int
	if err := c.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM image_set_tombstones WHERE set_id = ?`, id,
	).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// ListCrawlerImageSetSourceIDs 返回指定爬虫已入库的所有 source_id（用于去重 seen 文件）。
func (c *Catalog) ListCrawlerImageSetSourceIDs(ctx context.Context, sourceKind, driveID string) ([]string, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT source_id FROM image_sets
		WHERE source_kind = ? AND drive_id = ?
		UNION
		SELECT source_id FROM image_set_crawler_sources
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

// MarkCrawlerImageSetSourceSeen 记录爬虫已处理过的 source_id。
func (c *Catalog) MarkCrawlerImageSetSourceSeen(ctx context.Context, sourceKind, driveID, sourceID, status, setID string) error {
	now := time.Now().UnixMilli()
	setID = strings.TrimSpace(setID)
	_, err := c.db.ExecContext(ctx, `
		INSERT INTO image_set_crawler_sources (source_kind, drive_id, source_id, status, set_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_kind, drive_id, source_id) DO UPDATE SET
			status = excluded.status,
			set_id  = excluded.set_id,
			updated_at = excluded.updated_at
	`, sourceKind, driveID, sourceID, status, setID, now, now)
	return err
}

// ---------- helpers ----------

func scanImageSet(row rowScanner) (*ImageSet, error) {
	var s ImageSet
	var tagsJSON string
	var hidden int
	err := row.Scan(&s.ID, &s.DriveID, &s.SourceID, &s.Title, &s.Author, &s.CoverURL,
		&s.ImageCount, &tagsJSON, &s.Description, &hidden, &s.SourceKind,
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

// ValidateImageSet 校验入库的图集(给爬虫 adapter 用)。
func ValidateImageSet(s *ImageSet) error {
	if s == nil {
		return fmt.Errorf("nil image set")
	}
	if s.ID == "" {
		return fmt.Errorf("id required")
	}
	if strings.TrimSpace(s.Title) == "" {
		return fmt.Errorf("title required")
	}
	if len(s.Images) == 0 {
		return fmt.Errorf("images required")
	}
	for i, img := range s.Images {
		if img.URL == "" {
			return fmt.Errorf("images[%d].url required", i)
		}
	}
	return nil
}

// SortImages 按 position 排序(入库前调用,允许调用方乱序传入)。
func SortImages(items []ImageSetItem) {
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Position < items[j].Position
	})
}