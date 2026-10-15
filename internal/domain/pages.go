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
	Releases        []Release
	CreatedAt       time.Time
}

// Step — шаг job-а Actions.
type Step struct {
	Name               string
	Status, Conclusion string
	Took               time.Duration
}

// Job — job запуска: шаги и время.
type Job struct {
	ID                 int64
	Name               string
	Status, Conclusion string
	Took               time.Duration
	URL                string
	Steps              []Step
}

// State — success / failure / pending / "".
func (j Job) State() string { return RunState(j.Status, j.Conclusion) }

// Artifact — файл, который сохранил запуск (бинарники, отчёты покрытия…).
type Artifact struct {
	ID      int64
	Name    string
	Size    int64
	Expired bool
}

// RunDetail — страница запуска Actions.
type RunDetail struct {
	WorkflowRun
	Actor     string
	Attempt   int
	Took      time.Duration
	Jobs      []Job
	Artifacts []Artifact
}

// Workflow — workflow репозитория (файл в .github/workflows).
type Workflow struct {
	ID    int64
	Name  string
	Path  string
	State string // active, disabled_manually…
}

// Notification — уведомление GitHub (колокольчик на сайте): запрос ревью, упоминание, назначение…
type Notification struct {
	ID        string
	Repo      string
	Type      string // PullRequest, Issue, Release, CheckSuite, Discussion, Commit
	Title     string
	Reason    string // review_requested, mention, assign, author, comment, ci_activity, state_change, subscribed…
	Unread    bool
	Number    int    // номер PR / issue, если уведомление про них
	URL       string // страница на github.com
	UpdatedAt time.Time
}

// Day — день сетки вкладов: сколько коммитов, PR, issues, ревью.
type Day struct {
	Date  time.Time
	Count int
	Level int // 0…4 — насколько зелёная клетка, как на сайте
}

// Profile — страница пользователя: о себе, сетка вкладов за год, закреплённые репозитории.
type Profile struct {
	Login, Name, Bio     string
	Company, Location    string
	Website              string
	Followers, Following int
	PublicRepos          int
	Contributions        int     // за последний год
	Weeks                [][]Day // недели слева направо, в неделе — с воскресенья
	Pinned               []Repo
	Repos                []Repo // публичные, по свежести пуша
	Starred              []Repo
	CreatedAt            time.Time
}

// Found — найденный issue или PR.
type Found struct {
	Repo      string
	Number    int
	Title     string
	State     string
	PR        bool
	Author    string
	Comments  int
	URL       string
	UpdatedAt time.Time
}

// CodeHit — файл, где нашёлся текст, и строка вокруг находки.
type CodeHit struct {
	Repo, Path, URL string
	Fragment        string
}

// SearchResult — поиск по всему GitHub, как строка поиска на сайте.
type SearchResult struct {
	Query  string
	Repos  []Repo
	Issues []Found
	Users  []string
	Code   []CodeHit
	Totals [4]int // сколько всего нашлось: репозитории, issues и PR, люди, код
}

// Asset — файл релиза (бинарник, архив).
type Asset struct {
	Name      string
	Size      int64
	Downloads int
	URL       string
}

// Release — релиз: тег, заметки (markdown), файлы.
type Release struct {
	Tag, Name  string
	Body       string
	Author     string
	Draft      bool
	Prerelease bool
	CreatedAt  time.Time
	URL        string
	Assets     []Asset
}
