package config

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// An allowlist and not a ban on production: with a ban, an ENVIRONMENT typed "prod" would quietly let a fake send through.
var doubleAllowedEnvironments = []string{"development", "test"}

// Load reads the environment and reports every problem at once instead of failing on the first.
func Load() (Config, error) {
	var problems []string

	cfg := Config{
		HTTPAddr:    env("CORE_HTTP_ADDR", ":8080"),
		Environment: env("ENVIRONMENT", "development"),
		LogLevel:    env("LOG_LEVEL", "info"),

		DatabaseURL:   os.Getenv("DATABASE_URL"),
		DBPoolMinSize: 2,
		DBPoolMaxSize: 10,

		BrainBaseURL:       os.Getenv("BRAIN_BASE_URL"),
		BrainInternalToken: os.Getenv("BRAIN_INTERNAL_TOKEN"),
		BrainTimeout:       10 * time.Second,
		PreviewTimeout:     3 * time.Second,

		TurnMaxRetries:   3,
		TurnRetryBackoff: 250 * time.Millisecond,
		// Bounds how long one turn goroutine can live, and with it how long a deployment waits before cutting (ADR-0029).
		TurnBudget: 30 * time.Second,

		MetaGraphBaseURL: env("META_GRAPH_BASE_URL", "https://graph.facebook.com"),
		// Pinned and never floating: Meta versions the path, and an unpinned one changes the contract without a deploy.
		MetaAPIVersion: env("META_API_VERSION", "v21.0"),
		// Shorter than the brain's: Meta answers in hundreds of milliseconds, and the turn already spent its budget upstream.
		WhatsAppTimeout: 5 * time.Second,
	}

	for _, required := range []struct {
		key   string
		value string
	}{
		{"DATABASE_URL", cfg.DatabaseURL},
		{"BRAIN_BASE_URL", cfg.BrainBaseURL},
		{"BRAIN_INTERNAL_TOKEN", cfg.BrainInternalToken},
	} {
		if strings.TrimSpace(required.value) == "" {
			problems = append(problems, required.key+" is required")
		}
	}

	var err error

	if cfg.DBPoolMinSize, err = positiveInt("DB_POOL_MIN_SIZE", cfg.DBPoolMinSize); err != nil {
		problems = append(problems, err.Error())
	}
	if cfg.DBPoolMaxSize, err = positiveInt("DB_POOL_MAX_SIZE", cfg.DBPoolMaxSize); err != nil {
		problems = append(problems, err.Error())
	}
	if cfg.DBPoolMaxSize < cfg.DBPoolMinSize {
		problems = append(problems, fmt.Sprintf(
			"DB_POOL_MAX_SIZE=%d is smaller than DB_POOL_MIN_SIZE=%d", cfg.DBPoolMaxSize, cfg.DBPoolMinSize))
	}

	if cfg.BrainTimeout, err = durationMS("BRAIN_TIMEOUT_MS", cfg.BrainTimeout); err != nil {
		problems = append(problems, err.Error())
	}
	if cfg.PreviewTimeout, err = durationMS("BRAIN_PREVIEW_TIMEOUT_MS", cfg.PreviewTimeout); err != nil {
		problems = append(problems, err.Error())
	}

	if cfg.TurnMaxRetries, err = nonNegativeInt("TURN_MAX_RETRIES", cfg.TurnMaxRetries); err != nil {
		problems = append(problems, err.Error())
	}
	if cfg.TurnRetryBackoff, err = durationMS("TURN_RETRY_BACKOFF_MS", cfg.TurnRetryBackoff); err != nil {
		problems = append(problems, err.Error())
	}
	if cfg.TurnBudget, err = durationMS("TURN_BUDGET_MS", cfg.TurnBudget); err != nil {
		problems = append(problems, err.Error())
	}

	if cfg.WhatsAppTimeout, err = durationMS("WHATSAPP_TIMEOUT_MS", cfg.WhatsAppTimeout); err != nil {
		problems = append(problems, err.Error())
	}
	if cfg.WhatsAppUseDouble, err = boolean("WHATSAPP_USE_DOUBLE", false); err != nil {
		problems = append(problems, err.Error())
	}
	if cfg.WhatsAppUseDouble && !slices.Contains(doubleAllowedEnvironments, cfg.Environment) {
		problems = append(problems, fmt.Sprintf(
			"WHATSAPP_USE_DOUBLE answers without reaching Meta and ENVIRONMENT=%q is not one of %s",
			cfg.Environment, strings.Join(doubleAllowedEnvironments, ", ")))
	}

	switch cfg.LogLevel {
	case "debug", "info", "warning", "error":
	default:
		problems = append(problems, fmt.Sprintf("LOG_LEVEL=%q is not one of debug, info, warning, error", cfg.LogLevel))
	}

	if len(problems) > 0 {
		return Config{}, errors.New("invalid configuration: " + strings.Join(problems, "; "))
	}
	return cfg, nil
}

// env returns the variable trimmed, or the fallback when it is absent.
func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// durationMS reads a positive number of milliseconds as a duration.
func durationMS(key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		return fallback, fmt.Errorf("%s=%q is not a positive number of milliseconds", key, raw)
	}
	return time.Duration(ms) * time.Millisecond, nil
}

// boolean reads a flag, refusing anything that is not clearly true or false: a typo must not silently mean off.
func boolean(key string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback, fmt.Errorf("%s=%q is not true or false", key, raw)
	}
	return v, nil
}

// positiveInt reads an integer greater than zero.
func positiveInt(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback, fmt.Errorf("%s=%q is not a positive integer", key, raw)
	}
	return n, nil
}

// nonNegativeInt reads an integer greater than or equal to zero, so retries can be disabled.
func nonNegativeInt(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return fallback, fmt.Errorf("%s=%q is not a non-negative integer", key, raw)
	}
	return n, nil
}
