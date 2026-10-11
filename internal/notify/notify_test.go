package notify

import (
	"context"
	"encoding/json"
	"fmt"
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
	err := tg.Notify(context.Background(), Message{Kind: domain.EventCIFailed, Repo: "me/app", Title: "fix <bug>", Logs: "✗ test", URL: "https://x"})
	if err != nil {
		t.Fatal(err)
	}
	text, _ := got["text"].(string)
	for _, want := range []string{"❌ <b>CI УПАЛ</b>", "fix &lt;bug&gt;", `<pre><code class="language-ci">✗ test</code></pre>`} {
		if !strings.Contains(text, want) {
			t.Errorf("в сообщении нет %q:\n%s", want, text)
		}
	}
	if !strings.Contains(fmt.Sprint(got["reply_markup"]), "https://x") || got["chat_id"] != "42" {
		t.Fatalf("кнопка или чат: %v", got)
	}

	chats, err := tg.ChatIDs(context.Background())
	if err != nil || chats["42"] != "Я (@me)" {
		t.Fatalf("ChatIDs = %v, %v", chats, err)
	}

	bad := NewTelegram("WRONG", "42")
	bad.API = srv.URL
	if err := bad.Notify(context.Background(), Message{}); err == nil || !strings.Contains(err.Error(), "Unauthorized") {
		t.Fatalf("ошибка API должна дойти до вызывающего: %v", err)
	}
}

func TestCompose(t *testing.T) {
	events := []domain.Event{
		{Kind: domain.EventPush, Repo: "me/app", Actor: "me", Title: "main: 2 коммита", Body: "• abc fix", URL: "https://compare", SHA: "abc"},
		{Kind: domain.EventCIPassed, Repo: "me/app", Title: "ci", URL: "https://run/1", SHA: "abc", RunID: 1},
		{Kind: domain.EventCIFailed, Repo: "me/app", Title: "lint", URL: "https://run/2", SHA: "abc", RunID: 2},
	}
	m := Compose(events, map[int64]string{1: "✓ test", 2: "✗ lint"})
	if m.Kind != domain.EventCIFailed || !m.Push || m.Title != "main: 2 коммита" || m.URL != "https://compare" || m.LogsURL != "https://run/2" {
		t.Fatalf("склейка: %+v", m)
	}
	if m.Logs != "✓ test\n\n✗ lint" {
		t.Fatalf("логи: %q", m.Logs)
	}
	text := Render(m)
	if !strings.HasPrefix(text, "❌ <b>ПУШ · CI УПАЛ</b>") || !strings.Contains(text, "language-git") {
		t.Fatalf("сообщение:\n%s", text)
	}
}
