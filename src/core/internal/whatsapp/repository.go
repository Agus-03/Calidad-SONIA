package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// Repository takes the Executor and not the pool, so the turn can later reach these tables inside its transaction (regla 5).
type Repository struct{ exec db.Executor }

func NewRepository(exec db.Executor) *Repository { return &Repository{exec: exec} }

const inboxSelect = `
SELECT id, account_id, default_ai_agent_id, phone_number_id, verify_token, access_token_encrypted
FROM   inboxes
WHERE  deleted_at IS NULL`

// InboxByVerifyToken is how the handshake resolves: Meta sends the token and nothing else that names the number.
func (r *Repository) InboxByVerifyToken(ctx context.Context, token string) (Inbox, error) {
	return scanInbox(r.exec.QueryRow(ctx, inboxSelect+" AND verify_token = $1", token))
}

func scanInbox(row pgx.Row) (Inbox, error) {
	var inbox Inbox
	var accessToken []byte
	err := row.Scan(&inbox.ID, &inbox.AccountID, &inbox.DefaultAIAgentID, &inbox.PhoneNumberID, &inbox.VerifyToken, &accessToken)
	if errors.Is(err, pgx.ErrNoRows) {
		return Inbox{}, ErrInboxNotFound
	}
	if err != nil {
		return Inbox{}, fmt.Errorf("whatsapp: scan inbox: %w", err)
	}
	inbox.AccessToken = decryptToken(accessToken)
	return inbox, nil
}

// The single seam ADR-0006 will fill: today the column holds the bytes as they were written, and only this function changes.
// The NUL bytes are trimmed because that is what the seed writes to mean "no number connected yet", and no real token carries one.
func decryptToken(stored []byte) string {
	return strings.Trim(string(stored), "\x00")
}

// InboxByPhoneNumberID is how an incoming payload names the number: Meta identifies it by this and not by our id.
func (r *Repository) InboxByPhoneNumberID(ctx context.Context, phoneNumberID string) (Inbox, error) {
	return scanInbox(r.exec.QueryRow(ctx, inboxSelect+" AND phone_number_id = $1", phoneNumberID))
}

const webhookEventInsert = `
INSERT INTO webhook_events (id, inbox_id, event_type, wamid, payload_json, signature_valid, status, received_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

// SaveWebhookEvent writes the log before anything is interpreted, so a payload that breaks later still left its trace (F20).
func (r *Repository) SaveWebhookEvent(ctx context.Context, event WebhookEvent) error {
	_, err := r.exec.Exec(ctx, webhookEventInsert,
		event.ID, event.InboxID, event.EventType, event.WAMID, event.PayloadJSON,
		event.SignatureValid, event.Status, event.ReceivedAt)
	if err != nil {
		return fmt.Errorf("whatsapp: insert webhook event: %w", err)
	}
	return nil
}

// MarkEventProcessed closes the event; status and retries mutate while reprocessing and never after (F20).
func (r *Repository) MarkEventProcessed(ctx context.Context, eventID uuid.UUID, at time.Time) error {
	_, err := r.exec.Exec(ctx,
		`UPDATE webhook_events SET status = $2, processed_at = $3 WHERE id = $1`,
		eventID, enums.WebhookEventStatusProcessed, at)
	if err != nil {
		return fmt.Errorf("whatsapp: mark webhook event processed: %w", err)
	}
	return nil
}

// ContactByPhone reads a table contacts/ owns: the ownership rule restricts writes and not reads (07-secuencia).
func (r *Repository) ContactByPhone(ctx context.Context, accountID uuid.UUID, phone string) (Contact, error) {
	var contact Contact
	err := r.exec.QueryRow(ctx,
		`SELECT id, account_id FROM contacts WHERE account_id = $1 AND phone_whatsapp = $2 AND deleted_at IS NULL`,
		accountID, phone).Scan(&contact.ID, &contact.AccountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Contact{}, ErrContactNotFound
	}
	if err != nil {
		return Contact{}, fmt.Errorf("whatsapp: scan contact: %w", err)
	}
	return contact, nil
}
