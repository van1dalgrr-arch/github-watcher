package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	gh "github.com/google/go-github/v92/github"

	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
)

// Профиль: REST — о себе, репозитории, звёзды; GraphQL — сетка вкладов и закреплённые (в REST их нет).

const profileQuery = `query($login: String!) {
  user(login: $login) {
    contributionsCollection { contributionCalendar { totalContributions
      weeks { contributionDays { date contributionCount contributionLevel } } } }
    pinnedItems(first: 6, types: REPOSITORY) { nodes { ... on Repository {
      name owner { login } description stargazerCount isPrivate primaryLanguage { name } } } }
  }
}`

type profileGQL struct {
	Data struct {
		User struct {
			ContributionsCollection struct {
				ContributionCalendar struct {
					TotalContributions int
					Weeks              []struct {
						ContributionDays []struct {
							Date              string
							ContributionCount int
							ContributionLevel string // NONE, FIRST_QUARTILE … FOURTH_QUARTILE
						}
					}
				}
			}
			PinnedItems struct {
				Nodes []struct {
					Name            string
					Owner           struct{ Login string }
					Description     string
					StargazerCount  int
					IsPrivate       bool
					PrimaryLanguage *struct{ Name string }
				}
			}
		}
	}
	Errors []struct{ Message string }
}

var levels = map[string]int{"NONE": 0, "FIRST_QUARTILE": 1, "SECOND_QUARTILE": 2, "THIRD_QUARTILE": 3, "FOURTH_QUARTILE": 4}

func (c *client) graphql(ctx context.Context, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.github.com/graphql", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GraphQL: HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func toRepo(r *gh.Repository) domain.Repo {
	return domain.Repo{
		Owner: r.GetOwner().GetLogin(), Name: r.GetName(), Description: r.GetDescription(), Language: r.GetLanguage(),
		Private: r.GetPrivate(), Fork: r.GetFork(), Archived: r.GetArchived(), Stars: r.GetStargazersCount(),
		OpenIssues: r.GetOpenIssuesCount(), URL: r.GetHTMLURL(), PushedAt: r.GetPushedAt().Time,
	}
}

func (c *client) Profile(ctx context.Context, login string) (domain.Profile, error) {
	var (
		u   *gh.User
		gql profileGQL
		p   domain.Profile
	)
	err := parallel(
		func() (err error) { u, _, err = c.gh.Users.Get(ctx, login); return },
		func() error {
			if err := c.graphql(ctx, profileQuery, map[string]any{"login": login}, &gql); err != nil {
				return err
			}
			if len(gql.Errors) > 0 {
				return errors.New("GraphQL: " + gql.Errors[0].Message)
			}
			return nil
		},
		func() error {
			rs, _, err := c.gh.Repositories.ListByUser(ctx, login, &gh.RepositoryListByUserOptions{Sort: "pushed", ListOptions: gh.ListOptions{PerPage: 100}})
			for _, r := range rs {
				p.Repos = append(p.Repos, toRepo(r))
			}
			return err
		},
		func() error {
			rs, _, err := c.gh.Activity.ListStarred(ctx, login, &gh.ActivityListStarredOptions{Sort: "created", ListOptions: gh.ListOptions{PerPage: 100}})
			for _, r := range rs {
				p.Starred = append(p.Starred, toRepo(r.GetRepository()))
			}
			return err
		},
	)
	if err != nil {
		return domain.Profile{}, err
	}
	p.Login, p.Name, p.Bio = u.GetLogin(), u.GetName(), u.GetBio()
	p.Company, p.Location, p.Website = u.GetCompany(), u.GetLocation(), u.GetBlog()
	p.Followers, p.Following, p.PublicRepos = u.GetFollowers(), u.GetFollowing(), u.GetPublicRepos()
	p.CreatedAt = u.GetCreatedAt().Time
	cal := gql.Data.User.ContributionsCollection.ContributionCalendar
	p.Contributions = cal.TotalContributions
	for _, w := range cal.Weeks {
		var week []domain.Day
		for _, d := range w.ContributionDays {
			t, _ := time.Parse("2006-01-02", d.Date)
			week = append(week, domain.Day{Date: t, Count: d.ContributionCount, Level: levels[d.ContributionLevel]})
		}
		p.Weeks = append(p.Weeks, week)
	}
	for _, n := range gql.Data.User.PinnedItems.Nodes {
		r := domain.Repo{Owner: n.Owner.Login, Name: n.Name, Description: n.Description, Stars: n.StargazerCount, Private: n.IsPrivate}
		if n.PrimaryLanguage != nil {
			r.Language = n.PrimaryLanguage.Name
		}
		p.Pinned = append(p.Pinned, r)
	}
	return p, nil
}
