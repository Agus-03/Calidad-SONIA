// Command core is deployable 1 of ADR-0017: it starts the process and turns its failure into an exit code.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/mateo-velasquez/sonia/src/core/internal/app"
)

// main owns the signal and the exit code, and logs through slog because a configuration failure happens before the logger exists.
func main() {
	// One entrypoint with a flag, the same shape the brain has: seeding is something someone runs, never a side effect of serving (regla 3).
	seedAdmin := flag.Bool("seed-admin", false, "create the account and its first admin, then exit")
	connectInbox := flag.Bool("connect-inbox", false, "encrypt the Meta credentials of an existing inbox, then exit")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	run := app.Run
	switch {
	case *seedAdmin:
		run = app.SeedAdmin
	case *connectInbox:
		run = app.ConnectInbox
	}

	if err := run(ctx); err != nil {
		slog.Error("core stopped", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
