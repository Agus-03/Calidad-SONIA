package documents

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

// WithExecutor binds the repository to a transaction its caller opened (regla 5).
func (r *Repository) WithExecutor(exec db.Executor) *Repository { return &Repository{exec: exec} }

// Reads ai_agents and never writes it: whether the agent exists in the account is all documents/ needs from agents/ (ADR-0063).
const agentExists = `
SELECT EXISTS (SELECT 1 FROM ai_agents WHERE account_id = $1 AND id = $2 AND deleted_at IS NULL)`

func (r *Repository) AgentExists(ctx context.Context, accountID, agentID uuid.UUID) (bool, error) {
	var exists bool
	if err := r.exec.QueryRow(ctx, agentExists, accountID, agentID).Scan(&exists); err != nil {
		return false, fmt.Errorf("documents: read ai agent: %w", err)
	}
	return exists, nil
}

// ON CONFLICT on the partial unique index, so two first uploads at once create one base and not an error (ADR-0064, regla 5).
const baseInsert = `
INSERT INTO knowledge_bases (id, ai_agent_id, collection_name)
VALUES ($1, $2, $3)
ON CONFLICT (ai_agent_id) WHERE deleted_at IS NULL DO NOTHING`

const baseSelect = `
SELECT id, ai_agent_id, collection_name, size_bytes_total
FROM   knowledge_bases
WHERE  ai_agent_id = $1 AND deleted_at IS NULL`

// EnsureKnowledgeBase returns the agent's base, creating it if this is its first upload. kb carries the id to use if it is.
func (r *Repository) EnsureKnowledgeBase(ctx context.Context, kb KnowledgeBase) (KnowledgeBase, error) {
	if _, err := r.exec.Exec(ctx, baseInsert, kb.ID, kb.AIAgentID, kb.CollectionName); err != nil {
		return KnowledgeBase{}, fmt.Errorf("documents: insert knowledge base: %w", err)
	}
	return scanBase(r.exec.QueryRow(ctx, baseSelect, kb.AIAgentID))
}

// KnowledgeBase answers found = false for an agent that never uploaded, which is a base of zero bytes and not an error.
func (r *Repository) KnowledgeBase(ctx context.Context, agentID uuid.UUID) (KnowledgeBase, bool, error) {
	kb, err := scanBase(r.exec.QueryRow(ctx, baseSelect, agentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return KnowledgeBase{}, false, nil
	}
	return kb, err == nil, err
}

// KnowledgeBaseForUpdate is the lock of ADR-0064, regla 6 (c): every upload and every removal of a base pass through this row,
// so the limit and the duplicate are checked against a total nobody else is moving.
func (r *Repository) KnowledgeBaseForUpdate(ctx context.Context, agentID uuid.UUID) (KnowledgeBase, error) {
	kb, err := scanBase(r.exec.QueryRow(ctx, baseSelect+" FOR UPDATE", agentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return KnowledgeBase{}, errNoKnowledgeBase
	}
	return kb, err
}

// size_bytes_total is the materialized sum of relaciones.md §9, moved in the same transaction as the row that changes it.
const baseAddSize = `
UPDATE knowledge_bases
SET    size_bytes_total = size_bytes_total + $2, updated_at = now()
WHERE  id = $1`

func (r *Repository) AddSize(ctx context.Context, knowledgeBaseID uuid.UUID, delta int64) error {
	if _, err := r.exec.Exec(ctx, baseAddSize, knowledgeBaseID, delta); err != nil {
		return fmt.Errorf("documents: move knowledge base size: %w", err)
	}
	return nil
}

// Live documents only: a removed one frees its checksum, so the admin can upload the same file again (ADR-0070).
const checksumTaken = `
SELECT EXISTS (SELECT 1 FROM documents WHERE knowledge_base_id = $1 AND checksum = $2 AND deleted_at IS NULL)`

func (r *Repository) ChecksumTaken(ctx context.Context, knowledgeBaseID uuid.UUID, checksum string) (bool, error) {
	var taken bool
	if err := r.exec.QueryRow(ctx, checksumTaken, knowledgeBaseID, checksum).Scan(&taken); err != nil {
		return false, fmt.Errorf("documents: read checksum: %w", err)
	}
	return taken, nil
}

// The status is written and not defaulted: pending is what the worker looks for, and saying it here makes that visible (ADR-0064).
const documentInsert = `
INSERT INTO documents (id, knowledge_base_id, filename, mime_type, size_bytes, checksum, object_key, status,
                       uploaded_by_operator_id, personal_data_terms_version)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING created_at`

func (r *Repository) InsertDocument(ctx context.Context, d Document) (Document, error) {
	err := r.exec.QueryRow(ctx, documentInsert, d.ID, d.KnowledgeBaseID, d.Filename, d.MIMEType, d.SizeBytes, d.Checksum,
		d.ObjectKey, string(d.Status), d.UploadedByOperatorID, d.PersonalDataTermsVersion).Scan(&d.CreatedAt)
	if err != nil {
		return Document{}, fmt.Errorf("documents: insert document: %w", err)
	}
	return d, nil
}

const documentColumns = `d.id, d.knowledge_base_id, d.filename, d.mime_type, d.size_bytes, d.checksum, d.object_key, d.status,
       d.error_message, d.uploaded_by_operator_id, d.personal_data_terms_version, d.created_at`

// Scoped through the base to the agent and through the agent to the account, so an id guessed elsewhere reads as missing.
const documentScope = `
FROM   documents d
JOIN   knowledge_bases kb ON kb.id = d.knowledge_base_id AND kb.deleted_at IS NULL
JOIN   ai_agents a        ON a.id = kb.ai_agent_id AND a.deleted_at IS NULL
WHERE  a.account_id = $1 AND a.id = $2 AND d.deleted_at IS NULL`

// Newest first, because the admin uploads and then looks for what is still pending. Not paged, as agents/ is not: the 100 MB of
// F06 bound the list of one base, and the panel shows one base whole, with the space it has left.
const documentList = `SELECT ` + documentColumns + documentScope + `
ORDER BY d.created_at DESC, d.id DESC`

func (r *Repository) List(ctx context.Context, accountID, agentID uuid.UUID) ([]Document, error) {
	rows, err := r.exec.Query(ctx, documentList, accountID, agentID)
	if err != nil {
		return nil, fmt.Errorf("documents: list documents: %w", err)
	}
	defer rows.Close()

	found := []Document{}
	for rows.Next() {
		d, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		found = append(found, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("documents: read documents: %w", err)
	}
	return found, nil
}

// FOR UPDATE OF d so the lock is the document's and not the agent's: the base was already locked by whoever conducts.
const documentForUpdate = `SELECT ` + documentColumns + documentScope + ` AND d.id = $3
FOR UPDATE OF d`

func (r *Repository) DocumentForUpdate(ctx context.Context, accountID, agentID, documentID uuid.UUID) (Document, error) {
	d, err := scanDocument(r.exec.QueryRow(ctx, documentForUpdate, accountID, agentID, documentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrDocumentNotFound
	}
	return d, err
}

// The object stays in the store and the chunks stay for retrieval_logs: the worker takes the vectors out (ADR-0064, regla 9).
const documentSoftDelete = `
UPDATE documents
SET    deleted_at = now(), updated_at = now()
WHERE  id = $1 AND deleted_at IS NULL`

func (r *Repository) SoftDeleteDocument(ctx context.Context, documentID uuid.UUID) error {
	tag, err := r.exec.Exec(ctx, documentSoftDelete, documentID)
	if err != nil {
		return fmt.Errorf("documents: delete document: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrDocumentNotFound
	}
	return nil
}

func scanBase(row pgx.Row) (KnowledgeBase, error) {
	var kb KnowledgeBase
	if err := row.Scan(&kb.ID, &kb.AIAgentID, &kb.CollectionName, &kb.SizeBytesTotal); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return KnowledgeBase{}, err
		}
		return KnowledgeBase{}, fmt.Errorf("documents: scan knowledge base: %w", err)
	}
	return kb, nil
}

func scanDocument(row pgx.Row) (Document, error) {
	var d Document
	var status string
	if err := row.Scan(&d.ID, &d.KnowledgeBaseID, &d.Filename, &d.MIMEType, &d.SizeBytes, &d.Checksum, &d.ObjectKey, &status,
		&d.ErrorMessage, &d.UploadedByOperatorID, &d.PersonalDataTermsVersion, &d.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Document{}, err
		}
		return Document{}, fmt.Errorf("documents: scan document: %w", err)
	}
	d.Status = enums.DocumentStatus(status)
	return d, nil
}
