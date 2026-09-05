package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"budgetflow/internal/config"
	"budgetflow/internal/httpserver"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		log.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv := httpserver.New(cfg, log)
	if err := srv.ListenAndServe(ctx); err != nil {
		log.Error("server error", "error", err)
		os.Exit(1)
	}
}
