package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	gh "github.com/google/go-github/v92/github"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
)

// Actions: страница запуска (job-ы, шаги, артефакты), лог одного job-а, отмена, ручной запуск workflow.

func took(from, to time.Time) time.Duration {
	if from.IsZero() || to.IsZero() {
		return 0
	}
	return to.Sub(from).Round(time.Second)
}

func (c *client) Run(ctx context.Context, repo string, runID int64) (domain.RunDetail, error) {
	owner, name, err := split(repo)
	if err != nil {
		return domain.RunDetail{}, err
	}
	var (
		r    *gh.WorkflowRun
		jobs *gh.Jobs
		arts *gh.ArtifactList
	)
	err = parallel(
		func() (err error) { r, _, err = c.gh.Actions.GetWorkflowRunByID(ctx, owner, name, runID); return },
		func() (err error) {
			jobs, _, err = c.gh.Actions.ListWorkflowJobs(ctx, owner, name, runID, &gh.ListWorkflowJobsOptions{ListOptions: gh.ListOptions{PerPage: 100}})
			return
		},
		func() (err error) {
			arts, _, err = c.gh.Actions.ListWorkflowRunArtifacts(ctx, owner, name, runID, &gh.ListOptions{PerPage: 100})
			return
		},
	)
	if err != nil {
		return domain.RunDetail{}, err
	}
	d := domain.RunDetail{
		WorkflowRun: domain.WorkflowRun{
			Repo: repo, ID: r.GetID(), Name: r.GetName(), Branch: r.GetHeadBranch(), HeadSHA: r.GetHeadSHA(),
			Event: r.GetEvent(), Title: r.GetDisplayTitle(), Status: r.GetStatus(), Conclusion: r.GetConclusion(),
			URL: r.GetHTMLURL(), UpdatedAt: r.GetUpdatedAt().Time,
		},
		Actor: r.GetTriggeringActor().GetLogin(), Attempt: r.GetRunAttempt(),
	}
	if r.GetStatus() == "completed" {
		d.Took = took(r.GetRunStartedAt().Time, r.GetUpdatedAt().Time)
	} else if !r.GetRunStartedAt().IsZero() {
		d.Took = time.Since(r.GetRunStartedAt().Time).Round(time.Second)
	}
	for _, j := range jobs.Jobs {
		job := domain.Job{
			ID: j.GetID(), Name: j.GetName(), Status: j.GetStatus(), Conclusion: j.GetConclusion(), URL: j.GetHTMLURL(),
			Took: took(j.GetStartedAt().Time, j.GetCompletedAt().Time),
		}
		for _, s := range j.Steps {
			job.Steps = append(job.Steps, domain.Step{
				Name: s.GetName(), Status: s.GetStatus(), Conclusion: s.GetConclusion(),
				Took: took(s.GetStartedAt().Time, s.GetCompletedAt().Time),
			})
		}
		d.Jobs = append(d.Jobs, job)
	}
	for _, a := range arts.Artifacts {
		d.Artifacts = append(d.Artifacts, domain.Artifact{ID: a.GetID(), Name: a.GetName(), Size: a.GetSizeInBytes(), Expired: a.GetExpired()})
	}
	return d, nil
}

// JobLog — лог одного job-а (без меток времени).
func (c *client) JobLog(ctx context.Context, repo string, jobID int64) (string, error) {
	owner, name, err := split(repo)
	if err != nil {
		return "", err
	}
	return c.jobLog(ctx, owner, name, jobID)
}

func (c *client) CancelRun(ctx context.Context, repo string, runID int64) error {
	owner, name, err := split(repo)
	if err != nil {
		return err
	}
	_, err = c.gh.Actions.CancelWorkflowRunByID(ctx, owner, name, runID)
	var accepted *gh.AcceptedError // GitHub отвечает 202 — отмена принята
	if errors.As(err, &accepted) {
		return nil
	}
	return err
}

// DownloadArtifact скачивает zip артефакта в папку dir и возвращает путь к файлу.
func (c *client) DownloadArtifact(ctx context.Context, repo string, id int64, artName, dir string) (string, error) {
	owner, name, err := split(repo)
	if err != nil {
		return "", err
	}
	u, _, err := c.gh.Actions.DownloadArtifact(ctx, owner, name, id, 3)
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
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, artName+".zip")
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		return "", err
	}
	return path, f.Close()
}

func (c *client) Workflows(ctx context.Context, repo string) ([]domain.Workflow, error) {
	owner, name, err := split(repo)
	if err != nil {
		return nil, err
	}
	ws, _, err := c.gh.Actions.ListWorkflows(ctx, owner, name, &gh.ListOptions{PerPage: 100})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Workflow, 0, len(ws.Workflows))
	for _, w := range ws.Workflows {
		out = append(out, domain.Workflow{ID: w.GetID(), Name: w.GetName(), Path: w.GetPath(), State: w.GetState()})
	}
	return out, nil
}

// Dispatch — ручной запуск workflow на ветке ref (в workflow должен быть on: workflow_dispatch).
func (c *client) Dispatch(ctx context.Context, repo string, workflowID int64, ref string) error {
	owner, name, err := split(repo)
	if err != nil {
		return err
	}
	_, _, err = c.gh.Actions.CreateWorkflowDispatchEventByID(ctx, owner, name, workflowID, gh.CreateWorkflowDispatchEventRequest{Ref: ref})
	return err
}
