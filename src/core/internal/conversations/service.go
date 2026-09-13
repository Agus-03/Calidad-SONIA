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

	// ErrTaken is the pool race lost: somebody else answered first and the conversation is theirs (ADR-0004).
	ErrTaken = errors.New("conversations: the conversation is already assigned to an operator")

	// ErrNoAgent refuses to turn on a bot that does not exist: a conversation born without an agent has nobody to answer it.
	ErrNoAgent = errors.New("conversations: the conversation has no agent to turn on")
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
	Claim(ctx context.Context, conversationID, operatorID uuid.UUID) error
	SetMode(ctx context.Context, conversationID uuid.UUID, mode enums.AgentMode) error
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
func (s *Service) EnsureOpen(ctx context.Context, contactID, inboxID uuid.UUID, aiAgentID *uuid.UUID, at time.Time) (Conversation, error) {
	existing, err := s.repo.OpenByContact(ctx, contactID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Conversation{}, err
	}

	mode := enums.AgentModeOff
	// Without an agent the conversation is born off: a mode naming a bot that does not exist would be read as one that is silent.
	if aiAgentID != nil {
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

// RecordOperatorOutbound is the same write-before-send for an answer a person typed: the sender check of messages wants the
// operator's id and no agent's, so the row names who wrote it and nothing else (H-09, ADR-0020).
func (s *Service) RecordOperatorOutbound(ctx context.Context, conversationID, operatorID uuid.UUID, text string, at time.Time) (Message, error) {
	return s.appendOutbound(ctx, Message{
		ID:               id.New(),
		ConversationID:   conversationID,
		Direction:        enums.MessageDirectionOutbound,
		SenderType:       enums.SenderTypeOperator,
		SenderOperatorID: &operatorID,
		ContentText:      text,
		ContentType:      enums.MessageContentTypeText,
		SentAt:           at,
	})
}

// appendOutbound moves last_activity_at with the row, for the agent and the operator alike: the inbox orders by that column, and
// a conversation somebody just answered would otherwise sink below the ones nobody touched since their last inbound message.
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
// did, and the caller is expected to hold it in the transaction that writes the answer, so losing the race writes nothing.
func (s *Service) Claim(ctx context.Context, conversationID, operatorID uuid.UUID) error {
	return s.repo.Claim(ctx, conversationID, operatorID)
}

// SetMode changes how the agent answers one conversation and leaves ai_agent_id alone: turning the bot off is a mode and not the
// loss of the agent, or it could never be turned back on (F27). Which modes a caller may ask for is the caller's rule.
func (s *Service) SetMode(ctx context.Context, conversationID uuid.UUID, mode enums.AgentMode) error {
	return s.repo.SetMode(ctx, conversationID, mode)
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
