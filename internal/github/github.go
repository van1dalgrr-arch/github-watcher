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

	// действия
	MergePR(ctx context.Context, repo string, number int, method string) error
	CreateIssue(ctx context.Context, repo, title, body string) (domain.Issue, error)
	CloseIssue(ctx context.Context, repo string, number int) error
	Comment(ctx context.Context, repo string, number int, body string) error
	RerunWorkflow(ctx context.Context, repo string, runID int64, onlyFailed bool) error

	// для watcher-а: всё новое с момента since (Events + Notifications + Actions)
	EventsSince(ctx context.Context, since time.Time) ([]domain.Event, error)

	// переменные Actions — хранилище состояния для `gw notify` между запусками по расписанию
	Variable(ctx context.Context, repo, name string) (string, error)
	SetVariable(ctx context.Context, repo, name, value string) error
}
