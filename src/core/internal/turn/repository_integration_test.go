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
	phone = "549351" + contact.String()[:7]
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

// ADR-0022 makes message_batches the source of truth of the turn number, and the sequence has to hold against real DDL.
func TestTheTurnNumberAdvancesWithEachBatch(t *testing.T) {
	ctx := context.Background()

	err := db.InTx(ctx, integrationPool(t), func(exec db.Executor) error {
		conversationID, _ := conversation(t, ctx, exec)
		repo := NewRepository(exec)
		now := time.Now().UTC()

		first, err := repo.OpenBatch(ctx, conversationID, now)
		if err != nil {
			t.Fatalf("OpenBatch: got %v, want no error", err)
		}
		second, err := repo.OpenBatch(ctx, conversationID, now)
		if err != nil {
			t.Fatalf("OpenBatch: got %v, want no error", err)
		}

		if first.TurnNumber != 1 || second.TurnNumber != 2 {
			t.Fatalf("turn numbers %d and %d, want 1 and 2", first.TurnNumber, second.TurnNumber)
		}

		// Opened and closed at once because Fase 1 has no timer, and closed_reason describes a timer that does not exist yet.
		var closedAt *time.Time
		var reason *string
		var timerMS int
		if err := exec.QueryRow(ctx,
			`SELECT closed_at, closed_reason, timer_ms_used FROM message_batches WHERE id = $1`, first.ID).
			Scan(&closedAt, &reason, &timerMS); err != nil {
			t.Fatalf("read back the batch: %v", err)
		}
		if closedAt == nil || reason != nil || timerMS != 0 {
			t.Fatalf("closed_at %v, closed_reason %v, timer_ms_used %d", closedAt, reason, timerMS)
		}
		return rollback
	})

	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// A batch with closed_at and no processed_at means one thing only: nobody finished it (07-secuencia section 5).
func TestAFailedBatchKeepsItsReasonAndStaysUnprocessed(t *testing.T) {
	ctx := context.Background()

	err := db.InTx(ctx, integrationPool(t), func(exec db.Executor) error {
		conversationID, _ := conversation(t, ctx, exec)
		repo := NewRepository(exec)

		batch, err := repo.OpenBatch(ctx, conversationID, time.Now().UTC())
		if err != nil {
			t.Fatalf("OpenBatch: got %v, want no error", err)
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

		batch, err := repo.OpenBatch(ctx, conversationID, at)
		if err != nil {
			t.Fatalf("OpenBatch: got %v, want no error", err)
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

		batch, err := repo.OpenBatch(ctx, conversationID, time.Now().UTC())
		if err != nil {
			t.Fatalf("OpenBatch: got %v, want no error", err)
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
		                          latency_ms, cost, currency, status)
		 SELECT $1, c.id, c.ai_agent_id, $2, 'double', 'command-a-03-2025', 'system', 'hola', 10, 5, 120, 0.000075, 'USD', $3
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
