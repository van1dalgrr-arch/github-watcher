package tui

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
	"github.com/van1dalgrr-arch/github-watcher/internal/ui"
)

// Страницы PR, issue, репозитория и коммита — содержимое и действия.

// ── PR ─────────────────────────────────────────────────────────────────────

func (m *Model) openPRPage(repo string, number int, title string) tea.Cmd {
	return m.openPage(fmt.Sprintf("%s · #%d %s", repo, number, title), fmt.Sprintf("https://github.com/%s/pull/%d", repo, number),
		func(ctx context.Context) (func(p *page), error) {
			d, err := m.gh.PR(ctx, repo, number)
			if err != nil {
				return nil, err
			}
			return func(p *page) { m.fillPR(p, d) }, nil
		})
}

func (m *Model) fillPR(p *page, d domain.PRDetail) {
	p.title = fmt.Sprintf("%s · #%d %s", d.Repo, d.Number, d.Title)
	p.url = d.URL
	p.build = func(p *page, w int) {
		state := ui.PRState(d.PullRequest) + " " + ui.Bold.Render(prStateText(d))
		p.header = []string{
			state + ui.Faint.Render(fmt.Sprintf("  @%s хочет влить %d %s из ", d.Author, len(d.Commits),
				ui.Plural(len(d.Commits), "коммит", "коммита", "коммитов"))) +
				ui.Info.Render(d.Head) + ui.Faint.Render(" в ") + ui.Info.Render(d.Base) +
				ui.Faint.Render(" · обновлён "+ui.Ago(d.UpdatedAt)+" назад"),
			ui.Good.Render("+"+strconv.Itoa(d.Additions)) + " " + ui.Bad.Render("−"+strconv.Itoa(d.Deletions)) +
				ui.Faint.Render(fmt.Sprintf(" · %d %s · ", d.ChangedFiles, ui.Plural(d.ChangedFiles, "файл", "файла", "файлов"))) +
				"проверки " + ui.CI(d.CIStatus) + "  " + mergeableText(d) + labelBadges(d.Labels) + people("ревью ждут", d.Reviewers),
		}
		p.sections = []*section{
			{name: "Обсуждение", badge: count(len(d.Comments)), doc: conversation(d.Author, d.Body, d.UpdatedAt, d.Comments, w)},
			{name: "Коммиты", badge: count(len(d.Commits)), items: m.commitItems(d.Repo, d.Commits)},
			{name: "Файлы", badge: count(len(d.Files)), doc: filesDoc(d.Files, w)},
			{name: "Проверки", badge: ui.CI(d.CIStatus), items: checkItems(d.Checks)},
		}
	}
	open := d.State == "open"
	p.keys = []pageKey{
		{"a", "approve", func(m *Model) tea.Cmd {
			m.confirm = &confirm{question: fmt.Sprintf("Одобрить PR #%d «%s»?", d.Number, d.Title), onYes: func(m *Model) tea.Cmd {
				return m.actPage(p, "PR одобрен", func(ctx context.Context) error { return m.gh.Review(ctx, d.Repo, d.Number, "APPROVE", "") })
			}}
			return nil
		}},
		{"R", "просить правки", func(m *Model) tea.Cmd {
			return m.ask("Что поправить в #"+strconv.Itoa(d.Number)+" (request changes)", "что не так и как исправить", func(m *Model, v string) tea.Cmd {
				return m.actPage(p, "попросил правки", func(ctx context.Context) error {
					return m.gh.Review(ctx, d.Repo, d.Number, "REQUEST_CHANGES", v)
				})
			})
		}},
		{"c", "коммент", func(m *Model) tea.Cmd {
			return m.ask("Комментарий к #"+strconv.Itoa(d.Number), "текст, Enter — отправить", func(m *Model, v string) tea.Cmd {
				return m.actPage(p, "комментарий отправлен", func(ctx context.Context) error { return m.gh.Comment(ctx, d.Repo, d.Number, v) })
			})
		}},
		{"m", "merge", func(m *Model) tea.Cmd {
			if !open {
				m.setFlash("PR уже не открыт", true)
				return nil
			}
			q := fmt.Sprintf("Смёржить #%d «%s» в %s (squash)?", d.Number, d.Title, d.Base)
			if d.Mergeable == "dirty" {
				q = "В PR конфликты — GitHub не даст смёржить. Всё равно попробовать?"
			}
			m.confirm = &confirm{question: q, onYes: func(m *Model) tea.Cmd {
				return m.actPage(p, fmt.Sprintf("PR #%d смёржен", d.Number), func(ctx context.Context) error {
					return m.gh.MergePR(ctx, d.Repo, d.Number, "squash")
				})
			}}
			return nil
		}},
		{"C", "checkout", func(m *Model) tea.Cmd { return m.checkoutPR(d.Repo, d.Number) }},
	}
}

func prStateText(d domain.PRDetail) string {
	switch {
	case d.State == "merged":
		return "смёржен"
	case d.State == "closed":
		return "закрыт"
	case d.Draft:
		return "черновик"
	}
	return "открыт"
}

func mergeableText(d domain.PRDetail) string {
	if d.State != "open" {
		return ""
	}
	switch d.Mergeable {
	case "clean":
		return ui.Good.Render("можно мёржить")
	case "dirty":
		return ui.Bad.Render("конфликты с " + d.Base)
	case "blocked":
		return ui.Warn.Render("ждёт ревью или проверок")
	case "behind":
		return ui.Warn.Render("отстал от " + d.Base)
	case "unstable":
		return ui.Warn.Render("проверки не прошли")
	case "draft":
		return ui.Faint.Render("черновик")
	}
	return ""
}

// checkoutPR — PR к себе: ветка pr-N в клоне репозитория, команда видна в терминале внизу.
func (m *Model) checkoutPR(repo string, number int) tea.Cmd {
	dir := m.clone(repo)
	if dir == "" {
		m.setFlash("клона "+repo+" не нашёл — сначала git clone (терминал git на Dashboard)", true)
		return nil
	}
	line := fmt.Sprintf("git fetch origin pull/%d/head:pr-%d && git switch pr-%d", number, number, number)
	m.term.dir, m.term.open, m.term.focused = dir, true, true
	m.layout()
	return tea.Batch(m.term.in.Focus(), m.term.run(line))
}

// conversation — обсуждение: описание автора, дальше комментарии, ревью и замечания к строкам по времени.
func conversation(author, body string, at time.Time, comments []domain.Comment, w int) []string {
	out := []string{commentHead(ui.Bold.Render("@"+author), "открыл(а)", at)}
	out = append(out, indent(markdown(body, w-2))...)
	for _, c := range comments {
		out = append(out, "", ui.Faint.Render(strings.Repeat("─", min(w-2, 80))))
		switch c.Kind {
		case domain.CommentReview:
			icon, what := ui.Faint.Render("◆"), "оставил(а) ревью"
			switch c.State {
			case "APPROVED":
				icon, what = ui.Good.Render("✓"), ui.Good.Render("одобрил(а)")
			case "CHANGES_REQUESTED":
				icon, what = ui.Bad.Render("✗"), ui.Bad.Render("просит правки")
			case "DISMISSED":
				what = "ревью снято"
			}
			out = append(out, commentHead(icon+" "+ui.Bold.Render("@"+c.Author), what, c.CreatedAt))
			if strings.TrimSpace(c.Body) == "" {
				continue
			}
		case domain.CommentLine:
			out = append(out, commentHead(ui.Bold.Render("@"+c.Author), "к "+ui.Info.Render(fmt.Sprintf("%s:%d", c.Path, c.Line)), c.CreatedAt))
			if hunk := strings.Split(strings.TrimSpace(c.DiffHunk), "\n"); len(hunk) > 1 {
				tail := hunk[max(len(hunk)-4, 1):] // строка, к которой замечание, и пара над ней
				out = append(out, indent(patchLines(c.Path, hunk[0]+"\n"+strings.Join(tail, "\n"), w-2))...)
			}
		default:
			out = append(out, commentHead(ui.Bold.Render("@"+c.Author), "", c.CreatedAt))
		}
		out = append(out, indent(markdown(c.Body, w-2))...)
	}
	return out
}

func commentHead(who, what string, at time.Time) string {
	s := who
	if what != "" {
		s += " " + what
	}
	if !at.IsZero() {
		s += ui.Faint.Render(" · " + ui.Ago(at) + " назад")
	}
	return s
}

func indent(lines []string) []string {
	for i := range lines {
		lines[i] = "  " + lines[i]
	}
	return lines
}

func count(n int) string {
	if n == 0 {
		return ""
	}
	return ui.Faint.Render(strconv.Itoa(n))
}

func labelBadges(labels []string) string {
	s := ""
	for _, l := range labels {
		s += "  " + sBadge.Render(l)
	}
	return s
}

func people(what string, logins []string) string {
	if len(logins) == 0 {
		return ""
	}
	return ui.Faint.Render("  · "+what+": ") + "@" + strings.Join(logins, " @")
}

// filesDoc — все изменённые файлы подряд, как вкладка Files changed.
func filesDoc(files []domain.FileDiff, w int) []string {
	if len(files) == 0 {
		return []string{ui.Faint.Render("  файлов нет")}
	}
	var out []string
	for i, f := range files {
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, diffLines(f, w)...)
	}
	return out
}

func (m *Model) commitItems(repo string, commits []domain.Commit) []pageItem {
	items := make([]pageItem, 0, len(commits))
	for i := len(commits) - 1; i >= 0; i-- { // новые сверху, как в истории ветки
		cm := commits[i]
		items = append(items, pageItem{
			url: cm.URL,
			render: func(w int, sel bool) string {
				return row(sel, w, c(sKey.Render(cm.SHA[:min(7, len(cm.SHA))]), 7), flex(cm.Short()),
					c(ui.Faint.Render("@"+cm.Author), 16), c(ui.Faint.Render(ui.Ago(cm.Date)), 5))
			},
			open: func(m *Model) tea.Cmd { return m.openCommitPage(repo, cm.SHA, cm.Short()) },
		})
	}
	return items
}

func checkItems(checks []domain.Check) []pageItem {
	items := make([]pageItem, 0, len(checks))
	for _, ch := range checks {
		took := ""
		if ch.Took > 0 {
			took = ch.Took.Round(time.Second).String()
		}
		items = append(items, pageItem{
			url: ch.URL,
			render: func(w int, sel bool) string {
				return row(sel, w, c(ui.CI(ch.State()), 1), flex(ch.Name), c(ui.Faint.Render(ch.Conclusion), 12), c(ui.Faint.Render(took), 8))
			},
			open: func(m *Model) tea.Cmd { return openURL(ch.URL) },
		})
	}
	return items
}

// ── issue ──────────────────────────────────────────────────────────────────

func (m *Model) openIssuePage(repo string, number int, title string) tea.Cmd {
	return m.openPage(fmt.Sprintf("%s · #%d %s", repo, number, title), fmt.Sprintf("https://github.com/%s/issues/%d", repo, number),
		func(ctx context.Context) (func(p *page), error) {
			d, err := m.gh.Issue(ctx, repo, number)
			if err != nil {
				return nil, err
			}
			return func(p *page) { m.fillIssue(p, d) }, nil
		})
}

func (m *Model) fillIssue(p *page, d domain.IssueDetail) {
	p.title = fmt.Sprintf("%s · #%d %s", d.Repo, d.Number, d.Title)
	p.url = d.URL
	p.build = func(p *page, w int) {
		state := ui.IssueState(d.Issue) + " " + ui.Bold.Render(map[bool]string{true: "открыт", false: "закрыт"}[d.State == "open"])
		p.header = []string{
			state + ui.Faint.Render(fmt.Sprintf("  @%s · %d %s · обновлён %s назад", d.Author, len(d.Comments),
				ui.Plural(len(d.Comments), "комментарий", "комментария", "комментариев"), ui.Ago(d.UpdatedAt))) +
				labelBadges(d.Labels) + people("исполнители", d.Assignees),
		}
		p.sections = []*section{{name: "Обсуждение", badge: count(len(d.Comments)), doc: conversation(d.Author, d.Body, time.Time{}, d.Comments, w)}}
	}
	p.keys = []pageKey{
		{"c", "коммент", func(m *Model) tea.Cmd {
			return m.ask("Комментарий к #"+strconv.Itoa(d.Number), "текст, Enter — отправить", func(m *Model, v string) tea.Cmd {
				return m.actPage(p, "комментарий отправлен", func(ctx context.Context) error { return m.gh.Comment(ctx, d.Repo, d.Number, v) })
			})
		}},
	}
	if d.State == "open" {
		p.keys = append(p.keys, pageKey{"x", "закрыть", func(m *Model) tea.Cmd {
			m.confirm = &confirm{question: fmt.Sprintf("Закрыть issue #%d «%s»?", d.Number, d.Title), onYes: func(m *Model) tea.Cmd {
				return m.actPage(p, "issue закрыт", func(ctx context.Context) error { return m.gh.CloseIssue(ctx, d.Repo, d.Number) })
			}}
			return nil
		}})
	} else {
		p.keys = append(p.keys, pageKey{"x", "переоткрыть", func(m *Model) tea.Cmd {
			return m.actPage(p, "issue снова открыт", func(ctx context.Context) error { return m.gh.ReopenIssue(ctx, d.Repo, d.Number) })
		}})
	}
}

// ── репозиторий ────────────────────────────────────────────────────────────

func (m *Model) openRepoPage(repo string) tea.Cmd {
	return m.openPage(repo, "https://github.com/"+repo, func(ctx context.Context) (func(p *page), error) {
		d, err := m.gh.Repo(ctx, repo)
		if err != nil {
			return nil, err
		}
		return func(p *page) { m.fillRepo(p, d) }, nil
	})
}

func (m *Model) fillRepo(p *page, d domain.RepoDetail) {
	repo := d.FullName()
	p.build = func(p *page, w int) {
		var badges []string
		if d.Private {
			badges = append(badges, ui.Warn.Render("приватный"))
		}
		if d.Archived {
			badges = append(badges, ui.Faint.Render("архив"))
		}
		if d.Fork {
			badges = append(badges, ui.Faint.Render("форк"))
		}
		stats := []string{ui.Warn.Render("★ " + strconv.Itoa(d.Stars)), "⑂ " + strconv.Itoa(d.Forks), "◉ " + strconv.Itoa(d.Watchers)}
		if d.License != "" && d.License != "NOASSERTION" {
			stats = append(stats, d.License)
		}
		if d.LatestRelease != "" {
			stats = append(stats, ui.Good.Render("релиз "+d.LatestRelease))
		}
		stats = append(stats, ui.Faint.Render("ветка ")+ui.Info.Render(d.DefaultBranch), ui.Faint.Render("пуш "+ui.Ago(d.PushedAt)+" назад"))
		p.header = []string{
			strings.Join(append(badges, ui.Faint.Render(orDash(d.Description))), "  "),
			strings.Join(stats, ui.Faint.Render(" · ")),
		}
		if line := langLine(d.Languages); line != "" {
			p.header = append(p.header, line)
		}
		if len(d.Topics) > 0 || d.Homepage != "" {
			var t []string
			for _, topic := range d.Topics {
				t = append(t, ui.Info.Render("#"+topic))
			}
			if d.Homepage != "" {
				t = append(t, ui.Faint.Render(d.Homepage))
			}
			p.header = append(p.header, strings.Join(t, " "))
		}
		readme := markdown(d.Readme, w-2)
		if d.Readme == "" {
			readme = []string{ui.Faint.Render("  README нет")}
		}
		var branches []pageItem
		for _, b := range d.Branches {
			branches = append(branches, pageItem{
				url: "https://github.com/" + repo + "/tree/" + b,
				render: func(w int, sel bool) string {
					mark := "  "
					if b == d.DefaultBranch {
						mark = ui.Good.Render("● ")
					}
					return row(sel, w, flex(mark+ui.Info.Render(b)))
				},
			})
		}
		// коммиты ветки по умолчанию приходят новыми сверху — commitItems ждёт старые сверху (как в PR)
		commits := make([]domain.Commit, len(d.Commits))
		for i, c := range d.Commits {
			commits[len(d.Commits)-1-i] = c
		}
		p.sections = []*section{
			{name: "README", doc: readme},
			{name: "Коммиты", badge: count(len(d.Commits)), items: m.commitItems(repo, commits)},
			{name: "Ветки", badge: count(len(d.Branches)), items: branches},
		}
	}
	p.keys = []pageKey{
		{"p", "PR", func(m *Model) tea.Cmd { return m.goRepoTab(repo, tabPRs) }},
		{"i", "issues", func(m *Model) tea.Cmd { return m.goRepoTab(repo, tabIssues) }},
		{"a", "actions", func(m *Model) tea.Cmd { return m.goRepoTab(repo, tabActions) }},
		{"f", "файлы", func(m *Model) tea.Cmd { return m.goRepoTab(repo, tabCode) }},
	}
}

// goRepoTab — со страницы репозитория на его вкладку: страницы закрываются, репозиторий становится текущим.
func (m *Model) goRepoTab(repo string, t tab) tea.Cmd {
	m.pages = nil
	cmd := m.selectRepo(repo)
	return tea.Batch(cmd, m.switchTab(t))
}

func orDash(s string) string {
	if s == "" {
		return "без описания"
	}
	return s
}

// langLine — полоса языков, как под описанием репозитория на сайте.
func langLine(langs []domain.Lang) string {
	total := 0
	for _, l := range langs {
		total += l.Bytes
	}
	if total == 0 {
		return ""
	}
	var parts []string
	for _, l := range langs[:min(len(langs), 5)] {
		pct := float64(l.Bytes) * 100 / float64(total)
		if pct < 0.5 {
			break
		}
		parts = append(parts, ui.Info.Render(l.Name)+ui.Faint.Render(fmt.Sprintf(" %.0f%%", pct)))
	}
	return strings.Join(parts, ui.Faint.Render(" · "))
}

// ── коммит ─────────────────────────────────────────────────────────────────

func (m *Model) openCommitPage(repo, sha, title string) tea.Cmd {
	return m.openPage(fmt.Sprintf("%s · %s %s", repo, sha[:min(7, len(sha))], title), "https://github.com/"+repo+"/commit/"+sha,
		func(ctx context.Context) (func(p *page), error) {
			c, err := m.gh.Commit(ctx, repo, sha)
			if err != nil {
				return nil, err
			}
			return func(p *page) {
				p.url = c.URL
				p.build = func(p *page, w int) {
					p.header = []string{
						sKey.Render(c.SHA[:min(7, len(c.SHA))]) + ui.Faint.Render("  @"+c.Author+" · "+ui.Ago(c.Date)+" назад · ") +
							ui.Good.Render("+"+strconv.Itoa(c.Additions)) + " " + ui.Bad.Render("−"+strconv.Itoa(c.Deletions)) +
							ui.Faint.Render(fmt.Sprintf(" · %d %s", len(c.Files), ui.Plural(len(c.Files), "файл", "файла", "файлов"))),
					}
					msg := []string{}
					for _, l := range strings.Split(strings.TrimSpace(c.Message), "\n") {
						msg = append(msg, "  "+ansi.Wordwrap(l, max(w-4, 20), ""))
					}
					p.sections = []*section{
						{name: "Изменения", badge: count(len(c.Files)), doc: filesDoc(c.Files, w)},
						{name: "Сообщение", doc: msg},
					}
				}
			}, nil
		})
}

// ── общее ──────────────────────────────────────────────────────────────────

// ask — окно ввода: пустой ответ — ничего не делать.
func (m *Model) ask(title, placeholder string, onSubmit func(m *Model, v string) tea.Cmd) tea.Cmd {
	m.prompt = &prompt{
		title: title,
		in:    newInput(placeholder),
		onSubmit: func(m *Model, v string) tea.Cmd {
			if v == "" {
				return nil
			}
			return onSubmit(m, v)
		},
	}
	return m.prompt.in.Focus()
}

// actPage — действие со страницы: после успеха страница перезагружается (новый комментарий, статус).
func (m *Model) actPage(p *page, okText string, f func(ctx context.Context) error) tea.Cmd {
	return m.load(func(ctx context.Context) (tea.Msg, error) {
		if err := f(ctx); err != nil {
			return nil, err
		}
		return pageDoneMsg{id: p.id, text: okText}, nil
	})
}

type pageDoneMsg struct {
	id   int
	text string
}

var prURLRe = regexp.MustCompile(`github\.com/([^/]+/[^/]+)/(pull|issues)/(\d+)`)

// openByURL — событие ленты про PR или issue открывает его страницу; nil — ссылка не про них.
func (m *Model) openByURL(repo, url, title string) tea.Cmd {
	mm := prURLRe.FindStringSubmatch(url)
	if mm == nil {
		return nil
	}
	n, _ := strconv.Atoi(mm[3])
	if mm[2] == "pull" {
		return m.openPRPage(mm[1], n, title)
	}
	return m.openIssuePage(mm[1], n, title)
}
