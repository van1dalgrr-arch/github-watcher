package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
)

func TestRules(t *testing.T) {
	ci := domain.Event{Kind: domain.EventCIFailed, Repo: "me/app"}
	star := domain.Event{Kind: domain.EventStar, Repo: "me/app"}

	def := ParseRules("", "")
	if !def.Match(ci) || !def.Match(domain.Event{Kind: domain.EventPush}) || !def.Match(domain.Event{Kind: domain.EventCIPassed}) || def.Match(star) {
		t.Fatal("по умолчанию: CI (любой) и пуши — да, звёзды — нет")
	}
	if !ParseRules("all", "").Match(star) {
		t.Fatal("all должен пропускать всё")
	}
	only := ParseRules("ci_failed", "me/api")
	if only.Match(ci) {
		t.Fatal("фильтр по репозиторию не сработал")
	}
}

func TestTelegram(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/botTOKEN/sendMessage":
			_ = json.NewDecoder(r.Body).Decode(&got)
			_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
		case "/botTOKEN/getUpdates":
			_, _ = w.Write([]byte(`{"ok":true,"result":[{"message":{"chat":{"id":42,"username":"me","first_name":"Я"}}}]}`))
		default:
			_, _ = w.Write([]byte(`{"ok":false,"description":"Unauthorized"}`))
		}
	}))
	defer srv.Close()

	tg := NewTelegram("TOKEN", "42")
	tg.API = srv.URL
	err := tg.Notify(context.Background(), domain.Event{Kind: domain.EventCIFailed, Repo: "me/app", Title: "CI <упал>", URL: "https://x"})
	if err != nil {
		t.Fatal(err)
	}
	if text, _ := got["text"].(string); !strings.Contains(text, "CI &lt;упал&gt;") || got["chat_id"] != "42" {
		t.Fatalf("сообщение: %v", got)
	}

	chats, err := tg.ChatIDs(context.Background())
	if err != nil || chats["42"] != "Я (@me)" {
		t.Fatalf("ChatIDs = %v, %v", chats, err)
	}

	bad := NewTelegram("WRONG", "42")
	bad.API = srv.URL
	if err := bad.Notify(context.Background(), domain.Event{}); err == nil || !strings.Contains(err.Error(), "Unauthorized") {
		t.Fatalf("ошибка API должна дойти до вызывающего: %v", err)
	}
}
