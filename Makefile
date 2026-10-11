.PHONY: help run dev test lint up down build migrate-up migrate-down
DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/app?sslmode=disable

help:  ## эта справка
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  \033[35m%-13s\033[0m %s\n", $$1, $$2}'
run:   ## запустить локально
	go run ./cmd/api
dev:   ## hot reload (air)
	air
test:  ## тесты с детектором гонок
	go test -race -count=1 ./...
lint:  ## golangci-lint
	golangci-lint run ./...
up:    ## всё в Docker: база + приложение
	docker compose up -d --build
down:  ## остановить
	docker compose down
build: ## бинарник в bin/
	CGO_ENABLED=0 go build -o bin/app ./cmd/api
migrate-up:   ## применить миграции
	migrate -path migrations -database "$(DATABASE_URL)" up
migrate-down: ## откатить последнюю
	migrate -path migrations -database "$(DATABASE_URL)" down 1
