package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/agents"
	"github.com/mateo-velasquez/sonia/src/core/internal/brainclient"
	"github.com/mateo-velasquez/sonia/src/core/internal/contacts"
	"github.com/mateo-velasquez/sonia/src/core/internal/conversations"
	"github.com/mateo-velasquez/sonia/src/core/internal/inbox"
	"github.com/mateo-velasquez/sonia/src/core/internal/labels"
	"github.com/mateo-velasquez/sonia/src/core/internal/notes"
	"github.com/mateo-velasquez/sonia/src/core/internal/operators"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/auth"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/config"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/encryption"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
	quickreplies "github.com/mateo-velasquez/sonia/src/core/internal/quick_replies"
	"github.com/mateo-velasquez/sonia/src/core/internal/realtime"
	"github.com/mateo-velasquez/sonia/src/core/internal/reasoning"
	"github.com/mateo-velasquez/sonia/src/core/internal/reminders"
	"github.com/mateo-velasquez/sonia/src/core/internal/sales"
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
	// Held so the shutdown can stop it first: a poller still ticking would keep adding turns to what the deployment waits for.
	poller *turn.Poller
	// Held so the shutdown can close the sockets, which the server does not track once they are hijacked (ADR-0051).
	hub *realtime.Hub
	// Held so the shutdown stops it with the turn's poller: an alert marked as the process dies would be told to nobody (ADR-0058).
	alarms *reminders.Poller
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

	// The fan-out of F21, told by every package that writes a conversation once its transaction committed (ADR-0051).
	hub := realtime.NewHub(realtime.HubOptions{Rows: inbox.NewRepository(pool), Logger: log})

	// A repository of its own and not the one `book` holds below: a repository keeps no state past its executor, and sharing
	// the instance would tie two contexts together for a reason that is not a reason of the domain.
	channelContacts := contacts.NewRepository(pool, cipher)
	deskRepo := inbox.NewRepository(pool)

	// Binds the four collaborators to one transaction, the same pattern turn/ uses below: the webhook event, the contact it may
	// create, the conversation it opens and the vendor it gives it commit together or not at all (regla 5, ADR-0037).
	channelTx := func(ctx context.Context, fn func(whatsapp.Store, whatsapp.Aggregate, whatsapp.Contacts, whatsapp.Assigner) error) error {
		return db.InTx(ctx, pool, func(exec db.Executor) error {
			bound := aggregate.WithExecutor(exec)
			return fn(channelRepo.WithExecutor(exec), bound, channelContacts.WithExecutor(exec),
				inbox.NewDispatcher(deskRepo.WithExecutor(exec), bound, log))
		})
	}

	// One sender for the agent and the operator alike: the two answer the same customers through the same number, and a doubled
	// stack has to double both or the smoke would send a real message.
	send := sender(cfg, log)

	turnRepo := turn.NewRepository(pool)

	// The owner of contact_attributes, bound to the turn's transaction so what the brain extracted commits with it (ADR-0047).
	turnContacts := contacts.NewRepository(pool, cipher)

	// Binds the three collaborators to the same transaction, which is what lets it cross packages (regla 5). Shared by the two
	// sides of the turn: the one that gathers the messages and the one that answers them.
	turnTx := func(ctx context.Context, fn func(turn.Store, turn.Aggregate, turn.Attributes) error) error {
		return db.InTx(ctx, pool, func(exec db.Executor) error {
			return fn(turnRepo.WithExecutor(exec), aggregate.WithExecutor(exec), turnContacts.WithExecutor(exec))
		})
	}

	turns := turn.New(turn.Options{
		Tx:        turnTx,
		Store:     turnRepo,
		Aggregate: aggregate,
		Brain: brainclient.New(brainclient.Options{
			BaseURL: cfg.BrainBaseURL,
			Token:   cfg.BrainInternalToken,
			Timeout: cfg.BrainTimeout,
			Logger:  log,
		}),
		Channel:      channelRepo,
		Sender:       send,
		Events:       hub,
		Budget:       cfg.TurnBudget,
		MaxRetries:   cfg.TurnMaxRetries,
		RetryBackoff: cfg.TurnRetryBackoff,
		Logger:       log,
	})

	// What the webhook hands each message to: the turn no longer starts there, it starts when the batch closes (ADR-0041, regla 7).
	collector := turn.NewCollector(turn.CollectorOptions{
		Tx:          turnTx,
		Window:      cfg.TurnBatchWindow,
		MaxMessages: cfg.TurnBatchMaxMessages,
		Logger:      log,
	})

	// Where the timer of F10 goes off. Pool-bound and outside any transaction of ours: the close is one statement, and the locks
	// it takes with SKIP LOCKED last exactly as long as that statement (ADR-0041, regla 4).
	poller := turn.NewPoller(turn.PollerOptions{
		Store:       turnRepo,
		Interval:    cfg.TurnPollInterval,
		Window:      cfg.TurnBatchWindow,
		MaxDuration: cfg.TurnBatchMaxDuration,
		MaxMessages: cfg.TurnBatchMaxMessages,
		Limit:       cfg.TurnPollLimit,
		Turns:       turns,
		Logger:      log,
	})

	channel := whatsapp.NewController(whatsapp.ControllerOptions{
		Service:   whatsapp.NewService(channelRepo, channelTx, log),
		Collector: collector,
		Events:    hub,
		Logger:    log,
	})

	tokens := auth.NewTokens(cfg.JWTSecret, cfg.JWTTTL)
	people := operators.NewService(operators.NewRepository(pool), tokens, log)
	catalogue := labels.NewService(labels.NewRepository(pool), log)

	// A second repository over the same pool, and not the one the catalogue service holds: a repository keeps no state past its
	// executor, and sharing the instance would tie two contexts together for a reason that is not a reason of the domain.
	book := contacts.NewService(contacts.NewRepository(pool, cipher), labels.NewRepository(pool), log)
	desk := inbox.NewService(inbox.Options{
		Repo:      deskRepo,
		Aggregate: aggregate,
		Catalogue: labels.NewRepository(pool),
		// Binds the assignment and the aggregate to one transaction: taking the pool and writing the answer commit together (regla 5).
		Tx: func(ctx context.Context, fn func(inbox.Store, inbox.Aggregate, inbox.Reviews) error) error {
			return db.InTx(ctx, pool, func(exec db.Executor) error {
				return fn(deskRepo.WithExecutor(exec), aggregate.WithExecutor(exec), turnRepo.WithExecutor(exec))
			})
		},
		Channel: channelRepo,
		Sender:  send,
		Events:  hub,
		Logger:  log,

		TurnBudget: cfg.TurnBudget,
	})

	// A second client of the brain and not the turn's: an administrator is waiting on this one, so it gets the short timeout of
	// the admin panel and not the ten seconds a turn may take.
	adminBrain := brainclient.New(brainclient.Options{
		BaseURL: cfg.BrainBaseURL,
		Token:   cfg.BrainInternalToken,
		Timeout: cfg.PreviewTimeout,
		Logger:  log,
	})
	agentRepo := agents.NewRepository(pool)
	identity := agents.NewService(agents.Options{
		Repo: agentRepo,
		// Binds the agent, the inboxes and the conversations to one transaction: a pause or a removal and the bots it turns off
		// commit together, and so do a removal and the defaults it clears (regla 5).
		Tx: func(ctx context.Context, fn func(agents.Store, agents.Inboxes, agents.Conversations) error) error {
			return db.InTx(ctx, pool, func(exec db.Executor) error {
				return fn(agentRepo.WithExecutor(exec), channelRepo.WithExecutor(exec), aggregate.WithExecutor(exec))
			})
		},
		Catalogue: adminBrain,
		Events:    hub,
		Logger:    log,
	})

	blocks := agents.NewComponents(agents.ComponentOptions{
		Repo: agentRepo,
		// A block and its version commit together, so no component changes without the version OE2 reads (regla 5, ADR-0039).
		Tx: func(ctx context.Context, fn func(agents.ComponentStore) error) error {
			return db.InTx(ctx, pool, func(exec db.Executor) error {
				return fn(agentRepo.WithExecutor(exec))
			})
		},
		Logger: log,
	})

	// The admin's client of the brain, with the short timeout: an administrator is typing and the next keystroke asks again.
	previews := agents.NewPreviews(agents.PreviewOptions{Repo: agentRepo, Brain: adminBrain, Logger: log})

	// F18: the person and the audit row that says who changed them commit together, through agents/ as whatsapp/ does (ADR-0062).
	peopleRepo := operators.NewRepository(pool)
	roster := operators.NewRoster(operators.RosterOptions{
		Repo: peopleRepo,
		Tx: func(ctx context.Context, fn func(operators.RosterStore, operators.Auditor) error) error {
			return db.InTx(ctx, pool, func(exec db.Executor) error {
				return fn(peopleRepo.WithExecutor(exec), agentRepo.WithExecutor(exec))
			})
		},
		Logger: log,
	})

	numbers := whatsapp.NewSettings(whatsapp.SettingsOptions{
		Repo: channelRepo,
		// Binds the inbox and the audit to one transaction: the agent a number names and the row that says who named it commit together (regla 5).
		Tx: func(ctx context.Context, fn func(whatsapp.SettingsStore, whatsapp.Auditor) error) error {
			return db.InTx(ctx, pool, func(exec db.Executor) error {
				return fn(channelRepo.WithExecutor(exec), agentRepo.WithExecutor(exec))
			})
		},
		Logger: log,
	})

	// F28: the reminders hang from contacts/, which decides the reach, and go off through the hub when due (ADR-0058).
	reminderRepo := reminders.NewRepository(pool)
	alarms := reminders.NewPoller(reminders.PollerOptions{Store: reminderRepo, Alerts: hub, Logger: log})

	// The socket is public for the router because the browser cannot send the bearer on it; its ticket is the credential, and the
	// operator is read again with the same lookup the bearer uses (ADR-0051).
	gate := realtime.NewStreamGate(hub, resolveActor(people), log)

	return wiring{
		controllers: []soniahttp.Controller{
			operators.NewController(people, log),
			operators.NewRosterController(roster, log),
			labels.NewController(catalogue, log),
			contacts.NewController(book, log),
			inbox.NewController(desk, log),
			// The notes and F05 hang from a conversation, so the inbox's service is their reach (ADR-0063).
			notes.NewController(notes.NewService(notes.NewRepository(pool), desk), log),
			reasoning.NewController(reasoning.NewService(reasoning.NewRepository(pool), desk), log),
			realtime.NewStreamController(hub, log),
			agents.NewController(identity, log),
			agents.NewComponentsController(blocks, log),
			agents.NewPreviewController(previews, log),
			whatsapp.NewSettingsController(numbers, log),
			reminders.NewController(reminders.NewService(reminderRepo, book, log), log),
			sales.NewController(sales.NewService(sales.NewRepository(pool), log), log),
			quickreplies.NewController(quickreplies.NewService(quickreplies.NewRepository(pool), log), log),
		},
		tokenVerifier:     verifyToken(tokens),
		actorLookup:       resolveActor(people),
		publicControllers: []soniahttp.Controller{channel, operators.NewLoginController(people, log), gate},
		turns:             turns,
		poller:            poller,
		hub:               hub,
		alarms:            alarms,
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
