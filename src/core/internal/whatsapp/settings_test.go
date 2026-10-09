package whatsapp

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// stubSettings answers from memory and plays the reader and the transactional store, so the rules are tested apart from the SQL.
type stubSettings struct {
	inboxes    map[uuid.UUID]InboxSettings
	assignable map[uuid.UUID]bool
	writes     []string
}

func (s *stubSettings) ListSettings(_ context.Context, accountID uuid.UUID) ([]InboxSettings, error) {
	out := []InboxSettings{}
	for _, in := range s.inboxes {
		if in.AccountID == accountID {
			out = append(out, in)
		}
	}
	return out, nil
}

func (s *stubSettings) CreateSettings(_ context.Context, accountID uuid.UUID, displayName, phoneNumberID, wabaID, accessToken, verifyToken string) (InboxSettings, error) {
	in := InboxSettings{
		ID:            uuid.New(),
		AccountID:     accountID,
		DisplayName:   displayName,
		PhoneNumberID: phoneNumberID,
		Status:        enums.InboxStatusActive,
	}
	s.inboxes[in.ID] = in
	s.writes = append(s.writes, "inbox_create")
	return in, nil
}

func (s *stubSettings) SettingsForUpdate(_ context.Context, accountID, inboxID uuid.UUID) (InboxSettings, error) {
	in, ok := s.inboxes[inboxID]
	if !ok || in.AccountID != accountID {
		return InboxSettings{}, ErrInboxNotFound
	}
	return in, nil
}

func (s *stubSettings) RemoveSettings(_ context.Context, accountID, inboxID uuid.UUID) error {
	in, ok := s.inboxes[inboxID]
	if !ok || in.AccountID != accountID {
		return ErrInboxNotFound
	}
	in.Status = enums.InboxStatusInactive
	s.inboxes[inboxID] = in
	s.writes = append(s.writes, "inbox_remove")
	return nil
}

func (s *stubSettings) SetInboxOperators(_ context.Context, accountID, inboxID uuid.UUID, operatorIDs []uuid.UUID) error {
	in, ok := s.inboxes[inboxID]
	if !ok || in.AccountID != accountID {
		return ErrInboxNotFound
	}
	s.writes = append(s.writes, "inbox_operators")
	return nil
}

func (s *stubSettings) AIAgentAssignable(_ context.Context, _, aiAgentID uuid.UUID) error {
	if !s.assignable[aiAgentID] {
		return ErrAIAgentNotAssignable
	}
	return nil
}

func (s *stubSettings) SetDefaultAIAgent(_ context.Context, _, inboxID uuid.UUID, aiAgentID *uuid.UUID) (InboxSettings, error) {
	in := s.inboxes[inboxID]
	in.DefaultAIAgentID = aiAgentID
	s.inboxes[inboxID] = in
	s.writes = append(s.writes, "inbox")
	return in, nil
}

type auditCall struct {
	operatorID, inboxID uuid.UUID
	before, after       *uuid.UUID
}

type stubAuditor struct{ calls []auditCall }

func (a *stubAuditor) RecordInboxDefault(_ context.Context, operatorID, inboxID uuid.UUID, before, after *uuid.UUID) error {
	a.calls = append(a.calls, auditCall{operatorID, inboxID, before, after})
	return nil
}

var (
	settingsAccount  = id.New()
	settingsOperator = id.New()
)

// numbersOver builds the settings over one inbox that answers with current, and an agent the store considers assignable.
func numbersOver(current *uuid.UUID) (*Settings, *stubSettings, *stubAuditor, InboxSettings, uuid.UUID) {
	inbox := InboxSettings{ID: id.New(), AccountID: settingsAccount, DisplayName: "Numero", PhoneNumberID: "123",
		Status: enums.InboxStatusActive, DefaultAIAgentID: current}
	agent := id.New()
	store := &stubSettings{inboxes: map[uuid.UUID]InboxSettings{inbox.ID: inbox}, assignable: map[uuid.UUID]bool{agent: true}}
	audit := &stubAuditor{}
	settings := NewSettings(SettingsOptions{
		Repo: store,
		Tx: func(_ context.Context, fn func(SettingsStore, Auditor) error) error {
			return fn(store, audit)
		},
	})
	return settings, store, audit, inbox, agent
}

// Naming an agent writes the inbox and audits who changed it from what to what, in the transaction of the change (ADR-0007, ADR-0038).
func TestNamingAnAIAgentForANumberIsAudited(t *testing.T) {
	previous := id.New()
	settings, store, audit, inbox, agent := numbersOver(&previous)

	got, err := settings.SetDefaultAIAgent(context.Background(), settingsAccount, settingsOperator, inbox.ID, &agent)
	if err != nil {
		t.Fatalf("SetDefaultAIAgent: %v", err)
	}
	if got.DefaultAIAgentID == nil || *got.DefaultAIAgentID != agent {
		t.Fatalf("the inbox now defaults to %v, want %v", got.DefaultAIAgentID, agent)
	}
	if len(audit.calls) != 1 {
		t.Fatalf("audit calls = %d, want 1", len(audit.calls))
	}
	call := audit.calls[0]
	if call.operatorID != settingsOperator || call.inboxID != inbox.ID || *call.before != previous || *call.after != agent {
		t.Fatalf("audit = %+v, want %v changing %v from %v to %v", call, settingsOperator, inbox.ID, previous, agent)
	}
	if len(store.writes) != 1 {
		t.Fatalf("writes = %v, want the inbox once", store.writes)
	}
}

// null leaves the number without AI, which ADR-0012 declares valid: the conversations born after it start off.
func TestNullLeavesTheNumberWithoutAI(t *testing.T) {
	previous := id.New()
	settings, _, audit, inbox, _ := numbersOver(&previous)

	got, err := settings.SetDefaultAIAgent(context.Background(), settingsAccount, settingsOperator, inbox.ID, nil)
	if err != nil {
		t.Fatalf("SetDefaultAIAgent: %v", err)
	}
	if got.DefaultAIAgentID != nil || len(audit.calls) != 1 || audit.calls[0].after != nil {
		t.Fatalf("inbox %v with audit %+v, want no agent and the unlinking audited", got.DefaultAIAgentID, audit.calls)
	}
}

// A save that changes nothing writes nothing, audit included: the rule the agents already follow (ADR-0038, regla 9).
func TestNamingTheSameAIAgentWritesNothing(t *testing.T) {
	settings, store, audit, inbox, agent := numbersOver(nil)
	store.inboxes[inbox.ID] = InboxSettings{ID: inbox.ID, AccountID: settingsAccount, DefaultAIAgentID: &agent}

	if _, err := settings.SetDefaultAIAgent(context.Background(), settingsAccount, settingsOperator, inbox.ID, &agent); err != nil {
		t.Fatalf("SetDefaultAIAgent: %v", err)
	}
	if len(store.writes) != 0 || len(audit.calls) != 0 {
		t.Fatalf("a save with no change wrote %v and audited %d times", store.writes, len(audit.calls))
	}
}

// An agent that cannot answer the conversations of a number is refused before anything is written (ADR-0038).
func TestAnAIAgentThatCannotBeAssignedIsRefused(t *testing.T) {
	settings, store, audit, inbox, _ := numbersOver(nil)
	unknown := id.New()

	_, err := settings.SetDefaultAIAgent(context.Background(), settingsAccount, settingsOperator, inbox.ID, &unknown)
	if !errors.Is(err, ErrAIAgentNotAssignable) {
		t.Fatalf("SetDefaultAIAgent: got %v, want ErrAIAgentNotAssignable", err)
	}
	if len(store.writes) != 0 || len(audit.calls) != 0 {
		t.Fatalf("a refused agent still wrote %v and audited %d times", store.writes, len(audit.calls))
	}
}

func TestANumberOfAnotherAccountIsNotFound(t *testing.T) {
	settings, _, _, inbox, agent := numbersOver(nil)

	if _, err := settings.SetDefaultAIAgent(context.Background(), id.New(), settingsOperator, inbox.ID, &agent); !errors.Is(err, ErrInboxNotFound) {
		t.Fatalf("SetDefaultAIAgent: got %v, want ErrInboxNotFound", err)
	}
	if list, _ := settings.List(context.Background(), id.New()); len(list) != 0 {
		t.Fatalf("another account lists %d inboxes, want none", len(list))
	}
}
