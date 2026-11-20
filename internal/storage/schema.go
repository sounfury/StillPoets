package storage

import _ "embed"

// SchemaSQL 内嵌了本地 SQLite 数据库初始化脚本（含 FTS5 索引表）。
//
//go:embed schema.sql
var SchemaSQL string
