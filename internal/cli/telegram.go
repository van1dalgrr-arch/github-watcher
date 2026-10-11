package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/van1dalgrr-arch/github-watcher/internal/config"
	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
	"github.com/van1dalgrr-arch/github-watcher/internal/github"
	"github.com/van1dalgrr-arch/github-watcher/internal/notify"
	"github.com/van1dalgrr-arch/github-watcher/internal/ui"
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

// sendDemo шлёт свежие события из GitHub в боевом формате: последний пуш вместе с его CI
// и по одному событию других видов.
func sendDemo(ctx context.Context, cfg *config.Config, tg *notify.Telegram) error {
	gh, err := github.New(cfg.GitHubToken)
	if err != nil {
		return err
	}
	events, err := gh.EventsSince(ctx, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		return err
	}
	ci := map[string][]domain.Event{} // CI по коммитам — чтобы приклеить к пушу
	for _, e := range events {
		if e.SHA != "" && (e.Kind == domain.EventCIPassed || e.Kind == domain.EventCIFailed) {
			ci[e.Repo+"@"+e.SHA] = append(ci[e.Repo+"@"+e.SHA], e)
		}
	}
	seen := map[domain.EventKind]bool{}
	sent := 0
	for i := len(events) - 1; i >= 0 && sent < 4; i-- { // EventsSince отдаёт от старых к новым
		e := events[i]
		if seen[e.Kind] {
			continue
		}
		seen[e.Kind] = true
		batch := []domain.Event{e}
		if e.Kind == domain.EventPush {
			batch = append(batch, ci[e.Repo+"@"+e.SHA]...)
			for _, c := range ci[e.Repo+"@"+e.SHA] {
				seen[c.Kind] = true // этот CI уже в сообщении о пуше
			}
		}
		logs := map[int64]string{}
		for _, b := range batch {
			if b.RunID != 0 {
				if r, err := gh.RunReport(ctx, b.Repo, b.RunID); err == nil {
					logs[b.RunID] = r
				}
			}
		}
		m := notify.Compose(batch, logs)
		m.Title = "[демо] " + m.Title
		if err := tg.Notify(ctx, m); err != nil {
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
