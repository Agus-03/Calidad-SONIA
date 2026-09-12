package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/brainclient"
	"github.com/mateo-velasquez/sonia/src/core/internal/contacts"
	"github.com/mateo-velasquez/sonia/src/core/internal/conversations"
	"github.com/mateo-velasquez/sonia/src/core/internal/labels"
	"github.com/mateo-velasquez/sonia/src/core/internal/operators"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/auth"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/config"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/encryption"
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
//
// It returns an error because contacts is the first context that needs more than the pool: the cipher of the four sensitive
// columns. config already refused to boot without a valid key, so this can only fail on something impossible — and a panic and
// an error both stop the boot, but only one prints a line whoever is deploying can act on (ADR-0031, regla 7).
func wire(cfg config.Config, pool db.Pool, log *slog.Logger) (wiring, error) {
	cipher, err := encryption.New(cfg.EncryptionKey)
	if err != nil {
		return wiring{}, fmt.Errorf("app: the encryption key config accepted is not one the cipher can use: %w", err)
	}

	aggregate := conversations.NewService(conversations.NewRepository(pool), log)
	channelRepo := whatsapp.NewRepository(pool, cipher)

	// A repository of its own and not the one `book` holds below: a repository keeps no state past its executor, and sharing
	// the instance would tie two contexts together for a reason that is not a reason of the domain.
	channelContacts := contacts.NewRepository(pool, cipher)

	// Binds the three collaborators to one transaction, the same pattern turn/ uses below: the webhook event, the contact it
	// may create and the conversation it opens commit together or not at all (regla 5).
	channelTx := func(ctx context.Context, fn func(whatsapp.Store, whatsapp.Aggregate, whatsapp.Contacts) error) error {
		return db.InTx(ctx, pool, func(exec db.Executor) error {
			return fn(channelRepo.WithExecutor(exec), aggregate.WithExecutor(exec), channelContacts.WithExecutor(exec))
		})
	}

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

	channel := whatsapp.NewController(whatsapp.NewService(channelRepo, channelTx, log), turns, log)

	tokens := auth.NewTokens(cfg.JWTSecret, cfg.JWTTTL)
	people := operators.NewService(operators.NewRepository(pool), tokens, log)
	catalogue := labels.NewService(labels.NewRepository(pool), log)

	// A second repository over the same pool, and not the one the catalogue service holds: a repository keeps no state past its
	// executor, and sharing the instance would tie two contexts together for a reason that is not a reason of the domain.
	book := contacts.NewService(contacts.NewRepository(pool, cipher), labels.NewRepository(pool), log)

	return wiring{
		controllers: []soniahttp.Controller{
			operators.NewController(people, log),
			labels.NewController(catalogue, log),
			contacts.NewController(book, log),
		},
		tokenVerifier:     verifyToken(tokens),
		actorLookup:       resolveActor(people),
		publicControllers: []soniahttp.Controller{channel, operators.NewLoginController(people, log)},
		turns:             turns,
	}, nil
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
		return soniahttp.Actor{OperatorID: operator.ID, AccountID: operator.AccountID, Role: operator.Role}, nil
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
