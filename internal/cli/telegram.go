package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github-watcher/internal/config"
	"github-watcher/internal/domain"
	"github-watcher/internal/github"
	"github-watcher/internal/notify"
	"github-watcher/internal/ui"
)

// telegramCmd — мастер настройки: токен бота → ждём сообщение от тебя → сохраняем chat id → тестовое уведомление.
func telegramCmd(cfg *config.Config) *cobra.Command {
	var demo bool
	cmd := &cobra.Command{
		Use:   "telegram [bot-token]",
		Short: "Подключить уведомления на телефон через Telegram-бота",
		Long: `1. В Telegram напиши @BotFather → /newbot → получи токен.
2. gw telegram <токен>
3. Напиши своему боту любое сообщение — gw поймает chat id, сохранит и пришлёт тест.

Без аргументов — отправить тестовое уведомление с текущими настройками.
С --demo — прислать последние реальные события из твоих репозиториев (по одному каждого вида).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if len(args) == 0 {
				if cfg.TelegramBot == "" || cfg.TelegramChat == "" {
					return fmt.Errorf("telegram не настроен: gw telegram <bot-token>")
				}
				tg := notify.NewTelegram(cfg.TelegramBot, cfg.TelegramChat)
				if demo {
					return sendDemo(ctx, cfg, tg)
				}
				if err := tg.Notify(ctx, eventTest); err != nil {
					return err
				}
				say(ok("Тест отправлен — проверь телефон"))
				return nil
			}
			return setupTelegram(ctx, args[0])
		},
	}
	cmd.Flags().BoolVar(&demo, "demo", false, "прислать последние реальные события, чтобы посмотреть, как выглядят уведомления")
	return cmd
}

// sendDemo шлёт свежие события из GitHub — по одному каждого вида, самые новые.
func sendDemo(ctx context.Context, cfg *config.Config, tg *notify.Telegram) error {
	gh, err := github.New(cfg.GitHubToken)
	if err != nil {
		return err
	}
	events, err := gh.EventsSince(ctx, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		return err
	}
	seen := map[domain.EventKind]bool{}
	sent := 0
	for i := len(events) - 1; i >= 0 && sent < 6; i-- { // EventsSince отдаёт от старых к новым
		e := events[i]
		if seen[e.Kind] {
			continue
		}
		seen[e.Kind] = true
		e.Title = "[демо] " + e.Title
		if err := tg.Notify(ctx, e); err != nil {
			return err
		}
		sent++
	}
	if sent == 0 {
		say(dim("За 30 дней в твоих репозиториях событий не нашлось"))
		return nil
	}
	say(ok(fmt.Sprintf("Отправлено %d %s — проверь телефон", sent, ui.Plural(sent, "уведомление", "уведомления", "уведомлений"))))
	return nil
}

func setupTelegram(ctx context.Context, token string) error {
	tg := notify.NewTelegram(token, "")
	say(dim("Напиши своему боту в Telegram любое сообщение (жду 2 минуты)…"))
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
			say(ok(fmt.Sprintf("Подключено: %s (chat %s). Настройки в %s", name, id, config.Path())))
			say(dim("Перезапусти `gw daemon`, чтобы он начал слать уведомления."))
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("сообщение боту так и не пришло")
		case <-time.After(2 * time.Second):
		}
	}
}
