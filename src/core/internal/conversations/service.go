package conversations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

var (
	ErrNotFound       = errors.New("conversations: conversation not found")
	ErrAlreadyOpen    = errors.New("conversations: the contact already has an open conversation")
	ErrDuplicateWAMID = errors.New("conversations: the channel id is already stored")

	// ErrTaken is the pool race lost: somebody else answered first and the conversation is theirs (ADR-0004, ADR-0035 regla 8).
	ErrTaken = errors.New("conversations: the conversation is already assigned to an operator")

	// ErrNoAIAgent refuses to turn on a bot that cannot answer: a conversation born without an agent, or whose agent was paused or
	// removed, has nobody to answer it. The caller, which read the conversation first, tells the two apart (ADR-0035 regla 12, ADR-0038).
	ErrNoAIAgent = errors.New("conversations: the conversation has no agent that can answer")

	// ErrResolved is a close that found the conversation already closed: a resolved one never reopens (ADR-0002 (b)).
	ErrResolved = errors.New("conversations: the conversation is already resolved")

	// ErrTurnInProgress is a close refused while the bot still owes an answer: the end of that turn would reopen it (ADR-0054).
	ErrTurnInProgress = errors.New("conversations: a turn of the conversation is in flight")
)

// ServiceWindow is Meta's, not ours: outside it a free form answer is refused and only a template may be sent.
const ServiceWindow = 24 * time.Hour

// store is the slice of the repository the service uses, declared here so the caller side owns the interface.
type store interface {
	OpenByContact(ctx context.Context, contactID uuid.UUID) (Conversation, error)
	Create(ctx context.Context, c Conversation) error
	AppendMessage(ctx context.Context, m Message) error
	TouchInbound(ctx context.Context, conversationID uuid.UUID, at time.Time) error
	TouchOutbound(ctx context.Context, conversationID uuid.UUID) error
	MarkWaiting(ctx context.Context, conversationID uuid.UUID) error
	Claim(ctx context.Context, conversationID, operatorID uuid.UUID) error
	SetMode(ctx context.Context, conversationID uuid.UUID, mode enums.AgentMode) error
	SetStarred(ctx context.Context, conversationID uuid.UUID, starred bool) error
	Resolve(ctx context.Context, conversationID, operatorID uuid.UUID, turnBudget time.Duration) error
	Score(ctx context.Context, conversationID, operatorID uuid.UUID, score int) error
	MarkRead(ctx context.Context, conversationID uuid.UUID) error
	TurnOffAIAgent(ctx context.Context, aiAgentID uuid.UUID) ([]uuid.UUID, error)
	LinkToBatch(ctx context.Context, messageID, batchID uuid.UUID) error
	MarkSent(ctx context.Context, messageID uuid.UUID, wamid string, sentAt time.Time) error
	MarkFailed(ctx context.Context, messageID uuid.UUID, errorCode string) error
}

type Service struct {
	repo store
	log  *slog.Logger
}

func NewService(repo store, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{repo: repo, log: log}
}

// EnsureOpen resolves the conversation an inbound message belongs to, opening one when ADR-0002 says the previous is closed.
//
// canAnswer is whether that agent is active and not removed, read by the caller inside its transaction. An agent that cannot answer
// is still named, so the vendor can turn the bot on once it is resumed, but the conversation is born off: born on, its switch would
// say the bot answers while the turn skips it (ADR-0038).
func (s *Service) EnsureOpen(
	ctx context.Context, contactID, inboxID uuid.UUID, aiAgentID *uuid.UUID, canAnswer bool, at time.Time,
) (Conversation, error) {
	existing, err := s.repo.OpenByContact(ctx, contactID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Conversation{}, err
	}

	mode := enums.AgentModeOff
	// Without an agent the conversation is born off: a mode naming a bot that does not exist would be read as one that is silent.
	if aiAgentID != nil && canAnswer {
		mode = enums.AgentModeAutonomous
	}

	opened := Conversation{
		ID:              id.New(),
		ContactID:       contactID,
		InboxID:         inboxID,
		AIAgentID:       aiAgentID,
		Status:          enums.ConversationStatusActive,
		Mode:            mode,
		LastInboundAt:   at,
		WindowExpiresAt: at.Add(ServiceWindow),
	}

	switch err := s.repo.Create(ctx, opened); {
	case err == nil:
		return opened, nil
	case errors.Is(err, ErrAlreadyOpen):
		// Another inbound message opened it between the read and the insert, so the index decided and this one reads the winner.
		return s.repo.OpenByContact(ctx, contactID)
	default:
		return Conversation{}, err
	}
}

// RecordInbound stores what the contact sent and resets the window, in that order, so a crash never loses the message.
func (s *Service) RecordInbound(ctx context.Context, c Conversation, wamid, text string, contentType enums.MessageContentType, at time.Time) (Message, error) {
	message := Message{
		ID:             id.New(),
		ConversationID: c.ID,
		WAMID:          &wamid,
		Direction:      enums.MessageDirectionInbound,
		SenderType:     enums.SenderTypeContact,
		ContentText:    text,
		ContentType:    contentType,
		SentAt:         at,
	}

	if err := s.repo.AppendMessage(ctx, message); err != nil {
		return Message{}, err
	}
	if err := s.repo.TouchInbound(ctx, c.ID, at); err != nil {
		return Message{}, fmt.Errorf("conversations: record inbound: %w", err)
	}
	return message, nil
}

// DraftOutbound builds the row the turn writes before sending: no wamid and no status, because Meta has not answered yet.
func (s *Service) DraftOutbound(conversationID, aiAgentID uuid.UUID, text string, at time.Time) Message {
	return Message{
		ID:              id.New(),
		ConversationID:  conversationID,
		Direction:       enums.MessageDirectionOutbound,
		SenderType:      enums.SenderTypeAiAgent,
		SenderAIAgentID: &aiAgentID,
		ContentText:     text,
		ContentType:     enums.MessageContentTypeText,
		SentAt:          at,
	}
}

// RecordOutbound writes the answer before it leaves, so a send that fails leaves a visible row and never a hole (07-secuencia).
func (s *Service) RecordOutbound(ctx context.Context, conversationID, aiAgentID uuid.UUID, text string, at time.Time) (Message, error) {
	return s.appendOutbound(ctx, s.DraftOutbound(conversationID, aiAgentID, text, at))
}

// RecordSystemOutbound is the same write-before-send for the fixed text of a turn the pipeline cut (decision O2). No model wrote
// it and no person typed it, so the row names neither sender, which is the third arm of the sender check of messages (H-09): it is
// the system talking, and reading it later as an answer of the AIAgent would put words in a model's mouth that it never produced.
func (s *Service) RecordSystemOutbound(ctx context.Context, conversationID uuid.UUID, text string, at time.Time) (Message, error) {
	return s.appendOutbound(ctx, Message{
		ID:             id.New(),
		ConversationID: conversationID,
		Direction:      enums.MessageDirectionOutbound,
		SenderType:     enums.SenderTypeSystem,
		ContentText:    text,
		ContentType:    enums.MessageContentTypeText,
		SentAt:         at,
	})
}

// RecordOperatorOutbound is the same write-before-send for an answer a person typed: the sender check of messages wants the
// operator's id and no agent's, so the row names who wrote it and nothing else (H-09, ADR-0020).
//
// It also marks the conversation read, in the caller's transaction: whoever answered read what they answered (ADR-0052).
func (s *Service) RecordOperatorOutbound(ctx context.Context, conversationID, operatorID uuid.UUID, text string, at time.Time) (Message, error) {
	message, err := s.appendOutbound(ctx, Message{
		ID:               id.New(),
		ConversationID:   conversationID,
		Direction:        enums.MessageDirectionOutbound,
		SenderType:       enums.SenderTypeOperator,
		SenderOperatorID: &operatorID,
		ContentText:      text,
		ContentType:      enums.MessageContentTypeText,
		SentAt:           at,
	})
	if err != nil {
		return Message{}, err
	}
	if err := s.repo.MarkRead(ctx, conversationID); err != nil {
		return Message{}, fmt.Errorf("conversations: record operator outbound: %w", err)
	}
	return message, nil
}

// appendOutbound moves last_activity_at with the row, for the agent and the operator alike: the inbox orders by that column, and
// a conversation somebody just answered would otherwise sink below the ones nobody touched since their last inbound message
// (ADR-0036, regla 2).
func (s *Service) appendOutbound(ctx context.Context, message Message) (Message, error) {
	if err := s.repo.AppendMessage(ctx, message); err != nil {
		return Message{}, fmt.Errorf("conversations: record outbound: %w", err)
	}
	if err := s.repo.TouchOutbound(ctx, message.ConversationID); err != nil {
		return Message{}, fmt.Errorf("conversations: record outbound: %w", err)
	}
	return message, nil
}

// Claim takes a conversation out of the pool for whoever answers it first (ADR-0004). It returns ErrTaken when somebody already
// did, and the caller is expected to hold it in the transaction that writes the answer, so losing the race writes nothing
// (ADR-0035, reglas 7 y 8).
func (s *Service) Claim(ctx context.Context, conversationID, operatorID uuid.UUID) error {
	return s.repo.Claim(ctx, conversationID, operatorID)
}

// SetMode changes how the agent answers one conversation and leaves ai_agent_id alone: turning the bot off is a mode and not the
// loss of the agent, or it could never be turned back on (F27, ADR-0035 regla 12). Which modes a caller may ask for is the caller's rule.
func (s *Service) SetMode(ctx context.Context, conversationID uuid.UUID, mode enums.AgentMode) error {
	return s.repo.SetMode(ctx, conversationID, mode)
}

// MarkRead clears the unread count of F23: the column is this aggregate's, and who may clear it is the caller's rule (ADR-0052).
func (s *Service) MarkRead(ctx context.Context, conversationID uuid.UUID) error {
	return s.repo.MarkRead(ctx, conversationID)
}

// SetStarred is how the inbox marks a conversation for follow-up: the flag is this aggregate's column (F25, regla 1). Who may star
// which conversation is the caller's rule.
func (s *Service) SetStarred(ctx context.Context, conversationID uuid.UUID, starred bool) error {
	return s.repo.SetStarred(ctx, conversationID, starred)
}

// Resolve is the manual close of ADR-0002 (d); the status is this aggregate's column, and who may close is the caller's rule (ADR-0054).
func (s *Service) Resolve(ctx context.Context, conversationID, operatorID uuid.UUID, turnBudget time.Duration) error {
	return s.repo.Resolve(ctx, conversationID, operatorID, turnBudget)
}

// Score is F13 written by its owner: the score is this aggregate's column, and who may score is the caller's rule (ADR-0055).
func (s *Service) Score(ctx context.Context, conversationID, operatorID uuid.UUID, score int) error {
	return s.repo.Score(ctx, conversationID, operatorID, score)
}

// TurnOffAIAgent is how agents/ turns off the bots of an agent it paused or removed: the mode is this aggregate's column (regla 1),
// and whoever conducts the pause holds the transaction (ADR-0038).
func (s *Service) TurnOffAIAgent(ctx context.Context, aiAgentID uuid.UUID) ([]uuid.UUID, error) {
	return s.repo.TurnOffAIAgent(ctx, aiAgentID)
}

// MarkWaiting is where a turn that produced no answer leaves the conversation, and turn/ asks for it through here because the
// status is this aggregate's column (regla 1). Whoever conducts the turn holds the transaction (07-secuencia secciones 4 y 5).
func (s *Service) MarkWaiting(ctx context.Context, conversationID uuid.UUID) error {
	return s.repo.MarkWaiting(ctx, conversationID)
}

// LinkToBatch, MarkSent and MarkFailed pass through: they are the aggregate's API and turn/ must not reach the table itself.
func (s *Service) LinkToBatch(ctx context.Context, messageID, batchID uuid.UUID) error {
	return s.repo.LinkToBatch(ctx, messageID, batchID)
}

func (s *Service) MarkSent(ctx context.Context, messageID uuid.UUID, wamid string, at time.Time) error {
	return s.repo.MarkSent(ctx, messageID, wamid, at)
}

func (s *Service) MarkFailed(ctx context.Context, messageID uuid.UUID, errorCode string) error {
	return s.repo.MarkFailed(ctx, messageID, errorCode)
}

// WithExecutor returns the same service bound to a transaction, so the turn that conducts can carry it across packages (regla 5).
func (s *Service) WithExecutor(exec db.Executor) *Service {
	repo, ok := s.repo.(*Repository)
	if !ok {
		// A doubled store has no transaction to bind to, and a test that conducts one is testing the pool and not this.
		return s
	}
	return &Service{repo: repo.WithExecutor(exec), log: s.log}
}
