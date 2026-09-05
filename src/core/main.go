// Command core is deployable 1 of ADR-0017: it starts the process and turns its failure into an exit code.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/mateo-velasquez/sonia/src/core/internal/app"
)

// main owns the signal and the exit code, and logs through slog because a configuration failure happens before the logger exists.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx); err != nil {
		slog.Error("core stopped", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
