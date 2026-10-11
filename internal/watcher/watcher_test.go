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
	runs   []domain.WorkflowRun
}

func (f *fakeGH) WorkflowRuns(context.Context, string) ([]domain.WorkflowRun, error) {
	return f.runs, nil
}
func (f *fakeGH) RunReport(_ context.Context, _ string, id int64) (string, error) {
	return "✓ test  12с", nil
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
	sent []notify.Message
	fail bool
}

func (f *fakeNotifier) Notify(_ context.Context, m notify.Message) error {
	if f.fail {
		return errors.New("сеть упала")
	}
	f.sent = append(f.sent, m)
	return nil
}

func titles(ms []notify.Message) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.Title)
	}
	return out
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
		{SourceID: "old", Kind: domain.EventCIFailed, Repo: "me/app", Actor: "bob", Title: "old", CreatedAt: t0},
	}}
	n := &fakeNotifier{}
	w := &Watcher{GH: gh, Store: db, Notifier: n, Rules: notify.ParseRules("", ""),
		Interval: time.Minute, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	// первый запуск: история попадает в ленту, но на телефон не уходит
	if _, sent, err := w.tick(ctx); err != nil || sent != 0 || len(n.sent) != 0 {
		t.Fatalf("первый запуск: sent=%d %v err=%v", sent, n.sent, err)
	}

	gh.events = append(gh.events,
		domain.Event{SourceID: "mine", Kind: domain.EventIssue, Repo: "me/app", Actor: "me", Title: "mine", CreatedAt: t0.Add(time.Minute)},
		domain.Event{SourceID: "star", Kind: domain.EventStar, Repo: "me/app", Actor: "bob", Title: "star", CreatedAt: t0.Add(2 * time.Minute)},
		domain.Event{SourceID: "pr", Kind: domain.EventPROpened, Repo: "me/app", Actor: "bob", Title: "pr", CreatedAt: t0.Add(3 * time.Minute)},
		domain.Event{SourceID: "mypush", Kind: domain.EventPush, Repo: "me/app", Actor: "me", Title: "push", SHA: "abc", CreatedAt: t0.Add(4 * time.Minute)},
		domain.Event{SourceID: "ci", Kind: domain.EventCIFailed, Repo: "me/app", Title: "ci", SHA: "abc", RunID: 7, CreatedAt: t0.Add(5 * time.Minute)},
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
	// свой issue и звезда — только в ленте; PR — отдельно; свой пуш и его CI — одним сообщением
	if saved != 0 || sent != 2 || len(n.sent) != 2 {
		t.Fatalf("saved=%d sent=%d %v, want [pr push+ci]", saved, sent, titles(n.sent))
	}
	push := n.sent[1]
	if !push.Push || push.Kind != domain.EventCIFailed || push.Title != "push" || push.Logs != "✓ test  12с" {
		t.Fatalf("склейка пуша с CI: %+v", push)
	}
	if _, sent, _ := w.tick(ctx); sent != 0 {
		t.Fatalf("повторная отправка: %d", sent)
	}
}

// пуш, у которого CI ещё идёт, придерживается и уходит вместе с результатом
func TestPushWaitsForCI(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "gw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	start := time.Now().Add(-time.Hour)
	gh := &fakeGH{events: []domain.Event{{SourceID: "start", Kind: domain.EventStar, Repo: "me/app", CreatedAt: start}}}
	n := &fakeNotifier{}
	w := &Watcher{GH: gh, Store: db, Notifier: n, Rules: notify.ParseRules("", ""),
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if _, _, err := w.tick(ctx); err != nil { // первый запуск — тихий
		t.Fatal(err)
	}

	gh.events = append(gh.events, domain.Event{SourceID: "push", Kind: domain.EventPush, Repo: "me/app", Actor: "me", Title: "push", SHA: "abc", CreatedAt: time.Now()})
	gh.runs = []domain.WorkflowRun{{HeadSHA: "abc", Status: "in_progress", Event: "push"}}
	if _, sent, _ := w.tick(ctx); sent != 0 {
		t.Fatal("пуш ушёл, не дождавшись CI")
	}

	gh.events = append(gh.events, domain.Event{SourceID: "ci", Kind: domain.EventCIPassed, Repo: "me/app", Title: "ci", SHA: "abc", RunID: 1, CreatedAt: time.Now()})
	gh.runs[0].Status = "completed"
	if _, sent, _ := w.tick(ctx); sent != 1 || !n.sent[0].Push || n.sent[0].Kind != domain.EventCIPassed {
		t.Fatalf("ждали одно сообщение «пуш + CI», got %d: %+v", sent, n.sent)
	}
}
