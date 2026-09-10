package contacts

import (
	"context"

	"github.com/google/uuid"
)

// stubStore answers from memory, so the service and the handlers are tested against their own rules and not against the SQL that enforces them.
//
// It carries the reach on purpose: the narrowing is what the handlers rest on, so a double that ignored it would let a case pass
// while the real query leaked. assigned says which contacts an operator has a conversation with (ADR-0032, regla 2).
type stubStore struct {
	rows     []Contact
	assigned map[uuid.UUID][]uuid.UUID
	err      error
}

func newStubStore(rows ...Contact) *stubStore {
	return &stubStore{rows: rows, assigned: map[uuid.UUID][]uuid.UUID{}}
}

// assign is the seeding the integration test does with real conversations, done here as a map.
func (s *stubStore) assign(operatorID, contactID uuid.UUID) {
	s.assigned[operatorID] = append(s.assigned[operatorID], contactID)
}

func (s *stubStore) reaches(scope Scope, c Contact) bool {
	if c.AccountID != scope.AccountID {
		return false
	}
	if scope.reachesEveryContact() {
		return true
	}
	for _, id := range s.assigned[scope.OperatorID] {
		if id == c.ID {
			return true
		}
	}
	return false
}

func (s *stubStore) List(_ context.Context, scope Scope, limit, offset int) (Page, error) {
	if s.err != nil {
		return Page{}, s.err
	}

	reached := []Contact{}
	for _, c := range s.rows {
		if s.reaches(scope, c) {
			// The listing never carries the four, exactly as the query that does not select them (ADR-0032, regla 4).
			c.Sensitive = Sensitive{}
			reached = append(reached, c)
		}
	}

	page := Page{Items: []Contact{}, Total: len(reached)}
	if offset >= len(reached) {
		return page, nil
	}
	end := offset + limit
	if end > len(reached) {
		end = len(reached)
	}
	page.Items = append(page.Items, reached[offset:end]...)
	return page, nil
}

func (s *stubStore) ByID(_ context.Context, scope Scope, contactID uuid.UUID) (Contact, error) {
	if s.err != nil {
		return Contact{}, s.err
	}
	for _, c := range s.rows {
		if c.ID == contactID && s.reaches(scope, c) {
			return c, nil
		}
	}
	return Contact{}, ErrNotFound
}

func (s *stubStore) Create(_ context.Context, c Contact) error {
	if s.err != nil {
		return s.err
	}
	for _, existing := range s.rows {
		if normalizePhone(existing.PhoneWhatsapp) == normalizePhone(c.PhoneWhatsapp) {
			return ErrDuplicatePhone
		}
	}
	c.PhoneWhatsapp = normalizePhone(c.PhoneWhatsapp)
	s.rows = append(s.rows, c)
	return nil
}

func (s *stubStore) Update(_ context.Context, accountID, contactID uuid.UUID, ch Changes) error {
	if s.err != nil {
		return s.err
	}
	for i, c := range s.rows {
		if c.ID == contactID && c.AccountID == accountID {
			s.rows[i].Name, s.rows[i].Lastname, s.rows[i].Email = ch.Name, ch.Lastname, ch.Email
			s.rows[i].Type, s.rows[i].Notes = ch.Type, ch.Notes
			s.rows[i].OptOut, s.rows[i].Blocked = ch.OptOut, ch.Blocked
			return nil
		}
	}
	return ErrNotFound
}

func (s *stubStore) UpdateSensitive(_ context.Context, accountID, contactID uuid.UUID, sensitive Sensitive) error {
	if s.err != nil {
		return s.err
	}
	for i, c := range s.rows {
		if c.ID == contactID && c.AccountID == accountID {
			s.rows[i].Sensitive = sensitive
			return nil
		}
	}
	return ErrNotFound
}

func (s *stubStore) SoftDelete(_ context.Context, accountID, contactID uuid.UUID) error {
	if s.err != nil {
		return s.err
	}
	for i, c := range s.rows {
		if c.ID == contactID && c.AccountID == accountID {
			s.rows = append(s.rows[:i], s.rows[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}
