package whatsapp

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
)

// ErrAIAgentNotAssignable is an agent of another account, one that does not exist, or one the admin removed: none of them can
// answer the conversations of a number (ADR-0038). A paused agent is assignable, and the turn skips it until it is resumed.
var ErrAIAgentNotAssignable = errors.New("whatsapp: the ai agent does not exist or was removed")

// ErrPhoneNumberIDInUse is returned when trying to create an inbox with a phone number ID that already exists.
var ErrPhoneNumberIDInUse = errors.New("whatsapp: the phone number id is already in use")

// settingsReader is the pool-bound read of the admin panel, which needs no transaction of its own.
type settingsReader interface {
	ListSettings(ctx context.Context, accountID uuid.UUID) ([]InboxSettings, error)
	CreateSettings(ctx context.Context, accountID uuid.UUID, displayName, phoneNumberID, wabaID, accessToken, verifyToken string) (InboxSettings, error)
}

// SettingsStore is what the settings write inside the transaction they conduct. Exported because internal/app/deps.go binds it to
// that transaction from outside, the same way Store is (regla 5).
type SettingsStore interface {
	SettingsForUpdate(ctx context.Context, accountID, inboxID uuid.UUID) (InboxSettings, error)
	AIAgentAssignable(ctx context.Context, accountID, aiAgentID uuid.UUID) error
	SetDefaultAIAgent(ctx context.Context, accountID, inboxID uuid.UUID, aiAgentID *uuid.UUID) (InboxSettings, error)
}

// Auditor is agents/: audit_logs is its table, so the change of which agent answers a number is written through it and never with
// SQL of our own (regla 1). Plain ids and not a type of that package, so the two packages do not import each other.
type Auditor interface {
	RecordInboxDefault(ctx context.Context, operatorID, inboxID uuid.UUID, before, after *uuid.UUID) error
}

// SettingsTxFunc runs fn in one transaction with both collaborators bound to it (regla 5): the default and its audit commit together.
type SettingsTxFunc func(ctx context.Context, fn func(SettingsStore, Auditor) error) error

type SettingsOptions struct {
	Repo   settingsReader
	Tx     SettingsTxFunc
	Logger *slog.Logger
}

// Settings is what the admin configures of a connected number. It is a service of its own and not a method of Service, because
// Service is the webhook and answers Meta, and this one answers a person.
type Settings struct{ opts SettingsOptions }

func NewSettings(opts SettingsOptions) *Settings {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Settings{opts: opts}
}

func (s *Settings) List(ctx context.Context, accountID uuid.UUID) ([]InboxSettings, error) {
	return s.opts.Repo.ListSettings(ctx, accountID)
}

// CAMBIADO POR TOBI
func (s *Settings) Create(ctx context.Context, accountID uuid.UUID, displayName, phoneNumberID, wabaID, accessToken, verifyToken string) (InboxSettings, error) {
	return s.opts.Repo.CreateSettings(ctx, accountID, displayName, phoneNumberID, wabaID, accessToken, verifyToken)
}

// SetDefaultAIAgent names the agent that answers the conversations born in this inbox from now on, or none with nil. A save that
// changes nothing writes nothing, audit included, the same rule the agents follow (ADR-0038, regla 9).
func (s *Settings) SetDefaultAIAgent(ctx context.Context, accountID, operatorID, inboxID uuid.UUID, aiAgentID *uuid.UUID) (InboxSettings, error) {
	var updated InboxSettings
	changed := false
	err := s.opts.Tx(ctx, func(store SettingsStore, audit Auditor) error {
		current, err := store.SettingsForUpdate(ctx, accountID, inboxID)
		if err != nil {
			return err
		}
		if sameAIAgent(current.DefaultAIAgentID, aiAgentID) {
			updated = current
			return nil
		}
		if aiAgentID != nil {
			if err := store.AIAgentAssignable(ctx, accountID, *aiAgentID); err != nil {
				return err
			}
		}
		if updated, err = store.SetDefaultAIAgent(ctx, accountID, inboxID, aiAgentID); err != nil {
			return err
		}
		changed = true
		return audit.RecordInboxDefault(ctx, operatorID, inboxID, current.DefaultAIAgentID, aiAgentID)
	})
	if err != nil {
		return InboxSettings{}, err
	}

	if changed {
		s.opts.Logger.Info("inbox default agent set",
			slog.String("inbox_id", inboxID.String()), slog.String("operator_id", operatorID.String()))
	}
	return updated, nil
}

func sameAIAgent(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
