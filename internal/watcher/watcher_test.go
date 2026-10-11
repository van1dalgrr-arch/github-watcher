package watcher

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
	"github.com/van1dalgrr-arch/github-watcher/internal/github"
	"github.com/van1dalgrr-arch/github-watcher/internal/notify"
	"github.com/van1dalgrr-arch/github-watcher/internal/store"
)

type fakeGH struct {
	github.Client
	events []domain.Event
}

func (f *fakeGH) Me(context.Context) (string, error) { return "me", nil }
func (f *fakeGH) EventsSince(_ context.Context, since time.Time) ([]domain.Event, error) {
	var out []domain.Event
	for _, e := range f.events {
		if e.CreatedAt.After(since) {
			out = append(out, e)
		}
	}
	return out, nil
}

type fakeNotifier struct {
	sent []string
	fail bool
}

func (f *fakeNotifier) Notify(_ context.Context, e domain.Event) error {
	if f.fail {
		return errors.New("сеть упала")
	}
	f.sent = append(f.sent, e.SourceID)
	return nil
}

func TestTick(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "gw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	t0 := time.Now().Add(-time.Hour)
	gh := &fakeGH{events: []domain.Event{
		{SourceID: "old", Kind: domain.EventCIFailed, Repo: "me/app", Actor: "bob", CreatedAt: t0},
	}}
	n := &fakeNotifier{}
	w := &Watcher{GH: gh, Store: db, Notifier: n, Rules: notify.ParseRules("", ""),
		Interval: time.Minute, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	// первый запуск: история попадает в ленту, но на телефон не уходит
	if _, sent, err := w.tick(ctx); err != nil || sent != 0 || len(n.sent) != 0 {
		t.Fatalf("первый запуск: sent=%d %v err=%v", sent, n.sent, err)
	}

	gh.events = append(gh.events,
		domain.Event{SourceID: "mine", Kind: domain.EventIssue, Repo: "me/app", Actor: "me", CreatedAt: t0.Add(time.Minute)},
		domain.Event{SourceID: "star", Kind: domain.EventStar, Repo: "me/app", Actor: "bob", CreatedAt: t0.Add(2 * time.Minute)},
		domain.Event{SourceID: "ci", Kind: domain.EventCIFailed, Repo: "me/app", Actor: "bob", CreatedAt: t0.Add(3 * time.Minute)},
		domain.Event{SourceID: "mypush", Kind: domain.EventPush, Repo: "me/app", Actor: "me", CreatedAt: t0.Add(4 * time.Minute)},
	)

	// Telegram недоступен — ничего не теряем
	n.fail = true
	if _, _, err := w.tick(ctx); err != nil {
		t.Fatal(err)
	}
	n.fail = false
	saved, sent, err := w.tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// свой issue и звезда (не в правилах по умолчанию) — только в ленте; CI упал и свой пуш — на телефон
	if saved != 0 || sent != 2 || len(n.sent) != 2 || n.sent[0] != "ci" || n.sent[1] != "mypush" {
		t.Fatalf("saved=%d sent=%d %v, want [ci mypush]", saved, sent, n.sent)
	}
	if _, sent, _ := w.tick(ctx); sent != 0 {
		t.Fatalf("повторная отправка: %d", sent)
	}
}
