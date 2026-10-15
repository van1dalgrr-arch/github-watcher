// Package tui — интерактивный интерфейс на Bubble Tea.
//
// Вкладки: Dashboard, Repos, PRs, Issues, Actions, Events, Logs.
// Списки и действия идут напрямую в GitHub, лента событий и логи — от демона по HTTP.
package tui

import (
	"context"
	"fmt"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/van1dalgrr-arch/github-watcher/internal/config"
	"github.com/van1dalgrr-arch/github-watcher/internal/daemonclient"
	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
	"github.com/van1dalgrr-arch/github-watcher/internal/github"
	"github.com/van1dalgrr-arch/github-watcher/internal/ui"
	"github.com/van1dalgrr-arch/github-watcher/internal/watcher"
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
	tabCode
	tabInbox
	tabCount
)

var tabNames = [tabCount]string{"Dashboard", "Repos", "Pull requests", "Issues", "Actions", "Events", "Logs", "Code", "Inbox"}

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
	inbox      []domain.Notification // уведомления GitHub (вкладка Inbox, счётчик в шапке)
	inboxAll   bool                  // показывать и прочитанные
	inboxReady bool

	feedAt       time.Time // когда последний раз брали ленту напрямую из GitHub
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
	pages     []*page // открытые страницы (PR, issue, репозиторий, коммит), сверху — последняя
	pageSeq   int
	term      term                     // панель-терминал внизу (`:`)
	gitTerm   term                     // Dashboard: команды для git в папке репозитория
	daemon2   term                     // Dashboard: gw daemon, запущенный из TUI
	daemonCmd string                   // чем запускать демона; пусто — этот же gw с аргументом daemon (тесты подставляют своё)
	findClone func(repo string) string // где искать клон; nil — findClone (тесты подставляют свою папку)
	editor    string                   // чем править текст PR; пусто — $VISUAL / $EDITOR
	code      code                     // вкладка Code
}

type prompt struct {
	title    string
	in       textinput.Model
	onSubmit func(m *Model, value string) tea.Cmd
}

type confirm struct {
	question string
	onYes    func(m *Model) tea.Cmd
	alt      []pageKey // дополнительные ответы, кроме y / n (например, d — черновиком)
}

type viewer struct {
	title string
	vp    viewport.Model
	errs  []int // строки с ошибками в логах — для прыжков по e / E
	url   string
}

func Run(ctx context.Context, gh github.Client, cfg config.Config, repo string) error {
	// TUI живёт часами: чаще собираем мусор — меньше памяти держим про запас (данных тут немного)
	debug.SetGCPercent(50)
	m := newModel(ctx, gh, daemonclient.New(cfg.DaemonURL), cfg, repo)
	defer m.stopDaemon() // демон, запущенный из TUI, живёт, пока открыт gw
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
		term:       newTerm(termBottom, "команда — например git status, go test ./..."),
		gitTerm:    newGitTerm(repo),
		daemon2:    newDaemonTerm(),
	}
}

func newInput(placeholder string) textinput.Model {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = placeholder
	in.CharLimit = 2000
	steadyCursor(&in)
	return in
}

// steadyCursor — курсор без мигания: мигание будит TUI дважды в секунду ради перерисовки.
func steadyCursor(in *textinput.Model) {
	st := in.Styles()
	st.Cursor.Blink = false
	in.SetStyles(st)
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.loadMe(), m.loadRepos(), m.loadDaemon(), m.loadInbox(), m.spin.Tick, tick())
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
	case tabCode:
		if m.code.repo != m.repo {
			return m.loadTree()
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
	case tabCode:
		return m.loadTree()
	case tabInbox:
		return m.loadInbox()
	default:
		return m.loadDaemon()
	}
}

func (m *Model) setFlash(text string, bad bool) {
	m.flash, m.flashBad = text, bad
}

func (m *Model) bodyHeight() int { return max(m.h-4, 1) }

// termHeight — высота панели-терминала с рамкой; 0, если она скрыта.
func (m *Model) termHeight() int {
	if !m.term.open {
		return 0
	}
	return max(m.bodyHeight()*2/5, 8)
}

// mainHeight — сколько остаётся вкладке над терминалом.
func (m *Model) mainHeight() int { return max(m.bodyHeight()-m.termHeight(), 3) }

// layout раздаёт размеры прокручиваемым областям — после изменения окна или открытия терминала.
func (m *Model) layout() {
	m.logsVP.SetWidth(m.w)
	m.logsVP.SetHeight(m.mainHeight())
	if m.viewer != nil {
		m.viewer.vp.SetWidth(m.w)
		m.viewer.vp.SetHeight(m.mainHeight() - 1)
	}
	if p := m.topPage(); p != nil && p.ready {
		m.rebuild(p)
	}
	m.term.resize(m.w-4, m.termHeight()-4)
	m.term.in.SetWidth(max(m.w-lipgloss.Width(m.term.prompt())-6, 10))
	m.layoutDash()
}

// termByID — терминал, приславший вывод.
func (m *Model) termByID(id termID) *term {
	switch id {
	case termGit:
		return &m.gitTerm
	case termDaemon:
		return &m.daemon2
	}
	return &m.term
}

// focusedTerm — терминал, который сейчас принимает клавиатуру; nil — никакой.
func (m *Model) focusedTerm() *term {
	for _, t := range []*term{&m.term, &m.gitTerm} {
		if t.focused {
			return t
		}
	}
	return nil
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.layout()
		return m, nil

	case termOutMsg:
		t := m.termByID(msg.id)
		t.feed(msg.data)
		return m, t.wait()

	case termDoneMsg:
		t := m.termByID(msg.id)
		t.done(msg)
		if then := t.then; then != nil { // команду запустили ради следующего шага (git push → создать PR)
			t.then = nil
			return m, then(m, msg.code)
		}
		if msg.id == termDaemon {
			return m, m.loadDaemon() // демон остановился — шапка и панели узнают сразу
		}
		return m, nil

	case editedMsg:
		return m, m.edited(msg)

	case previewMsg:
		if msg.token == m.code.token && m.tab == tabCode {
			if p := m.selectedFile(); p != "" && p != m.code.path {
				return m, m.loadFile(p)
			}
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
		if p := m.topPage(); p != nil && p.live && p.ready && m.ticks%2 == 0 {
			cmds = append(cmds, m.fetchPage(p)) // идущий запуск: job-ы и шаги обновляются сами
		} else if m.tab == tabActions && m.hasPending() && m.ticks%2 == 0 {
			cmds = append(cmds, m.loadRuns())
		} else if m.ticks%12 == 0 && m.viewer == nil {
			cmds = append(cmds, m.reload(m.tab))
			if m.tab == tabDash {
				cmds = append(cmds, m.loadDashRuns())
			}
			if m.tab != tabInbox { // счётчик непрочитанных в шапке — свежий на любой вкладке
				cmds = append(cmds, m.loadInbox())
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
			return m, nil
		}
		if time.Since(m.feedAt) > time.Minute { // демона нет — раз в минуту берём ленту сами
			return m, m.loadFeed()
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
	case feedMsg:
		if !m.daemonUp {
			m.events = msg.events
		}
	case treeMsg:
		if msg.repo == m.code.repo {
			m.code.files, m.code.local = msg.files, msg.local
			m.lists[tabCode] = list{}
			return m, m.schedulePreview()
		}
	case fileMsg:
		if msg.repo == m.code.repo {
			m.code.remember(msg.path, msg.lines)
			if msg.path == m.selectedFile() {
				m.code.path, m.code.lines, m.code.err = msg.path, msg.lines, ""
			}
		}
	case dashRunsMsg:
		m.dashRuns = msg.runs
	case inboxMsg:
		if msg.all == m.inboxAll {
			m.inbox, m.inboxReady = msg.items, true
		}
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
	case pageMsg:
		m.fillPage(msg)
	case prCreatedMsg:
		m.setFlash(fmt.Sprintf("✓ PR #%d создан", msg.pr.Number), false)
		m.prsKey = "" // список PR перечитается при возврате
		return m, m.openPRPage(msg.pr.Repo, msg.pr.Number, msg.pr.Title)
	case pageDoneMsg:
		m.setFlash("✓ "+msg.text, false)
		if p := m.pageByID(msg.id); p != nil {
			return m, m.fetchPage(p)
		}
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
	if t := m.focusedTerm(); t != nil {
		if d < 0 {
			t.vp.ScrollUp(-d)
		} else {
			t.vp.ScrollDown(d)
		}
		return nil
	}
	if p := m.topPage(); p != nil && m.viewer == nil {
		m.pageWheel(p, d)
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
		if m.tab == tabCode {
			return m.schedulePreview()
		}
	}
	return nil
}

func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	if t := m.focusedTerm(); t != nil && m.confirm == nil && m.prompt == nil {
		return m.termKey(t, msg)
	}
	if k == "ctrl+c" {
		return m.quit()
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
		for _, a := range c.alt {
			if a.key == k {
				return a.do(m)
			}
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
		if m.tab == tabCode {
			return m.schedulePreview()
		}
		return nil
	case m.viewer != nil:
		return m.viewerKey(msg)
	}
	if p := m.topPage(); p != nil && k != ":" && k != "?" {
		return m.pageKey(p, msg)
	}

	// глобальные клавиши
	switch k {
	case "q":
		return m.quit()
	case ":":
		m.term.open, m.term.focused = true, true
		m.layout()
		return m.term.in.Focus()
	case "?":
		m.help = true
		return nil
	case "tab", "l", "right":
		return m.switchTab((m.tab + 1) % tabCount)
	case "shift+tab", "h", "left":
		return m.switchTab((m.tab + tabCount - 1) % tabCount)
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
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
		if m.tab == tabCode {
			return m.schedulePreview()
		}
		return nil
	}
	return m.tabKey(k)
}

func (m *Model) switchTab(t tab) tea.Cmd {
	m.tab = t
	m.flash = ""
	return m.ensure()
}

func (m *Model) listHeight() int { return m.mainHeight() - 1 } // строка заголовка над списком

// termKey — клавиши, когда в фокусе терминал. Ctrl+C здесь останавливает команду, а не gw.
func (m *Model) termKey(t *term, msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		if t.id == termBottom {
			t.open = false // панель внизу скрывается; терминалы Dashboard остаются на месте
		}
		t.focused = false // команда, если идёт, продолжает работать
		t.in.Blur()
		m.layout()
		return nil
	case "ctrl+c":
		if t.running() {
			t.interrupt()
		} else {
			t.in.SetValue("")
		}
		return nil
	case "ctrl+l":
		t.lines = nil
		t.refresh()
		return nil
	case "enter":
		line := t.in.Value()
		t.in.SetValue("")
		if t.running() {
			t.send(line)
			return nil
		}
		cmd := t.run(line)
		if !t.open && t.id == termBottom {
			m.layout() // exit закрыл панель
		}
		return cmd
	case "up", "down":
		if t.running() || len(t.history) == 0 {
			return nil
		}
		if msg.String() == "up" {
			t.hpos = max(t.hpos-1, 0)
		} else {
			t.hpos = min(t.hpos+1, len(t.history))
		}
		if t.hpos < len(t.history) {
			t.in.SetValue(t.history[t.hpos])
		} else {
			t.in.SetValue("")
		}
		t.in.CursorEnd()
		return nil
	case "pgup":
		t.vp.PageUp()
		return nil
	case "pgdown":
		t.vp.PageDown()
		return nil
	}
	var cmd tea.Cmd
	t.in, cmd = t.in.Update(msg)
	return cmd
}

// selectRepo делает репозиторий текущим и переходит к его PR.
func (m *Model) selectRepo(repo string) tea.Cmd {
	m.selectRepoQuiet(repo)
	m.setFlash("репозиторий: "+repo, false)
	return m.switchTab(tabPRs)
}

// selectRepoQuiet делает репозиторий текущим, не уходя с вкладки.
func (m *Model) selectRepoQuiet(repo string) {
	if repo == m.repo {
		return
	}
	m.repo = repo
	m.gitTerm.follow(repo)
	m.prs, m.issues, m.runs = nil, nil, nil
	m.prsKey, m.issuesKey, m.runsKey = "", "", ""
	for _, t := range []tab{tabPRs, tabIssues, tabActions} {
		m.lists[t] = list{}
		m.filters[t] = ""
	}
}

// tabKey — действия конкретной вкладки над выбранной строкой.
func (m *Model) tabKey(k string) tea.Cmd {
	vis := m.visible(m.tab)
	cur := m.lists[m.tab].cursor
	has := cur < len(vis)

	switch m.tab {
	case tabDash:
		switch k {
		case "enter", "i":
			return m.focusGit()
		case "d":
			return m.toggleDaemon()
		case "P":
			if m.me == "" {
				return nil
			}
			return m.openProfilePage(m.me)
		}

	case tabRepos:
		if !has {
			return nil
		}
		r := m.repos[vis[cur]]
		switch k {
		case "enter", "space": // страница репозитория: README, коммиты, ветки; p / i / a — к его PR, issues, Actions
			m.selectRepoQuiet(r.FullName())
			return m.openRepoPage(r.FullName())
		case "p":
			return m.selectRepo(r.FullName())
		case "o":
			return openURL(r.URL)
		}

	case tabPRs:
		if k == "n" {
			return m.newPR()
		}
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
			return m.openPRPage(p.Repo, p.Number, p.Title)
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
			return m.openIssuePage(i.Repo, i.Number, i.Title)
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
		if k == "w" {
			return m.openWorkflowsPage(m.repo)
		}
		if !has {
			return nil
		}
		r := m.runs[vis[cur]]
		switch k {
		case "enter", "space": // страница запуска: job-ы, шаги, артефакты
			return m.openRunPage(r)
		case "L":
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

	case tabCode:
		if !has {
			return nil
		}
		p := m.code.files[vis[cur]]
		switch k {
		case "enter", "space":
			if p != m.code.path {
				return m.loadFile(p) // ещё не загружен — откроем после загрузки по второму enter
			}
			m.viewer = m.newViewer(p, codeURL(m.repo, p), numbered(m.code.lines, 0))
		case "o":
			return openURL(codeURL(m.repo, p))
		}

	case tabInbox:
		i := 0
		if has {
			i = vis[cur]
		}
		return m.inboxKey(k, i, has)

	case tabEvents:
		if !has {
			return nil
		}
		e := m.events[vis[cur]]
		switch k {
		case "enter", "space":
			if cmd := m.openByURL(e.Repo, e.URL, e.Title); cmd != nil { // PR и issue — их страницы
				return cmd
			}
			if e.Body != "" { // у пуша — коммиты и файлы
				m.viewer = m.newViewer(e.Repo+" · "+e.Title, e.URL, colorBody(e.Body))
				return nil
			}
			if e.URL != "" {
				return openURL(e.URL)
			}
		case "o":
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
	vp := viewport.New(viewport.WithWidth(m.w), viewport.WithHeight(m.mainHeight()-1))
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

func codeURL(repo, path string) string { return "https://github.com/" + repo + "/blob/HEAD/" + path }
