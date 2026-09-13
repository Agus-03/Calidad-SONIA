package agents

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

type Repository struct{ exec db.Executor }

func NewRepository(exec db.Executor) *Repository { return &Repository{exec: exec} }

// WithExecutor returns the same repository bound to a transaction, so the caller that conducts owns it and this one never opens it (regla 5).
func (r *Repository) WithExecutor(exec db.Executor) *Repository { return &Repository{exec: exec} }

const agentColumns = `id, account_id, name, description, llm_provider, llm_model, temperature, max_tokens, active, created_at, updated_at`

// Removed agents are excluded here and nowhere else, so every read of the package treats a removal as absence (C09).
const agentSelect = `
SELECT ` + agentColumns + `
FROM   ai_agents
WHERE  deleted_at IS NULL`

func scanAgent(row pgx.Row) (Agent, error) {
	var a Agent
	err := row.Scan(&a.ID, &a.AccountID, &a.Name, &a.Description, &a.LLMProvider, &a.LLMModel,
		&a.Temperature, &a.MaxTokens, &a.Active, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Agent{}, ErrNotFound
	}
	if err != nil {
		return Agent{}, fmt.Errorf("agents: scan agent: %w", err)
	}
	return a, nil
}

// List has no page: an account configures a handful of agents, and a page number over five rows is a control nobody uses (ADR-0038).
func (r *Repository) List(ctx context.Context, accountID uuid.UUID) ([]Agent, error) {
	rows, err := r.exec.Query(ctx, agentSelect+" AND account_id = $1 ORDER BY lower(name), id", accountID)
	if err != nil {
		return nil, fmt.Errorf("agents: list agents: %w", err)
	}
	defer rows.Close()

	out := []Agent{}
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ByID is scoped by account and not only by id, so an identifier guessed from another account reads as missing.
func (r *Repository) ByID(ctx context.Context, accountID, agentID uuid.UUID) (Agent, error) {
	return scanAgent(r.exec.QueryRow(ctx, agentSelect+" AND account_id = $1 AND id = $2", accountID, agentID))
}

// ByIDForUpdate locks the row for the transaction that edits it, so two saves at once audit a before that each of them really saw.
func (r *Repository) ByIDForUpdate(ctx context.Context, accountID, agentID uuid.UUID) (Agent, error) {
	return scanAgent(r.exec.QueryRow(ctx, agentSelect+" AND account_id = $1 AND id = $2 FOR UPDATE", accountID, agentID))
}

// default_mode has no default in 0001 and is not part of the API until Fase 9, so every agent is born with the mode the conversation
// aggregate already assumes for it (ADR-0038).
const agentInsert = `
INSERT INTO ai_agents (id, account_id, name, description, default_mode, llm_provider, llm_model, temperature, max_tokens)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING ` + agentColumns

// Create returns the row as PostgreSQL wrote it, because the clock and the default of active are the database's (C06).
func (r *Repository) Create(ctx context.Context, a Agent) (Agent, error) {
	return scanAgent(r.exec.QueryRow(ctx, agentInsert,
		a.ID, a.AccountID, a.Name, a.Description, enums.AgentModeAutonomous,
		a.LLMProvider, a.LLMModel, a.Temperature, a.MaxTokens))
}

const agentUpdate = `
UPDATE ai_agents
SET    name = $3, description = $4, llm_provider = $5, llm_model = $6, temperature = $7, max_tokens = $8, active = $9,
       updated_at = now()
WHERE  account_id = $1 AND id = $2 AND deleted_at IS NULL
RETURNING ` + agentColumns

// Update writes the whole configuration: the service already merged what was sent into what the locked row said.
func (r *Repository) Update(ctx context.Context, accountID, agentID uuid.UUID, c Config) (Agent, error) {
	return scanAgent(r.exec.QueryRow(ctx, agentUpdate, accountID, agentID,
		c.Name, c.Description, c.LLMProvider, c.LLMModel, c.Temperature, c.MaxTokens, c.Active))
}

// The predicate repeats deleted_at IS NULL so removing twice reports a missing agent instead of moving the timestamp.
const agentSoftDelete = `
UPDATE ai_agents
SET    deleted_at = now(), updated_at = now()
WHERE  account_id = $1 AND id = $2 AND deleted_at IS NULL`

// SoftDelete and not a DELETE: generations.ai_agent_id and messages.sender_ai_agent_id still name it, and OE1 reads them (C09, C05).
func (r *Repository) SoftDelete(ctx context.Context, accountID, agentID uuid.UUID) error {
	tag, err := r.exec.Exec(ctx, agentSoftDelete, accountID, agentID)
	if err != nil {
		return fmt.Errorf("agents: delete agent: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const auditInsert = `
INSERT INTO audit_logs (id, operator_id, action, entity_type, entity_id, changes_json)
VALUES ($1, $2, $3, $4, $5, $6)`

// RecordAudit runs in the transaction of the change it describes, so no change commits without its row and no row without its change.
func (r *Repository) RecordAudit(ctx context.Context, e AuditEntry) error {
	if _, err := r.exec.Exec(ctx, auditInsert, e.ID, e.OperatorID, e.Action, e.EntityType, e.EntityID, e.Changes); err != nil {
		return fmt.Errorf("agents: insert audit log: %w", err)
	}
	return nil
}
