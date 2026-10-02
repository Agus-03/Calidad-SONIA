package inbox

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/conversations"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/httpclient"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
	"github.com/mateo-velasquez/sonia/src/core/internal/turn"
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
// the newest activity of all, so if the filter slipped it would be the first row every role sees (ADR-0035, regla 1).
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
				rows, err := repo.List(ctx, Scope{AccountID: w.account, OperatorID: w.mine, Role: tc.role}, Filter{}, nil, tc.limit)
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
// trip PostgreSQL to Go to PostgreSQL with its microseconds and the comparison stays strict (ADR-0036, regla 3).
func TestTheCursorContinuesStrictlyAfterTheRowItNamed(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor) {
		w := seedWorld(t, ctx, exec)
		service := NewService(Options{Repo: NewRepository(exec)})
		scope := Scope{AccountID: w.account, OperatorID: w.mine, Role: enums.OperatorRoleAdmin}

		var after *Cursor
		for i, want := range []uuid.UUID{w.theirsConv, w.mineConv, w.pooledConv} {
			page, err := service.List(ctx, scope, Filter{}, after, 1)
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

		rows, err := NewRepository(exec).List(ctx, Scope{AccountID: w.account, OperatorID: w.mine, Role: enums.OperatorRoleAdmin}, Filter{}, nil, 3)
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

// TestAContactTakenDownLeavesTheInbox is ADR-0035, regla 1, against real DDL: the conversation of a contact an admin took down is
// neither listed nor reachable by its id, and it stays in the database as history (C09).
func TestAContactTakenDownLeavesTheInbox(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor) {
		w := seedWorld(t, ctx, exec)
		repo := NewRepository(exec)

		if _, err := exec.Exec(ctx,
			`UPDATE contacts SET deleted_at = now() WHERE id = (SELECT contact_id FROM conversations WHERE id = $1)`, w.pooledConv); err != nil {
			t.Fatalf("take the contact down: %v", err)
		}

		rows, err := repo.List(ctx, Scope{AccountID: w.account, OperatorID: w.mine, Role: enums.OperatorRoleAdmin}, Filter{}, nil, 3)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		got := idsOf(rows)
		if len(got) < 2 || got[0] != w.theirsConv || got[1] != w.mineConv {
			t.Fatalf("the head of the inbox is %v, want %v and %v first", got, w.theirsConv, w.mineConv)
		}
		for _, conversation := range got {
			if conversation == w.pooledConv {
				t.Fatalf("the conversation of a contact taken down is still listed: %v", got)
			}
		}

		if _, err := repo.Target(ctx, w.account, w.pooledConv); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Target of a contact taken down: got %v, want ErrNotFound", err)
		}

		var kept bool
		if err := exec.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM messages WHERE conversation_id = $1)`, w.pooledConv).Scan(&kept); err != nil {
			t.Fatalf("look for the history: %v", err)
		}
		if !kept {
			t.Fatal("the history of the conversation went with the contact")
		}
	})
}

// sendingDesk is the real repository and the real aggregate bound to the case's transaction, with only the channel doubled: the
// writes and their order are what these cases prove, and Meta is the one collaborator that must never be called from a test.
func sendingDesk(exec db.Executor, sender *stubSender) *Service {
	repo := NewRepository(exec)
	aggregate := conversations.NewService(conversations.NewRepository(exec), nil)
	return NewService(Options{
		Repo:      repo,
		Aggregate: aggregate,
		Tx: func(_ context.Context, fn func(Store, Aggregate, Reviews) error) error {
			return fn(repo, aggregate, turn.NewRepository(exec))
		},
		Channel: &stubChannel{},
		Sender:  sender,
		Logger:  slog.New(slog.DiscardHandler),
	})
}

// TestTheFirstOperatorToAnswerThePoolKeepsIt is ADR-0004 end to end against real DDL: the answer, the owner, the history row and
// the first response land together, and from then on the conversation is out of the other operator's reach (ADR-0035, reglas 6 y 7).
func TestTheFirstOperatorToAnswerThePoolKeepsIt(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor) {
		w := seedWorld(t, ctx, exec)
		service := sendingDesk(exec, &stubSender{wamid: "wamid.integration." + id.New().String()})
		mine := Scope{AccountID: w.account, OperatorID: w.mine, Role: enums.OperatorRoleOperator}

		message, err := service.Send(ctx, mine, w.pooledConv, "buenas, te ayudo", nil)
		if err != nil {
			t.Fatalf("Send: %v", err)
		}
		if message.Status == nil || *message.Status != enums.MessageStatusSent {
			t.Fatalf("status %v, want sent", message.Status)
		}

		var owner *uuid.UUID
		var firstResponse *time.Time
		if err := exec.QueryRow(ctx, `SELECT operator_id, first_response_at FROM conversations WHERE id = $1`, w.pooledConv).
			Scan(&owner, &firstResponse); err != nil {
			t.Fatalf("read the conversation: %v", err)
		}
		if owner == nil || *owner != w.mine {
			t.Fatalf("the conversation belongs to %v, want the operator who answered, %v", owner, w.mine)
		}
		if firstResponse == nil {
			t.Fatalf("first_response_at is still empty after the first answer went out")
		}

		var from, by *uuid.UUID
		var to uuid.UUID
		var reason *string
		if err := exec.QueryRow(ctx, `
			SELECT from_operator_id, to_operator_id, assigned_by_operator_id, reason
			FROM   conversation_assignments WHERE conversation_id = $1`, w.pooledConv).Scan(&from, &to, &by, &reason); err != nil {
			t.Fatalf("read the assignment: %v", err)
		}
		if from != nil || to != w.mine || by == nil || *by != w.mine || reason == nil || *reason != autoAssignReason {
			t.Fatalf("assignment from %v to %v by %v reason %v, want from nobody to me, by me", from, to, by, reason)
		}

		var senderType string
		var senderOperator *uuid.UUID
		var wamid *string
		if err := exec.QueryRow(ctx, `SELECT sender_type, sender_operator_id, wamid FROM messages WHERE id = $1`, message.ID).
			Scan(&senderType, &senderOperator, &wamid); err != nil {
			t.Fatalf("read the message: %v", err)
		}
		if senderType != "operator" || senderOperator == nil || *senderOperator != w.mine || wamid == nil {
			t.Fatalf("message sender %q %v wamid %v, want the operator and Meta's id", senderType, senderOperator, wamid)
		}

		// Taken now: the other operator no longer reaches it, and a supervisor is told who holds it.
		theirs := Scope{AccountID: w.account, OperatorID: w.theirs, Role: enums.OperatorRoleOperator}
		if _, err := service.Send(ctx, theirs, w.pooledConv, "yo también", nil); !errors.Is(err, ErrNotFound) {
			t.Fatalf("the other operator answering: got %v, want ErrNotFound", err)
		}
		theirs.Role = enums.OperatorRoleSupervisor
		if _, err := service.Send(ctx, theirs, w.pooledConv, "yo también", nil); !errors.Is(err, ErrTaken) {
			t.Fatalf("a supervisor answering: got %v, want ErrTaken", err)
		}
	})
}

// TestAFailedManualSendStaysOnTheRowWithAnOwner is ADR-0020 against real DDL: the row says failed with Meta's code, nothing counts it
// as a response, and the conversation keeps the operator who has to decide whether to send it again (ADR-0035, regla 7).
func TestAFailedManualSendStaysOnTheRowWithAnOwner(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor) {
		w := seedWorld(t, ctx, exec)
		service := sendingDesk(exec, &stubSender{err: &httpclient.StatusError{
			Status: 400, Body: `{"error":{"message":"(#131030) Recipient not in allowed list","code":131030}}`,
		}})

		message, err := service.Send(ctx, Scope{AccountID: w.account, OperatorID: w.mine, Role: enums.OperatorRoleOperator}, w.pooledConv, "hola", nil)
		if err != nil {
			t.Fatalf("Send: got %v, want the failed row", err)
		}

		var status, code *string
		if err := exec.QueryRow(ctx, `SELECT status, error_code FROM messages WHERE id = $1`, message.ID).Scan(&status, &code); err != nil {
			t.Fatalf("read the message: %v", err)
		}
		if status == nil || *status != string(enums.MessageStatusFailed) || code == nil || *code != "131030" {
			t.Fatalf("the row reads status %v code %v, want failed with Meta's code", status, code)
		}

		var owner *uuid.UUID
		var firstResponse *time.Time
		if err := exec.QueryRow(ctx, `SELECT operator_id, first_response_at FROM conversations WHERE id = $1`, w.pooledConv).
			Scan(&owner, &firstResponse); err != nil {
			t.Fatalf("read the conversation: %v", err)
		}
		if owner == nil || *owner != w.mine {
			t.Fatalf("the failed answer left the conversation with %v, want its author", owner)
		}
		if firstResponse != nil {
			t.Fatalf("a failed send was counted as the first response")
		}
	})
}

// TestAClosedWindowWritesNothing refuses before the transaction, so not even the claim survives (ADR-0008, ADR-0035 regla 9).
func TestAClosedWindowWritesNothing(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor) {
		w := seedWorld(t, ctx, exec)
		if _, err := exec.Exec(ctx, `UPDATE conversations SET window_expires_at = now() - interval '1 minute' WHERE id = $1`, w.pooledConv); err != nil {
			t.Fatalf("close the window: %v", err)
		}
		sender := &stubSender{wamid: "wamid.never"}

		_, err := sendingDesk(exec, sender).Send(ctx, Scope{AccountID: w.account, OperatorID: w.mine, Role: enums.OperatorRoleOperator}, w.pooledConv, "hola", nil)
		if !errors.Is(err, ErrWindowClosed) {
			t.Fatalf("Send: got %v, want ErrWindowClosed", err)
		}

		var owner *uuid.UUID
		var assignments int
		if err := exec.QueryRow(ctx, `
			SELECT c.operator_id, (SELECT count(*) FROM conversation_assignments a WHERE a.conversation_id = c.id)
			FROM   conversations c WHERE c.id = $1`, w.pooledConv).Scan(&owner, &assignments); err != nil {
			t.Fatalf("read the conversation: %v", err)
		}
		if owner != nil || assignments != 0 || sender.calls != 0 {
			t.Fatalf("a refused answer claimed %v, wrote %d assignments and sent %d times", owner, assignments, sender.calls)
		}
	})
}

// TestASupervisorTurnsTheBotOffWithoutTakingTheConversation is F27 end to end against real DDL: the mode moves, the agent and the
// holder stay where they were, and a conversation born without an agent cannot be turned on (ADR-0035, reglas 12 y 13).
func TestASupervisorTurnsTheBotOffWithoutTakingTheConversation(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor) {
		w := seedWorld(t, ctx, exec)
		// One that can answer: since ADR-0038 a paused or removed agent refuses the switch, and the table may hold one.
		if _, err := exec.Exec(ctx, `
			UPDATE conversations
			SET    ai_agent_id = (SELECT id FROM ai_agents WHERE active AND deleted_at IS NULL LIMIT 1)
			WHERE  id = $1`, w.mineConv); err != nil {
			t.Fatalf("give the conversation an agent: %v", err)
		}
		service := sendingDesk(exec, &stubSender{})
		supervisor := Scope{AccountID: w.account, OperatorID: w.theirs, Role: enums.OperatorRoleSupervisor}

		read := func(conversationID uuid.UUID) (enums.AgentMode, *uuid.UUID, *uuid.UUID) {
			t.Helper()
			var mode enums.AgentMode
			var agent, holder *uuid.UUID
			if err := exec.QueryRow(ctx, `SELECT mode, ai_agent_id, operator_id FROM conversations WHERE id = $1`, conversationID).
				Scan(&mode, &agent, &holder); err != nil {
				t.Fatalf("read the conversation: %v", err)
			}
			return mode, agent, holder
		}

		for _, want := range []enums.AgentMode{enums.AgentModeAutonomous, enums.AgentModeOff} {
			got, err := service.SetMode(ctx, supervisor, w.mineConv, want)
			if err != nil || got != want {
				t.Fatalf("SetMode %s on another operator's conversation: got %q, %v", want, got, err)
			}
			mode, agent, holder := read(w.mineConv)
			if mode != want || agent == nil {
				t.Fatalf("after %s: mode %q agent %v, want the mode moved and the agent kept", want, mode, agent)
			}
			if holder == nil || *holder != w.mine {
				t.Fatalf("changing the mode moved the conversation to %v", holder)
			}
		}

		if _, err := service.SetMode(ctx, supervisor, w.pooledConv, enums.AgentModeAutonomous); !errors.Is(err, ErrNoAIAgent) {
			t.Fatalf("turning on a conversation with no agent: got %v, want ErrNoAIAgent", err)
		}
		if _, _, holder := read(w.pooledConv); holder != nil {
			t.Fatalf("a mode request took the pool for %v", *holder)
		}

		// The agent read through Target: once the admin pauses it, the switch is refused before the write (ADR-0038).
		paused := id.New()
		if _, err := exec.Exec(ctx, `
			INSERT INTO ai_agents (id, account_id, name, default_mode, llm_provider, llm_model, active)
			VALUES ($1, $2, 'Agente pausado', 'autonomous', 'cohere', 'command-a-03-2025', false)`, paused, w.account); err != nil {
			t.Fatalf("insert a paused agent: %v", err)
		}
		if _, err := exec.Exec(ctx, `UPDATE conversations SET ai_agent_id = $1 WHERE id = $2`, paused, w.mineConv); err != nil {
			t.Fatalf("give the conversation the paused agent: %v", err)
		}
		if _, err := service.SetMode(ctx, supervisor, w.mineConv, enums.AgentModeAutonomous); !errors.Is(err, ErrAIAgentUnavailable) {
			t.Fatalf("turning on a conversation whose agent is paused: got %v, want ErrAIAgentUnavailable", err)
		}
		if mode, _, _ := read(w.mineConv); mode != enums.AgentModeOff {
			t.Fatalf("the refused switch still wrote %q", mode)
		}

		// And the listing says so before anyone tries: the state of the bot F22 shows, read with the same three conditions.
		available := func() bool {
			t.Helper()
			rows, err := NewRepository(exec).List(ctx, supervisor, Filter{}, nil, 1000)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			for _, s := range rows {
				if s.ID == w.mineConv {
					return s.AIAgentAvailable
				}
			}
			t.Fatalf("the listing does not show %v", w.mineConv)
			return false
		}
		if available() {
			t.Fatal("the listing says the paused agent can answer")
		}
		if _, err := exec.Exec(ctx, `UPDATE ai_agents SET active = true WHERE id = $1`, paused); err != nil {
			t.Fatalf("resume the agent: %v", err)
		}
		if !available() {
			t.Fatal("the listing says the resumed agent cannot answer")
		}
	})
}

// TestAStarIsSharedAndLeavesTheInboxInPlace is F25 end to end against real DDL: the operator stars a conversation of the pool, the
// supervisor sees the same star on the listing, and the order of the inbox does not move, so no cursor in circulation shifts (ADR-0036).
func TestAStarIsSharedAndLeavesTheInboxInPlace(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor) {
		w := seedWorld(t, ctx, exec)
		service := sendingDesk(exec, &stubSender{})
		operator := Scope{AccountID: w.account, OperatorID: w.mine, Role: enums.OperatorRoleOperator}
		supervisor := Scope{AccountID: w.account, OperatorID: w.theirs, Role: enums.OperatorRoleSupervisor}
		head := []uuid.UUID{w.theirsConv, w.mineConv, w.pooledConv}

		starredOf := func() map[uuid.UUID]bool {
			t.Helper()
			rows, err := NewRepository(exec).List(ctx, supervisor, Filter{}, nil, len(head))
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if got := idsOf(rows); !sameIDs(got, head) {
				t.Fatalf("the head of the inbox is %v, want %v in the same order as before the star", got, head)
			}
			out := map[uuid.UUID]bool{}
			for _, r := range rows {
				out[r.ID] = r.Starred
			}
			return out
		}

		if starredOf()[w.pooledConv] {
			t.Fatal("the pooled conversation is starred before anyone starred it")
		}
		if got, err := service.SetStarred(ctx, operator, w.pooledConv, true); err != nil || !got {
			t.Fatalf("the operator starring the pool: got %v, %v", got, err)
		}
		if starred := starredOf(); !starred[w.pooledConv] || starred[w.mineConv] || starred[w.theirsConv] {
			t.Fatalf("after one star the listing says %v, want only the pooled conversation starred", starred)
		}

		// Another's conversation stays out of reach for the operator, and nothing is written.
		if _, err := service.SetStarred(ctx, operator, w.theirsConv, true); !errors.Is(err, ErrNotFound) {
			t.Fatalf("the operator starring another's conversation: got %v, want ErrNotFound", err)
		}

		if got, err := service.SetStarred(ctx, supervisor, w.pooledConv, false); err != nil || got {
			t.Fatalf("the supervisor clearing the star: got %v, %v", got, err)
		}
		if starred := starredOf(); starred[w.pooledConv] || starred[w.theirsConv] {
			t.Fatalf("after clearing the listing says %v, want nothing starred", starred)
		}

		// Starring never takes the pool (ADR-0035, regla 13).
		var holder *uuid.UUID
		if err := exec.QueryRow(ctx, `SELECT operator_id FROM conversations WHERE id = $1`, w.pooledConv).Scan(&holder); err != nil {
			t.Fatalf("read the holder: %v", err)
		}
		if holder != nil {
			t.Fatalf("a star took the pool for %v", *holder)
		}
	})
}

// TestEachFilterOfF23AgainstRealDDL gives each seeded conversation one mark a filter reads, and checks every filter answers its own
// among them: the SQL of narrow() is only proven here, since the stub filters in Go (ADR-0050). The world is read out of the whole
// filtered listing because the demo rows may match a filter too.
func TestEachFilterOfF23AgainstRealDDL(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor) {
		w := seedWorld(t, ctx, exec)
		mark := func(what, sql string, args ...any) {
			t.Helper()
			if _, err := exec.Exec(ctx, sql, args...); err != nil {
				t.Fatalf("%s: %v", what, err)
			}
		}

		// Theirs: unread and waiting. Mine: assigned already, starred, and resolved. The pool: a label and a reminder.
		mark("unread", `UPDATE conversations SET unread_count = 2, status = 'waiting' WHERE id = $1`, w.theirsConv)
		mark("starred", `UPDATE conversations SET starred = true, status = 'resolved' WHERE id = $1`, w.mineConv)
		label, gone := id.New(), id.New()
		for _, l := range []uuid.UUID{label, gone} {
			mark("a label", `INSERT INTO labels (id, account_id, name, color, created_by_operator_id) VALUES ($1, $2, $3, '#000000', $4)`,
				l, w.account, "Filtro "+l.String(), w.mine)
			mark("label the pool", `INSERT INTO conversation_labels (id, conversation_id, label_id) VALUES ($1, $2, $3)`, id.New(), w.pooledConv, l)
		}
		// A label taken out of the catalog no longer filters (C09).
		mark("delete a label", `UPDATE labels SET deleted_at = now() WHERE id = $1`, gone)
		var contact uuid.UUID
		if err := exec.QueryRow(ctx, `SELECT contact_id FROM conversations WHERE id = $1`, w.pooledConv).Scan(&contact); err != nil {
			t.Fatalf("read the contact: %v", err)
		}
		mark("a reminder", `INSERT INTO reminders (id, contact_id, operator_id, description, due_at) VALUES ($1, $2, $3, 'llamar', now())`,
			id.New(), contact, w.mine)
		// A deleted reminder of another contact marks nothing.
		var theirsContact uuid.UUID
		if err := exec.QueryRow(ctx, `SELECT contact_id FROM conversations WHERE id = $1`, w.theirsConv).Scan(&theirsContact); err != nil {
			t.Fatalf("read the contact: %v", err)
		}
		mark("a deleted reminder", `INSERT INTO reminders (id, contact_id, operator_id, description, due_at, deleted_at) VALUES ($1, $2, $3, 'no', now(), now())`,
			id.New(), theirsContact, w.mine)

		waiting, resolved := enums.ConversationStatusWaiting, enums.ConversationStatusResolved
		admin := Scope{AccountID: w.account, OperatorID: w.mine, Role: enums.OperatorRoleAdmin}
		operator := Scope{AccountID: w.account, OperatorID: w.mine, Role: enums.OperatorRoleOperator}
		for _, tc := range []struct {
			name   string
			scope  Scope
			filter Filter
			want   []uuid.UUID
		}{
			{"no leídas", admin, Filter{Unread: true}, []uuid.UUID{w.theirsConv}},
			{"asignadas", admin, Filter{Assigned: true}, []uuid.UUID{w.theirsConv, w.mineConv}},
			{"asignadas, for an operator, are its own", operator, Filter{Assigned: true}, []uuid.UUID{w.mineConv}},
			{"destacadas", admin, Filter{Starred: true}, []uuid.UUID{w.mineConv}},
			{"en espera", admin, Filter{Status: &waiting}, []uuid.UUID{w.theirsConv}},
			{"resueltas", admin, Filter{Status: &resolved}, []uuid.UUID{w.mineConv}},
			{"por etiqueta", admin, Filter{LabelID: &label}, []uuid.UUID{w.pooledConv}},
			{"por una etiqueta borrada", admin, Filter{LabelID: &gone}, []uuid.UUID{}},
			{"con recordatorio", admin, Filter{WithReminder: true}, []uuid.UUID{w.pooledConv}},
			{"combinadas", admin, Filter{Assigned: true, Unread: true, Status: &waiting}, []uuid.UUID{w.theirsConv}},
			{"combinadas sin coincidencia", admin, Filter{Starred: true, WithReminder: true}, []uuid.UUID{}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				rows, err := NewRepository(exec).List(ctx, tc.scope, tc.filter, nil, 1000)
				if err != nil {
					t.Fatalf("List: %v", err)
				}
				seeded := map[uuid.UUID]bool{w.theirsConv: true, w.mineConv: true, w.pooledConv: true, w.deletedConv: true}
				got := []uuid.UUID{}
				for _, r := range rows {
					if seeded[r.ID] {
						got = append(got, r.ID)
					}
				}
				if !sameIDs(got, tc.want) {
					t.Fatalf("the filtered inbox holds %v of the seeded ones, want %v", got, tc.want)
				}
			})
		}
	})
}

// TestTheRowOfAnEventIsTheRowOfTheListing is SummaryOf against real DDL: the event of F21 carries the same row the listing draws,
// with its account, and a deleted conversation or one never written is not found (C09, ADR-0051).
func TestTheRowOfAnEventIsTheRowOfTheListing(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor) {
		w := seedWorld(t, ctx, exec)
		repo := NewRepository(exec)

		account, row, err := repo.SummaryOf(ctx, w.mineConv)
		if err != nil {
			t.Fatalf("SummaryOf: %v", err)
		}
		if account != w.account {
			t.Fatalf("the account is %v, want %v", account, w.account)
		}
		listed, err := repo.List(ctx, Scope{AccountID: w.account, OperatorID: w.mine, Role: enums.OperatorRoleAdmin}, Filter{}, nil, 3)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		var fromListing *Summary
		for i := range listed {
			if listed[i].ID == w.mineConv {
				fromListing = &listed[i]
			}
		}
		if fromListing == nil {
			t.Fatalf("the listing does not show %v", w.mineConv)
		}
		// Nobody scored it, and nil is how the row says so: a zero would read as a score off the scale (ADR-0055).
		if row.QualityScore != nil || fromListing.QualityScore != nil {
			t.Fatalf("an unscored conversation carries the scores %v and %v", row.QualityScore, fromListing.QualityScore)
		}
		if row.Operator == nil || row.Operator.ID != w.mine || row.FailedCount != fromListing.FailedCount ||
			row.LastMessage == nil || fromListing.LastMessage == nil || row.LastMessage.ID != fromListing.LastMessage.ID {
			t.Fatalf("the event row %+v differs from the listing row %+v", row, *fromListing)
		}

		for _, missing := range []uuid.UUID{w.deletedConv, id.New()} {
			if _, _, err := repo.SummaryOf(ctx, missing); !errors.Is(err, ErrNotFound) {
				t.Fatalf("SummaryOf %v: got %v, want ErrNotFound", missing, err)
			}
		}
	})
}

// TestAnswerAndReadClearTheUnreadAgainstRealDDL is ADR-0052 through the inbox: answering the pool clears it with the real aggregate,
// a supervisor cannot clear a vendor's, the holder can, and the filter of F23 follows (ADR-0050).
func TestAnswerAndReadClearTheUnreadAgainstRealDDL(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor) {
		w := seedWorld(t, ctx, exec)
		if _, err := exec.Exec(ctx, `UPDATE conversations SET unread_count = 2 WHERE id = ANY($1)`, []uuid.UUID{w.pooledConv, w.mineConv}); err != nil {
			t.Fatalf("mark unread: %v", err)
		}
		service := sendingDesk(exec, &stubSender{wamid: "wamid.double"})
		mine := Scope{AccountID: w.account, OperatorID: w.mine, Role: enums.OperatorRoleOperator}
		supervisor := Scope{AccountID: w.account, OperatorID: w.theirs, Role: enums.OperatorRoleSupervisor}
		unread := func(conversationID uuid.UUID) int {
			t.Helper()
			var n int
			if err := exec.QueryRow(ctx, `SELECT unread_count FROM conversations WHERE id = $1`, conversationID).Scan(&n); err != nil {
				t.Fatalf("read unread: %v", err)
			}
			return n
		}

		// The pool's window is seeded in 2999, so the answer is not refused before it is written.
		if _, err := service.Send(ctx, mine, w.pooledConv, "te ayudo", nil); err != nil {
			t.Fatalf("answering the pool: %v", err)
		}
		if n := unread(w.pooledConv); n != 0 {
			t.Fatalf("after answering: unread %d, want 0", n)
		}

		if err := service.MarkRead(ctx, supervisor, w.mineConv); !errors.Is(err, ErrTaken) {
			t.Fatalf("a supervisor on a vendor's conversation: got %v, want ErrTaken", err)
		}
		if n := unread(w.mineConv); n != 2 {
			t.Fatalf("the supervisor's look cleared the vendor's warning: unread %d", n)
		}
		rows, err := NewRepository(exec).List(ctx, supervisor, Filter{Unread: true}, nil, 1000)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if !containsID(rows, w.mineConv) || containsID(rows, w.pooledConv) {
			t.Fatalf("no leidas holds %v, want the vendor's and not the answered one", idsOf(rows))
		}

		if err := service.MarkRead(ctx, mine, w.mineConv); err != nil {
			t.Fatalf("the holder marking read: %v", err)
		}
		if n := unread(w.mineConv); n != 0 {
			t.Fatalf("after the holder read it: unread %d, want 0", n)
		}
	})
}

func containsID(rows []Summary, want uuid.UUID) bool {
	for _, r := range rows {
		if r.ID == want {
			return true
		}
	}
	return false
}

// TestTheThreadIsReadNewestFirstWithItsFailures proves the thread against real DDL: newest first by our clock (ADR-0036, regla 2), the
// failed answer in its place with its code and its author, the hidden message left out, and the reach of the listing applied to one
// conversation (ADR-0035, regla 2).
func TestTheThreadIsReadNewestFirstWithItsFailures(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor) {
		w := seedWorld(t, ctx, exec)
		service := NewService(Options{Repo: NewRepository(exec)})
		mine := Scope{AccountID: w.account, OperatorID: w.mine, Role: enums.OperatorRoleOperator}

		page, err := service.Thread(ctx, mine, w.mineConv, nil, 20)
		if err != nil {
			t.Fatalf("Thread: %v", err)
		}
		if len(page.Items) != 2 || page.Next != nil {
			t.Fatalf("got %d messages and next %v, want the two seeded and no cursor", len(page.Items), page.Next)
		}
		answer, question := page.Items[0], page.Items[1]
		if answer.Direction != enums.MessageDirectionOutbound || question.Direction != enums.MessageDirectionInbound {
			t.Fatalf("the thread reads %s then %s, want the answer first: newest first", answer.Direction, question.Direction)
		}
		if answer.Status == nil || *answer.Status != enums.MessageStatusFailed || answer.ErrorCode == nil || *answer.ErrorCode != "131047" {
			t.Fatalf("the failed answer lost its status or its cause: %+v", answer.Message)
		}
		if answer.SenderOperator == nil || answer.SenderOperator.ID != w.mine || answer.SenderOperator.Name != "Prueba" {
			t.Fatalf("the answer does not say who wrote it: %+v", answer.SenderOperator)
		}
		if question.SenderOperator != nil {
			t.Fatalf("the customer's message carries an operator: %+v", question.SenderOperator)
		}

		// One message per page: the cursor made of our clock and the id goes back strictly past the row it named.
		first, err := service.Thread(ctx, mine, w.mineConv, nil, 1)
		if err != nil || len(first.Items) != 1 || first.Next == nil {
			t.Fatalf("first page: %d messages, next %v, err %v", len(first.Items), first.Next, err)
		}
		second, err := service.Thread(ctx, mine, w.mineConv, first.Next, 1)
		if err != nil || len(second.Items) != 1 || second.Items[0].ID != question.ID || second.Next != nil {
			t.Fatalf("second page: %v, next %v, err %v, want the question and the end", idsOfThread(second.Items), second.Next, err)
		}

		if _, err := service.Thread(ctx, mine, w.theirsConv, nil, 20); !errors.Is(err, ErrNotFound) {
			t.Fatalf("an operator reading another's thread: got %v, want ErrNotFound", err)
		}
		supervisor := Scope{AccountID: w.account, OperatorID: w.mine, Role: enums.OperatorRoleSupervisor}
		hidden, err := service.Thread(ctx, supervisor, w.theirsConv, nil, 20)
		if err != nil {
			t.Fatalf("a supervisor reading another's thread: %v", err)
		}
		if len(hidden.Items) != 0 {
			t.Fatalf("a hidden message reached the thread: %v", idsOfThread(hidden.Items))
		}
	})
}

func idsOfThread(items []ThreadMessage) []uuid.UUID {
	out := []uuid.UUID{}
	for _, m := range items {
		out = append(out, m.ID)
	}
	return out
}

// ADR-0046, section 2.5, against the real DDL: the newest draft still pending is the one the Operator copies; one already sent and
// an older one nobody used are not, and a conversation with none answers ErrNoSuggestion.
func TestPendingSuggestionIsTheNewestDraftStillWaiting(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor) {
		w := seedWorld(t, ctx, exec)
		repo := NewRepository(exec)

		write := func(status enums.GenerationStatus, text string, age time.Duration) uuid.UUID {
			generationID := id.New()
			if _, err := exec.Exec(ctx, `
				INSERT INTO generations (id, conversation_id, ai_agent_id, llm_provider, llm_model, prompt_rendered, completion_text,
				                         prompt_tokens, completion_tokens, latency_ms, cost, currency, status, assembled_at, created_at)
				SELECT $1, $2, a.id, 'double', 'command-a-03-2025', 'system', $3, 1, 1, 1, 0, 'USD', $4, now(), now() - $5 * interval '1 second'
				FROM   ai_agents a LIMIT 1`, generationID, w.mineConv, text, status, age.Seconds()); err != nil {
				t.Fatalf("insert generation: %v", err)
			}
			return generationID
		}
		write(enums.GenerationStatusPendingReview, "vieja", 60*time.Second)
		newest := write(enums.GenerationStatusPendingReview, "nueva", 30*time.Second)
		write(enums.GenerationStatusAutoSent, "respuesta", 0)

		got, err := repo.PendingSuggestion(ctx, w.mineConv)
		if err != nil || got.GenerationID != newest || got.Text != "nueva" {
			t.Fatalf("PendingSuggestion: got %+v, %v, want the newest pending draft", got, err)
		}
		if _, err := repo.PendingSuggestion(ctx, w.theirsConv); !errors.Is(err, ErrNoSuggestion) {
			t.Fatalf("PendingSuggestion of a conversation with none: got %v, want ErrNoSuggestion", err)
		}
	})
}

// ADR-0049 against the real DDL: the components come in the order they were assembled, with the version that was used and not the
// current one, a component removed afterwards still shows, and an id of another conversation is not found. The thread names the
// generation behind the answer it produced.
func TestTheReasoningIsTheVersionsTheGenerationWasAssembledFrom(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor) {
		w := seedWorld(t, ctx, exec)
		repo := NewRepository(exec)

		agentID := id.New()
		if _, err := exec.Exec(ctx, `
			INSERT INTO ai_agents (id, account_id, name, default_mode, llm_provider, llm_model)
			SELECT $1, a.id, 'Razona', 'autonomous', 'double', 'command-a-03-2025' FROM accounts a LIMIT 1`, agentID); err != nil {
			t.Fatalf("insert agent: %v", err)
		}
		version := func(kind enums.PromptComponentType, position, number int, content string) uuid.UUID {
			componentID, versionID := id.New(), id.New()
			if _, err := exec.Exec(ctx, `
				INSERT INTO prompt_components (id, ai_agent_id, type, content, position) VALUES ($1, $2, $3, $4, $5)`,
				componentID, agentID, kind, content+" (vigente)", position); err != nil {
				t.Fatalf("insert component: %v", err)
			}
			if _, err := exec.Exec(ctx, `
				INSERT INTO prompt_component_versions (id, prompt_component_id, version_number, content, active)
				VALUES ($1, $2, $3, $4, true)`, versionID, componentID, number, content); err != nil {
				t.Fatalf("insert version: %v", err)
			}
			if kind == enums.PromptComponentTypeRules {
				// Removed after the answer: what was assembled is still what was assembled (C09).
				if _, err := exec.Exec(ctx, `UPDATE prompt_components SET deleted_at = now() WHERE id = $1`, componentID); err != nil {
					t.Fatalf("remove component: %v", err)
				}
			}
			return versionID
		}
		objective := version(enums.PromptComponentTypeObjective, 1, 3, "Alquilar inmuebles.")
		rules := version(enums.PromptComponentTypeRules, 2, 1, "No tomar pagos.")

		generationID, answerID := id.New(), id.New()
		if _, err := exec.Exec(ctx, `
			INSERT INTO messages (id, conversation_id, direction, sender_type, sender_ai_agent_id, content_text, content_type, status, sent_at)
			VALUES ($1, $2, 'outbound', 'ai_agent', $3, 'hola', 'text', 'sent', now())`, answerID, w.mineConv, agentID); err != nil {
			t.Fatalf("insert answer: %v", err)
		}
		if _, err := exec.Exec(ctx, `
			INSERT INTO generations (id, conversation_id, ai_agent_id, message_id, llm_provider, llm_model, prompt_rendered, completion_text,
			                         prompt_tokens, completion_tokens, latency_ms, cost, currency, status, assembled_at)
			VALUES ($1, $2, $3, $4, 'double', 'command-a-03-2025', 'system', 'hola', 1, 1, 1, 0, 'USD', $5, now())`,
			generationID, w.mineConv, agentID, answerID, enums.GenerationStatusAutoSent); err != nil {
			t.Fatalf("insert generation: %v", err)
		}
		// Written out of order on purpose: position is what orders them, not the order of the rows.
		for _, gc := range []struct {
			version  uuid.UUID
			position int
		}{{rules, 2}, {objective, 1}} {
			if _, err := exec.Exec(ctx, `
				INSERT INTO generation_components (id, generation_id, prompt_component_version_id, position) VALUES ($1, $2, $3, $4)`,
				id.New(), generationID, gc.version, gc.position); err != nil {
				t.Fatalf("insert generation component: %v", err)
			}
		}

		got, err := repo.Reasoning(ctx, w.mineConv, generationID)
		if err != nil {
			t.Fatalf("Reasoning: %v", err)
		}
		if len(got.Components) != 2 ||
			got.Components[0].Type != enums.PromptComponentTypeObjective || got.Components[0].VersionNumber != 3 ||
			*got.Components[0].Content != "Alquilar inmuebles." ||
			got.Components[1].Type != enums.PromptComponentTypeRules || *got.Components[1].Content != "No tomar pagos." {
			t.Fatalf("components = %+v, want objective v3 then the removed rules v1, with the text that was used", got.Components)
		}
		if _, err := repo.Reasoning(ctx, w.theirsConv, generationID); !errors.Is(err, ErrGenerationNotFound) {
			t.Fatalf("the generation through another conversation: got %v, want ErrGenerationNotFound", err)
		}

		thread, err := repo.Thread(ctx, w.mineConv, nil, 50)
		if err != nil {
			t.Fatalf("Thread: %v", err)
		}
		for _, m := range thread {
			if m.ID == answerID {
				if m.GenerationID == nil || *m.GenerationID != generationID {
					t.Fatalf("the answer names generation %v, want %s", m.GenerationID, generationID)
				}
				return
			}
		}
		t.Fatal("the answer is not in the thread")
	})
}
