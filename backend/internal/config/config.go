// Package config loads server configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"time"
)

// Config holds all runtime configuration for the BudgetFlow server.
type Config struct {
	// Port the HTTP server listens on.
	Port string
	// DatabaseURL is the PostgreSQL connection string.
	DatabaseURL string
	// SessionSecret signs session cookies. Must be non-empty outside dev.
	SessionSecret string
	// Env is the runtime environment: "dev", "test", or "prod".
	Env string
	// ShutdownTimeout bounds graceful shutdown.
	ShutdownTimeout time.Duration
}

// Load reads configuration from the environment, applying dev-friendly
// defaults. It returns an error for invalid or missing required values in
// non-dev environments.
func Load() (Config, error) {
	cfg := Config{
		Port:            getenv("PORT", "8080"),
		DatabaseURL:     getenv("DATABASE_URL", "postgres://budgetflow:budgetflow@localhost:5432/budgetflow_dev?sslmode=disable"),
		SessionSecret:   getenv("SESSION_SECRET", ""),
		Env:             getenv("APP_ENV", "dev"),
		ShutdownTimeout: 10 * time.Second,
	}

	switch cfg.Env {
	case "dev", "test", "prod":
	default:
		return Config{}, fmt.Errorf("invalid APP_ENV %q: must be dev, test, or prod", cfg.Env)
	}

	if cfg.SessionSecret == "" {
		if cfg.Env == "prod" {
			return Config{}, fmt.Errorf("SESSION_SECRET is required when APP_ENV=prod")
		}
		cfg.SessionSecret = "dev-insecure-session-secret"
	}

	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
