package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

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

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("invalid database configuration", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	if err := pool.Ping(pingCtx); err != nil {
		cancel()
		log.Error("database unreachable", "error", err)
		os.Exit(1)
	}
	cancel()

	srv := httpserver.New(cfg, log, pool)
	if err := srv.ListenAndServe(ctx); err != nil {
		log.Error("server error", "error", err)
		os.Exit(1)
	}
}
