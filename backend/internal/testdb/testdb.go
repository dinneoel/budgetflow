// Package testdb provisions throwaway PostgreSQL databases for tests.
//
// Each call to New creates a uniquely named database on the server pointed at
// by TEST_DATABASE_URL (default: the docker-compose dev server), runs all
// migrations up, and registers cleanup that drops the database when the test
// finishes. Tests therefore never share state and can run in parallel.
package testdb

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	pgx5 "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver for the migrator

	"budgetflow/migrations"
)

const defaultURL = "postgres://budgetflow:budgetflow@localhost:5432/budgetflow_test?sslmode=disable"

// BaseURL returns the PostgreSQL URL tests connect to, from TEST_DATABASE_URL
// or the docker-compose default.
func BaseURL() string {
	if u := os.Getenv("TEST_DATABASE_URL"); u != "" {
		return u
	}
	return defaultURL
}

// New creates a fresh database with all migrations applied and returns a pool
// connected to it. The database is dropped when the test completes.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := createDatabase(t)

	m, err := NewMigrator(dbURL)
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	if err := m.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	if serr, derr := m.Close(); serr != nil || derr != nil {
		t.Fatalf("close migrator: %v / %v", serr, derr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// NewEmpty creates a fresh database with no migrations applied and returns its
// URL. Useful for exercising the migrations themselves.
func NewEmpty(t *testing.T) string {
	t.Helper()
	return createDatabase(t)
}

// NewMigrator builds a golang-migrate instance over the embedded migration
// files for the given database URL. The caller must Close it.
func NewMigrator(dbURL string) (*migrate.Migrate, error) {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("open embedded migrations: %w", err)
	}
	sqlDB, err := sql.Open("pgx", dbURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	driver, err := pgx5.WithInstance(sqlDB, &pgx5.Config{})
	if err != nil {
		return nil, fmt.Errorf("init migrate driver: %w", err)
	}
	return migrate.NewWithInstance("iofs", src, "pgx5", driver)
}

func createDatabase(t *testing.T) string {
	t.Helper()
	base := BaseURL()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	admin, err := pgxpool.New(ctx, base)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	if err := admin.Ping(ctx); err != nil {
		admin.Close()
		t.Fatalf("test database server unreachable at %s (start it with `make db-up` or docker): %v", base, err)
	}

	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		admin.Close()
		t.Fatalf("generate database name: %v", err)
	}
	name := "budgetflow_test_" + hex.EncodeToString(suffix)

	if _, err := admin.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", name)); err != nil {
		admin.Close()
		t.Fatalf("create test database %s: %v", name, err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, fmt.Sprintf("DROP DATABASE %s WITH (FORCE)", name)); err != nil {
			t.Errorf("drop test database %s: %v", name, err)
		}
		admin.Close()
	})

	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	u.Path = "/" + name
	return u.String()
}
