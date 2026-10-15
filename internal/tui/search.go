package tui

import (
	"context"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
	"github.com/van1dalgrr-arch/github-watcher/internal/ui"
)

// Поиск по всему GitHub (S с любой вкладки): репозитории, issues и PR, люди, код — как строка поиска на сайте.
// Работают и квалификаторы GitHub: language:go, user:octocat, is:pr is:open, stars:>100.

func (m *Model) askSearch() tea.Cmd {
	return m.ask("Поиск по GitHub", "что искать — можно с language:go, user:…, is:pr, stars:>100", func(m *Model, q string) tea.Cmd {
		return m.openSearchPage(q)
	})
}

func (m *Model) openSearchPage(q string) tea.Cmd {
	return m.openPage("Поиск: "+q, "https://github.com/search?q="+strings.ReplaceAll(q, " ", "+"),
		func(ctx context.Context) (func(p *page), error) {
			res, err := m.gh.Search(ctx, q)
			if err != nil {
				return nil, err
			}
			return func(p *page) { m.fillSearch(p, res) }, nil
		})
}

func total(shown, all int) string {
	if all > shown {
		return ui.Faint.Render(strconv.Itoa(shown) + " из " + strconv.Itoa(all))
	}
	return count(shown)
}

func (m *Model) fillSearch(p *page, res domain.SearchResult) {
	p.build = func(p *page, w int) {
		p.header = []string{ui.Faint.Render("enter — открыть · o — на сайте · S — новый поиск")}
		var issues []pageItem
		for _, f := range res.Issues {
			issues = append(issues, pageItem{
				url: f.URL,
				render: func(w int, sel bool) string {
					icon := ui.IssueState(domain.Issue{State: f.State})
					if f.PR {
						icon = ui.PRState(domain.PullRequest{State: f.State})
					}
					comments := ""
					if f.Comments > 0 {
						comments = ui.Faint.Render("✎" + strconv.Itoa(f.Comments))
					}
					return row(sel, w, c(icon, 1), c(ui.Bold.Render(f.Repo), 30), c(ui.Faint.Render("#"+strconv.Itoa(f.Number)), 6),
						flex(f.Title), c(comments, 4), c(ui.Faint.Render("@"+f.Author), 16), c(ui.Faint.Render(ui.Ago(f.UpdatedAt)), 5))
				},
				open: func(m *Model) tea.Cmd {
					if f.PR {
						return m.openPRPage(f.Repo, f.Number, f.Title)
					}
					return m.openIssuePage(f.Repo, f.Number, f.Title)
				},
			})
		}
		var users []pageItem
		for _, u := range res.Users {
			users = append(users, pageItem{
				url:    "https://github.com/" + u,
				render: func(w int, sel bool) string { return row(sel, w, flex(ui.Bold.Render("@"+u))) },
				open:   func(m *Model) tea.Cmd { return m.openProfilePage(u) },
			})
		}
		var code []pageItem
		for _, h := range res.Code {
			frag := strings.Join(strings.Fields(h.Fragment), " ")
			code = append(code, pageItem{
				url: h.URL,
				render: func(w int, sel bool) string {
					return row(sel, w, c(ui.Bold.Render(h.Repo), 30), c(ui.Info.Render(h.Path), 40), flex(ui.Faint.Render(frag)))
				},
				open: func(m *Model) tea.Cmd { return m.openRemoteFile(h.Repo, h.Path, h.URL) },
			})
		}
		p.sections = []*section{
			{name: "Репозитории", badge: total(len(res.Repos), res.Totals[0]), items: m.repoItems(res.Repos)},
			{name: "Issues и PR", badge: total(len(res.Issues), res.Totals[1]), items: orEmpty(issues)},
			{name: "Люди", badge: total(len(res.Users), res.Totals[2]), items: orEmpty(users)},
			{name: "Код", badge: total(len(res.Code), res.Totals[3]), items: orEmpty(code)},
		}
		// сразу на первый непустой раздел: искали человека — не листать пустые репозитории
		for i, s := range p.sections {
			if len(s.items) > 0 {
				if p.cur == 0 && len(p.sections[0].items) == 0 {
					p.cur = i
				}
				break
			}
		}
	}
}

// openRemoteFile — файл из GitHub целиком, с подсветкой и номерами строк.
func (m *Model) openRemoteFile(repo, path, url string) tea.Cmd {
	m.setFlash("загружаю "+path+"…", false)
	return m.load(func(ctx context.Context) (tea.Msg, error) {
		text, err := m.gh.File(ctx, repo, path)
		return remoteFileMsg{repo + " · " + path, url, highlight(path, text)}, err
	})
}

type remoteFileMsg struct {
	title, url string
	lines      []string
}
