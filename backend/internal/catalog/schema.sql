-- 视频元数据主表
CREATE TABLE IF NOT EXISTS videos (
    id               TEXT PRIMARY KEY,          -- <drive>-<fileID> 拼接的稳定 ID
    drive_id         TEXT NOT NULL,
    file_id          TEXT NOT NULL,
    file_name        TEXT DEFAULT '',           -- 网盘侧原始文件名，用于同名同大小去重
    content_hash     TEXT DEFAULT '',
    sampled_sha256   TEXT DEFAULT '',           -- 跨网盘统一采样指纹（size + sampled bytes）
    fingerprint_status TEXT DEFAULT 'pending',  -- pending / ready / failed
    fingerprint_error  TEXT DEFAULT '',
    parent_id        TEXT,
    title            TEXT NOT NULL,
    author           TEXT,
    tags             TEXT,                      -- JSON array
    duration_seconds INTEGER DEFAULT 0,
    size_bytes       INTEGER DEFAULT 0,
    ext              TEXT,
    media_type       TEXT DEFAULT 'video',      -- video / audio
    quality          TEXT,                      -- HD / SD
    thumbnail_url    TEXT,
    thumbnail_status TEXT DEFAULT 'pending',    -- pending / ready / failed / skipped
    thumbnail_failures INTEGER DEFAULT 0,        -- consecutive transient thumbnail generation failures
    preview_file_id  TEXT,                      -- deprecated: 旧版回写网盘后的预览视频 file id
    preview_local    TEXT,                      -- 本地预览视频路径（兜底）
    preview_status   TEXT DEFAULT 'pending',    -- pending / ready / failed / disabled
    transcode_status TEXT DEFAULT '',           -- '' / pending / ready / skipped / failed（浏览器兼容性转码）
    transcode_error  TEXT DEFAULT '',
    transcoded_file_id TEXT DEFAULT '',         -- 转码产物在同一 drive 上的 fileID，播放源优先用它
    transcoded_size  INTEGER DEFAULT 0,
    views            INTEGER DEFAULT 0,
    last_viewed_at   INTEGER DEFAULT 0,
    progress_seconds REAL DEFAULT 0,             -- 观看进度（秒）。0=未看；>=duration-30 视为看完
    progress_at      INTEGER DEFAULT 0,          -- 上次更新进度的时间（unix ms）
    favorites        INTEGER DEFAULT 0,
    comments         INTEGER DEFAULT 0,
    likes            INTEGER DEFAULT 0,
    dislikes         INTEGER DEFAULT 0,
    category         TEXT,
    hidden           INTEGER DEFAULT 0,          -- 1 = hidden from public display
    tags_manual      INTEGER DEFAULT 0,          -- 1 = user explicitly curated tags
    badges           TEXT,                      -- JSON array
    description      TEXT,
    published_at     INTEGER NOT NULL,          -- unix ms
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_videos_drive ON videos(drive_id, file_id);
CREATE INDEX IF NOT EXISTS idx_videos_pub   ON videos(published_at DESC);
CREATE INDEX IF NOT EXISTS idx_videos_views ON videos(views DESC);

-- 统一标签池
CREATE TABLE IF NOT EXISTS tags (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    label      TEXT NOT NULL UNIQUE COLLATE NOCASE,
    aliases    TEXT NOT NULL DEFAULT '[]',       -- JSON array
    source     TEXT NOT NULL DEFAULT 'user',     -- system / user / collection / legacy
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS video_tags (
    video_id   TEXT NOT NULL,
    tag_id     INTEGER NOT NULL,
    source     TEXT NOT NULL DEFAULT 'auto',     -- auto / manual / legacy
    created_at INTEGER NOT NULL,
    PRIMARY KEY (video_id, tag_id)
);

CREATE INDEX IF NOT EXISTS idx_video_tags_tag ON video_tags(tag_id);
CREATE INDEX IF NOT EXISTS idx_video_tags_video ON video_tags(video_id);

-- 用户手动删除过的非系统标签。自动扫描/迁移不再重新创建同名标签；
-- 管理员手动新建同名标签时会移除这里的记录。
CREATE TABLE IF NOT EXISTS deleted_tags (
    label      TEXT PRIMARY KEY COLLATE NOCASE,
    source     TEXT NOT NULL DEFAULT '',
    deleted_at INTEGER NOT NULL
);

-- 管理员显式删除过的视频。用于防止后续扫描 / spider91 爬虫把同一个源文件
-- 再次入库；不代表原始云盘文件已被删除。
CREATE TABLE IF NOT EXISTS deleted_videos (
    id           TEXT PRIMARY KEY,
    drive_id     TEXT NOT NULL DEFAULT '',
    file_id      TEXT NOT NULL DEFAULT '',
    content_hash TEXT NOT NULL DEFAULT '',
    file_name    TEXT NOT NULL DEFAULT '',
    size_bytes   INTEGER NOT NULL DEFAULT 0,
    deleted_at   INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_deleted_videos_drive_file
    ON deleted_videos(drive_id, file_id);
CREATE INDEX IF NOT EXISTS idx_deleted_videos_drive_hash
    ON deleted_videos(drive_id, content_hash);
CREATE INDEX IF NOT EXISTS idx_deleted_videos_drive_signature
    ON deleted_videos(drive_id, file_name, size_bytes);

-- 爬虫来源记录。用于把已确认重复的 source_id 写回 seen 列表，
-- 避免后续爬虫反复下载同一个候选视频。
CREATE TABLE IF NOT EXISTS crawler_seen_sources (
    kind               TEXT NOT NULL,
    drive_id           TEXT NOT NULL,
    source_id          TEXT NOT NULL,
    status             TEXT NOT NULL DEFAULT 'imported', -- imported / duplicate
    canonical_video_id TEXT NOT NULL DEFAULT '',
    sampled_sha256     TEXT NOT NULL DEFAULT '',
    size_bytes         INTEGER NOT NULL DEFAULT 0,
    first_seen_at      INTEGER NOT NULL,
    last_seen_at       INTEGER NOT NULL,
    PRIMARY KEY (kind, drive_id, source_id)
);

CREATE INDEX IF NOT EXISTS idx_crawler_seen_sources_drive
    ON crawler_seen_sources(kind, drive_id, status);

-- 网盘账户
CREATE TABLE IF NOT EXISTS drives (
    id            TEXT PRIMARY KEY,
    kind          TEXT NOT NULL,                -- quark / p115 / p123 / pikpak / wopan / guangyapan / onedrive / googledrive / localstorage / spider91
    name          TEXT NOT NULL,
    root_id       TEXT NOT NULL DEFAULT '0',
    scan_root_id  TEXT,                          -- deprecated: 扫描起点固定等于 root_id
    credentials   TEXT,                          -- JSON: cookie / refresh_token 等
    status        TEXT DEFAULT 'disconnected',   -- disconnected / ok / error
    last_error    TEXT,
    -- 是否给该盘生成预览视频：1 开 / 0 关。封面生成不受影响。
    -- 替代了早期的全局 preview.enabled 设置（保留旧 setting 行不再读）。
    teaser_enabled INTEGER NOT NULL DEFAULT 1,
    -- 扫描时要跳过的目录 ID 集合（JSON array of string）。命中其中任意一个的目录及其
    -- 全部子目录都不会被递归扫描，也不会进入 SeenFileIDs / VisitedDirIDs 统计。
    -- 替代了早期硬编码"影视"目录的特例分支。
    skip_dir_ids  TEXT NOT NULL DEFAULT '[]',
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
);

-- 扫描任务状态
CREATE TABLE IF NOT EXISTS scans (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    drive_id    TEXT NOT NULL,
    started_at  INTEGER NOT NULL,
    finished_at INTEGER,
    scanned     INTEGER DEFAULT 0,
    added       INTEGER DEFAULT 0,
    error       TEXT
);

-- 管理后台 session（简单 token 存储）
CREATE TABLE IF NOT EXISTS admin_sessions (
    token      TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);

-- 管理后台登录永久封禁 IP
CREATE TABLE IF NOT EXISTS banned_login_ips (
    ip         TEXT PRIMARY KEY,
    reason     TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);

-- 全局 key-value 设置（preview 开关等）
CREATE TABLE IF NOT EXISTS settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);

-- 图集主表（独立于 videos，承载一组图片）
CREATE TABLE IF NOT EXISTS image_sets (
    id            TEXT PRIMARY KEY,          -- 站点侧 source_id，作为稳定 ID
    drive_id      TEXT DEFAULT '',
    source_id     TEXT NOT NULL DEFAULT '',  -- 站点侧唯一 ID（与 id 同源，但便于 join）
    title         TEXT NOT NULL,
    author        TEXT DEFAULT '',
    cover_url     TEXT DEFAULT '',           -- 首页缩略图
    image_count   INTEGER NOT NULL DEFAULT 0,
    tags          TEXT DEFAULT '[]',         -- JSON array
    description   TEXT DEFAULT '',
    hidden        INTEGER NOT NULL DEFAULT 0,
    source_kind   TEXT DEFAULT 'crawler',    -- crawler | manual | local
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    published_at  INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_image_sets_pub ON image_sets(published_at DESC);

CREATE TABLE IF NOT EXISTS image_set_images (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    set_id      TEXT NOT NULL,
    position    INTEGER NOT NULL,            -- 0-based 翻页顺序
    url         TEXT NOT NULL,
    thumb_url   TEXT DEFAULT '',
    width       INTEGER DEFAULT 0,
    height      INTEGER DEFAULT 0,
    headers     TEXT DEFAULT '{}',           -- JSON object (Referer 等)
    FOREIGN KEY (set_id) REFERENCES image_sets(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_image_set_images_set ON image_set_images(set_id, position);

-- 图集黑名单墓碑（前台拉黑 / 后台删除后不再入库）
CREATE TABLE IF NOT EXISTS image_set_tombstones (
    set_id      TEXT PRIMARY KEY,
    created_at  INTEGER NOT NULL
);

-- 爬虫已处理的图集 source_id 记录（去重 + 状态追踪）
CREATE TABLE IF NOT EXISTS image_set_crawler_sources (
    source_kind TEXT NOT NULL,
    drive_id    TEXT NOT NULL,
    source_id   TEXT NOT NULL,
    status      TEXT DEFAULT '',   -- imported | duplicate | failed
    set_id      TEXT DEFAULT '',
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    PRIMARY KEY (source_kind, drive_id, source_id)
);

-- 小说主表（独立于 videos，承载文本或 PDF 章节）
CREATE TABLE IF NOT EXISTS novel_sets (
    id            TEXT PRIMARY KEY,          -- 站点侧 source_id，作为稳定 ID
    drive_id      TEXT DEFAULT '',
    source_id     TEXT NOT NULL DEFAULT '',  -- 站点侧唯一 ID
    title         TEXT NOT NULL,
    author        TEXT DEFAULT '',
    cover_url     TEXT DEFAULT '',
    content_type  TEXT NOT NULL DEFAULT 'text',  -- text | pdf
    chapter_count INTEGER NOT NULL DEFAULT 0,
    tags          TEXT DEFAULT '[]',         -- JSON array
    description   TEXT DEFAULT '',
    hidden        INTEGER NOT NULL DEFAULT 0,
    source_kind   TEXT DEFAULT 'crawler',
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    published_at  INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_novel_sets_pub ON novel_sets(published_at DESC);
CREATE INDEX IF NOT EXISTS idx_novel_sets_ctype ON novel_sets(content_type);

CREATE TABLE IF NOT EXISTS novel_chapters (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    novel_id    TEXT NOT NULL,
    position    INTEGER NOT NULL,            -- 0-based 阅读顺序
    title       TEXT NOT NULL DEFAULT '',
    content_type TEXT NOT NULL DEFAULT 'text', -- text | pdf（默认继承 novel_sets.content_type）
    body        TEXT DEFAULT '',             -- 文本章节正文
    pdf_url     TEXT DEFAULT '',             -- PDF 章节 URL
    headers     TEXT DEFAULT '{}',           -- JSON object
    FOREIGN KEY (novel_id) REFERENCES novel_sets(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_novel_chapters_novel ON novel_chapters(novel_id, position);

-- 小说黑名单墓碑
CREATE TABLE IF NOT EXISTS novel_tombstones (
    novel_id    TEXT PRIMARY KEY,
    created_at  INTEGER NOT NULL
);

-- 爬虫已处理的小说 source_id 记录
CREATE TABLE IF NOT EXISTS novel_crawler_sources (
    source_kind TEXT NOT NULL,
    drive_id    TEXT NOT NULL,
    source_id   TEXT NOT NULL,
    status      TEXT DEFAULT '',   -- imported | duplicate | failed
    novel_id    TEXT DEFAULT '',
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    PRIMARY KEY (source_kind, drive_id, source_id)
);

-- 影视解析/搜索源（后台可配置）
-- kind: 'search' 支持按关键词搜索 | 'parse' 支持解析 URL | 'both' 两者都支持
-- 'iframe' / 'iframe-search' 前端用 <iframe> 嵌入，不做服务端解析
-- search_url / parse_url 是 URL 模板，{kw} / {url} 会被替换为 URL 编码后的值
CREATE TABLE IF NOT EXISTS parse_sources (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    kind         TEXT NOT NULL DEFAULT 'parse',  -- search | parse | both | iframe | iframe-search
    search_url   TEXT DEFAULT '',                  -- 含 {kw} 占位符
    parse_url    TEXT DEFAULT '',                  -- 含 {url} 占位符
    enabled      INTEGER NOT NULL DEFAULT 1,
    sort         INTEGER NOT NULL DEFAULT 0,
    note         TEXT DEFAULT '',
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    -- 健康检查：最近一次后台自动 ping 的结果
    last_health_status     TEXT DEFAULT '',        -- ok | fail | ''
    last_health_at         INTEGER NOT NULL DEFAULT 0,
    last_health_error      TEXT DEFAULT '',
    last_health_response_ms INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_parse_sources_sort ON parse_sources(sort, name);
-- health 列直接在 CREATE TABLE 里定义（见上方），新表自动包含。
-- 老库需要手动 ALTER 加列（SQLite 没 IF NOT EXISTS）：
--   ALTER TABLE parse_sources ADD COLUMN last_health_status TEXT DEFAULT '';
--   ALTER TABLE parse_sources ADD COLUMN last_health_at INTEGER NOT NULL DEFAULT 0;
--   ALTER TABLE parse_sources ADD COLUMN last_health_error TEXT DEFAULT '';
--   ALTER TABLE parse_sources ADD COLUMN last_health_response_ms INTEGER NOT NULL DEFAULT 0;

-- resource_sites：影视资源站（行业标准 JSON API）。
-- 标准协议：GET {api_url} 替换 {kw} 为 URL 编码后的关键词，期望返回形如
--   { "code": 1, "list": [ { "vod_id": ..., "vod_name": ..., "vod_pic": ...,
--                            "vod_remarks": ..., "vod_year": ..., "vod_play_url": ... }, ... ] }
-- 的 JSON。vod_play_url 形如 "第1集$https://xxx.m3u8#第2集$https://yyy.m3u8"。
-- play_url_mode:
--   "first"   取 vod_play_url 第一段（默认），自动识别 m3u8/mp4 直链 vs 详情页
--   "direct"  始终当作直链（m3u8/mp4）播放
--   "detail"  始终当作详情页 URL，走已有 parse 流程
CREATE TABLE IF NOT EXISTS resource_sites (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL,
    api_url        TEXT NOT NULL,                  -- 含 {kw} 占位符
    play_url_mode  TEXT NOT NULL DEFAULT 'first',  -- first | direct | detail
    enabled        INTEGER NOT NULL DEFAULT 1,
    sort           INTEGER NOT NULL DEFAULT 0,
    note           TEXT DEFAULT '',
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_resource_sites_sort ON resource_sites(sort, name);
CREATE INDEX IF NOT EXISTS idx_resource_sites_enabled ON resource_sites(enabled);

-- 全文搜索索引（FTS5 / contentless）
--
-- 没用 external content 模式（content='videos'）——modernc.org/sqlite 在 external
-- content 下 SELECT 出 UNINDEXED 列会报 "no such column: T.video_id"。所以采用
-- 完全 contentless：FTS 内部自己存 title/description/tags 副本，由下面三个 trigger
-- 维护一致性。所有写 videos 的路径（UpsertVideo / UpdateVideoMeta / DeleteVideo /
-- DeleteVideoWithTombstone / scanner 直 SQL 等）自动同步 FTS，调用方不需额外逻辑。
--
-- 字段选择：只索引标题/描述/标签这三个最常用来搜的字段。tags 是 videos.tags 列
-- 里存的 JSON array，trigger 里用 REPLACE 把 []"/, 这几个字符折叠成空格，避免
-- FTS5 把整个 JSON 串当成一个 token。
--
-- 分词器：trigram（三字符滑窗）。这是为了兼顾中文搜索——unicode61 把连续
-- CJK 字符当成一个 token，"黎明破晓的风景" 整个是一个 token，搜 "破晓" 永远命中
-- 不了。trigram 把字符串切成每 3 字符一个 token 的窗口（"黎明破晓的风景" →
-- 黎明破 / 明破晓 / 破晓的 / 晓的风 / 的风景），用户搜 "破晓" 直接命中中间
-- 那个 token。英文短语不受影响（每 3 字符一窗，搜索 "wars" 也能命中
-- "star wars"）。代价：索引体积约 3-5 倍，10w 行 videos 也在 MB 量级，可接受。
--
-- 注意 trigram 不支持 remove_diacritics（它只切 3-char 窗口，不做归一化）。
-- modernc.org/sqlite 默认带 FTS5 编译（vendor 里能看到
-- -DSQLITE_ENABLE_FTS5），无需 build tag 或换驱动。
CREATE VIRTUAL TABLE IF NOT EXISTS videos_fts USING fts5(
    video_id UNINDEXED,
    title,
    description,
    tags,
    tokenize='trigram'
);

-- 触发器全部 IF NOT EXISTS，Open() 每次都会跑 schema.sql，重跑不报错。
-- 注意：contentless FTS5 表不支持 'delete' 命令，必须用 DELETE FROM videos_fts WHERE rowid=?。
CREATE TRIGGER IF NOT EXISTS videos_fts_ai AFTER INSERT ON videos BEGIN
  INSERT INTO videos_fts(rowid, video_id, title, description, tags)
  VALUES (
    new.rowid,
    new.id,
    COALESCE(new.title, ''),
    COALESCE(new.description, ''),
    -- 把 ["foo","bar"] 这种 JSON 折叠成 "foo bar"，让 FTS5 把它当多 token 索引。
    REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(COALESCE(new.tags, ''), '[', ' '), ']', ' '), '"', ' '), ',', ' '), '\n', ' ')
  );
END;

CREATE TRIGGER IF NOT EXISTS videos_fts_ad AFTER DELETE ON videos BEGIN
  DELETE FROM videos_fts WHERE rowid = old.rowid;
END;

CREATE TRIGGER IF NOT EXISTS videos_fts_au AFTER UPDATE ON videos BEGIN
  DELETE FROM videos_fts WHERE rowid = old.rowid;
  INSERT INTO videos_fts(rowid, video_id, title, description, tags)
  VALUES (
    new.rowid,
    new.id,
    COALESCE(new.title, ''),
    COALESCE(new.description, ''),
    REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(COALESCE(new.tags, ''), '[', ' '), ']', ' '), '"', ' '), ',', ' '), '\n', ' ')
  );
END;
