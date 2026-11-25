-- StillPoets 本地 SQLite Schema (v2 - 适配 Per-Device 代次快照与 Lamport 字段级版本)
-- 严格区分常驻元数据表与按需下载的正文表（poems）

PRAGMA foreign_keys = ON;

-- 诗集自定义分组（支持新建空组、重命名、排序、解散；删除永久生效）
CREATE TABLE IF NOT EXISTS book_groups (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    name_ver_c INTEGER NOT NULL DEFAULT 0,
    name_ver_i TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    sort_ver_c INTEGER NOT NULL DEFAULT 0,
    sort_ver_i TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    deleted INTEGER NOT NULL DEFAULT 0,
    deleted_at INTEGER NOT NULL DEFAULT 0,
    deleted_ver_c INTEGER NOT NULL DEFAULT 0,
    deleted_ver_i TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_book_groups_active ON book_groups (deleted, sort_order, id);

-- 入口 A：诗集元数据（可独立编辑字段与内容描述各自带 Lamport 版本；local_content_hash 仅本机使用）
CREATE TABLE IF NOT EXISTS books (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    author TEXT NOT NULL DEFAULT '',
    dynasty_or_genre TEXT NOT NULL DEFAULT '',
    cover_hash TEXT NOT NULL DEFAULT '',
    content_hash TEXT NOT NULL DEFAULT '',
    content_size INTEGER NOT NULL DEFAULT 0,
    raw_filename TEXT NOT NULL DEFAULT '',
    raw_size INTEGER NOT NULL DEFAULT 0,
    poem_count INTEGER NOT NULL DEFAULT 0,
    content_ver_c INTEGER NOT NULL DEFAULT 0,
    content_ver_i TEXT NOT NULL DEFAULT '',

    group_id TEXT NOT NULL DEFAULT '', -- 空串表示默认「全部诗集」组
    group_ver_c INTEGER NOT NULL DEFAULT 0,
    group_ver_i TEXT NOT NULL DEFAULT '',

    is_starred INTEGER NOT NULL DEFAULT 0,
    starred_ver_c INTEGER NOT NULL DEFAULT 0,
    starred_ver_i TEXT NOT NULL DEFAULT '',

    sort_order INTEGER NOT NULL DEFAULT 0,
    sort_ver_c INTEGER NOT NULL DEFAULT 0,
    sort_ver_i TEXT NOT NULL DEFAULT '',

    created_at INTEGER NOT NULL,
    deleted INTEGER NOT NULL DEFAULT 0,
    deleted_at INTEGER NOT NULL DEFAULT 0,
    deleted_ver_c INTEGER NOT NULL DEFAULT 0,
    deleted_ver_i TEXT NOT NULL DEFAULT '',

    -- 仅本机使用：记录当前已落库 poems 对应的 content_hash，由此推导 remote/downloaded/stale
    local_content_hash TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_books_active ON books (deleted, sort_order, id);
CREATE INDEX IF NOT EXISTS idx_books_group ON books (deleted, group_id, sort_order);

-- 按需下载后落库的正文表；清理单本缓存时仅 DELETE FROM poems WHERE book_id = ? 并清空 books.local_content_hash
CREATE TABLE IF NOT EXISTS poems (
    id TEXT PRIMARY KEY,
    book_id TEXT NOT NULL,
    order_index INTEGER NOT NULL,
    kind TEXT NOT NULL DEFAULT 'poem', -- poem | prose（译序/导读/后记/年表等附文不计入完读与小组件）
    section TEXT NOT NULL DEFAULT '',  -- 分辑/卷名
    title TEXT NOT NULL,
    subtitle TEXT NOT NULL DEFAULT '',
    author TEXT NOT NULL DEFAULT '',
    epigraph TEXT NOT NULL DEFAULT '', -- 诗前题记
    content_json TEXT NOT NULL,        -- Stanza[] JSON
    colophon TEXT NOT NULL DEFAULT '', -- 诗末落款（日期/地点）
    notes_json TEXT NOT NULL DEFAULT '[]',  -- Note[] JSON（内联脚注/尾注/诗末注）
    images_json TEXT NOT NULL DEFAULT '[]', -- ImageRef[] JSON（V1.0 预留，V1.1 渲染）
    plain_text TEXT NOT NULL,
    FOREIGN KEY (book_id) REFERENCES books (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_poems_book_order ON poems (book_id, order_index);

-- 诗篇级精选池 ❤️（合并与落库 upsert 主键均为 (book_id, poem_id)）
-- Active 与 Excerpt 各自带独立 Lamport 版本，编辑节选不会复活已取消的收藏
CREATE TABLE IF NOT EXISTS favorite_poems (
    id TEXT PRIMARY KEY,
    book_id TEXT NOT NULL,
    poem_id TEXT NOT NULL,
    poem_title TEXT NOT NULL DEFAULT '',
    author TEXT NOT NULL DEFAULT '',
    active INTEGER NOT NULL DEFAULT 1,
    active_ver_c INTEGER NOT NULL DEFAULT 0,
    active_ver_i TEXT NOT NULL DEFAULT '',
    deleted_at INTEGER NOT NULL DEFAULT 0,
    excerpt TEXT NOT NULL DEFAULT '',
    highlight_lines TEXT NOT NULL DEFAULT '',
    is_custom_quote INTEGER NOT NULL DEFAULT 0,
    excerpt_ver_c INTEGER NOT NULL DEFAULT 0,
    excerpt_ver_i TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_favorite_poems_pair ON favorite_poems (book_id, poem_id);
CREATE INDEX IF NOT EXISTS idx_favorite_poems_active ON favorite_poems (active, book_id, poem_id);

-- 散页库（诗歌节选 + 独立散句，随元数据常驻本地；删除对同一 ID 永久生效）
-- 诗歌节选记录 book_id/poem_id/poem_title/highlight_lines，若关联诗集已删则运行时降级为独立散句
CREATE TABLE IF NOT EXISTS quotes (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL DEFAULT 'standalone', -- excerpt | standalone
    content TEXT NOT NULL,
    author TEXT NOT NULL DEFAULT '',
    source_book TEXT NOT NULL DEFAULT '',
    book_id TEXT NOT NULL DEFAULT '',
    poem_id TEXT NOT NULL DEFAULT '',
    poem_title TEXT NOT NULL DEFAULT '',
    highlight_lines TEXT NOT NULL DEFAULT '',
    import_source TEXT NOT NULL DEFAULT '',
    tags_json TEXT NOT NULL DEFAULT '[]',
    body_ver_c INTEGER NOT NULL DEFAULT 0,
    body_ver_i TEXT NOT NULL DEFAULT '',
    is_favorite INTEGER NOT NULL DEFAULT 0,
    favorite_ver_c INTEGER NOT NULL DEFAULT 0,
    favorite_ver_i TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    deleted INTEGER NOT NULL DEFAULT 0,
    deleted_at INTEGER NOT NULL DEFAULT 0,
    deleted_ver_c INTEGER NOT NULL DEFAULT 0,
    deleted_ver_i TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_quotes_source ON quotes (deleted, source_book, id);
CREATE INDEX IF NOT EXISTS idx_quotes_author ON quotes (deleted, author, id);
CREATE INDEX IF NOT EXISTS idx_quotes_origin ON quotes (deleted, book_id, poem_id);

-- V1.1 预留：单诗点评与诗集长书评
CREATE TABLE IF NOT EXISTS annotations (
    id TEXT PRIMARY KEY,
    target_type TEXT NOT NULL, -- poem | book
    book_id TEXT NOT NULL,
    poem_id TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    deleted_at INTEGER NOT NULL DEFAULT 0,
    device_id TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_annotations_target ON annotations (deleted_at, target_type, book_id, poem_id);

-- 阅读进度与完读标记（续读位置 / 完读状态独立版本化；已读 poem_id 集合按并集去重同步；滚动比例仅本机）
CREATE TABLE IF NOT EXISTS reading_progress (
    book_id TEXT PRIMARY KEY,
    last_poem_id TEXT NOT NULL DEFAULT '',
    position_ver_c INTEGER NOT NULL DEFAULT 0,
    position_ver_i TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'unread', -- unread | reading | finished
    finished_at INTEGER NOT NULL DEFAULT 0,
    status_ver_c INTEGER NOT NULL DEFAULT 0,
    status_ver_i TEXT NOT NULL DEFAULT '',
    read_poem_ids_json TEXT NOT NULL DEFAULT '[]', -- 跨端并集同步的已读 poemId 有序数组
    local_scroll_ratio REAL NOT NULL DEFAULT 0     -- 仅本机使用，不同步
);

-- V1.1 预留：按“某诗集的某首诗”记录有效读诗事件
CREATE TABLE IF NOT EXISTS reading_events (
    id TEXT PRIMARY KEY,
    book_id TEXT NOT NULL,
    poem_id TEXT NOT NULL,
    poem_title TEXT NOT NULL DEFAULT '',
    author TEXT NOT NULL DEFAULT '',
    read_date TEXT NOT NULL, -- YYYY-MM-DD
    read_hour INTEGER NOT NULL, -- 0-23
    read_at INTEGER NOT NULL,
    is_reread INTEGER NOT NULL DEFAULT 0,
    device_id TEXT NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_reading_events_day_poem ON reading_events (book_id, poem_id, read_date);
CREATE INDEX IF NOT EXISTS idx_reading_events_date ON reading_events (read_date, read_at DESC);
CREATE INDEX IF NOT EXISTS idx_reading_events_author ON reading_events (author);

-- 本地持久化待上传队列（确保导书解析包与原始 EPUB 上传校验完成前，崩溃/重启不丢失原文件备份）
CREATE TABLE IF NOT EXISTS pending_uploads (
    book_id TEXT PRIMARY KEY,
    raw_filename TEXT NOT NULL,
    local_raw_path TEXT NOT NULL,
    local_spbook_path TEXT NOT NULL,
    local_cover_path TEXT NOT NULL DEFAULT '',
    content_hash TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending', -- pending | assets_verified | failed
    last_error TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

-- 本地同步状态（保存 install_id、lamport_counter、local_rev、dirty_seq、上次同步时间等）
CREATE TABLE IF NOT EXISTS sync_state (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);

-- FTS5 全文检索（已下载诗集正文 + 独立句子库）
CREATE VIRTUAL TABLE IF NOT EXISTS poems_fts USING fts5(
    poem_id UNINDEXED,
    book_id UNINDEXED,
    title,
    author,
    plain_text,
    tokenize = 'unicode61'
);

CREATE VIRTUAL TABLE IF NOT EXISTS quotes_fts USING fts5(
    quote_id UNINDEXED,
    content,
    author,
    source_book,
    tokenize = 'unicode61'
);
