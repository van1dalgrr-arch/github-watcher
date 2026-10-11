package cli

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/van1dalgrr-arch/github-watcher/internal/config"
	"github.com/van1dalgrr-arch/github-watcher/internal/daemonclient"
	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
	"github.com/van1dalgrr-arch/github-watcher/internal/github"
	"github.com/van1dalgrr-arch/github-watcher/internal/ui"
)

type needRepo func() (github.Client, string, error)

func number(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimPrefix(s, "#"))
	if err != nil {
		return 0, fmt.Errorf("%q — не номер", s)
	}
	return n, nil
}

func repoCmd(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{Use: "repo", Short: "Репозитории"}
	cmd.AddCommand(&cobra.Command{
		Use: "list", Aliases: []string{"ls"}, Short: "Мои репозитории (свежие сверху)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			gh, err := github.New(cfg.GitHubToken)
			if err != nil {
				return err
			}
			repos, err := gh.Repos(cmd.Context())
			if err != nil {
				return err
			}
			rows := [][]string{}
			for _, r := range repos {
				vis := ""
				if r.Private {
					vis = ui.Warn.Render("private")
				}
				rows = append(rows, []string{ui.Bold.Render(r.FullName()), vis, ui.Faint.Render(r.Language),
					ui.Warn.Render("★") + strconv.Itoa(r.Stars), ui.Faint.Render(ui.Ago(r.PushedAt)), r.Description})
			}
			printTable(rows)
			return nil
		},
	})
	return cmd
}

func prCmd(need needRepo) *cobra.Command {
	cmd := &cobra.Command{Use: "pr", Short: "Pull requests"}
	var state string
	list := &cobra.Command{
		Use: "list", Aliases: []string{"ls"}, Short: "Список PR",
		RunE: func(cmd *cobra.Command, _ []string) error {
			gh, repo, err := need()
			if err != nil {
				return err
			}
			prs, err := gh.PullRequests(cmd.Context(), repo, state)
			if err != nil {
				return err
			}
			rows := [][]string{}
			for _, p := range prs {
				rows = append(rows, []string{ui.PRState(p), ui.Faint.Render("#" + strconv.Itoa(p.Number)), ui.CI(p.CIStatus),
					ui.Info.Render(p.Head), ui.Faint.Render("@" + p.Author), ui.Faint.Render(ui.Ago(p.UpdatedAt)), p.Title})
			}
			printTable(rows)
			return nil
		},
	}
	list.Flags().StringVarP(&state, "state", "s", "open", "open | closed | all")

	var method string
	merge := &cobra.Command{
		Use: "merge <номер>", Short: "Смёржить PR", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gh, repo, err := need()
			if err != nil {
				return err
			}
			n, err := number(args[0])
			if err != nil {
				return err
			}
			if err := gh.MergePR(cmd.Context(), repo, n, method); err != nil {
				return err
			}
			say(ok(fmt.Sprintf("PR #%d смёржен (%s)", n, method)))
			return nil
		},
	}
	merge.Flags().StringVarP(&method, "method", "m", "squash", "merge | squash | rebase")

	cmd.AddCommand(list, merge, commentCmd(need))
	return cmd
}

func issueCmd(need needRepo) *cobra.Command {
	cmd := &cobra.Command{Use: "issue", Short: "Issues"}
	var state, body string
	list := &cobra.Command{
		Use: "list", Aliases: []string{"ls"}, Short: "Список issues",
		RunE: func(cmd *cobra.Command, _ []string) error {
			gh, repo, err := need()
			if err != nil {
				return err
			}
			issues, err := gh.Issues(cmd.Context(), repo, state)
			if err != nil {
				return err
			}
			rows := [][]string{}
			for _, i := range issues {
				labels := ""
				if len(i.Labels) > 0 {
					labels = ui.Info.Render(strings.Join(i.Labels, ","))
				}
				rows = append(rows, []string{ui.IssueState(i), ui.Faint.Render("#" + strconv.Itoa(i.Number)), labels,
					ui.Faint.Render("@" + i.Author), ui.Faint.Render(ui.Ago(i.UpdatedAt)), i.Title})
			}
			printTable(rows)
			return nil
		},
	}
	list.Flags().StringVarP(&state, "state", "s", "open", "open | closed | all")

	create := &cobra.Command{
		Use: "create <заголовок>", Short: "Создать issue", Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gh, repo, err := need()
			if err != nil {
				return err
			}
			i, err := gh.CreateIssue(cmd.Context(), repo, strings.Join(args, " "), body)
			if err != nil {
				return err
			}
			say(ok(fmt.Sprintf("Issue #%d создан  %s", i.Number, ui.Faint.Render(i.URL))))
			return nil
		},
	}
	create.Flags().StringVarP(&body, "body", "b", "", "текст issue")

	closeCmd := &cobra.Command{
		Use: "close <номер>", Short: "Закрыть issue", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gh, repo, err := need()
			if err != nil {
				return err
			}
			n, err := number(args[0])
			if err != nil {
				return err
			}
			if err := gh.CloseIssue(cmd.Context(), repo, n); err != nil {
				return err
			}
			say(ok(fmt.Sprintf("Issue #%d закрыт", n)))
			return nil
		},
	}
	cmd.AddCommand(list, create, closeCmd, commentCmd(need))
	return cmd
}

func commentCmd(need needRepo) *cobra.Command {
	return &cobra.Command{
		Use: "comment <номер> <текст>", Short: "Оставить комментарий", Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			gh, repo, err := need()
			if err != nil {
				return err
			}
			n, err := number(args[0])
			if err != nil {
				return err
			}
			if err := gh.Comment(cmd.Context(), repo, n, strings.Join(args[1:], " ")); err != nil {
				return err
			}
			say(ok(fmt.Sprintf("Комментарий в #%d отправлен", n)))
			return nil
		},
	}
}

func runCmd(need needRepo) *cobra.Command {
	cmd := &cobra.Command{Use: "run", Short: "GitHub Actions"}
	list := &cobra.Command{
		Use: "list", Aliases: []string{"ls"}, Short: "Последние запуски",
		RunE: func(cmd *cobra.Command, _ []string) error {
			gh, repo, err := need()
			if err != nil {
				return err
			}
			runs, err := gh.WorkflowRuns(cmd.Context(), repo)
			if err != nil {
				return err
			}
			rows := [][]string{}
			for _, r := range runs {
				rows = append(rows, []string{ui.CI(r.State()), ui.Faint.Render(strconv.FormatInt(r.ID, 10)), ui.Bold.Render(r.Name),
					ui.Info.Render(r.Branch), ui.Faint.Render(r.Event), ui.Faint.Render(ui.Ago(r.UpdatedAt)), r.Title})
			}
			printTable(rows)
			return nil
		},
	}
	logs := &cobra.Command{
		Use: "logs <id>", Short: "Логи запуска", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gh, repo, err := need()
			if err != nil {
				return err
			}
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("%q — не id запуска", args[0])
			}
			text, err := gh.RunLogs(cmd.Context(), repo, id)
			if err != nil {
				return err
			}
			_, _ = lipgloss.Print(renderLog(text))
			return nil
		},
	}
	var failed bool
	rerun := &cobra.Command{
		Use: "rerun <id>", Short: "Перезапустить", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gh, repo, err := need()
			if err != nil {
				return err
			}
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("%q — не id запуска", args[0])
			}
			if err := gh.RerunWorkflow(cmd.Context(), repo, id, failed); err != nil {
				return err
			}
			say(ok("Запуск перезапущен"))
			return nil
		},
	}
	rerun.Flags().BoolVar(&failed, "failed", false, "только упавшие job-ы")
	cmd.AddCommand(list, logs, rerun)
	return cmd
}

// renderLog подсвечивает лог Actions: заголовки job-ов, группы, ошибки.
func renderLog(text string) string {
	lines, _ := ui.LogLines(text)
	return strings.Join(lines, "\n") + "\n"
}

func eventsCmd(cfg *config.Config) *cobra.Command {
	var limit int
	var repo string
	cmd := &cobra.Command{
		Use: "events", Short: "Лента событий (нужен запущенный gw daemon)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			events, err := daemonclient.New(cfg.DaemonURL).Events(cmd.Context(), repo, limit)
			if err != nil {
				return err
			}
			rows := [][]string{}
			for i := len(events) - 1; i >= 0; i-- { // старые сверху — как в чате
				e := events[i]
				rows = append(rows, []string{ui.Faint.Render(ui.Ago(e.CreatedAt)), ui.Kind(e.Kind), ui.Bold.Render(e.Repo), eventTitle(e)})
			}
			printTable(rows)
			return nil
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 30, "сколько событий")
	cmd.Flags().StringVar(&repo, "for", "", "только этот репозиторий")
	return cmd
}

func eventTitle(e domain.Event) string {
	if e.Actor == "" {
		return e.Title
	}
	return e.Title + ui.Faint.Render("  @"+e.Actor)
}

func statusCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use: "status", Short: "Состояние демона",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := daemonclient.New(cfg.DaemonURL).Status(cmd.Context())
			if err != nil {
				return err
			}
			say(ok("демон работает " + ui.Faint.Render("с "+s.Started.Format("02.01 15:04"))))
			say(dim(fmt.Sprintf("  опрос каждые %s, последний — %s назад", ui.Interval(s.Interval), ui.Ago(s.LastTick))))
			say(dim(fmt.Sprintf("  событий сохранено: %d, уведомлений отправлено: %d", s.Saved, s.Sent)))
			if s.LastError != "" {
				say(ui.Bad.Render("  последняя ошибка: " + s.LastError))
			}
			if cfg.TelegramChat == "" {
				say(ui.Warn.Render("  telegram не настроен: gw telegram <bot-token>"))
			}
			return nil
		},
	}
}
