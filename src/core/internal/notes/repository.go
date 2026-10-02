package notes

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

// A deleted note leaves the thread and keeps its row (C09); the author is joined so the note is read with its signature.
const noteSelect = `
SELECT n.id, n.conversation_id, o.id, o.name, o.lastname, n.content, n.created_at, n.updated_at
FROM   internal_notes n
JOIN   operators o ON o.id = n.operator_id
WHERE  n.deleted_at IS NULL`

func (r *Repository) Notes(ctx context.Context, conversationID uuid.UUID) ([]Note, error) {
	rows, err := r.exec.Query(ctx, noteSelect+" AND n.conversation_id = $1 ORDER BY n.created_at, n.id", conversationID)
	if err != nil {
		return nil, fmt.Errorf("notes: read the notes: %w", err)
	}
	defer rows.Close()

	out := []Note{}
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// NoteByID reads the note only within its conversation, so an id of another thread is a note that does not exist.
func (r *Repository) NoteByID(ctx context.Context, conversationID, noteID uuid.UUID) (Note, error) {
	n, err := scanNote(r.exec.QueryRow(ctx, noteSelect+" AND n.conversation_id = $1 AND n.id = $2", conversationID, noteID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Note{}, ErrNoteNotFound
	}
	return n, err
}

func scanNote(row pgx.Row) (Note, error) {
	var n Note
	err := row.Scan(&n.ID, &n.ConversationID, &n.Author.ID, &n.Author.Name, &n.Author.Lastname, &n.Content, &n.CreatedAt, &n.UpdatedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Note{}, fmt.Errorf("notes: scan a note: %w", err)
	}
	return n, err
}

func (r *Repository) AddNote(ctx context.Context, n Note) error {
	if _, err := r.exec.Exec(ctx, `INSERT INTO internal_notes (id, conversation_id, operator_id, content) VALUES ($1, $2, $3, $4)`,
		n.ID, n.ConversationID, n.Author.ID, n.Content); err != nil {
		return fmt.Errorf("notes: add a note: %w", err)
	}
	return nil
}

// The author is in the predicate of both writes, so the rule holds even if the read before it was stale (ADR-0056).
func (r *Repository) EditNote(ctx context.Context, noteID, operatorID uuid.UUID, content string) error {
	return r.noteWrite(ctx, "edit", `UPDATE internal_notes SET content = $3, updated_at = now()
		WHERE id = $1 AND operator_id = $2 AND deleted_at IS NULL`, noteID, operatorID, content)
}

func (r *Repository) DeleteNote(ctx context.Context, noteID, operatorID uuid.UUID) error {
	return r.noteWrite(ctx, "delete", `UPDATE internal_notes SET deleted_at = now(), updated_at = now()
		WHERE id = $1 AND operator_id = $2 AND deleted_at IS NULL`, noteID, operatorID)
}

func (r *Repository) noteWrite(ctx context.Context, what, query string, args ...any) error {
	tag, err := r.exec.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("notes: %s a note: %w", what, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNoteNotFound
	}
	return nil
}
