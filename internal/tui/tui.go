// Package tui — интерактивный интерфейс на Bubble Tea.
//
// Вкладки: Dashboard, Repos, PRs, Issues, Actions, Events, Logs.
// Списки и действия идут напрямую в GitHub, лента событий и логи — от демона по HTTP.
package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github-watcher/internal/config"
	"github-watcher/internal/daemonclient"
	"github-watcher/internal/domain"
	"github-watcher/internal/github"
	"github-watcher/internal/ui"
	"github-watcher/internal/watcher"
)

type tab int

const (
	tabDash tab = iota
	tabRepos
	tabPRs
	tabIssues
	tabActions
	tabEvents
	tabLogs
	tabCount
)

var tabNames = [tabCount]string{"Dashboard", "Repos", "Pull requests", "Issues", "Actions", "Events", "Logs"}

type Model struct {
	parent context.Context
	gh     github.Client
	daemon *daemonclient.Client
	cfg    config.Config

	w, h    int
	tab     tab
	me      string
	repo    string // выбранный репозиторий
	loading int
	spin    spinner.Model
	ticks   int

	flash    string
	flashBad bool

	repos      []domain.Repo
	prs        []domain.PullRequest
	prState    string
	prsKey     string
	issues     []domain.Issue
	issueState string
	issuesKey  string
	runs       []domain.WorkflowRun
	runsKey    string
	dashRuns   []domain.WorkflowRun

	daemonUp     bool
	daemonStatus watcher.Status
	events       []domain.Event
	daemonLogs   []string
	logsVP       viewport.Model

	lists   [tabCount]list
	filters [tabCount]string

	// модальные режимы — не больше одного одновременно
	filtering bool
	filterIn  textinput.Model
	prompt    *prompt
	confirm   *confirm
	viewer    *viewer
	help      bool
}

type prompt struct {
	title    string
	in       textinput.Model
	onSubmit func(m *Model, value string) tea.Cmd
}

type confirm struct {
	question string
	onYes    func(m *Model) tea.Cmd
}

type viewer struct {
	title string
	vp    viewport.Model
	errs  []int // строки с ошибками в логах — для прыжков по e / E
	url   string
}

func Run(ctx context.Context, gh github.Client, cfg config.Config, repo string) error {
	m := newModel(ctx, gh, daemonclient.New(cfg.DaemonURL), cfg, repo)
	_, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	if err != nil && ctx.Err() != nil {
		return nil // Ctrl+C из терминала — нормальный выход
	}
	return err
}

func newModel(ctx context.Context, gh github.Client, d *daemonclient.Client, cfg config.Config, repo string) *Model {
	return &Model{
		parent:     ctx,
		gh:         gh,
		daemon:     d,
		cfg:        cfg,
		repo:       repo,
		prState:    "open",
		issueState: "open",
		spin:       spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(ui.Warn)),
		logsVP:     viewport.New(),
		filterIn:   newInput("фильтр…"),
	}
}

func newInput(placeholder string) textinput.Model {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = placeholder
	in.CharLimit = 2000
	return in
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.loadMe(), m.loadRepos(), m.loadDaemon(), m.spin.Tick, tick())
}

// ensure загружает данные вкладки, если для текущего репозитория их ещё нет.
func (m *Model) ensure() tea.Cmd {
	if m.repo == "" {
		return nil
	}
	switch m.tab {
	case tabPRs:
		if m.prsKey != m.repo+"|"+m.prState {
			return m.loadPRs()
		}
	case tabIssues:
		if m.issuesKey != m.repo+"|"+m.issueState {
			return m.loadIssues()
		}
	case tabActions:
		if m.runsKey != m.repo {
			return m.loadRuns()
		}
	}
	return nil
}

// reload — принудительно обновить вкладку t.
func (m *Model) reload(t tab) tea.Cmd {
	switch t {
	case tabDash:
		return tea.Batch(m.loadRepos(), m.loadDaemon())
	case tabRepos:
		return m.loadRepos()
	case tabPRs:
		return m.loadPRs()
	case tabIssues:
		return m.loadIssues()
	case tabActions:
		return m.loadRuns()
	default:
		return m.loadDaemon()
	}
}

func (m *Model) setFlash(text string, bad bool) {
	m.flash, m.flashBad = text, bad
}

func (m *Model) bodyHeight() int { return max(m.h-4, 1) }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.logsVP.SetWidth(m.w)
		m.logsVP.SetHeight(m.bodyHeight())
		if m.viewer != nil {
			m.viewer.vp.SetWidth(m.w)
			m.viewer.vp.SetHeight(m.bodyHeight() - 1)
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case tickMsg:
		m.ticks++
		cmds := []tea.Cmd{tick(), m.loadDaemon()}
		// «живые» данные: Actions с незавершёнными запусками — каждые 10 с, остальное — раз в минуту
		if m.tab == tabActions && m.hasPending() && m.ticks%2 == 0 {
			cmds = append(cmds, m.loadRuns())
		} else if m.ticks%12 == 0 && m.viewer == nil {
			cmds = append(cmds, m.reload(m.tab))
			if m.tab == tabDash {
				cmds = append(cmds, m.loadDashRuns())
			}
		}
		return m, tea.Batch(cmds...)

	case daemonMsg:
		m.daemonUp = msg.status != nil
		if m.daemonUp {
			m.daemonStatus = *msg.status
			m.events = msg.events
			follow := m.logsVP.AtBottom() || len(m.daemonLogs) == 0
			m.daemonLogs = msg.logs
			m.logsVP.SetContentLines(colorLog(msg.logs))
			if follow {
				m.logsVP.GotoBottom()
			}
		}
		return m, nil

	case loaded:
		m.loading--
		return m.handleLoaded(msg.msg)

	case tea.MouseWheelMsg:
		return m, m.wheel(msg)

	case tea.KeyPressMsg:
		return m, m.key(msg)
	}

	// всё остальное (например, мигание курсора) — активному полю ввода
	return m, m.updateInputs(msg)
}

func (m *Model) handleLoaded(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case errMsg:
		m.setFlash(msg.err.Error(), true)
	case meMsg:
		m.me = msg.me
	case reposMsg:
		first := m.repos == nil
		m.repos = msg.repos
		if m.repo == "" && len(m.repos) > 0 {
			m.repo = m.repos[0].FullName() // самый свежий
		}
		if first {
			return m, tea.Batch(m.loadDashRuns(), m.ensure())
		}
	case dashRunsMsg:
		m.dashRuns = msg.runs
	case prsMsg:
		if msg.key == m.prsKey {
			m.prs = msg.prs
		}
	case issuesMsg:
		if msg.key == m.issuesKey {
			m.issues = msg.issues
		}
	case runsMsg:
		if msg.repo == m.runsKey {
			m.runs = msg.runs
		}
	case runLogsMsg:
		m.openLogs(msg.run, msg.text)
	case doneMsg:
		m.setFlash("✓ "+msg.text, false)
		return m, m.reload(msg.reload)
	}
	return m, nil
}

func (m *Model) hasPending() bool {
	for _, r := range m.runs {
		if r.Status != "completed" {
			return true
		}
	}
	return false
}

func (m *Model) updateInputs(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch {
	case m.prompt != nil:
		m.prompt.in, cmd = m.prompt.in.Update(msg)
	case m.filtering:
		m.filterIn, cmd = m.filterIn.Update(msg)
		m.filters[m.tab] = m.filterIn.Value()
	}
	return cmd
}

func (m *Model) wheel(msg tea.MouseWheelMsg) tea.Cmd {
	d := 0
	switch msg.Button {
	case tea.MouseWheelUp:
		d = -3
	case tea.MouseWheelDown:
		d = 3
	default:
		return nil
	}
	switch {
	case m.viewer != nil:
		if d < 0 {
			m.viewer.vp.ScrollUp(-d)
		} else {
			m.viewer.vp.ScrollDown(d)
		}
	case m.tab == tabLogs:
		if d < 0 {
			m.logsVP.ScrollUp(-d)
		} else {
			m.logsVP.ScrollDown(d)
		}
	case m.tab != tabDash:
		m.lists[m.tab].scroll(d, len(m.visible(m.tab)), m.listHeight())
	}
	return nil
}

func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	if k == "ctrl+c" {
		return tea.Quit
	}

	// модальные режимы перехватывают клавиатуру целиком
	switch {
	case m.help:
		m.help = false
		return nil
	case m.confirm != nil:
		c := m.confirm
		m.confirm = nil
		if k == "y" || k == "enter" || k == "д" {
			return c.onYes(m)
		}
		m.setFlash("отменено", false)
		return nil
	case m.prompt != nil:
		switch k {
		case "esc":
			m.prompt = nil
			return nil
		case "enter":
			p := m.prompt
			m.prompt = nil
			return p.onSubmit(m, strings.TrimSpace(p.in.Value()))
		}
		return m.updateInputs(msg)
	case m.filtering:
		switch k {
		case "esc":
			m.filtering = false
			m.filters[m.tab] = ""
			m.filterIn.SetValue("")
		case "enter":
			m.filtering = false
		default:
			cmd := m.updateInputs(msg)
			m.lists[m.tab].cursor = 0
			return cmd
		}
		return nil
	case m.viewer != nil:
		return m.viewerKey(msg)
	}

	// глобальные клавиши
	switch k {
	case "q":
		return tea.Quit
	case "?":
		m.help = true
		return nil
	case "tab", "l", "right":
		return m.switchTab((m.tab + 1) % tabCount)
	case "shift+tab", "h", "left":
		return m.switchTab((m.tab + tabCount - 1) % tabCount)
	case "1", "2", "3", "4", "5", "6", "7":
		n, _ := strconv.Atoi(k)
		return m.switchTab(tab(n - 1))
	case "r":
		m.setFlash("обновляю…", false)
		cmd := m.reload(m.tab)
		if m.tab == tabDash {
			cmd = tea.Batch(cmd, m.loadDashRuns())
		}
		return cmd
	case "/":
		if m.tab != tabDash && m.tab != tabLogs {
			m.filtering = true
			m.filterIn.SetValue(m.filters[m.tab])
			return m.filterIn.Focus()
		}
	case "esc":
		m.filters[m.tab] = ""
		m.flash = ""
		return nil
	}

	if m.tab == tabLogs {
		var cmd tea.Cmd
		m.logsVP, cmd = m.logsVP.Update(msg)
		return cmd
	}
	if m.tab != tabDash && m.lists[m.tab].key(k, len(m.visible(m.tab)), m.listHeight()) {
		return nil
	}
	return m.tabKey(k)
}

func (m *Model) switchTab(t tab) tea.Cmd {
	m.tab = t
	m.flash = ""
	return m.ensure()
}

func (m *Model) listHeight() int { return m.bodyHeight() - 1 } // строка заголовка над списком

// selectRepo делает репозиторий текущим и переходит к его PR.
func (m *Model) selectRepo(repo string) tea.Cmd {
	m.repo = repo
	m.prs, m.issues, m.runs = nil, nil, nil
	m.prsKey, m.issuesKey, m.runsKey = "", "", ""
	for _, t := range []tab{tabPRs, tabIssues, tabActions} {
		m.lists[t] = list{}
		m.filters[t] = ""
	}
	m.setFlash("репозиторий: "+repo, false)
	return m.switchTab(tabPRs)
}

// tabKey — действия конкретной вкладки над выбранной строкой.
func (m *Model) tabKey(k string) tea.Cmd {
	vis := m.visible(m.tab)
	cur := m.lists[m.tab].cursor
	has := cur < len(vis)

	switch m.tab {
	case tabRepos:
		if !has {
			return nil
		}
		r := m.repos[vis[cur]]
		switch k {
		case "enter", "space":
			return m.selectRepo(r.FullName())
		case "o":
			return openURL(r.URL)
		}

	case tabPRs:
		if k == "s" {
			m.prState = nextState(m.prState)
			m.lists[tabPRs] = list{}
			return m.loadPRs()
		}
		if !has {
			return nil
		}
		p := m.prs[vis[cur]]
		switch k {
		case "enter", "space":
			m.openPR(p)
		case "o":
			return openURL(p.URL)
		case "m":
			if p.State != "open" {
				m.setFlash("PR уже не открыт", true)
				return nil
			}
			m.confirm = &confirm{
				question: fmt.Sprintf("Смёржить PR #%d «%s» (squash)?", p.Number, p.Title),
				onYes: func(m *Model) tea.Cmd {
					return m.act(fmt.Sprintf("PR #%d смёржен", p.Number), tabPRs, func(ctx context.Context) error {
						return m.gh.MergePR(ctx, p.Repo, p.Number, "squash")
					})
				},
			}
		case "c":
			m.askComment(p.Repo, p.Number, tabPRs)
		}

	case tabIssues:
		switch k {
		case "s":
			m.issueState = nextState(m.issueState)
			m.lists[tabIssues] = list{}
			return m.loadIssues()
		case "n":
			return m.askNewIssue()
		}
		if !has {
			return nil
		}
		i := m.issues[vis[cur]]
		switch k {
		case "enter", "space":
			m.openIssue(i)
		case "o":
			return openURL(i.URL)
		case "c":
			m.askComment(i.Repo, i.Number, tabIssues)
		case "x":
			if i.State != "open" {
				m.setFlash("issue уже закрыт", true)
				return nil
			}
			m.confirm = &confirm{
				question: fmt.Sprintf("Закрыть issue #%d «%s»?", i.Number, i.Title),
				onYes: func(m *Model) tea.Cmd {
					return m.act(fmt.Sprintf("issue #%d закрыт", i.Number), tabIssues, func(ctx context.Context) error {
						return m.gh.CloseIssue(ctx, i.Repo, i.Number)
					})
				},
			}
		}

	case tabActions:
		if !has {
			return nil
		}
		r := m.runs[vis[cur]]
		switch k {
		case "enter", "space":
			m.setFlash("загружаю логи "+r.Name+"…", false)
			return m.loadRunLogs(r)
		case "o":
			return openURL(r.URL)
		case "R", "f":
			if r.Status != "completed" {
				m.setFlash("запуск ещё идёт", true)
				return nil
			}
			failed := k == "f"
			what := "целиком"
			if failed {
				what = "только упавшие job-ы"
			}
			m.confirm = &confirm{
				question: fmt.Sprintf("Перезапустить «%s» (%s)?", r.Name, what),
				onYes: func(m *Model) tea.Cmd {
					return m.act("запуск перезапущен", tabActions, func(ctx context.Context) error {
						return m.gh.RerunWorkflow(ctx, r.Repo, r.ID, failed)
					})
				},
			}
		}

	case tabEvents:
		if !has {
			return nil
		}
		e := m.events[vis[cur]]
		switch k {
		case "enter", "space", "o":
			if e.URL != "" {
				return openURL(e.URL)
			}
		case "f": // показать репозиторий события
			return m.selectRepo(e.Repo)
		}
	}
	return nil
}

func nextState(s string) string {
	switch s {
	case "open":
		return "closed"
	case "closed":
		return "all"
	default:
		return "open"
	}
}

func (m *Model) askComment(repo string, number int, reload tab) {
	m.prompt = &prompt{
		title: fmt.Sprintf("Комментарий к #%d", number),
		in:    newInput("текст комментария, Enter — отправить"),
		onSubmit: func(m *Model, v string) tea.Cmd {
			if v == "" {
				return nil
			}
			return m.act(fmt.Sprintf("комментарий в #%d отправлен", number), reload, func(ctx context.Context) error {
				return m.gh.Comment(ctx, repo, number, v)
			})
		},
	}
	m.prompt.in.Focus()
}

// askNewIssue — два шага: заголовок, потом описание (можно пропустить Enter-ом).
func (m *Model) askNewIssue() tea.Cmd {
	repo := m.repo
	m.prompt = &prompt{
		title: "Новый issue в " + repo + " — заголовок",
		in:    newInput("что случилось?"),
		onSubmit: func(m *Model, title string) tea.Cmd {
			if title == "" {
				return nil
			}
			m.prompt = &prompt{
				title: "Описание (Enter — пропустить)",
				in:    newInput("подробности"),
				onSubmit: func(m *Model, body string) tea.Cmd {
					return m.act("issue создан: "+title, tabIssues, func(ctx context.Context) error {
						_, err := m.gh.CreateIssue(ctx, repo, title, body)
						return err
					})
				},
			}
			return m.prompt.in.Focus()
		},
	}
	return m.prompt.in.Focus()
}

func (m *Model) newViewer(title, url string, lines []string) *viewer {
	vp := viewport.New(viewport.WithWidth(m.w), viewport.WithHeight(m.bodyHeight()-1))
	vp.SetContentLines(lines)
	return &viewer{title: title, vp: vp, url: url}
}

func (m *Model) openLogs(r domain.WorkflowRun, text string) {
	lines, errs := ui.LogLines(text)
	m.viewer = m.newViewer(fmt.Sprintf("%s %s · %s · %s", ui.CI(r.State()), r.Name, r.Branch, r.Title), r.URL, lines)
	m.viewer.errs = errs
	if len(errs) > 0 { // сразу к первой ошибке — ради неё логи обычно и открывают
		m.viewer.vp.SetYOffset(max(errs[0]-5, 0))
		m.setFlash(fmt.Sprintf("ошибок в логе: %d — e / E для перехода", len(errs)), true)
	} else {
		m.flash = ""
	}
}

func (m *Model) viewerKey(msg tea.KeyPressMsg) tea.Cmd {
	v, k := m.viewer, msg.String()
	switch k {
	case "esc", "q", "backspace":
		m.viewer = nil
		return nil
	case "o":
		if v.url != "" {
			return openURL(v.url)
		}
	case "e", "E":
		if len(v.errs) == 0 {
			return nil
		}
		y := v.vp.YOffset() + 5 // ошибка, к которой прыгнули, стоит на 5 строк ниже верха
		target := v.errs[0]
		if k == "e" {
			for _, line := range v.errs {
				if line > y {
					target = line
					break
				}
			}
		} else {
			target = v.errs[len(v.errs)-1]
			for i := len(v.errs) - 1; i >= 0; i-- {
				if v.errs[i] < y {
					target = v.errs[i]
					break
				}
			}
		}
		v.vp.SetYOffset(max(target-5, 0))
	case "g", "home":
		v.vp.GotoTop()
	case "G", "end":
		v.vp.GotoBottom()
	default:
		var cmd tea.Cmd
		v.vp, cmd = v.vp.Update(msg)
		return cmd
	}
	return nil
}
