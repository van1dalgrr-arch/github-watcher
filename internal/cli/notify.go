package cli

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github-watcher/internal/config"
	"github-watcher/internal/github"
	"github-watcher/internal/notify"
	"github-watcher/internal/store"
	"github-watcher/internal/watcher"
)

// stateVar — переменная Actions, в которой `gw notify` хранит состояние между запусками.
const stateVar = "GW_STATE"

// notifyCmd — `gw notify`: одна проверка «GitHub → телефон» и выход. Для cron в GitHub Actions,
// когда держать `gw daemon` негде. Состояние — в переменной репозитория, базы не нужно.
func notifyCmd(cfg *config.Config) *cobra.Command {
	var stateRepo string
	cmd := &cobra.Command{
		Use:   "notify",
		Short: "Одна проверка: новые события → Telegram (для запуска по расписанию)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if cfg.TelegramBot == "" || cfg.TelegramChat == "" {
				return errors.New("нужны TELEGRAM_BOT_TOKEN и TELEGRAM_CHAT_ID")
			}
			if stateRepo == "" {
				return errors.New("укажи --state owner/repo (в Actions подставляется сам из GITHUB_REPOSITORY)")
			}
			gh, err := github.New(cfg.GitHubToken)
			if err != nil {
				return err
			}
			raw, err := gh.Variable(ctx, stateRepo, stateVar)
			if err != nil {
				return fmt.Errorf("состояние: %w", err)
			}
			mem := store.NewMemory(raw)
			w := &watcher.Watcher{
				GH:       gh,
				Store:    mem,
				Notifier: notify.NewTelegram(cfg.TelegramBot, cfg.TelegramChat),
				Rules:    notify.ParseRules(cfg.NotifyKinds, cfg.NotifyRepos),
				Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
			}
			saved, sent, tickErr := w.Once(ctx)
			// состояние сохраняем и при ошибке: то, что уже ушло на телефон, не должно прийти второй раз
			if err := gh.SetVariable(ctx, stateRepo, stateVar, mem.State()); err != nil {
				return fmt.Errorf("сохранить состояние: %w", err)
			}
			if tickErr != nil {
				return tickErr
			}
			// в логе Actions — только числа: заголовки событий из приватных репозиториев туда не попадают
			say(fmt.Sprintf("новых событий: %d, отправлено: %d", saved, sent))
			return nil
		},
	}
	cmd.Flags().StringVar(&stateRepo, "state", os.Getenv("GITHUB_REPOSITORY"), "репозиторий, где хранить состояние (переменная "+stateVar+")")
	return cmd
}
