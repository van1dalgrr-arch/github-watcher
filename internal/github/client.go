package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	gh "github.com/google/go-github/v92/github"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
)

// ErrNoToken — нет ни GITHUB_TOKEN, ни логина в gh.
var ErrNoToken = errors.New("нет токена GitHub: задай GITHUB_TOKEN или выполни `gh auth login`")

type client struct {
	gh    *gh.Client
	http  *http.Client // для скачивания логов по redirect-ссылке
	token string       // для GraphQL: сетка вкладов есть только там

	mu      sync.Mutex
	pushes  map[string]pushStats // кэш статистики пушей: before..head не меняется
	me      string
	mine    map[string]bool // мои репозитории — фильтр для ленты
	mineAt  time.Time
	prCache map[string]*gh.PullRequest
}

func New(token string) (Client, error) {
	if token == "" {
		return nil, ErrNoToken
	}
	c, err := gh.NewClient(gh.WithAuthToken(token), gh.WithTimeout(30*time.Second))
	if err != nil {
		return nil, err
	}
	return &client{gh: c, http: &http.Client{Timeout: 60 * time.Second}, token: token}, nil
}

func split(repo string) (owner, name string, err error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" {
		return "", "", fmt.Errorf("репозиторий %q: нужен формат owner/name", repo)
	}
	return owner, name, nil
}

func (c *client) Me(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.me != "" {
		return c.me, nil
	}
	u, _, err := c.gh.Users.Get(ctx, "")
	if err != nil {
		return "", err
	}
	c.me = u.GetLogin()
	return c.me, nil
}

func (c *client) Repos(ctx context.Context) ([]domain.Repo, error) {
	opts := &gh.RepositoryListByAuthenticatedUserOptions{
		Sort:        "pushed",
		Affiliation: "owner,collaborator,organization_member",
		ListOptions: gh.ListOptions{PerPage: 100},
	}
	var out []domain.Repo
	for {
		page, resp, err := c.gh.Repositories.ListByAuthenticatedUser(ctx, opts)
		if err != nil {
			return nil, err
		}
		for _, r := range page {
			out = append(out, domain.Repo{
				Owner:       r.GetOwner().GetLogin(),
				Name:        r.GetName(),
				Description: r.GetDescription(),
				Language:    r.GetLanguage(),
				Private:     r.GetPrivate(),
				Fork:        r.GetFork(),
				Archived:    r.GetArchived(),
				Stars:       r.GetStargazersCount(),
				OpenIssues:  r.GetOpenIssuesCount(),
				URL:         r.GetHTMLURL(),
				PushedAt:    r.GetPushedAt().Time,
			})
		}
		if resp.NextPage == 0 || len(out) >= 500 {
			return out, nil
		}
		opts.Page = resp.NextPage
	}
}

func (c *client) PullRequests(ctx context.Context, repo, state string) ([]domain.PullRequest, error) {
	owner, name, err := split(repo)
	if err != nil {
		return nil, err
	}
	prs, _, err := c.gh.PullRequests.List(ctx, owner, name, &gh.PullRequestListOptions{
		State: state, Sort: "updated", Direction: "desc", ListOptions: gh.ListOptions{PerPage: 50},
	})
	if err != nil {
		return nil, err
	}
	// Статус CI берём из последних запусков Actions: один запрос вместо запроса на каждый PR.
	ci := map[string]string{}
	if runs, err := c.WorkflowRuns(ctx, repo); err == nil {
		for _, r := range runs { // runs отсортированы от новых к старым — первый и есть актуальный
			if _, seen := ci[r.HeadSHA]; !seen {
				ci[r.HeadSHA] = domain.RunState(r.Status, r.Conclusion)
			}
		}
	}
	out := make([]domain.PullRequest, 0, len(prs))
	for _, p := range prs {
		st := p.GetState()
		if !p.GetMergedAt().IsZero() {
			st = "merged"
		}
		out = append(out, domain.PullRequest{
			Repo:      repo,
			Number:    p.GetNumber(),
			Title:     p.GetTitle(),
			Author:    p.GetUser().GetLogin(),
			State:     st,
			Draft:     p.GetDraft(),
			Head:      p.GetHead().GetRef(),
			Base:      p.GetBase().GetRef(),
			Body:      p.GetBody(),
			CIStatus:  ci[p.GetHead().GetSHA()],
			URL:       p.GetHTMLURL(),
			UpdatedAt: p.GetUpdatedAt().Time,
		})
	}
	return out, nil
}

func (c *client) Issues(ctx context.Context, repo, state string) ([]domain.Issue, error) {
	owner, name, err := split(repo)
	if err != nil {
		return nil, err
	}
	issues, _, err := c.gh.Issues.ListByRepo(ctx, owner, name, &gh.IssueListByRepoOptions{
		State: state, Sort: "updated", Direction: "desc", ListOptions: gh.ListOptions{PerPage: 50},
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Issue, 0, len(issues))
	for _, i := range issues {
		if i.IsPullRequest() { // Issues API отдаёт и PR — их показываем на своей вкладке
			continue
		}
		out = append(out, toIssue(repo, i))
	}
	return out, nil
}

func toIssue(repo string, i *gh.Issue) domain.Issue {
	labels := make([]string, 0, len(i.Labels))
	for _, l := range i.Labels {
		labels = append(labels, l.GetName())
	}
	return domain.Issue{
		Repo:      repo,
		Number:    i.GetNumber(),
		Title:     i.GetTitle(),
		Author:    i.GetUser().GetLogin(),
		State:     i.GetState(),
		Labels:    labels,
		Comments:  i.GetComments(),
		Body:      i.GetBody(),
		URL:       i.GetHTMLURL(),
		UpdatedAt: i.GetUpdatedAt().Time,
	}
}

func (c *client) WorkflowRuns(ctx context.Context, repo string) ([]domain.WorkflowRun, error) {
	owner, name, err := split(repo)
	if err != nil {
		return nil, err
	}
	runs, _, err := c.gh.Actions.ListRepositoryWorkflowRuns(ctx, owner, name, &gh.ListWorkflowRunsOptions{
		ListOptions: gh.ListOptions{PerPage: 30},
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.WorkflowRun, 0, len(runs.WorkflowRuns))
	for _, r := range runs.WorkflowRuns {
		out = append(out, domain.WorkflowRun{
			Repo:       repo,
			ID:         r.GetID(),
			Name:       r.GetName(),
			Branch:     r.GetHeadBranch(),
			HeadSHA:    r.GetHeadSHA(),
			Event:      r.GetEvent(),
			Title:      r.GetDisplayTitle(),
			Status:     r.GetStatus(),
			Conclusion: r.GetConclusion(),
			URL:        r.GetHTMLURL(),
			UpdatedAt:  r.GetUpdatedAt().Time,
		})
	}
	return out, nil
}

// RunLogs склеивает логи всех job-ов запуска в один текст.
func (c *client) RunLogs(ctx context.Context, repo string, runID int64) (string, error) {
	owner, name, err := split(repo)
	if err != nil {
		return "", err
	}
	jobs, _, err := c.gh.Actions.ListWorkflowJobs(ctx, owner, name, runID, &gh.ListWorkflowJobsOptions{
		ListOptions: gh.ListOptions{PerPage: 50},
	})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, j := range jobs.Jobs {
		fmt.Fprintf(&b, "\x00JOB %s · %s\n", j.GetName(), domain.RunState(j.GetStatus(), j.GetConclusion()))
		if j.GetStatus() != "completed" {
			b.WriteString("  (job ещё выполняется — логи появятся после завершения)\n")
			continue
		}
		text, err := c.jobLog(ctx, owner, name, j.GetID())
		if err != nil {
			fmt.Fprintf(&b, "  не удалось загрузить лог: %v\n", err)
			continue
		}
		b.WriteString(text)
	}
	return b.String(), nil
}

func (c *client) jobLog(ctx context.Context, owner, name string, jobID int64) (string, error) {
	u, _, err := c.gh.Actions.GetWorkflowJobLogs(ctx, owner, name, jobID, 3)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	// Каждая строка начинается с RFC3339-времени — в терминале это шум, убираем.
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	for i, l := range lines {
		if len(l) > 29 && l[4] == '-' && l[10] == 'T' {
			if _, rest, ok := strings.Cut(l, " "); ok {
				lines[i] = rest
			}
		}
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func (c *client) MergePR(ctx context.Context, repo string, number int, method string) error {
	owner, name, err := split(repo)
	if err != nil {
		return err
	}
	_, _, err = c.gh.PullRequests.Merge(ctx, owner, name, number, "", &gh.PullRequestOptions{MergeMethod: method})
	return err
}

func (c *client) CreateIssue(ctx context.Context, repo, title, body string) (domain.Issue, error) {
	owner, name, err := split(repo)
	if err != nil {
		return domain.Issue{}, err
	}
	req := gh.CreateIssueRequest{Title: title}
	if body != "" {
		req.Body = &body
	}
	i, _, err := c.gh.Issues.Create(ctx, owner, name, req)
	if err != nil {
		return domain.Issue{}, err
	}
	return toIssue(repo, i), nil
}

func (c *client) CloseIssue(ctx context.Context, repo string, number int) error {
	owner, name, err := split(repo)
	if err != nil {
		return err
	}
	closed := "closed"
	_, _, err = c.gh.Issues.Update(ctx, owner, name, number, gh.UpdateIssueRequest{State: &closed})
	return err
}

// Comment работает и для issue, и для PR — у GitHub это один и тот же API.
func (c *client) Comment(ctx context.Context, repo string, number int, body string) error {
	owner, name, err := split(repo)
	if err != nil {
		return err
	}
	_, _, err = c.gh.Issues.CreateComment(ctx, owner, name, number, gh.IssueCommentRequest{Body: body})
	return err
}

func (c *client) RerunWorkflow(ctx context.Context, repo string, runID int64, onlyFailed bool) error {
	owner, name, err := split(repo)
	if err != nil {
		return err
	}
	if onlyFailed {
		_, err = c.gh.Actions.RerunFailedJobsByID(ctx, owner, name, runID)
	} else {
		_, err = c.gh.Actions.RerunWorkflowByID(ctx, owner, name, runID)
	}
	return err
}

// EventsSince собирает ленту из трёх источников:
//   - received events — что происходит в моих и отслеживаемых репозиториях;
//   - notifications — упоминания, запросы ревью, назначения;
//   - Actions — завершённые запуски в недавно обновлённых репозиториях (в Events API их нет).
func (c *client) EventsSince(ctx context.Context, since time.Time) ([]domain.Event, error) {
	me, err := c.Me(ctx)
	if err != nil {
		return nil, err
	}
	var out []domain.Event

	mine, err := c.myRepos(ctx)
	if err != nil {
		return nil, fmt.Errorf("repos: %w", err)
	}
	received, _, err := c.gh.Activity.ListEventsReceivedByUser(ctx, me, false, &gh.ListOptions{PerPage: 100})
	if err != nil {
		return nil, fmt.Errorf("events: %w", err)
	}
	for _, e := range received {
		// received events включают активность всех, на кого ты подписан (целые организации) — это шум
		if !mine[e.GetRepo().GetName()] || !e.GetCreatedAt().After(since) {
			continue
		}
		if ev, ok := c.convertEvent(ctx, e); ok {
			out = append(out, ev)
		}
	}

	// мои собственные пуши в received events не попадают — берём из событий, которые сделал я
	performed, _, err := c.gh.Activity.ListEventsPerformedByUser(ctx, me, false, &gh.ListOptions{PerPage: 100})
	if err != nil {
		return nil, fmt.Errorf("my events: %w", err)
	}
	for _, e := range performed {
		if e.GetType() != "PushEvent" || !mine[e.GetRepo().GetName()] || !e.GetCreatedAt().After(since) {
			continue
		}
		if ev, ok := c.convertEvent(ctx, e); ok {
			out = append(out, ev)
		}
	}
	out = dedup(out)

	notes, _, err := c.gh.Activity.ListNotifications(ctx, &gh.NotificationListOptions{
		Participating: true, Since: since, ListOptions: gh.ListOptions{PerPage: 50},
	})
	if err != nil {
		return nil, fmt.Errorf("notifications: %w", err)
	}
	for _, n := range notes {
		switch n.GetReason() {
		case "mention", "team_mention", "review_requested", "assign":
		default:
			continue // остальное и так приходит через events
		}
		out = append(out, domain.Event{
			SourceID:  "n:" + n.GetID() + ":" + n.GetUpdatedAt().Format(time.RFC3339),
			Kind:      domain.EventMention,
			Repo:      n.GetRepository().GetFullName(),
			Title:     reasonText[n.GetReason()] + ": " + n.GetSubject().GetTitle(),
			URL:       n.GetRepository().GetHTMLURL(),
			CreatedAt: n.GetUpdatedAt().Time,
		})
	}

	runs, err := c.recentRuns(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("actions: %w", err)
	}
	out = append(out, runs...)

	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

var reasonText = map[string]string{
	"mention":          "Тебя упомянули",
	"team_mention":     "Упомянули команду",
	"review_requested": "Просят ревью",
	"assign":           "Назначили на тебя",
}

// recentRuns — завершённые после since запуски в репозиториях, куда пушили за последние сутки.
func (c *client) recentRuns(ctx context.Context, since time.Time) ([]domain.Event, error) {
	repos, _, err := c.gh.Repositories.ListByAuthenticatedUser(ctx, &gh.RepositoryListByAuthenticatedUserOptions{
		Sort: "pushed", Affiliation: "owner,collaborator", ListOptions: gh.ListOptions{PerPage: 10},
	})
	if err != nil {
		return nil, err
	}
	var out []domain.Event
	for _, r := range repos {
		if time.Since(r.GetPushedAt().Time) > 24*time.Hour {
			break // отсортированы по pushed — дальше только старые
		}
		runs, err := c.WorkflowRuns(ctx, r.GetFullName())
		if err != nil {
			continue
		}
		for _, run := range runs {
			if run.Status != "completed" || !run.UpdatedAt.After(since) {
				continue
			}
			// только CI на код: запуски по расписанию, вручную и цепочки (сам notify) — шум
			if run.Event != "push" && run.Event != "pull_request" {
				continue
			}
			kind, verb := domain.EventCIPassed, "прошёл"
			switch domain.RunState(run.Status, run.Conclusion) {
			case "failure":
				kind, verb = domain.EventCIFailed, "упал"
			case "cancelled":
				continue
			}
			out = append(out, domain.Event{
				SourceID:  fmt.Sprintf("run:%d:%s", run.ID, run.UpdatedAt.Format(time.RFC3339)),
				Kind:      kind,
				Repo:      run.Repo,
				Title:     fmt.Sprintf("CI %s: %s · %s (%s)", verb, run.Name, run.Title, run.Branch),
				SHA:       run.HeadSHA,
				RunID:     run.ID,
				URL:       run.URL,
				CreatedAt: run.UpdatedAt,
			})
		}
	}
	return out, nil
}

// myRepos — множество моих репозиториев, обновляется раз в 10 минут.
func (c *client) myRepos(ctx context.Context) (map[string]bool, error) {
	c.mu.Lock()
	if c.mine != nil && time.Since(c.mineAt) < 10*time.Minute {
		defer c.mu.Unlock()
		return c.mine, nil
	}
	c.mu.Unlock()
	repos, err := c.Repos(ctx)
	if err != nil {
		return nil, err
	}
	mine := make(map[string]bool, len(repos))
	for _, r := range repos {
		mine[r.FullName()] = true
	}
	c.mu.Lock()
	c.mine, c.mineAt = mine, time.Now()
	c.mu.Unlock()
	return mine, nil
}

// pr дозапрашивает PR: в Events API с 2025 года payload урезан — нет заголовка, ссылки и флага merged.
func (c *client) pr(ctx context.Context, repo string, number int) *gh.PullRequest {
	key := repo + "#" + strconv.Itoa(number)
	c.mu.Lock()
	if p, ok := c.prCache[key]; ok {
		c.mu.Unlock()
		return p
	}
	c.mu.Unlock()
	owner, name, err := split(repo)
	if err != nil {
		return &gh.PullRequest{}
	}
	p, _, err := c.gh.PullRequests.Get(ctx, owner, name, number)
	if err != nil {
		return &gh.PullRequest{}
	}
	c.mu.Lock()
	if c.prCache == nil || len(c.prCache) > 500 {
		c.prCache = map[string]*gh.PullRequest{}
	}
	c.prCache[key] = p
	c.mu.Unlock()
	return p
}

// fullPR возвращает PR из payload, а если он урезан — полный из API.
func (c *client) fullPR(ctx context.Context, repo string, number int, pr *gh.PullRequest) *gh.PullRequest {
	if pr.GetTitle() != "" {
		return pr
	}
	if number == 0 {
		number = pr.GetNumber()
	}
	return c.pr(ctx, repo, number)
}

func (c *client) convertEvent(ctx context.Context, e *gh.Event) (domain.Event, bool) {
	ev := domain.Event{
		SourceID:  "e:" + e.GetID(),
		Repo:      e.GetRepo().GetName(),
		Actor:     e.GetActor().GetLogin(),
		URL:       "https://github.com/" + e.GetRepo().GetName(),
		CreatedAt: e.GetCreatedAt().Time,
	}
	payload, err := e.ParsePayload()
	if err != nil {
		return ev, false
	}
	switch p := payload.(type) {
	case *gh.PullRequestEvent:
		if p.GetAction() != "opened" && p.GetAction() != "closed" {
			return ev, false
		}
		pr := c.fullPR(ctx, ev.Repo, p.GetNumber(), p.GetPullRequest())
		ev.URL = pr.GetHTMLURL()
		switch {
		case p.GetAction() == "opened":
			ev.Kind, ev.Title = domain.EventPROpened, fmt.Sprintf("Открыт PR #%d: %s", pr.GetNumber(), pr.GetTitle())
		case p.GetAction() == "closed" && pr.GetMerged():
			ev.Kind, ev.Title = domain.EventPRMerged, fmt.Sprintf("Смёржен PR #%d: %s", pr.GetNumber(), pr.GetTitle())
		default:
			return ev, false
		}
	case *gh.PullRequestReviewEvent:
		pr := c.fullPR(ctx, ev.Repo, 0, p.GetPullRequest())
		ev.Kind, ev.URL = domain.EventPRReview, p.GetReview().GetHTMLURL()
		ev.Title = fmt.Sprintf("Ревью (%s) в PR #%d: %s", reviewText(p.GetReview().GetState()), pr.GetNumber(), pr.GetTitle())
	case *gh.IssuesEvent:
		i := p.GetIssue()
		if p.GetAction() != "opened" && p.GetAction() != "closed" {
			return ev, false
		}
		verb := map[string]string{"opened": "Открыт", "closed": "Закрыт"}[p.GetAction()]
		ev.Kind, ev.URL = domain.EventIssue, i.GetHTMLURL()
		ev.Title = fmt.Sprintf("%s issue #%d: %s", verb, i.GetNumber(), i.GetTitle())
	case *gh.IssueCommentEvent:
		if p.GetAction() != "created" {
			return ev, false
		}
		ev.Kind, ev.URL = domain.EventComment, p.GetComment().GetHTMLURL()
		ev.Title = fmt.Sprintf("Комментарий в #%d: %s", p.GetIssue().GetNumber(), oneLine(p.GetComment().GetBody(), 80))
	case *gh.PushEvent:
		if !strings.HasPrefix(p.GetRef(), "refs/heads/") || strings.Trim(p.GetHead(), "0") == "" {
			return ev, false // теги и удаление веток — не пуш кода
		}
		branch := strings.TrimPrefix(p.GetRef(), "refs/heads/")
		st := c.pushStats(ctx, ev.Repo, p.GetBefore(), p.GetHead())
		ev.Kind, ev.SHA = domain.EventPush, p.GetHead()
		ev.Title, ev.Body = st.render(branch)
		if st.url != "" {
			ev.URL = st.url
		}
	case *gh.ReleaseEvent:
		if p.GetAction() != "published" {
			return ev, false
		}
		ev.Kind, ev.URL = domain.EventRelease, p.GetRelease().GetHTMLURL()
		ev.Title = "Релиз " + p.GetRelease().GetTagName()
	case *gh.WatchEvent:
		ev.Kind, ev.Title = domain.EventStar, "Новая звезда"
	default:
		return ev, false
	}
	return ev, true
}

func reviewText(state string) string {
	switch state {
	case "approved":
		return "одобрено"
	case "changes_requested":
		return "нужны правки"
	default:
		return "комментарий"
	}
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// Variable возвращает переменную Actions репозитория; нет такой — пустая строка без ошибки.
func (c *client) Variable(ctx context.Context, repo, name string) (string, error) {
	owner, rname, err := split(repo)
	if err != nil {
		return "", err
	}
	v, resp, err := c.gh.Actions.GetRepoVariable(ctx, owner, rname, name)
	if resp != nil && resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return v.Value, nil
}

func (c *client) SetVariable(ctx context.Context, repo, name, value string) error {
	owner, rname, err := split(repo)
	if err != nil {
		return err
	}
	resp, err := c.gh.Actions.UpdateRepoVariable(ctx, owner, rname, name, gh.ActionsUpdateVariableRequest{Value: &value})
	if resp != nil && resp.StatusCode == http.StatusNotFound {
		_, err = c.gh.Actions.CreateRepoVariable(ctx, owner, rname, gh.ActionsCreateVariableRequest{Name: name, Value: value})
	}
	return err
}

func dedup(events []domain.Event) []domain.Event {
	seen := map[string]bool{}
	out := events[:0]
	for _, e := range events {
		if !seen[e.SourceID] {
			seen[e.SourceID] = true
			out = append(out, e)
		}
	}
	return out
}

// pushStats — что принёс пуш: коммиты и изменения по файлам.
type pushStats struct {
	commits    []string // "a2d5d07 заголовок коммита"
	total      int      // коммитов всего (в списке — не больше 10)
	files      []fileStat
	add, del   int
	url        string
	unknownErr bool // Compare не ответил — покажем, что знаем
}

type fileStat struct {
	name     string
	add, del int
	status   string
}

func (c *client) pushStats(ctx context.Context, repo, before, head string) pushStats {
	key := repo + "@" + before + ".." + head
	c.mu.Lock()
	if st, ok := c.pushes[key]; ok {
		c.mu.Unlock()
		return st
	}
	c.mu.Unlock()

	st := c.fetchPushStats(ctx, repo, before, head)
	if !st.unknownErr {
		c.mu.Lock()
		if c.pushes == nil || len(c.pushes) > 500 {
			c.pushes = map[string]pushStats{}
		}
		c.pushes[key] = st
		c.mu.Unlock()
	}
	return st
}

func (c *client) fetchPushStats(ctx context.Context, repo, before, head string) pushStats {
	owner, name, err := split(repo)
	if err != nil {
		return pushStats{unknownErr: true}
	}
	var (
		commits []*gh.RepositoryCommit
		files   []*gh.CommitFile
		st      pushStats
	)
	if strings.Trim(before, "0") == "" { // новая ветка: сравнивать не с чем — берём последний коммит
		rc, _, err := c.gh.Repositories.GetCommit(ctx, owner, name, head, nil)
		if err != nil {
			return pushStats{unknownErr: true}
		}
		commits, files, st.total, st.url = []*gh.RepositoryCommit{rc}, rc.Files, 1, rc.GetHTMLURL()
	} else {
		cmp, _, err := c.gh.Repositories.CompareCommits(ctx, owner, name, before, head, &gh.ListOptions{PerPage: 100})
		if err != nil {
			return pushStats{unknownErr: true}
		}
		commits, files, st.total, st.url = cmp.Commits, cmp.Files, cmp.GetTotalCommits(), cmp.GetHTMLURL()
	}
	for i := len(commits) - 1; i >= 0 && len(st.commits) < 10; i-- { // новые сверху
		rc := commits[i]
		msg, _, _ := strings.Cut(rc.GetCommit().GetMessage(), "\n")
		st.commits = append(st.commits, rc.GetSHA()[:min(7, len(rc.GetSHA()))]+" "+msg)
	}
	for _, f := range files {
		st.add += f.GetAdditions()
		st.del += f.GetDeletions()
		st.files = append(st.files, fileStat{f.GetFilename(), f.GetAdditions(), f.GetDeletions(), f.GetStatus()})
	}
	// самые крупные изменения — первыми
	sort.SliceStable(st.files, func(i, j int) bool {
		return st.files[i].add+st.files[i].del > st.files[j].add+st.files[j].del
	})
	return st
}

// render — заголовок для ленты и подробности для бота и TUI.
func (st pushStats) render(branch string) (title, body string) {
	if st.unknownErr {
		return "Push в " + branch, ""
	}
	title = fmt.Sprintf("Push в %s: %d %s · %d %s · +%d −%d", branch,
		st.total, domain.Plural(st.total, "коммит", "коммита", "коммитов"),
		len(st.files), domain.Plural(len(st.files), "файл", "файла", "файлов"), st.add, st.del)

	var b strings.Builder
	for _, c := range st.commits {
		b.WriteString("• " + c + "\n")
	}
	if extra := st.total - len(st.commits); extra > 0 {
		fmt.Fprintf(&b, "  …и ещё %d\n", extra)
	}
	if len(st.files) > 0 {
		b.WriteString("\n")
	}
	for i, f := range st.files {
		if i == 8 {
			fmt.Fprintf(&b, "  …и ещё %d %s\n", len(st.files)-i, domain.Plural(len(st.files)-i, "файл", "файла", "файлов"))
			break
		}
		mark := map[string]string{"added": " (новый)", "removed": " (удалён)", "renamed": " (переименован)"}[f.status]
		fmt.Fprintf(&b, "+%d −%d  %s%s\n", f.add, f.del, f.name, mark)
	}
	return title, strings.TrimRight(b.String(), "\n")
}

// RunReport — отчёт о запуске для уведомления, как в терминале: job-ы и шаги с ✓/✗ и временем,
// у упавших — строки лога вокруг ошибки. Логи качаются только для упавших job-ов.
func (c *client) RunReport(ctx context.Context, repo string, runID int64) (string, error) {
	owner, name, err := split(repo)
	if err != nil {
		return "", err
	}
	jobs, _, err := c.gh.Actions.ListWorkflowJobs(ctx, owner, name, runID, &gh.ListWorkflowJobsOptions{
		ListOptions: gh.ListOptions{PerPage: 50},
	})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, j := range jobs.Jobs {
		fmt.Fprintf(&b, "%s %s  %s\n", mark(j.GetConclusion()), j.GetName(),
			dur(j.GetStartedAt().Time, j.GetCompletedAt().Time))
		failed := false
		for _, s := range j.Steps {
			if boringStep(s.GetName()) && s.GetConclusion() == "success" {
				continue
			}
			fmt.Fprintf(&b, "  %s %s  %s\n", mark(s.GetConclusion()), s.GetName(),
				dur(s.GetStartedAt().Time, s.GetCompletedAt().Time))
			failed = failed || s.GetConclusion() == "failure"
		}
		if failed {
			if text, err := c.jobLog(ctx, owner, name, j.GetID()); err == nil {
				for _, l := range errorContext(text) {
					b.WriteString("    │ " + l + "\n")
				}
			}
		}
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func mark(conclusion string) string {
	switch conclusion {
	case "success":
		return "✓"
	case "skipped", "neutral":
		return "–"
	case "cancelled":
		return "⊘"
	case "":
		return "…"
	default:
		return "✗"
	}
}

// boringStep — служебные шаги, которые есть в любом job-е.
func boringStep(name string) bool {
	return name == "Set up job" || name == "Complete job" || strings.HasPrefix(name, "Post ")
}

func dur(from, to time.Time) string {
	if from.IsZero() || to.IsZero() {
		return ""
	}
	d := to.Sub(from).Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%dс", int(d.Seconds()))
	}
	return fmt.Sprintf("%dм %dс", int(d.Minutes()), int(d.Seconds())%60)
}

// errorContext — до 12 строк лога, ведущих к первой ошибке, без служебной разметки Actions.
func errorContext(log string) []string {
	var lines []string
	errAt := -1
	for l := range strings.Lines(log) {
		l = strings.TrimRight(l, "\r\n")
		switch {
		case strings.HasPrefix(l, "##[group]"), strings.HasPrefix(l, "##[endgroup]"), strings.HasPrefix(l, "##[command]"):
			continue
		case strings.HasPrefix(l, "##[error]"):
			l = "✗ " + strings.TrimPrefix(l, "##[error]")
			if errAt < 0 {
				errAt = len(lines)
			}
		case errAt < 0 && (strings.Contains(l, "--- FAIL") || strings.HasPrefix(l, "FAIL") || strings.HasPrefix(l, "panic:")):
			errAt = len(lines)
		}
		lines = append(lines, l)
	}
	if errAt < 0 {
		errAt = len(lines) - 1
	}
	from := max(errAt-9, 0)
	to := min(errAt+3, len(lines))
	return lines[from:to]
}

// Tree — все файлы ветки по умолчанию одним запросом (рекурсивное дерево git).
func (c *client) Tree(ctx context.Context, repo string) ([]string, error) {
	owner, name, err := split(repo)
	if err != nil {
		return nil, err
	}
	tree, _, err := c.gh.Git.GetTree(ctx, owner, name, "HEAD", true)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range tree.Entries {
		if e.GetType() == "blob" {
			out = append(out, e.GetPath())
		}
	}
	return out, nil
}

// File — содержимое файла с ветки по умолчанию. Contents API отдаёт файлы до 1 МБ.
func (c *client) File(ctx context.Context, repo, path string) (string, error) {
	owner, name, err := split(repo)
	if err != nil {
		return "", err
	}
	f, _, _, err := c.gh.Repositories.GetContents(ctx, owner, name, path, nil)
	if err != nil {
		return "", err
	}
	if f == nil {
		return "", fmt.Errorf("%s — это папка", path)
	}
	return f.GetContent()
}
