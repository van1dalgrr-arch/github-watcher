package github

import (
	"context"
	"regexp"
	"strconv"
	"time"

	gh "github.com/google/go-github/v92/github"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
)

// Входящие: уведомления GitHub, как колокольчик на сайте.

// apiURLRe — https://api.github.com/repos/o/r/pulls/12 → тип и номер.
var apiURLRe = regexp.MustCompile(`/repos/[^/]+/[^/]+/(pulls|issues|commits|releases)/([^/]+)$`)

func (c *client) Notifications(ctx context.Context, all bool) ([]domain.Notification, error) {
	ns, _, err := c.gh.Activity.ListNotifications(ctx, &gh.NotificationListOptions{All: all, ListOptions: gh.ListOptions{PerPage: 50}})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Notification, 0, len(ns))
	for _, n := range ns {
		repo := n.GetRepository().GetFullName()
		d := domain.Notification{
			ID: n.GetID(), Repo: repo, Type: n.GetSubject().GetType(), Title: n.GetSubject().GetTitle(),
			Reason: n.GetReason(), Unread: n.GetUnread(), UpdatedAt: n.GetUpdatedAt().Time,
			URL: "https://github.com/" + repo,
		}
		if m := apiURLRe.FindStringSubmatch(n.GetSubject().GetURL()); m != nil {
			switch m[1] {
			case "pulls":
				d.Number, _ = strconv.Atoi(m[2])
				d.URL += "/pull/" + m[2]
			case "issues":
				d.Number, _ = strconv.Atoi(m[2])
				d.URL += "/issues/" + m[2]
			case "commits":
				d.URL += "/commit/" + m[2]
			case "releases":
				d.URL += "/releases"
			}
		}
		out = append(out, d)
	}
	return out, nil
}

func (c *client) MarkRead(ctx context.Context, id string) error {
	_, err := c.gh.Activity.MarkThreadRead(ctx, id)
	return err
}

// MarkDone убирает уведомление из входящих (кнопка Done на сайте).
func (c *client) MarkDone(ctx context.Context, id string) error {
	_, err := c.gh.Activity.MarkThreadDone(ctx, id)
	return err
}

func (c *client) MarkAllRead(ctx context.Context) error {
	_, err := c.gh.Activity.MarkNotificationsRead(ctx, gh.Timestamp{Time: time.Now()})
	return err
}
