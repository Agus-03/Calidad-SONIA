package logging

import (
	"log/slog"
	"testing"
)

// TestParseLevelFallsBackToInfo protects a logger built outside Load: an unknown level must not silence the process.
func TestParseLevelFallsBackToInfo(t *testing.T) {
	cases := []struct {
		name  string
		level string
		want  slog.Level
	}{
		{name: "debug is the noisiest level", level: "debug", want: slog.LevelDebug},
		{name: "info is the default of the four", level: "info", want: slog.LevelInfo},
		{name: "warn is what a 4xx logs", level: "warn", want: slog.LevelWarn},
		{name: "error is what a 5xx logs", level: "error", want: slog.LevelError},
		{name: "an unknown level logs at info instead of nothing", level: "verbose", want: slog.LevelInfo},
		{name: "an empty level logs at info instead of nothing", level: "", want: slog.LevelInfo},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseLevel(tc.level); got != tc.want {
				t.Fatalf("parseLevel(%q) = %v, want %v", tc.level, got, tc.want)
			}
		})
	}
}
