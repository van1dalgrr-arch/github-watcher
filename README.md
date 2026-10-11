# gw — GitHub в терминале

> **Код написан нейросетью.** Весь код, тесты и документацию в этом репозитории написал
> ИИ-ассистент Claude (Anthropic) в Claude Code. Автор репозитория ставил задачи и принимал решения
> (что должен уметь сервис, где его запускать), но код сам не писал.
> Коммиты с кодом помечены строкой `Co-Authored-By: Claude`.

Дашборд, репозитории, pull requests, issues, GitHub Actions с логами и живая лента событий — в одном TUI.
Фоновый демон следит за GitHub и шлёт важное на телефон через Telegram.

```
 ◆ gw  me/api                                                            @me  ● daemon
 1 Dashboard   2 Repos   3 Pull requests 2   4 Issues 1   5 Actions 1   6 Events   7 Logs

 Pull requests · open · 2
▌ ◆ #7    ✓ Добавить кэш                          feat/cache → main     @alice    1ч
  ◆ #6    ✗ Починить логин                        fix/login → main      @bob      3ч
                                   enter открыть  m merge  c коммент  s open  ? помощь
```

## Установка

```sh
make install        # gw появится в ~/go/bin
gh auth login       # если ещё не залогинен — gw возьмёт токен оттуда
```

## Использование

```sh
gw                  # TUI; репозиторий — из git remote текущей папки
gw -R owner/repo    # TUI сразу на нужном репозитории

gw daemon           # фоновый сервис: лента + уведомления (держи в отдельной вкладке или на сервере)
gw telegram TOKEN   # подключить телефон: токен от @BotFather, потом напиши боту — chat id поймается сам
gw status           # жив ли демон, сколько событий и уведомлений

gw repo list
gw pr list -s all            gw pr merge 42 -m squash      gw pr comment 42 "LGTM"
gw issue list                gw issue create "Баг" -b "…"  gw issue close 7
gw run list                  gw run logs <id>              gw run rerun <id> --failed
gw events -n 50
```

### Клавиши в TUI

| | |
|---|---|
| `1`…`7`, `tab` | вкладки |
| `j k` / `↑ ↓`, `g G` | по списку |
| `/` | фильтр, `esc` — сбросить |
| `enter` | выбрать репозиторий / подробности PR и issue / логи запуска |
| `m` `c` `s` | merge PR, комментарий, open/closed/all |
| `n` `x` | новый issue, закрыть issue |
| `R` `f` | перезапустить запуск целиком / только упавшие job-ы |
| `e` `E` | в логах — к следующей / предыдущей ошибке |
| `o` | открыть в браузере |
| `r` `?` `q` | обновить, помощь, выход |

## Уведомления без своего сервера

Держать `gw daemon` включённым не обязательно: workflow [notify.yml](.github/workflows/notify.yml)
раз в 30 минут запускает `gw notify` на серверах GitHub. Он проверяет новое, шлёт важное в Telegram
и выходит. Состояние между запусками хранится в переменной репозитория `GW_STATE`.

Нужны секреты репозитория: `GW_GITHUB_TOKEN` (`gh auth token | gh secret set GW_GITHUB_TOKEN`),
`TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID`. Ручной запуск — кнопка *Run workflow* на вкладке Actions.

## Настройки

Файл `~/.config/gw/config` (`KEY=VALUE`), переменные окружения важнее. Все ключи — в [.env.example](.env.example).
Самое полезное:

- `NOTIFY_KINDS` — что слать на телефон: по умолчанию PR, ревью, issues, комментарии, упавший CI, релизы,
  упоминания; `all` — всё; или список `ci_failed,mention`.
- `NOTIFY_REPOS` — только эти репозитории.
- `POLL_INTERVAL` — как часто демон ходит в GitHub (по умолчанию `1m`).

## Разработка

```sh
make test   make lint   make vuln   make run   make daemon
```

Как устроено — [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).
