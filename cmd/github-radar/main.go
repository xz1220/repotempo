package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/xz1220/repotempo/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cli := app.NewCLI()
	cli.Stdout = os.Stdout
	cli.Stderr = os.Stderr
	os.Exit(cli.Run(ctx, os.Args[1:]))
}
