package sales

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

var rollback = errors.New("sales: rolling back the integration case")

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

// TestSalesAgainstRealDDL is ADR-0059 against real DDL: the amount comes back as written, the channel from the contact, the rest kept.
func TestSalesAgainstRealDDL(t *testing.T) {
	ctx := context.Background()
	err := db.InTx(ctx, integrationPool(t), func(exec db.Executor) error {
		var account, inbox, agent uuid.UUID
		if err := exec.QueryRow(ctx, `SELECT a.id, i.id, g.id FROM accounts a, inboxes i, ai_agents g LIMIT 1`).Scan(&account, &inbox, &agent); err != nil {
			t.Skip("the demo rows are missing: load src/core/seeds/0001_demo.sql first")
		}
		vendor, contact, conversation := id.New(), id.New(), id.New()
		for _, step := range []struct {
			what  string
			query string
			args  []any
		}{
			{"an operator", `INSERT INTO operators (id, account_id, name, lastname, email, password_hash, role, active)
				VALUES ($1, $2, 'Prueba', 'Venta', $3, 'not-a-real-hash', 'operator', true)`, []any{vendor, account, vendor.String() + "@sonia.test"}},
			{"a contact", `INSERT INTO contacts (id, account_id, phone_whatsapp, name, lastname, type, source_channel)
				VALUES ($1, $2, $3, 'Prueba', 'Venta', 'prospect', 'qr')`, []any{contact, account, "54" + contact.String()}},
			{"a conversation", `INSERT INTO conversations (id, contact_id, inbox_id, ai_agent_id, operator_id, status, mode, last_inbound_at, window_expires_at)
				VALUES ($1, $2, $3, $4, $5, 'active', 'off', now(), now() + interval '24 hours')`, []any{conversation, contact, inbox, agent, vendor}},
		} {
			if _, err := exec.Exec(ctx, step.query, step.args...); err != nil {
				t.Fatalf("seed %s: %v", step.what, err)
			}
		}
		repo := NewRepository(exec)

		c, err := repo.Conversation(ctx, account, conversation)
		if err != nil || c.OperatorID == nil || *c.OperatorID != vendor || c.AIAgentID == nil || *c.AIAgentID != agent {
			t.Fatalf("Conversation: got %+v and %v, want its vendor and its agent", c, err)
		}
		if _, err := repo.Conversation(ctx, id.New(), conversation); !errors.Is(err, ErrConversationNotFound) {
			t.Fatalf("a conversation of another account: got %v, want ErrConversationNotFound", err)
		}

		sale := Sale{ID: id.New(), ConversationID: conversation, OperatorID: vendor, AIAgentID: &agent, Amount: "1234.50", Currency: "ARS"}
		if err := repo.Create(ctx, sale); err != nil {
			t.Fatalf("Create: %v", err)
		}
		got, err := repo.ByID(ctx, account, sale.ID)
		if err != nil || got.Amount != "1234.50" || got.SourceChannel != enums.SourceChannelQr || got.AIAgentID == nil || *got.AIAgentID != agent {
			t.Fatalf("ByID: got %+v and %v, want 1234.50 from the QR channel with its agent", got, err)
		}
		if err := repo.Update(ctx, sale.ID, "99.9", "USD"); err != nil {
			t.Fatalf("Update: %v", err)
		}
		if got, _ := repo.ByID(ctx, account, sale.ID); got.Amount != "99.90" || got.Currency != "USD" {
			t.Fatalf("after the correction: %s %s, want 99.90 USD as numeric(14,2) keeps it", got.Amount, got.Currency)
		}
		if _, err := repo.ByID(ctx, id.New(), sale.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("a sale read from another account: got %v, want ErrNotFound", err)
		}

		if err := repo.Delete(ctx, sale.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if listed, err := repo.OfConversation(ctx, account, conversation); err != nil || len(listed) != 0 {
			t.Fatalf("OfConversation after the delete: got %v and %v, want none", listed, err)
		}
		var kept int
		if err := exec.QueryRow(ctx, `SELECT count(*) FROM sales WHERE id = $1`, sale.ID).Scan(&kept); err != nil || kept != 1 {
			t.Fatalf("the deleted sale's row: %d and %v, want it kept (C09)", kept, err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}
