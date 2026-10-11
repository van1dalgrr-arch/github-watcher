// Package notify — доставка событий на телефон. Первая реализация — Telegram-бот.
package notify

import (
	"context"
	"strings"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
)

type Notifier interface {
	Notify(ctx context.Context, m Message) error
}

// Rules — какие события слать на телефон (остальные только в ленте TUI).
type Rules struct {
	Kinds map[domain.EventKind]bool
	Repos map[string]bool // пусто — все репозитории
}

// DefaultKinds — что слать на телефон: всё, кроме звёзд (их — только в ленту).
var DefaultKinds = []domain.EventKind{
	domain.EventPROpened, domain.EventPRMerged, domain.EventPRReview, domain.EventIssue,
	domain.EventComment, domain.EventCIFailed, domain.EventCIPassed, domain.EventRelease,
	domain.EventMention, domain.EventPush,
}

// ParseRules собирает правила из строк вида "ci_failed,mention" и "me/app,me/api".
// Пустая строка видов — DefaultKinds, "all" — всё.
func ParseRules(kinds, repos string) Rules {
	r := Rules{Kinds: map[domain.EventKind]bool{}, Repos: map[string]bool{}}
	switch strings.TrimSpace(kinds) {
	case "":
		for _, k := range DefaultKinds {
			r.Kinds[k] = true
		}
	case "all":
		for k := range heads {
			r.Kinds[k] = true
		}
	default:
		for k := range strings.SplitSeq(kinds, ",") {
			r.Kinds[domain.EventKind(strings.TrimSpace(k))] = true
		}
	}
	for repo := range strings.SplitSeq(repos, ",") {
		if repo = strings.TrimSpace(repo); repo != "" {
			r.Repos[repo] = true
		}
	}
	return r
}

func (r Rules) Match(e domain.Event) bool {
	if !r.Kinds[e.Kind] {
		return false
	}
	return len(r.Repos) == 0 || r.Repos[e.Repo]
}

// Message — одно уведомление. Пуш и CI того же коммита склеиваются в одно сообщение (Compose).
type Message struct {
	Kind    domain.EventKind // определяет заголовок; у склейки — результат CI
	Push    bool             // в сообщении есть пуш (у склейки заголовок «ПУШ · CI …»)
	Repo    string
	Actor   string
	Title   string // описание: что за коммит / PR / issue
	Body    string // подробности пуша: коммиты и файлы
	Logs    string // отчёт CI: job-ы, шаги, ошибка
	URL     string // «Открыть на GitHub»
	LogsURL string // «Логи CI», если отличается от URL
}

// Compose собирает сообщение из событий одного коммита (или из одного события).
// logs — отчёты CI по RunID, их заранее готовит watcher.
func Compose(events []domain.Event, logs map[int64]string) Message {
	var m Message
	var reports []string
	for _, e := range events {
		switch e.Kind {
		case domain.EventPush:
			m.Push = true
			m.Repo, m.Actor, m.Title, m.Body, m.URL = e.Repo, e.Actor, e.Title, e.Body, e.URL
			if m.Kind == "" {
				m.Kind = domain.EventPush
			}
		case domain.EventCIPassed, domain.EventCIFailed:
			if m.Kind != domain.EventCIFailed { // хоть один упавший — всё сообщение «упал»
				m.Kind = e.Kind
				m.LogsURL = e.URL
			}
			if r := logs[e.RunID]; r != "" {
				reports = append(reports, r)
			}
			if !m.Push {
				m.Repo, m.Title, m.URL = e.Repo, e.Title, e.URL
			}
		default:
			m = Message{Kind: e.Kind, Repo: e.Repo, Actor: e.Actor, Title: e.Title, Body: e.Body, URL: e.URL}
		}
	}
	m.Logs = strings.Join(reports, "\n\n")
	if m.LogsURL == m.URL {
		m.LogsURL = ""
	}
	return m
}
