package config

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/encryption"
)

// Any 32 bytes other than ENCRYPTION_KEY's zeros: the two keys may not be the same (ADR-0064, regla 2).
var documentKey = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x01}, encryption.KeyBytes))

// validEnv puts a bootable environment in place, so each case only changes the variable it is about.
func validEnv(t *testing.T) {
	t.Helper()

	t.Setenv("DATABASE_URL", "postgres://sonia:sonia@localhost:5432/sonia")
	t.Setenv("BRAIN_BASE_URL", "http://brain:8000")
	t.Setenv("BRAIN_INTERNAL_TOKEN", "internal-token")
	t.Setenv("JWT_SECRET", strings.Repeat("k", minJWTSecretBytes))
	t.Setenv("ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(make([]byte, encryption.KeyBytes)))
	t.Setenv("DOCUMENT_ENCRYPTION_KEY", documentKey)
	t.Setenv("FIREBASE_STORAGE_BUCKET", "sonia-test.appspot.com")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/run/secrets/firebase-service-account.json")
	t.Setenv("STORAGE_EMULATOR_HOST", "")

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
		{
			name:  "no signing secret means every operator session could be forged (ADR-0030)",
			key:   "JWT_SECRET",
			value: "",
		},
		{
			name:  "a short secret is an HS256 signature that is brute-forced offline (ADR-0030)",
			key:   "JWT_SECRET",
			value: "corto",
		},
		{
			name:  "a session lifetime that is not a number would silently become zero",
			key:   "JWT_TTL_MS",
			value: "doce horas",
		},
		{
			name:  "no encryption key means the four sensitive columns would be stored in the clear (ADR-0031)",
			key:   "ENCRYPTION_KEY",
			value: "",
		},
		{
			name:  "a key that is not base64 would decode to bytes nobody chose",
			key:   "ENCRYPTION_KEY",
			value: "esto no es base64!!",
		},
		{
			name:  "a key shorter than 32 bytes is not AES-256 (ADR-0031)",
			key:   "ENCRYPTION_KEY",
			value: "c2hvcnQ=",
		},
		{
			name:  "no document key means the documents would leave in the clear (ADR-0064)",
			key:   "DOCUMENT_ENCRYPTION_KEY",
			value: "",
		},
		{
			name:  "a document key shorter than 32 bytes is not AES-256 (ADR-0064)",
			key:   "DOCUMENT_ENCRYPTION_KEY",
			value: "c2hvcnQ=",
		},
		{
			name:  "the column key as the document key hands the worker the DNI (ADR-0064, regla 2)",
			key:   "DOCUMENT_ENCRYPTION_KEY",
			value: base64.StdEncoding.EncodeToString(make([]byte, encryption.KeyBytes)),
		},
		{
			name:  "no bucket means an upload has nowhere to go (ADR-0069)",
			key:   "FIREBASE_STORAGE_BUCKET",
			value: "",
		},
		{
			name:  "no credential and no emulator means nothing can reach the bucket (ADR-0069)",
			key:   "GOOGLE_APPLICATION_CREDENTIALS",
			value: "",
		},
		{
			name:  "a storage timeout that is not a number would silently become zero",
			key:   "STORAGE_TIMEOUT_MS",
			value: "dos minutos",
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

// TestLoadFallsBackToDefaults protects the optional half: only the four secrets are required, the rest has a working default.
func TestLoadFallsBackToDefaults(t *testing.T) {
	validEnv(t)
	for _, key := range []string{
		"CORE_HTTP_ADDR", "ENVIRONMENT", "LOG_LEVEL",
		"DB_POOL_MIN_SIZE", "DB_POOL_MAX_SIZE",
		"BRAIN_TIMEOUT_MS", "BRAIN_PREVIEW_TIMEOUT_MS",
		"TURN_MAX_RETRIES", "TURN_RETRY_BACKOFF_MS", "JWT_TTL_MS",
		"META_GRAPH_BASE_URL", "META_API_VERSION", "WHATSAPP_TIMEOUT_MS", "WHATSAPP_USE_DOUBLE",
	} {
		t.Setenv(key, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() rejected an environment with only the four secrets: %v", err)
	}

	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr = %q, want \":8080\"", cfg.HTTPAddr)
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("LogLevel = %q, want \"info\"", cfg.LogLevel)
	}
	if cfg.BrainTimeout != 15*time.Second {
		t.Fatalf("BrainTimeout = %v, want 15s", cfg.BrainTimeout)
	}
	if cfg.PreviewTimeout != 3*time.Second {
		t.Fatalf("PreviewTimeout = %v, want 3s", cfg.PreviewTimeout)
	}
	if cfg.JWTTTL != 12*time.Hour {
		t.Fatalf("JWTTTL = %v, want 12h", cfg.JWTTTL)
	}
	if cfg.MetaGraphBaseURL != "https://graph.facebook.com" || cfg.MetaAPIVersion != "v21.0" {
		t.Fatalf("Meta defaults = %q %q, want the pinned Cloud API host and version", cfg.MetaGraphBaseURL, cfg.MetaAPIVersion)
	}
	if cfg.WhatsAppTimeout != 5*time.Second {
		t.Fatalf("WhatsAppTimeout = %v, want 5s", cfg.WhatsAppTimeout)
	}
	// Off by default, because the deployment that forgets to say anything is the one that must not answer with a fake send.
	if cfg.WhatsAppUseDouble {
		t.Fatal("WhatsAppUseDouble defaulted to true")
	}
}

// An allowlist and not a ban on production: with a ban, an ENVIRONMENT typed "prod" would quietly let a fake send through.
func TestLoadRefusesTheDoubleOutsideDevelopmentAndTest(t *testing.T) {
	cases := []struct {
		name        string
		environment string
		wantErr     bool
	}{
		{"development is where there is no approved number, which is every machine today", "development", false},
		{"test is the CI, which has no WABA either", "test", false},
		{"production must never answer a customer without reaching Meta", "production", true},
		{"staging talks to real numbers too", "staging", true},
		{"a typo is not a permission: prod is not on the list", "prod", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			validEnv(t)
			t.Setenv("ENVIRONMENT", tc.environment)
			t.Setenv("WHATSAPP_USE_DOUBLE", "true")

			_, err := Load()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Load() error = %v, want an error: %v", err, tc.wantErr)
			}
		})
	}
}

// A typo must not silently mean off: a deployment that meant to double and did not would try to reach Meta with no token.
func TestLoadRefusesAFlagThatIsNotTrueOrFalse(t *testing.T) {
	validEnv(t)
	t.Setenv("WHATSAPP_USE_DOUBLE", "si")

	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted WHATSAPP_USE_DOUBLE=si")
	}
}

// The two configurations of the timer that mean nothing, refused at boot rather than discovered in production (ADR-0041, regla 10).
func TestLoadRefusesATimerThatCannotWork(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
	}{
		{
			name: "a poller slower than the window makes the tick the real window, and the window a number nobody applies",
			env:  map[string]string{"TURN_BATCH_WINDOW_MS": "1000", "TURN_POLL_INTERVAL_MS": "5000"},
		},
		{
			name: "a maximum duration below the window closes every batch by max_duration before it ever waits",
			env:  map[string]string{"TURN_BATCH_WINDOW_MS": "10000", "TURN_BATCH_MAX_DURATION_MS": "5000"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			validEnv(t)
			for key, value := range tc.env {
				t.Setenv(key, value)
			}

			if _, err := Load(); err == nil {
				t.Fatalf("Load() accepted %v, want an error", tc.env)
			}
		})
	}
}

// The four defaults of regla 10, which are a starting point and not a measurement: they have to be the ones the ADR declares.
func TestTheTimerDefaultsAreTheOnesTheDecisionDeclares(t *testing.T) {
	validEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() = %v, want no error", err)
	}

	if cfg.TurnBatchWindow != 10*time.Second {
		t.Fatalf("TurnBatchWindow = %v, want 10s", cfg.TurnBatchWindow)
	}
	if cfg.TurnPollInterval != time.Second {
		t.Fatalf("TurnPollInterval = %v, want 1s: it is a tenth of the window, so the close lags by 10%% of it", cfg.TurnPollInterval)
	}
	if cfg.TurnBatchMaxMessages != 10 {
		t.Fatalf("TurnBatchMaxMessages = %d, want 10", cfg.TurnBatchMaxMessages)
	}
	if cfg.TurnBatchMaxDuration != time.Minute {
		t.Fatalf("TurnBatchMaxDuration = %v, want 1m", cfg.TurnBatchMaxDuration)
	}
}

// TestTheStorageEmulatorStandsInForTheCredentialOnlyInDevelopmentAndTest is ADR-0070: the smoke writes to a fake-gcs-server
// without a service account, and a production that points there would keep the documents nowhere real.
func TestTheStorageEmulatorStandsInForTheCredentialOnlyInDevelopmentAndTest(t *testing.T) {
	validEnv(t)
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("STORAGE_EMULATOR_HOST", "storage:4443")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() refused the emulator in test: %v", err)
	}
	if cfg.StorageEmulatorHost != "storage:4443" || cfg.StorageBucket != "sonia-test.appspot.com" {
		t.Fatalf("storage = %q on %q, want the emulator and the bucket", cfg.StorageEmulatorHost, cfg.StorageBucket)
	}
	if cfg.StorageTimeout != 2*time.Minute {
		t.Fatalf("StorageTimeout = %v, want the two minutes a 100 MB upload may take", cfg.StorageTimeout)
	}
	if len(cfg.DocumentEncryptionKey) != encryption.KeyBytes {
		t.Fatalf("DocumentEncryptionKey has %d bytes, want %d", len(cfg.DocumentEncryptionKey), encryption.KeyBytes)
	}

	t.Setenv("ENVIRONMENT", "production")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "STORAGE_EMULATOR_HOST") {
		t.Fatalf("Load() in production with the emulator: got %v, want a refusal that names it", err)
	}
}
