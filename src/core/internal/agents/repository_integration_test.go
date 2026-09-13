package agents

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"

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
		created, err := NewRepository(exec).Create(ctx, Agent{
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
func TestARemovedAgentIsGoneForEveryRead(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor, account uuid.UUID) {
		repo := NewRepository(exec)
		created, err := repo.Create(ctx, Agent{ID: id.New(), AccountID: account, Name: "Agente a borrar",
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

// The removal of ADR-0038 against the real DDL, through the real repositories of the two packages and in one transaction: the agent
// goes away, the inbox that defaulted to it stops naming it, and audit_logs holds a jsonb a query can read.
func TestRemovingAnAgentIsOneTransactionAcrossTheTwoOwners(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor, account uuid.UUID) {
		operator := seededOperator(t, ctx, exec, account)
		agents, inboxes := NewRepository(exec), whatsapp.NewRepository(exec, testCipher(t))

		created, err := agents.Create(ctx, Agent{ID: id.New(), AccountID: account, Name: "Agente de una bandeja",
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

		service := NewService(Options{
			Repo: agents,
			// The case's own transaction plays the one the service would open, so the rollback undoes all of it.
			Tx:        func(_ context.Context, fn func(Store, Inboxes) error) error { return fn(agents, inboxes) },
			Catalogue: priced(),
		})
		if err := service.Delete(ctx, account, operator, created.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}

		var defaultAgent *uuid.UUID
		if err := exec.QueryRow(ctx, `SELECT default_ai_agent_id FROM inboxes WHERE id = $1`, inboxID).Scan(&defaultAgent); err != nil {
			t.Fatalf("read the inbox: %v", err)
		}
		if defaultAgent != nil {
			t.Fatalf("the inbox still defaults to %v after the removal", *defaultAgent)
		}

		var action, name, cleared string
		var auditOperator uuid.UUID
		if err := exec.QueryRow(ctx, `
			SELECT action, operator_id, changes_json->'before'->>'name', changes_json->'cleared_inbox_ids'->>0
			FROM   audit_logs
			WHERE  entity_type = 'ai_agents' AND entity_id = $1`, created.ID).
			Scan(&action, &auditOperator, &name, &cleared); err != nil {
			t.Fatalf("read the audit: %v", err)
		}
		if action != "delete" || auditOperator != operator || name != "Agente de una bandeja" || cleared != inboxID.String() {
			t.Fatalf("audit = %s by %v, before %q, cleared %q; want delete by %v of the agent and %v",
				action, auditOperator, name, cleared, operator, inboxID)
		}
	})
}

// An update through the real repository audits the jsonb diff and leaves the row with the new configuration.
func TestUpdateThroughTheServiceAuditsItsDiff(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor, account uuid.UUID) {
		operator := seededOperator(t, ctx, exec, account)
		repo := NewRepository(exec)
		created, err := repo.Create(ctx, Agent{ID: id.New(), AccountID: account, Name: "Agente a pausar",
			LLMProvider: "cohere", LLMModel: "command-a-03-2025"})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}

		service := NewService(Options{
			Repo:      repo,
			Tx:        func(_ context.Context, fn func(Store, Inboxes) error) error { return fn(repo, nil) },
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
