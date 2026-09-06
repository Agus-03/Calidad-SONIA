package whatsapp

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/conversations"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// The payload Meta really sends, kept verbatim so the parsing is tested against the shape and not against our idea of it.
const metaTextPayload = `{
  "object": "whatsapp_business_account",
  "entry": [{
    "id": "102290129340398",
    "changes": [{
      "field": "messages",
      "value": {
        "messaging_product": "whatsapp",
        "metadata": {"display_phone_number": "15550000000", "phone_number_id": "000000000000000"},
        "contacts": [{"profile": {"name": "Mateo"}, "wa_id": "5493510000000"}],
        "messages": [{
          "from": "5493510000000",
          "id": "wamid.HBgNNTQ5MzUxMDAwMDAwMBUCABIYFjNFQjA=",
          "timestamp": "1772841600",
          "type": "text",
          "text": {"body": "hola, queria consultar por los precios"}
        }]
      }
    }]
  }]
}`

type stubChannel struct {
	verifyToken   string
	phoneNumberID string
	inbox         Inbox
	knownPhone    string
	contactID     uuid.UUID
	lookupErr     error

	events    []WebhookEvent
	processed []uuid.UUID
}

func (s *stubChannel) InboxByVerifyToken(_ context.Context, token string) (Inbox, error) {
	if s.lookupErr != nil {
		return Inbox{}, s.lookupErr
	}
	if token != s.verifyToken {
		return Inbox{}, ErrInboxNotFound
	}
	return s.inbox, nil
}

func (s *stubChannel) InboxByPhoneNumberID(_ context.Context, phoneNumberID string) (Inbox, error) {
	if phoneNumberID != s.phoneNumberID {
		return Inbox{}, ErrInboxNotFound
	}
	return s.inbox, nil
}

func (s *stubChannel) SaveWebhookEvent(_ context.Context, event WebhookEvent) error {
	s.events = append(s.events, event)
	return nil
}

func (s *stubChannel) MarkEventProcessed(_ context.Context, eventID uuid.UUID, _ time.Time) error {
	s.processed = append(s.processed, eventID)
	return nil
}

func (s *stubChannel) ContactByPhone(_ context.Context, _ uuid.UUID, phone string) (Contact, error) {
	if phone != s.knownPhone {
		return Contact{}, ErrContactNotFound
	}
	return Contact{ID: s.contactID}, nil
}

type stubAggregate struct {
	conversationID uuid.UUID
	messages       []string
	duplicate      bool
}

func (s *stubAggregate) EnsureOpen(_ context.Context, contactID, inboxID uuid.UUID, agentID *uuid.UUID, _ time.Time) (conversations.Conversation, error) {
	return conversations.Conversation{ID: s.conversationID, ContactID: contactID, InboxID: inboxID, AIAgentID: agentID}, nil
}

func (s *stubAggregate) RecordInbound(_ context.Context, c conversations.Conversation, wamid, text string, _ enums.MessageContentType, _ time.Time) (conversations.Message, error) {
	if s.duplicate {
		return conversations.Message{}, conversations.ErrDuplicateWAMID
	}
	s.messages = append(s.messages, text)
	return conversations.Message{ID: uuid.New(), ConversationID: c.ID, WAMID: &wamid}, nil
}

// channel builds the pair every case needs, using the values of src/core/seeds so the fixtures agree with the demo rows.
func channel() (*stubChannel, *stubAggregate) {
	agent := uuid.New()
	return &stubChannel{
		verifyToken:   "sonia-demo-verify-token",
		phoneNumberID: "000000000000000",
		inbox:         Inbox{ID: uuid.New(), AccountID: uuid.New(), DefaultAIAgentID: &agent, PhoneNumberID: "000000000000000"},
		knownPhone:    "5493510000000",
		contactID:     uuid.New(),
	}, &stubAggregate{conversationID: uuid.New()}
}
