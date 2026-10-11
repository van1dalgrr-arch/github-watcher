// Package bot — Telegram-бот с командами и кнопками: /prs, /merge, /rerun…
//
// Это каркас: запуск, защита «отвечаю только хозяину» и список команд уже есть,
// сами команды пишем вместе — см. bot/README.md.
package bot

import (
	"context"
	"fmt"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/van1dalgrr-arch/github-watcher/bot/internal/command"
	"github.com/van1dalgrr-arch/github-watcher/internal/github"
)

type Bot struct {
	tg       *tgbot.Bot
	commands []command.Command
}

// New собирает бота. chatID — единственный, кому бот отвечает (тот же TELEGRAM_CHAT_ID, что для уведомлений).
func New(token string, chatID int64, gh github.Client) (*Bot, error) {
	commands := command.All(command.Deps{GH: gh})
	tg, err := tgbot.New(token,
		tgbot.WithMiddlewares(onlyOwner(chatID)),
		tgbot.WithDefaultHandler(func(ctx context.Context, b *tgbot.Bot, u *models.Update) {
			if u.Message != nil {
				_, _ = b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: u.Message.Chat.ID, Text: "Не знаю такой команды — /help"})
			}
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("telegram: %w", err)
	}
	for _, c := range commands {
		tg.RegisterHandler(tgbot.HandlerTypeMessageText, "/"+c.Name, tgbot.MatchTypeCommandStartOnly, c.Handle)
	}
	// TODO(вместе): кнопки — tg.RegisterHandler(tgbot.HandlerTypeCallbackQueryData, ..., keyboard.Parse …)
	return &Bot{tg: tg, commands: commands}, nil
}

// Run показывает команды в меню Telegram и слушает сообщения до отмены ctx.
func (b *Bot) Run(ctx context.Context) error {
	menu := make([]models.BotCommand, 0, len(b.commands))
	for _, c := range b.commands {
		menu = append(menu, models.BotCommand{Command: c.Name, Description: c.Description})
	}
	if _, err := b.tg.SetMyCommands(ctx, &tgbot.SetMyCommandsParams{Commands: menu}); err != nil {
		return fmt.Errorf("меню команд: %w", err)
	}
	b.tg.Start(ctx) // long polling: блокирует, пока ctx не отменят
	return nil
}

// onlyOwner — бот управляет твоими репозиториями, поэтому чужим не отвечает вовсе.
func onlyOwner(chatID int64) tgbot.Middleware {
	return func(next tgbot.HandlerFunc) tgbot.HandlerFunc {
		return func(ctx context.Context, b *tgbot.Bot, u *models.Update) {
			var from int64
			switch {
			case u.Message != nil:
				from = u.Message.Chat.ID
			case u.CallbackQuery != nil:
				from = u.CallbackQuery.From.ID
			}
			if from == chatID {
				next(ctx, b, u)
			}
		}
	}
}
