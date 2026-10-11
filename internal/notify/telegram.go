package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
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

// heads — крупная строка сверху: что случилось.
var heads = map[domain.EventKind]struct{ icon, label string }{
	domain.EventPROpened:   {"🟢", "НОВЫЙ PULL REQUEST"},
	domain.EventPRMerged:   {"🟣", "PULL REQUEST СМЁРЖЕН"},
	domain.EventPRReview:   {"👀", "РЕВЬЮ"},
	domain.EventIssue:      {"📌", "ISSUE"},
	domain.EventComment:    {"💬", "КОММЕНТАРИЙ"},
	domain.EventCIFailed:   {"❌", "CI УПАЛ"},
	domain.EventCIPassed:   {"✅", "CI ПРОШЁЛ"},
	domain.EventRelease:    {"🚀", "РЕЛИЗ"},
	domain.EventPush:       {"⬆️", "ПУШ"},
	domain.EventMention:    {"🔔", "УПОМИНАНИЕ"},
	domain.EventStar:       {"⭐", "НОВАЯ ЗВЕЗДА"},
	domain.EventDaemonInfo: {"⚙️", "GW"},
}

func head(m Message) string {
	h, ok := heads[m.Kind]
	if !ok {
		h = heads[domain.EventDaemonInfo]
	}
	label := h.label
	if m.Push && m.Kind != domain.EventPush { // склейка пуша с его CI
		label = "ПУШ · " + label
	}
	return h.icon + " <b>" + label + "</b>"
}

// Render — HTML сообщения: событие → где → описание → коммиты → отчёт CI. Кнопки — отдельно.
func Render(m Message) string {
	var b strings.Builder
	b.WriteString(head(m))
	b.WriteString("\n<b>" + html.EscapeString(m.Repo) + "</b>")
	if m.Actor != "" {
		b.WriteString(" · <i>@" + html.EscapeString(m.Actor) + "</i>")
	}
	if m.Title != "" {
		b.WriteString("\n\n" + html.EscapeString(m.Title))
	}
	// лимит Telegram — 4096 видимых символов на сообщение: делим между блоками
	if m.Body != "" {
		b.WriteString("\n\n" + block("git", m.Body, 1400))
	}
	if m.Logs != "" {
		b.WriteString("\n\n" + block("ci", m.Logs, 2000))
	}
	return b.String()
}

// block — моноширинный блок с подписью, как кусок терминала.
func block(lang, text string, limit int) string {
	if r := []rune(text); len(r) > limit {
		text = string(r[:limit]) + "\n…"
	}
	return `<pre><code class="language-` + lang + `">` + html.EscapeString(text) + "</code></pre>"
}

func buttons(m Message) map[string]any {
	var row []map[string]string
	if m.URL != "" {
		row = append(row, map[string]string{"text": "Открыть на GitHub", "url": m.URL})
	}
	if m.LogsURL != "" {
		row = append(row, map[string]string{"text": "Логи CI", "url": m.LogsURL})
	}
	if len(row) == 0 {
		return nil
	}
	return map[string]any{"inline_keyboard": [][]map[string]string{row}}
}

func (t *Telegram) Notify(ctx context.Context, m Message) error {
	body := map[string]any{
		"chat_id":              t.ChatID,
		"text":                 Render(m),
		"parse_mode":           "HTML",
		"link_preview_options": map[string]bool{"is_disabled": true},
	}
	if kb := buttons(m); kb != nil {
		body["reply_markup"] = kb
	}
	return t.call(ctx, "sendMessage", body, nil)
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

func (Nop) Notify(context.Context, Message) error { return nil }
