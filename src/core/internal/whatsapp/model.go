// Package whatsapp is the channel: what Meta calls and what answers it (F20, F29).
package whatsapp

import (
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// Inbox is one connected WhatsApp number, owned here and not by inbox/, which is the operator's tray (F05).
type Inbox struct {
	ID               uuid.UUID
	AccountID        uuid.UUID
	DefaultAIAgentID *uuid.UUID
	PhoneNumberID    string
	VerifyToken      string
}

// WebhookEvent is the log F20 asks for, written before anything is interpreted so an unparseable payload still leaves a trace.
type WebhookEvent struct {
	ID             uuid.UUID
	InboxID        uuid.UUID
	EventType      enums.WebhookEventType
	WAMID          *string
	PayloadJSON    []byte
	SignatureValid bool
	Status         enums.WebhookEventStatus
	ReceivedAt     time.Time
}

// Contact is read and never written here: creating one from a message is Fase 4, and the ABM belongs to contacts/.
type Contact struct {
	ID        uuid.UUID
	AccountID uuid.UUID
}
