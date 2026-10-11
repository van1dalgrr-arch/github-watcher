package tui

import (
	"context"
	"os/exec"
	"runtime"
	"slices"
	"sort"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
	"github.com/van1dalgrr-arch/github-watcher/internal/watcher"
)

// Ответы фоновых загрузок. В каждом — для чего грузили, чтобы отбросить устаревшее (репозиторий успели сменить).
type (
	meMsg    struct{ me string }
	reposMsg struct{ repos []domain.Repo }
	prsMsg   struct {
		key string
		prs []domain.PullRequest
	}
	issuesMsg struct {
		key    string
		issues []domain.Issue
	}
	runsMsg struct {
		repo string
		runs []domain.WorkflowRun
	}
	dashRunsMsg struct{ runs []domain.WorkflowRun }
	daemonMsg   struct {
		status *watcher.Status
		events []domain.Event
		logs   []string
	}
	runLogsMsg struct {
		run  domain.WorkflowRun
		text string
	}
	feedMsg struct{ events []domain.Event } // лента напрямую из GitHub, когда демон не запущен
	errMsg  struct{ err error }
	doneMsg struct { // результат действия (merge, комментарий…)
		text   string
		reload tab
	}
	tickMsg time.Time
)

const timeout = 30 * time.Second

func (m *Model) ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(m.parent, timeout)
}

// load оборачивает запрос: считает активные загрузки (для спиннера) и превращает ошибку в errMsg.
func (m *Model) load(f func(ctx context.Context) (tea.Msg, error)) tea.Cmd {
	m.loading++
	return func() tea.Msg {
		ctx, cancel := m.ctx()
		defer cancel()
		msg, err := f(ctx)
		if err != nil {
			return loaded{errMsg{err}}
		}
		return loaded{msg}
	}
}

// loaded — обёртка, по которой Update уменьшает счётчик загрузок.
type loaded struct{ msg tea.Msg }

func (m *Model) loadMe() tea.Cmd {
	return m.load(func(ctx context.Context) (tea.Msg, error) {
		me, err := m.gh.Me(ctx)
		return meMsg{me}, err
	})
}

func (m *Model) loadRepos() tea.Cmd {
	return m.load(func(ctx context.Context) (tea.Msg, error) {
		r, err := m.gh.Repos(ctx)
		return reposMsg{r}, err
	})
}

func (m *Model) loadPRs() tea.Cmd {
	repo, state := m.repo, m.prState
	m.prsKey = repo + "|" + state
	return m.load(func(ctx context.Context) (tea.Msg, error) {
		p, err := m.gh.PullRequests(ctx, repo, state)
		return prsMsg{repo + "|" + state, p}, err
	})
}

func (m *Model) loadIssues() tea.Cmd {
	repo, state := m.repo, m.issueState
	m.issuesKey = repo + "|" + state
	return m.load(func(ctx context.Context) (tea.Msg, error) {
		i, err := m.gh.Issues(ctx, repo, state)
		return issuesMsg{repo + "|" + state, i}, err
	})
}

func (m *Model) loadRuns() tea.Cmd {
	repo := m.repo
	m.runsKey = repo
	return m.load(func(ctx context.Context) (tea.Msg, error) {
		r, err := m.gh.WorkflowRuns(ctx, repo)
		return runsMsg{repo, r}, err
	})
}

// loadDashRuns — последний запуск CI в каждом из недавно обновлённых репозиториев, параллельно.
func (m *Model) loadDashRuns() tea.Cmd {
	var repos []string
	for _, r := range m.repos {
		if !r.Archived && len(repos) < 8 {
			repos = append(repos, r.FullName())
		}
	}
	return m.load(func(ctx context.Context) (tea.Msg, error) {
		var (
			mu  sync.Mutex
			wg  sync.WaitGroup
			out []domain.WorkflowRun
		)
		for _, repo := range repos {
			wg.Go(func() {
				runs, err := m.gh.WorkflowRuns(ctx, repo)
				if err != nil {
					return
				}
				for _, r := range runs { // последний CI на код, а не запуск по расписанию или служебный notify
					if r.Event == "push" || r.Event == "pull_request" {
						mu.Lock()
						out = append(out, r)
						mu.Unlock()
						return
					}
				}
			})
		}
		wg.Wait()
		sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
		return dashRunsMsg{out}, nil
	})
}

// loadDaemon не трогает счётчик загрузок: опрос идёт каждые несколько секунд, спиннер бы не гас.
func (m *Model) loadDaemon() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := m.ctx()
		defer cancel()
		st, err := m.daemon.Status(ctx)
		if err != nil {
			return daemonMsg{}
		}
		events, _ := m.daemon.Events(ctx, "", 200)
		logs, _ := m.daemon.Logs(ctx)
		return daemonMsg{status: &st, events: events, logs: logs}
	}
}

// loadFeed — лента за неделю прямо из GitHub: без демона TUI тоже показывает пуши и CI.
func (m *Model) loadFeed() tea.Cmd {
	m.feedAt = time.Now()
	return m.load(func(ctx context.Context) (tea.Msg, error) {
		events, err := m.gh.EventsSince(ctx, time.Now().Add(-7*24*time.Hour))
		slices.Reverse(events) // новые сверху, как у демона
		return feedMsg{events}, err
	})
}

func (m *Model) loadRunLogs(run domain.WorkflowRun) tea.Cmd {
	return m.load(func(ctx context.Context) (tea.Msg, error) {
		text, err := m.gh.RunLogs(ctx, run.Repo, run.ID)
		return runLogsMsg{run, text}, err
	})
}

// act — действие с подтверждением в статусной строке и перезагрузкой вкладки.
func (m *Model) act(okText string, reload tab, f func(ctx context.Context) error) tea.Cmd {
	return m.load(func(ctx context.Context) (tea.Msg, error) {
		return doneMsg{okText, reload}, f(ctx)
	})
}

func tick() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func openURL(url string) tea.Cmd {
	return func() tea.Msg {
		name := "xdg-open"
		if runtime.GOOS == "darwin" {
			name = "open"
		}
		_ = exec.Command(name, url).Start()
		return nil
	}
}
