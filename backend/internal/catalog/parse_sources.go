package catalog

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ParseSource 影视解析/搜索源。
//
// kind 含义：
//   - "search"   支持 GET {search_url} 拿搜索结果（{kw} 替换关键词）
//   - "parse"    支持 GET {parse_url}  拿播放链接（{url} 替换源视频 URL）
//   - "both"     两个都支持
//   - "iframe"   不做服务端解析；前端直接把 {url} 替换后用 <iframe> 嵌入
//                 （这是大多数公开"解析接口"的真实形态：返回带 player 的 HTML）
//   - "iframe-search"  iframe 模式 + 支持搜索（{kw} 占位符）
type ParseSource struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	Kind                  string `json:"kind"` // search | parse | both | iframe | iframe-search
	SearchURL             string `json:"searchUrl"`
	ParseURL              string `json:"parseUrl"`
	Enabled               bool   `json:"enabled"`
	Sort                  int    `json:"sort"`
	Note                  string `json:"note"`
	CreatedAt             int64  `json:"createdAt"`
	UpdatedAt             int64  `json:"updatedAt"`
	// 健康检查（由后台 cron 写入）
	LastHealthStatus     string `json:"lastHealthStatus,omitempty"` // ok | fail | ""
	LastHealthAt         int64  `json:"lastHealthAt,omitempty"`
	LastHealthError      string `json:"lastHealthError,omitempty"`
	LastHealthResponseMs int64  `json:"lastHealthResponseMs,omitempty"`
}

// Validate 校验入库数据。
func (s *ParseSource) Validate() error {
	if strings.TrimSpace(s.ID) == "" {
		return errors.New("id is required")
	}
	if strings.TrimSpace(s.Name) == "" {
		return errors.New("name is required")
	}
	switch s.Kind {
	case "search", "parse", "both", "iframe", "iframe-search":
	case "":
		s.Kind = "parse"
	default:
		return errors.New("kind must be search|parse|both|iframe|iframe-search")
	}
	needsSearch := s.Kind == "search" || s.Kind == "both" || s.Kind == "iframe-search"
	needsParse := s.Kind == "parse" || s.Kind == "both" || s.Kind == "iframe" || s.Kind == "iframe-search"
	if needsSearch && !strings.Contains(s.SearchURL, "{kw}") {
		return errors.New("search URL must contain {kw} placeholder")
	}
	if needsParse && !strings.Contains(s.ParseURL, "{url}") {
		return errors.New("parse URL must contain {url} placeholder")
	}
	return nil
}

// UpsertParseSource 写入或更新一条解析源。
func (c *Catalog) UpsertParseSource(ctx context.Context, s *ParseSource) error {
	if s == nil {
		return errors.New("nil source")
	}
	if err := s.Validate(); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	if s.CreatedAt == 0 {
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	_, err := c.db.ExecContext(ctx, `
		INSERT INTO parse_sources (id, name, kind, search_url, parse_url,
		                          enabled, sort, note, created_at, updated_at,
		                          last_health_status, last_health_at,
		                          last_health_error, last_health_response_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		        ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name       = excluded.name,
			kind       = excluded.kind,
			search_url = excluded.search_url,
			parse_url  = excluded.parse_url,
			enabled    = excluded.enabled,
			sort       = excluded.sort,
			note       = excluded.note,
			updated_at = excluded.updated_at
	`,
		s.ID, s.Name, s.Kind, s.SearchURL, s.ParseURL,
		boolToInt(s.Enabled), s.Sort, s.Note, s.CreatedAt, s.UpdatedAt,
		s.LastHealthStatus, s.LastHealthAt, s.LastHealthError, s.LastHealthResponseMs,
	)
	return err
}

// SetParseSourceHealth 写入健康检查结果（被 health checker 调用）。
func (c *Catalog) SetParseSourceHealth(ctx context.Context, id string, status string, errMsg string, responseMs int64) error {
	now := time.Now().UnixMilli()
	_, err := c.db.ExecContext(ctx, `
		UPDATE parse_sources
		SET last_health_status = ?,
		    last_health_at     = ?,
		    last_health_error  = ?,
		    last_health_response_ms = ?
		WHERE id = ?
	`, status, now, errMsg, responseMs, id)
	return err
}

// GetParseSource 按 id 取一条。
func (c *Catalog) GetParseSource(ctx context.Context, id string) (*ParseSource, error) {
	row := c.db.QueryRowContext(ctx, `
		SELECT id, name, kind, search_url, parse_url, enabled, sort, note,
		       created_at, updated_at,
		       last_health_status, last_health_at, last_health_error, last_health_response_ms
		FROM parse_sources WHERE id = ?
	`, id)
	return scanParseSource(row)
}

// ListParseSources 返回全部（前台 search 用 enabled=1；管理用全部）。
func (c *Catalog) ListParseSources(ctx context.Context, enabledOnly bool) ([]*ParseSource, error) {
	q := `SELECT id, name, kind, search_url, parse_url, enabled, sort, note,
	             created_at, updated_at,
	             last_health_status, last_health_at, last_health_error, last_health_response_ms
	      FROM parse_sources`
	if enabledOnly {
		q += ` WHERE enabled = 1`
	}
	q += ` ORDER BY sort ASC, name ASC`
	rows, err := c.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ParseSource
	for rows.Next() {
		s, err := scanParseSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// DeleteParseSource 删除一条。
func (c *Catalog) DeleteParseSource(ctx context.Context, id string) error {
	_, err := c.db.ExecContext(ctx, `DELETE FROM parse_sources WHERE id = ?`, id)
	return err
}

// SupportsSearch 解析源是否支持关键词搜索。
func (s *ParseSource) SupportsSearch() bool {
	return s != nil && s.Enabled &&
		(s.Kind == "search" || s.Kind == "both" || s.Kind == "iframe-search") &&
		s.SearchURL != ""
}

// SupportsParse 解析源是否支持 URL 解析。
func (s *ParseSource) SupportsParse() bool {
	return s != nil && s.Enabled &&
		(s.Kind == "parse" || s.Kind == "both" || s.Kind == "iframe" || s.Kind == "iframe-search") &&
		s.ParseURL != ""
}

// IsIframe 解析源是否走 iframe 模式（前端用 <iframe> 嵌入）。
func (s *ParseSource) IsIframe() bool {
	return s != nil && (s.Kind == "iframe" || s.Kind == "iframe-search")
}

// ExpandSearchURL 把模板里的 {kw} 替换为 URL 编码后的关键词。
func (s *ParseSource) ExpandSearchURL(keyword string) string {
	return expandTemplate(s.SearchURL, map[string]string{"kw": queryEscape(keyword)})
}

// ExpandParseURL 把模板里的 {url} 替换为 URL 编码后的源链接。
func (s *ParseSource) ExpandParseURL(videoURL string) string {
	return expandTemplate(s.ParseURL, map[string]string{"url": queryEscape(videoURL)})
}

func scanParseSource(row rowScanner) (*ParseSource, error) {
	var s ParseSource
	var enabled int
	var healthStatus, healthError string
	var healthAt, healthMs int64
	err := row.Scan(&s.ID, &s.Name, &s.Kind, &s.SearchURL, &s.ParseURL,
		&enabled, &s.Sort, &s.Note, &s.CreatedAt, &s.UpdatedAt,
		&healthStatus, &healthAt, &healthError, &healthMs)
	if err != nil {
		return nil, err
	}
	s.Enabled = enabled != 0
	s.LastHealthStatus = healthStatus
	s.LastHealthAt = healthAt
	s.LastHealthError = healthError
	s.LastHealthResponseMs = healthMs
	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	return &s, nil
}
