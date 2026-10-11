// Package watcher — сердце демона: раз в Interval спрашивает GitHub, что нового,
// сохраняет в store и отправляет подходящее в notifier.
package watcher

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
	"github.com/van1dalgrr-arch/github-watcher/internal/github"
	"github.com/van1dalgrr-arch/github-watcher/internal/notify"
	"github.com/van1dalgrr-arch/github-watcher/internal/store"
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
	var send []domain.Event
	for _, e := range pending {
		// История первого запуска и собственные действия на телефон не шлём — только в ленту.
		// Исключение — пуши: статистика своего пуша в боте и есть смысл уведомления.
		own := e.Actor == me && e.Kind != domain.EventPush
		if firstRun || own || !w.Rules.Match(e) {
			done = append(done, e.ID)
			continue
		}
		send = append(send, e)
	}

	for _, batch := range byCommit(send) {
		if w.waitForCI(ctx, batch) {
			continue // пуш придержали до конца его CI — уйдут одним сообщением
		}
		logs := map[int64]string{}
		for _, e := range batch {
			if e.RunID != 0 {
				if r, err := w.GH.RunReport(ctx, e.Repo, e.RunID); err == nil {
					logs[e.RunID] = r
				}
			}
		}
		if err := w.Notifier.Notify(ctx, notify.Compose(batch, logs)); err != nil {
			w.Log.Warn("notify", "event", batch[0].SourceID, "err", err)
			continue // не помечаем — повторим на следующем тике
		}
		sent++
		for _, e := range batch {
			done = append(done, e.ID)
		}
	}
	return len(fresh), sent, w.Store.MarkNotified(ctx, done)
}

// byCommit группирует пуш и CI одного коммита; остальные события — по одному. Порядок сохраняется.
func byCommit(events []domain.Event) [][]domain.Event {
	var out [][]domain.Event
	at := map[string]int{}
	for _, e := range events {
		groupable := e.SHA != "" && (e.Kind == domain.EventPush || e.Kind == domain.EventCIPassed || e.Kind == domain.EventCIFailed)
		if !groupable {
			out = append(out, []domain.Event{e})
			continue
		}
		key := e.Repo + "@" + e.SHA
		if i, ok := at[key]; ok {
			out[i] = append(out[i], e)
			continue
		}
		at[key] = len(out)
		out = append(out, []domain.Event{e})
	}
	return out
}

// holdPush — сколько ждать CI, чтобы прислать пуш вместе с результатом. Меньше окна перекрытия
// store.Memory (15 мин): иначе придержанный пуш выпал бы из следующего запроса.
const holdPush = 10 * time.Minute

// waitForCI — true, если в пачке пуш без результата CI, а CI по этому коммиту ещё идёт.
func (w *Watcher) waitForCI(ctx context.Context, batch []domain.Event) bool {
	var push *domain.Event
	for i := range batch {
		switch batch[i].Kind {
		case domain.EventCIPassed, domain.EventCIFailed:
			return false
		case domain.EventPush:
			push = &batch[i]
		}
	}
	if push == nil || time.Since(push.CreatedAt) > holdPush {
		return false
	}
	runs, err := w.GH.WorkflowRuns(ctx, push.Repo)
	if err != nil {
		return false
	}
	for _, r := range runs {
		if r.HeadSHA == push.SHA && r.Status != "completed" && (r.Event == "push" || r.Event == "pull_request") {
			return true
		}
	}
	return false
}
