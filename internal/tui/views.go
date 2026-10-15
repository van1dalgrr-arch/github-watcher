package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
	"github.com/van1dalgrr-arch/github-watcher/internal/ui"
)

func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "gw · " + m.repo
	return v
}

func (m *Model) render() string {
	if m.w == 0 {
		return ""
	}
	var body string
	switch {
	case m.help:
		body = m.helpView()
	case m.viewer != nil:
		body = ui.Bold.Render(fit(m.viewer.title, m.w)) + "\n" + m.viewer.vp.View()
	default:
		body = m.tabView()
	}
	body = lipgloss.NewStyle().Height(m.mainHeight()).MaxHeight(m.mainHeight()).Render(body)
	if m.term.open {
		body += "\n" + m.termView()
	}

	// модальные окна рисуем поверх тела по центру
	if m.confirm != nil || m.prompt != nil {
		body = lipgloss.Place(m.w, m.bodyHeight(), lipgloss.Center, lipgloss.Center, m.modalView())
	}
	return strings.Join([]string{m.headerView(), m.tabsView(), "", body, m.footerView()}, "\n")
}

func (m *Model) headerView() string {
	left := " " + wordmark() + ui.Faint.Render("  ·  ")
	if m.repo != "" {
		left += ui.Bold.Render(m.repo)
	} else {
		left += ui.Faint.Render("репозиторий не выбран")
	}
	var right []string
	if m.me != "" {
		right = append(right, ui.Faint.Render("@"+m.me))
	}
	if m.daemonUp {
		d := ui.Good.Render("● daemon")
		if m.daemonStatus.LastError != "" {
			d = ui.Warn.Render("● daemon: ошибка")
		}
		right = append(right, d)
	} else {
		right = append(right, ui.Faint.Render("○ daemon off"))
	}
	r := strings.Join(right, "  ")
	gap := max(m.w-lipgloss.Width(left)-lipgloss.Width(r), 1)
	return ansi.Truncate(left+strings.Repeat(" ", gap)+r, m.w, "")
}

func (m *Model) tabsView() string {
	var parts []string
	for i := range tabCount {
		label := ui.Faint.Render(strconv.Itoa(int(i)+1)) + " "
		if i == m.tab {
			label += sTabOn.Render(tabNames[i])
		} else {
			label += sTabOff.Render(tabNames[i])
		}
		if count := m.badge(i); count != "" { // отдельно: подчёркивание активной вкладки ломает вложенные цвета
			label += " " + count
		}
		parts = append(parts, label)
	}
	return ansi.Truncate(" "+strings.Join(parts, "   "), m.w, "")
}

// badge — счётчик рядом с названием вкладки.
func (m *Model) badge(t tab) string {
	n := 0
	switch t {
	case tabPRs:
		n = len(m.prs)
	case tabIssues:
		n = len(m.issues)
	case tabActions:
		for _, r := range m.runs {
			if r.Status != "completed" {
				n++
			}
		}
		if n > 0 {
			return ui.Warn.Render(strconv.Itoa(n))
		}
		return ""
	default:
		return ""
	}
	if n == 0 {
		return ""
	}
	return ui.Faint.Render(strconv.Itoa(n))
}

func (m *Model) footerView() string {
	var left string
	switch {
	case m.filtering:
		left = sKey.Render("/") + " " + m.filterIn.View()
	case m.loading > 0:
		left = m.spin.View() + " " + ui.Faint.Render("загрузка…")
		if m.flash != "" && !m.flashBad {
			left = m.spin.View() + " " + ui.Faint.Render(m.flash)
		}
	case m.flash != "":
		if m.flashBad {
			left = ui.Bad.Render(m.flash)
		} else {
			left = ui.Good.Render(m.flash)
		}
	case m.filters[m.tab] != "":
		left = ui.Faint.Render("фильтр: ") + ui.Info.Render(m.filters[m.tab]) + ui.Faint.Render("  (esc — сбросить)")
	}
	right := strings.Join(m.hints(), "  ")
	if lipgloss.Width(left)+lipgloss.Width(right)+2 > m.w {
		right = keyHint("?", "помощь")
	}
	gap := max(m.w-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return ansi.Truncate(left+strings.Repeat(" ", gap)+right, m.w, "")
}

func (m *Model) hints() []string {
	if t := m.focusedTerm(); t != nil {
		esc := keyHint("esc", "скрыть")
		if t.id != termBottom {
			esc = keyHint("esc", "к вкладкам")
		}
		return []string{keyHint("enter", "выполнить"), keyHint("↑↓", "история"), keyHint("ctrl+c", "стоп"),
			keyHint("pgup/pgdn", "листать"), esc}
	}
	if m.viewer != nil {
		h := []string{keyHint("↑↓", "листать"), keyHint("o", "браузер"), keyHint("esc", "назад")}
		if len(m.viewer.errs) > 0 {
			h = append([]string{keyHint("e/E", "ошибки")}, h...)
		}
		return h
	}
	var h []string
	switch m.tab {
	case tabDash:
		d := "демон"
		if m.daemon2.running() {
			d = "стоп демона"
		}
		h = []string{keyHint("enter", "git"), keyHint("d", d)}
	case tabRepos:
		h = []string{keyHint("enter", "выбрать"), keyHint("o", "браузер")}
	case tabPRs:
		h = []string{keyHint("enter", "открыть"), keyHint("m", "merge"), keyHint("c", "коммент"), keyHint("s", m.prState)}
	case tabIssues:
		h = []string{keyHint("n", "новый"), keyHint("x", "закрыть"), keyHint("c", "коммент"), keyHint("s", m.issueState)}
	case tabActions:
		h = []string{keyHint("enter", "логи"), keyHint("R", "перезапуск"), keyHint("f", "упавшие")}
	case tabEvents:
		h = []string{keyHint("enter", "подробности"), keyHint("o", "браузер"), keyHint("f", "к репо")}
	case tabCode:
		h = []string{keyHint("enter", "открыть"), keyHint("/", "найти файл"), keyHint("o", "браузер")}
	}
	return append(h, keyHint(":", "терминал"), keyHint("?", "помощь"), keyHint("q", "выход"))
}

func (m *Model) modalView() string {
	w := min(m.w-4, 72)
	if m.confirm != nil {
		q := lipgloss.NewStyle().Width(w - 6).Render(m.confirm.question)
		return sModal.Width(w).Render(q + "\n\n" + keyHint("y", "да") + "   " + keyHint("n", "нет"))
	}
	m.prompt.in.SetWidth(w - 6)
	return sModal.Width(w).Render(ui.Title.Render(m.prompt.title) + "\n\n" + m.prompt.in.View() +
		"\n\n" + keyHint("enter", "готово") + "   " + keyHint("esc", "отмена"))
}

func (m *Model) helpView() string {
	sec := func(title string, rows ...[2]string) string {
		out := []string{ui.Title.Render(title)}
		for _, r := range rows {
			out = append(out, "  "+fit(sKey.Render(r[0]), 12)+r[1])
		}
		return strings.Join(out, "\n")
	}
	cols := []string{
		sec("Везде",
			[2]string{"1…8", "вкладки"}, [2]string{"tab / h l", "соседняя вкладка"},
			[2]string{"↑↓ / j k", "по списку"}, [2]string{"g / G", "в начало / конец"},
			[2]string{"/", "фильтр"}, [2]string{"r", "обновить"}, [2]string{"o", "открыть в браузере"},
			[2]string{":", "терминал"},
			[2]string{"q", "выход"}),
		sec("Dashboard",
			[2]string{"enter / i", "терминал git"}, [2]string{"d", "запустить / остановить демона"},
			[2]string{"esc", "из терминала к вкладкам"}),
		sec("PR и issues",
			[2]string{"enter", "подробности"}, [2]string{"m", "смёржить PR"}, [2]string{"c", "комментарий"},
			[2]string{"n", "новый issue"}, [2]string{"x", "закрыть issue"}, [2]string{"s", "open / closed / all"}),
		sec("Actions и лента",
			[2]string{"enter", "логи запуска"}, [2]string{"e / E", "к след./пред. ошибке"},
			[2]string{"R", "перезапустить"}, [2]string{"f", "только упавшие"},
			[2]string{"f (Events)", "репо события"},
			[2]string{"8 → /", "найти файл в коде"}),
	}
	for i := range cols {
		cols[i] = lipgloss.NewStyle().Width(34).Render(cols[i])
	}
	grid := lipgloss.JoinHorizontal(lipgloss.Top, cols...)
	if lipgloss.Width(grid) > m.w {
		grid = lipgloss.JoinVertical(lipgloss.Left, cols...)
	}
	return grid + "\n\n" + ui.Faint.Render("  любая клавиша — закрыть")
}

// ── вкладки ────────────────────────────────────────────────────────────────

func (m *Model) tabView() string {
	switch m.tab {
	case tabDash:
		return m.dashView()
	case tabLogs:
		if !m.daemonUp {
			return m.daemonHint()
		}
		return m.logsVP.View()
	}
	if m.repo == "" && m.tab != tabRepos && m.tab != tabEvents {
		return ui.Faint.Render("  выбери репозиторий на вкладке 2")
	}
	if m.tab == tabCode {
		return m.codeView()
	}

	vis := m.visible(m.tab)
	h := m.listHeight()
	l := &m.lists[m.tab]
	title := m.listTitle(len(vis))
	if len(vis) == 0 {
		empty := "  пусто"
		if m.loading > 0 {
			empty = "  " + m.spin.View() + " загружаю…"
		}
		return title + "\n" + ui.Faint.Render(empty)
	}
	return title + "\n" + l.render(len(vis), h, func(i int, sel bool) string {
		return m.rowView(vis[i], sel)
	})
}

func (m *Model) listTitle(n int) string {
	var t string
	switch m.tab {
	case tabRepos:
		t = fmt.Sprintf("Репозитории · %d", n)
	case tabPRs:
		t = fmt.Sprintf("Pull requests · %s · %d", m.prState, n)
	case tabIssues:
		t = fmt.Sprintf("Issues · %s · %d", m.issueState, n)
	case tabActions:
		t = fmt.Sprintf("Запуски Actions · %d", n)
	case tabEvents:
		t = fmt.Sprintf("Лента событий · %d", n)
		if !m.daemonUp {
			t += ui.Faint.Render("  · напрямую из GitHub, обновляется раз в минуту")
		}
	}
	if l := m.lists[m.tab]; n > m.listHeight() {
		t += ui.Faint.Render(fmt.Sprintf("  %d/%d", l.cursor+1, n))
	}
	return " " + ui.Title.Render(t)
}

// visible — индексы строк вкладки, прошедших фильтр.
func (m *Model) visible(t tab) []int {
	var texts []string
	switch t {
	case tabRepos:
		for _, r := range m.repos {
			texts = append(texts, r.FullName()+" "+r.Language+" "+r.Description)
		}
	case tabPRs:
		for _, p := range m.prs {
			texts = append(texts, fmt.Sprintf("#%d %s %s %s", p.Number, p.Title, p.Author, p.Head))
		}
	case tabIssues:
		for _, i := range m.issues {
			texts = append(texts, fmt.Sprintf("#%d %s %s %s", i.Number, i.Title, i.Author, strings.Join(i.Labels, " ")))
		}
	case tabActions:
		for _, r := range m.runs {
			texts = append(texts, r.Name+" "+r.Title+" "+r.Branch+" "+r.Event+" "+r.State())
		}
	case tabEvents:
		for _, e := range m.events {
			texts = append(texts, e.Repo+" "+e.Title+" "+e.Actor+" "+string(e.Kind))
		}
	case tabCode:
		texts = m.code.files
	}
	f := strings.ToLower(m.filters[t])
	out := make([]int, 0, len(texts))
	for i, s := range texts {
		if f == "" || strings.Contains(strings.ToLower(s), f) {
			out = append(out, i)
		}
	}
	return out
}

func (m *Model) rowView(i int, sel bool) string {
	w := m.w
	switch m.tab {
	case tabRepos:
		r := m.repos[i]
		name := r.FullName()
		if r.Owner == m.me {
			name = r.Name // свои — без владельца, чужие (организации, коллаборации) — полностью
		}
		if r.FullName() == m.repo {
			name = ui.Title.Render(name)
		}
		vis := ui.Faint.Render("○")
		if r.Private {
			vis = ui.Warn.Render("◉")
		}
		if r.Archived {
			vis = ui.Faint.Render("▣")
		}
		return row(sel, w, c(vis, 1), c(name, 34), c(ui.Info.Render(r.Language), 11),
			c(ui.Warn.Render("★")+strconv.Itoa(r.Stars), 5), c(ui.Faint.Render(ui.Ago(r.PushedAt)), 5),
			flex(ui.Faint.Render(r.Description)))
	case tabPRs:
		p := m.prs[i]
		ci := ui.CI(p.CIStatus)
		if p.CIStatus == "pending" {
			ci = m.spin.View()
		}
		return row(sel, w, c(ui.PRState(p), 1), c(ui.Faint.Render("#"+strconv.Itoa(p.Number)), 5), c(ci, 1),
			flex(p.Title), c(ui.Info.Render(p.Head)+ui.Faint.Render(" → "+p.Base), 28),
			c(ui.Faint.Render("@"+p.Author), 16), c(ui.Faint.Render(ui.Ago(p.UpdatedAt)), 5))
	case tabIssues:
		is := m.issues[i]
		comments := ""
		if is.Comments > 0 {
			comments = ui.Faint.Render("✎" + strconv.Itoa(is.Comments))
		}
		return row(sel, w, c(ui.IssueState(is), 1), c(ui.Faint.Render("#"+strconv.Itoa(is.Number)), 5),
			flex(is.Title), c(ui.Info.Render(strings.Join(is.Labels, " ")), 18), c(comments, 4),
			c(ui.Faint.Render("@"+is.Author), 16), c(ui.Faint.Render(ui.Ago(is.UpdatedAt)), 5))
	case tabActions:
		r := m.runs[i]
		st := ui.CI(r.State())
		if r.State() == "pending" {
			st = m.spin.View()
		}
		return row(sel, w, c(st, 1), c(ui.Bold.Render(r.Name), 20), flex(r.Title),
			c(ui.Info.Render(r.Branch), 18), c(ui.Faint.Render(r.Event), 12), c(ui.Faint.Render(ui.Ago(r.UpdatedAt)), 5))
	case tabEvents:
		e := m.events[i]
		actor := ""
		if e.Actor != "" {
			actor = ui.Faint.Render("@" + e.Actor)
		}
		return row(sel, w, c(ui.Faint.Render(ui.Ago(e.CreatedAt)), 6), c(ui.Kind(e.Kind), 1),
			c(ui.Bold.Render(e.Repo), 30), flex(e.Title), c(actor, 16))
	}
	return ""
}

func (m *Model) daemonHint() string {
	return "\n" + lipgloss.NewStyle().PaddingLeft(2).Render(strings.Join([]string{
		ui.Title.Render("Демон не запущен"),
		"",
		"Лента событий, логи и уведомления на телефон работают через фоновый сервис.",
		"Запусти в соседнем терминале:",
		"",
		"  " + sKey.Render("gw daemon"),
		"",
		ui.Faint.Render("Для уведомлений в Telegram: ") + sKey.Render("gw telegram <bot-token>"),
	}, "\n"))
}

// ── Dashboard ──────────────────────────────────────────────────────────────

func (m *Model) profilePanel(w, h int) string {
	if m.repos == nil {
		return panel("Профиль", w, h, []string{ui.Faint.Render(m.spin.View() + " загружаю…")})
	}
	var stars, private, archived int
	langs := map[string]int{}
	for _, r := range m.repos {
		stars += r.Stars
		if r.Private {
			private++
		}
		if r.Archived {
			archived++
		}
		if r.Language != "" {
			langs[r.Language]++
		}
	}
	type kv struct {
		k string
		v int
	}
	var top []kv
	for k, v := range langs {
		top = append(top, kv{k, v})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].v > top[j].v || top[i].v == top[j].v && top[i].k < top[j].k })

	lines := []string{
		ui.Bold.Render("@" + m.me),
		"",
		ui.Bold.Render(strconv.Itoa(len(m.repos))) + " " + ui.Plural(len(m.repos), "репозиторий", "репозитория", "репозиториев") +
			ui.Faint.Render(" · ") + ui.Warn.Render(strconv.Itoa(private)) + " " + ui.Plural(private, "приватный", "приватных", "приватных") +
			ui.Faint.Render(" · ") + ui.Warn.Render("★"+strconv.Itoa(stars)),
	}
	if len(top) > 0 && h-2 >= len(lines)+4 { // языки — только если влезают хотя бы пара строк
		lines = append(lines, "", ui.Faint.Render("Языки"))
		total := 0
		for _, t := range top {
			total += t.v
		}
		barW := max(w-24, 6)
		for _, t := range top[:min(len(top), 4)] {
			n := max(t.v*barW/total, 1)
			lines = append(lines, fit(t.k, 12)+ui.Info.Render(strings.Repeat("━", n))+ui.Faint.Render(" "+strconv.Itoa(t.v)))
		}
	}
	return panel("Профиль", w, h, lines)
}

func (m *Model) ciPanel(w, h int) string {
	if m.dashRuns == nil {
		return panel("CI · последние запуски", w, h, []string{ui.Faint.Render(m.spin.View() + " загружаю…")})
	}
	if len(m.dashRuns) == 0 {
		return panel("CI · последние запуски", w, h, []string{ui.Faint.Render("в недавних репозиториях нет Actions")})
	}
	inner := w - 4
	var lines []string
	for _, r := range m.dashRuns {
		st := ui.CI(r.State())
		if r.State() == "pending" {
			st = m.spin.View()
		}
		name := r.Repo[strings.Index(r.Repo, "/")+1:]
		lines = append(lines, st+" "+fit(ui.Bold.Render(name), 20)+" "+
			fit(ui.Faint.Render(r.Name+" · "+r.Branch), max(inner-34, 8))+" "+fit(ui.Faint.Render(ui.Ago(r.UpdatedAt)), 6))
	}
	return panel("CI · последние запуски", w, h, lines)
}

// pushPanel — последний пуш: что за коммиты и сколько строк поменялось.
func (m *Model) pushPanel(w, h int) string {
	var push *domain.Event
	for i := range m.events {
		if m.events[i].Kind == domain.EventPush {
			push = &m.events[i]
			break
		}
	}
	switch {
	case push == nil && m.events == nil:
		return panel("Последний пуш", w, h, []string{ui.Faint.Render(m.spin.View() + " загружаю ленту…")})
	case push == nil:
		return panel("Последний пуш", w, h, []string{ui.Faint.Render("за неделю пушей не было")})
	}
	lines := []string{
		ui.Bold.Render(push.Repo) + ui.Faint.Render(" · "+ui.Ago(push.CreatedAt)+" назад"),
		ui.Info.Render(push.Title),
		"",
	}
	lines = append(lines, colorBody(push.Body)...)
	return panel("Последний пуш", w, h, lines)
}

func (m *Model) feedPanel(w, h int) string {
	if !m.daemonUp {
		return panel("Лента", w, h, []string{ui.Faint.Render("появится, когда запустишь gw daemon")})
	}
	if len(m.events) == 0 {
		return panel("Лента", w, h, []string{ui.Faint.Render("пока тихо — новые события появятся здесь")})
	}
	inner := w - 4
	var lines []string
	for _, e := range m.events[:min(len(m.events), h-3)] {
		repo := e.Repo[strings.Index(e.Repo, "/")+1:]
		lines = append(lines, fit(ui.Faint.Render(ui.Ago(e.CreatedAt)), 6)+ui.Kind(e.Kind)+" "+
			fit(ui.Bold.Render(repo), 18)+" "+fit(e.Title, max(inner-27, 8)))
	}
	return panel("Лента", w, h, lines)
}

// ── подробности ────────────────────────────────────────────────────────────

func (m *Model) openPR(p domain.PullRequest) {
	meta := []string{
		ui.PRState(p) + " " + ui.Bold.Render(p.State) + ui.Faint.Render("  @"+p.Author+" · обновлён "+ui.Ago(p.UpdatedAt)+" назад"),
		ui.Info.Render(p.Head) + ui.Faint.Render(" → ") + ui.Info.Render(p.Base) + "   CI " + ui.CI(p.CIStatus) + " " + ui.Faint.Render(p.CIStatus),
		ui.Faint.Render(p.URL),
		"",
	}
	m.viewer = m.newViewer(fmt.Sprintf("#%d %s", p.Number, p.Title), p.URL, append(meta, m.wrapBody(p.Body)...))
}

func (m *Model) openIssue(i domain.Issue) {
	labels := ""
	for _, l := range i.Labels {
		labels += sBadge.Render(l) + " "
	}
	meta := []string{
		ui.IssueState(i) + " " + ui.Bold.Render(i.State) + ui.Faint.Render(fmt.Sprintf("  @%s · комментариев: %d · обновлён %s назад", i.Author, i.Comments, ui.Ago(i.UpdatedAt))),
		labels,
		ui.Faint.Render(i.URL),
		"",
	}
	m.viewer = m.newViewer(fmt.Sprintf("#%d %s", i.Number, i.Title), i.URL, append(meta, m.wrapBody(i.Body)...))
}

func (m *Model) wrapBody(body string) []string {
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	if body == "" {
		return []string{ui.Faint.Render("(без описания)")}
	}
	return strings.Split(ansi.Wordwrap(body, max(m.w-2, 20), ""), "\n")
}

// colorBody раскрашивает подробности пуша: sha коммитов акцентом, +строки зелёным, −строки красным.
func colorBody(body string) []string {
	var out []string
	for l := range strings.Lines(body) {
		l = strings.TrimRight(l, "\n")
		switch {
		case strings.HasPrefix(l, "• ") && len(l) > 10:
			sha, msg, _ := strings.Cut(strings.TrimPrefix(l, "• "), " ")
			out = append(out, ui.Faint.Render("• ")+sKey.Render(sha)+" "+msg)
		case strings.HasPrefix(l, "+"):
			counts, name, _ := strings.Cut(l, "  ")
			add, del, _ := strings.Cut(counts, " ")
			out = append(out, fit(ui.Good.Render(add)+" "+ui.Bad.Render(del), 12)+name)
		default:
			out = append(out, ui.Faint.Render(l))
		}
	}
	return out
}

// colorLog подсвечивает уровни в строках slog: level=ERROR красным, WARN жёлтым.
// Длинный time=2026-10-11T18:37:13.715+03:00 сокращаем до 18:37:13.
func colorLog(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		prefix := ""
		if rest, ok := strings.CutPrefix(l, "time="); ok && len(rest) > 19 && rest[10] == 'T' {
			if _, tail, ok := strings.Cut(rest, " "); ok {
				prefix, l = rest[11:19]+" ", tail
			}
		}
		switch {
		case strings.Contains(l, "level=ERROR"):
			out[i] = ui.Faint.Render(prefix) + ui.Bad.Render(l)
		case strings.Contains(l, "level=WARN"):
			out[i] = ui.Faint.Render(prefix) + ui.Warn.Render(l)
		default:
			out[i] = ui.Faint.Render(prefix) + l
		}
	}
	return out
}

// termView — панель-терминал: заголовок, вывод, строка ввода.
func (m *Model) termView() string {
	t := &m.term
	title := ui.Title.Render("терминал")
	if t.running() {
		title += "  " + m.spin.View() + ui.Faint.Render(" "+t.history[len(t.history)-1])
	}
	input := t.prompt() + t.in.View()
	if t.running() {
		input = ui.Faint.Render("stdin › ") + t.in.View()
	}
	style := sPanel
	if t.focused {
		style = style.BorderForeground(ui.Accent)
	}
	return style.Width(m.w).Height(m.termHeight()).MaxHeight(m.termHeight()).
		Render(title + "\n" + t.vp.View() + "\n" + input)
}
