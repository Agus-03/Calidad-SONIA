// Package app boots the core outside package main, because package main cannot be imported by a test that starts the whole process.
package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/config"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/logging"
	"github.com/mateo-velasquez/sonia/src/core/migrations"
)

// Run serves until ctx is cancelled: who cancels it is the caller's business, not this one's.
func Run(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := logging.New(cfg.LogLevel, cfg.Environment)
	slog.SetDefault(log)

	pool, err := db.Open(ctx, db.Options{
		DatabaseURL: cfg.DatabaseURL,
		MinSize:     cfg.DBPoolMinSize,
		MaxSize:     cfg.DBPoolMaxSize,
	})
	if err != nil {
		return err
	}
	defer pool.Close()

	applied, err := db.Migrate(ctx, pool, migrations.FS)
	if err != nil {
		return err
	}
	if len(applied) > 0 {
		log.Info("migrations applied", slog.Int("count", len(applied)), slog.String("last", applied[len(applied)-1]))
	}

	deps, err := wire(cfg, pool, log)
	if err != nil {
		return err
	}

	router := soniahttp.NewRouter(soniahttp.RouterOptions{
		Logger:        log,
		TokenVerifier: deps.tokenVerifier,
		ActorLookup:   deps.actorLookup,
		Controllers:   deps.controllers,

		PublicControllers: deps.publicControllers,
		Ready: func() error {
			pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			return pool.Ping(pingCtx)
		},
	})

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Info("core listening", slog.String("addr", cfg.HTTPAddr))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// Its own context and not ctx: the shutdown below has to stop the ticker before it waits for the turns, and cancelling ctx
	// there would take the server with it (ADR-0041, regla 11).
	pollerCtx, stopPoller := context.WithCancel(context.Background())
	defer stopPoller()
	deps.poller.Start(pollerCtx)

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// The server first, so no message joins a batch from here on (ADR-0029).
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}

	// Then the poller, because a tick that ran while we waited would add turns to what this shutdown has to wait for (regla 11).
	stopPoller()
	if err := deps.poller.Wait(shutdownCtx); err != nil {
		log.Warn("shutdown cut the turn poller", slog.String("cause", err.Error()))
	}

	// A turn cut in half leaves the customer without an answer, and with the 200 already sent Meta will not send it again.
	if err := deps.turns.Wait(shutdownCtx); err != nil {
		log.Warn("shutdown cut turns still running", slog.String("cause", err.Error()))
	}
	return nil
}
