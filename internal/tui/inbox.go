package tui

import (
	"context"
	"fmt"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
	"github.com/van1dalgrr-arch/github-watcher/internal/ui"
)

// Вкладка Inbox — уведомления GitHub, как колокольчик на сайте: кто просит ревью, где упомянули, что назначили.
// enter — открыть страницу PR или issue (и отметить прочитанным), d — убрать из входящих, M — всё прочитано.

type inboxMsg struct {
	all   bool
	items []domain.Notification
}

func (m *Model) loadInbox() tea.Cmd {
	all := m.inboxAll
	return m.load(func(ctx context.Context) (tea.Msg, error) {
		items, err := m.gh.Notifications(ctx, all)
		return inboxMsg{all, items}, err
	})
}

func (m *Model) unread() int {
	n := 0
	for _, it := range m.inbox {
		if it.Unread {
			n++
		}
	}
	return n
}

var reasonText = map[string]string{
	"review_requested":   "просят ревью",
	"approval_requested": "просят одобрить",
	"mention":            "упомянули",
	"team_mention":       "упомянули команду",
	"assign":             "назначили",
	"author":             "ты автор",
	"comment":            "комментарий",
	"ci_activity":        "CI",
	"state_change":       "статус",
	"subscribed":         "подписка",
	"manual":             "подписка",
	"security_alert":     "уязвимость",
	"invitation":         "приглашение",
}

func notifIcon(n domain.Notification) string {
	switch n.Type {
	case "PullRequest":
		return ui.Good.Render("◆")
	case "Issue":
		return ui.Info.Render("●")
	case "Release":
		return ui.Warn.Render("▲")
	case "CheckSuite", "WorkflowRun":
		return ui.Bad.Render("⚙")
	case "Discussion":
		return ui.Info.Render("✎")
	}
	return ui.Faint.Render("·")
}

func (m *Model) inboxRow(n domain.Notification, sel bool) string {
	dot := ui.Faint.Render("○")
	title := n.Title
	if n.Unread {
		dot = sKey.Render("●")
		title = ui.Bold.Render(title)
	}
	reason := reasonText[n.Reason]
	if reason == "" {
		reason = n.Reason
	}
	if n.Reason == "review_requested" || n.Reason == "mention" || n.Reason == "assign" {
		reason = ui.Warn.Render(reason) // то, что ждёт именно тебя
	} else {
		reason = ui.Faint.Render(reason)
	}
	return row(sel, m.w, c(dot, 1), c(notifIcon(n), 1), c(ui.Bold.Render(n.Repo), 28), flex(title),
		c(reason, 16), c(ui.Faint.Render(ui.Ago(n.UpdatedAt)), 5))
}

// markRead — сразу на экране, запрос — в фоне.
func (m *Model) markRead(i int, done bool) tea.Cmd {
	n := m.inbox[i]
	if done {
		m.inbox = append(m.inbox[:i:i], m.inbox[i+1:]...)
	} else {
		m.inbox[i].Unread = false
	}
	return func() tea.Msg {
		ctx, cancel := m.ctx()
		defer cancel()
		var err error
		if done {
			err = m.gh.MarkDone(ctx, n.ID)
		} else if n.Unread {
			err = m.gh.MarkRead(ctx, n.ID)
		}
		if err != nil {
			return loaded{errMsg{err}}
		}
		return nil
	}
}

func (m *Model) inboxKey(k string, i int, has bool) tea.Cmd {
	switch k {
	case "s":
		m.inboxAll = !m.inboxAll
		m.lists[tabInbox] = list{}
		return m.loadInbox()
	case "M":
		if m.unread() == 0 {
			m.setFlash("непрочитанных нет", false)
			return nil
		}
		m.confirm = &confirm{question: fmt.Sprintf("Отметить прочитанными все %d?", m.unread()), onYes: func(m *Model) tea.Cmd {
			for j := range m.inbox {
				m.inbox[j].Unread = false
			}
			return m.act("всё прочитано", tabInbox, func(ctx context.Context) error { return m.gh.MarkAllRead(ctx) })
		}}
		return nil
	}
	if !has {
		return nil
	}
	n := m.inbox[i]
	switch k {
	case "enter", "space":
		read := m.markRead(i, false)
		switch {
		case n.Type == "PullRequest" && n.Number > 0:
			return tea.Batch(read, m.openPRPage(n.Repo, n.Number, n.Title))
		case n.Type == "Issue" && n.Number > 0:
			return tea.Batch(read, m.openIssuePage(n.Repo, n.Number, n.Title))
		}
		return tea.Batch(read, openURL(n.URL))
	case "m":
		return m.markRead(i, false)
	case "d":
		m.setFlash("убрано из входящих: "+n.Title, false)
		return m.markRead(i, true)
	case "o":
		return tea.Batch(m.markRead(i, false), openURL(n.URL))
	}
	return nil
}

func (m *Model) inboxTitle(n int) string {
	what := "непрочитанные"
	if m.inboxAll {
		what = "все"
	}
	return fmt.Sprintf("Входящие · %s · %s", what, strconv.Itoa(n))
}
