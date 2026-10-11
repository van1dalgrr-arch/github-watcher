# github-watcher

![ci](https://github.com/van1dalgrr-arch/github-watcher/actions/workflows/ci.yml/badge.svg)

Go + Gin + Postgres. `/health` проверяет и приложение, и базу.

```bash
up               # база в Docker + приложение с hot reload
make help        # все команды
make test        # тесты
make migrate-up  # миграции
make up          # всё в Docker
curl localhost:8080/health
```
