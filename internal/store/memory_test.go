package store

import (
	"context"
	"testing"
	"time"

	"github-watcher/internal/domain"
)

func TestMemoryAcrossRuns(t *testing.T) {
	ctx := context.Background()
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	ev := func(id string, min int) domain.Event {
		return domain.Event{SourceID: id, CreatedAt: t0.Add(time.Duration(min) * time.Minute)}
	}

	// запуск 1: первый, LastSeen нулевой
	m := NewMemory("")
	if last, _ := m.LastSeen(ctx); !last.IsZero() {
		t.Fatal("первый запуск должен начинаться с нуля")
	}
	fresh, _ := m.Save(ctx, []domain.Event{ev("a", 0), ev("b", 10)})
	_ = m.MarkNotified(ctx, []int64{fresh[0].ID, fresh[1].ID})

	// запуск 2: состояние из JSON; запрос с перекрытием, старые отсекаются
	m = NewMemory(m.State())
	if last, _ := m.LastSeen(ctx); !last.Equal(t0.Add(10*time.Minute - overlap)) {
		t.Fatalf("LastSeen = %v", last)
	}
	fresh, _ = m.Save(ctx, []domain.Event{ev("b", 10), ev("late", 5), ev("c", 20)})
	if len(fresh) != 2 || fresh[0].SourceID != "late" || fresh[1].SourceID != "c" {
		t.Fatalf("опоздавшее событие должно пройти, дубль — нет: %+v", fresh)
	}
	_ = m.MarkNotified(ctx, []int64{fresh[0].ID}) // "c" не отправилось

	// запуск 3: неотправленное приходит снова
	m = NewMemory(m.State())
	fresh, _ = m.Save(ctx, []domain.Event{ev("late", 5), ev("c", 20)})
	if len(fresh) != 1 || fresh[0].SourceID != "c" {
		t.Fatalf("неотправленное должно повториться: %+v", fresh)
	}

	if NewMemory("{битый json").seen == nil {
		t.Fatal("битое состояние должно давать пустое, а не nil")
	}
}
