package github

import (
	"context"
	"strings"

	gh "github.com/google/go-github/v92/github"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
)

// Search — поиск по всему GitHub: репозитории, issues и PR, люди, код. Разделы ищутся параллельно;
// если какой-то раздел GitHub отклонил (поиск по коду капризнее остальных), остальные всё равно показываются.
func (c *client) Search(ctx context.Context, query string) (domain.SearchResult, error) {
	res := domain.SearchResult{Query: query}
	opts := &gh.SearchOptions{ListOptions: gh.ListOptions{PerPage: 30}}
	var errs [4]error
	_ = parallel(
		func() error {
			r, _, err := c.gh.Search.Repositories(ctx, query, opts)
			if errs[0] = err; err == nil {
				res.Totals[0] = r.GetTotal()
				for _, repo := range r.Repositories {
					res.Repos = append(res.Repos, toRepo(repo))
				}
			}
			return nil
		},
		func() error {
			r, _, err := c.gh.Search.Issues(ctx, query, opts)
			if errs[1] = err; err == nil {
				res.Totals[1] = r.GetTotal()
				for _, i := range r.Issues {
					repo := strings.TrimPrefix(i.GetRepositoryURL(), "https://api.github.com/repos/")
					res.Issues = append(res.Issues, domain.Found{
						Repo: repo, Number: i.GetNumber(), Title: i.GetTitle(), State: i.GetState(), PR: i.IsPullRequest(),
						Author: i.GetUser().GetLogin(), Comments: i.GetComments(), URL: i.GetHTMLURL(), UpdatedAt: i.GetUpdatedAt().Time,
					})
				}
			}
			return nil
		},
		func() error {
			r, _, err := c.gh.Search.Users(ctx, query, opts)
			if errs[2] = err; err == nil {
				res.Totals[2] = r.GetTotal()
				for _, u := range r.Users {
					res.Users = append(res.Users, u.GetLogin())
				}
			}
			return nil
		},
		func() error {
			co := *opts
			co.TextMatch = true
			r, _, err := c.gh.Search.Code(ctx, query, &co)
			if errs[3] = err; err == nil {
				res.Totals[3] = r.GetTotal()
				for _, h := range r.CodeResults {
					hit := domain.CodeHit{Repo: h.GetRepository().GetFullName(), Path: h.GetPath(), URL: h.GetHTMLURL()}
					if len(h.TextMatches) > 0 {
						hit.Fragment = h.TextMatches[0].GetFragment()
					}
					res.Code = append(res.Code, hit)
				}
			}
			return nil
		},
	)
	if errs[0] != nil && errs[1] != nil && errs[2] != nil && errs[3] != nil {
		return res, errs[0] // не ответил ни один раздел — показываем причину
	}
	return res, nil
}
