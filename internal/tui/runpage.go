package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
	"github.com/van1dalgrr-arch/github-watcher/internal/ui"
)

// Страница запуска Actions: job-ы и шаги, лог job-а с прыжками по ошибкам, артефакты,
// перезапуск и отмена. Пока запуск идёт — обновляется сама раз в 10 секунд.

func (m *Model) openRunPage(r domain.WorkflowRun) tea.Cmd {
	return m.openPage(fmt.Sprintf("%s · %s · %s", r.Repo, r.Name, r.Title), r.URL, func(ctx context.Context) (func(p *page), error) {
		d, err := m.gh.Run(ctx, r.Repo, r.ID)
		if err != nil {
			return nil, err
		}
		return func(p *page) { m.fillRun(p, d) }, nil
	})
}

func (m *Model) fillRun(p *page, d domain.RunDetail) {
	p.live = d.Status != "completed"
	p.build = func(p *page, w int) {
		st := ui.CI(d.State())
		meta := ui.Faint.Render(fmt.Sprintf("  %s · %s", d.Event, ui.Ago(d.UpdatedAt)+" назад"))
		if d.Actor != "" {
			meta += ui.Faint.Render(" · @" + d.Actor)
		}
		if d.Attempt > 1 {
			meta += ui.Warn.Render(" · попытка " + strconv.Itoa(d.Attempt))
		}
		if d.Took > 0 {
			meta += ui.Faint.Render(" · " + d.Took.String())
		}
		p.header = []string{
			st + " " + ui.Bold.Render(d.Name) + ui.Faint.Render(" · ") + ui.Info.Render(d.Branch) +
				ui.Faint.Render(" · ") + sKey.Render(d.HeadSHA[:min(7, len(d.HeadSHA))]) + meta,
		}
		var jobs []pageItem
		for _, j := range d.Jobs {
			jobs = append(jobs, pageItem{
				url: j.URL,
				render: func(w int, sel bool) string {
					st := ui.CI(j.State())
					if j.State() == "pending" {
						st = m.spin.View()
					}
					failed := ""
					for _, s := range j.Steps {
						if s.Conclusion == "failure" {
							failed = ui.Bad.Render("упал: " + s.Name)
							break
						}
					}
					return row(sel, w, c(st, 1), c(ui.Bold.Render(j.Name), 28), flex(failed), c(ui.Faint.Render(dur(j.Took)), 8))
				},
				open: func(m *Model) tea.Cmd { return m.openJobLog(d.WorkflowRun, j) },
			})
		}
		var arts []pageItem
		for _, a := range d.Artifacts {
			arts = append(arts, pageItem{
				render: func(w int, sel bool) string {
					note := ui.Faint.Render(humanSize(a.Size))
					if a.Expired {
						note = ui.Faint.Render("истёк")
					}
					return row(sel, w, flex(a.Name), c(note, 10))
				},
				open: func(m *Model) tea.Cmd { return m.downloadArtifact(d.Repo, a) },
			})
		}
		p.sections = []*section{
			{name: "Job-ы", badge: count(len(d.Jobs)), items: jobs},
			{name: "Шаги", doc: stepsDoc(d.Jobs, ui.CI("pending"))},
			{name: "Артефакты", badge: count(len(d.Artifacts)), items: orEmpty(arts)},
		}
	}
	rerun := func(failed bool) func(m *Model) tea.Cmd {
		return func(m *Model) tea.Cmd {
			if d.Status != "completed" {
				m.setFlash("запуск ещё идёт — перезапустить можно после завершения (или X — отменить)", true)
				return nil
			}
			what := "целиком"
			if failed {
				what = "только упавшие job-ы"
			}
			m.confirm = &confirm{question: fmt.Sprintf("Перезапустить «%s» (%s)?", d.Name, what), onYes: func(m *Model) tea.Cmd {
				return m.actPage(p, "запуск перезапущен", func(ctx context.Context) error { return m.gh.RerunWorkflow(ctx, d.Repo, d.ID, failed) })
			}}
			return nil
		}
	}
	p.keys = []pageKey{
		{"R", "перезапуск", rerun(false)},
		{"f", "упавшие", rerun(true)},
		{"X", "отменить", func(m *Model) tea.Cmd {
			if d.Status == "completed" {
				m.setFlash("запуск уже закончился", true)
				return nil
			}
			m.confirm = &confirm{question: fmt.Sprintf("Отменить «%s» на %s?", d.Name, d.Branch), onYes: func(m *Model) tea.Cmd {
				return m.actPage(p, "отмена отправлена", func(ctx context.Context) error { return m.gh.CancelRun(ctx, d.Repo, d.ID) })
			}}
			return nil
		}},
		{"L", "весь лог", func(m *Model) tea.Cmd {
			m.setFlash("загружаю логи "+d.Name+"…", false)
			return m.loadRunLogs(d.WorkflowRun)
		}},
	}
}

// orEmpty — пустой список вместо nil: раздел остаётся списком («пусто»), а не документом.
func orEmpty(items []pageItem) []pageItem {
	if items == nil {
		return []pageItem{}
	}
	return items
}

func dur(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return d.String()
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f МБ", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d КБ", n>>10)
	}
	return fmt.Sprintf("%d Б", n)
}

// stepsDoc — job-ы и их шаги с ✓/✗ и временем, как левая колонка запуска на сайте.
func stepsDoc(jobs []domain.Job, spin string) []string {
	var out []string
	for i, j := range jobs {
		if i > 0 {
			out = append(out, "")
		}
		st := ui.CI(j.State())
		if j.State() == "pending" {
			st = spin
		}
		out = append(out, st+" "+ui.Bold.Render(j.Name)+ui.Faint.Render("  "+dur(j.Took)))
		for _, s := range j.Steps {
			mark := ui.CI(domain.RunState(s.Status, s.Conclusion))
			switch s.Conclusion {
			case "skipped":
				mark = ui.Faint.Render("–")
			case "cancelled":
				mark = ui.Faint.Render("⊘")
			}
			if s.Status != "completed" && s.Status != "" {
				mark = spin
			}
			name := s.Name
			if s.Conclusion == "failure" {
				name = ui.Bad.Render(name)
			}
			out = append(out, "    "+mark+" "+name+ui.Faint.Render("  "+dur(s.Took)))
		}
	}
	return out
}

func (m *Model) openJobLog(r domain.WorkflowRun, j domain.Job) tea.Cmd {
	if j.Status != "completed" {
		m.setFlash("job «"+j.Name+"» ещё идёт — лог появится после завершения (шаги — раздел 2)", true)
		return nil
	}
	m.setFlash("загружаю лог "+j.Name+"…", false)
	return m.load(func(ctx context.Context) (tea.Msg, error) {
		text, err := m.gh.JobLog(ctx, r.Repo, j.ID)
		r.Name += " · " + j.Name
		r.Conclusion, r.Status, r.URL = j.Conclusion, j.Status, j.URL
		return runLogsMsg{r, text}, err
	})
}

func (m *Model) downloadArtifact(repo string, a domain.Artifact) tea.Cmd {
	if a.Expired {
		m.setFlash("артефакт «"+a.Name+"» истёк — GitHub его уже удалил", true)
		return nil
	}
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Downloads")
	m.setFlash("качаю «"+a.Name+"»…", false)
	return m.load(func(ctx context.Context) (tea.Msg, error) {
		path, err := m.gh.DownloadArtifact(ctx, repo, a.ID, a.Name, dir)
		return doneMsg{text: "скачан: " + shortDir(path), reload: m.tab}, err
	})
}

// ── ручной запуск workflow ────────────────────────────────────────────────

func (m *Model) openWorkflowsPage(repo string) tea.Cmd {
	return m.openPage(repo+" · запустить workflow", "https://github.com/"+repo+"/actions", func(ctx context.Context) (func(p *page), error) {
		ws, err := m.gh.Workflows(ctx, repo)
		if err != nil {
			return nil, err
		}
		return func(p *page) {
			p.build = func(p *page, w int) {
				p.header = []string{ui.Faint.Render("enter — запустить на ветке (workflow должен уметь on: workflow_dispatch)")}
				var items []pageItem
				for _, wf := range ws {
					items = append(items, pageItem{
						url: "https://github.com/" + repo + "/actions/workflows/" + filepath.Base(wf.Path),
						render: func(w int, sel bool) string {
							state := ""
							if wf.State != "active" {
								state = ui.Faint.Render("выключен")
							}
							return row(sel, w, c(ui.Bold.Render(wf.Name), 30), flex(ui.Faint.Render(wf.Path)), c(state, 9))
						},
						open: func(m *Model) tea.Cmd { return m.askDispatch(repo, wf) },
					})
				}
				p.sections = []*section{{name: "Workflow", badge: count(len(ws)), items: orEmpty(items)}}
			}
		}, nil
	})
}

func (m *Model) askDispatch(repo string, wf domain.Workflow) tea.Cmd {
	ref := "main"
	if dir := m.clone(repo); dir != "" {
		if b := gitOut(dir, "branch", "--show-current"); b != "" {
			ref = b
		}
	}
	cmd := m.ask("Запустить «"+wf.Name+"» — на какой ветке?", "ветка или тег", func(m *Model, v string) tea.Cmd {
		return m.load(func(ctx context.Context) (tea.Msg, error) {
			err := m.gh.Dispatch(ctx, repo, wf.ID, v)
			if err != nil && strings.Contains(err.Error(), "workflow_dispatch") {
				err = fmt.Errorf("в «%s» нет on: workflow_dispatch — вручную его не запустить", wf.Name)
			}
			return doneMsg{text: "«" + wf.Name + "» запущен на " + v + " — появится в списке через пару секунд", reload: tabActions}, err
		})
	})
	m.prompt.in.SetValue(ref)
	m.prompt.in.CursorEnd()
	return cmd
}
