package turn

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/brainclient"
	"github.com/mateo-velasquez/sonia/src/core/internal/contacts"
	"github.com/mateo-velasquez/sonia/src/core/internal/conversations"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/whatsapp"
)

// Every write the turn makes, in the order it made them: the order is what 07-secuencia section 2 constrains, not the values.
type stubStore struct {
	mu     sync.Mutex
	target Target

	targetErr    error
	closeErr     error
	processedErr error
	linkErr      error
	contextErr   error

	// What the next tick finds due, handed over once.
	due []Batch

	// One per call and consumed in order, which is the only way to play a race: the first Append loses and the second wins.
	appendErrs []error

	batch     Batch
	appended  []uuid.UUID
	processed []uuid.UUID
	failed    map[uuid.UUID]string
	linked    map[uuid.UUID]uuid.UUID
	transfers []transfer
	adopted   []uuid.UUID
	steps     *[]string
}

// What a transfer_events row carries, which is what the test asserts about it: the event points at the evidence and duplicates
// none of it (H-18).
type transfer struct {
	ConversationID     uuid.UUID
	SignalEvaluationID uuid.UUID
	TurnNumber         int
}

func (s *stubStore) Append(_ context.Context, conversationID uuid.UUID, window time.Duration, _ int) (Batch, error) {
	s.mu.Lock()
	if len(s.appendErrs) > 0 {
		err := s.appendErrs[0]
		s.appendErrs = s.appendErrs[1:]
		if err != nil {
			s.mu.Unlock()
			return Batch{}, err
		}
	}
	s.appended = append(s.appended, conversationID)
	s.mu.Unlock()

	s.record("append")
	s.batch = Batch{ID: uuid.New(), ConversationID: conversationID, TurnNumber: 1, ExpiresAt: time.Now().UTC().Add(window)}
	return s.batch, nil
}

func (s *stubStore) Target(context.Context, uuid.UUID) (Target, error) {
	if s.targetErr != nil {
		return Target{}, s.targetErr
	}
	return s.target, nil
}

// due is what the next tick finds; each call hands one batch over, so a poller loop cannot close the same one forever.
func (s *stubStore) Close(context.Context, time.Duration, time.Duration, int, int) ([]Batch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closeErr != nil {
		return nil, s.closeErr
	}
	closed := s.due
	s.due = nil
	return closed, nil
}

func (s *stubStore) MarkProcessed(_ context.Context, batchID uuid.UUID, _ time.Time) error {
	if s.processedErr != nil {
		return s.processedErr
	}
	s.record("processed_at")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.processed = append(s.processed, batchID)
	return nil
}

func (s *stubStore) MarkFailed(_ context.Context, batchID uuid.UUID, reason string) error {
	s.record("batch_error_message")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed == nil {
		s.failed = map[uuid.UUID]string{}
	}
	s.failed[batchID] = reason
	return nil
}

func (s *stubStore) LinkGeneration(_ context.Context, generationID, messageID uuid.UUID) error {
	if s.linkErr != nil {
		return s.linkErr
	}
	s.record("generation_linked")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.linked == nil {
		s.linked = map[uuid.UUID]uuid.UUID{}
	}
	s.linked[generationID] = messageID
	return nil
}

func (s *stubStore) RecordTransfer(_ context.Context, conversationID, signalEvaluationID uuid.UUID, turnNumber int) error {
	s.record("transfer_event")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transfers = append(s.transfers, transfer{ConversationID: conversationID, SignalEvaluationID: signalEvaluationID, TurnNumber: turnNumber})
	return nil
}

func (s *stubStore) AdoptContext(_ context.Context, generationID uuid.UUID) error {
	if s.contextErr != nil {
		return s.contextErr
	}
	s.record("conversation_context")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.adopted = append(s.adopted, generationID)
	return nil
}

func (s *stubStore) record(step string) {
	if s.steps != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		*s.steps = append(*s.steps, step)
	}
}

type stubAggregate struct {
	mu sync.Mutex

	recordErr error

	linked   map[uuid.UUID]uuid.UUID
	waiting  []uuid.UUID
	outbound []string
	notices  []string
	sent     map[uuid.UUID]string
	failed   map[uuid.UUID]string
	modes    map[uuid.UUID]enums.AgentMode
	steps    *[]string
}

func (a *stubAggregate) SetMode(_ context.Context, conversationID uuid.UUID, mode enums.AgentMode) error {
	a.record("conversation_mode")
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.modes == nil {
		a.modes = map[uuid.UUID]enums.AgentMode{}
	}
	a.modes[conversationID] = mode
	return nil
}

func (a *stubAggregate) LinkToBatch(_ context.Context, messageID, batchID uuid.UUID) error {
	a.record("link_to_batch")
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.linked == nil {
		a.linked = map[uuid.UUID]uuid.UUID{}
	}
	a.linked[messageID] = batchID
	return nil
}

func (a *stubAggregate) MarkWaiting(_ context.Context, conversationID uuid.UUID) error {
	a.record("conversation_waiting")
	a.mu.Lock()
	defer a.mu.Unlock()
	a.waiting = append(a.waiting, conversationID)
	return nil
}

func (a *stubAggregate) RecordOutbound(_ context.Context, conversationID, aiAgentID uuid.UUID, text string, at time.Time) (conversations.Message, error) {
	if a.recordErr != nil {
		return conversations.Message{}, a.recordErr
	}
	a.record("outbound_message")
	a.mu.Lock()
	defer a.mu.Unlock()
	a.outbound = append(a.outbound, text)
	return conversations.Message{
		ID:              uuid.New(),
		ConversationID:  conversationID,
		Direction:       enums.MessageDirectionOutbound,
		SenderType:      enums.SenderTypeAiAgent,
		SenderAIAgentID: &aiAgentID,
		ContentText:     text,
		SentAt:          at,
	}, nil
}

// The fixed text of a cut (decision O2), recorded apart from the answer: a test that could not tell the two writes apart could not
// say whether what reached the customer was the answer the pipeline refused.
func (a *stubAggregate) RecordSystemOutbound(_ context.Context, conversationID uuid.UUID, text string, at time.Time) (conversations.Message, error) {
	if a.recordErr != nil {
		return conversations.Message{}, a.recordErr
	}
	a.record("notice_message")
	a.mu.Lock()
	defer a.mu.Unlock()
	a.notices = append(a.notices, text)
	return conversations.Message{
		ID:             uuid.New(),
		ConversationID: conversationID,
		Direction:      enums.MessageDirectionOutbound,
		SenderType:     enums.SenderTypeSystem,
		ContentText:    text,
		SentAt:         at,
	}, nil
}

func (a *stubAggregate) MarkSent(_ context.Context, messageID uuid.UUID, wamid string, _ time.Time) error {
	a.record("message_sent")
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sent == nil {
		a.sent = map[uuid.UUID]string{}
	}
	a.sent[messageID] = wamid
	return nil
}

func (a *stubAggregate) MarkFailed(_ context.Context, messageID uuid.UUID, errorCode string) error {
	a.record("message_failed")
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.failed == nil {
		a.failed = map[uuid.UUID]string{}
	}
	a.failed[messageID] = errorCode
	return nil
}

func (a *stubAggregate) record(step string) {
	if a.steps != nil {
		a.mu.Lock()
		defer a.mu.Unlock()
		*a.steps = append(*a.steps, step)
	}
}

type stubBrain struct {
	mu       sync.Mutex
	response brainclient.TurnResponse
	errs     []error
	calls    int
	requests []brainclient.TurnRequest
}

func (b *stubBrain) Turn(_ context.Context, req brainclient.TurnRequest) (brainclient.TurnResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls++
	b.requests = append(b.requests, req)

	if len(b.errs) > 0 {
		err := b.errs[0]
		b.errs = b.errs[1:]
		if err != nil {
			return brainclient.TurnResponse{}, err
		}
	}
	return b.response, nil
}

type stubChannel struct {
	inbox whatsapp.Inbox
	err   error
}

func (c *stubChannel) InboxByID(context.Context, uuid.UUID) (whatsapp.Inbox, error) {
	if c.err != nil {
		return whatsapp.Inbox{}, c.err
	}
	return c.inbox, nil
}

type stubSender struct {
	mu    sync.Mutex
	wamid string
	err   error
	calls int
	texts []string
}

// The turn never asks: an answer the brain already generated is written and its failure kept, as ADR-0020 wants of the bot.
func (s *stubSender) Ready(whatsapp.Inbox) error { return nil }

func (s *stubSender) Send(_ context.Context, _ whatsapp.Inbox, _, text string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.texts = append(s.texts, text)
	if s.err != nil {
		return "", s.err
	}
	return s.wamid, nil
}

// Carries a generation id because a real brain always writes the row first, or every case would exercise the evidence-less path.
func generated(text string) brainclient.TurnResponse {
	id := uuid.New()
	return brainclient.TurnResponse{
		Outcome:        brainclient.OutcomeGenerated,
		GenerationID:   &id,
		CompletionText: &text,
	}
}

// stubAttributes is contacts/ as the turn sees it: what it was handed and for whom, in the order of the writes.
type stubAttributes struct {
	steps    *[]string
	err      error
	contacts []uuid.UUID
	recorded []contacts.Extracted
}

func (a *stubAttributes) RecordExtracted(_ context.Context, contactID uuid.UUID, data []contacts.Extracted) error {
	if a.err != nil {
		return a.err
	}
	if a.steps != nil {
		*a.steps = append(*a.steps, "contact_attributes")
	}
	a.contacts = append(a.contacts, contactID)
	a.recorded = append(a.recorded, data...)
	return nil
}

type harness struct {
	orchestrator *Orchestrator
	store        *stubStore
	aggregate    *stubAggregate
	attributes   *stubAttributes
	brain        *stubBrain
	channel      *stubChannel
	sender       *stubSender
	// Every write in the order it happened, which is what section 2 constrains.
	steps *[]string
}

// The transaction is a plain call here: what the test checks is the order of the writes, and binding them is the pool's job.
func conductor() *harness {
	steps := &[]string{}
	agent := uuid.New()

	store := &stubStore{
		target: Target{
			ConversationID:   uuid.New(),
			InboxID:          uuid.New(),
			ContactID:        uuid.New(),
			ContactPhone:     "5493510000000",
			Mode:             enums.AgentModeAutonomous,
			AIAgentID:        &agent,
			AIAgentAvailable: true,
		},
		steps: steps,
	}
	aggregate := &stubAggregate{steps: steps}
	attributes := &stubAttributes{steps: steps}
	brain := &stubBrain{response: generated("buenas, si tenemos")}
	channel := &stubChannel{inbox: whatsapp.Inbox{ID: store.target.InboxID, AccessToken: "token"}}
	sender := &stubSender{wamid: "wamid.HBg"}

	h := &harness{store: store, aggregate: aggregate, attributes: attributes, brain: brain, channel: channel, sender: sender, steps: steps}
	h.orchestrator = New(Options{
		Tx: func(_ context.Context, fn func(Store, Aggregate, Attributes) error) error {
			return fn(store, aggregate, attributes)
		},
		Store:        store,
		Aggregate:    aggregate,
		Brain:        brain,
		Channel:      channel,
		Sender:       sender,
		Budget:       5 * time.Second,
		MaxRetries:   3,
		RetryBackoff: time.Millisecond,
		Logger:       slog.New(slog.DiscardHandler),
	})
	return h
}

// closed is what the poller hands over: the turn now begins at a batch that already stopped waiting (ADR-0041, regla 7).
func (h *harness) closed() Batch {
	h.store.batch = Batch{
		ID:             uuid.New(),
		ConversationID: h.store.target.ConversationID,
		TurnNumber:     1,
		MessagesCount:  3,
		TimerMsUsed:    10000,
		ClosedReason:   enums.BatchCloseReasonSilence,
	}
	return h.store.batch
}

// Runs one turn to completion: Start is asynchronous by contract, so a test that asserts on its writes has to wait for it.
func (h *harness) turn() {
	h.orchestrator.Start(context.Background(), h.closed())
	_ = h.orchestrator.Wait(context.Background())
}

var errBrainDown = errors.New("brain: unreachable")
