package notes

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/inbox"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

var (
	testAccount   = id.New()
	testOperator  = id.New()
	otherOperator = id.New()
)

// stubReach is the rule inbox/ owns, imitated: the holder or the pool for an operator, any for the roles above (ADR-0035, regla 2).
type stubReach struct{ holders map[uuid.UUID]*uuid.UUID }

func (r *stubReach) Reachable(_ context.Context, scope inbox.Scope, conversationID uuid.UUID) error {
	holder, ok := r.holders[conversationID]
	if !ok || (scope.Role == enums.OperatorRoleOperator && holder != nil && *holder != scope.OperatorID) {
		return inbox.ErrNotFound
	}
	return nil
}

// stubStore answers from memory, so the service is tested against its own rules and not against the SQL that enforces them.
type stubStore struct{ notes map[uuid.UUID]Note }

func (s *stubStore) Notes(_ context.Context, conversationID uuid.UUID) ([]Note, error) {
	out := []Note{}
	for _, n := range s.notes {
		if n.ConversationID == conversationID {
			out = append(out, n)
		}
	}
	return out, nil
}

func (s *stubStore) NoteByID(_ context.Context, conversationID, noteID uuid.UUID) (Note, error) {
	n, ok := s.notes[noteID]
	if !ok || n.ConversationID != conversationID {
		return Note{}, ErrNoteNotFound
	}
	return n, nil
}

func (s *stubStore) AddNote(_ context.Context, n Note) error {
	n.CreatedAt, n.UpdatedAt = time.Now(), time.Now()
	s.notes[n.ID] = n
	return nil
}

func (s *stubStore) EditNote(_ context.Context, noteID, operatorID uuid.UUID, content string) error {
	n, ok := s.notes[noteID]
	if !ok || n.Author.ID != operatorID {
		return ErrNoteNotFound
	}
	n.Content = content
	s.notes[noteID] = n
	return nil
}

func (s *stubStore) DeleteNote(_ context.Context, noteID, operatorID uuid.UUID) error {
	n, ok := s.notes[noteID]
	if !ok || n.Author.ID != operatorID {
		return ErrNoteNotFound
	}
	delete(s.notes, noteID)
	return nil
}

// desk is the service over both doubles, with helpers to open a conversation and to write a note the way AddNote would.
type desk struct {
	store   *stubStore
	reach   *stubReach
	service *Service
}

func newDesk() *desk {
	d := &desk{store: &stubStore{notes: map[uuid.UUID]Note{}}, reach: &stubReach{holders: map[uuid.UUID]*uuid.UUID{}}}
	d.service = NewService(d.store, d.reach)
	return d
}

func (d *desk) open(holder *uuid.UUID) uuid.UUID {
	conversationID := id.New()
	d.reach.holders[conversationID] = holder
	return conversationID
}

func (d *desk) note(conversationID, author uuid.UUID) uuid.UUID {
	noteID := id.New()
	_ = d.store.AddNote(context.Background(), Note{ID: noteID, ConversationID: conversationID, Author: Author{ID: author}, Content: "nota"})
	return noteID
}

func scopeOf(operatorID uuid.UUID, role enums.OperatorRole) inbox.Scope {
	return inbox.Scope{AccountID: testAccount, OperatorID: operatorID, Role: role}
}

// TestWhoReadsAndWritesTheNotesIsWhoReadsTheThread is ADR-0056 for the notes: the reach of the thread, asked of inbox/.
func TestWhoReadsAndWritesTheNotesIsWhoReadsTheThread(t *testing.T) {
	me, other := id.New(), id.New()

	for _, tc := range []struct {
		name    string
		holder  *uuid.UUID
		role    enums.OperatorRole
		content string
		wantErr error
	}{
		{"the vendor notes its own", &me, enums.OperatorRoleOperator, "llamar el lunes", nil},
		{"an operator notes one of the pool", nil, enums.OperatorRoleOperator, "nota", nil},
		{"a supervisor notes a vendor's", &other, enums.OperatorRoleSupervisor, "nota", nil},
		{"another's does not exist for an operator", &other, enums.OperatorRoleOperator, "nota", inbox.ErrNotFound},
		{"an empty note says nothing", &me, enums.OperatorRoleOperator, "  ", ErrEmptyNote},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk()
			conversationID := d.open(tc.holder)

			note, err := d.service.AddNote(context.Background(), scopeOf(me, tc.role), conversationID, tc.content)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("AddNote: got %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if len(d.store.notes) != 0 {
					t.Fatal("a refused note was written")
				}
				return
			}
			if note.Author.ID != me || note.Content != strings.TrimSpace(tc.content) || note.ConversationID != conversationID {
				t.Fatalf("the note %+v is not the caller's text on this conversation", note)
			}
			listed, err := d.service.Notes(context.Background(), scopeOf(me, tc.role), conversationID)
			if err != nil || len(listed) != 1 {
				t.Fatalf("Notes: got %v and %v, want the one written", listed, err)
			}
		})
	}
}

// TestOnlyTheAuthorEditsOrDeletesANote: the note is signed, so nobody else, not even an admin, rewrites it (ADR-0056).
func TestOnlyTheAuthorEditsOrDeletesANote(t *testing.T) {
	me, other := id.New(), id.New()

	for _, tc := range []struct {
		name    string
		caller  uuid.UUID
		role    enums.OperatorRole
		note    func(d *desk, conversationID uuid.UUID) uuid.UUID
		wantErr error
	}{
		{"the author", me, enums.OperatorRoleOperator, func(d *desk, c uuid.UUID) uuid.UUID { return d.note(c, me) }, nil},
		{"an admin who did not write it", other, enums.OperatorRoleAdmin, func(d *desk, c uuid.UUID) uuid.UUID { return d.note(c, me) }, ErrNotTheAuthor},
		{"a note that does not exist", me, enums.OperatorRoleOperator, func(*desk, uuid.UUID) uuid.UUID { return id.New() }, ErrNoteNotFound},
		{"a note of another conversation", me, enums.OperatorRoleOperator,
			func(d *desk, _ uuid.UUID) uuid.UUID { return d.note(d.open(nil), me) }, ErrNoteNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk()
			conversationID := d.open(nil)
			noteID := tc.note(d, conversationID)
			scope := scopeOf(tc.caller, tc.role)

			edited, err := d.service.EditNote(context.Background(), scope, conversationID, noteID, " corregida ")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("EditNote: got %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && edited.Content != "corregida" {
				t.Fatalf("the note reads %q after the edit, want the trimmed text", edited.Content)
			}
			if err := d.service.DeleteNote(context.Background(), scope, conversationID, noteID); !errors.Is(err, tc.wantErr) {
				t.Fatalf("DeleteNote: got %v, want %v", err, tc.wantErr)
			}
			listed, _ := d.service.Notes(context.Background(), scopeOf(me, enums.OperatorRoleAdmin), conversationID)
			if gone := len(listed) == 0; tc.wantErr == nil && !gone {
				t.Fatal("a deleted note is still in the thread")
			}
		})
	}

	d := newDesk()
	conversationID := d.open(nil)
	if _, err := d.service.EditNote(context.Background(), scopeOf(me, enums.OperatorRoleOperator), conversationID, d.note(conversationID, me), ""); !errors.Is(err, ErrEmptyNote) {
		t.Fatalf("an edit to nothing: got %v, want ErrEmptyNote", err)
	}
	if _, err := d.service.EditNote(context.Background(), scopeOf(me, enums.OperatorRoleOperator), d.open(&other), id.New(), "x"); !errors.Is(err, inbox.ErrNotFound) {
		t.Fatalf("a note of a thread out of reach: got %v, want inbox.ErrNotFound", err)
	}
}
