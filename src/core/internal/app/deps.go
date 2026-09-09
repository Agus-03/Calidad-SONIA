package app

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/brainclient"
	"github.com/mateo-velasquez/sonia/src/core/internal/conversations"
	"github.com/mateo-velasquez/sonia/src/core/internal/operators"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/auth"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/config"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
	"github.com/mateo-velasquez/sonia/src/core/internal/turn"
	"github.com/mateo-velasquez/sonia/src/core/internal/whatsapp"
)

// wiring is everything the router needs from the bounded contexts.
type wiring struct {
	controllers   []soniahttp.Controller
	tokenVerifier soniahttp.TokenVerifier
	actorLookup   soniahttp.ActorLookup

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

	tokens := auth.NewTokens(cfg.JWTSecret, cfg.JWTTTL)
	people := operators.NewService(operators.NewRepository(pool), tokens, log)

	return wiring{
		controllers:       []soniahttp.Controller{operators.NewController(people, log)},
		tokenVerifier:     verifyToken(tokens),
		actorLookup:       resolveActor(people),
		publicControllers: []soniahttp.Controller{channel, operators.NewLoginController(people, log)},
		turns:             turns,
	}
}

// The two errors are told apart here and not in platform/http, so the router never learns that the session token is a JWT (ADR-0030).
func verifyToken(tokens *auth.Tokens) soniahttp.TokenVerifier {
	return func(raw string) (uuid.UUID, error) {
		operatorID, err := tokens.Verify(raw)
		// A separate code because an expired session sends the operator to log in again, and a broken token does not.
		if errors.Is(err, auth.ErrExpiredToken) {
			return uuid.Nil, soniahttp.Unauthorized("session_expired", "the session expired and a new login is needed").WithCause(err)
		}
		if err != nil {
			return uuid.Nil, soniahttp.Unauthorized("invalid_credentials", "the session token is not valid").WithCause(err)
		}
		return operatorID, nil
	}
}

// One answer for a deleted operator and a disabled one, because both mean the same thing to whoever is holding the token (ADR-0030, regla 2).
func resolveActor(people *operators.Service) soniahttp.ActorLookup {
	return func(ctx context.Context, operatorID uuid.UUID) (soniahttp.Actor, error) {
		operator, err := people.Resolve(ctx, operatorID)
		if err != nil {
			return soniahttp.Actor{}, soniahttp.Unauthorized(
				"session_no_longer_valid", "this session no longer belongs to an active operator").WithCause(err)
		}
		return soniahttp.Actor{OperatorID: operator.ID, Role: string(operator.Role)}, nil
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
