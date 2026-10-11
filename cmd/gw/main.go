package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github-watcher/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	root := cli.New()
	root.SetContext(ctx)
	code := cli.Execute(root)
	stop()
	os.Exit(code)
}
