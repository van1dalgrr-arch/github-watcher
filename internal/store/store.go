// Package store — история событий в SQLite. Её читает TUI (лента/логи) и watcher (что уже видели).
package store

import (
	"context"
	"time"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
)

type Filter struct {
	Repo  string
	Kinds []domain.EventKind
	Since time.Time
	Limit int
}

type EventStore interface {
	// Save сохраняет новые события и возвращает только те, которых ещё не было (по SourceID).
	Save(ctx context.Context, events []domain.Event) ([]domain.Event, error)
	List(ctx context.Context, f Filter) ([]domain.Event, error)
	// Pending — сохранённые, но ещё не обработанные notifier-ом (в т.ч. упавшие при отправке).
	Pending(ctx context.Context) ([]domain.Event, error)
	MarkNotified(ctx context.Context, ids []int64) error
	// LastSeen — время последнего обработанного события, чтобы после рестарта не слать дубли.
	LastSeen(ctx context.Context) (time.Time, error)
}
