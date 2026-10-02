package sales

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
)

type Repository struct{ exec db.Executor }

func NewRepository(exec db.Executor) *Repository { return &Repository{exec: exec} }

// Scoped by the account of the contact, like the inbox: a conversation of another account is one this caller cannot know exists.
const conversationSelect = `
SELECT c.id, c.operator_id, c.ai_agent_id
FROM   conversations c
JOIN   contacts ct ON ct.id = c.contact_id AND ct.deleted_at IS NULL
WHERE  c.deleted_at IS NULL AND ct.account_id = $1 AND c.id = $2`

func (r *Repository) Conversation(ctx context.Context, accountID, conversationID uuid.UUID) (Conversation, error) {
	var c Conversation
	err := r.exec.QueryRow(ctx, conversationSelect, accountID, conversationID).Scan(&c.ID, &c.OperatorID, &c.AIAgentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Conversation{}, ErrConversationNotFound
	}
	if err != nil {
		return Conversation{}, fmt.Errorf("sales: read the conversation: %w", err)
	}
	return c, nil
}

// The amount comes back as text, so the two decimals are read as stored and never through a float (C08).
const saleSelect = `
SELECT s.id, s.conversation_id, s.operator_id, s.ai_agent_id, s.amount::text, s.currency, ct.source_channel, s.created_at
FROM   sales s
JOIN   conversations c ON c.id = s.conversation_id AND c.deleted_at IS NULL
JOIN   contacts ct ON ct.id = c.contact_id AND ct.deleted_at IS NULL
WHERE  s.deleted_at IS NULL AND ct.account_id = $1`

func scanSale(row pgx.Row) (Sale, error) {
	var s Sale
	err := row.Scan(&s.ID, &s.ConversationID, &s.OperatorID, &s.AIAgentID, &s.Amount, &s.Currency, &s.SourceChannel, &s.CreatedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Sale{}, fmt.Errorf("sales: scan sale: %w", err)
	}
	return s, err
}

func (r *Repository) ByID(ctx context.Context, accountID, saleID uuid.UUID) (Sale, error) {
	s, err := scanSale(r.exec.QueryRow(ctx, saleSelect+" AND s.id = $2", accountID, saleID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Sale{}, ErrNotFound
	}
	return s, err
}

func (r *Repository) OfConversation(ctx context.Context, accountID, conversationID uuid.UUID) ([]Sale, error) {
	rows, err := r.exec.Query(ctx, saleSelect+" AND s.conversation_id = $2 ORDER BY s.created_at, s.id", accountID, conversationID)
	if err != nil {
		return nil, fmt.Errorf("sales: list: %w", err)
	}
	defer rows.Close()

	out := []Sale{}
	for rows.Next() {
		s, err := scanSale(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// The amount travels as text and PostgreSQL casts it, so no float sits between the request and the column (C08).
func (r *Repository) Create(ctx context.Context, s Sale) error {
	if _, err := r.exec.Exec(ctx, `
		INSERT INTO sales (id, conversation_id, operator_id, ai_agent_id, amount, currency)
		VALUES ($1, $2, $3, $4, $5::numeric, $6)`,
		s.ID, s.ConversationID, s.OperatorID, s.AIAgentID, s.Amount, s.Currency); err != nil {
		return fmt.Errorf("sales: create: %w", err)
	}
	return nil
}

func (r *Repository) Update(ctx context.Context, saleID uuid.UUID, amount, currency string) error {
	return r.write(ctx, "update",
		`UPDATE sales SET amount = $2::numeric, currency = $3, updated_at = now() WHERE id = $1 AND deleted_at IS NULL`,
		saleID, amount, currency)
}

// A sale marked by mistake leaves the metrics and keeps its row (C09).
func (r *Repository) Delete(ctx context.Context, saleID uuid.UUID) error {
	return r.write(ctx, "delete", `UPDATE sales SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, saleID)
}

func (r *Repository) write(ctx context.Context, what, query string, args ...any) error {
	tag, err := r.exec.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("sales: %s: %w", what, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
