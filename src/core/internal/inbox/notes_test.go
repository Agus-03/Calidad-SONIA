package inbox

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// TestWhoReadsAndWritesTheNotesIsWhoReadsTheThread is ADR-0056 for the notes: the reach of the thread, on any status.
func TestWhoReadsAndWritesTheNotesIsWhoReadsTheThread(t *testing.T) {
	me, other := id.New(), id.New()

	for _, tc := range []struct {
		name    string
		holder  *uuid.UUID
		role    enums.OperatorRole
		status  enums.ConversationStatus
		content string
		wantErr error
	}{
		{"the vendor notes its own", &me, enums.OperatorRoleOperator, enums.ConversationStatusActive, "llamar el lunes", nil},
		{"an operator notes one of the pool", nil, enums.OperatorRoleOperator, enums.ConversationStatusActive, "nota", nil},
		{"a supervisor notes a vendor's", &other, enums.OperatorRoleSupervisor, enums.ConversationStatusActive, "nota", nil},
		{"a resolved one takes notes too", &me, enums.OperatorRoleOperator, enums.ConversationStatusResolved, "nota", nil},
		{"another's does not exist for an operator", &other, enums.OperatorRoleOperator, enums.ConversationStatusActive, "nota", ErrNotFound},
		{"an empty note says nothing", &me, enums.OperatorRoleOperator, enums.ConversationStatusActive, "  ", ErrEmptyNote},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			target := d.open(tc.holder)
			target.Status = tc.status
			d.store.targets[target.ConversationID] = target

			note, err := d.service.AddNote(context.Background(), scopeOf(me, tc.role), target.ConversationID, tc.content)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("AddNote: got %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if len(d.store.notes) != 0 {
					t.Fatal("a refused note was written")
				}
				return
			}
			if note.Author.ID != me || note.Content != strings.TrimSpace(tc.content) || note.ConversationID != target.ConversationID {
				t.Fatalf("the note %+v is not the caller's text on this conversation", note)
			}
			listed, err := d.service.Notes(context.Background(), scopeOf(me, tc.role), target.ConversationID)
			if err != nil || len(listed) != 1 {
				t.Fatalf("Notes: got %v and %v, want the one written", listed, err)
			}
			// A note is not a message: it reaches no inbox and moves no row (ADR-0056).
			if len(d.events.told) != 0 || d.sender.calls != 0 {
				t.Fatal("a note told the hub or reached the customer")
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
			func(d *desk, _ uuid.UUID) uuid.UUID { return d.note(d.open(nil).ConversationID, me) }, ErrNoteNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			target := d.open(nil)
			noteID := tc.note(d, target.ConversationID)
			scope := scopeOf(tc.caller, tc.role)

			edited, err := d.service.EditNote(context.Background(), scope, target.ConversationID, noteID, " corregida ")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("EditNote: got %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && edited.Content != "corregida" {
				t.Fatalf("the note reads %q after the edit, want the trimmed text", edited.Content)
			}
			if err := d.service.DeleteNote(context.Background(), scope, target.ConversationID, noteID); !errors.Is(err, tc.wantErr) {
				t.Fatalf("DeleteNote: got %v, want %v", err, tc.wantErr)
			}
			listed, _ := d.service.Notes(context.Background(), scopeOf(me, enums.OperatorRoleAdmin), target.ConversationID)
			if gone := len(listed) == 0; tc.wantErr == nil && !gone {
				t.Fatal("a deleted note is still in the thread")
			}
		})
	}

	d := newDesk(&stubStore{})
	target := d.open(nil)
	if _, err := d.service.EditNote(context.Background(), scopeOf(me, enums.OperatorRoleOperator), target.ConversationID, d.note(target.ConversationID, me), ""); !errors.Is(err, ErrEmptyNote) {
		t.Fatalf("an edit to nothing: got %v, want ErrEmptyNote", err)
	}
}

// TestTheNoteRoutesAnswerWithTheirCodes walks the four routes, because a wrong method or path compiles (ADR-0056).
func TestTheNoteRoutesAnswerWithTheirCodes(t *testing.T) {
	d := newDesk(&stubStore{})
	target := d.open(&testOperator)
	base := "/conversations/" + target.ConversationID.String() + "/notes"
	engine := mountDesk(enums.OperatorRoleOperator, d)

	if rec := postJSON(engine, http.MethodPost, base, `{"content":"llamar el lunes"}`); rec.Code != http.StatusCreated ||
		!strings.Contains(rec.Body.String(), `"content":"llamar el lunes"`) {
		t.Fatalf("POST: got %d - body %s, want 201 with the note", rec.Code, rec.Body)
	}
	var noteID string
	for k := range d.store.notes {
		noteID = k.String()
	}
	for _, tc := range []struct {
		method, path, body string
		want               int
		code               string
	}{
		{http.MethodGet, base, "", http.StatusOK, ""},
		{http.MethodPatch, base + "/" + noteID, `{"content":"el martes"}`, http.StatusOK, ""},
		{http.MethodPatch, base + "/" + noteID, `{"content":""}`, http.StatusBadRequest, "invalid_content"},
		{http.MethodPatch, base + "/no-es-un-id", `{"content":"x"}`, http.StatusBadRequest, "invalid_note_id"},
		{http.MethodPatch, base + "/" + id.New().String(), `{"content":"x"}`, http.StatusNotFound, "note_not_found"},
		{http.MethodPost, base, `no`, http.StatusBadRequest, "invalid_body"},
		{http.MethodDelete, base + "/" + noteID, "", http.StatusNoContent, ""},
	} {
		rec := postJSON(engine, tc.method, tc.path, tc.body)
		if rec.Code != tc.want {
			t.Fatalf("%s %s: got %d, want %d - body %s", tc.method, tc.path, rec.Code, tc.want, rec.Body)
		}
		if tc.code != "" {
			if got := errorCode(t, rec); got != tc.code {
				t.Fatalf("%s %s: code %q, want %q", tc.method, tc.path, got, tc.code)
			}
		}
	}

	other := newDesk(&stubStore{})
	theirs := other.open(&otherOperator)
	noteOfOther := other.note(theirs.ConversationID, otherOperator)
	rec := postJSON(mountDesk(enums.OperatorRoleAdmin, other), http.MethodDelete,
		"/conversations/"+theirs.ConversationID.String()+"/notes/"+noteOfOther.String(), "")
	if rec.Code != http.StatusForbidden || errorCode(t, rec) != "not_the_author" {
		t.Fatalf("an admin deleting another's note: got %d - body %s, want 403 not_the_author", rec.Code, rec.Body)
	}
}
