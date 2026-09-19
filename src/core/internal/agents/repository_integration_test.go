package agents

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/conversations"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/encryption"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
	"github.com/mateo-velasquez/sonia/src/core/internal/whatsapp"
)

// rollback ends every case: the assertions run against real DDL and leave the database as they found it.
var rollback = errors.New("agents: rolling back the integration case")

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

// An agent hangs from an account, and accounts holds a single row for the whole project (ADR-0012).
func seededAccount(t *testing.T, pool db.Pool) uuid.UUID {
	t.Helper()

	var account uuid.UUID
	if err := pool.QueryRow(context.Background(), `SELECT id FROM accounts LIMIT 1`).Scan(&account); err != nil {
		t.Skip("there is no account: run core --seed-admin first")
	}
	return account
}

// The operator the audit names is written inside the case's transaction, so it is never one another package's test deletes under it.
func seededOperator(t *testing.T, ctx context.Context, exec db.Executor, account uuid.UUID) uuid.UUID {
	t.Helper()

	operator := id.New()
	if _, err := exec.Exec(ctx, `
		INSERT INTO operators (id, account_id, name, lastname, email, password_hash, role, active)
		VALUES ($1, $2, 'Prueba', 'Agentes', $3, 'not-a-real-hash', 'admin', true)`,
		operator, account, operator.String()+"@sonia.test"); err != nil {
		t.Fatalf("seed an operator: %v", err)
	}
	return operator
}

// seededConversation opens a conversation of the agent with its bot on, the state a pause or a removal has to leave off. Its contact
// and inbox are the case's own, so the rollback takes all three.
func seededConversation(t *testing.T, ctx context.Context, exec db.Executor, account, inboxID, aiAgentID uuid.UUID) uuid.UUID {
	t.Helper()

	contact := id.New()
	if _, err := exec.Exec(ctx, `
		INSERT INTO contacts (id, account_id, phone_whatsapp, name, lastname, type, source_channel, consent_given_at)
		VALUES ($1, $2, $3, 'Prueba', 'Agentes', 'client', 'direct', now())`, contact, account, "549351"+digitsOf(contact.String())); err != nil {
		t.Fatalf("seed a contact: %v", err)
	}
	conversation := id.New()
	if _, err := exec.Exec(ctx, `
		INSERT INTO conversations (id, contact_id, inbox_id, ai_agent_id, status, mode, last_inbound_at, window_expires_at)
		VALUES ($1, $2, $3, $4, 'active', 'autonomous', now(), now() + interval '24 hours')`,
		conversation, contact, inboxID, aiAgentID); err != nil {
		t.Fatalf("seed a conversation: %v", err)
	}
	return conversation
}

// digitsOf keeps every digit of the id: the head of a UUID v7 is its timestamp, and two contacts of one case would share it.
func digitsOf(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			out = append(out, s[i])
		}
	}
	return string(out)
}

func modeOf(t *testing.T, ctx context.Context, exec db.Executor, conversationID uuid.UUID) (string, *uuid.UUID) {
	t.Helper()

	var mode string
	var aiAgentID *uuid.UUID
	if err := exec.QueryRow(ctx, `SELECT mode, ai_agent_id FROM conversations WHERE id = $1`, conversationID).Scan(&mode, &aiAgentID); err != nil {
		t.Fatalf("read the conversation: %v", err)
	}
	return mode, aiAgentID
}

func inTx(t *testing.T, fn func(ctx context.Context, exec db.Executor, account uuid.UUID)) {
	t.Helper()
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		fn(ctx, exec, account)
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// The columns the API hides still have to hold what the rest of the system reads: a mode the conversation aggregate assumes and the
// F31 toggle off (ADR-0038). And the temperature crosses numeric and back without drifting.
func TestCreateWritesTheRowTheTurnAndTheBrainRead(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor, account uuid.UUID) {
		created, err := NewRepository(exec).Create(ctx, AIAgent{
			ID: id.New(), AccountID: account, Name: "Agente de integracion",
			LLMProvider: "cohere", LLMModel: "command-a-03-2025", Temperature: ptr(0.3), MaxTokens: ptr(512),
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if !created.Active || created.CreatedAt.IsZero() || created.Description != nil {
			t.Fatalf("created = %+v, want active, with the database's clock and no description", created)
		}
		if created.Temperature == nil || *created.Temperature != 0.3 || created.MaxTokens == nil || *created.MaxTokens != 512 {
			t.Fatalf("temperature %v and max_tokens %v, want 0.3 and 512 read back", created.Temperature, created.MaxTokens)
		}

		var mode string
		var extraction bool
		if err := exec.QueryRow(ctx, `SELECT default_mode, extraction_enabled FROM ai_agents WHERE id = $1`, created.ID).
			Scan(&mode, &extraction); err != nil {
			t.Fatalf("read the hidden columns: %v", err)
		}
		if mode != "autonomous" || extraction {
			t.Fatalf("default_mode %q and extraction_enabled %v, want autonomous and false", mode, extraction)
		}
	})
}

// A removal is absence for every read of the package, and removing twice says so instead of moving the timestamp (C09).
func TestARemovedAIAgentIsGoneForEveryRead(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor, account uuid.UUID) {
		repo := NewRepository(exec)
		created, err := repo.Create(ctx, AIAgent{ID: id.New(), AccountID: account, Name: "Agente a borrar",
			LLMProvider: "cohere", LLMModel: "command-a-03-2025"})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}

		if err := repo.SoftDelete(ctx, account, created.ID); err != nil {
			t.Fatalf("SoftDelete: %v", err)
		}
		if err := repo.SoftDelete(ctx, account, created.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("SoftDelete twice: got %v, want ErrNotFound", err)
		}
		if _, err := repo.ByID(ctx, account, created.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("ByID: got %v, want ErrNotFound", err)
		}
		if _, err := repo.Update(ctx, account, created.ID, config("Otro")); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Update: got %v, want ErrNotFound", err)
		}
		list, err := repo.List(ctx, account)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		for _, a := range list {
			if a.ID == created.ID {
				t.Fatal("the listing still shows a removed agent")
			}
		}
		if _, err := repo.ByID(ctx, id.New(), created.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("ByID from another account: got %v, want ErrNotFound", err)
		}
	})
}

// The removal of ADR-0038 against the real DDL, through the real repositories of the three packages and in one transaction: the
// agent goes away, the inbox that defaulted to it stops naming it, its conversation is left with the bot off and the agent kept as
// who answered it, and audit_logs holds a jsonb a query can read.
func TestRemovingAnAIAgentIsOneTransactionAcrossTheTwoOwners(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor, account uuid.UUID) {
		operator := seededOperator(t, ctx, exec, account)
		agents, inboxes := NewRepository(exec), whatsapp.NewRepository(exec, testCipher(t))

		created, err := agents.Create(ctx, AIAgent{ID: id.New(), AccountID: account, Name: "Agente de una bandeja",
			LLMProvider: "cohere", LLMModel: "command-a-03-2025"})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		inboxID := id.New()
		if _, err := exec.Exec(ctx, `
			INSERT INTO inboxes (id, account_id, default_ai_agent_id, phone_number_id, waba_id, display_name, verify_token, status)
			VALUES ($1, $2, $3, $4, 'it-waba', 'Numero de integracion', $5, 'active')`,
			inboxID, account, created.ID, "it-"+id.New().String(), "it-"+id.New().String()); err != nil {
			t.Fatalf("seed an inbox: %v", err)
		}
		conversation := seededConversation(t, ctx, exec, account, inboxID, created.ID)

		aggregate := conversations.NewService(conversations.NewRepository(exec), nil)
		service := NewService(Options{
			Repo: agents,
			// The case's own transaction plays the one the service would open, so the rollback undoes all of it.
			Tx: func(_ context.Context, fn func(Store, Inboxes, Conversations) error) error {
				return fn(agents, inboxes, aggregate)
			},
			Catalogue: priced(),
		})
		if err := service.Delete(ctx, account, operator, created.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}

		var defaultAIAgent *uuid.UUID
		if err := exec.QueryRow(ctx, `SELECT default_ai_agent_id FROM inboxes WHERE id = $1`, inboxID).Scan(&defaultAIAgent); err != nil {
			t.Fatalf("read the inbox: %v", err)
		}
		if defaultAIAgent != nil {
			t.Fatalf("the inbox still defaults to %v after the removal", *defaultAIAgent)
		}
		if mode, aiAgentID := modeOf(t, ctx, exec, conversation); mode != "off" || aiAgentID == nil || *aiAgentID != created.ID {
			t.Fatalf("after the removal the conversation is %q with agent %v, want off and the agent kept", mode, aiAgentID)
		}

		var action, name, cleared, turnedOff string
		var auditOperator uuid.UUID
		if err := exec.QueryRow(ctx, `
			SELECT action, operator_id, changes_json->'before'->>'name', changes_json->'cleared_inbox_ids'->>0,
			       changes_json->'turned_off_conversation_ids'->>0
			FROM   audit_logs
			WHERE  entity_type = 'ai_agents' AND entity_id = $1`, created.ID).
			Scan(&action, &auditOperator, &name, &cleared, &turnedOff); err != nil {
			t.Fatalf("read the audit: %v", err)
		}
		if action != "delete" || auditOperator != operator || name != "Agente de una bandeja" || cleared != inboxID.String() ||
			turnedOff != conversation.String() {
			t.Fatalf("audit = %s by %v, before %q, cleared %q, turned off %q; want delete by %v of the agent, %v and %v",
				action, auditOperator, name, cleared, turnedOff, operator, inboxID, conversation)
		}
	})
}

// An update through the real repository audits the jsonb diff and leaves the row with the new configuration. Being a pause, it
// also turns off the bot of the conversation the agent held, keeping the agent on it so the vendor can turn it on after a resume.
func TestUpdateThroughTheServiceAuditsItsDiff(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor, account uuid.UUID) {
		operator := seededOperator(t, ctx, exec, account)
		repo := NewRepository(exec)
		created, err := repo.Create(ctx, AIAgent{ID: id.New(), AccountID: account, Name: "Agente a pausar",
			LLMProvider: "cohere", LLMModel: "command-a-03-2025"})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		inboxID := id.New()
		if _, err := exec.Exec(ctx, `
			INSERT INTO inboxes (id, account_id, default_ai_agent_id, phone_number_id, waba_id, display_name, verify_token, status)
			VALUES ($1, $2, $3, $4, 'it-waba', 'Numero de integracion', $5, 'active')`,
			inboxID, account, created.ID, "it-"+id.New().String(), "it-"+id.New().String()); err != nil {
			t.Fatalf("seed an inbox: %v", err)
		}
		conversation := seededConversation(t, ctx, exec, account, inboxID, created.ID)

		aggregate := conversations.NewService(conversations.NewRepository(exec), nil)
		service := NewService(Options{
			Repo: repo,
			Tx: func(_ context.Context, fn func(Store, Inboxes, Conversations) error) error {
				return fn(repo, nil, aggregate)
			},
			Catalogue: priced(),
		})
		paused, err := service.Update(ctx, account, operator, created.ID, func(c *Config) {
			c.Active = false
			c.LLMProvider, c.LLMModel = "anthropic", "claude-sonnet-4-5"
		})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if paused.Active || paused.LLMProvider != "anthropic" {
			t.Fatalf("paused = %+v, want inactive on anthropic", paused)
		}

		var before, after bool
		var model string
		if err := exec.QueryRow(ctx, `
			SELECT (changes_json->'before'->>'active')::boolean, (changes_json->'after'->>'active')::boolean,
			       changes_json->'after'->>'llm_model'
			FROM   audit_logs
			WHERE  entity_id = $1 AND action = 'update'`, created.ID).Scan(&before, &after, &model); err != nil {
			t.Fatalf("read the audit: %v", err)
		}
		if !before || after || model != "claude-sonnet-4-5" {
			t.Fatalf("audit before %v after %v model %q, want active before, paused after on the new model", before, after, model)
		}
		if mode, aiAgentID := modeOf(t, ctx, exec, conversation); mode != "off" || aiAgentID == nil || *aiAgentID != created.ID {
			t.Fatalf("after the pause the conversation is %q with agent %v, want off and the agent kept", mode, aiAgentID)
		}

		// The resume turns nothing back on: which conversations the bot answers again is the vendor's to decide (ADR-0038).
		if _, err := service.Update(ctx, account, operator, created.ID, func(c *Config) { c.Active = true }); err != nil {
			t.Fatalf("resume: %v", err)
		}
		if mode, _ := modeOf(t, ctx, exec, conversation); mode != "off" {
			t.Fatalf("after the resume the conversation is %q, want it still off", mode)
		}
	})
}

func testCipher(t *testing.T) *encryption.Cipher {
	t.Helper()

	key := make([]byte, encryption.KeyBytes)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand: %v", err)
	}
	cipher, err := encryption.New(key)
	if err != nil {
		t.Fatalf("encryption.New: %v", err)
	}
	return cipher
}
