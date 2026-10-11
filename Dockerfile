# образ демона (gw daemon) — для сервера, чтобы уведомления шли 24/7, даже когда ноутбук закрыт
# docker run -d -e GITHUB_TOKEN=… -e TELEGRAM_BOT_TOKEN=… -e TELEGRAM_CHAT_ID=… -v gw:/data -p 7777:7777 gw

# сборка
FROM golang:1.27-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /gw ./cmd/gw

# запуск: только бинарник, без shell и пакетов
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /gw /gw

ENV GW_DB=/data/events.json

VOLUME /data

EXPOSE 7777

ENTRYPOINT ["/gw", "daemon"]
