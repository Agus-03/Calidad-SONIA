package whatsapp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/contacts"
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

// The key the stub inbox signs with; TestSignedByMatchesADigestComputedOutsideGo pins it against a digest Go did not compute.
const testAppSecret = "sonia-test-app-secret"

// sign builds the header Meta sends, so the cases below can speak about a valid signature without restating how one is made.
func sign(body, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return signaturePrefix + hex.EncodeToString(mac.Sum(nil))
}

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

// stubContacts is the alta automática side: Create either records the row or answers what the test asked it to.
type stubContacts struct {
	created []contacts.Contact

	// dupPhone simulates the race of two deliveries creating the same first-time number: Create refuses it as a duplicate.
	dupPhone string
	// onDuplicate lets a case make the retried read succeed right when the duplicate is reported, as the winner's commit would.
	onDuplicate func()

	err error
}

func (c *stubContacts) Create(_ context.Context, contact contacts.Contact) error {
	if c.err != nil {
		return c.err
	}
	if contact.PhoneWhatsapp == c.dupPhone {
		if c.onDuplicate != nil {
			c.onDuplicate()
		}
		return contacts.ErrDuplicatePhone
	}
	c.created = append(c.created, contact)
	return nil
}

// channel builds the trio every case needs, using the values of src/core/seeds so the fixtures agree with the demo rows.
func channel() (*stubChannel, *stubAggregate, *stubContacts) {
	agent := uuid.New()
	return &stubChannel{
			verifyToken:   "sonia-demo-verify-token",
			phoneNumberID: "000000000000000",
			inbox:         Inbox{ID: uuid.New(), AccountID: uuid.New(), DefaultAIAgentID: &agent, PhoneNumberID: "000000000000000", AppSecret: testAppSecret},
			knownPhone:    "5493510000000",
			contactID:     uuid.New(),
		},
		&stubAggregate{conversationID: uuid.New()},
		&stubContacts{}
}

// service wires the trio into one Service, with the transaction as a plain call: what the tests check is behaviour and order,
// not that a real Postgres binds the three collaborators together (that lives in the integration tests), matching turn/'s stub.
func service(repo *stubChannel, agg *stubAggregate, cs *stubContacts, log *slog.Logger) *Service {
	return NewService(repo, func(_ context.Context, fn func(Store, Aggregate, Contacts) error) error {
		return fn(repo, agg, cs)
	}, log)
}
