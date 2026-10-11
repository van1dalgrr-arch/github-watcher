package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"time"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
)

// Telegram шлёт события через Bot API. Бот создаётся у @BotFather, chat id — `gw telegram`.
type Telegram struct {
	Token  string
	ChatID string
	HTTP   *http.Client
	API    string // https://api.telegram.org, в тестах — httptest
}

func NewTelegram(token, chatID string) *Telegram {
	return &Telegram{Token: token, ChatID: chatID, HTTP: &http.Client{Timeout: 15 * time.Second}, API: "https://api.telegram.org"}
}

var icons = map[domain.EventKind]string{
	domain.EventPROpened:   "🟢",
	domain.EventPRMerged:   "🟣",
	domain.EventPRReview:   "👀",
	domain.EventIssue:      "📌",
	domain.EventComment:    "💬",
	domain.EventCIFailed:   "🔴",
	domain.EventCIPassed:   "✅",
	domain.EventRelease:    "🚀",
	domain.EventPush:       "⬆️",
	domain.EventMention:    "🔔",
	domain.EventStar:       "⭐",
	domain.EventDaemonInfo: "⚙️",
}

func Icon(k domain.EventKind) string {
	if i, ok := icons[k]; ok {
		return i
	}
	return "•"
}

func (t *Telegram) Notify(ctx context.Context, e domain.Event) error {
	text := fmt.Sprintf("%s <b>%s</b>\n%s", Icon(e.Kind), html.EscapeString(e.Repo), html.EscapeString(e.Title))
	if e.Body != "" {
		body := e.Body
		if r := []rune(body); len(r) > 3000 { // лимит сообщения Telegram — 4096 символов
			body = string(r[:3000]) + "…"
		}
		text += "\n\n<pre>" + html.EscapeString(body) + "</pre>"
	}
	if e.Actor != "" {
		text += "\n<i>" + html.EscapeString(e.Actor) + "</i>"
	}
	if e.URL != "" {
		text += fmt.Sprintf("\n<a href=\"%s\">Открыть на GitHub</a>", html.EscapeString(e.URL))
	}
	return t.call(ctx, "sendMessage", map[string]any{
		"chat_id":                  t.ChatID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}, nil)
}

// ChatIDs возвращает чаты, которые недавно писали боту, — так узнаём свой chat id без ручного поиска.
func (t *Telegram) ChatIDs(ctx context.Context) (map[string]string, error) {
	var updates []struct {
		Message struct {
			Chat struct {
				ID        int64  `json:"id"`
				Username  string `json:"username"`
				FirstName string `json:"first_name"`
			} `json:"chat"`
		} `json:"message"`
	}
	if err := t.call(ctx, "getUpdates", map[string]any{}, &updates); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, u := range updates {
		c := u.Message.Chat
		if c.ID != 0 {
			name := c.FirstName
			if c.Username != "" {
				name += " (@" + c.Username + ")"
			}
			out[strconv.FormatInt(c.ID, 10)] = name
		}
	}
	return out, nil
}

func (t *Telegram) call(ctx context.Context, method string, body map[string]any, result any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		t.API+"/bot"+t.Token+"/"+method, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("telegram %s: %w", method, scrub(err, t.Token))
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("telegram %s: HTTP %d", method, resp.StatusCode)
	}
	if !out.OK {
		return fmt.Errorf("telegram %s: %s", method, out.Description)
	}
	if result != nil {
		return json.Unmarshal(out.Result, result)
	}
	return nil
}

// scrub убирает токен бота из текста ошибки (он есть в URL запроса) — чтобы не утёк в логи.
func scrub(err error, token string) error {
	return fmt.Errorf("%s", bytes.ReplaceAll([]byte(err.Error()), []byte(token), []byte("***")))
}

// Nop — когда Telegram не настроен: события копятся в ленте, на телефон ничего не уходит.
type Nop struct{}

func (Nop) Notify(context.Context, domain.Event) error { return nil }
