package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
)

func TestSQLite(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "gw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	ev := func(id string, min int) domain.Event {
		return domain.Event{SourceID: id, Kind: domain.EventPush, Repo: "me/app", Title: id, CreatedAt: t0.Add(time.Duration(min) * time.Minute)}
	}

	fresh, err := s.Save(ctx, []domain.Event{ev("a", 1), ev("b", 2)})
	if err != nil || len(fresh) != 2 {
		t.Fatalf("первое сохранение: %d новых, err=%v", len(fresh), err)
	}
	// повтор того же события не должен создавать дубль
	fresh, err = s.Save(ctx, []domain.Event{ev("b", 2), ev("c", 3)})
	if err != nil || len(fresh) != 1 || fresh[0].SourceID != "c" {
		t.Fatalf("дедупликация: %+v, err=%v", fresh, err)
	}

	last, _ := s.LastSeen(ctx)
	if !last.Equal(t0.Add(3 * time.Minute)) {
		t.Fatalf("LastSeen = %v", last)
	}

	pending, _ := s.Pending(ctx)
	if len(pending) != 3 {
		t.Fatalf("pending = %d, want 3", len(pending))
	}
	if err := s.MarkNotified(ctx, []int64{pending[0].ID, pending[1].ID}); err != nil {
		t.Fatal(err)
	}
	pending, _ = s.Pending(ctx)
	if len(pending) != 1 || pending[0].SourceID != "c" {
		t.Fatalf("после MarkNotified pending = %+v", pending)
	}

	list, _ := s.List(ctx, Filter{Repo: "me/app", Limit: 2})
	if len(list) != 2 || list[0].SourceID != "c" {
		t.Fatalf("List: новые должны быть первыми, got %+v", list)
	}
}

func TestMigrateBody(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	// имитируем базу из первой версии: без колонок, добавленных позже
	for _, col := range added {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE events DROP COLUMN `+col.name); err != nil {
			t.Fatal(err)
		}
	}
	_ = s.Close()

	s, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("миграция: %v", err)
	}
	defer func() { _ = s.Close() }()
	if _, err := s.Save(ctx, []domain.Event{{SourceID: "p", Kind: domain.EventPush, Title: "Push", Body: "• abc fix", SHA: "abc", RunID: 7, CreatedAt: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	list, _ := s.List(ctx, Filter{})
	if len(list) != 1 || list[0].Body != "• abc fix" || list[0].SHA != "abc" || list[0].RunID != 7 {
		t.Fatalf("новые колонки не сохранились: %+v", list)
	}
}
