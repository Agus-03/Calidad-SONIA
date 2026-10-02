package inbox

import (
	"context"
	"errors"
	"testing"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// TestHistoryAgainstRealDDL is ADR-0056 through the repository: who can receive, and a history read with names and reason.
func TestHistoryAgainstRealDDL(t *testing.T) {
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
