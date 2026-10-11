package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
	"github.com/van1dalgrr-arch/github-watcher/internal/store"
	"github.com/van1dalgrr-arch/github-watcher/internal/watcher"
)

// fakeDB — хранилище-заглушка: Ping возвращает заданную ошибку.
type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }

func TestHealth(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"хранилище доступно", nil, http.StatusOK},
		{"хранилище недоступно", errors.New("disk full"), http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			New(Deps{DB: fakeDB{tt.err}}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
			if w.Code != tt.want {
				t.Fatalf("status = %d, want %d", w.Code, tt.want)
			}
		})
	}
}

func TestAPI(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "events.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = db.Save(ctx, []domain.Event{
		{SourceID: "1", Repo: "me/app", Title: "a", CreatedAt: time.Now()},
		{SourceID: "2", Repo: "me/web", Title: "b", CreatedAt: time.Now()},
	})
	h := New(Deps{DB: db, Events: db,
		Status:  func() watcher.Status { return watcher.Status{Saved: 2} },
		LogTail: func() []string { return []string{"level=INFO msg=ok"} }})

	get := func(path string, v any) int {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if v != nil {
			_ = json.NewDecoder(w.Body).Decode(v)
		}
		return w.Code
	}
	var events []domain.Event
	if code := get("/api/events?repo=me/web", &events); code != 200 || len(events) != 1 || events[0].Title != "b" {
		t.Fatalf("events: %d %+v", code, events)
	}
	var st watcher.Status
	if get("/api/status", &st); st.Saved != 2 {
		t.Fatalf("status: %+v", st)
	}
	if code := get("/api/nope", nil); code != http.StatusNotFound {
		t.Fatalf("неизвестный адрес: %d", code)
	}
	if w := httptest.NewRecorder(); true {
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/events", nil))
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("POST должен быть запрещён: %d", w.Code)
		}
	}
}
