// Package ui — общий вид CLI и TUI: палитра, значки статусов, относительное время.
// Основной текст — цвет терминала, акценты — палитра berth.
package ui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
)

// Палитра — та же, что у berth (команда orbstack в dotfiles): фиолетовый → синий акцент.
var (
	Accent = lipgloss.Color("#a855f7")
	Blue   = lipgloss.Color("#3b82f6")
	Green  = lipgloss.Color("#22c55e")
	Red    = lipgloss.Color("#ef4444")
	Yellow = lipgloss.Color("#eab308")
	Purple = lipgloss.Color("#a855f7")
	Gray   = lipgloss.Color("#8a8a8a")
	White  = lipgloss.Color("#f0f0f0")
)

var (
	Bold   = lipgloss.NewStyle().Bold(true)
	Faint  = lipgloss.NewStyle().Foreground(Gray)
	Good   = lipgloss.NewStyle().Foreground(Green)
	Bad    = lipgloss.NewStyle().Foreground(Red)
	Warn   = lipgloss.NewStyle().Foreground(Yellow)
	Info   = lipgloss.NewStyle().Foreground(Blue)
	Violet = lipgloss.NewStyle().Foreground(Purple)
	Title  = lipgloss.NewStyle().Bold(true).Foreground(Accent)
)

// CI — значок статуса проверки: success / failure / pending / cancelled.
func CI(state string) string {
	switch state {
	case "success":
		return Good.Render("✓")
	case "failure":
		return Bad.Render("✗")
	case "pending":
		return Warn.Render("●")
	case "cancelled":
		return Faint.Render("⊘")
	default:
		return Faint.Render("·")
	}
}

// PRState — цветной значок PR, как на GitHub: зелёный открыт, фиолетовый смёржен, красный закрыт.
func PRState(p domain.PullRequest) string {
	switch {
	case p.State == "merged":
		return Violet.Render("◆")
	case p.State == "closed":
		return Bad.Render("◆")
	case p.Draft:
		return Faint.Render("◇")
	default:
		return Good.Render("◆")
	}
}

func IssueState(i domain.Issue) string {
	if i.State == "closed" {
		return Violet.Render("●")
	}
	return Good.Render("○")
}

// Kind — однобуквенный значок события: эмодзи в TUI ломают ширину колонок, поэтому только узкие символы.
func Kind(k domain.EventKind) string {
	switch k {
	case domain.EventPROpened:
		return Good.Render("◆")
	case domain.EventPRMerged:
		return Violet.Render("◆")
	case domain.EventPRReview:
		return Info.Render("◎")
	case domain.EventIssue:
		return Good.Render("○")
	case domain.EventComment:
		return Faint.Render("¶")
	case domain.EventCIFailed:
		return Bad.Render("✗")
	case domain.EventCIPassed:
		return Good.Render("✓")
	case domain.EventRelease:
		return Info.Render("▲")
	case domain.EventPush:
		return Info.Render("↑")
	case domain.EventMention:
		return Warn.Render("@")
	case domain.EventStar:
		return Warn.Render("★")
	default:
		return Faint.Render("•")
	}
}

// Ago — «5м», «3ч», «2д»: коротко, чтобы влезало в колонку.
func Ago(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "<1м"
	case d < time.Hour:
		return fmt.Sprintf("%dм", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dч", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dд", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dмес", int(d.Hours()/24/30))
	default:
		return fmt.Sprintf("%dг", int(d.Hours()/24/365))
	}
}

// Interval — "1m0s" / "15s" из статуса демона → "1м" / "15с".
func Interval(s string) string {
	d, err := time.ParseDuration(s)
	if err != nil {
		return s
	}
	if d%time.Minute == 0 {
		return fmt.Sprintf("%dм", int(d.Minutes()))
	}
	return fmt.Sprintf("%dс", int(d.Seconds()))
}

// Plural — русское склонение по числу: 1 репозиторий, 2 репозитория, 5 репозиториев.
func Plural(n int, one, few, many string) string { return domain.Plural(n, one, few, many) }

// LogLines раскрашивает лог GitHub Actions: заголовки job-ов, группы шагов, ошибки, предупреждения, команды.
// errs — номера строк с ошибками, чтобы TUI мог прыгать по ним.
func LogLines(text string) (out []string, errs []int) {
	for line := range strings.Lines(text) {
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, "\x00JOB "): // разделитель job-ов из github.RunLogs
			name, state, _ := strings.Cut(strings.TrimPrefix(line, "\x00JOB "), " · ")
			out = append(out, "", CI(state)+" "+Title.Render(name), Faint.Render(strings.Repeat("─", 60)))
		case strings.HasPrefix(line, "##[group]"):
			out = append(out, Info.Render("▸ ")+Bold.Render(strings.TrimPrefix(line, "##[group]")))
		case strings.HasPrefix(line, "##[endgroup]"):
		case strings.HasPrefix(line, "##[error]"):
			errs = append(errs, len(out))
			out = append(out, Bad.Render("✗ "+strings.TrimPrefix(line, "##[error]")))
		case strings.HasPrefix(line, "##[warning]"):
			out = append(out, Warn.Render("! "+strings.TrimPrefix(line, "##[warning]")))
		case strings.HasPrefix(line, "##[command]"):
			out = append(out, Info.Render("$ "+strings.TrimPrefix(line, "##[command]")))
		case strings.Contains(line, "--- FAIL") || strings.HasPrefix(line, "FAIL") || strings.HasPrefix(line, "panic:"):
			errs = append(errs, len(out))
			out = append(out, Bad.Render(line))
		case strings.HasPrefix(line, "--- PASS") || strings.HasPrefix(line, "ok  ") || line == "PASS":
			out = append(out, Good.Render(line))
		default:
			out = append(out, "  "+line)
		}
	}
	return out, errs
}
