package tui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"

	"github.com/van1dalgrr-arch/github-watcher/internal/ui"
)

// code — вкладка Code: файлы репозитория и код с подсветкой. Если gw запущен внутри клона
// этого репозитория — читаем с диска (видно и незакоммиченное), иначе — с GitHub без клонирования.
type code struct {
	repo  string // для какого репозитория загружен список
	local string // корень локального клона; "" — файлы с GitHub
	files []string

	path  string   // что показано в предпросмотре
	lines []string // подсвеченные строки предпросмотра
	err   string

	cache map[string][]string // последние открытые файлы — переключение по списку без повторных запросов
	order []string
	token int // защита от устаревших загрузок при быстрой прокрутке
}

// codeCacheSize — сколько подсвеченных файлов держать в памяти.
const codeCacheSize = 16

type (
	treeMsg struct {
		repo, local string
		files       []string
	}
	fileMsg struct {
		repo, path string
		lines      []string
	}
	previewMsg struct{ token int } // курсор задержался на файле — пора грузить
)

// localRoot — корень клона repo, если gw запущен внутри него.
func localRoot(repo string) string {
	top, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	m := remoteRe.FindStringSubmatch(strings.TrimSpace(string(out)))
	if m == nil || !strings.EqualFold(m[1]+"/"+m[2], repo) {
		return ""
	}
	return strings.TrimSpace(string(top))
}

func (m *Model) loadTree() tea.Cmd {
	repo := m.repo
	m.code = code{repo: repo, cache: m.code.cache}
	return m.load(func(ctx context.Context) (tea.Msg, error) {
		if root := localRoot(repo); root != "" {
			// git ls-files: только файлы проекта, без node_modules, bin и прочего из .gitignore
			out, err := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "--cached", "--others", "--exclude-standard").Output()
			if err == nil {
				return treeMsg{repo, root, strings.Fields(string(out))}, nil
			}
		}
		files, err := m.gh.Tree(ctx, repo)
		return treeMsg{repo, "", files}, err
	})
}

// schedulePreview — грузим файл, только если курсор задержался на нём: прокрутка списка не дёргает API.
func (m *Model) schedulePreview() tea.Cmd {
	m.code.token++
	tok := m.code.token
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg { return previewMsg{tok} })
}

func (m *Model) loadFile(path string) tea.Cmd {
	c := &m.code
	if lines, ok := c.cache[path]; ok {
		c.path, c.lines, c.err = path, lines, ""
		return nil
	}
	repo, local := c.repo, c.local
	return m.load(func(ctx context.Context) (tea.Msg, error) {
		var text string
		if local != "" {
			raw, err := os.ReadFile(filepath.Join(local, path))
			if err != nil {
				return nil, err
			}
			text = string(raw)
		} else {
			var err error
			if text, err = m.gh.File(ctx, repo, path); err != nil {
				return nil, err
			}
		}
		return fileMsg{repo, path, highlight(path, text)}, nil
	})
}

func (c *code) remember(path string, lines []string) {
	if c.cache == nil {
		c.cache = map[string][]string{}
	}
	if _, ok := c.cache[path]; !ok {
		c.order = append(c.order, path)
		if len(c.order) > codeCacheSize {
			delete(c.cache, c.order[0])
			c.order = c.order[1:]
		}
	}
	c.cache[path] = lines
}

// selectedFile — путь под курсором с учётом фильтра.
func (m *Model) selectedFile() string {
	vis := m.visible(tabCode)
	if cur := m.lists[tabCode].cursor; cur < len(vis) {
		return m.code.files[vis[cur]]
	}
	return ""
}

// ── подсветка ──────────────────────────────────────────────────────────────

const (
	maxHighlightBytes = 512 << 10 // больше — показываем без подсветки
	maxCodeLines      = 10000
)

// codeStyle — своя тема подсветки в палитре berth (фиолетовый → синий) на основе tokyonight.
// Своя, а не из chroma/styles: тот пакет при старте разбирает все ~70 тем — полтора-два мегабайта
// памяти ради одной. Фона нет — остаётся фон терминала.
var codeStyle = chroma.MustNewStyle("gw", chroma.StyleEntries{
	chroma.Text:                "#c0caf5",
	chroma.Error:               "#ef4444",
	chroma.Comment:             "italic #565f89",
	chroma.CommentPreproc:      "#7dcfff",
	chroma.Keyword:             "#a855f7",
	chroma.KeywordConstant:     "#ff9e64",
	chroma.KeywordType:         "#2ac3de",
	chroma.Name:                "#c0caf5",
	chroma.NameBuiltin:         "#2ac3de",
	chroma.NameFunction:        "#3b82f6",
	chroma.NameClass:           "#2ac3de",
	chroma.NameTag:             "#a855f7",
	chroma.NameAttribute:       "#7aa2f7",
	chroma.NameVariable:        "#c0caf5",
	chroma.NameDecorator:       "#ff9e64",
	chroma.LiteralString:       "#9ece6a",
	chroma.LiteralStringEscape: "#bb9af7",
	chroma.LiteralNumber:       "#ff9e64",
	chroma.Operator:            "#89ddff",
	chroma.Punctuation:         "#a9b1d6",
	chroma.GenericHeading:      "bold #3b82f6",
	chroma.GenericSubheading:   "bold #a855f7",
	chroma.GenericInserted:     "#22c55e",
	chroma.GenericDeleted:      "#ef4444",
	chroma.GenericEmph:         "italic",
	chroma.GenericStrong:       "bold",
})

// highlight раскрашивает файл построчно: многострочные комментарии и строки не теряют цвет на переносе.
func highlight(path, text string) []string {
	if strings.IndexByte(text[:min(len(text), 8000)], 0) >= 0 {
		return []string{ui.Faint.Render("двоичный файл — не показываю")}
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\t", "    ")
	plain := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(plain) > maxCodeLines {
		plain = append(plain[:maxCodeLines], ui.Faint.Render("… файл длиннее "+strconv.Itoa(maxCodeLines)+" строк"))
	}
	if len(text) > maxHighlightBytes {
		return plain
	}
	lexer := lexers.Match(path)
	if lexer == nil {
		lexer = lexers.Analyse(text)
	}
	if lexer == nil {
		return plain
	}
	it, err := chroma.Coalesce(lexer).Tokenise(nil, strings.Join(plain, "\n"))
	if err != nil {
		return plain
	}
	f := formatters.Get("terminal16m")
	var out []string
	for _, line := range chroma.SplitTokensIntoLines(it.Tokens()) {
		var b strings.Builder
		if err := f.Format(&b, codeStyle, chroma.Literator(line...)); err != nil {
			return plain
		}
		out = append(out, strings.TrimRight(b.String(), "\n"))
	}
	return out
}

// numbered — номера строк слева, как в редакторе.
func numbered(lines []string, from int) []string {
	w := len(strconv.Itoa(len(lines)))
	out := make([]string, 0, len(lines)-from)
	for i := from; i < len(lines); i++ {
		n := strconv.Itoa(i + 1)
		out = append(out, ui.Faint.Render(strings.Repeat(" ", w-len(n))+n+" │ ")+lines[i])
	}
	return out
}

// codeView — список файлов слева, предпросмотр справа.
func (m *Model) codeView() string {
	c := &m.code
	h := m.mainHeight()
	vis := m.visible(tabCode)
	leftW := min(max(m.w*2/5, 30), 60)
	rightW := m.w - leftW - 1

	src := "с GitHub"
	if c.local != "" {
		src = "с диска · " + c.local
	}
	title := " " + ui.Title.Render("Код · "+strconv.Itoa(len(vis))+" файлов") + ui.Faint.Render("  "+src)

	l := &m.lists[tabCode]
	left := l.render(len(vis), h-1, func(i int, sel bool) string {
		p := c.files[vis[i]]
		dir, file := filepath.Split(p)
		return row(sel, leftW, flex(ui.Faint.Render(dir)+file))
	})
	if len(vis) == 0 {
		empty := "  пусто"
		if m.loading > 0 {
			empty = "  " + m.spin.View() + " загружаю…"
		}
		left = ui.Faint.Render(empty)
	}

	var right []string
	switch {
	case c.err != "":
		right = []string{ui.Bad.Render(c.err)}
	case c.path != "":
		right = append([]string{ui.Bold.Render(c.path) + ui.Faint.Render("  · "+strconv.Itoa(len(c.lines))+" строк · enter — открыть")}, numbered(c.lines, 0)...)
	default:
		right = []string{ui.Faint.Render("выбери файл")}
	}
	for i := range right {
		right[i] = fit(right[i], rightW)
	}
	if len(right) > h-1 {
		right = right[:h-1]
	}

	leftBox := fitBox(left, leftW, h-1)
	rightBox := fitBox(strings.Join(right, "\n"), rightW, h-1)
	sep := strings.TrimRight(strings.Repeat(ui.Faint.Render("│")+"\n", h-1), "\n")
	return title + "\n" + joinCols(leftBox, sep, rightBox)
}

// fitBox — ровно h строк шириной w.
func fitBox(s string, w, h int) []string {
	lines := strings.Split(s, "\n")
	out := make([]string, h)
	for i := range out {
		if i < len(lines) {
			out[i] = fit(lines[i], w)
		} else {
			out[i] = strings.Repeat(" ", w)
		}
	}
	return out
}

func joinCols(left []string, sep string, right []string) string {
	seps := strings.Split(sep, "\n")
	out := make([]string, len(left))
	for i := range left {
		out[i] = left[i] + seps[i] + right[i]
	}
	return strings.Join(out, "\n")
}

var remoteRe = regexp.MustCompile(`github\.com[:/]([^/]+)/(.+?)(?:\.git)?/?$`)
