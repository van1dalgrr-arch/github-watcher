package bot

import (
	"time"

	"github.com/van1dalgrr-arch/github-watcher/bot/internal/command"
	tele "gopkg.in/telebot.v4"
)

func Run(token string) error {
	b, err := tele.NewBot(tele.Settings{
		Token: token,
		Poller: &tele.LongPoller{
			Timeout: 10 * time.Second,
		},
	})
	if err != nil {
		return err
	}

	b.Handle("/start", command.Start)
	b.Handle("/help", command.Help)
	b.Handle("/repos", command.Repos)

	b.Start()

	return nil
}
