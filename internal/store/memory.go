package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
)

// Memory — EventStore без базы для разового запуска по расписанию (`gw notify` в GitHub Actions).
// Между запусками живёт только маленькое состояние: время последнего события и id недавно обработанных.
type Memory struct {
	last   time.Time
	seen   map[string]time.Time // SourceID → время события
	events []domain.Event       // события этого запуска
}

// overlap — насколько раньше последнего события начинать следующий запрос: Events API отдаёт события
// с задержкой, и событие «из прошлого» может появиться уже после того, как мы ушли вперёд.
// Дубли отсекаются по seen.
const overlap = 15 * time.Minute

type memoryState struct {
	Last time.Time            `json:"last"`
	Seen map[string]time.Time `json:"seen"`
}

// NewMemory восстанавливает состояние из JSON; пустая или битая строка — как первый запуск.
func NewMemory(raw string) *Memory {
	var st memoryState
	_ = json.Unmarshal([]byte(raw), &st)
	if st.Seen == nil {
		st.Seen = map[string]time.Time{}
	}
	return &Memory{last: st.Last, seen: st.Seen}
}

// State — JSON для следующего запуска. Хранит только id в окне перекрытия, чтобы не расти бесконечно.
func (m *Memory) State() string {
	keep := map[string]time.Time{}
	for id, t := range m.seen {
		if t.After(m.last.Add(-2 * overlap)) {
			keep[id] = t
		}
	}
	raw, _ := json.Marshal(memoryState{Last: m.last, Seen: keep})
	return string(raw)
}

func (m *Memory) Save(_ context.Context, events []domain.Event) ([]domain.Event, error) {
	var fresh []domain.Event
	for _, e := range events {
		if _, ok := m.seen[e.SourceID]; ok || m.has(e.SourceID) {
			continue
		}
		e.ID = int64(len(m.events) + 1)
		m.events = append(m.events, e)
		fresh = append(fresh, e)
	}
	return fresh, nil
}

func (m *Memory) has(id string) bool {
	for _, e := range m.events {
		if e.SourceID == id {
			return true
		}
	}
	return false
}

func (m *Memory) List(context.Context, Filter) ([]domain.Event, error) { return m.events, nil }

func (m *Memory) Pending(context.Context) ([]domain.Event, error) {
	var out []domain.Event
	for _, e := range m.events {
		if !e.Notified {
			out = append(out, e)
		}
	}
	return out, nil
}

// MarkNotified запоминает событие как обработанное. Неотправленное в seen не попадает —
// следующий запуск увидит его снова (оно в окне перекрытия) и повторит.
func (m *Memory) MarkNotified(_ context.Context, ids []int64) error {
	for _, id := range ids {
		e := &m.events[id-1]
		e.Notified = true
		m.seen[e.SourceID] = e.CreatedAt
		if e.CreatedAt.After(m.last) {
			m.last = e.CreatedAt
		}
	}
	return nil
}

func (m *Memory) LastSeen(context.Context) (time.Time, error) {
	if m.last.IsZero() {
		return time.Time{}, nil // первый запуск: watcher возьмёт историю без уведомлений
	}
	return m.last.Add(-overlap), nil
}
