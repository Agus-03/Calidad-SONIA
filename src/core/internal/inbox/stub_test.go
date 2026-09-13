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
		if after != nil && !before(Summary{LastActivityAt: after.LastActivityAt, ID: after.ID}, row) {
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

func (s *stubStore) RecordAssignment(_ context.Context, a Assignment) error {
	if s.assignErr != nil {
		return s.assignErr
	}
	s.assignments = append(s.assignments, a)
	return nil
}

// stubAggregate is conversations/ seen through the four calls the inbox makes, recording each so a case can assert what was
// written and, as often, what was not.
type stubAggregate struct {
	claimErr      error
	recordErr     error
	markSentErr   error
	markFailedErr error
	setModeErr    error

	claimed  map[uuid.UUID]uuid.UUID
	recorded []conversations.Message
	sent     map[uuid.UUID]string
	failed   map[uuid.UUID]string
	modes    map[uuid.UUID]enums.AgentMode
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
	wamid  string
	err    error
	calls  int
	to     []string
	texts  []string
	ctxErr error
}

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
	channel   *stubChannel
	sender    *stubSender
	service   *Service
}

func newDesk(store *stubStore) *desk {
	if store.targets == nil {
		store.targets = map[uuid.UUID]Target{}
	}
	d := &desk{store: store, aggregate: &stubAggregate{}, channel: &stubChannel{}, sender: &stubSender{wamid: "wamid.double"}}
	d.service = NewService(Options{
		Repo:      store,
		Aggregate: d.aggregate,
		Tx: func(_ context.Context, fn func(Store, Aggregate) error) error {
			return fn(d.store, d.aggregate)
		},
		Channel: d.channel,
		Sender:  d.sender,
		Logger:  slog.New(slog.DiscardHandler),
	})
	return d
}

// open adds a conversation the window still admits, held by holder or in the pool when holder is nil.
func (d *desk) open(holder *uuid.UUID) Target {
	agent := id.New()
	t := Target{
		ConversationID: id.New(), InboxID: id.New(), ContactPhone: "5493510000000",
		OperatorID: holder, Status: enums.ConversationStatusActive, WindowExpiresAt: time.Now().Add(time.Hour),
		AIAgentID: &agent, Mode: enums.AgentModeAutonomous,
	}
	d.store.targets[t.ConversationID] = t
	return t
}
