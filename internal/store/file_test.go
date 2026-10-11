package store

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
)

func TestFile(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "gw", "events.json")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

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

	// перезапуск демона: всё на месте, включая отметки об отправке
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	pending, _ = s.Pending(ctx)
	if len(pending) != 1 || pending[0].SourceID != "c" {
		t.Fatalf("после перезапуска pending = %+v", pending)
	}
	if fresh, _ := s.Save(ctx, []domain.Event{ev("d", 4)}); len(fresh) != 1 || fresh[0].ID != 4 {
		t.Fatalf("id после перезапуска должны продолжаться: %+v", fresh)
	}

	list, _ := s.List(ctx, Filter{Repo: "me/app", Limit: 2})
	if len(list) != 2 || list[0].SourceID != "d" {
		t.Fatalf("List: новые должны быть первыми, got %+v", list)
	}
}

func TestFileLimit(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "events.json"))
	if err != nil {
		t.Fatal(err)
	}
	var batch []domain.Event
	for i := range fileLimit + 10 {
		batch = append(batch, domain.Event{SourceID: strconv.Itoa(i), CreatedAt: time.Unix(int64(i), 0)})
	}
	if _, err := s.Save(ctx, batch); err != nil {
		t.Fatal(err)
	}
	all, _ := s.Pending(ctx)
	if len(all) != fileLimit || all[0].SourceID != "10" {
		t.Fatalf("храним последние %d: got %d, первый %q", fileLimit, len(all), all[0].SourceID)
	}
}
