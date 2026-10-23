package command

import (
	tele "gopkg.in/telebot.v4"
)

func Start(c tele.Context) error {
	return c.Send("Привет! я телеграмм бот наблюдающий за Github аккаунтом: van1dalgrr-arch")
}
