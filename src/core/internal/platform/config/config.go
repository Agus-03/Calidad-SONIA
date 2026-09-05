package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

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
