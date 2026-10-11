package cli

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// printTable печатает строки колонками; ширина считается по видимым символам, ANSI-цвета не мешают.
// Последняя колонка не выравнивается — в ней обычно длинный заголовок.
func printTable(rows [][]string) {
	if len(rows) == 0 {
		say(dim("пусто"))
		return
	}
	widths := make([]int, len(rows[0]))
	for _, r := range rows {
		for i, c := range r[:len(r)-1] {
			widths[i] = max(widths[i], lipgloss.Width(c))
		}
	}
	var b strings.Builder
	for _, r := range rows {
		for i, c := range r {
			b.WriteString(c)
			if i < len(r)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-lipgloss.Width(c)+2))
			}
		}
		b.WriteByte('\n')
	}
	_, _ = lipgloss.Print(b.String())
}
