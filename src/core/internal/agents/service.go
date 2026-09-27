package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/brainclient"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

var (
	// ErrNotFound covers a removed agent too, not only an absent one (C09).
	ErrNotFound = errors.New("agents: ai agent not found")

	// ErrEmptyName is refused before the database sees it: the column is NOT NULL but blanks would satisfy it.
	ErrEmptyName = errors.New("agents: the name is empty")

	ErrEmptyModel = errors.New("agents: the provider and the model are required")

	// ErrUnpricedModel is a model every turn would refuse before calling the provider: refused on save, where the administrator sees
	// it, and not on the first customer message, where only the log does (ADR-0038).
	ErrUnpricedModel = errors.New("agents: the brain has no tariff for that provider and model")

	// ErrModelsUnavailable refuses the save instead of skipping the check: an agent saved unchecked is the silent failure the check
	// exists for (ADR-0038).
	ErrModelsUnavailable = errors.New("agents: the brain could not say which models it can price")

	// No provider accepts a negative temperature, and the brain would only find out at the first turn.
	ErrInvalidTemperature = errors.New("agents: the temperature cannot be negative")

	// The ceiling is the column's: integer in 0001, so a larger number would fail in the database as a 500 and not as the 400 it is.
	ErrInvalidMaxTokens = errors.New("agents: max_tokens has to be a whole number between 1 and 2147483647")

	// ErrChanged is a save that lost a race: another save moved the model between the first read and the lock, so what this one
	// asked to keep is no longer what the row holds. Refused as that, and not as an unpriced model, which it may not be.
	ErrChanged = errors.New("agents: the ai agent changed while it was being saved")
)

// store is the pool-bound slice of the repository: the reads, which need no transaction of their own.
type store interface {
	List(ctx context.Context, accountID uuid.UUID) ([]AIAgent, error)
	ByID(ctx context.Context, accountID, agentID uuid.UUID) (AIAgent, error)
}

// Store is what this package writes inside the transaction it conducts. Exported because internal/app/deps.go binds it to that
// transaction from outside, the same way turn.Store and inbox.Store are (regla 5).
type Store interface {
	ByIDForUpdate(ctx context.Context, accountID, agentID uuid.UUID) (AIAgent, error)
	Create(ctx context.Context, a AIAgent) (AIAgent, error)
	Update(ctx context.Context, accountID, agentID uuid.UUID, c Config) (AIAgent, error)
	SoftDelete(ctx context.Context, accountID, agentID uuid.UUID) error
	RecordAudit(ctx context.Context, e AuditEntry) error
}

// Inboxes is whatsapp/: removing an agent clears the inboxes that default to it through the repository that owns them (regla 1).
type Inboxes interface {
	ClearDefaultAIAgent(ctx context.Context, accountID, aiAgentID uuid.UUID) ([]uuid.UUID, error)
}

// Catalogue is the brain's list of priced models, asked before a transaction opens: none may wait on the other service (ADR-0017 (i).3).
type Catalogue interface {
	Models(ctx context.Context) (brainclient.ModelsResponse, error)
}

// Conversations is the conversation aggregate: a pause or a removal turns off the bot of every conversation the agent holds through
// the package that owns them (regla 1), so no switch says the bot answers when nobody will (ADR-0038).
type Conversations interface {
	TurnOffAIAgent(ctx context.Context, aiAgentID uuid.UUID) ([]uuid.UUID, error)
}

// TxFunc runs fn in one transaction with every collaborator bound to it (regla 5), and is a function so a test needs no PostgreSQL.
type TxFunc func(ctx context.Context, fn func(Store, Inboxes, Conversations) error) error

// Events is inbox's hub as agents/ sees it: the bots a pause or a removal turned off, told once it committed. ADR-0038 left the
// vendor with nothing telling it in the moment; this is that telling (ADR-0051).
type Events interface {
	Changed(conversationID uuid.UUID)
}

type Options struct {
	Repo      store
	Tx        TxFunc
	Catalogue Catalogue
	// Nil tells nobody, which is what every test of the agents alone needs.
	Events Events
	Logger *slog.Logger
}

type Service struct{ opts Options }

func NewService(opts Options) *Service {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Service{opts: opts}
}

func (s *Service) List(ctx context.Context, accountID uuid.UUID) ([]AIAgent, error) {
	return s.opts.Repo.List(ctx, accountID)
}

func (s *Service) Get(ctx context.Context, accountID, agentID uuid.UUID) (AIAgent, error) {
	return s.opts.Repo.ByID(ctx, accountID, agentID)
}

// Create is born active: a new agent answers nobody until an inbox names it as its default, so there is nothing to pause yet.
func (s *Service) Create(ctx context.Context, accountID, operatorID uuid.UUID, c Config) (AIAgent, error) {
	c = normalize(c)
	c.Active = true
	if err := validate(c); err != nil {
		return AIAgent{}, err
	}
	priced, err := s.priced(ctx)
	if err != nil {
		return AIAgent{}, err
	}
	if err := checkModel(priced, c); err != nil {
		return AIAgent{}, err
	}

	var created AIAgent
	err = s.opts.Tx(ctx, func(repo Store, _ Inboxes, _ Conversations) error {
		if created, err = repo.Create(ctx, AIAgent{
			ID: id.New(), AccountID: accountID, Name: c.Name, Description: c.Description,
			LLMProvider: c.LLMProvider, LLMModel: c.LLMModel, Temperature: c.Temperature, MaxTokens: c.MaxTokens,
			ExtractionEnabled: c.ExtractionEnabled,
		}); err != nil {
			return err
		}
		after := created.Config()
		return record(ctx, repo, operatorID, actionCreate, created.ID, change{After: &after})
	})
	if err != nil {
		return AIAgent{}, err
	}

	s.opts.Logger.Info("ai agent created",
		slog.String("ai_agent_id", created.ID.String()), slog.String("operator_id", operatorID.String()))
	return created, nil
}

// Update applies edit to the row as it stands, so what nobody sent keeps its value, and writes nothing when nothing changed: an
// audit row per click of "save" would bury the changes that matter for OE2 (ADR-0007).
func (s *Service) Update(ctx context.Context, accountID, operatorID, agentID uuid.UUID, edit func(*Config)) (AIAgent, error) {
	current, err := s.opts.Repo.ByID(ctx, accountID, agentID)
	if err != nil {
		return AIAgent{}, err
	}
	wanted := apply(current.Config(), edit)
	if err := validate(wanted); err != nil {
		return AIAgent{}, err
	}

	// Asked only when the model is what changes, and before the transaction: holding a row lock across an HTTP call to the other
	// service would make every save of this agent wait on the brain (ADR-0017 (i).3).
	var priced brainclient.ModelsResponse
	asked := false
	if wanted.LLMProvider != current.LLMProvider || wanted.LLMModel != current.LLMModel {
		if priced, err = s.priced(ctx); err != nil {
			return AIAgent{}, err
		}
		asked = true
	}

	updated := current
	var turnedOff []uuid.UUID
	err = s.opts.Tx(ctx, func(repo Store, _ Inboxes, conversations Conversations) error {
		locked, err := repo.ByIDForUpdate(ctx, accountID, agentID)
		if err != nil {
			return err
		}
		// Merged again on the locked row: a save that committed between the two reads is what this edit applies to.
		before := locked.Config()
		after := apply(before, edit)
		if err := validate(after); err != nil {
			return err
		}
		if after.equal(before) {
			updated = locked
			return nil
		}
		if after.LLMProvider != before.LLMProvider || after.LLMModel != before.LLMModel {
			// Unasked here is a model that did not change on the first read and did on the second: never saved unchecked.
			if !asked {
				return ErrChanged
			}
			if err := checkModel(priced, after); err != nil {
				return err
			}
		}
		if updated, err = repo.Update(ctx, accountID, agentID, after); err != nil {
			return err
		}
		// A pause leaves the conversations to their vendors: a switch still on would tell the vendor the bot answers (ADR-0038).
		if before.Active && !after.Active {
			if turnedOff, err = conversations.TurnOffAIAgent(ctx, agentID); err != nil {
				return err
			}
		}
		return record(ctx, repo, operatorID, actionUpdate, agentID,
			change{Before: &before, After: &after, TurnedOffConversationIDs: turnedOff})
	})
	if err != nil {
		return AIAgent{}, err
	}

	s.told(turnedOff)
	if len(turnedOff) > 0 {
		s.opts.Logger.Info("ai agent paused, its bots turned off",
			slog.String("ai_agent_id", agentID.String()), slog.String("operator_id", operatorID.String()),
			slog.Int("turned_off_conversations", len(turnedOff)))
	}
	return updated, nil
}

// Delete removes the agent, clears the inboxes that default to it and turns off the bot of the conversations it holds, in one
// transaction: no conversation is born with it once the removal commits, and none keeps a switch saying it answers. The
// conversations keep its id, which is who answered them (ADR-0038).
func (s *Service) Delete(ctx context.Context, accountID, operatorID, agentID uuid.UUID) error {
	var cleared, turnedOff []uuid.UUID
	err := s.opts.Tx(ctx, func(repo Store, inboxes Inboxes, conversations Conversations) error {
		locked, err := repo.ByIDForUpdate(ctx, accountID, agentID)
		if err != nil {
			return err
		}
		if err := repo.SoftDelete(ctx, accountID, agentID); err != nil {
			return err
		}
		if cleared, err = inboxes.ClearDefaultAIAgent(ctx, accountID, agentID); err != nil {
			return err
		}
		if turnedOff, err = conversations.TurnOffAIAgent(ctx, agentID); err != nil {
			return err
		}
		before := locked.Config()
		return record(ctx, repo, operatorID, actionDelete, agentID,
			change{Before: &before, ClearedInboxIDs: cleared, TurnedOffConversationIDs: turnedOff})
	})
	if err != nil {
		return err
	}

	s.told(turnedOff)
	s.opts.Logger.Info("ai agent removed",
		slog.String("ai_agent_id", agentID.String()), slog.String("operator_id", operatorID.String()),
		slog.Int("cleared_inboxes", len(cleared)), slog.Int("turned_off_conversations", len(turnedOff)))
	return nil
}

func (s *Service) told(conversationIDs []uuid.UUID) {
	if s.opts.Events == nil {
		return
	}
	for _, conversationID := range conversationIDs {
		s.opts.Events.Changed(conversationID)
	}
}

func (s *Service) priced(ctx context.Context) (brainclient.ModelsResponse, error) {
	res, err := s.opts.Catalogue.Models(ctx)
	if err != nil {
		return brainclient.ModelsResponse{}, fmt.Errorf("%w: %w", ErrModelsUnavailable, err)
	}
	return res, nil
}

// The refusal names the tariff version that decided it, so the log says which pricing.yaml to look at (ADR-0038).
func checkModel(priced brainclient.ModelsResponse, c Config) error {
	for _, m := range priced.Models {
		if m.Provider == c.LLMProvider && m.Model == c.LLMModel {
			return nil
		}
	}
	return fmt.Errorf("%w: %s/%s is not in pricing version %d", ErrUnpricedModel, c.LLMProvider, c.LLMModel, priced.PricingVersion)
}

func apply(c Config, edit func(*Config)) Config {
	edit(&c)
	return normalize(c)
}

// A blank description is no description: stored as NULL, so a list does not show an empty line as if somebody wrote one.
func normalize(c Config) Config {
	c.Name = strings.TrimSpace(c.Name)
	c.LLMProvider = strings.TrimSpace(c.LLMProvider)
	c.LLMModel = strings.TrimSpace(c.LLMModel)
	if c.Description != nil {
		trimmed := strings.TrimSpace(*c.Description)
		c.Description = nil
		if trimmed != "" {
			c.Description = &trimmed
		}
	}
	return c
}

func validate(c Config) error {
	switch {
	case c.Name == "":
		return ErrEmptyName
	case c.LLMProvider == "" || c.LLMModel == "":
		return ErrEmptyModel
	case c.Temperature != nil && *c.Temperature < 0:
		return ErrInvalidTemperature
	case c.MaxTokens != nil && (*c.MaxTokens < 1 || *c.MaxTokens > math.MaxInt32):
		return ErrInvalidMaxTokens
	}
	return nil
}

func record(ctx context.Context, repo Store, operatorID uuid.UUID, action string, agentID uuid.UUID, ch change) error {
	changes, err := json.Marshal(ch)
	if err != nil {
		return fmt.Errorf("agents: encode the audit of %s: %w", action, err)
	}
	return repo.RecordAudit(ctx, AuditEntry{
		ID: id.New(), OperatorID: operatorID, Action: action, EntityType: entityAIAgent, EntityID: agentID, Changes: changes,
	})
}
