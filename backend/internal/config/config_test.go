package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want 8080", cfg.Port)
	}
	if cfg.Env != "dev" {
		t.Errorf("Env = %q, want dev", cfg.Env)
	}
	if cfg.SessionSecret == "" {
		t.Error("SessionSecret should get a dev default")
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("PORT", "9999")
	t.Setenv("APP_ENV", "test")
	t.Setenv("DATABASE_URL", "postgres://x/y")
	t.Setenv("SESSION_SECRET", "secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Port != "9999" || cfg.Env != "test" || cfg.DatabaseURL != "postgres://x/y" || cfg.SessionSecret != "secret" {
		t.Errorf("Load() = %+v, env overrides not applied", cfg)
	}
}

func TestLoadRejectsInvalidEnv(t *testing.T) {
	t.Setenv("APP_ENV", "staging")
	if _, err := Load(); err == nil {
		t.Fatal("Load() should reject invalid APP_ENV")
	}
}

func TestLoadRequiresSecretInProd(t *testing.T) {
	t.Setenv("APP_ENV", "prod")
	t.Setenv("SESSION_SECRET", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load() should require SESSION_SECRET in prod")
	}
}
