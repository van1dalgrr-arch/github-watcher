package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// Логотип в духе berth (команда orbstack в dotfiles): пиксельные буквы 5×5, каждый пиксель — две клетки,
// горизонтальный градиент фиолетовый → синий и «подтёки» краски снизу.
var glyphs = map[rune][5]string{
	'g': {" ####", "#    ", "#  ##", "#   #", " ### "},
	'i': {"#####", "  #  ", "  #  ", "  #  ", "#####"},
	't': {"#####", "  #  ", "  #  ", "  #  ", "  #  "},
	'h': {"#   #", "#   #", "#####", "#   #", "#   #"},
	'u': {"#   #", "#   #", "#   #", "#   #", " ### "},
	'b': {"#### ", "#   #", "#### ", "#   #", "#### "},
}

const logoWord = "github"

var logo = func() []string {
	rows := make([]string, 5)
	for i, ch := range logoWord {
		for r := range rows {
			if i > 0 {
				rows[r] += " "
			}
			rows[r] += glyphs[ch][r]
		}
	}
	return rows
}()

// bannerWidth/bannerHeight — сколько места занимает баннер (с отступом и тремя рядами подтёков).
var (
	bannerWidth  = 2 + 2*len(logo[0])
	bannerHeight = len(logo) + 3
)

func banner() string {
	cols := gradient("#a855f7", "#3b82f6", len(logo[0]))
	var b strings.Builder
	for _, row := range logo {
		b.WriteString("  ")
		for i, ch := range row {
			if ch == '#' {
				b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(cols[i])).Render("██"))
			} else {
				b.WriteString("  ")
			}
		}
		b.WriteByte('\n')
	}
	shades := []string{"▓▓", "▒▒", "░░"}
	bottom := logo[len(logo)-1]
	for k := 1; k <= 3; k++ {
		b.WriteString("  ")
		for i, ch := range bottom {
			drip := (i*7 + i/3*5) % 4 // длина подтёка от номера колонки, не случайная: не дёргается между кадрами
			if ch == '#' && k <= drip {
				b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(shade(cols[i], 4-k, 4))).Render(shades[k-1]))
			} else {
				b.WriteString("  ")
			}
		}
		if k < 3 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// wordmark — однострочный «github» с тем же градиентом, для шапки.
func wordmark() string {
	cols := gradient("#a855f7", "#3b82f6", len(logoWord))
	var b strings.Builder
	for i, ch := range logoWord {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(cols[i])).Bold(true).Render(string(ch)))
	}
	return b.String()
}

func gradient(from, to string, n int) []string {
	r1, g1, b1 := hexRGB(from)
	r2, g2, b2 := hexRGB(to)
	out := make([]string, n)
	for i := range out {
		t := 0.0
		if n > 1 {
			t = float64(i) / float64(n-1)
		}
		out[i] = fmt.Sprintf("#%02x%02x%02x", lerp(r1, r2, t), lerp(g1, g2, t), lerp(b1, b2, t))
	}
	return out
}

func shade(hex string, num, den int) string {
	r, g, b := hexRGB(hex)
	return fmt.Sprintf("#%02x%02x%02x", r*num/den, g*num/den, b*num/den)
}

func hexRGB(h string) (r, g, b int) {
	_, _ = fmt.Sscanf(strings.TrimPrefix(h, "#"), "%02x%02x%02x", &r, &g, &b)
	return r, g, b
}

func lerp(a, b int, t float64) int { return a + int(float64(b-a)*t+0.5) }
