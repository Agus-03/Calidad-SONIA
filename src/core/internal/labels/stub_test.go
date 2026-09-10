package labels

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

// stubStore answers from memory, so the service is tested against its own rules and not against the SQL that enforces them.
type stubStore struct {
	rows []Label
	// Set to make a read or a write fail, which is how a test reaches the branch that lets an unknown error through as a 500.
	err error
}

func newStubStore(rows ...Label) *stubStore { return &stubStore{rows: rows} }

// Folded the same way the functional index folds it, because a stub that is stricter than the database proves nothing.
func (s *stubStore) taken(accountID uuid.UUID, name string, except uuid.UUID) bool {
	for _, l := range s.rows {
		if l.AccountID == accountID && l.ID != except && strings.EqualFold(l.Name, normalizeName(name)) {
			return true
		}
	}
	return false
}

func (s *stubStore) List(_ context.Context, accountID uuid.UUID, limit, offset int) (Page, error) {
	if s.err != nil {
		return Page{}, s.err
	}

	mine := []Label{}
	for _, l := range s.rows {
		if l.AccountID == accountID {
			mine = append(mine, l)
		}
	}

	page := Page{Items: []Label{}, Total: len(mine)}
	if offset >= len(mine) {
		return page, nil
	}
	end := offset + limit
	if end > len(mine) {
		end = len(mine)
	}
	page.Items = append(page.Items, mine[offset:end]...)
	return page, nil
}

func (s *stubStore) ByID(_ context.Context, accountID, labelID uuid.UUID) (Label, error) {
	if s.err != nil {
		return Label{}, s.err
	}
	for _, l := range s.rows {
		if l.ID == labelID && l.AccountID == accountID {
			return l, nil
		}
	}
	return Label{}, ErrNotFound
}

func (s *stubStore) Create(_ context.Context, l Label) error {
	if s.err != nil {
		return s.err
	}
	if s.taken(l.AccountID, l.Name, uuid.Nil) {
		return ErrDuplicateName
	}
	l.Name = normalizeName(l.Name)
	s.rows = append(s.rows, l)
	return nil
}

func (s *stubStore) Update(_ context.Context, accountID, labelID uuid.UUID, name, color string) error {
	if s.err != nil {
		return s.err
	}
	if s.taken(accountID, name, labelID) {
		return ErrDuplicateName
	}
	for i, l := range s.rows {
		if l.ID == labelID && l.AccountID == accountID {
			s.rows[i].Name, s.rows[i].Color = normalizeName(name), color
			return nil
		}
	}
	return ErrNotFound
}

func (s *stubStore) SoftDelete(_ context.Context, accountID, labelID uuid.UUID) error {
	if s.err != nil {
		return s.err
	}
	for i, l := range s.rows {
		if l.ID == labelID && l.AccountID == accountID {
			s.rows = append(s.rows[:i], s.rows[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}
