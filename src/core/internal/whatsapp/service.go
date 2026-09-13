package whatsapp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/contacts"
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

// Meta names the algorithm in front of the hex digest, so a header without it is not a signature this code knows how to check.
const signaturePrefix = "sha256="

// Store is the slice of the repository the channel needs, declared here so a test needs no database. Exported because
// internal/app/deps.go must bind it to a transaction from outside the package, the same way turn.Store is (regla 5).
type Store interface {
	InboxByVerifyToken(ctx context.Context, token string) (Inbox, error)
	InboxByPhoneNumberID(ctx context.Context, phoneNumberID string) (Inbox, error)
	SaveWebhookEvent(ctx context.Context, event WebhookEvent) error
	MarkEventProcessed(ctx context.Context, eventID uuid.UUID, at time.Time) error
	ContactByPhone(ctx context.Context, accountID uuid.UUID, phone string) (Contact, error)
}

// Aggregate is what conversations/ exposes to the channel: whatsapp never writes those two tables itself (regla 1).
type Aggregate interface {
	EnsureOpen(ctx context.Context, contactID, inboxID uuid.UUID, aiAgentID *uuid.UUID, at time.Time) (conversations.Conversation, error)
	RecordInbound(ctx context.Context, c conversations.Conversation, wamid, text string, contentType enums.MessageContentType, at time.Time) (conversations.Message, error)
}

// Contacts is what contacts/ exposes to the channel: an unknown number gets a row through here and never through SQL of our
// own, the same pattern contacts/ itself uses to reach labels/ (struct.md regla 1).
type Contacts interface {
	Create(ctx context.Context, c contacts.Contact) error
}

// Assigner is what inbox/ exposes to the channel: a conversation nobody holds gets its vendor here, never through SQL of our own,
// because conversation_assignments is inbox's (struct.md regla 1; the assignment itself is ADR-0037).
type Assigner interface {
	Assign(ctx context.Context, accountID, contactID, conversationID uuid.UUID) error
}

// TxFunc runs fn in one transaction with the four collaborators bound to it (regla 5): the event, the contact it may create, the
// conversation it opens and the vendor it gives that conversation commit together or not at all.
type TxFunc func(ctx context.Context, fn func(Store, Aggregate, Contacts, Assigner) error) error

type Service struct {
	// Pool-bound: Verify and the inbox lookup that decides where a payload writes need no transaction of their own.
	repo Store
	tx   TxFunc
	log  *slog.Logger
}

func NewService(repo Store, tx TxFunc, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{repo: repo, tx: tx, log: log}
}

// Inbound is what the turn needs to start, and what the controller returns to after the 200 has gone out.
type Inbound struct {
	ConversationID uuid.UUID
	MessageID      uuid.UUID
	AIAgentID      *uuid.UUID
}

// Receive persists in the order 07-secuencia fixes: the event first, then the message, and only then does the caller answer Meta.
// signature is the header as it arrived: it is checked per inbox, because the key belongs to the number the payload names.
func (s *Service) Receive(ctx context.Context, body []byte, signature string, at time.Time) ([]Inbound, error) {
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
			inbound, err := s.receiveChange(ctx, change.Value, body, signature, at)
			if err != nil {
				return nil, err
			}
			started = append(started, inbound...)
		}
	}
	return started, nil
}

func (s *Service) receiveChange(ctx context.Context, value Value, body []byte, signature string, at time.Time) ([]Inbound, error) {
	inbox, err := s.repo.InboxByPhoneNumberID(ctx, value.Metadata.PhoneNumberID)
	if err != nil {
		return nil, err
	}

	signed := signedBy(body, signature, inbox.AppSecret)
	event := WebhookEvent{
		ID:             id.New(),
		InboxID:        inbox.ID,
		EventType:      eventTypeOf(value),
		WAMID:          firstWAMID(value),
		PayloadJSON:    body,
		SignatureValid: signed,
		Status:         enums.WebhookEventStatusReceived,
		ReceivedAt:     at,
	}
	// Kept and not refused: the row is the trace of whoever tried, and a 403 would leave signature_valid with no false to hold (F20).
	if !signed {
		event.Status = enums.WebhookEventStatusDiscarded
	}

	var started []Inbound
	// One transaction for the whole change: the event, any contact it creates and every message it stores are one unit (regla 5).
	// whatsapp/ is the one that conducts here, the same way turn/ conducts its own (07-secuencia).
	err = s.tx(ctx, func(store Store, agg Aggregate, registrar Contacts, assigner Assigner) error {
		if err := store.SaveWebhookEvent(ctx, event); err != nil {
			return err
		}
		if !signed {
			s.log.Warn("webhook discarded, the signature does not match", slog.String("inbox_id", inbox.ID.String()))
			return nil
		}

		for _, message := range value.Messages {
			inbound, err := s.recordMessage(ctx, store, agg, registrar, assigner, inbox, value, message, at)
			if errors.Is(err, conversations.ErrDuplicateWAMID) {
				// Meta redelivers what it did not hear a 200 for, and skipping is right: the turn already ran the first time (07-secuencia).
				s.log.Info("message already stored, redelivery ignored", slog.String("wamid", message.ID))
				continue
			}
			if err != nil {
				return err
			}
			started = append(started, inbound)
		}

		return store.MarkEventProcessed(ctx, event.ID, at)
	})
	if err != nil {
		return nil, err
	}
	return started, nil
}

func (s *Service) recordMessage(
	ctx context.Context, store Store, agg Aggregate, registrar Contacts, assigner Assigner, inbox Inbox, value Value, message MetaMessage,
	at time.Time,
) (Inbound, error) {
	contact, err := s.contactFor(ctx, store, registrar, inbox, value, message)
	if err != nil {
		return Inbound{}, err
	}

	sentAt := message.SentAt(at)
	conversation, err := agg.EnsureOpen(ctx, contact.ID, inbox.ID, inbox.DefaultAIAgentID, sentAt)
	if err != nil {
		return Inbound{}, err
	}
	stored, err := agg.RecordInbound(ctx, conversation, message.ID, textOf(message), contentTypeOf(message), sentAt)
	if err != nil {
		return Inbound{}, err
	}
	// Every conversation has a vendor from the start (ADR-0037). One that has none is new, or one that arrived when nobody could
	// receive it: either way the customer writing is when it gets one, and one already held is never moved from here. After the
	// message is stored, so a redelivery, which stores nothing, assigns nothing either (regla 3: the next time the customer writes).
	if conversation.OperatorID == nil {
		if err := assigner.Assign(ctx, inbox.AccountID, contact.ID, conversation.ID); err != nil {
			return Inbound{}, err
		}
	}

	return Inbound{ConversationID: conversation.ID, MessageID: stored.ID, AIAgentID: conversation.AIAgentID}, nil
}

// contactFor resolves the contact a message names, creating one the moment WhatsApp names a number nobody has seen: F15 needs
// an origin for every contact, sold or not (ADR-0001), and there is no ABM screen upstream of a webhook to have created it first.
func (s *Service) contactFor(ctx context.Context, store Store, registrar Contacts, inbox Inbox, value Value, message MetaMessage) (Contact, error) {
	found, err := store.ContactByPhone(ctx, inbox.AccountID, message.From)
	if err == nil {
		return found, nil
	}
	if !errors.Is(err, ErrContactNotFound) {
		return Contact{}, err
	}

	created := contacts.Contact{
		ID:            id.New(),
		AccountID:     inbox.AccountID,
		PhoneWhatsapp: message.From,
		// WhatsApp gives one display name and this package asks for no lastname: it stays blank until an operator splits it (F30).
		Name: contactNameOrPhone(profileNameFor(value, message.From), message.From),
		// Always these two, never read from Meta's referral object: it is the fixed default ADR-0034 chose, not an inference.
		Type:          enums.ContactTypeProspect,
		SourceChannel: enums.SourceChannelDirect,
	}
	if err := registrar.Create(ctx, created); err != nil {
		if errors.Is(err, contacts.ErrDuplicatePhone) {
			// Two deliveries raced to create the same first-time number inside their own transactions; the loser reads what the winner committed.
			return store.ContactByPhone(ctx, inbox.AccountID, message.From)
		}
		return Contact{}, fmt.Errorf("whatsapp: create the contact a message named: %w", err)
	}
	return Contact{ID: created.ID, AccountID: created.AccountID}, nil
}

// profileNameFor reads the display name Meta sends once per contact and matches it by wa_id: MetaMessage itself carries no name.
func profileNameFor(value Value, waID string) string {
	for _, c := range value.Contacts {
		if c.WaID == waID {
			return c.Profile.Name
		}
	}
	return ""
}

// contactNameOrPhone falls back to the number because name is NOT NULL: a business-only WhatsApp profile often carries none at all.
func contactNameOrPhone(name, phone string) string {
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		return trimmed
	}
	return phone
}

// signedBy is computed over the raw bytes, never over a re-encoding of the parsed payload: Meta signed what it sent, byte for byte.
// An empty secret signs nothing, because an HMAC keyed with nothing is one anybody can compute.
func signedBy(body []byte, header, secret string) bool {
	if secret == "" {
		return false
	}
	digest, found := strings.CutPrefix(header, signaturePrefix)
	if !found {
		return false
	}
	claimed, err := hex.DecodeString(digest)
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	// hmac.Equal and not bytes.Equal: a comparison that stops at the first wrong byte tells a forger how many it got right.
	return hmac.Equal(claimed, mac.Sum(nil))
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
