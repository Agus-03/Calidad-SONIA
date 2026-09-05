package db

import (
	"context"
	"testing"
)

// TestOpenRejectsAnUnusableDSN protects the startup contract: config checks the variable exists, Open checks it works.
func TestOpenRejectsAnUnusableDSN(t *testing.T) {
	cases := []struct {
		name string
		opts Options
	}{
		{
			name: "a malformed DSN fails before any network call",
			opts: Options{DatabaseURL: "postgres://sonia@localhost:not-a-port/sonia"},
		},
		{
			name: "an unreachable PostgreSQL fails at startup and not on the first customer message",
			opts: Options{DatabaseURL: "postgres://sonia:sonia@127.0.0.1:1/sonia", MinSize: 1, MaxSize: 2},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool, err := Open(context.Background(), tc.opts)
			if err == nil {
				pool.Close()
				t.Fatalf("Open(%q) returned no error, want one", tc.opts.DatabaseURL)
			}
		})
	}
}
