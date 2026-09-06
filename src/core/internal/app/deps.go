package app

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/brainclient"
	"github.com/mateo-velasquez/sonia/src/core/internal/conversations"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/config"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
	"github.com/mateo-velasquez/sonia/src/core/internal/turn"
	"github.com/mateo-velasquez/sonia/src/core/internal/whatsapp"
)

// wiring is everything the router needs from the bounded contexts.
type wiring struct {
	controllers []soniahttp.Controller
	actorLookup soniahttp.ActorLookup

	// What Meta calls: outside /api/v1 because an external callback is not a resource of the operator API.
	publicControllers []soniahttp.Controller

	// Held so the shutdown can wait for the turns in flight: with the 200 already sent, nobody else would (ADR-0029).
	turns *turn.Orchestrator
}

// wire builds the object graph, repository then service then controller: it is the only part that grows with each context.
func wire(cfg config.Config, pool db.Pool, log *slog.Logger) wiring {
	aggregate := conversations.NewService(conversations.NewRepository(pool), log)
	channelRepo := whatsapp.NewRepository(pool)

	turnRepo := turn.NewRepository(pool)

	turns := turn.New(turn.Options{
		// Binds both collaborators to the same transaction, which is what lets it cross packages (regla 5).
		Tx: func(ctx context.Context, fn func(turn.Store, turn.Aggregate) error) error {
			return db.InTx(ctx, pool, func(exec db.Executor) error {
				return fn(turnRepo.WithExecutor(exec), aggregate.WithExecutor(exec))
			})
		},
		Store:     turnRepo,
		Aggregate: aggregate,
		Brain: brainclient.New(brainclient.Options{
			BaseURL: cfg.BrainBaseURL,
			Token:   cfg.BrainInternalToken,
			Timeout: cfg.BrainTimeout,
			Logger:  log,
		}),
		Channel:      channelRepo,
		Sender:       sender(cfg, log),
		Budget:       cfg.TurnBudget,
		MaxRetries:   cfg.TurnMaxRetries,
		RetryBackoff: cfg.TurnRetryBackoff,
		Logger:       log,
	})

	channel := whatsapp.NewController(whatsapp.NewService(channelRepo, aggregate, log), turns, log)

	return wiring{
		controllers:       nil,
		actorLookup:       denyEveryone,
		publicControllers: []soniahttp.Controller{channel},
		turns:             turns,
	}
}

// The double is chosen here and refused elsewhere: config already rejected WHATSAPP_USE_DOUBLE outside development and test.
func sender(cfg config.Config, log *slog.Logger) whatsapp.Sender {
	if cfg.WhatsAppUseDouble {
		log.Warn("whatsapp sender doubled", slog.String("environment", cfg.Environment))
		return whatsapp.NewDoubleSender(log)
	}
	return whatsapp.NewClient(whatsapp.ClientOptions{
		GraphBaseURL: cfg.MetaGraphBaseURL,
		APIVersion:   cfg.MetaAPIVersion,
		Timeout:      cfg.WhatsAppTimeout,
		Logger:       log,
	})
}

// denyEveryone stands in until operators exists: Fase 0 mounts no endpoint under /api/v1, so no request should get past authentication.
func denyEveryone(context.Context, uuid.UUID) (soniahttp.Actor, error) {
	return soniahttp.Actor{}, soniahttp.Unauthorized("authentication_unavailable", "operator authentication is not wired yet")
}
