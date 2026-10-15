package tui

import (
	"context"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/van1dalgrr-arch/github-watcher/internal/ui"
)

// page — страница поверх вкладок, как на github.com: PR, issue, репозиторий, коммит.
// Страницы складываются стопкой (репозиторий → коммит), esc — на шаг назад.
// Внутри — разделы («Обсуждение», «Файлы»…), переключаются цифрами или tab.
type page struct {
	id       int
	title    string
	url      string
	header   []string // под заголовком: статус, ветки, цифры
	sections []*section
	cur      int
	err      string
	ready    bool
	live     bool // что-то ещё идёт (запуск Actions) — обновлять раз в 10 секунд

	build func(p *page, w int)                             // перестроить содержимое под ширину (данные уже в замыкании)
	keys  []pageKey                                        // действия страницы: approve, merge, комментарий…
	load  func(ctx context.Context) (func(p *page), error) // загрузка — и для обновления после действия
}

// section — раздел страницы: документ (листается) или список (enter — открыть).
type section struct {
	name  string
	badge string
	doc   []string
	items []pageItem
	list  list
	vp    viewport.Model
}

type pageItem struct {
	render func(w int, sel bool) string
	open   func(m *Model) tea.Cmd
	url    string
}

type pageKey struct {
	key, hint string
	do        func(m *Model) tea.Cmd
}

// pageMsg — данные страницы пришли: fill кладёт их в страницу и задаёт build.
type pageMsg struct {
	id   int
	fill func(p *page)
}

func (m *Model) topPage() *page {
	if len(m.pages) == 0 {
		return nil
	}
	return m.pages[len(m.pages)-1]
}

// openPage кладёт пустую страницу на стопку и грузит данные.
func (m *Model) openPage(title, url string, load func(ctx context.Context) (func(p *page), error)) tea.Cmd {
	m.pageSeq++
	p := &page{id: m.pageSeq, title: title, url: url, load: load}
	m.pages = append(m.pages, p)
	m.flash = ""
	return m.fetchPage(p)
}

func (m *Model) fetchPage(p *page) tea.Cmd {
	id, load := p.id, p.load
	return m.load(func(ctx context.Context) (tea.Msg, error) {
		fill, err := load(ctx)
		if err != nil {
			e := err.Error()
			return pageMsg{id: id, fill: func(p *page) { p.err = e }}, nil
		}
		return pageMsg{id: id, fill: fill}, nil
	})
}

// fillPage — данные пришли: в страницу и под текущую ширину.
func (m *Model) fillPage(msg pageMsg) {
	p := m.pageByID(msg.id)
	if p == nil {
		return // страницу уже закрыли
	}
	p.err = ""
	msg.fill(p)
	p.ready = true
	m.rebuild(p)
}

func (m *Model) pageByID(id int) *page {
	for _, p := range m.pages {
		if p.id == id {
			return p
		}
	}
	return nil
}

// pageBodyH — высота области раздела: всё, кроме заголовка, шапки и строки разделов.
func (m *Model) pageBodyH(p *page) int { return max(m.mainHeight()-len(p.header)-3, 3) }

// rebuild — перестроить страницу под текущую ширину, сохранив позицию в разделах.
func (m *Model) rebuild(p *page) {
	if p.build == nil {
		return
	}
	saved := make([]int, len(p.sections))
	for i, s := range p.sections {
		saved[i] = s.vp.YOffset()
	}
	cur := p.cur
	p.header, p.sections = nil, nil
	p.build(p, m.w)
	p.cur = min(cur, max(len(p.sections)-1, 0))
	h := m.pageBodyH(p)
	for i, s := range p.sections {
		s.vp = viewport.New(viewport.WithWidth(m.w), viewport.WithHeight(h))
		s.vp.SetContentLines(s.doc)
		if i < len(saved) {
			s.vp.SetYOffset(saved[i])
		}
	}
}

func (m *Model) pageView(p *page) string {
	out := []string{ui.Bold.Render(fit(p.title, m.w))}
	if !p.ready {
		return strings.Join(append(out, "", "  "+m.spin.View()+ui.Faint.Render(" загружаю…")), "\n")
	}
	if p.err != "" {
		return strings.Join(append(out, "", "  "+ui.Bad.Render(p.err), "", ui.Faint.Render("  esc — назад, o — открыть на сайте")), "\n")
	}
	for _, h := range p.header {
		out = append(out, ansi.Truncate(h, m.w, "…"))
	}
	var tabs []string
	for i, s := range p.sections {
		label := ui.Faint.Render(strconv.Itoa(i+1)) + " "
		if i == p.cur {
			label += sTabOn.Render(s.name)
		} else {
			label += sTabOff.Render(s.name)
		}
		if s.badge != "" {
			label += " " + s.badge
		}
		tabs = append(tabs, label)
	}
	out = append(out, ansi.Truncate(" "+strings.Join(tabs, "   "), m.w, ""), "")
	if len(p.sections) == 0 {
		return strings.Join(out, "\n")
	}
	s := p.sections[p.cur]
	h := m.pageBodyH(p)
	if s.items != nil {
		if len(s.items) == 0 {
			out = append(out, ui.Faint.Render("  пусто"))
		} else {
			out = append(out, s.list.render(len(s.items), h, func(i int, sel bool) string { return s.items[i].render(m.w, sel) }))
		}
	} else {
		s.vp.SetHeight(h)
		out = append(out, s.vp.View())
	}
	return strings.Join(out, "\n")
}

func (m *Model) pageKey(p *page, msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	switch k {
	case "esc", "q", "backspace":
		m.pages = m.pages[:len(m.pages)-1]
		m.flash = ""
		if top := m.topPage(); top != nil {
			m.rebuild(top) // пока была сверху другая, окно могли растянуть
		}
		return nil
	case "r":
		m.setFlash("обновляю…", false)
		return m.fetchPage(p)
	case "o":
		if s := m.curSection(p); s != nil && s.items != nil && len(s.items) > 0 && s.items[s.list.cursor].url != "" {
			return openURL(s.items[s.list.cursor].url)
		}
		return openURL(p.url)
	case "tab", "l", "right":
		if len(p.sections) > 0 {
			p.cur = (p.cur + 1) % len(p.sections)
		}
		return nil
	case "shift+tab", "h", "left":
		if len(p.sections) > 0 {
			p.cur = (p.cur + len(p.sections) - 1) % len(p.sections)
		}
		return nil
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		if n, _ := strconv.Atoi(k); n <= len(p.sections) {
			p.cur = n - 1
		}
		return nil
	}
	for _, pk := range p.keys {
		if pk.key == k && p.ready && p.err == "" {
			return pk.do(m)
		}
	}
	s := m.curSection(p)
	if s == nil {
		return nil
	}
	if s.items != nil {
		if k == "enter" || k == "space" {
			if len(s.items) > 0 && s.items[s.list.cursor].open != nil {
				return s.items[s.list.cursor].open(m)
			}
			return nil
		}
		s.list.key(k, len(s.items), m.pageBodyH(p))
		return nil
	}
	switch k {
	case "g", "home":
		s.vp.GotoTop()
	case "G", "end":
		s.vp.GotoBottom()
	case "j":
		s.vp.ScrollDown(1)
	case "k":
		s.vp.ScrollUp(1)
	case " ", "space", "ctrl+d":
		s.vp.HalfPageDown()
	case "ctrl+u":
		s.vp.HalfPageUp()
	default:
		var cmd tea.Cmd
		s.vp, cmd = s.vp.Update(msg)
		return cmd
	}
	return nil
}

func (m *Model) curSection(p *page) *section {
	if p.cur < len(p.sections) {
		return p.sections[p.cur]
	}
	return nil
}

func (m *Model) pageWheel(p *page, d int) {
	s := m.curSection(p)
	if s == nil {
		return
	}
	if s.items != nil {
		s.list.scroll(d, len(s.items), m.pageBodyH(p))
		return
	}
	if d < 0 {
		s.vp.ScrollUp(-d)
	} else {
		s.vp.ScrollDown(d)
	}
}

func (m *Model) pageHints(p *page) []string {
	var h []string
	for _, pk := range p.keys {
		h = append(h, keyHint(pk.key, pk.hint))
	}
	if s := m.curSection(p); s != nil && s.items != nil {
		h = append(h, keyHint("enter", "открыть"))
	}
	return append(h, keyHint("1…"+strconv.Itoa(max(len(p.sections), 1)), "разделы"), keyHint("o", "сайт"), keyHint("esc", "назад"))
}
