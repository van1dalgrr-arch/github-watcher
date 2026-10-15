package tui

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
	"github.com/van1dalgrr-arch/github-watcher/internal/ui"
)

// Релизы: раздел на странице репозитория, страница релиза (заметки, файлы), N — новый релиз
// с заметками, которые собирает GitHub (как «Generate release notes» на сайте).

func (m *Model) releaseItems(repo string, rels []domain.Release) []pageItem {
	items := make([]pageItem, 0, len(rels))
	for _, r := range rels {
		items = append(items, pageItem{
			url: r.URL,
			render: func(w int, sel bool) string {
				kind := ""
				switch {
				case r.Draft:
					kind = ui.Faint.Render("черновик")
				case r.Prerelease:
					kind = ui.Warn.Render("pre-release")
				}
				return row(sel, w, c(ui.Good.Render(r.Tag), 14), flex(r.Name), c(kind, 11),
					c(ui.Faint.Render(strconv.Itoa(len(r.Assets))+" "+ui.Plural(len(r.Assets), "файл", "файла", "файлов")), 10),
					c(ui.Faint.Render(ui.Ago(r.CreatedAt)), 5))
			},
			open: func(m *Model) tea.Cmd { return m.openReleasePage(repo, r) },
		})
	}
	return orEmpty(items)
}

// openReleasePage — данные уже есть (пришли со страницей репозитория), страница открывается сразу.
func (m *Model) openReleasePage(repo string, r domain.Release) tea.Cmd {
	return m.openPage(repo+" · "+r.Tag+" "+r.Name, r.URL, func(context.Context) (func(p *page), error) {
		return func(p *page) {
			p.build = func(p *page, w int) {
				meta := ui.Good.Render(r.Tag) + ui.Faint.Render("  @"+r.Author+" · "+ui.Ago(r.CreatedAt)+" назад")
				if r.Draft {
					meta += "  " + ui.Faint.Render("черновик")
				}
				if r.Prerelease {
					meta += "  " + ui.Warn.Render("pre-release")
				}
				p.header = []string{meta}
				var assets []pageItem
				for _, a := range r.Assets {
					assets = append(assets, pageItem{
						url: a.URL,
						render: func(w int, sel bool) string {
							return row(sel, w, flex(a.Name), c(ui.Faint.Render(humanSize(a.Size)), 10), c(ui.Faint.Render("↓"+strconv.Itoa(a.Downloads)), 7))
						},
						open: func(m *Model) tea.Cmd { return openURL(a.URL) }, // браузер скачает (и приватный — с твоей сессией)
					})
				}
				p.sections = []*section{
					{name: "Заметки", doc: indent(markdown(r.Body, w-2))},
					{name: "Файлы", badge: count(len(r.Assets)), items: orEmpty(assets)},
				}
			}
		}, nil
	})
}

var semverRe = regexp.MustCompile(`^(v?)(\d+)\.(\d+)\.(\d+)$`)

// nextVersion — следующий патч после последнего релиза: v1.2.3 → v1.2.4; релизов не было — v0.1.0.
func nextVersion(last string) string {
	mm := semverRe.FindStringSubmatch(last)
	if mm == nil {
		return "v0.1.0"
	}
	patch, _ := strconv.Atoi(mm[4])
	return fmt.Sprintf("%s%s.%s.%d", mm[1], mm[2], mm[3], patch+1)
}

func (m *Model) askRelease(p *page, d domain.RepoDetail) tea.Cmd {
	repo := d.FullName()
	cmd := m.ask("Новый релиз "+repo+" — тег", "например v1.2.0", func(m *Model, tag string) tea.Cmd {
		create := func(draft bool) func(m *Model) tea.Cmd {
			return func(m *Model) tea.Cmd {
				return m.load(func(ctx context.Context) (tea.Msg, error) {
					r, err := m.gh.CreateRelease(ctx, repo, tag, d.DefaultBranch, draft)
					return releaseCreatedMsg{repo, p.id, r}, err
				})
			}
		}
		m.confirm = &confirm{
			question: fmt.Sprintf("Создать релиз %s на %s? Тег поставится на последний коммит ветки, заметки соберёт GitHub.", tag, d.DefaultBranch),
			onYes:    create(false),
			alt:      []pageKey{{key: "d", hint: "черновиком", do: create(true)}},
		}
		return nil
	})
	m.prompt.in.SetValue(nextVersion(d.LatestRelease))
	m.prompt.in.CursorEnd()
	return cmd
}

type releaseCreatedMsg struct {
	repo   string
	pageID int
	rel    domain.Release
}
