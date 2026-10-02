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
	turnedOff   []uuid.UUID
	waiting     []uuid.UUID

	appendErr   error
	touchOutErr error
	touchedOut  int
	claimErr    error
	claimedBy   map[uuid.UUID]uuid.UUID
	modes       map[uuid.UUID]enums.AgentMode
	starred     map[uuid.UUID]bool
	read        []uuid.UUID
	readErr     error
	resolvedBy  map[uuid.UUID]uuid.UUID
}

func (f *fakeStore) Resolve(_ context.Context, conversationID, operatorID uuid.UUID, _ time.Duration) error {
	if f.resolvedBy == nil {
		f.resolvedBy = map[uuid.UUID]uuid.UUID{}
	}
	f.resolvedBy[conversationID] = operatorID
	return nil
}

func (f *fakeStore) MarkRead(_ context.Context, conversationID uuid.UUID) error {
	if f.readErr != nil {
		return f.readErr
	}
	f.read = append(f.read, conversationID)
	return nil
}

func (f *fakeStore) SetStarred(_ context.Context, conversationID uuid.UUID, starred bool) error {
	if f.starred == nil {
		f.starred = map[uuid.UUID]bool{}
	}
	f.starred[conversationID] = starred
	return nil
}

func (f *fakeStore) SetMode(_ context.Context, conversationID uuid.UUID, mode enums.AgentMode) error {
	if f.modes == nil {
		f.modes = map[uuid.UUID]enums.AgentMode{}
	}
	f.modes[conversationID] = mode
	return nil
}

func (f *fakeStore) TurnOffAIAgent(_ context.Context, aiAgentID uuid.UUID) ([]uuid.UUID, error) {
	f.turnedOff = append(f.turnedOff, aiAgentID)
	return []uuid.UUID{aiAgentID}, nil
}

func (f *fakeStore) MarkWaiting(_ context.Context, conversationID uuid.UUID) error {
	f.waiting = append(f.waiting, conversationID)
	return nil
}

func (f *fakeStore) TouchOutbound(context.Context, uuid.UUID) error {
	if f.touchOutErr != nil {
		return f.touchOutErr
	}
	f.touchedOut++
	return nil
}

func (f *fakeStore) Claim(_ context.Context, conversationID, operatorID uuid.UUID) error {
	if f.claimErr != nil {
		return f.claimErr
	}
	if f.claimedBy == nil {
		f.claimedBy = map[uuid.UUID]uuid.UUID{}
	}
	f.claimedBy[conversationID] = operatorID
	return nil
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
	if f.appendErr != nil {
		return f.appendErr
	}
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
		canAnswer   bool
		wantCreated int
		wantMode    enums.AgentMode
	}{
		{
			name:        "an open conversation is reused instead of opening a second one",
			store:       &fakeStore{open: &winner, appearsOn: 1},
			agent:       &agent,
			canAnswer:   true,
			wantCreated: 0,
		},
		{
			name:        "with no open conversation one is opened in autonomous mode",
			store:       &fakeStore{},
			agent:       &agent,
			canAnswer:   true,
			wantCreated: 1,
			wantMode:    enums.AgentModeAutonomous,
		},
		{
			name:        "with an agent that is paused or removed the conversation is born off, so its switch never says it answers",
			store:       &fakeStore{},
			agent:       &agent,
			canAnswer:   false,
			wantCreated: 1,
			wantMode:    enums.AgentModeOff,
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
			canAnswer:   true,
			wantCreated: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Now().UTC()
			got, err := NewService(tc.store, slog.Default()).EnsureOpen(context.Background(), contact, inbox, tc.agent, tc.canAnswer, at)
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

	_, err := NewService(store, slog.Default()).EnsureOpen(context.Background(), uuid.New(), uuid.New(), nil, false, time.Now())
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

// The fixed text of a turn the pipeline cut (decision O2): the row says the system wrote it, because no model produced it and no
// person typed it. Reading it later as an answer of the AIAgent would put words in a model's mouth that it never said (OE2).
func TestRecordSystemOutboundNamesNoSender(t *testing.T) {
	store := &fakeStore{}
	conversation := uuid.New()

	message, err := NewService(store, slog.Default()).
		RecordSystemOutbound(context.Background(), conversation, "le paso tu mensaje a una persona", time.Now().UTC())
	if err != nil {
		t.Fatalf("RecordSystemOutbound: got %v, want no error", err)
	}

	if message.SenderType != enums.SenderTypeSystem {
		t.Fatalf("sender = %q, want system", message.SenderType)
	}
	// The third arm of the sender check of messages: system with both sender columns empty (H-09).
	if message.SenderAIAgentID != nil || message.SenderOperatorID != nil {
		t.Fatalf("the row named a sender: %+v", message)
	}
	if message.Direction != enums.MessageDirectionOutbound {
		t.Fatalf("direction = %q, want outbound", message.Direction)
	}
	if message.WAMID != nil || message.Status != nil {
		t.Fatalf("the row was written as if it had already been sent: %+v", message)
	}
	if len(store.appended) != 1 {
		t.Fatalf("appended %d rows, want 1", len(store.appended))
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

// TestAnOperatorAnswerNamesThePersonAndNoAIAgent is the sender check of messages seen from the service: a row naming both, or
// neither, is refused by the database, and one naming the agent would credit the bot with a person's work (H-09, ADR-0025).
func TestAnOperatorAnswerNamesThePersonAndNoAIAgent(t *testing.T) {
	store := &fakeStore{}
	operator, conversation := uuid.New(), uuid.New()

	message, err := NewService(store, slog.Default()).
		RecordOperatorOutbound(context.Background(), conversation, operator, "te paso la dirección", time.Now().UTC())
	if err != nil {
		t.Fatalf("RecordOperatorOutbound: got %v, want no error", err)
	}

	if message.SenderType != enums.SenderTypeOperator || message.SenderOperatorID == nil || *message.SenderOperatorID != operator {
		t.Fatalf("sender %q %v, want the operator %v", message.SenderType, message.SenderOperatorID, operator)
	}
	if message.SenderAIAgentID != nil {
		t.Fatalf("the operator's answer also names an agent: %v", *message.SenderAIAgentID)
	}
	if message.WAMID != nil || message.Status != nil {
		t.Fatalf("the row was written as if Meta had already answered: %+v", message)
	}
	if len(store.appended) != 1 || store.touchedOut != 1 {
		t.Fatalf("appended %d and moved the activity %d times, want 1 and 1", len(store.appended), store.touchedOut)
	}
	// Whoever answered read what they answered (ADR-0052).
	if len(store.read) != 1 || store.read[0] != conversation {
		t.Fatalf("marked read %v, want the conversation answered", store.read)
	}
}

// TestAnAnswerMovesTheActivityWhoeverWroteIt keeps the inbox order honest: the agent's answers move the column the same as a person's.
func TestAnAnswerMovesTheActivityWhoeverWroteIt(t *testing.T) {
	store := &fakeStore{}

	if _, err := NewService(store, slog.Default()).
		RecordOutbound(context.Background(), uuid.New(), uuid.New(), "buenas", time.Now().UTC()); err != nil {
		t.Fatalf("RecordOutbound: %v", err)
	}
	if store.touchedOut != 1 {
		t.Fatalf("the agent's answer moved the activity %d times, want 1", store.touchedOut)
	}
	// The bot answering is not the vendor reading what the customer wrote (ADR-0052).
	if len(store.read) != 0 {
		t.Fatalf("the agent's answer marked the conversation read: %v", store.read)
	}
}

// TestAnOutboundWriteThatFailsSaysSo keeps a half-written answer from being reported as written.
func TestAnOutboundWriteThatFailsSaysSo(t *testing.T) {
	boom := errors.New("connection refused")

	for _, tc := range []struct {
		name  string
		store *fakeStore
	}{
		{"the row could not be written", &fakeStore{appendErr: boom}},
		{"the activity could not be moved", &fakeStore{touchOutErr: boom}},
		{"the conversation could not be marked read", &fakeStore{readErr: boom}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewService(tc.store, slog.Default()).
				RecordOperatorOutbound(context.Background(), uuid.New(), uuid.New(), "hola", time.Now().UTC())
			if !errors.Is(err, boom) {
				t.Fatalf("got %v, want the underlying failure", err)
			}
		})
	}
}

// TestClaimReportsTheRaceLost passes ErrTaken through untouched, because it is what the inbox turns into "somebody answered first".
func TestClaimReportsTheRaceLost(t *testing.T) {
	store := &fakeStore{}
	conversation, operator := uuid.New(), uuid.New()

	if err := NewService(store, slog.Default()).Claim(context.Background(), conversation, operator); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if store.claimedBy[conversation] != operator {
		t.Fatalf("the claim did not reach the store: %v", store.claimedBy)
	}

	lost := &fakeStore{claimErr: ErrTaken}
	if err := NewService(lost, slog.Default()).Claim(context.Background(), conversation, operator); !errors.Is(err, ErrTaken) {
		t.Fatalf("Claim on a taken conversation: got %v, want ErrTaken", err)
	}
}

// TestSetModeReachesTheStore is the passthrough F27 needs: the inbox never writes the mode column with SQL of its own (regla 1).
func TestSetModeReachesTheStore(t *testing.T) {
	store := &fakeStore{}
	conversation := uuid.New()

	if err := NewService(store, slog.Default()).SetMode(context.Background(), conversation, enums.AgentModeOff); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	if store.modes[conversation] != enums.AgentModeOff {
		t.Fatalf("the mode did not reach the store: %v", store.modes)
	}
}

// TestSetStarredReachesTheStore is the passthrough F25 needs: the inbox never writes the starred column with SQL of its own (regla 1).
func TestSetStarredReachesTheStore(t *testing.T) {
	store := &fakeStore{}
	conversation := uuid.New()

	if err := NewService(store, slog.Default()).SetStarred(context.Background(), conversation, true); err != nil {
		t.Fatalf("SetStarred: %v", err)
	}
	if got, ok := store.starred[conversation]; !ok || !got {
		t.Fatalf("the star did not reach the store: %v", store.starred)
	}
}

// TestMarkReadReachesTheStore is the passthrough the inbox needs: the unread count is this aggregate's column (regla 1, ADR-0052).
func TestMarkReadReachesTheStore(t *testing.T) {
	store := &fakeStore{}
	conversation := uuid.New()

	if err := NewService(store, slog.Default()).MarkRead(context.Background(), conversation); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	if len(store.read) != 1 || store.read[0] != conversation {
		t.Fatalf("the read did not reach the store: %v", store.read)
	}
}

// TestResolveReachesTheStore is the passthrough the manual close needs: the status is this aggregate's column (regla 1, ADR-0054).
func TestResolveReachesTheStore(t *testing.T) {
	store := &fakeStore{}
	conversation, operator := uuid.New(), uuid.New()

	if err := NewService(store, slog.Default()).Resolve(context.Background(), conversation, operator, time.Minute); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if store.resolvedBy[conversation] != operator {
		t.Fatalf("the close did not reach the store with who closed: %v", store.resolvedBy)
	}
}

// TestTurningOffAnAIAgentReachesTheStore is the passthrough a pause needs: agents/ never writes the mode column itself (regla 1),
// and the ids come back because the audit of the pause is the only record of them (ADR-0038).
func TestTurningOffAnAIAgentReachesTheStore(t *testing.T) {
	store := &fakeStore{}
	agent := uuid.New()

	got, err := NewService(store, slog.Default()).TurnOffAIAgent(context.Background(), agent)
	if err != nil {
		t.Fatalf("TurnOffAIAgent: %v", err)
	}
	if len(store.turnedOff) != 1 || store.turnedOff[0] != agent || len(got) != 1 {
		t.Fatalf("the store turned off %v and answered %v, want the agent asked for and its conversations back", store.turnedOff, got)
	}
}

// TestWithExecutorLeavesADoubledStoreAlone is the other half of the binding: a doubled store has no transaction to bind to, so the
// service comes back as it was instead of failing every unit test of a package that conducts one.
func TestWithExecutorLeavesADoubledStoreAlone(t *testing.T) {
	service := NewService(&fakeStore{}, slog.Default())
	if service.WithExecutor(nil) != service {
		t.Fatal("binding a doubled store built a new service")
	}
}
