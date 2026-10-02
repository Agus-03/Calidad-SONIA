// Package notes owns internal_notes: what the team writes on a conversation for itself, never sent to the customer (F26, ADR-0056).
package notes

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/inbox"
)

var (
	// ErrEmptyNote is a note with no text, which tells the next vendor nothing (ADR-0056).
	ErrEmptyNote = errors.New("notes: the note has no text")

	// ErrNoteNotFound covers a note that does not exist, was deleted or is of another conversation: the path names one of this thread.
	ErrNoteNotFound = errors.New("notes: note not found")

	// ErrNotTheAuthor is a note edited or deleted by anybody but who wrote it, which the group chose to forbid (ADR-0056).
	ErrNotTheAuthor = errors.New("notes: only the author edits or deletes a note")
)

// Reach is inbox/: a note hangs from a conversation, so who reaches it is the reach of the thread and nothing of its own (ADR-0063).
type Reach interface {
	Reachable(ctx context.Context, scope inbox.Scope, conversationID uuid.UUID) error
}

// Author is who signs a note, with the name the thread shows next to it.
type Author struct {
	ID       uuid.UUID
	Name     string
	Lastname string
}

// Note is one internal note of F26: for the team, signed by who wrote it, and never sent to the customer (ADR-0056).
type Note struct {
	ID             uuid.UUID
	ConversationID uuid.UUID
	Author         Author
	Content        string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
