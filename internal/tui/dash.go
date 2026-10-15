package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/van1dalgrr-arch/github-watcher/internal/ui"
)

// Dashboard: сверху профиль, CI и лента, снизу два терминала —
// слева gw daemon (d — запустить здесь или остановить), справа git в папке репозитория (enter — писать).

// dashGeom — раскладка Dashboard по высоте: баннер, верхний ряд панелей, ряд терминалов.
type dashGeom struct {
	banner     bool
	topH, botH int
	wide       bool // терминалы рядом; иначе друг под другом
	leftW      int  // ширина терминала демона (в широкой раскладке)
}

func (m *Model) dashGeom() dashGeom {
	h := m.mainHeight()
	g := dashGeom{wide: m.w >= 100}
	// большой логотип, как у berth, — только когда терминалам остаётся много места
	if m.w >= bannerWidth+2 && h-bannerHeight-1 >= 40 {
		g.banner = true
		h -= bannerHeight + 1
	}
	g.topH = min(max(h*2/5, 8), 13)
	if !g.wide {
		g.topH = min(g.topH, 8)
	}
	g.botH = h - g.topH
	g.leftW = m.w / 2
	return g
}

// layoutDash раздаёт размеры терминалам Dashboard: PTY должен знать ширину панели.
func (m *Model) layoutDash() {
	g := m.dashGeom()
	dw, gw, dh, gh := g.leftW, m.w-g.leftW, g.botH, g.botH
	if !g.wide {
		dw, gw = m.w, m.w
		dh = g.botH / 2
		gh = g.botH - dh
	}
	// рамка с отступами — 4 по ширине; по высоте: рамка 2, заголовок 1, строка ввода 1
	m.daemon2.resize(dw-4, dh-4)
	m.gitTerm.resize(gw-4, gh-4)
	m.gitTerm.in.SetWidth(max(gw-lipgloss.Width(m.gitTerm.prompt())-6, 10))
}

func (m *Model) dashView() string {
	g := m.dashGeom()
	var top string
	switch {
	case m.w >= 130: // профиль, CI и лента (или последний пуш, пока демон не запущен)
		pw := max(m.w/4, 38)
		cw := (m.w - pw) / 2
		third := m.feedPanel(m.w-pw-cw, g.topH)
		if !m.daemonUp {
			third = m.pushPanel(m.w-pw-cw, g.topH)
		}
		top = lipgloss.JoinHorizontal(lipgloss.Top, m.profilePanel(pw, g.topH), m.ciPanel(cw, g.topH), third)
	case g.wide:
		pw := max(m.w*2/5, 38)
		top = lipgloss.JoinHorizontal(lipgloss.Top, m.profilePanel(pw, g.topH), m.ciPanel(m.w-pw, g.topH))
	default:
		top = m.ciPanel(m.w, g.topH)
	}
	var bottom string
	if g.wide {
		bottom = lipgloss.JoinHorizontal(lipgloss.Top,
			m.daemonPanel(g.leftW, g.botH), m.gitPanel(m.w-g.leftW, g.botH))
	} else {
		dh := g.botH / 2
		bottom = lipgloss.JoinVertical(lipgloss.Left, m.daemonPanel(m.w, dh), m.gitPanel(m.w, g.botH-dh))
	}
	out := lipgloss.JoinVertical(lipgloss.Left, top, bottom)
	if g.banner {
		out = banner() + "\n\n" + out
	}
	return out
}

// termPanel — терминал в рамке: заголовок, вывод, нижняя строка (ввод или подсказка).
func termPanel(title string, w, h int, focused bool, body, last string) string {
	style := sPanel
	if focused {
		style = style.BorderForeground(ui.Accent)
	}
	inner := w - 4
	lines := []string{fit(title, inner)}
	for l := range strings.Lines(body) {
		lines = append(lines, fit(strings.TrimRight(l, "\n"), inner))
	}
	for len(lines) < h-3 {
		lines = append(lines, "")
	}
	lines = append(lines[:h-3], fit(last, inner))
	return style.Width(w).Height(h).MaxHeight(h).Render(strings.Join(lines, "\n"))
}

// ── git ────────────────────────────────────────────────────────────────────

func newGitTerm(repo string) term {
	t := newTerm(termGit, "git status, git pull, git log --oneline…")
	t.follow(repo)
	return t
}

// follow переводит git-терминал в клон выбранного репозитория — пока в нём ничего не идёт
// и ты не ушёл в другую папку сам (cd).
func (t *term) follow(repo string) {
	if t.running() || t.moved || repo == "" {
		return
	}
	t.repo = repo
	if dir := findClone(repo); dir != "" {
		t.dir, t.clone = dir, true
		return
	}
	t.clone = false
	if wd, err := os.Getwd(); err == nil {
		t.dir = wd
	}
}

// findClone ищет локальный клон repo: текущая папка, потом привычные места (~/dev, ~/code, ~/src…).
func findClone(repo string) string {
	if dir := localRoot(repo); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	owner, name, _ := strings.Cut(repo, "/")
	var cands []string
	for _, base := range []string{"dev", "code", "src", "projects", "work", "Developer"} {
		cands = append(cands, filepath.Join(home, base, name))
		more, _ := filepath.Glob(filepath.Join(home, base, "*", name))
		cands = append(cands, more...)
	}
	cands = append(cands, filepath.Join(home, "go", "src", "github.com", owner, name), filepath.Join(home, name))
	for _, dir := range cands {
		out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
		if err != nil {
			continue
		}
		if mm := remoteRe.FindStringSubmatch(strings.TrimSpace(string(out))); mm != nil && strings.EqualFold(mm[1]+"/"+mm[2], repo) {
			return dir
		}
	}
	return ""
}

func (m *Model) focusGit() tea.Cmd {
	m.gitTerm.focused = true
	m.layout()
	return m.gitTerm.in.Focus()
}

func (m *Model) gitPanel(w, h int) string {
	t := &m.gitTerm
	title := ui.Title.Render("git")
	switch {
	case t.running():
		title += "  " + m.spin.View() + ui.Faint.Render(" "+t.history[len(t.history)-1])
	case !t.clone && t.repo != "":
		title += ui.Faint.Render("  · клона " + t.repo + " не нашёл — работаю в " + shortDir(t.dir))
	default:
		title += ui.Faint.Render("  · " + shortDir(t.dir))
	}
	body := t.vp.View()
	if len(t.lines) == 0 && t.partial == "" {
		hint := []string{
			ui.Faint.Render("Команды для git — с родными цветами, как в обычном терминале:"),
			"  " + sKey.Render("git status") + ui.Faint.Render("   ") + sKey.Render("git pull") + ui.Faint.Render("   ") +
				sKey.Render("git log --oneline -10") + ui.Faint.Render("   ") + sKey.Render("git switch -c feat/x"),
			"",
			ui.Faint.Render("↑↓ — история, ctrl+c — остановить, cd — сменить папку, esc — назад к вкладкам"),
		}
		if !t.clone && t.repo != "" {
			hint = append(hint, "", ui.Warn.Render("клона нет: ")+sKey.Render("git clone https://github.com/"+t.repo))
		}
		body = strings.Join(hint, "\n")
	}
	last := t.prompt() + t.in.View()
	switch {
	case t.running():
		last = ui.Faint.Render("stdin › ") + t.in.View()
	case !t.focused:
		last = t.prompt() + ui.Faint.Render("enter — писать команду")
	}
	return termPanel(title, w, h, t.focused, body, last)
}

func shortDir(dir string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(dir, home) {
		return "~" + strings.TrimPrefix(dir, home)
	}
	return dir
}

// ── демон ──────────────────────────────────────────────────────────────────

func newDaemonTerm() term {
	t := newTerm(termDaemon, "")
	t.format = colorLog // level=ERROR красным, длинное время — коротко, как во вкладке Logs
	return t
}

// toggleDaemon — d на Dashboard: запустить gw daemon прямо здесь или остановить свой.
func (m *Model) toggleDaemon() tea.Cmd {
	switch {
	case m.daemon2.running():
		m.confirm = &confirm{
			question: "Остановить демона? Лента и уведомления на телефон встанут, пока не запустишь снова.",
			onYes: func(m *Model) tea.Cmd {
				m.stopDaemon()
				m.setFlash("останавливаю демона…", false)
				return nil
			},
		}
		return nil
	case m.daemonUp:
		m.setFlash("демон уже работает в другом терминале — его логи здесь; остановить: ctrl+c там", false)
		return nil
	}
	line := m.daemonCmd
	if line == "" {
		exe, err := os.Executable()
		if err != nil {
			m.setFlash("не нашёл, где лежит gw: "+err.Error(), true)
			return nil
		}
		line = shellQuote(exe) + " daemon"
	}
	m.daemon2.lines = nil
	m.setFlash("демон запускается — через пару секунд появится лента", false)
	return m.daemon2.run(line)
}

// stopDaemon — SIGINT своему демону (он сохраняет ленту и выходит). Чужой не трогаем.
func (m *Model) stopDaemon() {
	if m.daemon2.running() {
		m.daemon2.interrupt()
	}
}

// quit — выход; если из TUI запущен демон, сначала спросим: он остановится вместе с gw.
func (m *Model) quit() tea.Cmd {
	if !m.daemon2.running() {
		return tea.Quit
	}
	m.confirm = &confirm{
		question: "Демон запущен из gw и остановится вместе с ним. Выйти?",
		onYes:    func(m *Model) tea.Cmd { return tea.Quit },
	}
	return nil
}

func (m *Model) daemonPanel(w, h int) string {
	t := &m.daemon2
	ours := t.running()
	title := ui.Title.Render("демон")
	var body, last string
	switch {
	case ours:
		title += "  " + ui.Good.Render("● работает здесь")
		if m.daemonUp {
			title += ui.Faint.Render(" · событий " + strconv.Itoa(m.daemonStatus.Saved) + " · отправлено " + strconv.Itoa(m.daemonStatus.Sent))
		}
		body = t.vp.View()
		last = keyHint("d", "остановить") + "  " + ui.Faint.Render("живёт, пока открыт gw")
	case m.daemonUp:
		title += "  " + ui.Good.Render("● работает отдельно")
		logs := colorLog(m.daemonLogs)
		if n := h - 4; len(logs) > n {
			logs = logs[len(logs)-n:]
		}
		body = strings.Join(logs, "\n")
		last = ui.Faint.Render("логи демона из другого терминала · вкладка 7 — целиком")
	default:
		title += "  " + ui.Faint.Render("○ не запущен")
		lines := []string{
			ui.Faint.Render("Следит за GitHub: лента событий, логи CI, уведомления на телефон."),
			"",
			sKey.Render("d") + " — запустить здесь " + ui.Faint.Render("(работает, пока открыт gw)"),
			ui.Faint.Render("постоянно — в отдельном терминале: ") + sKey.Render("gw daemon"),
		}
		if m.cfg.TelegramChat == "" {
			lines = append(lines, "", ui.Faint.Render("на телефон: ")+sKey.Render("gw telegram <bot-token>"))
		}
		if len(t.lines) > 0 { // прошлый запуск закончился — покажем, чем
			lines = append(lines, "", ui.Faint.Render("прошлый запуск:"))
			tail := t.lines[max(len(t.lines)-(h-12), 0):]
			lines = append(lines, colorLog(tail)...)
		}
		body = strings.Join(lines, "\n")
		last = keyHint("d", "запустить")
	}
	return termPanel(title, w, h, false, body, last)
}

// shellQuote — путь в одинарных кавычках для sh -c.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
