// Package keyboard — кнопки под сообщениями бота (inline keyboard) и разбор нажатий.
//
// Нажатие на кнопку приходит боту как callback с короткой строкой данных (до 64 байт).
// Договоримся о формате "действие:репозиторий:номер", например "merge:me/app:42".
// Пишем вместе: какие кнопки где показывать и что они делают.
package keyboard

import (
	"strconv"
	"strings"

	"github.com/go-telegram/bot/models"
)

// Action — что нажали: Do — действие (merge, rerun, close…), Repo и Number — над чем.
type Action struct {
	Do     string
	Repo   string
	Number int64
}

// Data — строка для кнопки: Action{"merge", "me/app", 42} → "merge:me/app:42".
func (a Action) Data() string {
	return a.Do + ":" + a.Repo + ":" + strconv.FormatInt(a.Number, 10)
}

// Parse — обратно из строки кнопки; ok=false, если формат не наш.
func Parse(data string) (a Action, ok bool) {
	parts := strings.SplitN(data, ":", 3)
	if len(parts) != 3 {
		return a, false
	}
	n, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return a, false
	}
	return Action{Do: parts[0], Repo: parts[1], Number: n}, true
}

// Button — одна кнопка с действием.
func Button(text string, a Action) models.InlineKeyboardButton {
	return models.InlineKeyboardButton{Text: text, CallbackData: a.Data()}
}

// Link — кнопка-ссылка (открывает браузер, боту ничего не приходит).
func Link(text, url string) models.InlineKeyboardButton {
	return models.InlineKeyboardButton{Text: text, URL: url}
}

// Rows — клавиатура из рядов кнопок.
func Rows(rows ...[]models.InlineKeyboardButton) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}
