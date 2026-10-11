// Package watcher — сердце демона: раз в Interval спрашивает GitHub, что нового,
// сохраняет в store и отправляет подходящее в notifier.
package watcher

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github-watcher/internal/github"
	"github-watcher/internal/notify"
	"github-watcher/internal/store"
)

type Watcher struct {
	GH       github.Client
	Store    store.EventStore
	Notifier notify.Notifier
	Rules    notify.Rules
	Interval time.Duration
	Log      *slog.Logger

	mu     sync.Mutex
	status Status
}

// Status — для `gw` и TUI: жив ли демон и когда последний раз ходил в GitHub.
type Status struct {
	Started   time.Time `json:"started"`
	LastTick  time.Time `json:"last_tick"`
	LastError string    `json:"last_error,omitempty"`
	Interval  string    `json:"interval"`
	Saved     int       `json:"saved"` // событий сохранено с момента запуска
	Sent      int       `json:"sent"`  // уведомлений отправлено
}

func (w *Watcher) Status() Status {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.status
}

// Run крутится до отмены ctx.
func (w *Watcher) Run(ctx context.Context) error {
	w.mu.Lock()
	w.status = Status{Started: time.Now(), Interval: w.Interval.String()}
	w.mu.Unlock()
	w.Log.Info("watcher started", "interval", w.Interval)

	t := time.NewTicker(w.Interval)
	defer t.Stop()
	for {
		saved, sent, err := w.tick(ctx)
		w.mu.Lock()
		w.status.LastTick = time.Now()
		w.status.Saved += saved
		w.status.Sent += sent
		w.status.LastError = ""
		if err != nil {
			w.status.LastError = err.Error()
		}
		w.mu.Unlock()
		switch {
		case err != nil && ctx.Err() == nil:
			w.Log.Error("tick", "err", err)
		case saved > 0 || sent > 0:
			w.Log.Info("tick", "new", saved, "notified", sent)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

// Once — одна проверка без цикла: для запуска по расписанию (GitHub Actions cron).
func (w *Watcher) Once(ctx context.Context) (saved, sent int, err error) { return w.tick(ctx) }

// tick: GitHub → новые события → store → телефон.
func (w *Watcher) tick(ctx context.Context) (saved, sent int, err error) {
	since, err := w.Store.LastSeen(ctx)
	if err != nil {
		return 0, 0, err
	}
	firstRun := since.IsZero()
	if firstRun {
		since = time.Now().Add(-7 * 24 * time.Hour) // для ленты подтянем неделю истории
	}
	events, err := w.GH.EventsSince(ctx, since)
	if err != nil {
		return 0, 0, err
	}
	fresh, err := w.Store.Save(ctx, events)
	if err != nil {
		return 0, 0, err
	}
	me, _ := w.GH.Me(ctx)

	pending, err := w.Store.Pending(ctx)
	if err != nil {
		return len(fresh), 0, err
	}
	var done []int64
	for _, e := range pending {
		// История первого запуска и собственные действия на телефон не шлём — только в ленту.
		if !firstRun && e.Actor != me && w.Rules.Match(e) {
			if err := w.Notifier.Notify(ctx, e); err != nil {
				w.Log.Warn("notify", "event", e.SourceID, "err", err)
				continue // не помечаем — повторим на следующем тике
			}
			sent++
		}
		done = append(done, e.ID)
	}
	return len(fresh), sent, w.Store.MarkNotified(ctx, done)
}
