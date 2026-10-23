package command

import (
	"fmt"
	"strings"

	gh "github.com/van1dalgrr-arch/github-watcher/bot/github"
	tele "gopkg.in/telebot.v4"
)

const githubUsername = "van1dalgrr-arch"

func Repos(c tele.Context) error {
	repos, err := gh.GetRepos(githubUsername)
	if err != nil {
		return c.Send("❌ Не удалось получить репозитории GitHub.")
	}

	if len(repos) == 0 {
		return c.Send("Публичных репозиториев не найдено.")
	}

	var message strings.Builder

	fmt.Fprintf(&message, "📦 Репозитории @%s:\n\n", githubUsername)

	for i, repo := range repos {
		if i >= 10 {
			break
		}

		fmt.Fprintf(&message, "🔹 %s\n%s\n\n", repo.Name, repo.HTMLURL)
	}

	return c.Send(message.String())
}
