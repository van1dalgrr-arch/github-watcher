package cli

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/van1dalgrr-arch/github-watcher/internal/config"
	"github.com/van1dalgrr-arch/github-watcher/internal/domain"
	"github.com/van1dalgrr-arch/github-watcher/internal/github"
	"github.com/van1dalgrr-arch/github-watcher/internal/logbuf"
	"github.com/van1dalgrr-arch/github-watcher/internal/notify"
	"github.com/van1dalgrr-arch/github-watcher/internal/server"
	"github.com/van1dalgrr-arch/github-watcher/internal/store"
	"github.com/van1dalgrr-arch/github-watcher/internal/watcher"
)

// daemonCmd — `gw daemon`: фоновый сервис, следит за GitHub и шлёт уведомления на телефон.
func daemonCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "daemon",
		Short: "Фоновый сервис: следит за GitHub, копит ленту, шлёт уведомления в Telegram",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDaemon(cmd.Context(), *cfg)
		},
	}
}

func runDaemon(ctx context.Context, cfg config.Config) error {
	ring := logbuf.New(500)
	log := slog.New(slog.NewTextHandler(io.MultiWriter(os.Stdout, ring), nil))

	gh, err := github.New(cfg.GitHubToken)
	if err != nil {
		return err
	}
	me, err := gh.Me(ctx)
	if err != nil {
		return err
	}

	db, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	var notifier notify.Notifier = notify.Nop{}
	if cfg.TelegramBot != "" && cfg.TelegramChat != "" {
		notifier = notify.NewTelegram(cfg.TelegramBot, cfg.TelegramChat)
		log.Info("telegram enabled", "chat", cfg.TelegramChat)
	} else {
		log.Warn("telegram not configured — уведомления только в ленте TUI; настрой: gw telegram <bot-token>")
	}

	w := &watcher.Watcher{
		GH:       gh,
		Store:    db,
		Notifier: notifier,
		Rules:    notify.ParseRules(cfg.NotifyKinds, cfg.NotifyRepos),
		Interval: cfg.PollInterval,
		Log:      log,
	}

	r := server.New(server.Deps{DB: db, Events: db, Status: w.Status, LogTail: ring.Lines})

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: r, ReadHeaderTimeout: 5 * time.Second}
	errc := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()
	go func() { _ = w.Run(ctx) }()
	log.Info("daemon started", "user", me, "addr", srv.Addr, "db", cfg.DBPath)

	select {
	case <-ctx.Done(): // Ctrl+C или SIGTERM от Docker — дать запросам доработать
	case err := <-errc:
		return err
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
	log.Info("daemon stopped")
	return nil
}

// eventTest — пробное сообщение для `gw telegram`.
var eventTest = notify.Message{
	Kind:  domain.EventDaemonInfo,
	Repo:  "gw",
	Title: "Связь есть! Сюда будут приходить события из GitHub.",
}
