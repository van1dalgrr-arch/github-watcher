package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"charm.land/glamour/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
	"github.com/van1dalgrr-arch/github-watcher/internal/ui"
)

// Markdown и diff — как их рисует github.com: заголовки, списки, код с подсветкой; diff с номерами строк.

var md struct {
	sync.Mutex
	w int
	r *glamour.TermRenderer
}

// htmlComment — <!-- шаблонные подсказки --> из PR и issue: на сайте их не видно, тут тоже.
var htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)

// markdown рисует текст под ширину w. Тёмная тема задана явно: автоопределение спросило бы
// цвет фона у терминала, а Bubble Tea в этот момент уже читает ввод.
func markdown(text string, w int) []string {
	text = strings.TrimSpace(htmlComment.ReplaceAllString(strings.ReplaceAll(text, "\r\n", "\n"), ""))
	if text == "" {
		return []string{ui.Faint.Render("  (без описания)")}
	}
	w = max(w, 20)
	md.Lock()
	defer md.Unlock()
	if md.r == nil || md.w != w {
		r, err := glamour.NewTermRenderer(glamour.WithStandardStyle("dark"), glamour.WithWordWrap(w-4), glamour.WithEmoji())
		if err != nil {
			return plainWrap(text, w)
		}
		md.r, md.w = r, w
	}
	out, err := md.r.Render(text)
	if err != nil {
		return plainWrap(text, w)
	}
	lines := strings.Split(strings.Trim(out, "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return lines
}

func plainWrap(text string, w int) []string {
	return strings.Split(ansi.Wordwrap(text, max(w-2, 20), ""), "\n")
}

// diffLines — файл из diff: шапка с +/−, дальше строки с номерами (старый / новый) и подсветкой синтаксиса.
func diffLines(f domain.FileDiff, w int) []string {
	status := map[string]string{"added": ui.Good.Render("A"), "removed": ui.Bad.Render("D"), "renamed": ui.Info.Render("R")}[f.Status]
	if status == "" {
		status = ui.Warn.Render("M")
	}
	name := ui.Bold.Render(f.Path)
	if f.PrevPath != "" && f.PrevPath != f.Path {
		name = ui.Faint.Render(f.PrevPath+" → ") + name
	}
	out := []string{
		status + " " + name + "  " + ui.Good.Render("+"+strconv.Itoa(f.Additions)) + " " + ui.Bad.Render("−"+strconv.Itoa(f.Deletions)),
	}
	if f.Patch == "" {
		return append(out, ui.Faint.Render("    diff не показан: двоичный файл или слишком большой — o откроет на сайте"))
	}
	return append(out, patchLines(f.Path, f.Patch, w)...)
}

var hunkRe = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@(.*)$`)

// patchLines раскрашивает unified diff: номера строк, + зелёным, − красным, код — подсветкой по языку файла.
func patchLines(path, patch string, w int) []string {
	raw := strings.Split(strings.TrimSuffix(strings.ReplaceAll(patch, "\r\n", "\n"), "\n"), "\n")
	// код без маркеров +/−/пробел подсвечиваем одним куском — многострочные конструкции не теряют цвет
	code := make([]string, len(raw))
	for i, l := range raw {
		if !strings.HasPrefix(l, "@@") && l != "" {
			code[i] = l[1:]
		}
	}
	hl := highlight(path, strings.Join(code, "\n"))
	if len(hl) != len(raw) { // подсветка не справилась — без неё
		hl = code
	}
	var out []string
	oldN, newN := 0, 0
	for i, l := range raw {
		if m := hunkRe.FindStringSubmatch(l); m != nil {
			oldN, _ = strconv.Atoi(m[1])
			newN, _ = strconv.Atoi(m[2])
			out = append(out, ui.Info.Render(fmt.Sprintf("    @@ −%s +%s @@", m[1], m[2]))+ui.Faint.Render(m[3]))
			continue
		}
		if l == "" || l == `\ No newline at end of file` {
			continue
		}
		var nums, mark string
		switch l[0] {
		case '+':
			nums = ui.Good.Render(fmt.Sprintf("%5s %5d ", "", newN))
			mark = ui.Good.Render("+ ")
			newN++
		case '-':
			nums = ui.Bad.Render(fmt.Sprintf("%5d %5s ", oldN, ""))
			mark = ui.Bad.Render("− ")
			oldN++
		default:
			nums = ui.Faint.Render(fmt.Sprintf("%5d %5d ", oldN, newN))
			mark = "  "
			oldN++
			newN++
		}
		out = append(out, ansi.Truncate(nums+ui.Faint.Render("│")+mark+hl[i], w, "…"))
	}
	return out
}
