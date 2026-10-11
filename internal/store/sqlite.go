package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // чистый Go, без cgo — бинарник собирается с CGO_ENABLED=0

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
)

//go:embed schema.sql
var schema string

type SQLite struct{ db *sql.DB }

// Open открывает (и при необходимости создаёт) базу по пути path.
func Open(ctx context.Context, path string) (*SQLite, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite пишет в один поток — так не бывает "database is locked"
	if _, err := db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &SQLite{db: db}, nil
}

func (s *SQLite) Close() error                   { return s.db.Close() }
func (s *SQLite) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *SQLite) Save(ctx context.Context, events []domain.Event) ([]domain.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO events (source_id, kind, repo, actor, title, url, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (source_id) DO NOTHING`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stmt.Close() }()

	var fresh []domain.Event
	for _, e := range events {
		res, err := stmt.ExecContext(ctx, e.SourceID, e.Kind, e.Repo, e.Actor, e.Title, e.URL, e.CreatedAt.UTC().UnixMilli())
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue // уже было
		}
		e.ID, _ = res.LastInsertId()
		fresh = append(fresh, e)
	}
	return fresh, tx.Commit()
}

func (s *SQLite) List(ctx context.Context, f Filter) ([]domain.Event, error) {
	q := `SELECT id, source_id, kind, repo, actor, title, url, created_at, notified FROM events WHERE 1=1`
	var args []any
	if f.Repo != "" {
		q += ` AND repo = ?`
		args = append(args, f.Repo)
	}
	if !f.Since.IsZero() {
		q += ` AND created_at > ?`
		args = append(args, f.Since.UTC().UnixMilli())
	}
	if len(f.Kinds) > 0 {
		q += ` AND kind IN (?` + strings.Repeat(`,?`, len(f.Kinds)-1) + `)`
		for _, k := range f.Kinds {
			args = append(args, k)
		}
	}
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 100
	}
	q += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, f.Limit)
	return s.query(ctx, q, args...)
}

func (s *SQLite) Pending(ctx context.Context) ([]domain.Event, error) {
	return s.query(ctx, `SELECT id, source_id, kind, repo, actor, title, url, created_at, notified
		FROM events WHERE NOT notified ORDER BY created_at, id LIMIT 200`)
}

func (s *SQLite) MarkNotified(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE events SET notified = 1 WHERE id IN (?`+strings.Repeat(`,?`, len(ids)-1)+`)`, args...)
	return err
}

func (s *SQLite) LastSeen(ctx context.Context) (time.Time, error) {
	var ms sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(created_at) FROM events`).Scan(&ms); err != nil {
		return time.Time{}, err
	}
	if !ms.Valid {
		return time.Time{}, nil
	}
	return time.UnixMilli(ms.Int64), nil
}

func (s *SQLite) query(ctx context.Context, q string, args ...any) ([]domain.Event, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Event
	for rows.Next() {
		var e domain.Event
		var ms int64
		if err := rows.Scan(&e.ID, &e.SourceID, &e.Kind, &e.Repo, &e.Actor, &e.Title, &e.URL, &ms, &e.Notified); err != nil {
			return nil, err
		}
		e.CreatedAt = time.UnixMilli(ms)
		out = append(out, e)
	}
	return out, rows.Err()
}
