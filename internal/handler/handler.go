package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Pinger — всё, что нужно handler-у от базы. *pgxpool.Pool подходит, а в тестах — заглушка.
type Pinger interface {
	Ping(ctx context.Context) error
}

func Register(r *gin.Engine, db Pinger) {
	r.GET("/health", health(db))
}

// health — живо ли приложение и доступна ли база (для Docker/Kubernetes healthcheck).
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
