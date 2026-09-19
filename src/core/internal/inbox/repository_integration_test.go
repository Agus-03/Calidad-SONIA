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
// trip PostgreSQL to Go to PostgreSQL with its microseconds and the comparison stays strict (ADR-0036, regla 3).
func TestTheCursorContinuesStrictlyAfterTheRowItNamed(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor) {
		w := seedWorld(t, ctx, exec)
		service := NewService(Options{Repo: NewRepository(exec)})
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

		rows, err := repo.List(ctx, Scope{AccountID: w.account, OperatorID: w.mine, Role: enums.OperatorRoleAdmin}, nil, 3)
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
		Tx: func(_ context.Context, fn func(Store, Aggregate) error) error {
			return fn(repo, aggregate)
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

		message, err := service.Send(ctx, mine, w.pooledConv, "buenas, te ayudo")
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
		if _, err := service.Send(ctx, theirs, w.pooledConv, "yo también"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("the other operator answering: got %v, want ErrNotFound", err)
		}
		theirs.Role = enums.OperatorRoleSupervisor
		if _, err := service.Send(ctx, theirs, w.pooledConv, "yo también"); !errors.Is(err, ErrTaken) {
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

		message, err := service.Send(ctx, Scope{AccountID: w.account, OperatorID: w.mine, Role: enums.OperatorRoleOperator}, w.pooledConv, "hola")
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

		_, err := sendingDesk(exec, sender).Send(ctx, Scope{AccountID: w.account, OperatorID: w.mine, Role: enums.OperatorRoleOperator}, w.pooledConv, "hola")
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
			rows, err := NewRepository(exec).List(ctx, supervisor, nil, 1000)
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
