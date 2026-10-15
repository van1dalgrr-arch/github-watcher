package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
	"github.com/van1dalgrr-arch/github-watcher/internal/ui"
)

// Профиль, как на github.com: о себе, сетка вкладов за год, закреплённые репозитории, репозитории, звёзды.

// greens — клетки сетки в цветах тёмной темы GitHub (пустая чуть светлее, чтобы сетку было видно).
var greens = []lipgloss.Style{
	lipgloss.NewStyle().Foreground(lipgloss.Color("#2d333b")),
	lipgloss.NewStyle().Foreground(lipgloss.Color("#0e4429")),
	lipgloss.NewStyle().Foreground(lipgloss.Color("#006d32")),
	lipgloss.NewStyle().Foreground(lipgloss.Color("#26a641")),
	lipgloss.NewStyle().Foreground(lipgloss.Color("#39d353")),
}

var monthsRU = []string{"янв", "фев", "мар", "апр", "май", "июн", "июл", "авг", "сен", "окт", "ноя", "дек"}

// contribGraph — сетка вкладов: 7 строк (вс…сб), недели слева направо; сколько влезает в ширину w.
func contribGraph(weeks [][]domain.Day, w int) []string {
	n := min(len(weeks), max((w-5)/2, 1))
	if n == 0 {
		return nil
	}
	weeks = weeks[len(weeks)-n:]
	months := []rune(strings.Repeat(" ", 4+2*n))
	last := -1
	for i, wk := range weeks {
		if len(wk) == 0 {
			continue
		}
		if mo := int(wk[0].Date.Month()) - 1; mo != last {
			if last != -1 || wk[0].Date.Day() <= 7 { // первая неполная неделя месяца без подписи — не налезет на соседний
				label := []rune(monthsRU[mo])
				at := 4 + 2*i
				if at+len(label) <= len(months) {
					copy(months[at:], label)
				}
			}
			last = mo
		}
	}
	out := []string{ui.Faint.Render(strings.TrimRight(string(months), " "))}
	dayLabel := map[int]string{1: "пн", 3: "ср", 5: "пт"}
	for d := range 7 {
		var b strings.Builder
		b.WriteString(ui.Faint.Render(fmt.Sprintf("%-4s", dayLabel[d])))
		for _, wk := range weeks {
			if d < len(wk) {
				b.WriteString(greens[min(wk[d].Level, 4)].Render("■") + " ")
			} else {
				b.WriteString("  ")
			}
		}
		out = append(out, strings.TrimRight(b.String(), " "))
	}
	legend := ui.Faint.Render("меньше ")
	for _, g := range greens {
		legend += g.Render("■") + " "
	}
	legend += ui.Faint.Render("больше")
	// легенда — по правому краю сетки (ширина сетки 3+2n, легенды — 23)
	return append(out, strings.Repeat(" ", max(3+2*n-lipgloss.Width(legend), 0))+legend)
}

func (m *Model) openProfilePage(login string) tea.Cmd {
	return m.openPage("@"+login, "https://github.com/"+login, func(ctx context.Context) (func(p *page), error) {
		pr, err := m.gh.Profile(ctx, login)
		if err != nil {
			return nil, err
		}
		return func(p *page) { m.fillProfile(p, pr) }, nil
	})
}

func (m *Model) fillProfile(p *page, pr domain.Profile) {
	if pr.Name != "" {
		p.title = pr.Name + "  @" + pr.Login
	}
	p.build = func(p *page, w int) {
		meta := []string{
			ui.Bold.Render(strconv.Itoa(pr.Followers)) + ui.Faint.Render(" "+ui.Plural(pr.Followers, "подписчик", "подписчика", "подписчиков")),
			ui.Bold.Render(strconv.Itoa(pr.Following)) + ui.Faint.Render(" подписок"),
			ui.Bold.Render(strconv.Itoa(pr.PublicRepos)) + ui.Faint.Render(" "+ui.Plural(pr.PublicRepos, "публичный репозиторий", "публичных репозитория", "публичных репозиториев")),
		}
		for _, s := range []string{pr.Company, pr.Location, pr.Website} {
			if s != "" {
				meta = append(meta, ui.Info.Render(s))
			}
		}
		if !pr.CreatedAt.IsZero() {
			meta = append(meta, ui.Faint.Render("на GitHub с "+strconv.Itoa(pr.CreatedAt.Year())))
		}
		p.header = []string{strings.Join(meta, ui.Faint.Render(" · "))}
		if pr.Bio != "" {
			p.header = append(p.header, pr.Bio)
		}

		over := []string{ui.Bold.Render(fmt.Sprintf("%d %s за год", pr.Contributions,
			ui.Plural(pr.Contributions, "вклад", "вклада", "вкладов"))), ""}
		over = append(over, contribGraph(pr.Weeks, w-2)...)
		if len(pr.Pinned) > 0 {
			over = append(over, "", ui.Bold.Render("Закреплённые"))
			for _, r := range pr.Pinned {
				line := "  " + ui.Info.Render(r.FullName()) + "  " + ui.Warn.Render("★"+strconv.Itoa(r.Stars))
				if r.Language != "" {
					line += ui.Faint.Render("  " + r.Language)
				}
				over = append(over, line)
				if r.Description != "" {
					over = append(over, "    "+ui.Faint.Render(r.Description))
				}
			}
		}
		p.sections = []*section{
			{name: "Обзор", doc: indent(over)},
			{name: "Репозитории", badge: count(len(pr.Repos)), items: m.repoItems(pr.Repos)},
			{name: "Звёзды", badge: count(len(pr.Starred)), items: m.repoItems(pr.Starred)},
		}
	}
}

// repoItems — список репозиториев: enter — страница репозитория.
func (m *Model) repoItems(repos []domain.Repo) []pageItem {
	items := make([]pageItem, 0, len(repos))
	for _, r := range repos {
		items = append(items, pageItem{
			url: r.URL,
			render: func(w int, sel bool) string {
				vis := ui.Faint.Render("○")
				if r.Private {
					vis = ui.Warn.Render("◉")
				}
				return row(sel, w, c(vis, 1), c(ui.Bold.Render(r.FullName()), 36), c(ui.Info.Render(r.Language), 11),
					c(ui.Warn.Render("★")+strconv.Itoa(r.Stars), 6), flex(ui.Faint.Render(r.Description)))
			},
			open: func(m *Model) tea.Cmd { return m.openRepoPage(r.FullName()) },
		})
	}
	return orEmpty(items)
}
