package inbox

import (
	"bytes"
	"context"
	"log/slog"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/conversations"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
	"github.com/mateo-velasquez/sonia/src/core/internal/whatsapp"
)

// stubStore answers from memory, so the service and the handlers are tested against their own rules and not against the SQL.
//
// It carries the reach and the keyset on purpose: the handlers rest on both, so a double that ignored them would let a case pass
// while the real query leaked the conversations of another operator or repeated a row across pages.
type stubStore struct {
	rows      []Summary
	err       error
	lastLimit int

	targets     map[uuid.UUID]Target
	assignments []Assignment
	assignErr   error

	threads   map[uuid.UUID][]ThreadMessage
	threadErr error

	suggestions map[uuid.UUID]Suggestion
	reasonings  map[uuid.UUID]stubReasoning

	// The two answers of the dispatcher's reads (ADR-0037): nil is "nobody", which is a real answer and not a failure.
	previous, next           *uuid.UUID
	previousErr, nextErr     error
	previousAsked, nextAsked int
}

func (s *stubStore) PreviousOperator(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*uuid.UUID, error) {
	s.previousAsked++
	return s.previous, s.previousErr
}

func (s *stubStore) NextInTurn(context.Context, uuid.UUID) (*uuid.UUID, error) {
	s.nextAsked++
	return s.next, s.nextErr
}

// newer is the ORDER BY of the thread: the latest recorded first, and the id when two share the instant.
func newer(a, b ThreadMessage) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.After(b.CreatedAt)
	}
	return bytes.Compare(a.ID[:], b.ID[:]) > 0
}

func (s *stubStore) Thread(_ context.Context, conversationID uuid.UUID, after *Cursor, limit int) ([]ThreadMessage, error) {
	s.lastLimit = limit
	if s.threadErr != nil {
		return nil, s.threadErr
	}

	ordered := append([]ThreadMessage{}, s.threads[conversationID]...)
	sort.Slice(ordered, func(i, j int) bool { return newer(ordered[i], ordered[j]) })

	out := []ThreadMessage{}
	for _, m := range ordered {
		if after != nil && !newer(ThreadMessage{CreatedAt: after.At, Message: Message{ID: after.ID}}, m) {
			continue
		}
		if len(out) == limit {
			break
		}
		out = append(out, m)
	}
	return out, nil
}

func (s *stubStore) reaches(scope Scope, row Summary) bool {
	if scope.seesTheWholeTeam() {
		return true
	}
	return row.Operator == nil || row.Operator.ID == scope.OperatorID
}

// before is the ORDER BY of the query: newest activity first, and the id when two share the instant.
func before(a, b Summary) bool {
	if !a.LastActivityAt.Equal(b.LastActivityAt) {
		return a.LastActivityAt.After(b.LastActivityAt)
	}
	return bytes.Compare(a.ID[:], b.ID[:]) > 0
}

func (s *stubStore) List(_ context.Context, scope Scope, after *Cursor, limit int) ([]Summary, error) {
	s.lastLimit = limit
	if s.err != nil {
		return nil, s.err
	}

	reached := []Summary{}
	for _, row := range s.rows {
		if s.reaches(scope, row) {
			reached = append(reached, row)
		}
	}
	sort.Slice(reached, func(i, j int) bool { return before(reached[i], reached[j]) })

	out := []Summary{}
	for _, row := range reached {
		if after != nil && !before(Summary{LastActivityAt: after.At, ID: after.ID}, row) {
			continue
		}
		if len(out) == limit {
			break
		}
		out = append(out, row)
	}
	return out, nil
}

func (s *stubStore) Target(_ context.Context, _, conversationID uuid.UUID) (Target, error) {
	if s.err != nil {
		return Target{}, s.err
	}
	t, ok := s.targets[conversationID]
	if !ok {
		return Target{}, ErrNotFound
	}
	return t, nil
}

// stubReasoning is a generation and the conversation it belongs to, which the real read checks in its WHERE.
type stubReasoning struct {
	Reasoning
	conversationID uuid.UUID
}

// Keyed by generation, and refused when it is of another conversation, which is what the WHERE of the real read does.
func (s *stubStore) Reasoning(_ context.Context, conversationID, generationID uuid.UUID) (Reasoning, error) {
	r, ok := s.reasonings[generationID]
	if !ok || r.conversationID != conversationID {
		return Reasoning{}, ErrGenerationNotFound
	}
	return r.Reasoning, nil
}

func (s *stubStore) PendingSuggestion(_ context.Context, conversationID uuid.UUID) (Suggestion, error) {
	sg, ok := s.suggestions[conversationID]
	if !ok {
		return Suggestion{}, ErrNoSuggestion
	}
	return sg, nil
}

// stubReviews is turn/ as the inbox sees it: one review per draft sent, recorded so a case can assert what was reviewed.
type stubReviews struct {
	err      error
	reviewed []reviewed
}

type reviewed struct {
	generationID, conversationID, messageID, operatorID uuid.UUID
	text                                                string
}

func (r *stubReviews) ReviewGeneration(
	_ context.Context, generationID, conversationID, messageID, operatorID uuid.UUID, text string,
) (enums.GenerationStatus, error) {
	if r.err != nil {
		return "", r.err
	}
	r.reviewed = append(r.reviewed, reviewed{generationID, conversationID, messageID, operatorID, text})
	return enums.GenerationStatusAccepted, nil
}

func (s *stubStore) RecordAssignment(_ context.Context, a Assignment) error {
	if s.assignErr != nil {
		return s.assignErr
	}
	s.assignments = append(s.assignments, a)
	return nil
}

// stubAggregate is conversations/ seen through the six calls the inbox makes, recording each so a case can assert what was
// written and, as often, what was not.
type stubAggregate struct {
	claimErr      error
	recordErr     error
	markSentErr   error
	markFailedErr error
	setModeErr    error
	setStarredErr error

	claimed  map[uuid.UUID]uuid.UUID
	recorded []conversations.Message
	sent     map[uuid.UUID]string
	failed   map[uuid.UUID]string
	modes    map[uuid.UUID]enums.AgentMode
	starred  map[uuid.UUID]bool
}

func (a *stubAggregate) SetStarred(_ context.Context, conversationID uuid.UUID, starred bool) error {
	if a.setStarredErr != nil {
		return a.setStarredErr
	}
	if a.starred == nil {
		a.starred = map[uuid.UUID]bool{}
	}
	a.starred[conversationID] = starred
	return nil
}

func (a *stubAggregate) SetMode(_ context.Context, conversationID uuid.UUID, mode enums.AgentMode) error {
	if a.setModeErr != nil {
		return a.setModeErr
	}
	if a.modes == nil {
		a.modes = map[uuid.UUID]enums.AgentMode{}
	}
	a.modes[conversationID] = mode
	return nil
}

func (a *stubAggregate) Claim(_ context.Context, conversationID, operatorID uuid.UUID) error {
	if a.claimErr != nil {
		return a.claimErr
	}
	if a.claimed == nil {
		a.claimed = map[uuid.UUID]uuid.UUID{}
	}
	a.claimed[conversationID] = operatorID
	return nil
}

func (a *stubAggregate) RecordOperatorOutbound(_ context.Context, conversationID, operatorID uuid.UUID, text string, at time.Time) (conversations.Message, error) {
	if a.recordErr != nil {
		return conversations.Message{}, a.recordErr
	}
	m := conversations.Message{
		ID: id.New(), ConversationID: conversationID,
		Direction: enums.MessageDirectionOutbound, SenderType: enums.SenderTypeOperator, SenderOperatorID: &operatorID,
		ContentText: text, ContentType: enums.MessageContentTypeText, SentAt: at,
	}
	a.recorded = append(a.recorded, m)
	return m, nil
}

func (a *stubAggregate) MarkSent(_ context.Context, messageID uuid.UUID, wamid string, _ time.Time) error {
	if a.sent == nil {
		a.sent = map[uuid.UUID]string{}
	}
	a.sent[messageID] = wamid
	return a.markSentErr
}

func (a *stubAggregate) MarkFailed(_ context.Context, messageID uuid.UUID, errorCode string) error {
	if a.failed == nil {
		a.failed = map[uuid.UUID]string{}
	}
	a.failed[messageID] = errorCode
	return a.markFailedErr
}

type stubChannel struct{ err error }

func (c *stubChannel) InboxByID(_ context.Context, inboxID uuid.UUID) (whatsapp.Inbox, error) {
	if c.err != nil {
		return whatsapp.Inbox{}, c.err
	}
	return whatsapp.Inbox{ID: inboxID, PhoneNumberID: "000000000000000", AccessToken: "token"}, nil
}

// stubSender records the context it was handed, because whether the request's cancellation reaches the send is a rule of its own.
type stubSender struct {
	wamid    string
	err      error
	readyErr error
	calls    int
	to       []string
	texts    []string
	ctxErr   error
}

func (s *stubSender) Ready(whatsapp.Inbox) error { return s.readyErr }

func (s *stubSender) Send(ctx context.Context, _ whatsapp.Inbox, to, text string) (string, error) {
	s.calls++
	s.to = append(s.to, to)
	s.texts = append(s.texts, text)
	s.ctxErr = ctx.Err()
	if s.err != nil {
		return "", s.err
	}
	return s.wamid, nil
}

// desk is the service with every collaborator doubled. The transaction is a plain call: what these cases check is the order and the
// presence of the writes, and binding them to one transaction is the pool's job, proven by the integration cases.
type desk struct {
	store     *stubStore
	aggregate *stubAggregate
	reviews   *stubReviews
	channel   *stubChannel
	sender    *stubSender
	service   *Service
}

func newDesk(store *stubStore) *desk {
	if store.targets == nil {
		store.targets = map[uuid.UUID]Target{}
	}
	d := &desk{store: store, aggregate: &stubAggregate{}, reviews: &stubReviews{}, channel: &stubChannel{}, sender: &stubSender{wamid: "wamid.double"}}
	d.service = NewService(Options{
		Repo:      store,
		Aggregate: d.aggregate,
		Tx: func(_ context.Context, fn func(Store, Aggregate, Reviews) error) error {
			return fn(d.store, d.aggregate, d.reviews)
		},
		Channel: d.channel,
		Sender:  d.sender,
		Logger:  slog.New(slog.DiscardHandler),
	})
	return d
}

// write adds n messages to a conversation one minute apart, oldest first, and returns them newest first: the order the thread answers.
func (d *desk) write(conversationID uuid.UUID, n int) []ThreadMessage {
	if d.store.threads == nil {
		d.store.threads = map[uuid.UUID][]ThreadMessage{}
	}
	start := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		d.store.threads[conversationID] = append(d.store.threads[conversationID], ThreadMessage{
			Message: Message{
				ID: id.New(), Direction: enums.MessageDirectionInbound, SenderType: enums.SenderTypeContact,
				ContentText: "mensaje", ContentType: enums.MessageContentTypeText, SentAt: start.Add(time.Duration(i) * time.Minute),
			},
			CreatedAt: start.Add(time.Duration(i) * time.Minute),
		})
	}
	written := d.store.threads[conversationID]
	out := make([]ThreadMessage, 0, len(written))
	for i := len(written) - 1; i >= 0; i-- {
		out = append(out, written[i])
	}
	return out
}

// open adds a conversation the window still admits, held by holder or in the pool when holder is nil.
func (d *desk) open(holder *uuid.UUID) Target {
	agent := id.New()
	t := Target{
		ConversationID: id.New(), InboxID: id.New(), ContactPhone: "5493510000000",
		OperatorID: holder, Status: enums.ConversationStatusActive, WindowExpiresAt: time.Now().Add(time.Hour),
		AIAgentID: &agent, Mode: enums.AgentModeAutonomous, AIAgentAvailable: true,
	}
	d.store.targets[t.ConversationID] = t
	return t
}
