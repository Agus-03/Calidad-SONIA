package inbox

import (
	"context"
	"errors"
	"testing"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// TestNotesAndHistoryAgainstRealDDL is ADR-0056 through the repository: who receives, notes kept by their author, a named history.
func TestNotesAndHistoryAgainstRealDDL(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor) {
		w := seedWorld(t, ctx, exec)
		repo := NewRepository(exec)

		if available, err := repo.OperatorState(ctx, w.account, w.mine); err != nil || !available {
			t.Fatalf("an active operator: got %v and %v, want available", available, err)
		}
		if _, err := exec.Exec(ctx, `UPDATE operators SET active = false WHERE id = $1`, w.theirs); err != nil {
			t.Fatalf("disable an operator: %v", err)
		}
		if available, err := repo.OperatorState(ctx, w.account, w.theirs); err != nil || available {
			t.Fatalf("a disabled operator: got %v and %v, want unavailable", available, err)
		}
		if _, err := repo.OperatorState(ctx, id.New(), w.mine); !errors.Is(err, ErrOperatorNotFound) {
			t.Fatalf("an operator of another account: got %v, want ErrOperatorNotFound", err)
		}

		first, second := id.New(), id.New()
		for _, n := range []Note{
			{ID: first, ConversationID: w.mineConv, Author: Operator{ID: w.mine}, Content: "primera"},
			{ID: second, ConversationID: w.mineConv, Author: Operator{ID: w.theirs}, Content: "segunda"},
		} {
			if err := repo.AddNote(ctx, n); err != nil {
				t.Fatalf("AddNote: %v", err)
			}
		}
		notes, err := repo.Notes(ctx, w.mineConv)
		if err != nil || len(notes) != 2 || notes[0].ID != first || notes[0].Author.Name != "Prueba" {
			t.Fatalf("Notes: got %+v and %v, want both, oldest first, signed", notes, err)
		}
		if err := repo.EditNote(ctx, second, w.mine, "ajena"); !errors.Is(err, ErrNoteNotFound) {
			t.Fatalf("editing another's note: got %v, want ErrNoteNotFound", err)
		}
		if err := repo.EditNote(ctx, first, w.mine, "corregida"); err != nil {
			t.Fatalf("EditNote: %v", err)
		}
		if n, err := repo.NoteByID(ctx, w.mineConv, first); err != nil || n.Content != "corregida" {
			t.Fatalf("NoteByID after the edit: got %+v and %v", n, err)
		}
		if err := repo.DeleteNote(ctx, first, w.mine); err != nil {
			t.Fatalf("DeleteNote: %v", err)
		}
		if _, err := repo.NoteByID(ctx, w.mineConv, first); !errors.Is(err, ErrNoteNotFound) {
			t.Fatalf("a deleted note: got %v, want ErrNoteNotFound", err)
		}
		if _, err := repo.NoteByID(ctx, w.theirsConv, second); !errors.Is(err, ErrNoteNotFound) {
			t.Fatalf("a note read through another conversation: got %v, want ErrNoteNotFound", err)
		}
		var kept int
		if err := exec.QueryRow(ctx, `SELECT count(*) FROM internal_notes WHERE id = $1`, first).Scan(&kept); err != nil || kept != 1 {
			t.Fatalf("the deleted note's row: %d and %v, want it kept (C09)", kept, err)
		}

		for _, a := range []Assignment{
			{ID: id.New(), ConversationID: w.pooledConv, ToOperatorID: w.mine, Reason: "auto_assigned_on_first_reply", AssignedByOperatorID: &w.mine},
			{ID: id.New(), ConversationID: w.pooledConv, FromOperatorID: &w.mine, ToOperatorID: w.theirs, AssignedByOperatorID: &w.mine, Reason: "ausencia"},
		} {
			if err := repo.RecordAssignment(ctx, a); err != nil {
				t.Fatalf("RecordAssignment: %v", err)
			}
		}
		history, err := repo.Assignments(ctx, w.pooledConv)
		if err != nil || len(history) < 2 {
			t.Fatalf("Assignments: got %+v and %v, want the two written", history, err)
		}
		taken, moved := history[len(history)-2], history[len(history)-1]
		if taken.From != nil || taken.To.ID != w.mine || moved.From == nil || moved.From.ID != w.mine ||
			moved.To.ID != w.theirs || moved.AssignedBy == nil || moved.Reason != "ausencia" {
			t.Fatalf("the history reads %+v then %+v, want the pool taken and then moved, with names and reason", taken, moved)
		}
	})
}
