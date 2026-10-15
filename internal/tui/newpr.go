package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
)

// Новый PR из текущей ветки, как gh pr create: ветка не на GitHub — запушим (команда видна в терминале),
// заголовок и описание — в $EDITOR (заготовка из коммитов ветки), потом y — создать, d — черновиком.

type (
	editedMsg struct {
		file       string
		err        error
		repo, head string
		base       string
	}
	prCreatedMsg struct{ pr domain.PullRequest }
)

// gitOut — вывод git в папке dir; "" при ошибке.
func gitOut(dir string, args ...string) string {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (m *Model) clone(repo string) string {
	if m.findClone != nil {
		return m.findClone(repo)
	}
	return findClone(repo)
}

func (m *Model) newPR() tea.Cmd {
	repo := m.repo
	dir := m.clone(repo)
	if dir == "" {
		m.setFlash("клона "+repo+" не нашёл — git clone в терминале на Dashboard", true)
		return nil
	}
	branch := gitOut(dir, "branch", "--show-current")
	base := strings.TrimPrefix(gitOut(dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"), "origin/")
	if base == "" {
		base = "main"
	}
	switch branch {
	case "":
		m.setFlash("в клоне не ветка, а отдельный коммит (detached HEAD) — переключись на ветку", true)
		return nil
	case base:
		m.setFlash("ты на "+base+" — PR делается из своей ветки: git switch -c feat/… (терминал git на Dashboard)", true)
		return nil
	}
	subjects := strings.Split(gitOut(dir, "log", "--reverse", "--format=%s", "origin/"+base+"..HEAD"), "\n")
	if subjects[0] == "" {
		m.setFlash("в "+branch+" нет коммитов поверх "+base+" — нечего предлагать", true)
		return nil
	}
	upstream := gitOut(dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if upstream == "" || gitOut(dir, "rev-list", "--count", "@{upstream}..HEAD") != "0" {
		m.confirm = &confirm{
			question: fmt.Sprintf("Ветки %s ещё нет на GitHub или в ней незапушенные коммиты. Запушить и продолжить?", branch),
			onYes: func(m *Model) tea.Cmd {
				m.term.dir, m.term.open = dir, true
				m.layout()
				m.term.then = func(m *Model, code int) tea.Cmd {
					if code != 0 {
						m.setFlash("git push не прошёл — вывод в терминале внизу", true)
						return nil
					}
					return m.newPR()
				}
				return m.term.run("git push -u origin " + shellQuote(branch))
			},
		}
		return nil
	}

	title, body := humanBranch(branch), ""
	if len(subjects) == 1 {
		title = subjects[0]
		body = gitOut(dir, "log", "-1", "--format=%b")
	} else {
		body = "- " + strings.Join(subjects, "\n- ")
	}
	file := filepath.Join(os.TempDir(), "gw-pr-"+safeName.ReplaceAllString(branch, "-")+".md")
	tpl := title + "\n\n" + body + "\n\n" + prHelp(branch, base, len(subjects))
	if err := os.WriteFile(file, []byte(tpl), 0o600); err != nil {
		m.setFlash(err.Error(), true)
		return nil
	}
	editor := m.editor
	if editor == "" {
		editor = firstNonEmpty(os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi")
	}
	cmd := exec.Command("sh", "-c", editor+" "+shellQuote(file)) // в EDITOR бывают аргументы: "code -w"
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editedMsg{file: file, err: err, repo: repo, head: branch, base: base}
	})
}

func prHelp(branch, base string, n int) string {
	return fmt.Sprintf("<!--\nНовый PR: %s → %s, коммитов: %d.\nПервая строка — заголовок, дальше — описание (markdown).\n"+
		"Сохрани и закрой редактор. Пустой заголовок — отмена. Этот блок в PR не попадёт.\n-->\n", branch, base, n)
}

var safeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// humanBranch — заголовок из имени ветки: feat/cache-layer → Cache layer.
func humanBranch(b string) string {
	if i := strings.LastIndexByte(b, '/'); i >= 0 {
		b = b[i+1:]
	}
	b = strings.NewReplacer("-", " ", "_", " ").Replace(b)
	if b == "" {
		return b
	}
	return strings.ToUpper(b[:1]) + b[1:]
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// parsePR — заголовок и описание из файла редактора: комментарии-подсказки убраны, первая строка — заголовок.
func parsePR(text string) (title, body string) {
	text = strings.TrimSpace(htmlComment.ReplaceAllString(strings.ReplaceAll(text, "\r\n", "\n"), ""))
	title, body, _ = strings.Cut(text, "\n")
	return strings.TrimSpace(title), strings.TrimSpace(body)
}

// edited — редактор закрыт: спросить, создавать ли (y — обычный, d — черновиком).
func (m *Model) edited(msg editedMsg) tea.Cmd {
	data, err := os.ReadFile(msg.file)
	if msg.err != nil || err != nil {
		m.setFlash("редактор: "+firstErr(msg.err, err).Error(), true)
		return nil
	}
	title, body := parsePR(string(data))
	if title == "" {
		m.setFlash("пустой заголовок — PR не создан (текст остался в "+msg.file+")", false)
		return nil
	}
	create := func(draft bool) func(m *Model) tea.Cmd {
		return func(m *Model) tea.Cmd {
			return m.load(func(ctx context.Context) (tea.Msg, error) {
				pr, err := m.gh.CreatePR(ctx, msg.repo, msg.head, msg.base, title, body, draft)
				if err == nil {
					_ = os.Remove(msg.file)
				}
				return prCreatedMsg{pr}, err
			})
		}
	}
	m.confirm = &confirm{
		question: fmt.Sprintf("Создать PR «%s»: %s → %s?", title, msg.head, msg.base),
		onYes:    create(false),
		alt:      []pageKey{{key: "d", hint: "черновиком", do: create(true)}},
	}
	return nil
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
