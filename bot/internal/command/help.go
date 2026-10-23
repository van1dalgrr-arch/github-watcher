package command

import tele "gopkg.in/telebot.v4"

func Help(c tele.Context) error {
	return c.Send(
		"Привет! Я GitHub-Watcher.\n\n" +
			"Отслеживаю события GitHub-аккаунта.\n\n" +
			"Доступные команды:\n" +
			"/start — информация о боте\n" +
			"/help — список команд\n" +
			"/repos — список репозиториев\n\n" +
			"В будущем: уведомления о коммитах, Pull Request и GitHub Actions.",
	)
}
