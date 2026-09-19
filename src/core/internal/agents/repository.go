package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
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

func scanAIAgent(row pgx.Row) (AIAgent, error) {
	var a AIAgent
	err := row.Scan(&a.ID, &a.AccountID, &a.Name, &a.Description, &a.LLMProvider, &a.LLMModel,
		&a.Temperature, &a.MaxTokens, &a.Active, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AIAgent{}, ErrNotFound
	}
	if err != nil {
		return AIAgent{}, fmt.Errorf("agents: scan agent: %w", err)
	}
	return a, nil
}

// List has no page: an account configures a handful of agents, and a page number over five rows is a control nobody uses (ADR-0038).
func (r *Repository) List(ctx context.Context, accountID uuid.UUID) ([]AIAgent, error) {
	rows, err := r.exec.Query(ctx, agentSelect+" AND account_id = $1 ORDER BY lower(name), id", accountID)
	if err != nil {
		return nil, fmt.Errorf("agents: list agents: %w", err)
	}
	defer rows.Close()

	out := []AIAgent{}
	for rows.Next() {
		a, err := scanAIAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ByID is scoped by account and not only by id, so an identifier guessed from another account reads as missing.
func (r *Repository) ByID(ctx context.Context, accountID, agentID uuid.UUID) (AIAgent, error) {
	return scanAIAgent(r.exec.QueryRow(ctx, agentSelect+" AND account_id = $1 AND id = $2", accountID, agentID))
}

// ByIDForUpdate locks the row for the transaction that edits it, so two saves at once audit a before that each of them really saw.
// NO KEY UPDATE and not UPDATE: the id never changes, and a full lock would stop every foreign key check on the agent —a message
// the turn writes, a generation the brain writes, a conversation born— while a save or a pause is open. It still conflicts with the
// FOR SHARE of an assignment and of a birth, which is what serialises them (ADR-0038).
func (r *Repository) ByIDForUpdate(ctx context.Context, accountID, agentID uuid.UUID) (AIAgent, error) {
	return scanAIAgent(r.exec.QueryRow(ctx, agentSelect+" AND account_id = $1 AND id = $2 FOR NO KEY UPDATE", accountID, agentID))
}

// default_mode has no default in 0001 and is not part of the API until Fase 9, so every agent is born with the mode the conversation
// aggregate already assumes for it (ADR-0038).
const agentInsert = `
INSERT INTO ai_agents (id, account_id, name, description, default_mode, llm_provider, llm_model, temperature, max_tokens)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING ` + agentColumns

// Create returns the row as PostgreSQL wrote it, because the clock and the default of active are the database's (C06).
func (r *Repository) Create(ctx context.Context, a AIAgent) (AIAgent, error) {
	return scanAIAgent(r.exec.QueryRow(ctx, agentInsert,
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
func (r *Repository) Update(ctx context.Context, accountID, agentID uuid.UUID, c Config) (AIAgent, error) {
	return scanAIAgent(r.exec.QueryRow(ctx, agentUpdate, accountID, agentID,
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

// RecordInboxDefault is how whatsapp/ audits the agent it names for a number: audit_logs is this package's table, so it is written
// here, inside the caller's transaction (regla 1, ADR-0038). Plain ids, so whatsapp/ does not import this package.
func (r *Repository) RecordInboxDefault(ctx context.Context, operatorID, inboxID uuid.UUID, before, after *uuid.UUID) error {
	changes, err := json.Marshal(inboxChange{Before: inboxDefault{DefaultAIAgentID: before}, After: inboxDefault{DefaultAIAgentID: after}})
	if err != nil {
		return fmt.Errorf("agents: encode the audit of an inbox default: %w", err)
	}
	return r.RecordAudit(ctx, AuditEntry{
		ID: id.New(), OperatorID: operatorID, Action: actionUpdate, EntityType: entityInbox, EntityID: inboxID, Changes: changes,
	})
}

// The version is read and not stored in the row: prompt_component_versions is the one place it lives, so the two cannot disagree. The
// agent is joined for its account and its removal: a component of a removed agent is out of reach like the agent (C09).
const componentSelect = `
SELECT c.id, c.ai_agent_id, c.type, c.content, c.position, c.active, c.created_at, c.updated_at,
       COALESCE((SELECT max(v.version_number) FROM prompt_component_versions v WHERE v.prompt_component_id = c.id), 0)
FROM   prompt_components c
JOIN   ai_agents a ON a.id = c.ai_agent_id
WHERE  c.deleted_at IS NULL AND a.deleted_at IS NULL AND a.account_id = $1 AND c.ai_agent_id = $2`

func scanComponent(row pgx.Row) (Component, error) {
	var c Component
	err := row.Scan(&c.ID, &c.AIAgentID, &c.Type, &c.Content, &c.Position, &c.Active, &c.CreatedAt, &c.UpdatedAt, &c.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return Component{}, ErrComponentNotFound
	}
	if err != nil {
		return Component{}, fmt.Errorf("agents: scan prompt component: %w", err)
	}
	return c, nil
}

// Components lists the blocks of an agent in the order the brain assembles them, the inactive ones included: the panel shows them to
// be turned back on.
func (r *Repository) Components(ctx context.Context, accountID, agentID uuid.UUID) ([]Component, error) {
	rows, err := r.exec.Query(ctx, componentSelect+" ORDER BY c.position, c.id", accountID, agentID)
	if err != nil {
		return nil, fmt.Errorf("agents: list prompt components: %w", err)
	}
	defer rows.Close()

	out := []Component{}
	for rows.Next() {
		c, err := scanComponent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repository) Component(ctx context.Context, accountID, agentID, componentID uuid.UUID) (Component, error) {
	return scanComponent(r.exec.QueryRow(ctx, componentSelect+" AND c.id = $3", accountID, agentID, componentID))
}

// ComponentForUpdate locks the block and not the agent, so two edits of one block number their versions one after the other.
func (r *Repository) ComponentForUpdate(ctx context.Context, accountID, agentID, componentID uuid.UUID) (Component, error) {
	return scanComponent(r.exec.QueryRow(ctx, componentSelect+" AND c.id = $3 FOR NO KEY UPDATE OF c", accountID, agentID, componentID))
}

// Every live block of the agent and not only the active ones: the unique index of 0001 covers the active, and one block per type
// is the rule of ADR-0039, whose check lives here while the caller holds the agent locked.
const componentTaken = `
SELECT COALESCE(bool_or(type = $2), false), COALESCE(bool_or(position = $3), false)
FROM   prompt_components
WHERE  ai_agent_id = $1 AND deleted_at IS NULL`

// ComponentTaken answers whether the agent already has a block of that type, and one at that position.
func (r *Repository) ComponentTaken(ctx context.Context, agentID uuid.UUID, t enums.PromptComponentType, position int16) (bool, bool, error) {
	var typeTaken, positionTaken bool
	if err := r.exec.QueryRow(ctx, componentTaken, agentID, t, position).Scan(&typeTaken, &positionTaken); err != nil {
		return false, false, fmt.Errorf("agents: read the prompt components taken: %w", err)
	}
	return typeTaken, positionTaken, nil
}

const componentInsert = `
INSERT INTO prompt_components (id, ai_agent_id, type, content, position)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, ai_agent_id, type, content, position, active, created_at, updated_at, 0`

// CreateComponent returns the row as PostgreSQL wrote it, at version 0 until the caller appends the first one.
func (r *Repository) CreateComponent(ctx context.Context, c Component) (Component, error) {
	return scanComponent(r.exec.QueryRow(ctx, componentInsert, c.ID, c.AIAgentID, c.Type, c.Content, c.Position))
}

const componentUpdate = `
UPDATE prompt_components
SET    content = $2, active = $3, updated_at = now()
WHERE  id = $1 AND deleted_at IS NULL
RETURNING id, ai_agent_id, type, content, position, active, created_at, updated_at, 0`

// UpdateComponent writes the text and the switch; the type and the position never change once the block exists (ADR-0039).
func (r *Repository) UpdateComponent(ctx context.Context, componentID uuid.UUID, content string, active bool) (Component, error) {
	return scanComponent(r.exec.QueryRow(ctx, componentUpdate, componentID, content, active))
}

// SoftDeleteComponent and not a DELETE: generation_components names its versions, and OE1 reads deleted_at to know since when the
// block no longer had to be assembled (05-consultas-de-metricas, OE1).
func (r *Repository) SoftDeleteComponent(ctx context.Context, componentID uuid.UUID) error {
	tag, err := r.exec.Exec(ctx,
		`UPDATE prompt_components SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, componentID)
	if err != nil {
		return fmt.Errorf("agents: delete prompt component: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrComponentNotFound
	}
	return nil
}

// The number is the next one of the block, counted here and not by the caller: the caller holds the block locked, so two versions
// can never read the same maximum, and the unique index of 0001 would refuse it if they did.
const versionInsert = `
INSERT INTO prompt_component_versions (id, prompt_component_id, version_number, content, active, changed_by_operator_id, change_note)
SELECT $1, $2, COALESCE(max(version_number), 0) + 1, $3, $4, $5, $6
FROM   prompt_component_versions
WHERE  prompt_component_id = $2
RETURNING version_number`

// AppendVersion is only ever an INSERT: a version is what the block said at a moment, and history is not corrected (C09).
func (r *Repository) AppendVersion(ctx context.Context, v ComponentVersion) (int, error) {
	var number int
	if err := r.exec.QueryRow(ctx, versionInsert, v.ID, v.ComponentID, v.Content, v.Active, v.ChangedBy, v.ChangeNote).Scan(&number); err != nil {
		return 0, fmt.Errorf("agents: insert prompt component version: %w", err)
	}
	return number, nil
}

// Versions answers one page of the history of a block, newest first, which is the order the panel shows it in, with the total the
// page-number listings of ADR-0036 regla 9 answer: the history only grows when somebody edits, like a catalogue.
func (r *Repository) Versions(ctx context.Context, componentID uuid.UUID, limit, offset int) (VersionPage, error) {
	rows, err := r.exec.Query(ctx, `
		SELECT id, prompt_component_id, version_number, content, active, changed_by_operator_id, change_note, created_at,
		       count(*) OVER ()
		FROM   prompt_component_versions
		WHERE  prompt_component_id = $1
		ORDER  BY version_number DESC
		LIMIT  $2 OFFSET $3`, componentID, limit, offset)
	if err != nil {
		return VersionPage{}, fmt.Errorf("agents: list prompt component versions: %w", err)
	}
	defer rows.Close()

	page := VersionPage{Items: []ComponentVersion{}}
	for rows.Next() {
		var v ComponentVersion
		if err := rows.Scan(&v.ID, &v.ComponentID, &v.Number, &v.Content, &v.Active, &v.ChangedBy, &v.ChangeNote, &v.CreatedAt,
			&page.Total); err != nil {
			return VersionPage{}, fmt.Errorf("agents: scan prompt component version: %w", err)
		}
		page.Items = append(page.Items, v)
	}
	if err := rows.Err(); err != nil {
		return VersionPage{}, fmt.Errorf("agents: list prompt component versions: %w", err)
	}
	// A page past the end reads no row and so no window count: the total is asked for on its own, so it is never a false zero.
	if len(page.Items) == 0 && offset > 0 {
		if err := r.exec.QueryRow(ctx, `SELECT count(*) FROM prompt_component_versions WHERE prompt_component_id = $1`,
			componentID).Scan(&page.Total); err != nil {
			return VersionPage{}, fmt.Errorf("agents: count prompt component versions: %w", err)
		}
	}
	return page, nil
}

// LatestVersion reads the number in a statement of its own, after the block is locked: the subquery of componentSelect is evaluated
// with the snapshot from before the wait, so behind another edit it would answer the version that edit replaced.
func (r *Repository) LatestVersion(ctx context.Context, componentID uuid.UUID) (int, error) {
	var number int
	if err := r.exec.QueryRow(ctx, `
		SELECT COALESCE(max(version_number), 0) FROM prompt_component_versions WHERE prompt_component_id = $1`,
		componentID).Scan(&number); err != nil {
		return 0, fmt.Errorf("agents: read the latest prompt component version: %w", err)
	}
	return number, nil
}
