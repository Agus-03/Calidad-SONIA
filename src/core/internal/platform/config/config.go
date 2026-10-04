package config

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/encryption"
)

// An allowlist and not a ban on production: with a ban, an ENVIRONMENT typed "prod" would quietly let a fake send through.
var doubleAllowedEnvironments = []string{"development", "test"}

// HS256 is no stronger than its key, and a short one is brute-forced offline without anything at runtime noticing (ADR-0030).
const minJWTSecretBytes = 32

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

		JWTSecret: os.Getenv("JWT_SECRET"),
		// Twelve hours so the session outlasts a long shift and expires outside working time, never mid-conversation (ADR-0030).
		JWTTTL: 12 * time.Hour,

		SeedAdminEmail:    os.Getenv("SEED_ADMIN_EMAIL"),
		SeedAdminPassword: os.Getenv("SEED_ADMIN_PASSWORD"),
		// Only used when the database has no account yet, which is the first run of an installation and nothing else.
		SeedAdminAccountName: env("SEED_ADMIN_ACCOUNT_NAME", "SONIA"),

		ConnectInboxPhoneNumberID: os.Getenv("CONNECT_INBOX_PHONE_NUMBER_ID"),
		ConnectInboxAccessToken:   os.Getenv("CONNECT_INBOX_ACCESS_TOKEN"),
		ConnectInboxAppSecret:     os.Getenv("CONNECT_INBOX_APP_SECRET"),

		BrainBaseURL:       os.Getenv("BRAIN_BASE_URL"),
		BrainInternalToken: os.Getenv("BRAIN_INTERNAL_TOKEN"),
		BrainTimeout:       10 * time.Second,
		PreviewTimeout:     3 * time.Second,

		TurnMaxRetries:   3,
		TurnRetryBackoff: 250 * time.Millisecond,
		// Bounds how long one turn goroutine can live, and with it how long a deployment waits before cutting (ADR-0029).
		TurnBudget: 30 * time.Second,

		// A starting point and not a measurement: the right window is the one the customers' own rhythm shows (ADR-0041, regla 10).
		TurnBatchWindow:      10 * time.Second,
		TurnPollInterval:     time.Second,
		TurnBatchMaxMessages: 10,
		TurnBatchMaxDuration: 60 * time.Second,
		TurnPollLimit:        10,

		MetaGraphBaseURL: env("META_GRAPH_BASE_URL", "https://graph.facebook.com"),
		// Pinned and never floating: Meta versions the path, and an unpinned one changes the contract without a deploy.
		MetaAPIVersion: env("META_API_VERSION", "v21.0"),
		// Shorter than the brain's: Meta answers in hundreds of milliseconds, and the turn already spent its budget upstream.
		WhatsAppTimeout: 5 * time.Second,

		StorageBucket: os.Getenv("FIREBASE_STORAGE_BUCKET"),
		// The two names Google's own clients read, so the worker's Python client finds the same values without a translation.
		StorageCredentialsFile: os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"),
		StorageEmulatorHost:    strings.TrimSpace(os.Getenv("STORAGE_EMULATOR_HOST")),
		// Two minutes because a whole upload of up to 100 MB rides on it, and an administrator is waiting for it and not a customer.
		StorageTimeout: 2 * time.Minute,
	}

	for _, required := range []struct {
		key   string
		value string
	}{
		{"DATABASE_URL", cfg.DatabaseURL},
		{"BRAIN_BASE_URL", cfg.BrainBaseURL},
		{"BRAIN_INTERNAL_TOKEN", cfg.BrainInternalToken},
		{"JWT_SECRET", cfg.JWTSecret},
	} {
		if strings.TrimSpace(required.value) == "" {
			problems = append(problems, required.key+" is required")
		}
	}

	// Guarded on the empty case so a missing secret is reported once, as missing, and not twice.
	if n := len(strings.TrimSpace(cfg.JWTSecret)); n > 0 && n < minJWTSecretBytes {
		problems = append(problems, fmt.Sprintf("JWT_SECRET is %d bytes and needs at least %d", n, minJWTSecretBytes))
	}

	// Validated at boot even though nothing has encrypted anything yet: a missing key must stop the process, not surface on the first contact with a DNI (ADR-0031, regla 7).
	if raw := strings.TrimSpace(os.Getenv("ENCRYPTION_KEY")); raw == "" {
		problems = append(problems, "ENCRYPTION_KEY is required")
	} else if key, err := encryption.DecodeKey(raw); err != nil {
		problems = append(problems, "ENCRYPTION_KEY "+err.Error())
	} else {
		cfg.EncryptionKey = key
	}

	// Validated at boot for the same reason, and kept apart from ENCRYPTION_KEY: two equal keys would give the worker the DNI (ADR-0064, regla 2).
	if raw := strings.TrimSpace(os.Getenv("DOCUMENT_ENCRYPTION_KEY")); raw == "" {
		problems = append(problems, "DOCUMENT_ENCRYPTION_KEY is required")
	} else if key, err := encryption.DecodeKey(raw); err != nil {
		problems = append(problems, "DOCUMENT_ENCRYPTION_KEY "+err.Error())
	} else if cfg.EncryptionKey != nil && string(key) == string(cfg.EncryptionKey) {
		problems = append(problems, "DOCUMENT_ENCRYPTION_KEY is the same as ENCRYPTION_KEY, and the worker that holds it must not read the sensitive columns")
	} else {
		cfg.DocumentEncryptionKey = key
	}

	if strings.TrimSpace(cfg.StorageBucket) == "" {
		problems = append(problems, "FIREBASE_STORAGE_BUCKET is required")
	}
	// The emulator takes no credential and the real service needs one, so exactly one of the two has to be there.
	switch {
	case cfg.StorageEmulatorHost != "" && !slices.Contains(doubleAllowedEnvironments, cfg.Environment):
		problems = append(problems, fmt.Sprintf(
			"STORAGE_EMULATOR_HOST writes the documents to a fake store and ENVIRONMENT=%q is not one of %s",
			cfg.Environment, strings.Join(doubleAllowedEnvironments, ", ")))
	case cfg.StorageEmulatorHost == "" && strings.TrimSpace(cfg.StorageCredentialsFile) == "":
		problems = append(problems, "GOOGLE_APPLICATION_CREDENTIALS is required unless STORAGE_EMULATOR_HOST is set")
	}

	var err error

	if cfg.StorageTimeout, err = durationMS("STORAGE_TIMEOUT_MS", cfg.StorageTimeout); err != nil {
		problems = append(problems, err.Error())
	}

	if cfg.JWTTTL, err = durationMS("JWT_TTL_MS", cfg.JWTTTL); err != nil {
		problems = append(problems, err.Error())
	}

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
	if cfg.TurnBatchWindow, err = durationMS("TURN_BATCH_WINDOW_MS", cfg.TurnBatchWindow); err != nil {
		problems = append(problems, err.Error())
	}
	if cfg.TurnPollInterval, err = durationMS("TURN_POLL_INTERVAL_MS", cfg.TurnPollInterval); err != nil {
		problems = append(problems, err.Error())
	}
	if cfg.TurnBatchMaxMessages, err = positiveInt("TURN_BATCH_MAX_MESSAGES", cfg.TurnBatchMaxMessages); err != nil {
		problems = append(problems, err.Error())
	}
	if cfg.TurnBatchMaxDuration, err = durationMS("TURN_BATCH_MAX_DURATION_MS", cfg.TurnBatchMaxDuration); err != nil {
		problems = append(problems, err.Error())
	}
	if cfg.TurnPollLimit, err = positiveInt("TURN_POLL_LIMIT", cfg.TurnPollLimit); err != nil {
		problems = append(problems, err.Error())
	}
	// The two configurations that mean nothing, refused at boot (ADR-0041, regla 10): a poller slower than the window turns the
	// tick into the real window, and a maximum duration below it closes every batch by max_duration before it ever waits.
	if cfg.TurnPollInterval > cfg.TurnBatchWindow {
		problems = append(problems, fmt.Sprintf("TURN_POLL_INTERVAL_MS=%d is longer than TURN_BATCH_WINDOW_MS=%d, which makes the tick the real window",
			cfg.TurnPollInterval.Milliseconds(), cfg.TurnBatchWindow.Milliseconds()))
	}
	if cfg.TurnBatchMaxDuration < cfg.TurnBatchWindow {
		problems = append(problems, fmt.Sprintf("TURN_BATCH_MAX_DURATION_MS=%d is shorter than TURN_BATCH_WINDOW_MS=%d, so every batch would close by max_duration",
			cfg.TurnBatchMaxDuration.Milliseconds(), cfg.TurnBatchWindow.Milliseconds()))
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
