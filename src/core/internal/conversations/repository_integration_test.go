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
// the conversation keeps the first one.
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
		conversation, err := service.EnsureOpen(ctx, contact, inbox, &agent, time.Now().UTC())
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
		conversation, err := service.EnsureOpen(ctx, contact, inbox, &agent, opened)
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
		if firstResponse == nil || !firstResponse.Equal(sentAt) {
			t.Fatalf("first_response_at is %v, want the first send, %v", firstResponse, sentAt)
		}

		later, err := service.RecordOperatorOutbound(ctx, conversation.ID, operator, "algo más", sentAt.Add(time.Hour))
		if err != nil {
			t.Fatalf("RecordOperatorOutbound: %v", err)
		}
		if err := service.MarkSent(ctx, later.ID, "wamid.second", sentAt.Add(time.Hour)); err != nil {
			t.Fatalf("MarkSent: %v", err)
		}
		read()
		if !firstResponse.Equal(sentAt) {
			t.Fatalf("a later send moved first_response_at to %v", *firstResponse)
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
