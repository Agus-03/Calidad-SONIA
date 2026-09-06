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
		contact, account, "549351"+contact.String()[:7])
	return contact, err
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
		first, err := NewService(repo, nil).EnsureOpen(ctx, contact, inbox, &agent, at)
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

		second, err := NewService(repo, nil).EnsureOpen(ctx, contact, inbox, &agent, at)
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
		conversation, err := service.EnsureOpen(ctx, contact, inbox, &agent, at)
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
