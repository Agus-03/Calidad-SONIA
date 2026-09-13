package inbox

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mateo-velasquez/sonia/src/core/internal/conversations"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// scene is the account and inbox of the demo, with every vendor the database already had switched off inside the case's transaction:
// the turn then answers about the vendors the case seeds and not about rows another run or package left behind.
type scene struct {
	t          *testing.T
	ctx        context.Context
	exec       db.Executor
	account    uuid.UUID
	inbox      uuid.UUID
	dispatcher *Dispatcher
}

func newScene(t *testing.T, ctx context.Context, exec db.Executor) scene {
	t.Helper()
	s := scene{t: t, ctx: ctx, exec: exec}
	if err := exec.QueryRow(ctx, `SELECT a.id, i.id FROM accounts a, inboxes i LIMIT 1`).Scan(&s.account, &s.inbox); err != nil {
		t.Skip("the demo rows are missing: load src/core/seeds/0001_demo.sql first")
	}
	if _, err := exec.Exec(ctx, `UPDATE operators SET active = false WHERE role = 'operator'`); err != nil {
		t.Fatalf("switch off the vendors already there: %v", err)
	}
	s.dispatcher = NewDispatcher(NewRepository(exec), conversations.NewService(conversations.NewRepository(exec), nil),
		slog.New(slog.DiscardHandler))
	return s
}

func (s scene) vendor(active bool) uuid.UUID {
	s.t.Helper()
	vendor := id.New()
	if _, err := s.exec.Exec(s.ctx, `
		INSERT INTO operators (id, account_id, name, lastname, email, password_hash, role, active)
		VALUES ($1, $2, 'Prueba', 'Reparto', $3, 'not-a-real-hash', 'operator', $4)`,
		vendor, s.account, vendor.String()+"@sonia.test", active); err != nil {
		s.t.Fatalf("seed a vendor: %v", err)
	}
	return vendor
}

func (s scene) contact() uuid.UUID {
	s.t.Helper()
	contact := id.New()
	if _, err := s.exec.Exec(s.ctx, `
		INSERT INTO contacts (id, account_id, phone_whatsapp, name, lastname, type, source_channel)
		VALUES ($1, $2, $3, 'Prueba', 'Reparto', 'prospect', 'direct')`,
		contact, s.account, digitsOf(contact.String())); err != nil {
		s.t.Fatalf("seed a contact: %v", err)
	}
	return contact
}

func (s scene) conversation(contact uuid.UUID, holder *uuid.UUID, status string) uuid.UUID {
	s.t.Helper()
	conversation := id.New()
	if _, err := s.exec.Exec(s.ctx, `
		INSERT INTO conversations (id, contact_id, inbox_id, operator_id, status, mode, last_inbound_at, window_expires_at)
		VALUES ($1, $2, $3, $4, $5, 'autonomous', now(), now() + interval '24 hours')`,
		conversation, contact, s.inbox, holder, status); err != nil {
		s.t.Fatalf("seed a conversation: %v", err)
	}
	return conversation
}

// earlier writes a resolved conversation of the contact that began some time ago, so which one is the latest never rests on ids.
func (s scene) earlier(contact uuid.UUID, holder *uuid.UUID, ago time.Duration) {
	s.t.Helper()
	conversation := s.conversation(contact, holder, "resolved")
	if _, err := s.exec.Exec(s.ctx, `UPDATE conversations SET created_at = now() - make_interval(secs => $2) WHERE id = $1`,
		conversation, ago.Seconds()); err != nil {
		s.t.Fatalf("date an earlier conversation: %v", err)
	}
}

// received writes the history row that makes a vendor's turn: the last conversation it was given, and when.
func (s scene) received(vendor uuid.UUID, ago time.Duration) {
	s.t.Helper()
	conversation := s.conversation(s.contact(), &vendor, "active")
	if _, err := s.exec.Exec(s.ctx, `
		INSERT INTO conversation_assignments (id, conversation_id, to_operator_id, reason, created_at)
		VALUES ($1, $2, $3, 'seeded', now() - make_interval(secs => $4))`,
		id.New(), conversation, vendor, ago.Seconds()); err != nil {
		s.t.Fatalf("seed a past assignment: %v", err)
	}
}

// assign runs the dispatcher on a new conversation of the contact and answers who holds it and why.
func (s scene) assign(contact uuid.UUID) (holder *uuid.UUID, reason string) {
	s.t.Helper()
	conversation := s.conversation(contact, nil, "active")
	if err := s.dispatcher.Assign(s.ctx, s.account, contact, conversation); err != nil {
		s.t.Fatalf("Assign: %v", err)
	}
	if err := s.exec.QueryRow(s.ctx, `SELECT operator_id FROM conversations WHERE id = $1`, conversation).Scan(&holder); err != nil {
		s.t.Fatalf("read the holder: %v", err)
	}
	err := s.exec.QueryRow(s.ctx, `SELECT reason FROM conversation_assignments WHERE conversation_id = $1`, conversation).Scan(&reason)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		s.t.Fatalf("read the history: %v", err)
	}
	return holder, reason
}

// TestANewCustomerGoesToTheVendorWhoWaitedLongest is ADR-0037, regla 1, against real DDL: never having received one comes first, and
// among the rest, the one whose last conversation is oldest. Inactive vendors are not in the turn, and neither is the seeded admin,
// who never received one either and would come first if the role were not filtered (regla 2).
func TestANewCustomerGoesToTheVendorWhoWaitedLongest(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor) {
		s := newScene(t, ctx, exec)
		recent, oldest := s.vendor(true), s.vendor(true)
		s.received(recent, time.Hour)
		s.received(oldest, 2*time.Hour)
		// Never received one, which would put it first if being switched off did not take it out of the turn.
		s.vendor(false)

		if holder, reason := s.assign(s.contact()); holder == nil || *holder != oldest || reason != reasonTurn {
			t.Fatalf("the conversation went to %v for %q, want %v, whose last one is oldest, by turn", holder, reason, oldest)
		}
		// Having just received one, it goes to the back of the turn.
		if holder, _ := s.assign(s.contact()); holder == nil || *holder != recent {
			t.Fatalf("the next conversation went to %v, want %v", holder, recent)
		}

		fresh := s.vendor(true)
		if holder, _ := s.assign(s.contact()); holder == nil || *holder != fresh {
			t.Fatalf("a vendor who never received one did not come first: got %v, want %v", holder, fresh)
		}
	})
}

// TestAReturningCustomerGoesBackToItsVendor is ADR-0037, regla 1: the vendor of the contact's latest earlier conversation is chosen
// over the turn, as long as it can still receive; one that was switched off is skipped and the turn decides.
func TestAReturningCustomerGoesBackToItsVendor(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor) {
		s := newScene(t, ctx, exec)
		known, waiting := s.vendor(true), s.vendor(true)

		customer := s.contact()
		s.conversation(customer, &known, "resolved")
		if holder, reason := s.assign(customer); holder == nil || *holder != known || reason != reasonPreviousOperator {
			t.Fatalf("the returning customer went to %v for %q, want its vendor %v", holder, reason, known)
		}

		// The same customer again, after its vendor was switched off: the one it knew can no longer receive it.
		if _, err := exec.Exec(ctx, `UPDATE conversations SET status = 'resolved' WHERE contact_id = $1`, customer); err != nil {
			t.Fatalf("close the conversation: %v", err)
		}
		if _, err := exec.Exec(ctx, `UPDATE operators SET active = false WHERE id = $1`, known); err != nil {
			t.Fatalf("switch the vendor off: %v", err)
		}
		if holder, reason := s.assign(customer); holder == nil || *holder != waiting || reason != reasonTurn {
			t.Fatalf("with its vendor off the customer went to %v for %q, want %v by turn", holder, reason, waiting)
		}
	})
}

// TestOnlyTheCustomersLatestVendorCounts is ADR-0037, regla 1, where two readings part: an earlier conversation nobody held is
// skipped, because it arrived when nobody could receive it and says nothing about the customer's vendor; but a latest vendor who
// can no longer receive sends the customer to the turn, and never to a vendor from further back.
func TestOnlyTheCustomersLatestVendorCounts(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor) {
		s := newScene(t, ctx, exec)
		older, latest, waiting := s.vendor(true), s.vendor(true), s.vendor(true)
		// Both have just received one, so whenever the turn decides it is waiting's.
		s.received(older, time.Minute)
		s.received(latest, time.Minute)

		kept := s.contact()
		s.earlier(kept, &older, 2*time.Hour)
		s.earlier(kept, nil, time.Hour)
		if holder, reason := s.assign(kept); holder == nil || *holder != older || reason != reasonPreviousOperator {
			t.Fatalf("past a conversation of the pool the customer went to %v for %q, want its vendor %v", holder, reason, older)
		}

		passed := s.contact()
		s.earlier(passed, &older, 2*time.Hour)
		s.earlier(passed, &latest, time.Hour)
		if _, err := exec.Exec(ctx, `UPDATE operators SET active = false WHERE id = $1`, latest); err != nil {
			t.Fatalf("switch the latest vendor off: %v", err)
		}
		if holder, reason := s.assign(passed); holder == nil || *holder != waiting || reason != reasonTurn {
			t.Fatalf("with its latest vendor off the customer went to %v for %q, want %v by turn and not the older %v",
				holder, reason, waiting, older)
		}
	})
}

// TestNeitherRuleChoosesWhoIsNoLongerAVendor is ADR-0037, regla 2, and C09, for both queries: a vendor who became a supervisor is
// no longer in sales, and one taken down is gone even with active still true. Both never received a conversation, so the turn would
// put them first if either filter were missing.
func TestNeitherRuleChoosesWhoIsNoLongerAVendor(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor) {
		s := newScene(t, ctx, exec)
		promoted, gone, waiting := s.vendor(true), s.vendor(true), s.vendor(true)
		s.received(waiting, time.Hour)
		if _, err := exec.Exec(ctx, `UPDATE operators SET role = 'supervisor' WHERE id = $1`, promoted); err != nil {
			t.Fatalf("promote the vendor: %v", err)
		}
		if _, err := exec.Exec(ctx, `UPDATE operators SET deleted_at = now() WHERE id = $1`, gone); err != nil {
			t.Fatalf("take the vendor down: %v", err)
		}

		for _, previous := range []uuid.UUID{promoted, gone} {
			customer := s.contact()
			s.earlier(customer, &previous, time.Hour)
			if holder, reason := s.assign(customer); holder == nil || *holder != waiting || reason != reasonTurn {
				t.Fatalf("the customer of %v went to %v for %q, want %v by turn", previous, holder, reason, waiting)
			}
		}
	})
}

// TestTheTurnAlternatesInsideOnePayload is a webhook that brings several new customers at once: its assignments share now(), and the
// turn still alternates instead of handing everything after the first round to the lowest id.
func TestTheTurnAlternatesInsideOnePayload(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor) {
		s := newScene(t, ctx, exec)
		first, second := s.vendor(true), s.vendor(true)

		for i, want := range []uuid.UUID{first, second, first, second} {
			if holder, _ := s.assign(s.contact()); holder == nil || *holder != want {
				t.Fatalf("conversation %d of the payload went to %v, want %v", i+1, holder, want)
			}
		}
	})
}

// TestWithNobodyActiveTheConversationStaysInThePool is ADR-0037, regla 3: no vendor, no claim and no history row, and the manual
// inbox takes it from there as ADR-0004 always did.
func TestWithNobodyActiveTheConversationStaysInThePool(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor) {
		s := newScene(t, ctx, exec)
		if holder, reason := s.assign(s.contact()); holder != nil || reason != "" {
			t.Fatalf("with nobody active the conversation went to %v for %q, want the pool and no history", holder, reason)
		}
	})
}
