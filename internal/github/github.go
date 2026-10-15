// Package github — единственное место, которое ходит в GitHub API.
// Остальной код знает только интерфейс Client, поэтому в тестах подставляется заглушка.
package github

import (
	"context"
	"time"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
)

type Client interface {
	// чтение
	Me(ctx context.Context) (string, error)
	Repos(ctx context.Context) ([]domain.Repo, error)
	PullRequests(ctx context.Context, repo string, state string) ([]domain.PullRequest, error)
	Issues(ctx context.Context, repo string, state string) ([]domain.Issue, error)
	WorkflowRuns(ctx context.Context, repo string) ([]domain.WorkflowRun, error)
	RunLogs(ctx context.Context, repo string, runID int64) (string, error)
	RunReport(ctx context.Context, repo string, runID int64) (string, error) // сжатый отчёт для уведомления
	Tree(ctx context.Context, repo string) ([]string, error)                 // пути всех файлов ветки по умолчанию
	File(ctx context.Context, repo, path string) (string, error)

	// страницы — как на github.com: PR с обсуждением и diff, issue, репозиторий, коммит
	PR(ctx context.Context, repo string, number int) (domain.PRDetail, error)
	Issue(ctx context.Context, repo string, number int) (domain.IssueDetail, error)
	Repo(ctx context.Context, repo string) (domain.RepoDetail, error)
	Commit(ctx context.Context, repo, sha string) (domain.Commit, error)

	// действия
	MergePR(ctx context.Context, repo string, number int, method string) error
	CreateIssue(ctx context.Context, repo, title, body string) (domain.Issue, error)
	CloseIssue(ctx context.Context, repo string, number int) error
	Comment(ctx context.Context, repo string, number int, body string) error
	RerunWorkflow(ctx context.Context, repo string, runID int64, onlyFailed bool) error
	Review(ctx context.Context, repo string, number int, event, body string) error // APPROVE / REQUEST_CHANGES / COMMENT
	ReopenIssue(ctx context.Context, repo string, number int) error
	CreatePR(ctx context.Context, repo, head, base, title, body string, draft bool) (domain.PullRequest, error)

	// Actions: страница запуска, лог job-а, отмена, артефакты, ручной запуск
	Run(ctx context.Context, repo string, runID int64) (domain.RunDetail, error)
	JobLog(ctx context.Context, repo string, jobID int64) (string, error)
	CancelRun(ctx context.Context, repo string, runID int64) error
	DownloadArtifact(ctx context.Context, repo string, id int64, name, dir string) (string, error)
	Workflows(ctx context.Context, repo string) ([]domain.Workflow, error)
	Dispatch(ctx context.Context, repo string, workflowID int64, ref string) error

	// входящие: уведомления GitHub
	Notifications(ctx context.Context, all bool) ([]domain.Notification, error)
	MarkRead(ctx context.Context, id string) error
	MarkDone(ctx context.Context, id string) error
	MarkAllRead(ctx context.Context) error

	// профиль: о себе, сетка вкладов за год, закреплённые, репозитории, звёзды
	Profile(ctx context.Context, login string) (domain.Profile, error)
	Search(ctx context.Context, query string) (domain.SearchResult, error) // по всему GitHub: репозитории, issues и PR, люди, код
	CreateRelease(ctx context.Context, repo, tag, target string, draft bool) (domain.Release, error)

	// для watcher-а: всё новое с момента since (Events + Notifications + Actions)
	EventsSince(ctx context.Context, since time.Time) ([]domain.Event, error)

	// переменные Actions — хранилище состояния для `gw notify` между запусками по расписанию
	Variable(ctx context.Context, repo, name string) (string, error)
	SetVariable(ctx context.Context, repo, name, value string) error
}
