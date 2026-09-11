package contacts

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/labels"
)

// stubStore answers from memory, so the service and the handlers are tested against their own rules and not against the SQL that enforces them.
//
// It carries the reach on purpose: the narrowing is what the handlers rest on, so a double that ignored it would let a case pass
// while the real query leaked. assigned says which contacts an operator has a conversation with (ADR-0032, regla 2).
type stubStore struct {
	rows     []Contact
	assigned map[uuid.UUID][]uuid.UUID
	tags     map[uuid.UUID][]uuid.UUID
	err      error
}

func newStubStore(rows ...Contact) *stubStore {
	return &stubStore{rows: rows, assigned: map[uuid.UUID][]uuid.UUID{}, tags: map[uuid.UUID][]uuid.UUID{}}
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

// matches is the filter as the SQL means it, kept here so a case can be written against the double and still be about the rule.
func (s *stubStore) matches(filter Filter, c Contact) bool {
	if len(filter.LabelIDs) > 0 {
		carried := 0
		for _, wanted := range filter.LabelIDs {
			for _, has := range s.tags[c.ID] {
				if has == wanted {
					carried++
					break
				}
			}
		}
		if filter.Match == MatchAny && carried == 0 {
			return false
		}
		if filter.Match != MatchAny && carried != len(filter.LabelIDs) {
			return false
		}
	}

	if term := strings.TrimSpace(filter.Search); term != "" {
		whole := strings.ToLower(c.Name + " " + c.Lastname)
		hit := strings.Contains(whole, strings.ToLower(term))
		if digits := normalizePhone(term); !hit && digits != "" {
			hit = strings.Contains(normalizePhone(c.PhoneWhatsapp), digits)
		}
		if !hit {
			return false
		}
	}
	return true
}

func (s *stubStore) List(_ context.Context, scope Scope, filter Filter, limit, offset int) (Page, error) {
	if s.err != nil {
		return Page{}, s.err
	}

	reached := []Contact{}
	for _, c := range s.rows {
		if s.reaches(scope, c) && s.matches(filter, c) {
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

// The link table as a map, keeping the one property the SQL has that a slice would not: the unique constraint, which is what
// makes tagging twice a no-op instead of a second row.
func (s *stubStore) AssignLabel(_ context.Context, link ContactLabel) error {
	if s.err != nil {
		return s.err
	}
	for _, existing := range s.tags[link.ContactID] {
		if existing == link.LabelID {
			return nil
		}
	}
	s.tags[link.ContactID] = append(s.tags[link.ContactID], link.LabelID)
	return nil
}

// Removing a tag that is not there is not an error here either, matching the DELETE that removes no row.
func (s *stubStore) UnassignLabel(_ context.Context, contactID, labelID uuid.UUID) error {
	if s.err != nil {
		return s.err
	}
	kept := []uuid.UUID{}
	for _, existing := range s.tags[contactID] {
		if existing != labelID {
			kept = append(kept, existing)
		}
	}
	s.tags[contactID] = kept
	return nil
}

func (s *stubStore) LabelIDsOf(_ context.Context, contactID uuid.UUID) ([]uuid.UUID, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]uuid.UUID{}, s.tags[contactID]...), nil
}

// stubCatalogue is labels/ seen through the only two questions this package asks it. It is scoped by account on purpose: the
// case worth testing is a label id that exists and belongs to somebody else, which the foreign key would happily accept.
type stubCatalogue struct{ rows []labels.Label }

func newStubCatalogue(rows ...labels.Label) *stubCatalogue { return &stubCatalogue{rows: rows} }

func (c *stubCatalogue) ByID(_ context.Context, accountID, labelID uuid.UUID) (labels.Label, error) {
	for _, l := range c.rows {
		if l.ID == labelID && l.AccountID == accountID {
			return l, nil
		}
	}
	return labels.Label{}, labels.ErrNotFound
}

func (c *stubCatalogue) ByIDs(_ context.Context, accountID uuid.UUID, ids []uuid.UUID) ([]labels.Label, error) {
	found := []labels.Label{}
	for _, l := range c.rows {
		if l.AccountID != accountID {
			continue
		}
		for _, wanted := range ids {
			if l.ID == wanted {
				found = append(found, l)
				break
			}
		}
	}
	return found, nil
}
