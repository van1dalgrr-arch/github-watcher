package tui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/creack/pty"

	"github.com/van1dalgrr-arch/github-watcher/internal/ui"
)

// term — терминал внутри TUI. Их три: панель внизу (`:`) и два на Dashboard — git и демон.
// Команды идут через настоящий PTY, поэтому git, go test и прочие сами раскрашивают вывод,
// как в обычном терминале. Интерактивные полноэкранные программы (vim, htop) здесь не работают —
// для них есть обычный терминал.
type term struct {
	id            termID
	open, focused bool
	dir           string
	in            textinput.Model
	vp            viewport.Model
	cols, rows    int                     // размер PTY — по размеру панели (layout)
	repo          string                  // git-терминал: чей клон ищем
	clone, moved  bool                    // git-терминал: папка — клон repo; ушли из неё через cd
	format        func([]string) []string // как показывать вывод; nil — как есть

	lines   []string // готовые строки вывода (уже без лишних escape-последовательностей)
	partial string   // текущая, ещё не законченная строка
	pending []byte   // хвост чанка: недописанный UTF-8 или escape-последовательность

	cmd     *exec.Cmd
	ptmx    *os.File
	out     chan []byte
	started time.Time

	history []string
	hpos    int
}

// maxTermLines — сколько строк вывода держим в памяти: длинные логи не раздувают процесс.
const maxTermLines = 2000

// termID — какой из терминалов прислал вывод.
type termID int

const (
	termBottom termID = iota // панель внизу по `:`
	termGit                  // Dashboard: команды для git
	termDaemon               // Dashboard: gw daemon
)

type (
	termOutMsg struct {
		id   termID
		data []byte
	}
	termDoneMsg struct {
		id   termID
		code int
		dur  time.Duration
	}
)

func newTerm(id termID, placeholder string) term {
	dir, _ := os.Getwd()
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = placeholder
	in.CharLimit = 4000
	steadyCursor(&in)
	return term{id: id, dir: dir, in: in, vp: viewport.New(), cols: 80, rows: 20}
}

// resize — новый размер области вывода; работающей команде сообщаем его через PTY.
func (t *term) resize(cols, rows int) {
	cols, rows = max(cols, 20), max(rows, 3)
	t.vp.SetWidth(cols)
	t.vp.SetHeight(rows)
	if cols == t.cols && rows == t.rows {
		return
	}
	t.cols, t.rows = cols, rows
	if t.ptmx != nil {
		_ = pty.Setsize(t.ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	}
	t.refresh()
}

func (t *term) running() bool { return t.cmd != nil }

// prompt — «~/dev/github-watcher $» как в шелле.
func (t *term) prompt() string {
	dir := t.dir
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(dir, home) {
		dir = "~" + strings.TrimPrefix(dir, home)
	}
	return ui.Info.Render(dir) + sKey.Render(" $ ")
}

func (t *term) println(s string) {
	t.lines = append(t.lines, s)
	if over := len(t.lines) - maxTermLines; over > 0 {
		t.lines = t.lines[over:]
	}
}

func (t *term) refresh() {
	follow := t.vp.AtBottom() || t.running()
	content := t.lines
	if t.partial != "" {
		content = append(content[:len(content):len(content)], clean(t.partial))
	}
	if t.format != nil {
		content = t.format(content)
	}
	t.vp.SetContentLines(content)
	if follow {
		t.vp.GotoBottom()
	}
}

// run выполняет строку: встроенные cd / clear / exit — сами, остальное — в шелле через PTY.
func (t *term) run(line string) tea.Cmd {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	if len(t.history) == 0 || t.history[len(t.history)-1] != line {
		t.history = append(t.history, line)
	}
	t.hpos = len(t.history)
	t.println(t.prompt() + line)

	switch fields := strings.Fields(line); fields[0] {
	case "clear":
		t.lines = nil
		t.refresh()
		return nil
	case "exit":
		t.open, t.focused = false, false
		return nil
	case "cd":
		target := "~"
		if len(fields) > 1 {
			target = fields[1]
		}
		if err := t.cd(target); err != nil {
			t.println(ui.Bad.Render("cd: " + err.Error()))
		} else {
			t.moved = true // дальше папку выбираешь ты, а не смена репозитория
		}
		t.refresh()
		return nil
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell, "-c", line)
	cmd.Dir = t.dir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "CLICOLOR_FORCE=1", "PAGER=cat", "GIT_PAGER="+gitPager())
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(t.cols), Rows: uint16(t.rows)})
	if err != nil {
		t.println(ui.Bad.Render("не запустилось: " + err.Error()))
		t.refresh()
		return nil
	}
	t.cmd, t.ptmx, t.started = cmd, ptmx, time.Now()
	t.out = make(chan []byte, 64)
	go func(ptmx *os.File, out chan<- []byte) {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				out <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				close(out)
				return
			}
		}
	}(ptmx, t.out)
	t.refresh()
	return t.wait()
}

// gitPager — без пролистывания: less ждал бы q, а в панели его не нажать. Если git настроен
// на delta — остаётся delta с её цветами, только без пейджера.
var gitPager = sync.OnceValue(func() string {
	out, _ := exec.Command("git", "config", "core.pager").Output()
	if strings.Contains(string(out), "delta") {
		return "delta --paging=never"
	}
	return "cat"
})

// wait — следующий кусок вывода или сообщение о завершении команды.
func (t *term) wait() tea.Cmd {
	id, out, cmd, started := t.id, t.out, t.cmd, t.started
	return func() tea.Msg {
		if data, ok := <-out; ok {
			return termOutMsg{id, data}
		}
		code := 0
		var exitErr *exec.ExitError
		if err := cmd.Wait(); errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else if err != nil {
			code = -1
		}
		return termDoneMsg{id: id, code: code, dur: time.Since(started)}
	}
}

func (t *term) done(msg termDoneMsg) {
	if t.partial != "" {
		t.println(clean(t.partial))
		t.partial = ""
	}
	_ = t.ptmx.Close()
	t.cmd, t.ptmx, t.out, t.pending = nil, nil, nil, nil
	took := msg.dur.Round(100 * time.Millisecond).String()
	if msg.code == 0 {
		t.println(ui.Faint.Render("✓ " + took))
	} else {
		t.println(ui.Bad.Render("✗ код "+strconv.Itoa(msg.code)) + ui.Faint.Render(" · "+took))
	}
	t.refresh()
}

// interrupt — Ctrl+C: SIGINT всей группе процессов команды (PTY запускает её в своей сессии).
func (t *term) interrupt() {
	if t.cmd != nil && t.cmd.Process != nil {
		_ = syscall.Kill(-t.cmd.Process.Pid, syscall.SIGINT)
	}
}

// send — строка на stdin работающей команды (ответить y/n, ввести пароль и т.п.).
func (t *term) send(line string) {
	if t.ptmx != nil {
		_, _ = t.ptmx.WriteString(line + "\n")
	}
}

func (t *term) cd(target string) error {
	if target == "~" || strings.HasPrefix(target, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		target = filepath.Join(home, strings.TrimPrefix(target, "~"))
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(t.dir, target)
	}
	st, err := os.Stat(target)
	if err != nil {
		return errors.New("нет такой папки: " + target)
	}
	if !st.IsDir() {
		return errors.New("это не папка: " + target)
	}
	t.dir = filepath.Clean(target)
	return nil
}

// feed разбирает поток из PTY на строки: \r\n — конец строки, одинокий \r — перерисовка строки
// (прогресс-бары), \b — шаг назад. Хвост с недописанным символом откладывается до следующего чанка.
func (t *term) feed(data []byte) {
	data = append(t.pending, data...)
	t.pending = nil
	if cut := incompleteTail(data); cut < len(data) {
		t.pending = append([]byte(nil), data[cut:]...)
		data = data[:cut]
	}
	parts := strings.Split(t.partial+string(data), "\n")
	for _, l := range parts[:len(parts)-1] {
		t.println(clean(l))
	}
	t.partial = parts[len(parts)-1]
	t.refresh()
}

// incompleteTail — где начинается недописанный хвост: оборванный UTF-8 или escape без конца.
func incompleteTail(b []byte) int {
	if i := strings.LastIndexByte(string(b), 0x1b); i >= 0 && !escComplete.Match(b[i:]) {
		return i
	}
	for i := len(b) - 1; i >= 0 && i >= len(b)-3; i-- {
		if utf8.RuneStart(b[i]) {
			if !utf8.FullRune(b[i:]) {
				return i
			}
			break
		}
	}
	return len(b)
}

var (
	escComplete = regexp.MustCompile(`^\x1b(\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(\x07|\x1b\\)|[()][0-9A-Za-z]|[=>78DEHMc])`)
	ansiCSI     = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	ansiOther   = regexp.MustCompile(`\x1b(\][^\x07\x1b]*(\x07|\x1b\\)|[()][0-9A-Za-z]|[=>78DEHMc])`)
)

// clean оставляет в строке только цвета (SGR): перемещения курсора, заголовки окна и прочее
// в панели смысла не имеют и ломали бы вёрстку.
func clean(l string) string {
	l = strings.TrimSuffix(l, "\r")
	if i := strings.LastIndexByte(l, '\r'); i >= 0 {
		l = l[i+1:]
	}
	l = ansiOther.ReplaceAllString(l, "")
	l = ansiCSI.ReplaceAllStringFunc(l, func(s string) string {
		if strings.HasSuffix(s, "m") {
			return s
		}
		return ""
	})
	for strings.Contains(l, "\b") {
		i := strings.IndexByte(l, '\b')
		if i == 0 {
			l = l[1:]
			continue
		}
		_, size := utf8.DecodeLastRuneInString(l[:i])
		l = l[:i-size] + l[i+1:]
	}
	return strings.ReplaceAll(l, "\t", "    ")
}
