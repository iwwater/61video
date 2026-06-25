package catalog

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ResourceSite 影视资源站（行业标准 JSON API）。
//
// 标准协议：GET {api_url} 把 {kw} 替换为 URL 编码后的关键词，期望返回：
//
//	{ "code": 1, "list": [
//	    { "vod_id": ..., "vod_name": ..., "vod_pic": ...,
//	      "vod_remarks": ..., "vod_year": ..., "vod_play_url": ... },
//	    ...
//	]}
//
// vod_play_url 形如 "第1集$https://xxx.m3u8#第2集$https://yyy.m3u8"，每段用 $ 分
// 名称和 URL，段之间用 # 分隔。
type ResourceSite struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	APIURL       string `json:"apiUrl"`
	PlayURLMode  string `json:"playUrlMode"` // first | direct | detail
	Enabled      bool   `json:"enabled"`
	Sort         int    `json:"sort"`
	Note         string `json:"note"`
	CreatedAt    int64  `json:"createdAt"`
	UpdatedAt    int64  `json:"updatedAt"`
}

// Validate 校验入库数据。
func (s *ResourceSite) Validate() error {
	if strings.TrimSpace(s.ID) == "" {
		return errors.New("id is required")
	}
	if strings.TrimSpace(s.Name) == "" {
		return errors.New("name is required")
	}
	if !strings.Contains(s.APIURL, "{kw}") {
		return errors.New("api url must contain {kw} placeholder")
	}
	switch s.PlayURLMode {
	case "", "first":
		s.PlayURLMode = "first"
	case "direct", "detail":
	default:
		return errors.New("playUrlMode must be first|direct|detail")
	}
	return nil
}

// UpsertResourceSite 写入或更新一条资源站。
func (c *Catalog) UpsertResourceSite(ctx context.Context, s *ResourceSite) error {
	if s == nil {
		return errors.New("nil resource site")
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
		INSERT INTO resource_sites (id, name, api_url, play_url_mode,
		                            enabled, sort, note, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name          = excluded.name,
			api_url       = excluded.api_url,
			play_url_mode = excluded.play_url_mode,
			enabled       = excluded.enabled,
			sort          = excluded.sort,
			note          = excluded.note,
			updated_at    = excluded.updated_at
	`,
		s.ID, s.Name, s.APIURL, s.PlayURLMode,
		boolToInt(s.Enabled), s.Sort, s.Note, s.CreatedAt, s.UpdatedAt,
	)
	return err
}

// GetResourceSite 按 id 取一条。
func (c *Catalog) GetResourceSite(ctx context.Context, id string) (*ResourceSite, error) {
	row := c.db.QueryRowContext(ctx, `
		SELECT id, name, api_url, play_url_mode, enabled, sort, note, created_at, updated_at
		FROM resource_sites WHERE id = ?
	`, id)
	return scanResourceSite(row)
}

// ListResourceSites 返回全部或仅 enabled 的资源站。
func (c *Catalog) ListResourceSites(ctx context.Context, enabledOnly bool) ([]*ResourceSite, error) {
	q := `SELECT id, name, api_url, play_url_mode, enabled, sort, note, created_at, updated_at
	      FROM resource_sites`
	if enabledOnly {
		q += ` WHERE enabled = 1`
	}
	q += ` ORDER BY sort ASC, name ASC`
	rows, err := c.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ResourceSite
	for rows.Next() {
		s, err := scanResourceSite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// DeleteResourceSite 删除一条。
func (c *Catalog) DeleteResourceSite(ctx context.Context, id string) error {
	_, err := c.db.ExecContext(ctx, `DELETE FROM resource_sites WHERE id = ?`, id)
	return err
}

// 默认资源站预设：行业里常见的 资源站 JSON API 端点。
//
// 注意：这些站点的可用性会随时间变化（站点可能改版/下线/被墙）。
// 全部以 **enabled** 状态插入（极速资源已验证可用；其它作为可选项）。
// 用户可在后台禁用/删除/修改 URL。
//
// 格式：GET {api_url}，{kw} 替换为 URL 编码后的关键词；返回：
//
//	{ "code": 1, "list": [
//	    { "vod_id": ..., "vod_name": ..., "vod_pic": ...,
//	      "vod_remarks": ..., "vod_year": ..., "vod_play_url": "第1集$url#第2集$url" } ]}
//
// 列表里 vod_play_url 通常为空（行业惯例，详情接口才给），所以点击后
// 走 /api/anime/resource/detail 拉详情，再根据是否 m3u8 决定直播还是 parse。
var defaultResourceSites = []ResourceSite{
	{
		ID:          "jszy",
		Name:        "极速资源（已验证可用）",
		APIURL:      "https://jszyapi.com/api.php/provide/vod/?ac=list&wd={kw}",
		PlayURLMode: "first",
		Enabled:     true,
		Sort:        10,
		Note:        "已测试：搜索「间谍过家家」返回 7 条。API 域名/路径可能更新，如不可用请修改 URL。",
	},
	{
		ID:          "cj-lzi",
		Name:        "LZI 资源（已验证可用）",
		APIURL:      "https://cj.lziapi.com/api.php/provide/vod/?ac=list&wd={kw}",
		PlayURLMode: "first",
		Enabled:     true,
		Sort:        20,
		Note:        "已测试：搜索「间谍过家家」返回 5 条。",
	},
	{
		ID:          "okzyw",
		Name:        "OK 资源（已失效占位）",
		APIURL:      "https://okzyw.com/api.php/provide/vod/?ac=list&wd={kw}",
		PlayURLMode: "first",
		Enabled:     false,
		Sort:        30,
		Note:        "返回 HTML 不是 JSON，已失效。如域名复活可启用。",
	},
	{
		ID:          "zdzy",
		Name:        "最大资源（已失效占位）",
		APIURL:      "https://www.zdziyuan.com/api.php/provide/vod/?ac=list&wd={kw}",
		PlayURLMode: "first",
		Enabled:     false,
		Sort:        40,
		Note:        "返回空响应，已失效。",
	},
}

// SeedDefaultResourceSites 把默认资源站预设合并到 DB：
//   - 表为空时全部插入
//   - 表不为空但缺少某个 ID 时补齐
//   - 已存在的预设 ID 不会覆盖（保留用户修改）
func (c *Catalog) SeedDefaultResourceSites(ctx context.Context) error {
	// 已存在的 ID 集合
	existing := map[string]bool{}
	rows, err := c.db.QueryContext(ctx, `SELECT id FROM resource_sites`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		existing[id] = true
	}
	rows.Close()

	// 缺哪些补哪些；不覆盖已存在的
	for i := range defaultResourceSites {
		s := defaultResourceSites[i]
		if existing[s.ID] {
			continue
		}
		if err := c.UpsertResourceSite(ctx, &s); err != nil {
			return err
		}
	}
	return nil
}

// ExpandAPIURL 把模板里的 {kw} 替换为 URL 编码后的关键词。
func (s *ResourceSite) ExpandAPIURL(keyword string) string {
	return expandTemplate(s.APIURL, map[string]string{"kw": queryEscape(keyword)})
}

func scanResourceSite(row rowScanner) (*ResourceSite, error) {
	var s ResourceSite
	var enabled int
	err := row.Scan(&s.ID, &s.Name, &s.APIURL, &s.PlayURLMode,
		&enabled, &s.Sort, &s.Note, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	s.Enabled = enabled != 0
	if s.PlayURLMode == "" {
		s.PlayURLMode = "first"
	}
	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	return &s, nil
}
