package inbox

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/conversations"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// TestAConversationGoesToTheVendorTheCustomerKnowsOrToTheNextInTurn is ADR-0037, reglas 1 y 3, as a table: the previous vendor wins,
// the turn comes second, and nobody active leaves the conversation in the pool without writing anything.
func TestAConversationGoesToTheVendorTheCustomerKnowsOrToTheNextInTurn(t *testing.T) {
	known, next := id.New(), id.New()

	for _, tc := range []struct {
		name       string
		previous   *uuid.UUID
		next       *uuid.UUID
		want       *uuid.UUID
		wantReason string
		wantTurn   bool
	}{
		{"a returning customer goes back to its vendor, before any turn", &known, &next, &known, reasonPreviousOperator, false},
		{"a new customer goes to the next vendor in turn", nil, &next, &next, reasonTurn, true},
		{"with no vendor active the conversation stays in the pool", nil, nil, nil, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &stubStore{previous: tc.previous, next: tc.next}
			aggregate := &stubAggregate{}
			conversation := id.New()

			if err := NewDispatcher(store, aggregate, slog.New(slog.DiscardHandler)).Assign(context.Background(), id.New(), id.New(), conversation); err != nil {
				t.Fatalf("Assign: %v", err)
			}
			if (store.nextAsked > 0) != tc.wantTurn {
				t.Fatalf("the turn was asked %d times, want asked = %v", store.nextAsked, tc.wantTurn)
			}

			if tc.want == nil {
				if len(aggregate.claimed) != 0 || len(store.assignments) != 0 {
					t.Fatalf("nobody to receive it and still %d claims and %d assignments", len(aggregate.claimed), len(store.assignments))
				}
				return
			}
			if aggregate.claimed[conversation] != *tc.want {
				t.Fatalf("the conversation went to %v, want %v", aggregate.claimed[conversation], *tc.want)
			}
			if len(store.assignments) != 1 {
				t.Fatalf("%d assignments written, want the one F26 will read as history", len(store.assignments))
			}
			a := store.assignments[0]
			// Nobody assigned it, so there is no "by" and no "from": the reason is what says it was the system, and which rule.
			if a.ToOperatorID != *tc.want || a.FromOperatorID != nil || a.AssignedByOperatorID != nil || a.Reason != tc.wantReason {
				t.Fatalf("assignment %+v, want to %v from nobody, by nobody, reason %q", a, *tc.want, tc.wantReason)
			}
		})
	}
}

// TestAConversationTakenMeanwhileIsLeftToWhoeverTookIt is the race with the manual inbox: someone answered it from the pool between
// the read and the claim. It has a vendor, which is the point, so nothing more is written and the webhook does not fail.
func TestAConversationTakenMeanwhileIsLeftToWhoeverTookIt(t *testing.T) {
	next := id.New()
	store := &stubStore{next: &next}
	aggregate := &stubAggregate{claimErr: conversations.ErrTaken}

	if err := NewDispatcher(store, aggregate, nil).Assign(context.Background(), id.New(), id.New(), id.New()); err != nil {
		t.Fatalf("Assign: got %v, want no error", err)
	}
	if len(store.assignments) != 0 {
		t.Fatalf("a lost claim still wrote %d assignments", len(store.assignments))
	}
}

// TestADispatchThatCannotReadOrWriteFailsTheWebhook keeps every other failure visible: the webhook's transaction rolls back, and a
// conversation never commits without the vendor it was owed.
func TestADispatchThatCannotReadOrWriteFailsTheWebhook(t *testing.T) {
	boom := errors.New("connection reset")
	next := id.New()

	for _, tc := range []struct {
		name      string
		store     *stubStore
		aggregate *stubAggregate
	}{
		{"the previous vendor cannot be read", &stubStore{previousErr: boom}, &stubAggregate{}},
		{"the turn cannot be read", &stubStore{nextErr: boom}, &stubAggregate{}},
		{"the claim fails for a reason other than the race", &stubStore{next: &next}, &stubAggregate{claimErr: boom}},
		{"the history row cannot be written", &stubStore{next: &next, assignErr: boom}, &stubAggregate{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := NewDispatcher(tc.store, tc.aggregate, nil).Assign(context.Background(), id.New(), id.New(), id.New()); !errors.Is(err, boom) {
				t.Fatalf("Assign: got %v, want the underlying failure", err)
			}
		})
	}
}
