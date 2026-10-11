package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github-watcher/internal/config"
	"github-watcher/internal/notify"
)

// telegramCmd — мастер настройки: токен бота → ждём сообщение от тебя → сохраняем chat id → тестовое уведомление.
func telegramCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "telegram [bot-token]",
		Short: "Подключить уведомления на телефон через Telegram-бота",
		Long: `1. В Telegram напиши @BotFather → /newbot → получи токен.
2. gw telegram <токен>
3. Напиши своему боту любое сообщение — gw поймает chat id, сохранит и пришлёт тест.

Без аргументов — отправить тестовое уведомление с текущими настройками.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if len(args) == 0 {
				if cfg.TelegramBot == "" || cfg.TelegramChat == "" {
					return fmt.Errorf("telegram не настроен: gw telegram <bot-token>")
				}
				if err := notify.NewTelegram(cfg.TelegramBot, cfg.TelegramChat).Notify(ctx, eventTest); err != nil {
					return err
				}
				fmt.Println(ok("Тест отправлен — проверь телефон"))
				return nil
			}
			return setupTelegram(ctx, args[0])
		},
	}
}

func setupTelegram(ctx context.Context, token string) error {
	tg := notify.NewTelegram(token, "")
	fmt.Println(dim("Напиши своему боту в Telegram любое сообщение (жду 2 минуты)…"))
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for {
		chats, err := tg.ChatIDs(ctx)
		if err != nil {
			return err
		}
		for id, name := range chats {
			if err := config.Save(map[string]string{"TELEGRAM_BOT_TOKEN": token, "TELEGRAM_CHAT_ID": id}); err != nil {
				return err
			}
			tg.ChatID = id
			if err := tg.Notify(ctx, eventTest); err != nil {
				return err
			}
			fmt.Println(ok(fmt.Sprintf("Подключено: %s (chat %s). Настройки в %s", name, id, config.Path())))
			fmt.Println(dim("Перезапусти `gw daemon`, чтобы он начал слать уведомления."))
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("сообщение боту так и не пришло")
		case <-time.After(2 * time.Second):
		}
	}
}
