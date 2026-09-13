package conversations

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
)

// TestTheScopeReachesOnlyWhatIsAssignedNow is ADR-0032, reglas 2 and 3, against real DDL: an operator reaches the contact of a
// conversation it holds right now, and not one in the pool, one held by somebody else or one that was deleted.
func TestTheScopeReachesOnlyWhatIsAssignedNow(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account, inbox, agent := seeded(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		service := NewService(NewRepository(exec), nil)

		me, err := insertOperator(ctx, exec, account)
		if err != nil {
			t.Fatalf("insert operator: %v", err)
		}
		other, err := insertOperator(ctx, exec, account)
		if err != nil {
			t.Fatalf("insert operator: %v", err)
		}

		// open gives each case a contact of its own, because a contact holds one open conversation at a time (ADR-0002).
		open := func(holder *uuid.UUID, deleted bool) uuid.UUID {
			t.Helper()
			contact, err := insertContact(ctx, exec, account)
			if err != nil {
				t.Fatalf("insert contact: %v", err)
			}
			conversation, err := service.EnsureOpen(ctx, contact, inbox, &agent, time.Now().UTC())
			if err != nil {
				t.Fatalf("EnsureOpen: %v", err)
			}
			if _, err := exec.Exec(ctx,
				`UPDATE conversations SET operator_id = $2, deleted_at = CASE WHEN $3 THEN now() END WHERE id = $1`,
				conversation.ID, holder, deleted); err != nil {
				t.Fatalf("shape the conversation: %v", err)
			}
			return contact
		}
		mine := open(&me, false)
		pooled := open(nil, false)
		theirs := open(&other, false)
		gone := open(&me, true)

		// The placeholder is $2 on purpose: the caller owns the numbering, and the fragment has to take whatever index it is given.
		rows, err := exec.Query(ctx,
			`SELECT contacts.id FROM contacts WHERE contacts.id = ANY($1) AND `+ScopedToOperator("contacts.id", 2),
			[]uuid.UUID{mine, pooled, theirs, gone}, me)
		if err != nil {
			t.Fatalf("query the scope: %v", err)
		}
		defer rows.Close()

		reached := []uuid.UUID{}
		for rows.Next() {
			var contact uuid.UUID
			if err := rows.Scan(&contact); err != nil {
				t.Fatalf("scan: %v", err)
			}
			reached = append(reached, contact)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("rows: %v", err)
		}

		if !slices.Equal(reached, []uuid.UUID{mine}) {
			t.Fatalf("the operator reached %v, want only the contact of its own conversation %v (pool %v, theirs %v, deleted %v)",
				reached, mine, pooled, theirs, gone)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}
