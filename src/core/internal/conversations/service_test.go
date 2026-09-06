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
	linked      map[uuid.UUID]uuid.UUID
	sent        map[uuid.UUID]string
	failed      map[uuid.UUID]string
}

func (f *fakeStore) LinkToBatch(_ context.Context, messageID, batchID uuid.UUID) error {
	if f.linked == nil {
		f.linked = map[uuid.UUID]uuid.UUID{}
	}
	f.linked[messageID] = batchID
	return nil
}

func (f *fakeStore) MarkSent(_ context.Context, messageID uuid.UUID, wamid string, _ time.Time) error {
	if f.sent == nil {
		f.sent = map[uuid.UUID]string{}
	}
	f.sent[messageID] = wamid
	return nil
}

func (f *fakeStore) MarkFailed(_ context.Context, messageID uuid.UUID, errorCode string) error {
	if f.failed == nil {
		f.failed = map[uuid.UUID]string{}
	}
	f.failed[messageID] = errorCode
	return nil
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
	draft := NewService(&fakeStore{}, slog.Default()).DraftOutbound(uuid.New(), agent, "buenas", time.Now())

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

// The answer is written before it is sent, because a send that crashes must leave a visible row and never a hole (07-secuencia).
func TestRecordOutboundWritesTheRowBeforeAnybodySends(t *testing.T) {
	store := &fakeStore{}
	agent := uuid.New()
	conversation := uuid.New()

	message, err := NewService(store, slog.Default()).
		RecordOutbound(context.Background(), conversation, agent, "buenas", time.Now().UTC())
	if err != nil {
		t.Fatalf("RecordOutbound: got %v, want no error", err)
	}

	if len(store.appended) != 1 {
		t.Fatalf("appended %d rows, want 1", len(store.appended))
	}
	if message.WAMID != nil || message.Status != nil {
		t.Fatalf("the row was written as if it had already been sent: %+v", message)
	}
	if message.Direction != enums.MessageDirectionOutbound || message.SenderType != enums.SenderTypeAiAgent {
		t.Fatalf("direction %q and sender %q, want outbound and ai_agent", message.Direction, message.SenderType)
	}
}

// The turn writes messages through here and never with SQL of its own (regla 1), so the three passthroughs have to exist.
func TestTheAggregateExposesWhatTheTurnNeeds(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store, slog.Default())
	message, batch := uuid.New(), uuid.New()

	if err := service.LinkToBatch(context.Background(), message, batch); err != nil {
		t.Fatalf("LinkToBatch: %v", err)
	}
	if err := service.MarkSent(context.Background(), message, "wamid.HBg", time.Now()); err != nil {
		t.Fatalf("MarkSent: %v", err)
	}
	if err := service.MarkFailed(context.Background(), message, "131047"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	if store.linked[message] != batch || store.sent[message] != "wamid.HBg" || store.failed[message] != "131047" {
		t.Fatalf("the aggregate did not reach the store: %+v %+v %+v", store.linked, store.sent, store.failed)
	}
}
