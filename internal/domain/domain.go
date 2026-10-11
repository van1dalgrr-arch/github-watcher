// Package domain — общие типы. Ни от чего не зависит, от него зависят все.
package domain

import "time"

type Repo struct {
	Owner, Name string
	Description string
	Language    string
	Private     bool
	Fork        bool
	Archived    bool
	Stars       int
	OpenIssues  int // GitHub считает сюда и PR
	URL         string
	PushedAt    time.Time
}

func (r Repo) FullName() string { return r.Owner + "/" + r.Name }

type PullRequest struct {
	Repo      string // owner/name
	Number    int
	Title     string
	Author    string
	State     string // open, closed, merged
	Draft     bool
	Head      string // ветка PR
	Base      string // куда мержим
	Body      string
	CIStatus  string // success, failure, pending, ""
	URL       string
	UpdatedAt time.Time
}

type Issue struct {
	Repo      string
	Number    int
	Title     string
	Author    string
	State     string
	Labels    []string
	Comments  int
	Body      string
	URL       string
	UpdatedAt time.Time
}

// WorkflowRun — запуск GitHub Actions.
type WorkflowRun struct {
	Repo       string
	ID         int64
	Name       string
	Branch     string
	HeadSHA    string
	Event      string // push, pull_request, schedule…
	Title      string // заголовок коммита/PR
	Status     string // queued, in_progress, completed
	Conclusion string // success, failure, cancelled, ""
	URL        string
	UpdatedAt  time.Time
}

// State сводит status+conclusion к одному слову: success, failure, pending, cancelled.
func (r WorkflowRun) State() string { return RunState(r.Status, r.Conclusion) }

func RunState(status, conclusion string) string {
	if status != "completed" {
		return "pending"
	}
	switch conclusion {
	case "success", "skipped", "neutral":
		return "success"
	case "cancelled":
		return "cancelled"
	default:
		return "failure"
	}
}

// EventKind — что произошло. По нему фильтруются уведомления.
type EventKind string

const (
	EventPROpened   EventKind = "pr_opened"
	EventPRMerged   EventKind = "pr_merged"
	EventPRReview   EventKind = "pr_review"
	EventIssue      EventKind = "issue"
	EventComment    EventKind = "comment"
	EventCIFailed   EventKind = "ci_failed"
	EventCIPassed   EventKind = "ci_passed"
	EventRelease    EventKind = "release"
	EventPush       EventKind = "push"
	EventMention    EventKind = "mention"
	EventStar       EventKind = "star"
	EventDaemonInfo EventKind = "daemon" // служебные: демон запустился, упал поллинг и т.п.
)

// Event — единица ленты: watcher создаёт, store хранит, notifier отправляет, TUI показывает.
type Event struct {
	ID        int64     `json:"id"`
	SourceID  string    `json:"source_id"` // id у GitHub — для дедупликации
	Kind      EventKind `json:"kind"`
	Repo      string    `json:"repo"`
	Actor     string    `json:"actor,omitempty"`
	Title     string    `json:"title"`
	Body      string    `json:"body,omitempty"`   // подробности: для пуша — коммиты и файлы с +/−
	SHA       string    `json:"sha,omitempty"`    // коммит: по нему пуш и его CI склеиваются в одно уведомление
	RunID     int64     `json:"run_id,omitempty"` // запуск Actions — для логов в уведомлении
	Logs      string    `json:"-"`                // отчёт CI для уведомления; собирается перед отправкой, не хранится
	URL       string    `json:"url,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Notified  bool      `json:"notified"`
}

// Plural — русское склонение по числу: 1 коммит, 2 коммита, 5 коммитов.
func Plural(n int, one, few, many string) string {
	n %= 100
	switch {
	case n >= 11 && n <= 14:
		return many
	case n%10 == 1:
		return one
	case n%10 >= 2 && n%10 <= 4:
		return few
	default:
		return many
	}
}
