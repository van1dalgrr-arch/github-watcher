package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/van1dalgrr-arch/github-watcher/bot"
	"github.com/van1dalgrr-arch/github-watcher/internal/config"
)

func botCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "bot",
		Short: "Start telegram bot",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cfg.TelegramBot == "" {
				return fmt.Errorf("bot token is empty: run gw telegram <token> first")
			}
			return bot.Run(cfg.TelegramBot)
		},
	}
}
