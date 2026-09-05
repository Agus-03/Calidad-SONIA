// Package logging builds the process logger: JSON to stdout, so the container runtime collects it.
package logging

import (
	"log/slog"
	"os"
)

// New returns a JSON logger at the given level, tagged with the service and the environment.
func New(level string, environment string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: parseLevel(level)})
	return slog.New(handler).With(
		slog.String("service", "core"),
		slog.String("environment", environment),
	)
}

// parseLevel maps the configured level, falling back to info for a logger built outside Load.
func parseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
