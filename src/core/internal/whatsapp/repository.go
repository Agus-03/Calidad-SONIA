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

// The columns of the admin panel and none of the three secrets: they never leave this file for that screen (ADR-0031, regla 11).
const settingsSelect = `
SELECT id, account_id, display_name, phone_number_id, status, default_ai_agent_id
FROM   inboxes
WHERE  deleted_at IS NULL AND account_id = $1`

func scanSettings(row pgx.Row) (InboxSettings, error) {
	var s InboxSettings
	err := row.Scan(&s.ID, &s.AccountID, &s.DisplayName, &s.PhoneNumberID, &s.Status, &s.DefaultAIAgentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return InboxSettings{}, ErrInboxNotFound
	}
	if err != nil {
		return InboxSettings{}, fmt.Errorf("whatsapp: scan inbox settings: %w", err)
	}
	return s, nil
}

// ListSettings has no page: an account connects a handful of numbers, the same reading as the agents of ADR-0038 regla 11.
func (r *Repository) ListSettings(ctx context.Context, accountID uuid.UUID) ([]InboxSettings, error) {
	rows, err := r.exec.Query(ctx, settingsSelect+" ORDER BY lower(display_name), id", accountID)
	if err != nil {
		return nil, fmt.Errorf("whatsapp: list inboxes: %w", err)
	}
	defer rows.Close()

	out := []InboxSettings{}
	for rows.Next() {
		s, err := scanSettings(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CAMBIADO POR TOBI
const inboxCreateSettings = `
INSERT INTO inboxes (
	id, account_id, phone_number_id, waba_id, display_name, 
	verify_token, access_token_encrypted, status
) VALUES (
	$1, $2, $3, $4, $5, 
	$6, $7, 'active'
) RETURNING id, account_id, display_name, phone_number_id, status, default_ai_agent_id`

// CreateSettings inserts a new inbox from the UI.
func (r *Repository) CreateSettings(ctx context.Context, accountID uuid.UUID, displayName, phoneNumberID, wabaID, accessToken, verifyToken string) (InboxSettings, error) {
	inboxID := uuid.New()
	
	var encryptedToken []byte
	var err error
	if accessToken != "" {
		encryptedToken, err = r.cipher.Encrypt(accessToken)
		if err != nil {
			return InboxSettings{}, fmt.Errorf("whatsapp: encrypt the access token: %w", err)
		}
	}

	return scanSettings(r.exec.QueryRow(ctx, inboxCreateSettings, 
		inboxID, accountID, phoneNumberID, wabaID, displayName, verifyToken, encryptedToken))
}

// SettingsForUpdate locks the inbox for the transaction that edits it, so the audit's before is the one this edit replaced.
func (r *Repository) SettingsForUpdate(ctx context.Context, accountID, inboxID uuid.UUID) (InboxSettings, error) {
	return scanSettings(r.exec.QueryRow(ctx, settingsSelect+" AND id = $2 FOR UPDATE", accountID, inboxID))
}

// FOR SHARE and not a plain read: the removal of an agent locks its row FOR UPDATE, so an assignment and a removal at once are
// serialised, and whichever commits second sees the other (ADR-0038). A paused agent passes: a pause is not a removal.
const aiAgentAssignable = `
SELECT 1
FROM   ai_agents
WHERE  id = $1 AND account_id = $2 AND deleted_at IS NULL
FOR SHARE`

// AIAgentAssignable reads a table agents/ owns: the ownership rule restricts writes and not reads.
func (r *Repository) AIAgentAssignable(ctx context.Context, accountID, aiAgentID uuid.UUID) error {
	var one int
	err := r.exec.QueryRow(ctx, aiAgentAssignable, aiAgentID, accountID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAIAgentNotAssignable
	}
	if err != nil {
		return fmt.Errorf("whatsapp: read the agent to assign: %w", err)
	}
	return nil
}

// The default of the inbox is read again here and not taken from the read before the transaction, so an admin who just changed or
// cleared it is obeyed. FOR SHARE OF a, the same lock as an assignment, and on the agent alone: a pause or a removal holds the agent
// while it turns its bots off, so a birth at that instant waits and reads what it committed. Locking the inbox too would take inbox
// then agent, the reverse of a removal, and the two could wait on each other (ADR-0038).
const aiAgentForBirth = `
SELECT a.id, a.active
FROM   inboxes i
JOIN   ai_agents a ON a.id = i.default_ai_agent_id AND a.account_id = i.account_id AND a.deleted_at IS NULL
WHERE  i.id = $1 AND i.account_id = $2 AND i.deleted_at IS NULL
FOR SHARE OF a`

// AIAgentForBirth answers the agent a new conversation of the inbox is born with and whether it can answer. An inbox without one, or
// whose agent was removed, names none; a paused one is still named, and born off (ADR-0038, reglas 3 y 15).
func (r *Repository) AIAgentForBirth(ctx context.Context, accountID, inboxID uuid.UUID) (*uuid.UUID, bool, error) {
	var aiAgentID uuid.UUID
	var active bool
	err := r.exec.QueryRow(ctx, aiAgentForBirth, inboxID, accountID).Scan(&aiAgentID, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("whatsapp: read the agent a conversation is born with: %w", err)
	}
	return &aiAgentID, active, nil
}

const inboxSetDefaultAIAgent = `
UPDATE inboxes
SET    default_ai_agent_id = $3, updated_at = now()
WHERE  account_id = $1 AND id = $2 AND deleted_at IS NULL
RETURNING id, account_id, display_name, phone_number_id, status, default_ai_agent_id`

// SetDefaultAIAgent takes nil to leave the number without AI, which is a state ADR-0012 declares valid. Only the conversations born
// after it get the new agent: the open ones keep the one that answered them (ADR-0038).
func (r *Repository) SetDefaultAIAgent(ctx context.Context, accountID, inboxID uuid.UUID, aiAgentID *uuid.UUID) (InboxSettings, error) {
	return scanSettings(r.exec.QueryRow(ctx, inboxSetDefaultAIAgent, accountID, inboxID, aiAgentID))
}

// Every inbox of the account and not only the live ones: a default naming a removed agent is wrong whether or not the inbox is.
const inboxClearDefaultAIAgent = `
UPDATE inboxes
SET    default_ai_agent_id = NULL, updated_at = now()
WHERE  account_id = $1 AND default_ai_agent_id = $2
RETURNING id`

// ClearDefaultAIAgent runs inside the transaction that removes the agent, so no conversation is born with it after the removal
// commits (ADR-0038). It returns the inboxes it changed, because the audit of the removal is the only place that records them.
func (r *Repository) ClearDefaultAIAgent(ctx context.Context, accountID, aiAgentID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.exec.Query(ctx, inboxClearDefaultAIAgent, accountID, aiAgentID)
	if err != nil {
		return nil, fmt.Errorf("whatsapp: clear the default agent: %w", err)
	}
	defer rows.Close()

	cleared := []uuid.UUID{}
	for rows.Next() {
		var inboxID uuid.UUID
		if err := rows.Scan(&inboxID); err != nil {
			return nil, fmt.Errorf("whatsapp: scan a cleared inbox: %w", err)
		}
		cleared = append(cleared, inboxID)
	}
	return cleared, rows.Err()
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
