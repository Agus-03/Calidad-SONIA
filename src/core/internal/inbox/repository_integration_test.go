package inbox

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// rollback ends every case: the assertions run against real DDL and leave the database as they found it.
var rollback = errors.New("inbox: rolling back the integration case")

func integrationPool(t *testing.T) db.Pool {
	t.Helper()

	dsn := os.Getenv("SONIA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SONIA_TEST_DATABASE_URL is not set: bring the stack up and run make test-integration")
	}

	pool, err := db.Open(context.Background(), db.Options{DatabaseURL: dsn, MinSize: 1, MaxSize: 4})
	if err != nil {
		t.Fatalf("Open: got %v, want no error", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// world is one conversation of each kind the listing tells apart, seeded inside the case's own transaction.
//
// Their activity is set a millennium ahead so they sort before any row the demo or another package committed: the cases can then read
// the head of the inbox and know it is theirs, without clearing tables that other packages are writing in parallel.
type world struct {
	account, mine, theirs   uuid.UUID
	theirsConv, mineConv    uuid.UUID
	pooledConv, deletedConv uuid.UUID
	failedAt                time.Time
}

// Microseconds on purpose: PostgreSQL keeps them, and a cursor that lost them on the way back would repeat or skip a row.
var (
	activityDeleted = time.Date(2999, 1, 4, 10, 0, 0, 654321000, time.UTC)
	activityTheirs  = time.Date(2999, 1, 3, 10, 0, 0, 123456000, time.UTC)
	activityMine    = time.Date(2999, 1, 2, 10, 0, 0, 123456000, time.UTC)
	activityPooled  = time.Date(2999, 1, 1, 10, 0, 0, 1000, time.UTC)
)

func seedWorld(t *testing.T, ctx context.Context, exec db.Executor) world {
	t.Helper()

	var w world
	var inbox uuid.UUID
	if err := exec.QueryRow(ctx, `SELECT a.id, i.id FROM accounts a, inboxes i LIMIT 1`).Scan(&w.account, &inbox); err != nil {
		t.Skip("the demo rows are missing: load src/core/seeds/0001_demo.sql first")
	}

	w.mine, w.theirs = id.New(), id.New()
	for _, o := range []uuid.UUID{w.mine, w.theirs} {
		if _, err := exec.Exec(ctx, `
			INSERT INTO operators (id, account_id, name, lastname, email, password_hash, role, active)
			VALUES ($1, $2, 'Prueba', 'Bandeja', $3, 'not-a-real-hash', 'operator', true)`,
			o, w.account, o.String()+"@sonia.test"); err != nil {
			t.Fatalf("seed an operator: %v", err)
		}
	}

	conversation := func(operator *uuid.UUID, activity time.Time) uuid.UUID {
		contact, conv := id.New(), id.New()
		if _, err := exec.Exec(ctx, `
			INSERT INTO contacts (id, account_id, phone_whatsapp, name, lastname, type, source_channel)
			VALUES ($1, $2, $3, 'Prueba', 'Bandeja', 'prospect', 'direct')`,
			contact, w.account, digitsOf(contact.String())); err != nil {
			t.Fatalf("seed a contact: %v", err)
		}
		if _, err := exec.Exec(ctx, `
			INSERT INTO conversations (id, contact_id, inbox_id, operator_id, status, mode, last_inbound_at, window_expires_at, last_activity_at)
			VALUES ($1, $2, $3, $4, 'active', 'off', $5, $5::timestamptz + interval '24 hours', $5)`,
			conv, contact, inbox, operator, activity); err != nil {
			t.Fatalf("seed a conversation: %v", err)
		}
		return conv
	}

	w.theirsConv = conversation(&w.theirs, activityTheirs)
	w.mineConv = conversation(&w.mine, activityMine)
	// Unassigned, which is how every incoming conversation arrives until somebody answers (ADR-0004).
	w.pooledConv = conversation(nil, activityPooled)
	w.deletedConv = conversation(&w.mine, activityDeleted)
	if _, err := exec.Exec(ctx, `UPDATE conversations SET deleted_at = now() WHERE id = $1`, w.deletedConv); err != nil {
		t.Fatalf("delete a conversation: %v", err)
	}

	w.failedAt = time.Date(2999, 1, 1, 9, 0, 0, 0, time.UTC)
	message := func(conv uuid.UUID, direction enums.MessageDirection, status *string, hidden bool, at time.Time) {
		senderType, sender := "contact", (*uuid.UUID)(nil)
		if direction == enums.MessageDirectionOutbound {
			senderType, sender = "operator", &w.mine
		}
		var code *string
		if status != nil {
			c := "131047"
			code = &c
		}
		var hiddenAt *time.Time
		if hidden {
			hiddenAt = &at
		}
		if _, err := exec.Exec(ctx, `
			INSERT INTO messages (id, conversation_id, direction, sender_type, sender_operator_id, content_text, content_type,
			                      status, error_code, sent_at, hidden_at, created_at)
			VALUES ($1, $2, $3, $4, $5, 'texto', 'text', $6, $7, $8, $9, $8)`,
			id.New(), conv, direction, senderType, sender, status, code, at, hiddenAt); err != nil {
			t.Fatalf("seed a message: %v", err)
		}
	}
	failed := string(enums.MessageStatusFailed)

	// Mine ends on the failed send, so the last message itself has to say so.
	message(w.mineConv, enums.MessageDirectionInbound, nil, false, w.failedAt.Add(-time.Hour))
	message(w.mineConv, enums.MessageDirectionOutbound, &failed, false, w.failedAt)
	// The pool's failure is followed by the customer writing again: the case that would bury it.
	message(w.pooledConv, enums.MessageDirectionOutbound, &failed, false, w.failedAt)
	message(w.pooledConv, enums.MessageDirectionInbound, nil, false, w.failedAt.Add(time.Hour))
	// Theirs holds only a hidden message, which the inbox must not draw (C09).
	message(w.theirsConv, enums.MessageDirectionInbound, nil, true, w.failedAt)

	return w
}

// Digits only and unique per contact, the shape of Meta's wa_id, so nothing collides with the demo rows or another package's.
func digitsOf(s string) string {
	out := []byte{}
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			out = append(out, s[i])
		}
	}
	return string(out)
}

func inTx(t *testing.T, fn func(ctx context.Context, exec db.Executor)) {
	t.Helper()
	ctx := context.Background()
	err := db.InTx(ctx, integrationPool(t), func(exec db.Executor) error {
		fn(ctx, exec)
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

func idsOf(rows []Summary) []uuid.UUID {
	out := []uuid.UUID{}
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

func sameIDs(got, want []uuid.UUID) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestTheInboxOfEachRoleIsTheOneAnexoBDescribes proves the reach against real DDL, and the deleted conversation along with it: it has
// the newest activity of all, so if the filter slipped it would be the first row every role sees.
func TestTheInboxOfEachRoleIsTheOneAnexoBDescribes(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor) {
		w := seedWorld(t, ctx, exec)
		repo := NewRepository(exec)

		for _, tc := range []struct {
			role  enums.OperatorRole
			limit int
			want  []uuid.UUID
		}{
			{enums.OperatorRoleOperator, 2, []uuid.UUID{w.mineConv, w.pooledConv}},
			{enums.OperatorRoleSupervisor, 3, []uuid.UUID{w.theirsConv, w.mineConv, w.pooledConv}},
			{enums.OperatorRoleAdmin, 3, []uuid.UUID{w.theirsConv, w.mineConv, w.pooledConv}},
		} {
			t.Run(string(tc.role), func(t *testing.T) {
				rows, err := repo.List(ctx, Scope{AccountID: w.account, OperatorID: w.mine, Role: tc.role}, nil, tc.limit)
				if err != nil {
					t.Fatalf("List: %v", err)
				}
				if got := idsOf(rows); !sameIDs(got, tc.want) {
					t.Fatalf("the head of the inbox is %v, want %v", got, tc.want)
				}
			})
		}
	})
}

// TestTheCursorContinuesStrictlyAfterTheRowItNamed walks three pages of one row through the service, so the instant makes the round
// trip PostgreSQL to Go to PostgreSQL with its microseconds and the comparison stays strict.
func TestTheCursorContinuesStrictlyAfterTheRowItNamed(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor) {
		w := seedWorld(t, ctx, exec)
		service := NewService(NewRepository(exec), nil)
		scope := Scope{AccountID: w.account, OperatorID: w.mine, Role: enums.OperatorRoleAdmin}

		var after *Cursor
		for i, want := range []uuid.UUID{w.theirsConv, w.mineConv, w.pooledConv} {
			page, err := service.List(ctx, scope, after, 1)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(page.Items) != 1 || page.Items[0].ID != want {
				t.Fatalf("the page after %+v holds %v, want %v", after, idsOf(page.Items), want)
			}
			// The third may be the last one of a freshly seeded database, so only the two with a seeded row behind them must continue.
			if i < 2 && page.Next == nil {
				t.Fatalf("no cursor after %v, with seeded rows still ahead", want)
			}
			after = page.Next
		}
	})
}

// TestAFailedSendStaysVisibleInTheInbox is ADR-0020 against real DDL: the failure is on the row whether it is the last message or not,
// and a hidden message is neither drawn nor counted.
func TestAFailedSendStaysVisibleInTheInbox(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor) {
		w := seedWorld(t, ctx, exec)

		rows, err := NewRepository(exec).List(ctx, Scope{AccountID: w.account, OperatorID: w.mine, Role: enums.OperatorRoleAdmin}, nil, 3)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(rows) != 3 {
			t.Fatalf("got %d rows, want the three seeded", len(rows))
		}
		theirs, mine, pooled := rows[0], rows[1], rows[2]

		if mine.LastMessage == nil || mine.LastMessage.Status == nil || *mine.LastMessage.Status != enums.MessageStatusFailed {
			t.Fatalf("the last message of mine does not say it failed: %+v", mine.LastMessage)
		}
		if mine.LastMessage.ErrorCode == nil || *mine.LastMessage.ErrorCode != "131047" {
			t.Fatalf("the failure lost its error: %+v", mine.LastMessage)
		}
		if mine.Operator == nil || mine.Operator.ID != w.mine {
			t.Fatalf("mine is assigned to %+v, want %v", mine.Operator, w.mine)
		}

		if pooled.LastMessage == nil || pooled.LastMessage.Direction != enums.MessageDirectionInbound {
			t.Fatalf("the last message of the pool should be the customer's: %+v", pooled.LastMessage)
		}
		if pooled.FailedCount != 1 || pooled.LastFailedAt == nil || !pooled.LastFailedAt.Equal(w.failedAt) {
			t.Fatalf("the customer's message buried the failure: count %d at %v", pooled.FailedCount, pooled.LastFailedAt)
		}
		if pooled.Operator != nil {
			t.Fatalf("the pool came back assigned to %+v", pooled.Operator)
		}

		if theirs.LastMessage != nil || theirs.FailedCount != 0 {
			t.Fatalf("a hidden message reached the inbox: %+v, %d failed", theirs.LastMessage, theirs.FailedCount)
		}
	})
}
