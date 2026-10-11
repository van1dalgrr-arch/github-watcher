package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/van1dalgrr-arch/github-watcher/internal/ui"
)

var (
	sTabOn   = lipgloss.NewStyle().Bold(true).Foreground(ui.Accent).Underline(true)
	sTabOff  = lipgloss.NewStyle().Foreground(ui.Gray)
	sPanel   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(ui.Gray).Padding(0, 1)
	sModal   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(ui.Accent).Padding(1, 2)
	sSelBar  = lipgloss.NewStyle().Foreground(ui.Accent).Bold(true)
	sKey     = lipgloss.NewStyle().Foreground(ui.Accent).Bold(true)
	sSelText = lipgloss.NewStyle().Bold(true)
	sBadge   = lipgloss.NewStyle().Foreground(ui.White).Background(ui.Accent).Padding(0, 1)
)

// fit обрезает строку (с учётом ANSI) до w видимых символов и добивает пробелами.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = ansi.Truncate(s, w, "…")
	if pad := w - lipgloss.Width(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// cell — колонка строки списка. w == 0 — гибкая колонка, забирает всё оставшееся место.
type cell struct {
	s string
	w int
}

func c(s string, w int) cell { return cell{s, w} }
func flex(s string) cell     { return cell{s, 0} }

// row собирает строку списка: выделенная получает акцентную полоску слева и жирный текст гибкой колонки.
func row(selected bool, width int, cells ...cell) string {
	const gap = 1
	fixed := 2 // полоска выделения
	for _, cl := range cells {
		fixed += cl.w + gap
	}
	flexW := max(width-fixed+gap, 4)

	var b strings.Builder
	if selected {
		b.WriteString(sSelBar.Render("▌ "))
	} else {
		b.WriteString("  ")
	}
	for i, cl := range cells {
		w, s := cl.w, cl.s
		if w == 0 {
			w = flexW
			if selected {
				s = sSelText.Render(s)
			}
		}
		b.WriteString(fit(s, w))
		if i < len(cells)-1 {
			b.WriteString(strings.Repeat(" ", gap))
		}
	}
	return ansi.Truncate(b.String(), width, "")
}

// panel — рамка с заголовком; содержимое обрезается по ширине и высоте.
func panel(title string, w, h int, lines []string) string {
	inner := w - 4 // рамка + отступы
	out := []string{ui.Title.Render(title)}
	for _, l := range lines {
		if len(out) >= h-2 {
			break
		}
		out = append(out, fit(l, inner))
	}
	for len(out) < h-2 {
		out = append(out, "")
	}
	return sPanel.Width(w).Height(h).Render(strings.Join(out, "\n"))
}

func keyHint(k, what string) string { return sKey.Render(k) + " " + ui.Faint.Render(what) }
