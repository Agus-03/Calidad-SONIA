package turn

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
var rollback = errors.New("turn: rolling back the integration case")

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

// conversation builds the row the turn hangs from, using the fixed rows of src/core/seeds that the phase runs on.
func conversation(t *testing.T, ctx context.Context, exec db.Executor) (conversationID uuid.UUID, phone string) {
	t.Helper()

	var account, inbox, agent uuid.UUID
	err := exec.QueryRow(ctx, `SELECT a.id, i.id, g.id FROM accounts a, inboxes i, ai_agents g LIMIT 1`).
		Scan(&account, &inbox, &agent)
	if err != nil {
		t.Skip("the demo rows are missing: load src/core/seeds/0001_demo.sql first")
	}

	contact := id.New()
	// The tail and not the head: a UUID v7 starts with the timestamp, so two contacts built in the same millisecond would
	// collide on uq_contacts_phone_whatsapp, and a case that needs two conversations builds them one after the other.
	phone = "549351" + contact.String()[24:]
	if _, err := exec.Exec(ctx,
		`INSERT INTO contacts (id, account_id, phone_whatsapp, name, lastname, type, source_channel, consent_given_at)
		 VALUES ($1, $2, $3, 'Prueba', 'Turno', 'client', 'direct', now())`, contact, account, phone); err != nil {
		t.Fatalf("insert contact: %v", err)
	}

	conversationID = id.New()
	now := time.Now().UTC()
	if _, err := exec.Exec(ctx,
		`INSERT INTO conversations (id, contact_id, inbox_id, ai_agent_id, status, mode, last_inbound_at, window_expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		conversationID, contact, inbox, agent, enums.ConversationStatusActive, enums.AgentModeAutonomous,
		now, now.Add(24*time.Hour)); err != nil {
		t.Fatalf("insert conversation: %v", err)
	}
	return conversationID, phone
}

// The turn needs which number sends and which receives, and neither is in the four ids the webhook hands over.
func TestTargetJoinsTheConversationToItsContact(t *testing.T) {
	ctx := context.Background()

	err := db.InTx(ctx, integrationPool(t), func(exec db.Executor) error {
		conversationID, phone := conversation(t, ctx, exec)

		target, err := NewRepository(exec).Target(ctx, conversationID)
		if err != nil {
			t.Fatalf("Target: got %v, want no error", err)
		}
		if target.ContactPhone != phone {
			t.Fatalf("phone = %q, want %q", target.ContactPhone, phone)
		}
		if target.Mode != enums.AgentModeAutonomous {
			t.Fatalf("mode = %q, want autonomous", target.Mode)
		}
		if target.InboxID == uuid.Nil {
			t.Fatal("the target carries no inbox, so nothing knows which number answers")
		}
		return rollback
	})

	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// The pause and the removal are read from ai_agents at the moment of the turn, against real DDL: a stub cannot tell a NULL agent
// from a NULL active, and the COALESCE is what keeps the first from scanning into a bool (ADR-0038).
func TestTargetSaysWhetherTheAIAgentCanAnswer(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name  string
		agent string
		want  bool
	}{
		{"an active agent answers", `UPDATE ai_agents SET active = true WHERE id = $1`, true},
		{"a paused agent keeps its configuration and answers nothing", `UPDATE ai_agents SET active = false WHERE id = $1`, false},
		{"a removed agent answers nothing even if it was active", `UPDATE ai_agents SET deleted_at = now() WHERE id = $1`, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := db.InTx(ctx, integrationPool(t), func(exec db.Executor) error {
				conversationID, _ := conversation(t, ctx, exec)
				agentID := ownAIAgent(t, ctx, exec, conversationID)
				if _, err := exec.Exec(ctx, tc.agent, agentID); err != nil {
					t.Fatalf("set the agent up: %v", err)
				}

				target, err := NewRepository(exec).Target(ctx, conversationID)
				if err != nil {
					t.Fatalf("Target: got %v, want no error", err)
				}
				if target.AIAgentAvailable != tc.want {
					t.Fatalf("AIAgentAvailable = %v, want %v", target.AIAgentAvailable, tc.want)
				}
				return rollback
			})
			if !errors.Is(err, rollback) {
				t.Fatalf("InTx: got %v, want the rollback sentinel", err)
			}
		})
	}

	t.Run("a conversation born without an agent has nobody to answer", func(t *testing.T) {
		err := db.InTx(ctx, integrationPool(t), func(exec db.Executor) error {
			conversationID, _ := conversation(t, ctx, exec)
			if _, err := exec.Exec(ctx,
				`UPDATE conversations SET ai_agent_id = NULL, mode = 'off' WHERE id = $1`, conversationID); err != nil {
				t.Fatalf("take the agent away: %v", err)
			}

			target, err := NewRepository(exec).Target(ctx, conversationID)
			if err != nil {
				t.Fatalf("Target: got %v, want no error", err)
			}
			if target.AIAgentAvailable {
				t.Fatal("AIAgentAvailable = true for a conversation with no agent")
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			t.Fatalf("InTx: got %v, want the rollback sentinel", err)
		}
	})
}

// ownAIAgent gives the conversation an agent of its own, so a case that pauses or removes it never touches the demo agent.
func ownAIAgent(t *testing.T, ctx context.Context, exec db.Executor, conversationID uuid.UUID) uuid.UUID {
	t.Helper()

	agentID := id.New()
	if _, err := exec.Exec(ctx, `
		INSERT INTO ai_agents (id, account_id, name, default_mode, llm_provider, llm_model)
		SELECT $1, account_id, 'Agente de integracion', 'autonomous', 'cohere', 'command-a-03-2025'
		FROM   ai_agents
		WHERE  id = (SELECT ai_agent_id FROM conversations WHERE id = $2)`, agentID, conversationID); err != nil {
		t.Fatalf("insert an agent: %v", err)
	}
	if _, err := exec.Exec(ctx, `UPDATE conversations SET ai_agent_id = $1 WHERE id = $2`, agentID, conversationID); err != nil {
		t.Fatalf("give the conversation its agent: %v", err)
	}
	return agentID
}

func TestTargetReportsAConversationThatIsNotThere(t *testing.T) {
	ctx := context.Background()

	err := db.InTx(ctx, integrationPool(t), func(exec db.Executor) error {
		if _, err := NewRepository(exec).Target(ctx, id.New()); !errors.Is(err, ErrConversationNotFound) {
			t.Fatalf("Target: got %v, want ErrConversationNotFound", err)
		}
		return rollback
	})

	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// The whole of regla 2 against real DDL: the second message joins the turn the first opened instead of starting one of its own,
// and the wait starts over, which is what F10 calls resetting the timer.
func TestASecondMessageJoinsTheOpenBatchAndPushesTheWaitForward(t *testing.T) {
	ctx := context.Background()

	err := db.InTx(ctx, integrationPool(t), func(exec db.Executor) error {
		conversationID, _ := conversation(t, ctx, exec)
		repo := NewRepository(exec)

		first, err := repo.Append(ctx, conversationID, time.Second, 10)
		if err != nil {
			t.Fatalf("Append: got %v, want no error", err)
		}
		inbound(t, ctx, exec, conversationID, first.ID)

		// A window twice as long, so what is compared is the expiry and not the clock: the reset has to be visible in the row.
		second, err := repo.Append(ctx, conversationID, 2*time.Second, 10)
		if err != nil {
			t.Fatalf("Append: got %v, want no error", err)
		}

		if second.ID != first.ID || second.TurnNumber != first.TurnNumber {
			t.Fatalf("the second message opened batch %v turn %d, want %v turn %d",
				second.ID, second.TurnNumber, first.ID, first.TurnNumber)
		}
		if !second.ExpiresAt.After(first.ExpiresAt) {
			t.Fatalf("expires_at went from %v to %v: the timer did not start over", first.ExpiresAt, second.ExpiresAt)
		}

		var open int
		if err := exec.QueryRow(ctx,
			`SELECT count(*) FROM message_batches WHERE conversation_id = $1 AND closed_at IS NULL`, conversationID).
			Scan(&open); err != nil {
			t.Fatalf("count the open batches: %v", err)
		}
		if open != 1 {
			t.Fatalf("%d open batches, want 1: a second one splits the turn in two (regla 3)", open)
		}
		return rollback
	})

	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// The message that reaches the cap does not close anything: it leaves the batch due, and the poller picks it up on the next
// tick like any other (ADR-0041, regla 5). One writer of closed_at is what makes the close a fact and not a race.
func TestTheMessageThatReachesTheCapLeavesTheBatchDue(t *testing.T) {
	ctx := context.Background()

	err := db.InTx(ctx, integrationPool(t), func(exec db.Executor) error {
		conversationID, _ := conversation(t, ctx, exec)
		repo := NewRepository(exec)

		// A cap of two: the first message is below it and the second is the one that reaches it.
		batch, err := repo.Append(ctx, conversationID, time.Hour, 2)
		if err != nil {
			t.Fatalf("Append: got %v, want no error", err)
		}
		if !batch.ExpiresAt.After(batch.OpenedAt) {
			t.Fatalf("the first message left the batch due at %v, opened at %v", batch.ExpiresAt, batch.OpenedAt)
		}
		inbound(t, ctx, exec, conversationID, batch.ID)

		capped, err := repo.Append(ctx, conversationID, time.Hour, 2)
		if err != nil {
			t.Fatalf("Append: got %v, want no error", err)
		}
		if capped.ExpiresAt.After(batch.OpenedAt.Add(time.Minute)) {
			t.Fatalf("expires_at = %v, want an instant already past: the cap has to stop the wait", capped.ExpiresAt)
		}

		var closedAt *time.Time
		if err := exec.QueryRow(ctx, `SELECT closed_at FROM message_batches WHERE id = $1`, batch.ID).
			Scan(&closedAt); err != nil {
			t.Fatalf("read back the batch: %v", err)
		}
		if closedAt != nil {
			t.Fatal("the message that reached the cap closed the batch, and only the poller may write closed_at")
		}
		return rollback
	})

	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// A closed batch is never extended: the message that arrives after the poller cut has to start the next turn (regla 2, regla 3).
func TestAMessageAfterTheCloseOpensTheNextTurn(t *testing.T) {
	ctx := context.Background()

	err := db.InTx(ctx, integrationPool(t), func(exec db.Executor) error {
		conversationID, _ := conversation(t, ctx, exec)
		repo := NewRepository(exec)

		first, err := repo.Append(ctx, conversationID, time.Second, 10)
		if err != nil {
			t.Fatalf("Append: got %v, want no error", err)
		}
		if _, err := exec.Exec(ctx,
			`UPDATE message_batches SET closed_at = now(), closed_reason = 'silence' WHERE id = $1`, first.ID); err != nil {
			t.Fatalf("close the batch: %v", err)
		}

		second, err := repo.Append(ctx, conversationID, time.Second, 10)
		if err != nil {
			t.Fatalf("Append: got %v, want no error", err)
		}
		if second.ID == first.ID {
			t.Fatal("the message joined a batch that was already closed, which rewrites a turn that was already answered")
		}
		if second.TurnNumber != first.TurnNumber+1 {
			t.Fatalf("turn numbers %d then %d, want consecutive", first.TurnNumber, second.TurnNumber)
		}
		return rollback
	})

	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// inbound is what conversations/ writes on every customer message, done here with SQL because this case is about the batch:
// what Append counts for the cap is exactly what is already linked.
func inbound(t *testing.T, ctx context.Context, exec db.Executor, conversationID, batchID uuid.UUID) {
	t.Helper()

	if _, err := exec.Exec(ctx,
		`INSERT INTO messages (id, conversation_id, message_batch_id, direction, sender_type, content_text, content_type, sent_at)
		 VALUES ($1, $2, $3, 'inbound', 'contact', 'hola', 'text', now())`,
		id.New(), conversationID, batchID); err != nil {
		t.Fatalf("insert message: %v", err)
	}
}

// A batch with closed_at and no processed_at means one thing only: nobody finished it (07-secuencia section 5).
func TestAFailedBatchKeepsItsReasonAndStaysUnprocessed(t *testing.T) {
	ctx := context.Background()

	err := db.InTx(ctx, integrationPool(t), func(exec db.Executor) error {
		conversationID, _ := conversation(t, ctx, exec)
		repo := NewRepository(exec)

		batch, err := repo.Append(ctx, conversationID, time.Second, 10)
		if err != nil {
			t.Fatalf("Append: got %v, want no error", err)
		}
		if err := repo.MarkFailed(ctx, batch.ID, "brain: 503"); err != nil {
			t.Fatalf("MarkFailed: got %v, want no error", err)
		}

		var processedAt *time.Time
		var reason string
		if err := exec.QueryRow(ctx,
			`SELECT processed_at, error_message FROM message_batches WHERE id = $1`, batch.ID).
			Scan(&processedAt, &reason); err != nil {
			t.Fatalf("read back the batch: %v", err)
		}
		if processedAt != nil {
			t.Fatal("processed_at was written for a turn that failed, which erases the marker of section 5")
		}
		if reason != "brain: 503" {
			t.Fatalf("error_message = %q, want the cause that was recorded", reason)
		}
		return rollback
	})

	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

func TestMarkProcessedClosesTheTurn(t *testing.T) {
	ctx := context.Background()

	err := db.InTx(ctx, integrationPool(t), func(exec db.Executor) error {
		conversationID, _ := conversation(t, ctx, exec)
		repo := NewRepository(exec)
		at := time.Now().UTC()

		batch, err := repo.Append(ctx, conversationID, time.Second, 10)
		if err != nil {
			t.Fatalf("Append: got %v, want no error", err)
		}
		if err := repo.MarkProcessed(ctx, batch.ID, at); err != nil {
			t.Fatalf("MarkProcessed: got %v, want no error", err)
		}

		var processedAt *time.Time
		if err := exec.QueryRow(ctx, `SELECT processed_at FROM message_batches WHERE id = $1`, batch.ID).
			Scan(&processedAt); err != nil {
			t.Fatalf("read back the batch: %v", err)
		}
		if processedAt == nil {
			t.Fatal("processed_at is missing, and it is what closes the turn")
		}

		// A batch that is not there has to say so instead of reporting a silent success on zero rows.
		if err := repo.MarkProcessed(ctx, id.New(), at); !errors.Is(err, ErrBatchNotFound) {
			t.Fatalf("MarkProcessed on a missing batch: got %v, want ErrBatchNotFound", err)
		}
		return rollback
	})

	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// Checked against the real DDL because a stub would pass on a column name that does not exist (ADR-0017 (i).2).
func TestLinkGenerationJoinsTheEvidenceToTheAnswer(t *testing.T) {
	ctx := context.Background()

	err := db.InTx(ctx, integrationPool(t), func(exec db.Executor) error {
		conversationID, _ := conversation(t, ctx, exec)
		repo := NewRepository(exec)

		batch, err := repo.Append(ctx, conversationID, time.Second, 10)
		if err != nil {
			t.Fatalf("Append: got %v, want no error", err)
		}
		generationID := evidence(t, ctx, exec, conversationID, batch.ID)
		messageID := outbound(t, ctx, exec, conversationID)

		if err := repo.LinkGeneration(ctx, generationID, messageID); err != nil {
			t.Fatalf("LinkGeneration: got %v, want no error", err)
		}

		var linked uuid.UUID
		if err := exec.QueryRow(ctx, `SELECT message_id FROM generations WHERE id = $1`, generationID).
			Scan(&linked); err != nil {
			t.Fatalf("read back the generation: %v", err)
		}
		if linked != messageID {
			t.Fatalf("message_id = %s, want %s", linked, messageID)
		}
		return rollback
	})

	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// A generation nobody wrote is not a link to retry: it means the brain named a row that is not there.
func TestLinkGenerationReportsAGenerationThatIsNotThere(t *testing.T) {
	ctx := context.Background()

	err := db.InTx(ctx, integrationPool(t), func(exec db.Executor) error {
		conversationID, _ := conversation(t, ctx, exec)
		messageID := outbound(t, ctx, exec, conversationID)

		if err := NewRepository(exec).LinkGeneration(ctx, id.New(), messageID); !errors.Is(err, ErrGenerationNotFound) {
			t.Fatalf("LinkGeneration: got %v, want ErrGenerationNotFound", err)
		}
		return rollback
	})

	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// Written with SQL and not through the brain, because the brain is the other deployable and this case is about the DDL.
func evidence(t *testing.T, ctx context.Context, exec db.Executor, conversationID, batchID uuid.UUID) uuid.UUID {
	t.Helper()

	generationID := id.New()
	_, err := exec.Exec(ctx,
		`INSERT INTO generations (id, conversation_id, ai_agent_id, message_batch_id, llm_provider, llm_model,
		                          prompt_rendered, completion_text, prompt_tokens, completion_tokens,
		                          latency_ms, cost, currency, status, assembled_at)
		 SELECT $1, c.id, c.ai_agent_id, $2, 'double', 'command-a-03-2025', 'system', 'hola', 10, 5, 120, 0.000075, 'USD', $3, now()
		 FROM   conversations c WHERE c.id = $4`,
		generationID, batchID, enums.GenerationStatusAutoSent, conversationID)
	if err != nil {
		t.Fatalf("insert generation: %v", err)
	}
	return generationID
}

func outbound(t *testing.T, ctx context.Context, exec db.Executor, conversationID uuid.UUID) uuid.UUID {
	t.Helper()

	messageID := id.New()
	_, err := exec.Exec(ctx,
		`INSERT INTO messages (id, conversation_id, direction, sender_type, sender_ai_agent_id, content_text, content_type, status, sent_at)
		 SELECT $1, c.id, 'outbound', 'ai_agent', c.ai_agent_id, 'hola', 'text', 'sent', now()
		 FROM   conversations c WHERE c.id = $2`,
		messageID, conversationID)
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	return messageID
}

// The close is where the timer of F10 actually goes off, and it is one statement: everything the closed row says is decided
// there (ADR-0041, regla 4). Checked against real DDL because the CASE that picks the reason is SQL and not Go.
func TestCloseTakesTheDueBatchesAndLeavesTheOnesStillWaiting(t *testing.T) {
	ctx := context.Background()

	err := db.InTx(ctx, integrationPool(t), func(exec db.Executor) error {
		waiting, _ := conversation(t, ctx, exec)
		expired, _ := conversation(t, ctx, exec)
		repo := NewRepository(exec)

		// An hour of window on one and none on the other, which is the difference between the two the poller has to see.
		stillWaiting, err := repo.Append(ctx, waiting, time.Hour, 10)
		if err != nil {
			t.Fatalf("Append: got %v, want no error", err)
		}
		due, err := repo.Append(ctx, expired, 0, 10)
		if err != nil {
			t.Fatalf("Append: got %v, want no error", err)
		}
		inbound(t, ctx, exec, expired, due.ID)
		inbound(t, ctx, exec, expired, due.ID)

		closed, err := repo.Close(ctx, 10*time.Second, time.Hour, 10, 10)
		if err != nil {
			t.Fatalf("Close: got %v, want no error", err)
		}

		if len(closed) != 1 || closed[0].ID != due.ID {
			t.Fatalf("closed %v, want only the batch whose wait was over (%v)", closed, due.ID)
		}
		if closed[0].MessagesCount != 2 {
			t.Fatalf("messages_count = %d, want 2: it is counted at the close and never kept in a column", closed[0].MessagesCount)
		}
		if closed[0].TimerMsUsed != 10000 {
			t.Fatalf("timer_ms_used = %d, want the window that was applied and not how long the batch lasted", closed[0].TimerMsUsed)
		}
		if closed[0].ClosedReason != enums.BatchCloseReasonSilence {
			t.Fatalf("closed_reason = %q, want silence: the wait ran out with nobody writing", closed[0].ClosedReason)
		}

		var openStillThere bool
		if err := exec.QueryRow(ctx,
			`SELECT closed_at IS NULL FROM message_batches WHERE id = $1`, stillWaiting.ID).Scan(&openStillThere); err != nil {
			t.Fatalf("read back the batch that was waiting: %v", err)
		}
		if !openStillThere {
			t.Fatal("a batch whose wait was not over was closed, which answers a customer still typing")
		}
		return rollback
	})

	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// The three values of BatchCloseReason, in the order that decides them: the cap first, because attributing to silence a wait the
// cap deliberately cut would hide the one case F10 does not describe (ADR-0041, regla 5).
// The two cases that open with an hour-long window are the ones that prove a cap closes: their wait is nowhere near over, so a
// reason coming back at all means something other than silence took the batch.
func TestCloseWritesTheReasonThatStoppedTheWait(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name string
		// The window the batch opens with. An hour means its wait is nowhere near over, so what closes it can only be a cap.
		window   time.Duration
		messages int
		// How long ago the batch was opened, which is what max_duration is measured against.
		age  time.Duration
		want enums.BatchCloseReason
	}{
		{"the wait ran out with nobody writing", 0, 1, 0, enums.BatchCloseReasonSilence},
		{"the customer sent more than the batch takes", 0, 3, 0, enums.BatchCloseReasonMaxMessages},
		{"the customer kept writing past the maximum a batch may last", time.Hour, 1, 2 * time.Hour, enums.BatchCloseReasonMaxDuration},
		{"over both at once, and the cap is the one that must show", time.Hour, 3, 2 * time.Hour, enums.BatchCloseReasonMaxMessages},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := db.InTx(ctx, integrationPool(t), func(exec db.Executor) error {
				conversationID, _ := conversation(t, ctx, exec)
				repo := NewRepository(exec)

				batch, err := repo.Append(ctx, conversationID, tc.window, 100)
				if err != nil {
					t.Fatalf("Append: got %v, want no error", err)
				}
				for range tc.messages {
					inbound(t, ctx, exec, conversationID, batch.ID)
				}
				if tc.age > 0 {
					if _, err := exec.Exec(ctx,
						`UPDATE message_batches SET opened_at = now() - ($2 * interval '1 millisecond') WHERE id = $1`,
						batch.ID, tc.age.Milliseconds()); err != nil {
						t.Fatalf("age the batch: %v", err)
					}
				}

				closed, err := repo.Close(ctx, 10*time.Second, time.Hour, 3, 10)
				if err != nil {
					t.Fatalf("Close: got %v, want no error", err)
				}
				if len(closed) != 1 {
					t.Fatalf("closed %d batches, want 1", len(closed))
				}
				if closed[0].ClosedReason != tc.want {
					t.Fatalf("closed_reason = %q, want %q", closed[0].ClosedReason, tc.want)
				}
				return rollback
			})
			if !errors.Is(err, rollback) {
				t.Fatalf("InTx: got %v, want the rollback sentinel", err)
			}
		})
	}
}

// A batch closed twice is a customer answered twice, which cannot be taken back (ADR-0020). What prevents it is the statement
// itself: closed_at is written where it was NULL, so the second caller finds nothing due.
// The case ADR-0041 regla 5 exists for, written the way it happens: someone who sends another message just before the window
// runs out pushes expires_at forward every time, so silence never arrives. Without max_duration closing on its own, that batch
// stays open forever and the customer is never answered.
func TestCloseCutsTheCustomerWhoNeverStopsWriting(t *testing.T) {
	ctx := context.Background()

	err := db.InTx(ctx, integrationPool(t), func(exec db.Executor) error {
		conversationID, _ := conversation(t, ctx, exec)
		repo := NewRepository(exec)

		// Three messages, each one restarting a ten-second window, so the wait is never over.
		var batch Batch
		for range 3 {
			var err error
			if batch, err = repo.Append(ctx, conversationID, 10*time.Second, 100); err != nil {
				t.Fatalf("Append: got %v, want no error", err)
			}
			inbound(t, ctx, exec, conversationID, batch.ID)
		}
		if !batch.ExpiresAt.After(time.Now()) {
			t.Fatalf("expires_at = %v, want a wait that is still running", batch.ExpiresAt)
		}

		// The conversation has been going on for two hours, which is what the maximum duration is measured against.
		if _, err := exec.Exec(ctx,
			`UPDATE message_batches SET opened_at = now() - interval '2 hours' WHERE id = $1`, batch.ID); err != nil {
			t.Fatalf("age the batch: %v", err)
		}

		closed, err := repo.Close(ctx, 10*time.Second, time.Hour, 100, 10)
		if err != nil {
			t.Fatalf("Close: got %v, want no error", err)
		}
		if len(closed) != 1 {
			t.Fatalf("closed %d batches, want the one that outlived its maximum duration", len(closed))
		}
		if closed[0].ClosedReason != enums.BatchCloseReasonMaxDuration {
			t.Fatalf("closed_reason = %q, want %q", closed[0].ClosedReason, enums.BatchCloseReasonMaxDuration)
		}
		if closed[0].MessagesCount != 3 {
			t.Fatalf("messages_count = %d, want 3", closed[0].MessagesCount)
		}
		return rollback
	})

	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

func TestCloseNeverTakesTheSameBatchTwice(t *testing.T) {
	ctx := context.Background()

	err := db.InTx(ctx, integrationPool(t), func(exec db.Executor) error {
		conversationID, _ := conversation(t, ctx, exec)
		repo := NewRepository(exec)

		batch, err := repo.Append(ctx, conversationID, 0, 10)
		if err != nil {
			t.Fatalf("Append: got %v, want no error", err)
		}

		first, err := repo.Close(ctx, 10*time.Second, time.Hour, 10, 10)
		if err != nil {
			t.Fatalf("Close: got %v, want no error", err)
		}
		second, err := repo.Close(ctx, 10*time.Second, time.Hour, 10, 10)
		if err != nil {
			t.Fatalf("Close: got %v, want no error", err)
		}

		if len(first) != 1 || first[0].ID != batch.ID {
			t.Fatalf("the first tick closed %v, want the batch that was due", first)
		}
		if len(second) != 0 {
			t.Fatalf("the second tick closed %v again, which answers the same customer twice", second)
		}
		return rollback
	})

	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// The limit is not a queue: it only bounds what one tick may open, so a core that was down for an hour drains its backlog over
// several ticks instead of calling the LLM once per batch in the same second.
func TestCloseTakesNoMoreThanTheTickAllows(t *testing.T) {
	ctx := context.Background()

	err := db.InTx(ctx, integrationPool(t), func(exec db.Executor) error {
		repo := NewRepository(exec)
		for range 3 {
			conversationID, _ := conversation(t, ctx, exec)
			if _, err := repo.Append(ctx, conversationID, 0, 10); err != nil {
				t.Fatalf("Append: got %v, want no error", err)
			}
		}

		closed, err := repo.Close(ctx, 10*time.Second, time.Hour, 10, 2)
		if err != nil {
			t.Fatalf("Close: got %v, want no error", err)
		}
		if len(closed) != 2 {
			t.Fatalf("one tick closed %d batches, want at most 2", len(closed))
		}

		rest, err := repo.Close(ctx, 10*time.Second, time.Hour, 10, 2)
		if err != nil {
			t.Fatalf("Close: got %v, want no error", err)
		}
		if len(rest) != 1 {
			t.Fatalf("the next tick closed %d, want the one the first left behind", len(rest))
		}
		return rollback
	})

	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// ADR-0046, section 2.5, against the real DDL: the CASE that tells accepted from edited is SQL, and so is the predicate that refuses
// a draft already used or of another conversation.
func TestReviewGenerationMarksTheDraftByWhatTheOperatorSent(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		sent string
		want enums.GenerationStatus
	}{
		{"the draft left as it was proposed is accepted", "hola", enums.GenerationStatusAccepted},
		{"a draft the Operator changed is edited", "hola, ¿en qué te ayudo?", enums.GenerationStatusEdited},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := db.InTx(ctx, integrationPool(t), func(exec db.Executor) error {
				conversationID, _ := conversation(t, ctx, exec)
				repo := NewRepository(exec)
				batch, err := repo.Append(ctx, conversationID, time.Second, 10)
				if err != nil {
					t.Fatalf("Append: %v", err)
				}
				generationID := draft(t, ctx, exec, conversationID, batch.ID)
				operatorID := operator(t, ctx, exec)
				messageID := outbound(t, ctx, exec, conversationID)

				status, err := repo.ReviewGeneration(ctx, generationID, conversationID, messageID, operatorID, tc.sent)
				if err != nil || status != tc.want {
					t.Fatalf("ReviewGeneration: got %q, %v, want %q", status, err, tc.want)
				}
				var linked bool
				if err := exec.QueryRow(ctx, `SELECT message_id = $2 AND reviewed_by_operator_id = $3 AND reviewed_at IS NOT NULL
					FROM generations WHERE id = $1`, generationID, messageID, operatorID).Scan(&linked); err != nil || !linked {
					t.Fatalf("the draft was not linked and reviewed: %v", err)
				}
				// The same draft a second time is a refusal and not a second review.
				if _, err := repo.ReviewGeneration(ctx, generationID, conversationID, messageID, operatorID, tc.sent); !errors.Is(err, ErrGenerationNotFound) {
					t.Fatalf("a second review: got %v, want ErrGenerationNotFound", err)
				}
				return rollback
			})
			if !errors.Is(err, rollback) {
				t.Fatalf("InTx: got %v, want the rollback sentinel", err)
			}
		})
	}
}

// A draft of another conversation is not this one's to review, whatever id the request carried.
func TestReviewGenerationRefusesADraftOfAnotherConversation(t *testing.T) {
	ctx := context.Background()

	err := db.InTx(ctx, integrationPool(t), func(exec db.Executor) error {
		mine, _ := conversation(t, ctx, exec)
		theirs, _ := conversation(t, ctx, exec)
		repo := NewRepository(exec)
		batch, err := repo.Append(ctx, theirs, time.Second, 10)
		if err != nil {
			t.Fatalf("Append: %v", err)
		}
		generationID := draft(t, ctx, exec, theirs, batch.ID)

		_, err = repo.ReviewGeneration(ctx, generationID, mine, outbound(t, ctx, exec, mine), operator(t, ctx, exec), "hola")
		if !errors.Is(err, ErrGenerationNotFound) {
			t.Fatalf("ReviewGeneration: got %v, want ErrGenerationNotFound", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// A generation of suggestion mode, which is what evidence() writes with the one status that makes it a draft.
func draft(t *testing.T, ctx context.Context, exec db.Executor, conversationID, batchID uuid.UUID) uuid.UUID {
	t.Helper()

	generationID := evidence(t, ctx, exec, conversationID, batchID)
	if _, err := exec.Exec(ctx, `UPDATE generations SET status = $2 WHERE id = $1`, generationID, enums.GenerationStatusPendingReview); err != nil {
		t.Fatalf("make it a draft: %v", err)
	}
	return generationID
}

func operator(t *testing.T, ctx context.Context, exec db.Executor) uuid.UUID {
	t.Helper()

	operatorID := id.New()
	if _, err := exec.Exec(ctx, `
		INSERT INTO operators (id, account_id, name, lastname, email, password_hash, role, active)
		SELECT $1, a.id, 'Revisa', 'Sugerencia', $2, 'x', 'operator', true FROM accounts a LIMIT 1`,
		operatorID, "review-"+operatorID.String()+"@coop.test"); err != nil {
		t.Fatalf("insert operator: %v", err)
	}
	return operatorID
}
