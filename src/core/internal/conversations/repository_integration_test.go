package conversations

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
var rollback = errors.New("conversations: rolling back the integration case")

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

// seeded reads the fixed rows of src/core/seeds, which is what the phase runs on; without them there is nothing to hang a conversation from.
func seeded(t *testing.T, pool db.Pool) (account, inbox, agent uuid.UUID) {
	t.Helper()

	err := pool.QueryRow(context.Background(),
		`SELECT a.id, i.id, g.id FROM accounts a, inboxes i, ai_agents g LIMIT 1`).Scan(&account, &inbox, &agent)
	if err != nil {
		t.Skip("the demo rows are missing: load src/core/seeds/0001_demo.sql first")
	}
	return account, inbox, agent
}

func insertContact(ctx context.Context, exec db.Executor, account uuid.UUID) (uuid.UUID, error) {
	contact := id.New()
	_, err := exec.Exec(ctx,
		`INSERT INTO contacts (id, account_id, phone_whatsapp, name, lastname, type, source_channel, consent_given_at)
		 VALUES ($1, $2, $3, 'Prueba', 'Integracion', 'client', 'direct', now())`,
		contact, account, "549351"+digitsOf(contact.String()))
	return contact, err
}

// digitsOf keeps every digit of the id and not a prefix: the head of a UUID v7 is its timestamp, so two contacts built in the same
// case would share it and collide on the unique index of the phone.
func digitsOf(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			out = append(out, s[i])
		}
	}
	return string(out)
}

// TestTheIndexAndNotTheReadDecidesWhoOpens proves ADR-0002 against real DDL: two racing inbound messages cannot open two conversations.
func TestTheIndexAndNotTheReadDecidesWhoOpens(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account, inbox, agent := seeded(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec)
		contact, err := insertContact(ctx, exec, account)
		if err != nil {
			t.Fatalf("insert contact: %v", err)
		}

		at := time.Now().UTC()
		first, err := NewService(repo, nil).EnsureOpen(ctx, contact, inbox, &agent, true, at)
		if err != nil {
			t.Fatalf("EnsureOpen: %v", err)
		}

		// The second caller skips the read on purpose, which is exactly what a lost race looks like from the database.
		if err := repo.Create(ctx, Conversation{
			ID: id.New(), ContactID: contact, InboxID: inbox, AIAgentID: &agent,
			Status: enums.ConversationStatusActive, Mode: enums.AgentModeAutonomous,
			LastInboundAt: at, WindowExpiresAt: at.Add(ServiceWindow),
		}); !errors.Is(err, ErrAlreadyOpen) {
			t.Fatalf("a second open conversation was accepted: got %v, want ErrAlreadyOpen", err)
		}

		second, err := NewService(repo, nil).EnsureOpen(ctx, contact, inbox, &agent, true, at)
		if err != nil {
			t.Fatalf("EnsureOpen after the race: %v", err)
		}
		if second.ID != first.ID {
			t.Fatalf("the race produced two conversations, %v and %v", first.ID, second.ID)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestTheOutboundMessageIsWrittenBeforeItIsSent is the operation the schema forbade until the nullable fix, and the reason for it.
func TestTheOutboundMessageIsWrittenBeforeItIsSent(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account, inbox, agent := seeded(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec)
		service := NewService(repo, nil)

		contact, err := insertContact(ctx, exec, account)
		if err != nil {
			t.Fatalf("insert contact: %v", err)
		}

		at := time.Now().UTC()
		conversation, err := service.EnsureOpen(ctx, contact, inbox, &agent, true, at)
		if err != nil {
			t.Fatalf("EnsureOpen: %v", err)
		}
		if _, err := service.RecordInbound(ctx, conversation, "wamid."+id.New().String(), "hola", enums.MessageContentTypeText, at); err != nil {
			t.Fatalf("RecordInbound: %v", err)
		}

		draft := service.DraftOutbound(conversation.ID, agent, "buenas, en que puedo ayudarte", at)
		if err := repo.AppendMessage(ctx, draft); err != nil {
			t.Fatalf("the outbound message could not be written before being sent: %v", err)
		}

		var wamid *string
		var status *string
		if err := exec.QueryRow(ctx, `SELECT wamid, status FROM messages WHERE id = $1`, draft.ID).Scan(&wamid, &status); err != nil {
			t.Fatalf("read back the draft: %v", err)
		}
		if wamid != nil || status != nil {
			t.Fatalf("the draft was stored already sent: wamid %v status %v", wamid, status)
		}

		if err := repo.MarkSent(ctx, draft.ID, "wamid.sent", at); err != nil {
			t.Fatalf("MarkSent: %v", err)
		}
		if err := exec.QueryRow(ctx, `SELECT wamid, status FROM messages WHERE id = $1`, draft.ID).Scan(&wamid, &status); err != nil {
			t.Fatalf("read back after sending: %v", err)
		}
		if wamid == nil || *wamid != "wamid.sent" || status == nil || *status != string(enums.MessageStatusSent) {
			t.Fatalf("Meta's answer did not land on the row: wamid %v status %v", wamid, status)
		}

		// ADR-0020 sends at most once, so a rejected send has to stay readable instead of being retried or removed.
		failed := service.DraftOutbound(conversation.ID, agent, "esta no sale", at)
		if err := repo.WithExecutor(exec).AppendMessage(ctx, failed); err != nil {
			t.Fatalf("append the message that will fail: %v", err)
		}
		if err := repo.MarkFailed(ctx, failed.ID, "131047"); err != nil {
			t.Fatalf("MarkFailed: %v", err)
		}

		var code *string
		if err := exec.QueryRow(ctx, `SELECT status, error_code FROM messages WHERE id = $1`, failed.ID).Scan(&status, &code); err != nil {
			t.Fatalf("read back the failed message: %v", err)
		}
		if status == nil || *status != string(enums.MessageStatusFailed) || code == nil || *code != "131047" {
			t.Fatalf("the failed send left no visible trace: status %v code %v", status, code)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// insertOperator writes a real operator, because conversations.operator_id and messages.sender_operator_id are foreign keys.
func insertOperator(ctx context.Context, exec db.Executor, account uuid.UUID) (uuid.UUID, error) {
	operator := id.New()
	_, err := exec.Exec(ctx,
		`INSERT INTO operators (id, account_id, name, lastname, email, password_hash, role, active)
		 VALUES ($1, $2, 'Prueba', 'Agregado', $3, 'not-a-real-hash', 'operator', true)`,
		operator, account, operator.String()+"@sonia.test")
	return operator, err
}

// TestOnlyTheFirstClaimTakesThePool is ADR-0004 against real DDL: the predicate decides, so the second operator gets ErrTaken and
// the conversation keeps the first one (ADR-0035, regla 8).
func TestOnlyTheFirstClaimTakesThePool(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account, inbox, agent := seeded(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		service := NewService(NewRepository(exec), nil)

		contact, err := insertContact(ctx, exec, account)
		if err != nil {
			t.Fatalf("insert contact: %v", err)
		}
		first, err := insertOperator(ctx, exec, account)
		if err != nil {
			t.Fatalf("insert operator: %v", err)
		}
		second, err := insertOperator(ctx, exec, account)
		if err != nil {
			t.Fatalf("insert operator: %v", err)
		}
		conversation, err := service.EnsureOpen(ctx, contact, inbox, &agent, true, time.Now().UTC())
		if err != nil {
			t.Fatalf("EnsureOpen: %v", err)
		}

		if err := service.Claim(ctx, conversation.ID, first); err != nil {
			t.Fatalf("the first claim on the pool: %v", err)
		}
		if err := service.Claim(ctx, conversation.ID, second); !errors.Is(err, ErrTaken) {
			t.Fatalf("the second claim: got %v, want ErrTaken", err)
		}

		var owner uuid.UUID
		if err := exec.QueryRow(ctx, `SELECT operator_id FROM conversations WHERE id = $1`, conversation.ID).Scan(&owner); err != nil {
			t.Fatalf("read the owner: %v", err)
		}
		if owner != first {
			t.Fatalf("the conversation belongs to %v, want the first to answer, %v", owner, first)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestALateInboundNeverMovesTheClocksBack is Meta delivering an older message after a newer one: the window keeps the newer one,
// because shortening it would refuse answers Meta still accepts (ADR-0008), and the activity takes the database clock and not Meta's,
// the same one the outbound side writes (C06).
func TestALateInboundNeverMovesTheClocksBack(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account, inbox, agent := seeded(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		service := NewService(NewRepository(exec), nil)

		contact, err := insertContact(ctx, exec, account)
		if err != nil {
			t.Fatalf("insert contact: %v", err)
		}
		// Meta's instants, months before the database clock, so the activity cannot pass by coinciding with one of them.
		newer := time.Date(2026, 1, 12, 10, 0, 0, 0, time.UTC)
		conversation, err := service.EnsureOpen(ctx, contact, inbox, &agent, true, newer)
		if err != nil {
			t.Fatalf("EnsureOpen: %v", err)
		}

		var lastInbound, windowExpires, activity time.Time
		receive := func(at time.Time) {
			t.Helper()
			if _, err := service.RecordInbound(ctx, conversation, "wamid."+id.New().String(), "hola", enums.MessageContentTypeText, at); err != nil {
				t.Fatalf("RecordInbound: %v", err)
			}
			if err := exec.QueryRow(ctx,
				`SELECT last_inbound_at, window_expires_at, last_activity_at FROM conversations WHERE id = $1`, conversation.ID).
				Scan(&lastInbound, &windowExpires, &activity); err != nil {
				t.Fatalf("read the conversation: %v", err)
			}
		}

		receive(newer)
		if !activity.After(newer) {
			t.Fatalf("last_activity_at = %v, want the database clock and not Meta's %v", activity, newer)
		}
		before := activity

		receive(newer.Add(-10 * time.Minute))
		if !lastInbound.Equal(newer) || !windowExpires.Equal(newer.Add(ServiceWindow)) {
			t.Fatalf("a late webhook moved the window back: last_inbound_at %v, window_expires_at %v, want %v and %v",
				lastInbound, windowExpires, newer, newer.Add(ServiceWindow))
		}
		if activity.Before(before) {
			t.Fatalf("a late webhook sank the conversation: last_activity_at went from %v to %v", before, activity)
		}

		// And the ordinary case still moves forward, so the guard is not a window frozen at the first message.
		latest := newer.Add(5 * time.Minute)
		receive(latest)
		if !lastInbound.Equal(latest) || !windowExpires.Equal(latest.Add(ServiceWindow)) {
			t.Fatalf("a newer message did not reset the window: last_inbound_at %v, window_expires_at %v", lastInbound, windowExpires)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestTheFirstSendIsTheFirstResponse proves the three columns an answer moves: the operator's row passes the sender check, the
// activity moves when it is written, and first_response_at takes the first send only — never a failed one, never a later one.
func TestTheFirstSendIsTheFirstResponse(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account, inbox, agent := seeded(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec)
		service := NewService(repo, nil)

		contact, err := insertContact(ctx, exec, account)
		if err != nil {
			t.Fatalf("insert contact: %v", err)
		}
		operator, err := insertOperator(ctx, exec, account)
		if err != nil {
			t.Fatalf("insert operator: %v", err)
		}
		// Well before the database clock, so "the activity moved" cannot pass by the two instants happening to coincide.
		opened := time.Date(2026, 1, 12, 10, 0, 0, 0, time.UTC)
		conversation, err := service.EnsureOpen(ctx, contact, inbox, &agent, true, opened)
		if err != nil {
			t.Fatalf("EnsureOpen: %v", err)
		}
		if _, err := exec.Exec(ctx, `UPDATE conversations SET last_activity_at = $2 WHERE id = $1`, conversation.ID, opened); err != nil {
			t.Fatalf("pin the activity: %v", err)
		}

		failed, err := service.RecordOperatorOutbound(ctx, conversation.ID, operator, "esta no sale", opened)
		if err != nil {
			t.Fatalf("RecordOperatorOutbound: the sender check refused the operator's row: %v", err)
		}
		if err := service.MarkFailed(ctx, failed.ID, "131047"); err != nil {
			t.Fatalf("MarkFailed: %v", err)
		}

		var activity time.Time
		var firstResponse *time.Time
		read := func() {
			t.Helper()
			if err := exec.QueryRow(ctx, `SELECT last_activity_at, first_response_at FROM conversations WHERE id = $1`, conversation.ID).
				Scan(&activity, &firstResponse); err != nil {
				t.Fatalf("read the conversation: %v", err)
			}
		}
		read()
		if !activity.After(opened) {
			t.Fatalf("writing the answer left last_activity_at at %v", activity)
		}
		if firstResponse != nil {
			t.Fatalf("a failed send was taken as the first response: %v", *firstResponse)
		}

		sentAt := opened.Add(time.Minute)
		answer, err := service.RecordOperatorOutbound(ctx, conversation.ID, operator, "ahora sí", sentAt)
		if err != nil {
			t.Fatalf("RecordOperatorOutbound: %v", err)
		}
		if err := service.MarkSent(ctx, answer.ID, "wamid.first", sentAt); err != nil {
			t.Fatalf("MarkSent: %v", err)
		}
		read()
		// The database clock and not the caller's sentAt: F34 subtracts created_at from it, which is that clock too (C06).
		if firstResponse == nil || firstResponse.Equal(sentAt) || !firstResponse.After(opened) {
			t.Fatalf("first_response_at is %v, want the database clock at the first send, not the caller's %v", firstResponse, sentAt)
		}
		// now() does not move inside one transaction, so a second send would write the same instant: pinned to another one, the
		// comparison below can tell a kept value from a rewritten one.
		first := opened.Add(time.Second)
		if _, err := exec.Exec(ctx, `UPDATE conversations SET first_response_at = $2 WHERE id = $1`, conversation.ID, first); err != nil {
			t.Fatalf("pin the first response: %v", err)
		}

		later, err := service.RecordOperatorOutbound(ctx, conversation.ID, operator, "algo más", sentAt.Add(time.Hour))
		if err != nil {
			t.Fatalf("RecordOperatorOutbound: %v", err)
		}
		if err := service.MarkSent(ctx, later.ID, "wamid.second", sentAt.Add(time.Hour)); err != nil {
			t.Fatalf("MarkSent: %v", err)
		}
		read()
		if !firstResponse.Equal(first) {
			t.Fatalf("a later send moved first_response_at from %v to %v", first, *firstResponse)
		}

		// Decision O2 through the column F34 reads: the fixed text of a cut turn goes out like any other message and answers
		// nothing the customer asked, so a conversation whose only outbound row is that text has no response time at all.
		if _, err := exec.Exec(ctx, `UPDATE conversations SET first_response_at = NULL WHERE id = $1`, conversation.ID); err != nil {
			t.Fatalf("clear the first response: %v", err)
		}
		notice, err := service.RecordSystemOutbound(ctx, conversation.ID, "prefiero que esto lo vea una persona", sentAt)
		if err != nil {
			t.Fatalf("RecordSystemOutbound: %v", err)
		}
		if err := service.MarkSent(ctx, notice.ID, "wamid.notice", sentAt); err != nil {
			t.Fatalf("MarkSent on the fixed text: %v", err)
		}
		read()
		if firstResponse != nil {
			t.Fatalf("the fixed text of a cut turn was taken as the first response: %v", *firstResponse)
		}

		if err := service.MarkSent(ctx, id.New(), "wamid.nobody", sentAt); !errors.Is(err, ErrNotFound) {
			t.Fatalf("MarkSent on a message that does not exist: got %v, want ErrNotFound", err)
		}
		if err := repo.TouchOutbound(ctx, id.New()); !errors.Is(err, ErrNotFound) {
			t.Fatalf("TouchOutbound on a conversation that does not exist: got %v, want ErrNotFound", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestTheBotIsTurnedOffWithoutLosingItsAIAgent is F27 against real DDL: off keeps ai_agent_id so the bot can come back, and a
// conversation born without an agent cannot be turned on, because the predicate and not a read decides it (ADR-0035, regla 12).
func TestTheBotIsTurnedOffWithoutLosingItsAIAgent(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account, inbox, agent := seeded(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		service := NewService(NewRepository(exec), nil)
		mode := func(conversationID uuid.UUID) (enums.AgentMode, *uuid.UUID) {
			t.Helper()
			var m enums.AgentMode
			var a *uuid.UUID
			if err := exec.QueryRow(ctx, `SELECT mode, ai_agent_id FROM conversations WHERE id = $1`, conversationID).Scan(&m, &a); err != nil {
				t.Fatalf("read the mode: %v", err)
			}
			return m, a
		}

		withAIAgent, err := insertContact(ctx, exec, account)
		if err != nil {
			t.Fatalf("insert contact: %v", err)
		}
		answered, err := service.EnsureOpen(ctx, withAIAgent, inbox, &agent, true, time.Now().UTC())
		if err != nil {
			t.Fatalf("EnsureOpen: %v", err)
		}

		if err := service.SetMode(ctx, answered.ID, enums.AgentModeOff); err != nil {
			t.Fatalf("turning the bot off: %v", err)
		}
		if m, a := mode(answered.ID); m != enums.AgentModeOff || a == nil || *a != agent {
			t.Fatalf("after turning it off: mode %q agent %v, want off and the agent kept", m, a)
		}
		if err := service.SetMode(ctx, answered.ID, enums.AgentModeAutonomous); err != nil {
			t.Fatalf("turning the bot back on: %v", err)
		}
		if m, _ := mode(answered.ID); m != enums.AgentModeAutonomous {
			t.Fatalf("after turning it on: mode %q, want autonomous", m)
		}

		withoutAIAgent, err := insertContact(ctx, exec, account)
		if err != nil {
			t.Fatalf("insert contact: %v", err)
		}
		silent, err := service.EnsureOpen(ctx, withoutAIAgent, inbox, nil, false, time.Now().UTC())
		if err != nil {
			t.Fatalf("EnsureOpen: %v", err)
		}
		if err := service.SetMode(ctx, silent.ID, enums.AgentModeAutonomous); !errors.Is(err, ErrNoAIAgent) {
			t.Fatalf("turning on a conversation with no agent: got %v, want ErrNoAIAgent", err)
		}
		if m, _ := mode(silent.ID); m != enums.AgentModeOff {
			t.Fatalf("the refused change still wrote %q", m)
		}

		// An agent the conversation has and that cannot answer is refused by the same predicate, paused or removed: it is what the
		// turn reads before answering, and a switch that disagrees with it lies to the vendor (ADR-0038). The agent is this case's
		// own, so pausing it never touches the demo agent.
		for _, stop := range []string{
			`UPDATE ai_agents SET active = false WHERE id = $1`,
			`UPDATE ai_agents SET deleted_at = now() WHERE id = $1`,
		} {
			own := id.New()
			if _, err := exec.Exec(ctx, `
				INSERT INTO ai_agents (id, account_id, name, default_mode, llm_provider, llm_model)
				VALUES ($1, $2, 'Agente de integracion', 'autonomous', 'cohere', 'command-a-03-2025')`, own, account); err != nil {
				t.Fatalf("insert an agent: %v", err)
			}
			contact, err := insertContact(ctx, exec, account)
			if err != nil {
				t.Fatalf("insert contact: %v", err)
			}
			stopped, err := service.EnsureOpen(ctx, contact, inbox, &own, true, time.Now().UTC())
			if err != nil {
				t.Fatalf("EnsureOpen: %v", err)
			}
			if err := service.SetMode(ctx, stopped.ID, enums.AgentModeOff); err != nil {
				t.Fatalf("turning the bot off: %v", err)
			}
			if _, err := exec.Exec(ctx, stop, own); err != nil {
				t.Fatalf("stop the agent: %v", err)
			}
			if err := service.SetMode(ctx, stopped.ID, enums.AgentModeAutonomous); !errors.Is(err, ErrNoAIAgent) {
				t.Fatalf("turning on after %q: got %v, want ErrNoAIAgent", stop, err)
			}
			if m, a := mode(stopped.ID); m != enums.AgentModeOff || a == nil || *a != own {
				t.Fatalf("after %q: mode %q agent %v, want off and the agent kept as evidence", stop, m, a)
			}
		}

		if err := service.SetMode(ctx, id.New(), enums.AgentModeOff); !errors.Is(err, ErrNotFound) {
			t.Fatalf("turning off a conversation that does not exist: got %v, want ErrNotFound", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestAStarMarksTheConversationWithoutMovingItInTheInbox is F25 against real DDL: the flag goes on and off, and last_activity_at stays
// where the channel left it, because the inbox and every cursor in circulation are ordered by it (ADR-0036).
func TestAStarMarksTheConversationWithoutMovingItInTheInbox(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account, inbox, _ := seeded(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		service := NewService(NewRepository(exec), nil)
		read := func(conversationID uuid.UUID) (bool, time.Time) {
			t.Helper()
			var starred bool
			var activity time.Time
			if err := exec.QueryRow(ctx, `SELECT starred, last_activity_at FROM conversations WHERE id = $1`, conversationID).
				Scan(&starred, &activity); err != nil {
				t.Fatalf("read the conversation: %v", err)
			}
			return starred, activity
		}

		contact, err := insertContact(ctx, exec, account)
		if err != nil {
			t.Fatalf("insert contact: %v", err)
		}
		conversation, err := service.EnsureOpen(ctx, contact, inbox, nil, false, time.Now().UTC())
		if err != nil {
			t.Fatalf("EnsureOpen: %v", err)
		}
		starred, before := read(conversation.ID)
		if starred {
			t.Fatal("a conversation is born starred")
		}

		for _, want := range []bool{true, false} {
			if err := service.SetStarred(ctx, conversation.ID, want); err != nil {
				t.Fatalf("SetStarred %v: %v", want, err)
			}
			starred, activity := read(conversation.ID)
			if starred != want {
				t.Fatalf("after SetStarred %v the column says %v", want, starred)
			}
			if !activity.Equal(before) {
				t.Fatalf("the star moved last_activity_at from %v to %v", before, activity)
			}
		}

		if err := service.SetStarred(ctx, id.New(), true); !errors.Is(err, ErrNotFound) {
			t.Fatalf("starring a conversation that does not exist: got %v, want ErrNotFound", err)
		}
		// A conversation deleted logically is one that does not exist (C09).
		if _, err := exec.Exec(ctx, `UPDATE conversations SET deleted_at = now() WHERE id = $1`, conversation.ID); err != nil {
			t.Fatalf("delete the conversation: %v", err)
		}
		if err := service.SetStarred(ctx, conversation.ID, true); !errors.Is(err, ErrNotFound) {
			t.Fatalf("starring a deleted conversation: got %v, want ErrNotFound", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// Where a turn that produced no answer leaves the conversation (07-secuencia secciones 4 y 5). Against real DDL because the value
// has to pass the CHECK of ConversationStatus: a status the schema refuses would only show up on the first blocked turn.
func TestATurnThatAnsweredNobodyLeavesTheConversationWaiting(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account, inbox, agent := seeded(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		service := NewService(NewRepository(exec), nil)
		status := func(conversationID uuid.UUID) enums.ConversationStatus {
			t.Helper()
			var got enums.ConversationStatus
			if err := exec.QueryRow(ctx, `SELECT status FROM conversations WHERE id = $1`, conversationID).Scan(&got); err != nil {
				t.Fatalf("read the status: %v", err)
			}
			return got
		}

		contact, err := insertContact(ctx, exec, account)
		if err != nil {
			t.Fatalf("insert contact: %v", err)
		}
		conversation, err := service.EnsureOpen(ctx, contact, inbox, &agent, true, time.Now().UTC())
		if err != nil {
			t.Fatalf("EnsureOpen: %v", err)
		}

		if err := service.MarkWaiting(ctx, conversation.ID); err != nil {
			t.Fatalf("MarkWaiting: %v", err)
		}
		if got := status(conversation.ID); got != enums.ConversationStatusWaiting {
			t.Fatalf("status %q, want waiting: nobody is going to answer what the bot did not", got)
		}

		// A conversation somebody already resolved moves back too, which is the decision the repository declares: the message that
		// was not answered is unanswered whatever anybody had decided about the conversation a minute earlier.
		if _, err := exec.Exec(ctx, `UPDATE conversations SET status = $2 WHERE id = $1`,
			conversation.ID, enums.ConversationStatusResolved); err != nil {
			t.Fatalf("resolve the conversation: %v", err)
		}
		if err := service.MarkWaiting(ctx, conversation.ID); err != nil {
			t.Fatalf("MarkWaiting over a resolved conversation: %v", err)
		}
		if got := status(conversation.ID); got != enums.ConversationStatusWaiting {
			t.Fatalf("status %q, want waiting", got)
		}

		if err := service.MarkWaiting(ctx, id.New()); !errors.Is(err, ErrNotFound) {
			t.Fatalf("marking a conversation that does not exist: got %v, want ErrNotFound", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}
