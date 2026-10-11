package cli

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/van1dalgrr-arch/github-watcher/bot"
	"github.com/van1dalgrr-arch/github-watcher/internal/config"
	"github.com/van1dalgrr-arch/github-watcher/internal/github"
)

// botCmd — `gw bot`: Telegram-бот с командами. Работает, пока запущен (long polling).
func botCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "bot",
		Short: "Telegram-бот с командами (/prs, /merge…) — каркас, пишем вместе",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cfg.TelegramBot == "" || cfg.TelegramChat == "" {
				return errors.New("telegram не настроен: gw telegram <bot-token>")
			}
			chatID, err := strconv.ParseInt(cfg.TelegramChat, 10, 64)
			if err != nil {
				return fmt.Errorf("TELEGRAM_CHAT_ID %q — не число", cfg.TelegramChat)
			}
			gh, err := github.New(cfg.GitHubToken)
			if err != nil {
				return err
			}
			b, err := bot.New(cfg.TelegramBot, chatID, gh)
			if err != nil {
				return err
			}
			say(ok("бот запущен — напиши ему /start (Ctrl+C — остановить)"))
			return b.Run(cmd.Context())
		},
	}
}
