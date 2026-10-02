package notes

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/inbox"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// store is the slice of the repository the service uses, declared here so the caller side owns the interface.
type store interface {
	Notes(ctx context.Context, conversationID uuid.UUID) ([]Note, error)
	NoteByID(ctx context.Context, conversationID, noteID uuid.UUID) (Note, error)
	AddNote(ctx context.Context, n Note) error
	EditNote(ctx context.Context, noteID, operatorID uuid.UUID, content string) error
	DeleteNote(ctx context.Context, noteID, operatorID uuid.UUID) error
}

type Service struct {
	repo  store
	reach Reach
}

func NewService(repo store, reach Reach) *Service { return &Service{repo: repo, reach: reach} }

// Notes reads the internal notes of F26, oldest first, with the reach of the thread: they are for the team and never reach the customer.
func (s *Service) Notes(ctx context.Context, scope inbox.Scope, conversationID uuid.UUID) ([]Note, error) {
	if err := s.reach.Reachable(ctx, scope, conversationID); err != nil {
		return nil, err
	}
	return s.repo.Notes(ctx, conversationID)
}

// AddNote writes with the reach of the thread too, on a resolved conversation as well: a note is what the next vendor reads (ADR-0056).
func (s *Service) AddNote(ctx context.Context, scope inbox.Scope, conversationID uuid.UUID, content string) (Note, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return Note{}, ErrEmptyNote
	}
	if err := s.reach.Reachable(ctx, scope, conversationID); err != nil {
		return Note{}, err
	}

	noteID := id.New()
	if err := s.repo.AddNote(ctx, Note{ID: noteID, ConversationID: conversationID, Author: Author{ID: scope.OperatorID}, Content: content}); err != nil {
		return Note{}, err
	}
	return s.repo.NoteByID(ctx, conversationID, noteID)
}

// EditNote rewrites the text of a note, only by its author: the note is signed with that name (ADR-0056).
func (s *Service) EditNote(ctx context.Context, scope inbox.Scope, conversationID, noteID uuid.UUID, content string) (Note, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return Note{}, ErrEmptyNote
	}
	if err := s.authored(ctx, scope, conversationID, noteID); err != nil {
		return Note{}, err
	}
	if err := s.repo.EditNote(ctx, noteID, scope.OperatorID, content); err != nil {
		return Note{}, err
	}
	return s.repo.NoteByID(ctx, conversationID, noteID)
}

// DeleteNote takes a note out of the thread and keeps the row, only by its author (C09, ADR-0056).
func (s *Service) DeleteNote(ctx context.Context, scope inbox.Scope, conversationID, noteID uuid.UUID) error {
	if err := s.authored(ctx, scope, conversationID, noteID); err != nil {
		return err
	}
	return s.repo.DeleteNote(ctx, noteID, scope.OperatorID)
}

// authored is the reach of the thread and then the author, in that order: a note of a conversation it cannot see does not exist.
func (s *Service) authored(ctx context.Context, scope inbox.Scope, conversationID, noteID uuid.UUID) error {
	if err := s.reach.Reachable(ctx, scope, conversationID); err != nil {
		return err
	}
	note, err := s.repo.NoteByID(ctx, conversationID, noteID)
	if err != nil {
		return err
	}
	if note.Author.ID != scope.OperatorID {
		return ErrNotTheAuthor
	}
	return nil
}
