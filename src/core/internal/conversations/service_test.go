package conversations

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// fakeStore lets the race be reproduced on purpose, which a live database cannot be asked to do reliably.
type fakeStore struct {
	open        *Conversation
	createErr   error
	appearsOn   int
	openCalls   int
	created     []Conversation
	appended    []Message
	touched     int
	openFailure error
}

func (f *fakeStore) OpenByContact(context.Context, uuid.UUID) (Conversation, error) {
	f.openCalls++
	if f.openFailure != nil {
		return Conversation{}, f.openFailure
	}
	if f.open != nil && f.openCalls >= f.appearsOn {
		return *f.open, nil
	}
	return Conversation{}, ErrNotFound
}

func (f *fakeStore) Create(_ context.Context, c Conversation) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.created = append(f.created, c)
	return nil
}

func (f *fakeStore) AppendMessage(_ context.Context, m Message) error {
	f.appended = append(f.appended, m)
	return nil
}

func (f *fakeStore) TouchInbound(context.Context, uuid.UUID, time.Time) error {
	f.touched++
	return nil
}

// TestEnsureOpenHonoursTheInvariantOfADR0002 protects the partial unique index: the read is a hint and the index is the decision.
func TestEnsureOpenHonoursTheInvariantOfADR0002(t *testing.T) {
	contact, inbox, agent := uuid.New(), uuid.New(), uuid.New()
	winner := Conversation{ID: uuid.New(), ContactID: contact, InboxID: inbox}

	cases := []struct {
		name        string
		store       *fakeStore
		agent       *uuid.UUID
		wantCreated int
		wantMode    enums.AgentMode
	}{
		{
			name:        "an open conversation is reused instead of opening a second one",
			store:       &fakeStore{open: &winner, appearsOn: 1},
			agent:       &agent,
			wantCreated: 0,
		},
		{
			name:        "with no open conversation one is opened in autonomous mode",
			store:       &fakeStore{},
			agent:       &agent,
			wantCreated: 1,
			wantMode:    enums.AgentModeAutonomous,
		},
		{
			name:        "with no agent the conversation is born off, never autonomous over nothing",
			store:       &fakeStore{},
			agent:       nil,
			wantCreated: 1,
			wantMode:    enums.AgentModeOff,
		},
		{
			name:        "losing the race re-reads the winner instead of failing the message",
			store:       &fakeStore{open: &winner, appearsOn: 2, createErr: ErrAlreadyOpen},
			agent:       &agent,
			wantCreated: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Now().UTC()
			got, err := NewService(tc.store, slog.Default()).EnsureOpen(context.Background(), contact, inbox, tc.agent, at)
			if err != nil {
				t.Fatalf("EnsureOpen: got %v, want no error", err)
			}
			if len(tc.store.created) != tc.wantCreated {
				t.Fatalf("created %d conversations, want %d", len(tc.store.created), tc.wantCreated)
			}
			if tc.wantCreated == 0 && got.ID != winner.ID {
				t.Fatalf("returned %v, want the conversation the index kept, %v", got.ID, winner.ID)
			}
			if tc.wantCreated > 0 {
				if got.Mode != tc.wantMode {
					t.Fatalf("mode %q, want %q", got.Mode, tc.wantMode)
				}
				if !got.WindowExpiresAt.Equal(at.Add(ServiceWindow)) {
					t.Fatalf("window closes at %v, want %v: the 24 hours are Meta's", got.WindowExpiresAt, at.Add(ServiceWindow))
				}
			}
		})
	}
}

// TestEnsureOpenDoesNotSwallowARealFailure separates a closed conversation from a database that is down.
func TestEnsureOpenDoesNotSwallowARealFailure(t *testing.T) {
	boom := errors.New("connection refused")
	store := &fakeStore{openFailure: boom}

	_, err := NewService(store, slog.Default()).EnsureOpen(context.Background(), uuid.New(), uuid.New(), nil, time.Now())
	if !errors.Is(err, boom) {
		t.Fatalf("EnsureOpen returned %v, want the underlying failure", err)
	}
	if len(store.created) != 0 {
		t.Fatalf("a conversation was opened while the database was unreachable")
	}
}

// TestRecordInboundStoresBeforeItMovesTheWindow protects the order of 07-secuencia: the message survives a crash, the clock can be recomputed.
func TestRecordInboundStoresBeforeItMovesTheWindow(t *testing.T) {
	store := &fakeStore{}
	at := time.Now().UTC()

	message, err := NewService(store, slog.Default()).RecordInbound(
		context.Background(), Conversation{ID: uuid.New()}, "wamid.HBg", "hola", enums.MessageContentTypeText, at)
	if err != nil {
		t.Fatalf("RecordInbound: got %v, want no error", err)
	}
	if len(store.appended) != 1 || store.touched != 1 {
		t.Fatalf("appended %d and touched %d, want 1 and 1", len(store.appended), store.touched)
	}
	if message.WAMID == nil || *message.WAMID != "wamid.HBg" {
		t.Fatalf("the inbound message lost its channel id, which is the idempotency key of F20")
	}
	if message.SenderType != enums.SenderTypeContact {
		t.Fatalf("sender_type %q, want contact", message.SenderType)
	}
}

// TestDraftOutboundCarriesNoChannelIdYet protects the write before the send: Meta has not answered when this row is built.
func TestDraftOutboundCarriesNoChannelIdYet(t *testing.T) {
	agent := uuid.New()
	draft := NewService(&fakeStore{}, slog.Default()).DraftOutbound(Conversation{ID: uuid.New()}, agent, "buenas", time.Now())

	if draft.WAMID != nil {
		t.Fatalf("the draft already carries a wamid, which only Meta can give it")
	}
	if draft.Status != nil {
		t.Fatalf("the draft already carries a delivery status, which belongs to the channel")
	}
	if draft.SenderAIAgentID == nil || *draft.SenderAIAgentID != agent {
		t.Fatalf("the outbound message does not name the agent, and the sender check constraint rejects that")
	}
}
