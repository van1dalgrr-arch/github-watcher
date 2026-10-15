package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	reviews       []string
	created       []string
	cancelled     []int64
	read, done    []string
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
			case loaded, termOutMsg, termDoneMsg, previewMsg:
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

		s := screen(t, m, name("dashboard"))
		if !strings.Contains(s, "@octocat") || !strings.Contains(s, "демон") || !strings.Contains(s, "git") {
			t.Errorf("dashboard без профиля или терминалов:\n%s", s)
		}
		if size[0] >= 130 && !strings.Contains(s, "CI упал") { // лента — в широком окне
			t.Errorf("dashboard без ленты:\n%s", s)
		}
		press(m, "2")
		if s := screen(t, m, name("repos")); !strings.Contains(s, "◉ web") {
			t.Errorf("repos:\n%s", s)
		}
		press(m, "p") // выбрать первый репозиторий → вкладка PR
		if m.tab != tabPRs {
			t.Fatalf("p на репозитории должен открыть его PR, вкладка %d", m.tab)
		}
		if s := screen(t, m, name("prs")); !strings.Contains(s, "Добавить кэш") {
			t.Errorf("prs:\n%s", s)
		}
		press(m, "4")
		screen(t, m, name("issues"))
		press(m, "5")
		screen(t, m, name("actions"))
		press(m, "L")
		if s := screen(t, m, name("run logs")); !strings.Contains(s, "Process completed with exit code 1") {
			t.Errorf("логи:\n%s", s)
		}
		press(m, "esc", "6")
		screen(t, m, name("events"))
		press(m, "7")
		screen(t, m, name("logs"))
		run(m, m.loadInbox())
		press(m, "9")
		if s := screen(t, m, name("inbox")); !strings.Contains(s, "просят ревью") {
			t.Errorf("inbox:\n%s", s)
		}
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

func (f *fakeGH) Tree(context.Context, string) ([]string, error) {
	return []string{"main.go", "internal/store/store.go", "README.md"}, nil
}

func (f *fakeGH) File(_ context.Context, _, path string) (string, error) {
	if path == "main.go" {
		return "package main\n\n/* многострочный\n   комментарий */\nfunc main() {\n\tprintln(\"hi\")\n}\n", nil
	}
	return "# readme\n", nil
}

func TestCodeTab(t *testing.T) {
	m, _ := newTestModel(t, 140, 40)
	press(m, "8")
	if len(m.code.files) != 3 || m.code.local != "" {
		t.Fatalf("список файлов (с GitHub): %+v", m.code)
	}
	if m.code.path != "main.go" {
		t.Fatalf("предпросмотр первого файла не загрузился: %q", m.code.path)
	}
	out := screen(t, m, "code")
	if !strings.Contains(out, "package main") || !strings.Contains(out, "1 │") {
		t.Fatalf("предпросмотр:\n%s", out)
	}
	if !strings.Contains(m.render(), "\x1b[38;2;") {
		t.Fatal("нет подсветки синтаксиса (truecolor)")
	}
	// многострочный комментарий раскрашен и на второй строке
	if l := m.code.lines[3]; !strings.Contains(l, "\x1b[") {
		t.Fatalf("вторая строка комментария без цвета: %q", l)
	}

	press(m, "/", "r", "e", "a", "enter")
	if m.selectedFile() != "README.md" || m.code.path != "README.md" {
		t.Fatalf("фильтр файлов: выбран %q, показан %q", m.selectedFile(), m.code.path)
	}
	press(m, "enter")
	if m.viewer == nil || !strings.Contains(screen(t, m, "файл"), "# readme") {
		t.Fatal("enter должен открыть файл целиком")
	}
}

// Dashboard: enter — терминал git в папке репозитория, d — демон прямо в TUI
func TestDashTerminals(t *testing.T) {
	m, _ := newTestModel(t, 140, 44)
	m.Update(daemonMsg{}) // демона нет
	m.gitTerm.dir = t.TempDir()

	press(m, "enter")
	if !m.gitTerm.focused {
		t.Fatal("enter на Dashboard должен открыть терминал git")
	}
	for _, r := range "git init -q . && git status --short && echo done" {
		press(m, string(r))
	}
	press(m, "enter")
	if out := strings.Join(m.gitTerm.lines, "\n"); !strings.Contains(out, "done") || !strings.Contains(out, "✓") {
		t.Fatalf("вывод git-терминала:\n%s", out)
	}
	if s := screen(t, m, "dash git"); !strings.Contains(s, "done") {
		t.Errorf("вывод не виден в панели:\n%s", s)
	}
	press(m, "q") // в терминале q — просто буква, gw не закрывается
	if !m.gitTerm.focused || m.gitTerm.in.Value() != "q" {
		t.Fatalf("q в терминале: focused=%v value=%q", m.gitTerm.focused, m.gitTerm.in.Value())
	}
	press(m, "esc")
	if m.gitTerm.focused {
		t.Fatal("esc должен вернуть к вкладкам")
	}

	m.daemonCmd = "echo 'level=INFO msg=демон-слушает'; echo 'level=ERROR msg=boom'"
	press(m, "d")
	if out := strings.Join(m.daemon2.lines, "\n"); !strings.Contains(out, "демон-слушает") {
		t.Fatalf("вывод демона:\n%s", out)
	}
	s := screen(t, m, "dash daemon")
	if !strings.Contains(s, "прошлый запуск") || !strings.Contains(s, "boom") {
		t.Errorf("панель демона после выхода:\n%s", s)
	}
}

// пока свой демон работает, q спрашивает подтверждение — он остановится вместе с gw
func TestQuitWithDaemon(t *testing.T) {
	m, _ := newTestModel(t, 140, 44)
	m.Update(daemonMsg{})
	m.daemonCmd = "sleep 30"
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	if !m.daemon2.running() {
		t.Fatal("демон не запустился")
	}
	_ = cmd // вывод не ждём: sleep молчит
	press(m, "q")
	if m.confirm == nil {
		t.Fatal("выход без подтверждения при работающем демоне")
	}
	press(m, "n")
	m.stopDaemon()
	deadline := time.Now().Add(3 * time.Second)
	for m.daemon2.running() && time.Now().Before(deadline) {
		run(m, m.daemon2.wait())
	}
	if m.daemon2.running() {
		t.Fatal("демон не остановился по SIGINT")
	}
}

func (f *fakeGH) PR(_ context.Context, repo string, n int) (domain.PRDetail, error) {
	return domain.PRDetail{
		PullRequest: domain.PullRequest{Repo: repo, Number: n, Title: "Добавить кэш", Author: "alice", State: "open",
			Head: "feat/cache", Base: "main", Body: "## Зачем\n\nЗапросы к **базе** медленные.\n\n- кэш на 5 минут\n- `go test` зелёный\n<!-- шаблон -->",
			UpdatedAt: now},
		Additions: 30, Deletions: 5, ChangedFiles: 1, Mergeable: "clean", HeadSHA: "a1b2c3d4", Labels: []string{"perf"},
		Comments: []domain.Comment{
			{Kind: domain.CommentPlain, Author: "bob", Body: "А TTL настраивается?", CreatedAt: now.Add(-2 * time.Hour)},
			{Kind: domain.CommentLine, Author: "bob", Path: "cache.go", Line: 3, Body: "здесь гонка",
				DiffHunk: "@@ -0,0 +1,3 @@\n+package cache\n+\n+var m = map[string]string{}", CreatedAt: now.Add(-time.Hour)},
			{Kind: domain.CommentReview, Author: "carol", State: "APPROVED", CreatedAt: now},
		},
		Commits: []domain.Commit{{SHA: "a1b2c3d4e5", Message: "Добавить кэш\n\nподробности", Author: "alice", Date: now}},
		Files: []domain.FileDiff{{Path: "cache.go", Status: "added", Additions: 3,
			Patch: "@@ -0,0 +1,3 @@\n+package cache\n+\n+var m = map[string]string{}"}},
		Checks: []domain.Check{{Name: "test", Status: "completed", Conclusion: "success", Took: 42 * time.Second}},
	}, nil
}

func (f *fakeGH) Issue(_ context.Context, repo string, n int) (domain.IssueDetail, error) {
	return domain.IssueDetail{
		Issue:    domain.Issue{Repo: repo, Number: n, Title: "Падает на пустом конфиге", Author: "carol", State: "open", Body: "Шаги: `gw` без конфига", UpdatedAt: now},
		Comments: []domain.Comment{{Kind: domain.CommentPlain, Author: "alice", Body: "воспроизвела", CreatedAt: now}},
	}, nil
}

func (f *fakeGH) Repo(_ context.Context, repo string) (domain.RepoDetail, error) {
	owner, name, _ := strings.Cut(repo, "/")
	return domain.RepoDetail{
		Repo:  domain.Repo{Owner: owner, Name: name, Description: "Backend", Stars: 12, PushedAt: now},
		Forks: 3, DefaultBranch: "main", License: "MIT", Topics: []string{"go"},
		Readme:    "# API\n\nСервис **заказов**.",
		Branches:  []string{"main", "feat/cache"},
		Commits:   []domain.Commit{{SHA: "ffff111", Message: "Свежий коммит", Author: "alice", Date: now}, {SHA: "eeee000", Message: "Старый", Author: "bob", Date: now.Add(-time.Hour)}},
		Languages: []domain.Lang{{Name: "Go", Bytes: 900}, {Name: "Shell", Bytes: 100}},
	}, nil
}

func (f *fakeGH) Commit(_ context.Context, _, sha string) (domain.Commit, error) {
	return domain.Commit{SHA: sha, Message: "Свежий коммит\n\nс подробностями", Author: "alice", Date: now, Additions: 1, Deletions: 1,
		Files: []domain.FileDiff{{Path: "main.go", Status: "modified", Additions: 1, Deletions: 1, Patch: "@@ -1,2 +1,2 @@\n package main\n-var x = 1\n+var x = 2"}}}, nil
}

func (f *fakeGH) Review(_ context.Context, _ string, _ int, event, body string) error {
	f.reviews = append(f.reviews, event+":"+body)
	return nil
}

func (f *fakeGH) ReopenIssue(context.Context, string, int) error { return nil }

// страница PR: обсуждение в markdown, замечание к строке с куском diff, коммиты, файлы, проверки, approve
func TestPRPage(t *testing.T) {
	m, gh := newTestModel(t, 140, 44)
	press(m, "3", "enter")
	p := m.topPage()
	if p == nil || !p.ready {
		t.Fatal("enter на PR должен открыть его страницу")
	}
	s := screen(t, m, "pr page")
	for _, want := range []string{"#7 Добавить кэш", "хочет влить 1 коммит", "+30", "можно мёржить", "perf", "Зачем", "кэш на 5 минут", "А TTL настраивается?", "cache.go:3", "здесь гонка", "одобрил(а)"} {
		if !strings.Contains(s, want) {
			t.Errorf("на странице PR нет %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "шаблон") {
		t.Error("HTML-комментарий шаблона должен быть скрыт")
	}
	press(m, "3")
	if s := screen(t, m, "pr files"); !strings.Contains(s, "A cache.go") || !strings.Contains(s, "+ package cache") {
		t.Errorf("файлы PR:\n%s", s)
	}
	press(m, "4")
	if s := screen(t, m, "pr checks"); !strings.Contains(s, "test") || !strings.Contains(s, "42s") {
		t.Errorf("проверки:\n%s", s)
	}
	press(m, "2", "enter") // коммит → его страница поверх PR
	if len(m.pages) != 2 || !strings.Contains(screen(t, m, "commit"), "var x = 2") {
		t.Fatalf("страница коммита: стопка %d", len(m.pages))
	}
	press(m, "esc")
	if m.topPage() != p {
		t.Fatal("esc должен вернуть на страницу PR")
	}

	press(m, "a", "y")
	if len(gh.reviews) != 1 || gh.reviews[0] != "APPROVE:" {
		t.Fatalf("approve: %v", gh.reviews)
	}
	press(m, "R")
	for _, r := range "добавь тест" {
		press(m, string(r))
	}
	press(m, "enter")
	if len(gh.reviews) != 2 || gh.reviews[1] != "REQUEST_CHANGES:добавь тест" {
		t.Fatalf("request changes: %v", gh.reviews)
	}
	press(m, "esc")
	if m.topPage() != nil || m.tab != tabPRs {
		t.Fatal("esc со страницы — обратно к списку PR")
	}
}

func TestIssueAndRepoPages(t *testing.T) {
	m, gh := newTestModel(t, 120, 40)
	press(m, "4", "enter")
	if s := screen(t, m, "issue page"); !strings.Contains(s, "Падает на пустом конфиге") || !strings.Contains(s, "воспроизвела") {
		t.Errorf("страница issue:\n%s", s)
	}
	press(m, "c")
	for _, r := range "ок" {
		press(m, string(r))
	}
	press(m, "enter")
	if len(gh.comments) != 1 || gh.comments[0] != "ок" {
		t.Fatalf("комментарий со страницы issue: %v", gh.comments)
	}
	press(m, "esc", "2", "enter")
	s := screen(t, m, "repo page")
	for _, want := range []string{"octocat/api", "★ 12", "MIT", "Go 90%", "#go", "API", "заказов"} {
		if !strings.Contains(s, want) {
			t.Errorf("на странице репозитория нет %q:\n%s", want, s)
		}
	}
	press(m, "2")
	if s := screen(t, m, "repo commits"); strings.Index(s, "Свежий коммит") > strings.Index(s, "Старый") {
		t.Errorf("коммиты репозитория — новые сверху:\n%s", s)
	}
	press(m, "p")
	if m.topPage() != nil || m.tab != tabPRs || m.repo != "octocat/api" {
		t.Fatalf("p со страницы репозитория: вкладка %d, репо %s", m.tab, m.repo)
	}
}

func (f *fakeGH) CreatePR(_ context.Context, repo, head, base, title, body string, draft bool) (domain.PullRequest, error) {
	f.created = append(f.created, fmt.Sprintf("%s→%s|%s|%s|draft=%v", head, base, title, body, draft))
	return domain.PullRequest{Repo: repo, Number: 8, Title: title, State: "open"}, nil
}

// новый PR: ветка не на GitHub → push в терминале → заготовка из коммитов → d — черновиком → страница PR
func TestNewPR(t *testing.T) {
	sh := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	root := t.TempDir()
	origin, clone := filepath.Join(root, "origin.git"), filepath.Join(root, "api")
	sh(root, "init", "-q", "--bare", "-b", "main", origin)
	sh(root, "clone", "-q", origin, clone)
	sh(clone, "commit", "-q", "--allow-empty", "-m", "init")
	sh(clone, "push", "-q", "origin", "main")
	sh(clone, "remote", "set-head", "origin", "main")
	sh(clone, "switch", "-q", "-c", "feat/cache")
	sh(clone, "commit", "-q", "--allow-empty", "-m", "Добавить кэш", "-m", "TTL — 5 минут")

	m, gh := newTestModel(t, 140, 44)
	m.findClone = func(string) string { return clone }
	press(m, "3", "n")
	if m.confirm == nil || !strings.Contains(m.confirm.question, "feat/cache") {
		t.Fatalf("незапушенная ветка — сначала вопрос про push: %+v", m.confirm)
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	run(m, cmd) // git push в терминале, после него — снова newPR, который зовёт редактор
	if out := strings.Join(m.term.lines, "\n"); !strings.Contains(out, "git push -u origin") {
		t.Fatalf("push не виден в терминале:\n%s", out)
	}
	if gitOut(clone, "rev-parse", "--abbrev-ref", "@{upstream}") != "origin/feat/cache" {
		t.Fatal("ветка не запушилась")
	}
	file := filepath.Join(os.TempDir(), "gw-pr-feat-cache.md")
	tpl, err := os.ReadFile(file)
	if err != nil || !strings.HasPrefix(string(tpl), "Добавить кэш\n\nTTL — 5 минут") || !strings.Contains(string(tpl), "<!--") {
		t.Fatalf("заготовка PR: %q %v", tpl, err)
	}
	// «пользователь» дописал описание в редакторе и закрыл его
	if err := os.WriteFile(file, []byte("Кэш для заказов\n\nTTL — 5 минут.\n\n<!-- подсказка -->\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, cmd = m.Update(editedMsg{file: file, repo: "octocat/api", head: "feat/cache", base: "main"})
	run(m, cmd)
	if m.confirm == nil || len(m.confirm.alt) != 1 {
		t.Fatal("после редактора — подтверждение с вариантом «черновиком»")
	}
	screen(t, m, "new pr confirm")
	press(m, "d")
	if len(gh.created) != 1 || gh.created[0] != "feat/cache→main|Кэш для заказов|TTL — 5 минут.|draft=true" {
		t.Fatalf("создан: %v", gh.created)
	}
	if p := m.topPage(); p == nil || !strings.Contains(p.title, "#8") {
		t.Fatal("после создания — страница нового PR")
	}
}

func TestHumanBranch(t *testing.T) {
	for in, want := range map[string]string{"feat/cache-layer": "Cache layer", "fix_login": "Fix login", "x": "X"} {
		if got := humanBranch(in); got != want {
			t.Errorf("humanBranch(%q) = %q, want %q", in, got, want)
		}
	}
}

func (f *fakeGH) Run(_ context.Context, repo string, id int64) (domain.RunDetail, error) {
	st, concl := "completed", "failure"
	if id == 2 {
		st, concl = "in_progress", ""
	}
	return domain.RunDetail{
		WorkflowRun: domain.WorkflowRun{Repo: repo, ID: id, Name: "ci", Branch: "main", HeadSHA: "abc1234", Event: "push", Title: "Добавить кэш", Status: st, Conclusion: concl, UpdatedAt: now},
		Actor:       "alice", Took: 95 * time.Second,
		Jobs: []domain.Job{
			{ID: 11, Name: "lint", Status: "completed", Conclusion: "success", Took: 20 * time.Second,
				Steps: []domain.Step{{Name: "golangci-lint", Status: "completed", Conclusion: "success", Took: 15 * time.Second}}},
			{ID: 12, Name: "test", Status: st, Conclusion: concl, Took: 75 * time.Second,
				Steps: []domain.Step{{Name: "go test", Status: st, Conclusion: concl, Took: 70 * time.Second}}},
		},
		Artifacts: []domain.Artifact{{ID: 5, Name: "coverage", Size: 3 << 20}},
	}, nil
}

func (f *fakeGH) JobLog(context.Context, string, int64) (string, error) {
	return "go test ./...\n--- FAIL: TestCache\n##[error]Process completed with exit code 1.\n", nil
}

func (f *fakeGH) CancelRun(_ context.Context, _ string, id int64) error {
	f.cancelled = append(f.cancelled, id)
	return nil
}

// страница запуска: job-ы с упавшим шагом, шаги, лог job-а, артефакты; идущий запуск — X отменяет
func TestRunPage(t *testing.T) {
	m, gh := newTestModel(t, 140, 40)
	press(m, "5", "j", "enter") // второй в списке — упавший ci
	s := screen(t, m, "run page")
	for _, want := range []string{"ci", "@alice", "1m35s", "lint", "test", "упал: go test"} {
		if !strings.Contains(s, want) {
			t.Errorf("на странице запуска нет %q:\n%s", want, s)
		}
	}
	press(m, "2")
	if s := screen(t, m, "run steps"); !strings.Contains(s, "golangci-lint") || !strings.Contains(s, "1m10s") {
		t.Errorf("шаги:\n%s", s)
	}
	press(m, "3")
	if s := screen(t, m, "run artifacts"); !strings.Contains(s, "coverage") || !strings.Contains(s, "3.0 МБ") {
		t.Errorf("артефакты:\n%s", s)
	}
	press(m, "1", "j", "enter") // лог упавшего job-а — с переходом к ошибке
	if m.viewer == nil || len(m.viewer.errs) == 0 {
		t.Fatal("enter на job-е — его лог с ошибками")
	}
	press(m, "esc")
	if m.viewer != nil || m.topPage() == nil {
		t.Fatal("esc из лога — обратно на страницу запуска")
	}
	press(m, "X")
	if m.confirm != nil {
		t.Fatal("законченный запуск отменять нечего")
	}
	press(m, "esc", "j", "enter") // идущий deploy
	if p := m.topPage(); p == nil || !p.live {
		t.Fatal("идущий запуск должен обновляться сам")
	}
	press(m, "X", "y")
	if len(gh.cancelled) != 1 || gh.cancelled[0] != 2 {
		t.Fatalf("отмена: %v", gh.cancelled)
	}
}

func (f *fakeGH) Notifications(context.Context, bool) ([]domain.Notification, error) {
	return []domain.Notification{
		{ID: "1", Repo: "octocat/api", Type: "PullRequest", Title: "Добавить кэш", Reason: "review_requested", Unread: true, Number: 7, UpdatedAt: now},
		{ID: "2", Repo: "octocat/web", Type: "Issue", Title: "Падает на пустом конфиге", Reason: "mention", Unread: true, Number: 3, UpdatedAt: now},
		{ID: "3", Repo: "octocat/api", Type: "Release", Title: "v1.2.0", Reason: "subscribed", UpdatedAt: now.Add(-time.Hour)},
	}, nil
}
func (f *fakeGH) MarkRead(_ context.Context, id string) error {
	f.read = append(f.read, id)
	return nil
}
func (f *fakeGH) MarkDone(_ context.Context, id string) error {
	f.done = append(f.done, id)
	return nil
}
func (f *fakeGH) MarkAllRead(context.Context) error { return nil }

// входящие: счётчик в шапке, enter — страница PR и «прочитано», d — убрать
func TestInbox(t *testing.T) {
	m, gh := newTestModel(t, 140, 40)
	run(m, m.loadInbox())
	if s := screen(t, m, "header"); !strings.Contains(s, "✉ 2") {
		t.Errorf("в шапке нет счётчика непрочитанных:\n%s", strings.Split(s, "\n")[0])
	}
	press(m, "9", "enter")
	if p := m.topPage(); p == nil || !strings.Contains(p.title, "#7") {
		t.Fatal("enter на уведомлении о PR — его страница")
	}
	if len(gh.read) != 1 || gh.read[0] != "1" || m.unread() != 1 {
		t.Fatalf("прочитано: %v, непрочитанных %d", gh.read, m.unread())
	}
	press(m, "esc", "j", "d")
	if len(gh.done) != 1 || gh.done[0] != "2" || len(m.inbox) != 2 {
		t.Fatalf("d — убрать: %v, осталось %d", gh.done, len(m.inbox))
	}
}

func (f *fakeGH) Profile(_ context.Context, login string) (domain.Profile, error) {
	var weeks [][]domain.Day
	start := time.Date(2025, 10, 19, 0, 0, 0, 0, time.UTC) // воскресенье
	for w := range 53 {
		var week []domain.Day
		for d := range 7 {
			lvl := (w + d) % 5
			week = append(week, domain.Day{Date: start.AddDate(0, 0, w*7+d), Count: lvl * 2, Level: lvl})
		}
		weeks = append(weeks, week)
	}
	return domain.Profile{
		Login: login, Name: "Octo Cat", Bio: "Пишу на Go", Location: "Москва", Followers: 21, Following: 3, PublicRepos: 2,
		Contributions: 1234, Weeks: weeks, CreatedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		Pinned:  []domain.Repo{{Owner: login, Name: "api", Stars: 12, Language: "Go", Description: "Backend"}},
		Repos:   []domain.Repo{{Owner: login, Name: "api", Stars: 12}, {Owner: login, Name: "web"}},
		Starred: []domain.Repo{{Owner: "golang", Name: "go", Stars: 120000}},
	}, nil
}

// профиль: сетка вкладов с месяцами, закреплённые; репозиторий из списка — его страница
func TestProfilePage(t *testing.T) {
	m, _ := newTestModel(t, 140, 40)
	press(m, "P")
	s := screen(t, m, "profile")
	for _, want := range []string{"Octo Cat", "21 подписчик", "Москва", "на GitHub с 2020", "Пишу на Go", "1234 вклада за год", "меньше", "больше", "окт", "пн", "Закреплённые", "octocat/api"} {
		if !strings.Contains(s, want) {
			t.Errorf("в профиле нет %q:\n%s", want, s)
		}
	}
	if !strings.Contains(m.render(), "#39d353") && !strings.Contains(m.render(), "\x1b[38;2;57;211;83m") {
		t.Error("в сетке нет самых зелёных клеток")
	}
	press(m, "3", "enter")
	if p := m.topPage(); p == nil || !strings.Contains(p.title, "golang/go") {
		t.Fatal("enter на звезде — страница репозитория")
	}
}

func TestContribGraphNarrow(t *testing.T) {
	_, _ = newTestModel(t, 60, 30)
	gh := &fakeGH{}
	pr, _ := gh.Profile(context.Background(), "x")
	for _, w := range []int{40, 60, 200} {
		for _, l := range contribGraph(pr.Weeks, w) {
			if lw := lipgloss.Width(l); lw > w {
				t.Fatalf("ширина %d: строка %d > %d", w, lw, w)
			}
		}
	}
}
