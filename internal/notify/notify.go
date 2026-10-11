// Package notify — доставка событий на телефон. Первая реализация — Telegram-бот.
package notify

import (
	"context"
	"strings"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
)

type Notifier interface {
	Notify(ctx context.Context, e domain.Event) error
}

// Rules — какие события слать на телефон (остальные только в ленте TUI).
type Rules struct {
	Kinds map[domain.EventKind]bool
	Repos map[string]bool // пусто — все репозитории
}

// DefaultKinds — то, ради чего стоит отвлечься: звёзды и успешный CI по умолчанию только в ленте.
var DefaultKinds = []domain.EventKind{
	domain.EventPROpened, domain.EventPRMerged, domain.EventPRReview, domain.EventIssue,
	domain.EventComment, domain.EventCIFailed, domain.EventRelease, domain.EventMention, domain.EventPush,
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
		for k := range icons {
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
