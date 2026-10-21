package bot

import (
	"time"

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

	bot.Handle("/start", func(c tele.Context) error {
		return c.Send("Привет! я github-Watcher, я наблюдаю за событиями Github аккаунта (van1dal-arch)")
	})
	bot.Start()

	return nil
}
