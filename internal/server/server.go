// Package server — HTTP API демона. Через него TUI получает ленту событий, статус и логи демона.
// Стандартный net/http: для четырёх адресов фреймворк не нужен, а Gin тянул бы за собой
// HTTP/3, bson и валидаторы — лишние мегабайты памяти у каждого запуска gw.
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
	"github.com/van1dalgrr-arch/github-watcher/internal/store"
	"github.com/van1dalgrr-arch/github-watcher/internal/watcher"
)

// Pinger — всё, что нужно health-проверке от хранилища.
type Pinger interface {
	Ping(ctx context.Context) error
}

type Deps struct {
	DB      Pinger
	Events  store.EventStore
	Status  func() watcher.Status
	LogTail func() []string
}

func New(d Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health(d.DB))
	mux.HandleFunc("GET /api/events", listEvents(d.Events))
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, d.Status()) })
	mux.HandleFunc("GET /api/logs", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, d.LogTail()) })
	return mux
}

// health — живо ли приложение и доступно ли хранилище (для Docker healthcheck).
func health(db Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "db unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// listEvents — GET /api/events?repo=owner/name&limit=100
func listEvents(events store.EventStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		list, err := events.List(r.Context(), store.Filter{Repo: r.URL.Query().Get("repo"), Limit: limit})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if list == nil {
			list = []domain.Event{} // [] вместо null
		}
		writeJSON(w, http.StatusOK, list)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
