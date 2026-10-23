package bot

import (
	"time"

	"github.com/van1dalgrr-arch/github-watcher/bot/internal/command"
	tele "gopkg.in/telebot.v4"
)

func Run(token string) error {
	bot, err := tele.NewBot(tele.Settings{
		Token:  token,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil {
		return err
	}
	bot.Handle("/start", command.Start)
	bot.Handle("/help", command.Help)

	bot.Start()

	return nil
}
