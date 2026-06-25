package catalog

import (
	"context"
	"fmt"
	"strings"
)

const ftsBackfillMarkerKey = "videos_fts_backfilled"

// backfillVideosFTS 一次性把 videos 表里已有数据写入 videos_fts，避免新建
// FTS5 索引后老视频搜不到。完成后在 settings 表里写 marker，下次启动短路。
//
// 注意：用 INSERT OR IGNORE INTO videos_fts SELECT ... 而不是直接触发器：触发器
// 是基于 videos 的 INSERT/UPDATE/DELETE，不会反向灌存量数据。OR IGNORE 是为了
// 兼容 "schema.sql 已经把触发器建好 + videos 已有数据" 的边缘场景——触发器已
// 经写过 FTS，再灌一次会撞 rowid UNIQUE 约束。
func (c *Catalog) backfillVideosFTS(ctx context.Context) error {
	if marker, err := c.GetSetting(ctx, ftsBackfillMarkerKey, ""); err != nil {
		return fmt.Errorf("catalog: read fts backfill marker: %w", err)
	} else if marker == "1" {
		return nil
	}
	// videos.tags 是 JSON 列（["a","b"]），跟 schema 里 trigger 用同一种折叠方式。
	// 这里不靠 trigger，因为我们要灌存量；直接拼 SQL。
	if _, err := c.db.ExecContext(ctx, `
INSERT OR IGNORE INTO videos_fts (rowid, video_id, title, description, tags)
SELECT rowid,
       id,
       COALESCE(title, ''),
       COALESCE(description, ''),
       REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(COALESCE(tags, ''), '[', ' '), ']', ' '), '"', ' '), ',', ' '), '\n', ' ')
  FROM videos
`); err != nil {
		return fmt.Errorf("catalog: backfill videos_fts: %w", err)
	}
	if err := c.SetSetting(ctx, ftsBackfillMarkerKey, "1"); err != nil {
		return fmt.Errorf("catalog: write fts backfill marker: %w", err)
	}
	return nil
}

// SearchOptions 控制 FTS5 视频搜索的行为。
//
//   - Query：必填，已由 SearchVideos 内部做 sanitize。
//   - MediaType：可选过滤，"video" / "audio"。空 = 不限；其它值按不限处理。
//   - Limit/Offset：分页参数。<0 一律按 0 / 20 处理。
type SearchOptions struct {
	MediaType string
	Limit     int
	Offset    int
}

// fts5SpecialChars 是 FTS5 查询里需要剔除的元字符。剔除后用户输入的 "foo:bar*"
// 不会被 FTS5 当成 column-filter + prefix 表达式触发解析错误。
const fts5SpecialChars = `"*():^-`

// sanitizeFTSQuery 把用户原始查询整理成 FTS5 MATCH 接受的字符串。
//
// 分词器是 trigram（每 3 字符一个窗口），所以：
//  1. 去掉 FTS5 元字符，避免解析报错或被当成 column-filter
//  2. 合并空白
//  3. 给每个剩余 token 加 `*` 后缀做 prefix 匹配（trigram 要求 prefix 至少 3 字符）
//  4. 空查询返回 ""，调用方应短路不再走 SQL
//
// trigram 限制：少于 3 字符的查询不会命中任何内容（每个 trigram 都是 3 字符）。
// 对中文用户来说，输入"黎明"会被 sanitize 成"黎明*"——长度 < 3，但 SQLite trigram
// 对 2-char prefix 会回退到精确匹配，所以仍然有效；4 字符以上是最理想情况。
func sanitizeFTSQuery(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// 先把全角空白归一
	raw = strings.ReplaceAll(raw, "　", " ")
	// 剔除 FTS5 元字符
	for _, r := range fts5SpecialChars {
		raw = strings.ReplaceAll(raw, string(r), " ")
	}
	// 折叠多余空白
	raw = strings.Join(strings.Fields(raw), " ")
	if raw == "" {
		return ""
	}
	// 给每个 token 加 * 后缀
	tokens := strings.Fields(raw)
	for i, t := range tokens {
		tokens[i] = t + "*"
	}
	return strings.Join(tokens, " ")
}

// SearchVideos 通过 FTS5 索引搜索视频。
//
// 返回：命中的视频列表（按 bm25 排序，相关性高的在前）+ 总命中数。
//
//   - query：用户原始关键词，会被 sanitizeFTSQuery 处理。空字符串返回 nil。
//   - opts.MediaType：可选过滤媒体类型。
//   - opts.Limit/Offset：分页参数，越界自动夹紧。
//
// 隐藏视频（hidden=1）会过滤掉，墓碑视频（deleted_videos）本来就不在 videos
// 表里，不需要额外排除。
//
// 实现细节：videos_fts（contentless 模式）和 videos 都有 title/description/tags
// 列名，直接 JOIN 会让 SELECT 里的列引用产生歧义。所以分两步：先在 FTS 上做
// MATCH + bm25 排序拿到有序的 rowid 列表（含分页），再按 rowid 列表回 videos
// 表拿完整字段。回查后用 ORDER BY CASE 保留 FTS 给的相关性顺序。
func (c *Catalog) SearchVideos(ctx context.Context, query string, opts SearchOptions) ([]*Video, int, error) {
	cleaned := sanitizeFTSQuery(query)
	if cleaned == "" {
		return nil, 0, nil
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}

	// total：先数一遍，应用 hidden=0 + media_type 过滤，跟用户最终看到的 items 集合一致。
	// 注意：必须在 count 时也加上 media_type 过滤，否则 total 会大于 items（filter 后）。
	extraWhere := ""
	extraArgs := []any{}
	if mt := normalizeMediaTypeFilter(opts.MediaType); mt != "" {
		extraWhere = " AND COALESCE(videos.media_type, 'video') = ?"
		extraArgs = append(extraArgs, mt)
	}

	var total int
	if err := c.db.QueryRowContext(ctx,
		`SELECT COUNT(*)
		   FROM videos_fts
		   JOIN videos ON videos.rowid = videos_fts.rowid
		  WHERE videos_fts MATCH ?
		    AND COALESCE(videos.hidden, 0) = 0`+extraWhere,
		append([]any{cleaned}, extraArgs...)...,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("catalog: count fts: %w", err)
	}
	if total == 0 {
		return nil, 0, nil
	}

	// Step 1: 从 FTS 拿有序 rowid + score（这一步只读 videos_fts 内部 rowid），
	// JOIN videos 主要是过滤 hidden / media_type，不读 videos 的 title 列。
	hitsSQL := `
SELECT videos.rowid, bm25(videos_fts) AS score
  FROM videos_fts
  JOIN videos ON videos.rowid = videos_fts.rowid
 WHERE videos_fts MATCH ?
   AND COALESCE(videos.hidden, 0) = 0` + extraWhere + `
 ORDER BY score ASC, videos.published_at DESC
 LIMIT ? OFFSET ?`
	hitsArgs := append(append([]any{cleaned}, extraArgs...), limit, offset)

	hitsRows, err := c.db.QueryContext(ctx, hitsSQL, hitsArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("catalog: query fts: %w", err)
	}
	type ftsHit struct {
		rowid int64
		score float64
	}
	var hits []ftsHit
	for hitsRows.Next() {
		var h ftsHit
		if err := hitsRows.Scan(&h.rowid, &h.score); err != nil {
			hitsRows.Close()
			return nil, 0, err
		}
		hits = append(hits, h)
	}
	if err := hitsRows.Err(); err != nil {
		hitsRows.Close()
		return nil, 0, err
	}
	hitsRows.Close()

	if len(hits) == 0 {
		return nil, total, nil
	}

	// Step 2: 按 rowid 列表回查 videos 拿完整字段，SELECT 不再 JOIN FTS 表，
	// 避免 title/description/tags 列名歧义。
	rowids := make([]int64, 0, len(hits))
	for _, h := range hits {
		rowids = append(rowids, h.rowid)
	}
	placeholders := strings.Repeat("?,", len(rowids))
	placeholders = placeholders[:len(placeholders)-1]
	videosSQL := "SELECT " + allVideoCols + " FROM videos WHERE rowid IN (" + placeholders + ")"
	videosArgs := make([]any, 0, len(rowids))
	for _, r := range rowids {
		videosArgs = append(videosArgs, r)
	}
	vRows, err := c.db.QueryContext(ctx, videosSQL, videosArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("catalog: load videos by rowids: %w", err)
	}
	defer vRows.Close()

	byRowID := make(map[int64]*Video, len(rowids))
	for vRows.Next() {
		v, err := scanVideo(vRows)
		if err != nil {
			return nil, 0, err
		}
		byRowID[v.RowID] = v
	}
	if err := vRows.Err(); err != nil {
		return nil, 0, err
	}

	// 按 FTS 原始顺序输出
	out := make([]*Video, 0, len(hits))
	for _, h := range hits {
		if v, ok := byRowID[h.rowid]; ok && v != nil {
			out = append(out, v)
		}
	}
	return out, total, nil
}

// normalizeMediaTypeFilter 把 opts.MediaType 标准化成 mediatype 包认可的取值。
// 非 video / audio 一律按不限处理（返回 ""）。
func normalizeMediaTypeFilter(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	switch raw {
	case "video", "audio":
		return raw
	default:
		return ""
	}
}
