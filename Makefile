.PHONY: help run daemon test lint vuln build install

help:    ## эта справка
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  \033[35m%-9s\033[0m %s\n", $$1, $$2}'
run:     ## открыть TUI
	go run ./cmd/gw
daemon:  ## запустить демона
	go run ./cmd/gw daemon
test:    ## тесты с детектором гонок
	go test -race -count=1 ./...
lint:    ## golangci-lint
	golangci-lint run ./...
vuln:    ## уязвимости в зависимостях
	govulncheck ./...
build:   ## бинарник в bin/
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/gw ./cmd/gw
install: ## поставить gw в $GOPATH/bin — команда доступна из любой папки
	CGO_ENABLED=0 go install -trimpath -ldflags="-s -w" ./cmd/gw
