package agents

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/brainclient"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// stubRepo answers from memory and plays both halves of the repository, so the service is tested against its own rules and not
// against the SQL that enforces them.
type stubRepo struct {
	rows    []AIAgent
	removed map[uuid.UUID]bool
	audits  []AuditEntry
	// Every write in the order it happened, which is how a case sees that a no-op save wrote nothing.
	writes []string
	// Set to make a write fail, which is how a test sees the transaction give up before the audit.
	err error
	// Runs as the lock is taken, which is how a case plays a save that committed between the first read and the lock.
	beforeLock func(*stubRepo)
}

func newStubRepo(rows ...AIAgent) *stubRepo {
	return &stubRepo{rows: rows, removed: map[uuid.UUID]bool{}}
}

func (s *stubRepo) find(accountID, agentID uuid.UUID) (int, bool) {
	for i, a := range s.rows {
		if a.ID == agentID && a.AccountID == accountID && !s.removed[a.ID] {
			return i, true
		}
	}
	return 0, false
}

func (s *stubRepo) List(_ context.Context, accountID uuid.UUID) ([]AIAgent, error) {
	out := []AIAgent{}
	for _, a := range s.rows {
		if a.AccountID == accountID && !s.removed[a.ID] {
			out = append(out, a)
		}
	}
	return out, nil
}

func (s *stubRepo) ByID(_ context.Context, accountID, agentID uuid.UUID) (AIAgent, error) {
	i, ok := s.find(accountID, agentID)
	if !ok {
		return AIAgent{}, ErrNotFound
	}
	return s.rows[i], nil
}

func (s *stubRepo) ByIDForUpdate(ctx context.Context, accountID, agentID uuid.UUID) (AIAgent, error) {
	if s.beforeLock != nil {
		s.beforeLock(s)
	}
	return s.ByID(ctx, accountID, agentID)
}

func (s *stubRepo) Create(_ context.Context, a AIAgent) (AIAgent, error) {
	if s.err != nil {
		return AIAgent{}, s.err
	}
	a.Active, a.CreatedAt, a.UpdatedAt = true, time.Now().UTC(), time.Now().UTC()
	s.rows = append(s.rows, a)
	s.writes = append(s.writes, "create")
	return a, nil
}

func (s *stubRepo) Update(_ context.Context, accountID, agentID uuid.UUID, c Config) (AIAgent, error) {
	if s.err != nil {
		return AIAgent{}, s.err
	}
	i, ok := s.find(accountID, agentID)
	if !ok {
		return AIAgent{}, ErrNotFound
	}
	a := &s.rows[i]
	a.Name, a.Description, a.LLMProvider, a.LLMModel = c.Name, c.Description, c.LLMProvider, c.LLMModel
	a.Temperature, a.MaxTokens, a.Active = c.Temperature, c.MaxTokens, c.Active
	s.writes = append(s.writes, "update")
	return *a, nil
}

func (s *stubRepo) SoftDelete(_ context.Context, accountID, agentID uuid.UUID) error {
	if s.err != nil {
		return s.err
	}
	if _, ok := s.find(accountID, agentID); !ok {
		return ErrNotFound
	}
	s.removed[agentID] = true
	s.writes = append(s.writes, "delete")
	return nil
}

func (s *stubRepo) RecordAudit(_ context.Context, e AuditEntry) error {
	s.audits = append(s.audits, e)
	s.writes = append(s.writes, "audit")
	return nil
}

// lastChange decodes the newest audit row, which is what a case asserts on: the before and after the administrator changed.
func (s *stubRepo) lastChange() (AuditEntry, change) {
	e := s.audits[len(s.audits)-1]
	var ch change
	_ = json.Unmarshal(e.Changes, &ch)
	return e, ch
}

type stubInboxes struct {
	cleared []uuid.UUID
	asked   []uuid.UUID
}

func (s *stubInboxes) ClearDefaultAIAgent(_ context.Context, _, aiAgentID uuid.UUID) ([]uuid.UUID, error) {
	s.asked = append(s.asked, aiAgentID)
	return s.cleared, nil
}

// stubConversations records which agents had their bots turned off and answers the conversations a case says it turned off.
type stubConversations struct {
	turnedOff []uuid.UUID
	asked     []uuid.UUID
	err       error
}

func (s *stubConversations) TurnOffAIAgent(_ context.Context, aiAgentID uuid.UUID) ([]uuid.UUID, error) {
	s.asked = append(s.asked, aiAgentID)
	return s.turnedOff, s.err
}

type stubCatalogue struct {
	models []brainclient.PricedModel
	err    error
	calls  int
}

func (s *stubCatalogue) Models(context.Context) (brainclient.ModelsResponse, error) {
	s.calls++
	if s.err != nil {
		return brainclient.ModelsResponse{}, s.err
	}
	return brainclient.ModelsResponse{PricingVersion: 1, Models: s.models}, nil
}

// priced is the tariff file of the demo, so a case reads the same pair the seed agent runs on.
func priced() *stubCatalogue {
	return &stubCatalogue{models: []brainclient.PricedModel{
		{Provider: "cohere", Model: "command-a-03-2025"},
		{Provider: "anthropic", Model: "claude-sonnet-4-5"},
	}}
}

// The transaction is a plain call here: what the test checks is which writes happen, and binding them is the pool's job.
func serviceOver(repo *stubRepo, inboxes *stubInboxes, catalogue *stubCatalogue) *Service {
	return serviceWith(repo, inboxes, &stubConversations{}, catalogue)
}

// serviceWith is serviceOver with the conversations in the case's hands, for the cases about the bots a pause turns off.
func serviceWith(repo *stubRepo, inboxes *stubInboxes, conversations *stubConversations, catalogue *stubCatalogue) *Service {
	return NewService(Options{
		Repo: repo,
		Tx: func(_ context.Context, fn func(Store, Inboxes, Conversations) error) error {
			return fn(repo, inboxes, conversations)
		},
		Catalogue: catalogue,
	})
}

var (
	testAccount  = id.New()
	testOperator = id.New()
)

func seeded(name string) AIAgent {
	return AIAgent{
		ID: id.New(), AccountID: testAccount, Name: name, LLMProvider: "cohere", LLMModel: "command-a-03-2025", Active: true,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
}
