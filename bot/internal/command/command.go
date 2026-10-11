// Package command — команды бота. Одна команда = имя, описание для меню и обработчик.
// Новая команда: дописать её в All() и сделать обработчик (лучше — в своём файле: prs.go, merge.go…).
package command

import (
	"context"
	"strings"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/van1dalgrr-arch/github-watcher/internal/github"
)

// Deps — то, чем команды пользуются. GH — тот же клиент GitHub, что у TUI и CLI.
type Deps struct {
	GH github.Client
}

type Command struct {
	Name        string // без слэша: "prs"
	Description string // видно в меню Telegram
	Handle      tgbot.HandlerFunc
}

// All — все команды бота в порядке меню.
func All(d Deps) []Command {
	cmds := []Command{
		{Name: "prs", Description: "открытые pull requests", Handle: todo("prs")},
		{Name: "issues", Description: "открытые issues", Handle: todo("issues")},
		{Name: "runs", Description: "последние запуски Actions", Handle: todo("runs")},
		{Name: "merge", Description: "смёржить PR: /merge 42", Handle: todo("merge")},
		{Name: "rerun", Description: "перезапустить упавший CI", Handle: todo("rerun")},
		{Name: "status", Description: "что происходит в репозиториях", Handle: todo("status")},
	}
	help := helpText(cmds)
	return append([]Command{
		{Name: "start", Description: "что умеет бот", Handle: reply("Привет! Я gw — твой GitHub в Telegram.\n\n" + help)},
		{Name: "help", Description: "список команд", Handle: reply(help)},
	}, cmds...)
}

func helpText(cmds []Command) string {
	var b strings.Builder
	b.WriteString("Команды:\n")
	for _, c := range cmds {
		b.WriteString("/" + c.Name + " — " + c.Description + "\n")
	}
	return b.String()
}

// reply — обработчик, который просто отвечает текстом.
func reply(text string) tgbot.HandlerFunc {
	return func(ctx context.Context, b *tgbot.Bot, u *models.Update) {
		_, _ = b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: u.Message.Chat.ID, Text: text})
	}
}

// todo — заглушка для команд, которые пишем вместе.
func todo(name string) tgbot.HandlerFunc {
	return reply("🚧 /" + name + " ещё не написана — пишем вместе (bot/internal/command)")
}
