package reminders

import (
	"bytes"
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/contacts"
)

// stubStore answers from memory, so the service is tested against its own rules and not against the SQL.
type stubStore struct {
	rows          map[uuid.UUID]Reminder
	conversations map[uuid.UUID]uuid.UUID // conversation -> contact
	// addressed is who each reminder goes to, the rule the real query computes from the conversations (ADR-0058).
	addressed map[uuid.UUID]uuid.UUID
	due       []Due
	dueErr    error
}

func newStore() *stubStore {
	return &stubStore{rows: map[uuid.UUID]Reminder{}, conversations: map[uuid.UUID]uuid.UUID{}, addressed: map[uuid.UUID]uuid.UUID{}}
}

func sorted(rs []Reminder) []Reminder {
	sort.Slice(rs, func(i, j int) bool {
		if !rs[i].DueAt.Equal(rs[j].DueAt) {
			return rs[i].DueAt.Before(rs[j].DueAt)
		}
		return bytes.Compare(rs[i].ID[:], rs[j].ID[:]) < 0
	})
	return rs
}

func (s *stubStore) ByID(_ context.Context, _, reminderID uuid.UUID) (Reminder, error) {
	r, ok := s.rows[reminderID]
	if !ok {
		return Reminder{}, ErrNotFound
	}
	return r, nil
}

func (s *stubStore) OfContact(_ context.Context, _, contactID uuid.UUID) ([]Reminder, error) {
	out := []Reminder{}
	for _, r := range s.rows {
		if r.ContactID == contactID {
			out = append(out, r)
		}
	}
	return sorted(out), nil
}

func (s *stubStore) Addressed(_ context.Context, _, operatorID uuid.UUID, onlyDue bool) ([]Reminder, error) {
	out := []Reminder{}
	for _, r := range s.rows {
		to, ok := s.addressed[r.ID]
		if !ok {
			to = r.OperatorID
		}
		if to == operatorID && (!onlyDue || !r.DueAt.After(time.Now())) {
			out = append(out, r)
		}
	}
	return sorted(out), nil
}

func (s *stubStore) ConversationOfContact(_ context.Context, conversationID, contactID uuid.UUID) (bool, error) {
	return s.conversations[conversationID] == contactID, nil
}

func (s *stubStore) Create(_ context.Context, r Reminder) error {
	s.rows[r.ID] = r
	return nil
}

func (s *stubStore) Update(_ context.Context, reminderID uuid.UUID, description string, dueAt time.Time) error {
	r, ok := s.rows[reminderID]
	if !ok {
		return ErrNotFound
	}
	if !r.DueAt.Equal(dueAt) {
		r.Notified, r.NotifiedAt = false, nil
	}
	r.Description, r.DueAt = description, dueAt
	s.rows[reminderID] = r
	return nil
}

func (s *stubStore) Delete(_ context.Context, reminderID uuid.UUID) error {
	if _, ok := s.rows[reminderID]; !ok {
		return ErrNotFound
	}
	delete(s.rows, reminderID)
	return nil
}

func (s *stubStore) Due(context.Context, int) ([]Due, error) { return s.due, s.dueErr }

// stubContacts is contacts/ as this package asks it: the caller reaches the contact or it does not exist for it.
type stubContacts struct{ reached map[uuid.UUID]bool }

func (c *stubContacts) ByID(_ context.Context, _ contacts.Scope, contactID uuid.UUID) (contacts.Contact, error) {
	if !c.reached[contactID] {
		return contacts.Contact{}, contacts.ErrNotFound
	}
	return contacts.Contact{ID: contactID}, nil
}

// stubAlerts records who was told what, guarded because the poller tells from its own goroutine.
type stubAlerts struct {
	mu   sync.Mutex
	told []uuid.UUID
}

func (a *stubAlerts) Remind(recipient uuid.UUID, _ any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.told = append(a.told, recipient)
}

// clock is the instant every case of the service calls now, so "in the past" never depends on how fast a test runs.
var clock = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

func newService(store *stubStore, reached ...uuid.UUID) *Service {
	book := &stubContacts{reached: map[uuid.UUID]bool{}}
	for _, c := range reached {
		book.reached[c] = true
	}
	s := NewService(store, book, slog.New(slog.DiscardHandler))
	s.now = func() time.Time { return clock }
	return s
}
