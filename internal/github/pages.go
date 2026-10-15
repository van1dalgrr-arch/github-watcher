package github

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"sync"

	gh "github.com/google/go-github/v92/github"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
)

// Страницы: PR, issue, репозиторий, коммит. Независимые запросы идут параллельно —
// страница PR открывается за время одного ответа GitHub, а не семи подряд.

// parallel запускает запросы одновременно и возвращает первую ошибку.
func parallel(fs ...func() error) error {
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		first error
	)
	for _, f := range fs {
		wg.Go(func() {
			if err := f(); err != nil {
				mu.Lock()
				if first == nil {
					first = err
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return first
}

func logins(users []*gh.User) []string {
	out := make([]string, 0, len(users))
	for _, u := range users {
		out = append(out, u.GetLogin())
	}
	return out
}

func labelNames(labels []*gh.Label) []string {
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		out = append(out, l.GetName())
	}
	return out
}

func toCommit(rc *gh.RepositoryCommit) domain.Commit {
	author := rc.GetAuthor().GetLogin()
	if author == "" {
		author = rc.GetCommit().GetAuthor().GetName() // коммит не привязан к аккаунту GitHub
	}
	c := domain.Commit{
		SHA:       rc.GetSHA(),
		Message:   rc.GetCommit().GetMessage(),
		Author:    author,
		Date:      rc.GetCommit().GetAuthor().GetDate().Time,
		URL:       rc.GetHTMLURL(),
		Additions: rc.GetStats().GetAdditions(),
		Deletions: rc.GetStats().GetDeletions(),
	}
	for _, f := range rc.Files {
		c.Files = append(c.Files, toFileDiff(f))
	}
	return c
}

func toFileDiff(f *gh.CommitFile) domain.FileDiff {
	return domain.FileDiff{
		Path: f.GetFilename(), PrevPath: f.GetPreviousFilename(), Status: f.GetStatus(),
		Additions: f.GetAdditions(), Deletions: f.GetDeletions(), Patch: f.GetPatch(),
	}
}

func issueComments(ctx context.Context, c *client, owner, name string, number int) ([]domain.Comment, error) {
	var out []domain.Comment
	opts := &gh.IssueListCommentsOptions{ListOptions: gh.ListOptions{PerPage: 100}}
	for {
		page, resp, err := c.gh.Issues.ListComments(ctx, owner, name, number, opts)
		if err != nil {
			return nil, err
		}
		for _, cm := range page {
			out = append(out, domain.Comment{
				Kind: domain.CommentPlain, Author: cm.GetUser().GetLogin(), Body: cm.GetBody(),
				URL: cm.GetHTMLURL(), CreatedAt: cm.GetCreatedAt().Time,
			})
		}
		if resp.NextPage == 0 || len(out) >= 500 {
			return out, nil
		}
		opts.Page = resp.NextPage
	}
}

func (c *client) PR(ctx context.Context, repo string, number int) (domain.PRDetail, error) {
	owner, name, err := split(repo)
	if err != nil {
		return domain.PRDetail{}, err
	}
	p, _, err := c.gh.PullRequests.Get(ctx, owner, name, number)
	if err != nil {
		return domain.PRDetail{}, err
	}
	st := p.GetState()
	if !p.GetMergedAt().IsZero() {
		st = "merged"
	}
	d := domain.PRDetail{
		PullRequest: domain.PullRequest{
			Repo: repo, Number: p.GetNumber(), Title: p.GetTitle(), Author: p.GetUser().GetLogin(), State: st,
			Draft: p.GetDraft(), Head: p.GetHead().GetRef(), Base: p.GetBase().GetRef(), Body: p.GetBody(),
			URL: p.GetHTMLURL(), UpdatedAt: p.GetUpdatedAt().Time,
		},
		Additions: p.GetAdditions(), Deletions: p.GetDeletions(), ChangedFiles: p.GetChangedFiles(),
		Mergeable: p.GetMergeableState(), HeadSHA: p.GetHead().GetSHA(),
		Labels: labelNames(p.Labels), Reviewers: logins(p.RequestedReviewers), Assignees: logins(p.Assignees),
	}
	var plain, reviews, lines []domain.Comment
	page := &gh.ListOptions{PerPage: 100}
	err = parallel(
		func() (err error) { plain, err = issueComments(ctx, c, owner, name, number); return },
		func() error {
			rs, _, err := c.gh.PullRequests.ListReviews(ctx, owner, name, number, page)
			for _, r := range rs {
				if r.GetState() == "PENDING" { // черновик чужого ревью не виден и на сайте
					continue
				}
				reviews = append(reviews, domain.Comment{
					Kind: domain.CommentReview, Author: r.GetUser().GetLogin(), Body: r.GetBody(), State: r.GetState(),
					URL: r.GetHTMLURL(), CreatedAt: r.GetSubmittedAt().Time,
				})
			}
			return err
		},
		func() error {
			cs, _, err := c.gh.PullRequests.ListComments(ctx, owner, name, number, &gh.PullRequestListCommentsOptions{ListOptions: *page})
			for _, cm := range cs {
				line := cm.GetLine()
				if line == 0 {
					line = cm.GetOriginalLine() // строку уже переписали — GitHub помнит, где была
				}
				lines = append(lines, domain.Comment{
					Kind: domain.CommentLine, Author: cm.GetUser().GetLogin(), Body: cm.GetBody(), Path: cm.GetPath(),
					Line: line, DiffHunk: cm.GetDiffHunk(), URL: cm.GetHTMLURL(), CreatedAt: cm.GetCreatedAt().Time,
				})
			}
			return err
		},
		func() error {
			cs, _, err := c.gh.PullRequests.ListCommits(ctx, owner, name, number, page)
			for _, rc := range cs {
				d.Commits = append(d.Commits, toCommit(rc))
			}
			return err
		},
		func() error {
			fs, _, err := c.gh.PullRequests.ListFiles(ctx, owner, name, number, page)
			for _, f := range fs {
				d.Files = append(d.Files, toFileDiff(f))
			}
			return err
		},
		func() error {
			// проверки — не главное на странице: токену без прав на checks откроем PR и без них
			res, _, err := c.gh.Checks.ListCheckRunsForRef(ctx, owner, name, d.HeadSHA, &gh.ListCheckRunsOptions{ListOptions: *page})
			if err != nil || res == nil {
				return nil
			}
			for _, r := range res.CheckRuns {
				ch := domain.Check{Name: r.GetName(), Status: r.GetStatus(), Conclusion: r.GetConclusion(), URL: r.GetHTMLURL()}
				if !r.GetCompletedAt().IsZero() {
					ch.Took = r.GetCompletedAt().Sub(r.GetStartedAt().Time)
				}
				d.Checks = append(d.Checks, ch)
			}
			return err
		},
	)
	if err != nil {
		return domain.PRDetail{}, err
	}
	d.Comments = append(append(plain, reviews...), lines...)
	sort.SliceStable(d.Comments, func(i, j int) bool { return d.Comments[i].CreatedAt.Before(d.Comments[j].CreatedAt) })
	sort.SliceStable(d.Checks, func(i, j int) bool { return checkRank(d.Checks[i]) < checkRank(d.Checks[j]) })
	d.CIStatus = checksState(d.Checks)
	return d, nil
}

// checkRank — упавшие проверки наверх, потом идущие, потом прошедшие.
func checkRank(c domain.Check) int {
	switch c.State() {
	case "failure":
		return 0
	case "pending":
		return 1
	}
	return 2
}

// checksState — общий статус проверок, как значок у PR на сайте.
func checksState(cs []domain.Check) string {
	st := ""
	for _, c := range cs {
		switch c.State() {
		case "failure":
			return "failure"
		case "pending":
			st = "pending"
		case "success":
			if st == "" {
				st = "success"
			}
		}
	}
	return st
}

func (c *client) Issue(ctx context.Context, repo string, number int) (domain.IssueDetail, error) {
	owner, name, err := split(repo)
	if err != nil {
		return domain.IssueDetail{}, err
	}
	var (
		i        *gh.Issue
		comments []domain.Comment
	)
	err = parallel(
		func() (err error) { i, _, err = c.gh.Issues.Get(ctx, owner, name, number); return },
		func() (err error) { comments, err = issueComments(ctx, c, owner, name, number); return },
	)
	if err != nil {
		return domain.IssueDetail{}, err
	}
	return domain.IssueDetail{
		Issue: toIssue(repo, i), Assignees: logins(i.Assignees), Comments: comments, ClosedAt: i.GetClosedAt().Time,
	}, nil
}

func (c *client) Repo(ctx context.Context, repo string) (domain.RepoDetail, error) {
	owner, name, err := split(repo)
	if err != nil {
		return domain.RepoDetail{}, err
	}
	r, _, err := c.gh.Repositories.Get(ctx, owner, name)
	if err != nil {
		return domain.RepoDetail{}, err
	}
	d := domain.RepoDetail{
		Repo: domain.Repo{
			Owner: owner, Name: name, Description: r.GetDescription(), Language: r.GetLanguage(),
			Private: r.GetPrivate(), Fork: r.GetFork(), Archived: r.GetArchived(), Stars: r.GetStargazersCount(),
			OpenIssues: r.GetOpenIssuesCount(), URL: r.GetHTMLURL(), PushedAt: r.GetPushedAt().Time,
		},
		Forks: r.GetForksCount(), Watchers: r.GetSubscribersCount(), DefaultBranch: r.GetDefaultBranch(),
		Homepage: r.GetHomepage(), License: r.GetLicense().GetSPDXID(), Topics: r.Topics, CreatedAt: r.GetCreatedAt().Time,
	}
	notFound := func(err error) error { // у репозитория может не быть README или релизов — это не ошибка
		var e *gh.ErrorResponse
		if errors.As(err, &e) && e.Response != nil && e.Response.StatusCode == http.StatusNotFound {
			return nil
		}
		return err
	}
	err = parallel(
		func() error {
			rm, _, err := c.gh.Repositories.GetReadme(ctx, owner, name, nil)
			if err != nil {
				return notFound(err)
			}
			d.Readme, err = rm.GetContent()
			return err
		},
		func() error {
			bs, _, err := c.gh.Repositories.ListBranches(ctx, owner, name, &gh.BranchListOptions{ListOptions: gh.ListOptions{PerPage: 100}})
			for _, b := range bs {
				d.Branches = append(d.Branches, b.GetName())
			}
			return err
		},
		func() error {
			cs, _, err := c.gh.Repositories.ListCommits(ctx, owner, name, &gh.CommitsListOptions{ListOptions: gh.ListOptions{PerPage: 40}})
			for _, rc := range cs {
				d.Commits = append(d.Commits, toCommit(rc))
			}
			return notFound(err) // пустой репозиторий — 409/404, коммитов просто нет
		},
		func() error {
			langs, _, err := c.gh.Repositories.ListLanguages(ctx, owner, name)
			for l, n := range langs {
				d.Languages = append(d.Languages, domain.Lang{Name: l, Bytes: n})
			}
			sort.Slice(d.Languages, func(i, j int) bool { return d.Languages[i].Bytes > d.Languages[j].Bytes })
			return err
		},
		func() error {
			rel, _, err := c.gh.Repositories.GetLatestRelease(ctx, owner, name)
			if err != nil {
				return notFound(err)
			}
			d.LatestRelease = rel.GetTagName()
			return nil
		},
		func() error {
			rs, _, err := c.gh.Repositories.ListReleases(ctx, owner, name, &gh.ListOptions{PerPage: 30})
			for _, r := range rs {
				d.Releases = append(d.Releases, toRelease(r))
			}
			return notFound(err)
		},
	)
	if err != nil {
		return domain.RepoDetail{}, err
	}
	return d, nil
}

func (c *client) Commit(ctx context.Context, repo, sha string) (domain.Commit, error) {
	owner, name, err := split(repo)
	if err != nil {
		return domain.Commit{}, err
	}
	rc, _, err := c.gh.Repositories.GetCommit(ctx, owner, name, sha, &gh.ListOptions{PerPage: 100})
	if err != nil {
		return domain.Commit{}, err
	}
	return toCommit(rc), nil
}

// Review — ревью PR целиком: APPROVE, REQUEST_CHANGES или COMMENT (у двух последних нужен текст).
func (c *client) Review(ctx context.Context, repo string, number int, event, body string) error {
	owner, name, err := split(repo)
	if err != nil {
		return err
	}
	req := &gh.PullRequestReviewRequest{Event: &event}
	if body != "" {
		req.Body = &body
	}
	_, _, err = c.gh.PullRequests.CreateReview(ctx, owner, name, number, req)
	return err
}

func (c *client) ReopenIssue(ctx context.Context, repo string, number int) error {
	owner, name, err := split(repo)
	if err != nil {
		return err
	}
	open := "open"
	_, _, err = c.gh.Issues.Update(ctx, owner, name, number, gh.UpdateIssueRequest{State: &open})
	return err
}

// CreatePR — новый PR из ветки head в base (ветка уже должна быть на GitHub).
func (c *client) CreatePR(ctx context.Context, repo, head, base, title, body string, draft bool) (domain.PullRequest, error) {
	owner, name, err := split(repo)
	if err != nil {
		return domain.PullRequest{}, err
	}
	req := gh.CreatePullRequest{Title: &title, Head: head, Base: base, Draft: &draft}
	if body != "" {
		req.Body = &body
	}
	p, _, err := c.gh.PullRequests.Create(ctx, owner, name, req)
	if err != nil {
		return domain.PullRequest{}, err
	}
	return domain.PullRequest{Repo: repo, Number: p.GetNumber(), Title: p.GetTitle(), URL: p.GetHTMLURL(), Head: head, Base: base, State: "open", Draft: draft}, nil
}

func toRelease(r *gh.RepositoryRelease) domain.Release {
	rel := domain.Release{
		Tag: r.GetTagName(), Name: r.GetName(), Body: r.GetBody(), Author: r.GetAuthor().GetLogin(),
		Draft: r.GetDraft(), Prerelease: r.GetPrerelease(), CreatedAt: r.GetCreatedAt().Time, URL: r.GetHTMLURL(),
	}
	for _, a := range r.Assets {
		rel.Assets = append(rel.Assets, domain.Asset{Name: a.GetName(), Size: int64(a.GetSize()), Downloads: a.GetDownloadCount(), URL: a.GetBrowserDownloadURL()})
	}
	return rel
}

// CreateRelease — релиз с тегом tag на ветке target; заметки собирает GitHub (PR и авторы с прошлого релиза).
func (c *client) CreateRelease(ctx context.Context, repo, tag, target string, draft bool) (domain.Release, error) {
	owner, name, err := split(repo)
	if err != nil {
		return domain.Release{}, err
	}
	generate := true
	req := gh.CreateReleaseRequest{TagName: tag, Name: &tag, Draft: &draft, GenerateReleaseNotes: &generate}
	if target != "" {
		req.TargetCommitish = &target
	}
	r, _, err := c.gh.Repositories.CreateRelease(ctx, owner, name, req)
	if err != nil {
		return domain.Release{}, err
	}
	return toRelease(r), nil
}
