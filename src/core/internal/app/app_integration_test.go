package app

import (
	"context"
	"encoding/base64"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/encryption"
	"net/http"
	"os"
	"testing"
	"time"
)

const testAddr = "127.0.0.1:18080"

// Run is what main calls, and nothing below this test proves that the whole graph boots and then stops cleanly.
func TestRunServesHealthAndStopsWhenTheContextIsCancelled(t *testing.T) {
	dsn := os.Getenv("SONIA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SONIA_TEST_DATABASE_URL is not set: bring the stack up and run make test-integration")
	}

	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("BRAIN_BASE_URL", "http://127.0.0.1:8000")
	t.Setenv("BRAIN_INTERNAL_TOKEN", "test-token")
	// Required since ADR-0030, and the process refuses to boot without it: long enough to clear the 32-byte floor.
	t.Setenv("JWT_SECRET", "test-secret-that-is-long-enough-for-hs256")
	// The four sensitive columns need a key before the process will boot at all, and any 32 bytes are a key here (ADR-0031, regla 7).
	t.Setenv("ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(make([]byte, encryption.KeyBytes)))
	t.Setenv("CORE_HTTP_ADDR", testAddr)

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() { stopped <- Run(ctx) }()

	if status := waitForHealth(t); status != http.StatusOK {
		cancel()
		t.Fatalf("/health: got %d, want 200", status)
	}

	cancel()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Run: got %v, want a clean shutdown", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Run: the process did not stop within the shutdown timeout")
	}
}

// Polls because the server starts in its own goroutine and a fixed sleep is either flaky or slow.
func waitForHealth(t *testing.T) int {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		res, err := http.Get("http://" + testAddr + "/health")
		if err == nil {
			defer func() { _ = res.Body.Close() }()
			return res.StatusCode
		}
		time.Sleep(100 * time.Millisecond)
	}
	return 0
}
