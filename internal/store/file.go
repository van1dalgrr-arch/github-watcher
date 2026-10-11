package store

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
)

// fileLimit — сколько последних событий хранит демон. Лента показывает свежее,
// а уведомления идут сразу — старьё держать незачем.
const fileLimit = 2000

// File — EventStore демона: события в памяти + JSON-файл на диске, чтобы пережить перезапуск.
// Вместо SQLite: одна таблица на пару тысяч строк не стоит драйвера базы, который один весит
// мегабайты памяти при старте.
type File struct {
	path string

	mu     sync.Mutex
	events []domain.Event // от старых к новым
	nextID int64
}

// Open читает (или создаёт) файл ленты.
func Open(_ context.Context, path string) (*File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f := &File{path: path}
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(raw, &f.events); err != nil {
			return nil, err
		}
	}
	for _, e := range f.events {
		f.nextID = max(f.nextID, e.ID)
	}
	return f, nil
}

func (f *File) Close() error               { return nil }
func (f *File) Ping(context.Context) error { return nil }

func (f *File) Save(_ context.Context, events []domain.Event) ([]domain.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	known := make(map[string]bool, len(f.events))
	for _, e := range f.events {
		known[e.SourceID] = true
	}
	var fresh []domain.Event
	for _, e := range events {
		if known[e.SourceID] {
			continue
		}
		known[e.SourceID] = true
		f.nextID++
		e.ID = f.nextID
		f.events = append(f.events, e)
		fresh = append(fresh, e)
	}
	if len(fresh) == 0 {
		return nil, nil
	}
	slices.SortStableFunc(f.events, func(a, b domain.Event) int { return a.CreatedAt.Compare(b.CreatedAt) })
	if over := len(f.events) - fileLimit; over > 0 {
		f.events = slices.Clone(f.events[over:])
	}
	return fresh, f.flush()
}

func (f *File) List(_ context.Context, flt Filter) ([]domain.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if flt.Limit <= 0 || flt.Limit > 1000 {
		flt.Limit = 100
	}
	var out []domain.Event
	for i := len(f.events) - 1; i >= 0 && len(out) < flt.Limit; i-- { // новые первыми
		e := f.events[i]
		if flt.Repo != "" && e.Repo != flt.Repo || !flt.Since.IsZero() && !e.CreatedAt.After(flt.Since) ||
			len(flt.Kinds) > 0 && !slices.Contains(flt.Kinds, e.Kind) {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func (f *File) Pending(context.Context) ([]domain.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Event
	for _, e := range f.events {
		if !e.Notified {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *File) MarkNotified(_ context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.events {
		if slices.Contains(ids, f.events[i].ID) {
			f.events[i].Notified = true
		}
	}
	return f.flush()
}

func (f *File) LastSeen(context.Context) (time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.events) == 0 {
		return time.Time{}, nil
	}
	return f.events[len(f.events)-1].CreatedAt, nil
}

// flush пишет файл атомарно: сначала во временный, потом переименование — оборванная запись
// не испортит ленту.
func (f *File) flush() error {
	raw, err := json.Marshal(f.events)
	if err != nil {
		return err
	}
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.path)
}
