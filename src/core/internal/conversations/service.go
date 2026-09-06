package conversations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

var (
	ErrNotFound       = errors.New("conversations: conversation not found")
	ErrAlreadyOpen    = errors.New("conversations: the contact already has an open conversation")
	ErrDuplicateWAMID = errors.New("conversations: the channel id is already stored")
)

// ServiceWindow is Meta's, not ours: outside it a free form answer is refused and only a template may be sent.
const ServiceWindow = 24 * time.Hour

// store is the slice of the repository the service uses, declared here so the caller side owns the interface.
type store interface {
	OpenByContact(ctx context.Context, contactID uuid.UUID) (Conversation, error)
	Create(ctx context.Context, c Conversation) error
	AppendMessage(ctx context.Context, m Message) error
	TouchInbound(ctx context.Context, conversationID uuid.UUID, at time.Time) error
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
func (s *Service) DraftOutbound(c Conversation, aiAgentID uuid.UUID, text string, at time.Time) Message {
	return Message{
		ID:              id.New(),
		ConversationID:  c.ID,
		Direction:       enums.MessageDirectionOutbound,
		SenderType:      enums.SenderTypeAiAgent,
		SenderAIAgentID: &aiAgentID,
		ContentText:     text,
		ContentType:     enums.MessageContentTypeText,
		SentAt:          at,
	}
}
