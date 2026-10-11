package tui

import "strings"

// list — курсор и прокрутка для списка из n строк в окне высотой h.
type list struct {
	cursor, offset int
}

func (l *list) clamp(n, h int) {
	if n == 0 || h <= 0 {
		l.cursor, l.offset = 0, 0
		return
	}
	l.cursor = min(max(l.cursor, 0), n-1)
	if l.cursor < l.offset {
		l.offset = l.cursor
	}
	if l.cursor >= l.offset+h {
		l.offset = l.cursor - h + 1
	}
	l.offset = min(max(l.offset, 0), max(n-h, 0))
}

// key обрабатывает навигацию; false — клавиша не про список.
func (l *list) key(k string, n, h int) bool {
	switch k {
	case "up", "k":
		l.cursor--
	case "down", "j":
		l.cursor++
	case "pgup", "ctrl+u":
		l.cursor -= max(h/2, 1)
	case "pgdown", "ctrl+d":
		l.cursor += max(h/2, 1)
	case "home", "g":
		l.cursor = 0
	case "end", "G":
		l.cursor = n - 1
	default:
		return false
	}
	l.clamp(n, h)
	return true
}

func (l *list) scroll(d, n, h int) {
	l.cursor += d
	l.clamp(n, h)
}

// render рисует ровно h строк: видимое окно списка, остальное — пустые строки.
func (l *list) render(n, h int, line func(i int, selected bool) string) string {
	l.clamp(n, h)
	out := make([]string, 0, h)
	for i := l.offset; i < n && len(out) < h; i++ {
		out = append(out, line(i, i == l.cursor))
	}
	for len(out) < h {
		out = append(out, "")
	}
	return strings.Join(out, "\n")
}
