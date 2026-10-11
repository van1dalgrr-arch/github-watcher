package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// fakeDB — база-заглушка: Ping возвращает заданную ошибку, Postgres не нужен.
type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }

func TestHealth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"база доступна", nil, http.StatusOK},
		{"база недоступна", errors.New("connection refused"), http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			Register(r, Deps{DB: fakeDB{tt.err}})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
			if w.Code != tt.want {
				t.Fatalf("status = %d, want %d", w.Code, tt.want)
			}
		})
	}
}
