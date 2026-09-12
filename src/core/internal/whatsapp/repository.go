package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/encryption"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// Repository takes the Executor and not the pool, so the turn can later reach these tables inside its transaction (regla 5).
// The cipher is held for the two secrets of inboxes, which leave this package in the clear and never as bytes (ADR-0031, regla 11).
type Repository struct {
	exec   db.Executor
	cipher *encryption.Cipher
}

func NewRepository(exec db.Executor, cipher *encryption.Cipher) *Repository {
	return &Repository{exec: exec, cipher: cipher}
}

// WithExecutor returns the same repository bound to a transaction, so the caller that conducts owns it and this one never opens it (regla 5).
func (r *Repository) WithExecutor(exec db.Executor) *Repository {
	return &Repository{exec: exec, cipher: r.cipher}
}

const inboxSelect = `
SELECT id, account_id, default_ai_agent_id, phone_number_id, verify_token, access_token_encrypted, app_secret_encrypted
FROM   inboxes
WHERE  deleted_at IS NULL`

// InboxByVerifyToken is how the handshake resolves: Meta sends the token and nothing else that names the number.
func (r *Repository) InboxByVerifyToken(ctx context.Context, token string) (Inbox, error) {
	return r.scanInbox(r.exec.QueryRow(ctx, inboxSelect+" AND verify_token = $1", token))
}

// A secret that fails to decrypt refuses the whole inbox: a webhook checked against a wrong key would read as forged (ADR-0031, regla 2).
func (r *Repository) scanInbox(row pgx.Row) (Inbox, error) {
	var inbox Inbox
	var accessToken, appSecret []byte
	err := row.Scan(&inbox.ID, &inbox.AccountID, &inbox.DefaultAIAgentID, &inbox.PhoneNumberID, &inbox.VerifyToken,
		&accessToken, &appSecret)
	if errors.Is(err, pgx.ErrNoRows) {
		return Inbox{}, ErrInboxNotFound
	}
	if err != nil {
		return Inbox{}, fmt.Errorf("whatsapp: scan inbox: %w", err)
	}

	if inbox.AccessToken, err = r.cipher.Decrypt(accessToken); err != nil {
		return Inbox{}, fmt.Errorf("whatsapp: decrypt the access token: %w", err)
	}
	if inbox.AppSecret, err = r.cipher.Decrypt(appSecret); err != nil {
		return Inbox{}, fmt.Errorf("whatsapp: decrypt the app secret: %w", err)
	}
	return inbox, nil
}

// InboxByPhoneNumberID is how an incoming payload names the number: Meta identifies it by this and not by our id.
func (r *Repository) InboxByPhoneNumberID(ctx context.Context, phoneNumberID string) (Inbox, error) {
	return r.scanInbox(r.exec.QueryRow(ctx, inboxSelect+" AND phone_number_id = $1", phoneNumberID))
}

// InboxByID is how the turn resolves which number answers: it holds the conversation's inbox_id and nothing else about it.
func (r *Repository) InboxByID(ctx context.Context, inboxID uuid.UUID) (Inbox, error) {
	return r.scanInbox(r.exec.QueryRow(ctx, inboxSelect+" AND id = $1", inboxID))
}

const inboxConnect = `
UPDATE inboxes
SET    access_token_encrypted = $2, app_secret_encrypted = $3, updated_at = now()
WHERE  phone_number_id = $1 AND deleted_at IS NULL
RETURNING id`

// Connect updates and never inserts: the rest of the row is plain SQL anyone can write, and only the two secrets need the cipher.
// It overwrites on purpose, because running it again is how an expired or leaked token gets replaced (ADR-0031).
func (r *Repository) Connect(ctx context.Context, phoneNumberID, accessToken, appSecret string) (uuid.UUID, error) {
	token, err := r.cipher.Encrypt(accessToken)
	if err != nil {
		return uuid.Nil, fmt.Errorf("whatsapp: encrypt the access token: %w", err)
	}
	secret, err := r.cipher.Encrypt(appSecret)
	if err != nil {
		return uuid.Nil, fmt.Errorf("whatsapp: encrypt the app secret: %w", err)
	}

	var inboxID uuid.UUID
	err = r.exec.QueryRow(ctx, inboxConnect, phoneNumberID, token, secret).Scan(&inboxID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrInboxNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("whatsapp: connect inbox: %w", err)
	}
	return inboxID, nil
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
