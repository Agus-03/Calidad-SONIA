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
