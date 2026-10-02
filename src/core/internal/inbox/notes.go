package inbox

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

var (
	// ErrEmptyNote is a note with no text, which tells the next vendor nothing (ADR-0056).
	ErrEmptyNote = errors.New("inbox: the note has no text")

	// ErrNoteNotFound covers a note that does not exist, was deleted or is of another conversation: the path names one of this thread.
	ErrNoteNotFound = errors.New("inbox: note not found")

	// ErrNotTheAuthor is a note edited or deleted by anybody but who wrote it, which the group chose to forbid (ADR-0056).
	ErrNotTheAuthor = errors.New("inbox: only the author edits or deletes a note")
)

// Notes reads the internal notes of F26, oldest first, with the reach of the thread: they are for the team and never reach the customer.
func (s *Service) Notes(ctx context.Context, scope Scope, conversationID uuid.UUID) ([]Note, error) {
	if err := s.reachable(ctx, scope, conversationID); err != nil {
		return nil, err
	}
	return s.opts.Repo.Notes(ctx, conversationID)
}

// AddNote writes with the reach of the thread too, on a resolved conversation as well: a note is what the next vendor reads (ADR-0056).
func (s *Service) AddNote(ctx context.Context, scope Scope, conversationID uuid.UUID, content string) (Note, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return Note{}, ErrEmptyNote
	}
	if err := s.reachable(ctx, scope, conversationID); err != nil {
		return Note{}, err
	}

	noteID := id.New()
	if err := s.opts.Repo.AddNote(ctx, Note{ID: noteID, ConversationID: conversationID, Author: Operator{ID: scope.OperatorID}, Content: content}); err != nil {
		return Note{}, err
	}
	return s.opts.Repo.NoteByID(ctx, conversationID, noteID)
}

// EditNote rewrites the text of a note, only by its author: the note is signed with that name (ADR-0056).
func (s *Service) EditNote(ctx context.Context, scope Scope, conversationID, noteID uuid.UUID, content string) (Note, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return Note{}, ErrEmptyNote
	}
	if err := s.authored(ctx, scope, conversationID, noteID); err != nil {
		return Note{}, err
	}
	if err := s.opts.Repo.EditNote(ctx, noteID, scope.OperatorID, content); err != nil {
		return Note{}, err
	}
	return s.opts.Repo.NoteByID(ctx, conversationID, noteID)
}

// DeleteNote takes a note out of the thread and keeps the row, only by its author (C09, ADR-0056).
func (s *Service) DeleteNote(ctx context.Context, scope Scope, conversationID, noteID uuid.UUID) error {
	if err := s.authored(ctx, scope, conversationID, noteID); err != nil {
		return err
	}
	return s.opts.Repo.DeleteNote(ctx, noteID, scope.OperatorID)
}

// authored is the reach of the thread and then the author, in that order: a note of a conversation it cannot see does not exist.
func (s *Service) authored(ctx context.Context, scope Scope, conversationID, noteID uuid.UUID) error {
	if err := s.reachable(ctx, scope, conversationID); err != nil {
		return err
	}
	note, err := s.opts.Repo.NoteByID(ctx, conversationID, noteID)
	if err != nil {
		return err
	}
	if note.Author.ID != scope.OperatorID {
		return ErrNotTheAuthor
	}
	return nil
}
