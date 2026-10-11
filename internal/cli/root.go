// Package cli — команды `gw`. Без аргументов открывается TUI, с аргументами — обычный CLI для скриптов.
package cli

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/van1dalgrr-arch/github-watcher/internal/config"
	"github.com/van1dalgrr-arch/github-watcher/internal/github"
	"github.com/van1dalgrr-arch/github-watcher/internal/tui"
	"github.com/van1dalgrr-arch/github-watcher/internal/ui"
)

func New() *cobra.Command {
	cfg := config.Load()
	var repo string
	root := &cobra.Command{
		Use:   "gw",
		Short: "GitHub в терминале: репозитории, PR, issues, Actions и лента событий",
		Long: `gw — GitHub в терминале.

  gw                 интерактивный интерфейс
  gw daemon          фоновый сервис: лента событий + уведомления на телефон
  gw telegram TOKEN  подключить Telegram`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			gh, err := github.New(cfg.GitHubToken)
			if err != nil {
				return err
			}
			return tui.Run(cmd.Context(), gh, cfg, repoOrGit(repo))
		},
	}
	root.PersistentFlags().StringVarP(&repo, "repo", "R", "", "репозиторий owner/name (по умолчанию — из git remote текущей папки)")

	need := func() (github.Client, string, error) {
		gh, err := github.New(cfg.GitHubToken)
		if err != nil {
			return nil, "", err
		}
		r := repoOrGit(repo)
		if r == "" {
			return nil, "", fmt.Errorf("не понял, какой репозиторий: запусти внутри git-репозитория или укажи -R owner/name")
		}
		return gh, r, nil
	}
	root.AddCommand(
		daemonCmd(&cfg),
		telegramCmd(&cfg),
		notifyCmd(&cfg),
		botCmd(&cfg),
		statusCmd(&cfg),
		eventsCmd(&cfg),
		repoCmd(&cfg),
		prCmd(need),
		issueCmd(need),
		runCmd(need),
	)
	return root
}

// Execute запускает CLI и печатает ошибку красиво.
func Execute(cmd *cobra.Command) int {
	if err := cmd.Execute(); err != nil {
		_, _ = lipgloss.Fprintln(os.Stderr, ui.Bad.Render("✗ ")+err.Error())
		return 1
	}
	return 0
}

var remoteRe = regexp.MustCompile(`github\.com[:/]([^/]+)/(.+?)(?:\.git)?/?$`)

// repoOrGit — явный -R или owner/name из `git remote get-url origin` текущей папки.
func repoOrGit(flag string) string {
	if flag != "" {
		return flag
	}
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	m := remoteRe.FindStringSubmatch(strings.TrimSpace(string(out)))
	if m == nil {
		return ""
	}
	return m[1] + "/" + m[2]
}

func ok(s string) string  { return ui.Good.Render("✓ ") + s }
func dim(s string) string { return ui.Faint.Render(s) }

// say печатает строку, подстраивая цвета под терминал (без цветов — если вывод в pipe).
func say(s string) { _, _ = lipgloss.Println(s) }
