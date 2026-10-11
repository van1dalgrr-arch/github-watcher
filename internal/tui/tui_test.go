package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/van1dalgrr-arch/github-watcher/internal/config"
	"github.com/van1dalgrr-arch/github-watcher/internal/daemonclient"
	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
	"github.com/van1dalgrr-arch/github-watcher/internal/github"
	"github.com/van1dalgrr-arch/github-watcher/internal/watcher"
)

// fakeGH — GitHub-заглушка: отдаёт фиксированные данные и запоминает действия.
type fakeGH struct {
	github.Client // нереализованные методы паникуют — значит, тест дёрнул лишнее
	merged        []int
	closed        []int
	comments      []string
}

var now = time.Now()

func (f *fakeGH) Me(context.Context) (string, error) { return "octocat", nil }
func (f *fakeGH) Repos(context.Context) ([]domain.Repo, error) {
	return []domain.Repo{
		{Owner: "octocat", Name: "api", Language: "Go", Stars: 12, Description: "Backend", PushedAt: now.Add(-time.Hour)},
		{Owner: "octocat", Name: "web", Language: "TypeScript", Private: true, PushedAt: now.Add(-48 * time.Hour)},
	}, nil
}
func (f *fakeGH) PullRequests(_ context.Context, repo, _ string) ([]domain.PullRequest, error) {
	return []domain.PullRequest{
		{Repo: repo, Number: 7, Title: "Добавить кэш", Author: "alice", State: "open", Head: "feat/cache", Base: "main", CIStatus: "success", UpdatedAt: now},
		{Repo: repo, Number: 6, Title: "Починить логин", Author: "bob", State: "open", Head: "fix/login", Base: "main", CIStatus: "failure", UpdatedAt: now},
	}, nil
}
func (f *fakeGH) Issues(_ context.Context, repo, _ string) ([]domain.Issue, error) {
	return []domain.Issue{{Repo: repo, Number: 3, Title: "Падает на пустом конфиге", Author: "carol", State: "open", Labels: []string{"bug"}, Comments: 2}}, nil
}
func (f *fakeGH) WorkflowRuns(_ context.Context, repo string) ([]domain.WorkflowRun, error) {
	return []domain.WorkflowRun{
		{Repo: repo, ID: 3, Name: "notify", Branch: "main", Event: "schedule", Title: "notify", Status: "completed", Conclusion: "success", UpdatedAt: now},
		{Repo: repo, ID: 1, Name: "ci", Branch: "main", Event: "push", Title: "Добавить кэш", Status: "completed", Conclusion: "failure", UpdatedAt: now},
		{Repo: repo, ID: 2, Name: "deploy", Branch: "main", Event: "push", Title: "Релиз", Status: "in_progress", UpdatedAt: now},
	}, nil
}
func (f *fakeGH) RunLogs(context.Context, string, int64) (string, error) {
	return "\x00JOB test · failure\n##[group]Run go test\ngo test ./...\n##[endgroup]\n--- FAIL: TestX\n##[error]Process completed with exit code 1.\n", nil
}
func (f *fakeGH) MergePR(_ context.Context, _ string, n int, _ string) error {
	f.merged = append(f.merged, n)
	return nil
}
func (f *fakeGH) CloseIssue(_ context.Context, _ string, n int) error {
	f.closed = append(f.closed, n)
	return nil
}
func (f *fakeGH) Comment(_ context.Context, _ string, _ int, body string) error {
	f.comments = append(f.comments, body)
	return nil
}

func newTestModel(t *testing.T, w, h int) (*Model, *fakeGH) {
	t.Helper()
	gh := &fakeGH{}
	m := newModel(context.Background(), gh, daemonclient.New("http://127.0.0.1:1"), config.Config{TelegramChat: "1"}, "octocat/api")
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	run(m, m.loadMe(), m.loadRepos())
	run(m, m.loadDashRuns())
	m.Update(daemonMsg{
		status: &watcher.Status{Started: now.Add(-time.Hour), LastTick: now, Interval: "1m0s", Saved: 42, Sent: 5},
		events: []domain.Event{{Kind: domain.EventCIFailed, Repo: "octocat/api", Title: "CI упал: ci", CreatedAt: now}},
		logs:   []string{"level=INFO msg=started", "level=ERROR msg=boom"},
	})
	return m, gh
}

// run выполняет команды синхронно и скармливает результат в Update (Batch разворачивается).
func run(m *Model, cmds ...tea.Cmd) {
	for _, cmd := range cmds {
		if cmd == nil {
			continue
		}
		switch msg := cmd().(type) {
		case tea.BatchMsg:
			run(m, msg...)
		case nil:
		default:
			switch msg.(type) {
			case loaded, termOutMsg, termDoneMsg:
			default:
				continue // тики, спиннер и т.п. в тесте не нужны
			}
			_, next := m.Update(msg)
			run(m, next)
		}
	}
}

func press(m *Model, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		default:
			r := []rune(k)[0]
			msg = tea.KeyPressMsg{Code: r, Text: k}
		}
		_, cmd := m.Update(msg)
		run(m, cmd)
	}
}

// screen проверяет, что кадр ровно по размеру терминала, и при GW_DUMP=1 печатает его.
func screen(t *testing.T, m *Model, name string) string {
	t.Helper()
	out := m.render()
	lines := strings.Split(out, "\n")
	if len(lines) != m.h {
		t.Errorf("%s: %d строк, а терминал %d", name, len(lines), m.h)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w > m.w {
			t.Errorf("%s: строка %d шириной %d > %d: %q", name, i, w, m.w, ansi.Strip(l))
		}
	}
	if os.Getenv("GW_DUMP") != "" {
		fmt.Printf("\n===== %s (%dx%d) =====\n%s\n", name, m.w, m.h, out)
	}
	return ansi.Strip(out)
}

func TestScreens(t *testing.T) {
	for _, size := range [][2]int{{140, 48}, {140, 36}, {90, 30}} {
		m, _ := newTestModel(t, size[0], size[1])
		name := func(s string) string { return fmt.Sprintf("%s %dx%d", s, size[0], size[1]) }

		if s := screen(t, m, name("dashboard")); !strings.Contains(s, "@octocat") || !strings.Contains(s, "CI упал") {
			t.Errorf("dashboard без профиля или ленты:\n%s", s)
		}
		press(m, "2")
		if s := screen(t, m, name("repos")); !strings.Contains(s, "◉ web") {
			t.Errorf("repos:\n%s", s)
		}
		press(m, "enter") // выбрать первый репозиторий → вкладка PR
		if m.tab != tabPRs {
			t.Fatalf("enter на репозитории должен открыть PR, вкладка %d", m.tab)
		}
		if s := screen(t, m, name("prs")); !strings.Contains(s, "Добавить кэш") {
			t.Errorf("prs:\n%s", s)
		}
		press(m, "4")
		screen(t, m, name("issues"))
		press(m, "5")
		screen(t, m, name("actions"))
		press(m, "enter")
		if s := screen(t, m, name("run logs")); !strings.Contains(s, "Process completed with exit code 1") {
			t.Errorf("логи:\n%s", s)
		}
		press(m, "esc", "6")
		screen(t, m, name("events"))
		press(m, "7")
		screen(t, m, name("logs"))
		press(m, "?")
		screen(t, m, name("help"))
	}
}

func TestActions(t *testing.T) {
	m, gh := newTestModel(t, 120, 30)

	press(m, "3", "j", "m") // второй PR → merge → окно подтверждения
	if m.confirm == nil {
		t.Fatal("merge без подтверждения")
	}
	screen(t, m, "confirm")
	press(m, "y")
	if len(gh.merged) != 1 || gh.merged[0] != 6 {
		t.Fatalf("merged = %v, want [6]", gh.merged)
	}

	press(m, "4", "c")
	for _, r := range "lgtm" {
		press(m, string(r))
	}
	screen(t, m, "prompt")
	press(m, "enter")
	if len(gh.comments) != 1 || gh.comments[0] != "lgtm" {
		t.Fatalf("comments = %v", gh.comments)
	}

	press(m, "x", "n") // отказ — ничего не закрываем
	if len(gh.closed) != 0 {
		t.Fatal("issue закрыт без подтверждения")
	}

	press(m, "/", "к", "э", "enter") // фильтр по «кэ» на вкладке issues — ничего не найдёт
	if n := len(m.visible(tabIssues)); n != 0 {
		t.Fatalf("фильтр не применился: %d строк", n)
	}
	press(m, "esc")
	if n := len(m.visible(tabIssues)); n != 1 {
		t.Fatalf("esc не сбросил фильтр: %d строк", n)
	}
}

func (f *fakeGH) EventsSince(context.Context, time.Time) ([]domain.Event, error) {
	return []domain.Event{{
		SourceID: "e:1", Kind: domain.EventPush, Repo: "octocat/api", Actor: "octocat", CreatedAt: now,
		Title: "Push в main: 2 коммита · 3 файла · +40 −7",
		Body:  "• a1b2c3d Добавить кэш\n• d4e5f6a Тесты\n\n+30 −5  cache.go (новый)\n+10 −2  main.go",
	}}, nil
}

// без демона лента берётся прямо из GitHub, Enter на пуше открывает коммиты и файлы
func TestPushWithoutDaemon(t *testing.T) {
	gh := &fakeGH{}
	m := newModel(context.Background(), gh, daemonclient.New("http://127.0.0.1:1"), config.Config{}, "octocat/api")
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	_, cmd := m.Update(daemonMsg{}) // демон не ответил
	run(m, cmd)
	if len(m.events) != 1 {
		t.Fatalf("лента без демона не загрузилась: %d", len(m.events))
	}
	if s := screen(t, m, "dashboard без демона"); !strings.Contains(s, "Последний пуш") || !strings.Contains(s, "cache.go") {
		t.Errorf("нет панели пуша:\n%s", s)
	}
	press(m, "6", "enter")
	if s := screen(t, m, "пуш"); m.viewer == nil || !strings.Contains(s, "a1b2c3d") || !strings.Contains(s, "+30") {
		t.Errorf("подробности пуша:\n%s", s)
	}
}

func TestTerminal(t *testing.T) {
	m, _ := newTestModel(t, 120, 40)
	m.term.dir = t.TempDir()
	m.term.lines = nil

	typeLine := func(s string) {
		for _, r := range s {
			press(m, string(r))
		}
	}
	press(m, ":")
	if !m.term.open || !m.term.focused {
		t.Fatal("`:` должен открыть терминал")
	}
	typeLine(`printf 'a\033[31mb\033[0m\n'; echo $((40+2))`)
	press(m, "enter")
	out := strings.Join(m.term.lines, "\n")
	if !strings.Contains(out, "a\x1b[31mb\x1b[0m") || !strings.Contains(out, "42") || !strings.Contains(out, "✓") {
		t.Fatalf("вывод (цвета должны сохраниться):\n%q", out)
	}
	screen(t, m, "терминал")

	typeLine("mkdir sub && cd sub")
	press(m, "enter") // mkdir выполнится, а cd в шелле не меняет папку панели — проверим встроенный cd
	typeLine("cd sub")
	press(m, "enter")
	if !strings.HasSuffix(m.term.dir, "sub") {
		t.Fatalf("cd: папка %q", m.term.dir)
	}

	typeLine("false")
	press(m, "enter")
	if last := ansi.Strip(m.term.lines[len(m.term.lines)-1]); !strings.Contains(last, "код 1") {
		t.Fatalf("код возврата: %q", last)
	}

	// ctrl+c в терминале не закрывает gw
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("ctrl+c в терминале закрыл gw")
		}
	}
	press(m, "esc")
	if m.term.open {
		t.Fatal("esc должен скрыть терминал")
	}
}

func TestClean(t *testing.T) {
	cases := map[string]string{
		"progress 10%\rprogress 100%\r": "progress 100%",
		"\x1b[32mok\x1b[0m\x1b[K":       "\x1b[32mok\x1b[0m",
		"\x1b]0;title\x07text":          "text",
		"abc\bd":                        "abd",
		"tab\there":                     "tab    here",
	}
	for in, want := range cases {
		if got := clean(in); got != want {
			t.Errorf("clean(%q) = %q, want %q", in, got, want)
		}
	}
}
