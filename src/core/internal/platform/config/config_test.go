package config

import (
	"strings"
	"testing"
	"time"
)

// validEnv puts a bootable environment in place, so each case only changes the variable it is about.
func validEnv(t *testing.T) {
	t.Helper()

	t.Setenv("DATABASE_URL", "postgres://sonia:sonia@localhost:5432/sonia")
	t.Setenv("BRAIN_BASE_URL", "http://brain:8000")
	t.Setenv("BRAIN_INTERNAL_TOKEN", "internal-token")

	t.Setenv("CORE_HTTP_ADDR", ":8080")
	t.Setenv("ENVIRONMENT", "test")
	t.Setenv("LOG_LEVEL", "info")

	t.Setenv("DB_POOL_MIN_SIZE", "2")
	t.Setenv("DB_POOL_MAX_SIZE", "10")
	t.Setenv("BRAIN_TIMEOUT_MS", "10000")
	t.Setenv("BRAIN_PREVIEW_TIMEOUT_MS", "3000")
	t.Setenv("TURN_MAX_RETRIES", "3")
	t.Setenv("TURN_RETRY_BACKOFF_MS", "250")
}

// TestLoadReportsEveryProblemAtOnce protects the accumulation: booting three times to discover three missing variables is worse than reading a list.
func TestLoadReportsEveryProblemAtOnce(t *testing.T) {
	validEnv(t)
	t.Setenv("DATABASE_URL", "")
	t.Setenv("BRAIN_INTERNAL_TOKEN", "")
	t.Setenv("LOG_LEVEL", "verbose")

	_, err := Load()
	if err == nil {
		t.Fatalf("Load() accepted an environment with three problems, want an error")
	}

	for _, want := range []string{"DATABASE_URL", "BRAIN_INTERNAL_TOKEN", "LOG_LEVEL"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %s, want every problem reported at once", err, want)
		}
	}
}

// TestLoadRejectsAnEnvironmentThatCannotServe protects regla 10: if a secret is missing the process does not start.
func TestLoadRejectsAnEnvironmentThatCannotServe(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		value string
	}{
		{
			name:  "no database means nothing can be persisted",
			key:   "DATABASE_URL",
			value: "",
		},
		{
			name:  "no internal token means the brain would reject every call (ADR-0017 (k))",
			key:   "BRAIN_INTERNAL_TOKEN",
			value: "",
		},
		{
			name:  "a log level outside the four is a typo, not a level",
			key:   "LOG_LEVEL",
			value: "verbose",
		},
		{
			name:  "a pool of zero connections hangs on the first query",
			key:   "DB_POOL_MIN_SIZE",
			value: "0",
		},
		{
			name:  "a maximum below the minimum is a pool that cannot be built",
			key:   "DB_POOL_MAX_SIZE",
			value: "1",
		},
		{
			name:  "a timeout that is not a number would silently become zero",
			key:   "BRAIN_TIMEOUT_MS",
			value: "diez segundos",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			validEnv(t)
			t.Setenv(tc.key, tc.value)

			if _, err := Load(); err == nil {
				t.Fatalf("Load() accepted %s=%q, want an error", tc.key, tc.value)
			}
		})
	}
}

// TestLoadAcceptsRetriesDisabled protects the split between positiveInt and nonNegativeInt: zero retries is a choice, zero connections is a mistake.
func TestLoadAcceptsRetriesDisabled(t *testing.T) {
	validEnv(t)
	t.Setenv("TURN_MAX_RETRIES", "0")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() rejected TURN_MAX_RETRIES=0: %v", err)
	}
	if cfg.TurnMaxRetries != 0 {
		t.Fatalf("TurnMaxRetries = %d, want 0", cfg.TurnMaxRetries)
	}
}

// TestLoadFallsBackToDefaults protects the optional half: only the three secrets are required, the rest has a working default.
func TestLoadFallsBackToDefaults(t *testing.T) {
	validEnv(t)
	for _, key := range []string{
		"CORE_HTTP_ADDR", "ENVIRONMENT", "LOG_LEVEL",
		"DB_POOL_MIN_SIZE", "DB_POOL_MAX_SIZE",
		"BRAIN_TIMEOUT_MS", "BRAIN_PREVIEW_TIMEOUT_MS",
		"TURN_MAX_RETRIES", "TURN_RETRY_BACKOFF_MS",
	} {
		t.Setenv(key, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() rejected an environment with only the three secrets: %v", err)
	}

	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr = %q, want \":8080\"", cfg.HTTPAddr)
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("LogLevel = %q, want \"info\"", cfg.LogLevel)
	}
	if cfg.BrainTimeout != 10*time.Second {
		t.Fatalf("BrainTimeout = %v, want 10s", cfg.BrainTimeout)
	}
	if cfg.PreviewTimeout != 3*time.Second {
		t.Fatalf("PreviewTimeout = %v, want 3s", cfg.PreviewTimeout)
	}
}
