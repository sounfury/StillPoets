-- StillPoets 本地 SQLite Schema (v1)
-- 严格区分常驻元数据表与按需下载的正文表（poems）

PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS books (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    author TEXT NOT NULL DEFAULT '',
    dynasty_or_genre TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT '',
    is_starred INTEGER NOT NULL DEFAULT 0,
    sort_order INTEGER NOT NULL DEFAULT 0,
    cover_hash TEXT NOT NULL DEFAULT '',
    content_hash TEXT NOT NULL DEFAULT '',
    content_size INTEGER NOT NULL DEFAULT 0,
    raw_filename TEXT NOT NULL DEFAULT '',
    raw_size INTEGER NOT NULL DEFAULT 0,
    poem_count INTEGER NOT NULL DEFAULT 0,
    local_state TEXT NOT NULL DEFAULT 'remote', -- remote | downloading | downloaded | stale
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    deleted_at INTEGER NOT NULL DEFAULT 0,
    device_id TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_books_active ON books (deleted_at, sort_order, updated_at DESC);

-- 按需下载后落库的正文表；清理单本缓存时仅 DELETE FROM poems WHERE book_id = ?
CREATE TABLE IF NOT EXISTS poems (
    id TEXT PRIMARY KEY,
    book_id TEXT NOT NULL,
    order_index INTEGER NOT NULL,
    title TEXT NOT NULL,
    subtitle TEXT NOT NULL DEFAULT '',
    author TEXT NOT NULL DEFAULT '',
    content_json TEXT NOT NULL, -- Stanza[] JSON
    plain_text TEXT NOT NULL,
    FOREIGN KEY (book_id) REFERENCES books (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_poems_book_order ON poems (book_id, order_index);

CREATE TABLE IF NOT EXISTS favorite_poems (
    id TEXT PRIMARY KEY,
    book_id TEXT NOT NULL,
    poem_id TEXT NOT NULL,
    poem_title TEXT NOT NULL DEFAULT '',
    author TEXT NOT NULL DEFAULT '',
    excerpt TEXT NOT NULL DEFAULT '',
    highlight_lines TEXT NOT NULL DEFAULT '',
    is_custom_quote INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    deleted_at INTEGER NOT NULL DEFAULT 0,
    device_id TEXT NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_favorite_poems_pair ON favorite_poems (book_id, poem_id);
CREATE INDEX IF NOT EXISTS idx_favorite_poems_active ON favorite_poems (deleted_at, updated_at DESC);

-- 入口 B：独立句子 / 摘录库（随元数据常驻本地）
CREATE TABLE IF NOT EXISTS quotes (
    id TEXT PRIMARY KEY,
    content TEXT NOT NULL,
    author TEXT NOT NULL DEFAULT '',
    source_book TEXT NOT NULL DEFAULT '',
    import_source TEXT NOT NULL DEFAULT '',
    tags_json TEXT NOT NULL DEFAULT '[]',
    is_favorite INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    deleted_at INTEGER NOT NULL DEFAULT 0,
    device_id TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_quotes_source ON quotes (deleted_at, source_book, updated_at DESC);

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

CREATE TABLE IF NOT EXISTS reading_progress (
    book_id TEXT PRIMARY KEY,
    last_poem_id TEXT NOT NULL DEFAULT '',
    scroll_ratio REAL NOT NULL DEFAULT 0,
    read_poem_count INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'unread', -- unread | reading | finished
    finished_at INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL,
    device_id TEXT NOT NULL DEFAULT ''
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

CREATE INDEX IF NOT EXISTS idx_reading_events_date ON reading_events (read_date, read_at DESC);
CREATE INDEX IF NOT EXISTS idx_reading_events_author ON reading_events (author);

-- 本地同步状态（记录各远端分片上次同步的 ETag / Hash / 时间）
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
