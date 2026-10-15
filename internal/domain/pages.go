package domain

import "time"

// Страницы TUI — то, что на github.com открывается по клику: PR с обсуждением и diff, issue, репозиторий, коммит.

// CommentKind — откуда запись в обсуждении.
type CommentKind string

const (
	CommentPlain  CommentKind = "comment" // обычный комментарий к PR или issue
	CommentReview CommentKind = "review"  // ревью: approve / request changes / comment
	CommentLine   CommentKind = "line"    // замечание к строке кода в ревью
)

// Comment — запись обсуждения.
type Comment struct {
	Kind      CommentKind
	Author    string
	Body      string
	State     string // у ревью: APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED
	Path      string // у замечания к строке: файл и строка
	Line      int
	DiffHunk  string // кусок diff, к которому замечание
	URL       string
	CreatedAt time.Time
}

// FileDiff — изменённый файл: что с ним и сам diff (patch в формате unified, без заголовков).
type FileDiff struct {
	Path      string
	PrevPath  string // при переименовании
	Status    string // added, modified, removed, renamed
	Additions int
	Deletions int
	Patch     string // пусто у бинарных и слишком больших файлов
}

// Commit — коммит; Files заполнены только на странице коммита.
type Commit struct {
	SHA       string
	Message   string
	Author    string
	Date      time.Time
	URL       string
	Additions int
	Deletions int
	Files     []FileDiff
}

// Short — первая строка сообщения.
func (c Commit) Short() string {
	for i, r := range c.Message {
		if r == '\n' {
			return c.Message[:i]
		}
	}
	return c.Message
}

// Check — проверка коммита (job Actions или внешний CI).
type Check struct {
	Name       string
	Status     string // queued, in_progress, completed
	Conclusion string // success, failure, neutral, cancelled, skipped, timed_out, action_required
	URL        string
	Took       time.Duration
}

// State — success / failure / pending / "" — как у запусков.
func (c Check) State() string { return RunState(c.Status, c.Conclusion) }

// PRDetail — страница PR: всё, что видно на github.com.
type PRDetail struct {
	PullRequest
	Additions, Deletions, ChangedFiles int
	Mergeable                          string // clean, dirty (конфликты), blocked, behind, unstable, draft, unknown
	HeadSHA                            string
	Labels, Reviewers, Assignees       []string
	Comments                           []Comment // обсуждение по времени: комментарии, ревью, замечания к строкам
	Commits                            []Commit
	Files                              []FileDiff
	Checks                             []Check
}

// IssueDetail — страница issue.
type IssueDetail struct {
	Issue
	Assignees []string
	Comments  []Comment
	ClosedAt  time.Time
}

// Lang — язык репозитория и его доля в байтах.
type Lang struct {
	Name  string
	Bytes int
}

// RepoDetail — главная страница репозитория.
type RepoDetail struct {
	Repo
	Forks, Watchers int
	DefaultBranch   string
	Homepage        string
	License         string
	Topics          []string
	Readme          string // markdown
	Branches        []string
	Commits         []Commit // последние на ветке по умолчанию
	Languages       []Lang   // от большего к меньшему
	LatestRelease   string
	CreatedAt       time.Time
}
