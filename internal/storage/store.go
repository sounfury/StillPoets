package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/egoist/mygo/plugins/sqlite"

	"stillpoets/internal/domain"
)

var ErrNotFound = errors.New("storage: record not found")

type Store struct {
	db *sqlite.DB
	// Serializes reads that determine subsequent writes or span several tables.
	mu sync.Mutex
}

type SyncMeta struct {
	InstallID      string
	LamportCounter uint64
	LocalRev       uint64
	Dirty          bool
	DirtySeq       uint64
	LastSyncAt     int64
}

func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sqlite.Open(ctx, path, sqlite.OpenOptions{})
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.initialize(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("storage: initialize: %w", err)
	}
	return s, nil
}

func (s *Store) initialize(ctx context.Context) error {
	if _, err := s.db.Execute(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return err
	}
	// The embedded schema uses line comments and has no semicolons in literals.
	var ddl strings.Builder
	for line := range strings.SplitSeq(SchemaSQL, "\n") {
		line, _, _ = strings.Cut(line, "--")
		ddl.WriteString(line)
		ddl.WriteByte('\n')
	}
	var statements []sqlite.Statement
	for sql := range strings.SplitSeq(ddl.String(), ";") {
		sql = strings.TrimSpace(sql)
		if sql != "" && sql != "PRAGMA foreign_keys = ON" {
			statements = append(statements, stmt(sql))
		}
	}
	statements = append(statements, stmt(`INSERT INTO sync_state (key, value, updated_at) VALUES ('install_id', ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at WHERE sync_state.value = ''`,
		domain.NewInstallID(), domain.NowMillis()))
	return s.transact(ctx, statements)
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) LoadSettings(ctx context.Context) (domain.Settings, error) {
	settings := domain.Settings{
		ThemePreset: "artbook",
		FontSize:    18,
		LineHeight:  1.85,
		WidgetMode:  domain.WidgetModeGlobalRandom,
		WidgetTheme: domain.WidgetThemeConfig{
			Preset: "artbook", BgColor: "#F4F4EF", TextColor: "#1A1A1A", AccentColor: "#BC5B46",
		},
	}
	row, err := s.firstRow(ctx, `SELECT value FROM sync_state WHERE key = 'local_settings'`)
	if errors.Is(err, ErrNotFound) {
		return settings, nil
	}
	if err != nil {
		return domain.Settings{}, err
	}
	if err := json.Unmarshal([]byte(row.text(0)), &settings); err != nil {
		return domain.Settings{}, fmt.Errorf("storage: decode settings: %w", err)
	}
	return settings, nil
}

func (s *Store) SaveSettings(ctx context.Context, settings domain.Settings) error {
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	return s.transact(ctx, []sqlite.Statement{setSyncState("local_settings", string(data), domain.NowMillis())})
}

func (s *Store) syncMeta(ctx context.Context) (SyncMeta, error) {
	var meta SyncMeta
	rows, err := s.db.Query(ctx, `SELECT key, value FROM sync_state WHERE key IN
		('install_id', 'lamport_counter', 'local_rev', 'dirty', 'dirty_seq', 'last_sync_at')`)
	if err != nil {
		return meta, err
	}
	for _, values := range rows.Values {
		row := row(values)
		key, value := row.text(0), row.text(1)
		switch key {
		case "install_id":
			meta.InstallID = value
		case "dirty":
			meta.Dirty = value == "1"
		case "last_sync_at":
			meta.LastSyncAt, err = strconv.ParseInt(value, 10, 64)
		case "lamport_counter":
			meta.LamportCounter, err = strconv.ParseUint(value, 10, 64)
		case "local_rev":
			meta.LocalRev, err = strconv.ParseUint(value, 10, 64)
		case "dirty_seq":
			meta.DirtySeq, err = strconv.ParseUint(value, 10, 64)
		}
		if err != nil {
			return SyncMeta{}, fmt.Errorf("storage: decode sync state %s: %w", key, err)
		}
	}
	return meta, nil
}

func (s *Store) mutate(ctx context.Context, change func(domain.Version, int64) ([]sqlite.Statement, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, err := s.syncMeta(ctx)
	if err != nil {
		return err
	}
	if meta.LamportCounter >= math.MaxInt64 || meta.DirtySeq == math.MaxUint64 {
		return errors.New("storage: local version exhausted")
	}
	version := domain.Version{Counter: meta.LamportCounter + 1, InstallID: meta.InstallID}
	now := domain.NowMillis()
	statements, err := change(version, now)
	if err != nil || len(statements) == 0 {
		return err
	}
	return s.transact(ctx, append(statements,
		setSyncState("lamport_counter", strconv.FormatUint(version.Counter, 10), now),
		setSyncState("dirty_seq", strconv.FormatUint(meta.DirtySeq+1, 10), now),
		setSyncState("dirty", "1", now),
	))
}

func (s *Store) transact(ctx context.Context, statements []sqlite.Statement) error {
	_, err := s.db.Transaction(ctx, statements)
	return err
}

func stmt(sql string, args ...any) sqlite.Statement {
	return sqlite.Statement{SQL: sql, Args: args}
}

func setSyncState(key, value string, now int64) sqlite.Statement {
	return stmt(`INSERT INTO sync_state (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, key, value, now)
}

type row []any

func (r row) text(i int) string   { return r[i].(string) }
func (r row) integer(i int) int64 { return r[i].(int64) }
func (r row) flag(i int) bool     { return r.integer(i) != 0 }
func (r row) version(i int) domain.Version {
	return domain.Version{Counter: uint64(r.integer(i)), InstallID: r.text(i + 1)}
}

func (s *Store) firstRow(ctx context.Context, sql string, args ...any) (row, error) {
	rows, err := s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) == 0 {
		return nil, ErrNotFound
	}
	return rows.Values[0], nil
}

func selectRows[T any](ctx context.Context, s *Store, decode func(row) (T, error), sql string, args ...any) ([]T, error) {
	rows, err := s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	items := make([]T, 0, len(rows.Values))
	for _, values := range rows.Values {
		item, err := decode(values)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// Separate Han characters for phrase searches within unspaced Chinese text.
func ftsText(text string) string {
	var indexed strings.Builder
	for _, ch := range text {
		if unicode.Is(unicode.Han, ch) {
			indexed.WriteByte(' ')
			indexed.WriteRune(ch)
			indexed.WriteByte(' ')
		} else {
			indexed.WriteRune(ch)
		}
	}
	return indexed.String()
}

func ftsPhrase(text string) string {
	text = strings.ReplaceAll(ftsText(text), "\x00", " ")
	return `"` + strings.ReplaceAll(text, `"`, `""`) + `"`
}
