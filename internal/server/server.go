// Package server — HTTP API демона. Через него TUI получает ленту событий, статус и логи демона.
package server

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
	"github.com/van1dalgrr-arch/github-watcher/internal/store"
	"github.com/van1dalgrr-arch/github-watcher/internal/watcher"
)

// Pinger — всё, что нужно handler-у от базы. *store.SQLite подходит, а в тестах — заглушка.
type Pinger interface {
	Ping(ctx context.Context) error
}

type Deps struct {
	DB      Pinger
	Events  store.EventStore
	Status  func() watcher.Status
	LogTail func() []string
}

func Register(r *gin.Engine, d Deps) {
	r.GET("/health", health(d.DB))
	api := r.Group("/api")
	api.GET("/events", listEvents(d.Events))
	api.GET("/status", func(c *gin.Context) { c.JSON(http.StatusOK, d.Status()) })
	api.GET("/logs", func(c *gin.Context) { c.JSON(http.StatusOK, d.LogTail()) })
}

// health — живо ли приложение и доступна ли база (для Docker healthcheck).
func health(db Pinger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "db unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}

// listEvents — GET /api/events?repo=owner/name&limit=100
func listEvents(events store.EventStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
		list, err := events.List(c.Request.Context(), store.Filter{Repo: c.Query("repo"), Limit: limit})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if list == nil {
			list = []domain.Event{} // [] вместо null
		}
		c.JSON(http.StatusOK, list)
	}
}
