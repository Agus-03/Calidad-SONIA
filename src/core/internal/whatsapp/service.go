package whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/conversations"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

var (
	ErrInboxNotFound        = errors.New("whatsapp: inbox not found")
	ErrContactNotFound      = errors.New("whatsapp: contact not found")
	ErrVerificationRejected = errors.New("whatsapp: verification rejected")
	ErrMalformedPayload     = errors.New("whatsapp: payload is not a meta webhook")
)

// The literal Meta sends; it is not an enumeration of ours, so it does not belong in contracts/enums.yaml.
const subscribeMode = "subscribe"

// inboxFinder is declared by the consumer and not by the repository, so the handshake can be tested without a database.
type inboxFinder interface {
	InboxByVerifyToken(ctx context.Context, token string) (Inbox, error)
}

// channelStore is the slice of the repository the channel needs, declared here so a test needs no database.
type channelStore interface {
	inboxFinder
	InboxByPhoneNumberID(ctx context.Context, phoneNumberID string) (Inbox, error)
	SaveWebhookEvent(ctx context.Context, event WebhookEvent) error
	MarkEventProcessed(ctx context.Context, eventID uuid.UUID, at time.Time) error
	ContactByPhone(ctx context.Context, accountID uuid.UUID, phone string) (Contact, error)
}

// aggregate is what conversations/ exposes to the channel: whatsapp never writes those two tables itself (regla 1).
type aggregate interface {
	EnsureOpen(ctx context.Context, contactID, inboxID uuid.UUID, aiAgentID *uuid.UUID, at time.Time) (conversations.Conversation, error)
	RecordInbound(ctx context.Context, c conversations.Conversation, wamid, text string, contentType enums.MessageContentType, at time.Time) (conversations.Message, error)
}

type Service struct {
	repo          channelStore
	conversations aggregate
	log           *slog.Logger
}

func NewService(repo channelStore, agg aggregate, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{repo: repo, conversations: agg, log: log}
}

// Inbound is what the turn needs to start, and what the controller returns to after the 200 has gone out.
type Inbound struct {
	ConversationID uuid.UUID
	MessageID      uuid.UUID
	AIAgentID      *uuid.UUID
}

// Receive persists in the order 07-secuencia fixes: the event first, then the message, and only then does the caller answer Meta.
func (s *Service) Receive(ctx context.Context, body []byte, at time.Time) ([]Inbound, error) {
	var payload Payload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, ErrMalformedPayload
	}

	var started []Inbound
	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			// Only the messages field matters here; account_update and the rest are other webhooks of the same app.
			if change.Field != "messages" {
				continue
			}
			inbound, err := s.receiveChange(ctx, change.Value, body, at)
			if err != nil {
				return nil, err
			}
			started = append(started, inbound...)
		}
	}
	return started, nil
}

func (s *Service) receiveChange(ctx context.Context, value Value, body []byte, at time.Time) ([]Inbound, error) {
	inbox, err := s.repo.InboxByPhoneNumberID(ctx, value.Metadata.PhoneNumberID)
	if err != nil {
		return nil, err
	}

	event := WebhookEvent{
		ID:        id.New(),
		InboxID:   inbox.ID,
		EventType: eventTypeOf(value),
		WAMID:     firstWAMID(value),
		// False and not true: Fase 1 does not check the signature, so no valid one was ever established (F20 completes this).
		PayloadJSON:    body,
		SignatureValid: false,
		Status:         enums.WebhookEventStatusReceived,
		ReceivedAt:     at,
	}
	if err := s.repo.SaveWebhookEvent(ctx, event); err != nil {
		return nil, err
	}

	var started []Inbound
	for _, message := range value.Messages {
		inbound, err := s.recordMessage(ctx, inbox, message, at)
		if errors.Is(err, ErrContactNotFound) {
			// Creating a contact from a message is Fase 4; until then an unknown number is logged and dropped, never invented.
			s.log.Warn("message from an unknown contact", slog.String("wamid", message.ID))
			continue
		}
		if errors.Is(err, conversations.ErrDuplicateWAMID) {
			// Meta redelivers what it did not hear a 200 for, and 07-secuencia leans on the wamid to cover exactly this.
			// It is skipped and not answered again: the turn already ran for this message the first time it arrived.
			s.log.Info("message already stored, redelivery ignored", slog.String("wamid", message.ID))
			continue
		}
		if err != nil {
			return nil, err
		}
		started = append(started, inbound)
	}

	if err := s.repo.MarkEventProcessed(ctx, event.ID, at); err != nil {
		return nil, err
	}
	return started, nil
}

func (s *Service) recordMessage(ctx context.Context, inbox Inbox, message MetaMessage, at time.Time) (Inbound, error) {
	contact, err := s.repo.ContactByPhone(ctx, inbox.AccountID, message.From)
	if err != nil {
		return Inbound{}, err
	}

	sentAt := message.SentAt(at)
	conversation, err := s.conversations.EnsureOpen(ctx, contact.ID, inbox.ID, inbox.DefaultAIAgentID, sentAt)
	if err != nil {
		return Inbound{}, err
	}

	stored, err := s.conversations.RecordInbound(ctx, conversation, message.ID, textOf(message), contentTypeOf(message), sentAt)
	if err != nil {
		return Inbound{}, err
	}

	return Inbound{ConversationID: conversation.ID, MessageID: stored.ID, AIAgentID: conversation.AIAgentID}, nil
}

// A payload with neither messages nor statuses is still logged: unknown is a value of the enumeration and not an error.
func eventTypeOf(value Value) enums.WebhookEventType {
	switch {
	case len(value.Messages) > 0:
		return enums.WebhookEventTypeMessage
	case len(value.Statuses) > 0:
		return enums.WebhookEventTypeStatus
	default:
		return enums.WebhookEventTypeUnknown
	}
}

func firstWAMID(value Value) *string {
	switch {
	case len(value.Messages) > 0:
		return &value.Messages[0].ID
	case len(value.Statuses) > 0:
		return &value.Statuses[0].ID
	default:
		return nil
	}
}

// Audio and image carry no body, and content_text is NOT NULL, so the type is what says there is more than text.
func textOf(message MetaMessage) string {
	if message.Text != nil {
		return message.Text.Body
	}
	return ""
}

func contentTypeOf(message MetaMessage) enums.MessageContentType {
	switch message.Type {
	case string(enums.MessageContentTypeAudio):
		return enums.MessageContentTypeAudio
	case string(enums.MessageContentTypeImage):
		return enums.MessageContentTypeImage
	default:
		return enums.MessageContentTypeText
	}
}

// Verify answers the subscription handshake, without which Meta never delivers a single message to the webhook.
func (s *Service) Verify(ctx context.Context, mode, token string) error {
	if mode != subscribeMode {
		return ErrVerificationRejected
	}

	inbox, err := s.repo.InboxByVerifyToken(ctx, token)
	if err != nil {
		// A token that matches nothing and a caller that is not Meta are the same event, and neither may say which it was.
		if errors.Is(err, ErrInboxNotFound) {
			return ErrVerificationRejected
		}
		return err
	}

	s.log.Info("webhook verified", slog.String("inbox_id", inbox.ID.String()))
	return nil
}
