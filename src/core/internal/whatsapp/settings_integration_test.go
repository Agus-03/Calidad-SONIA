package whatsapp

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/agents"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// TestTheAdminNamesTheAIAgentOfANumberAgainstRealDDL runs ADR-0038 regla 5 through the real repositories of the two owners in one
// transaction: a paused agent is assignable, a removed one is not, null leaves the number without AI, and every change leaves its
// audit in the jsonb a query can read.
func TestTheAdminNamesTheAIAgentOfANumberAgainstRealDDL(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		operator := id.New()
		if _, err := exec.Exec(ctx, `
			INSERT INTO operators (id, account_id, name, lastname, email, password_hash, role, active)
			VALUES ($1, $2, 'Prueba', 'Bandejas', $3, 'not-a-real-hash', 'admin', true)`,
			operator, account, operator.String()+"@sonia.test"); err != nil {
			t.Fatalf("seed an operator: %v", err)
		}
		agent := func(state string) uuid.UUID {
			agentID := id.New()
			if _, err := exec.Exec(ctx, `
				INSERT INTO ai_agents (id, account_id, name, default_mode, llm_provider, llm_model)
				VALUES ($1, $2, 'Agente de integracion', 'autonomous', 'cohere', 'command-a-03-2025')`, agentID, account); err != nil {
				t.Fatalf("seed an agent: %v", err)
			}
			if state != "" {
				if _, err := exec.Exec(ctx, `UPDATE ai_agents SET `+state+` WHERE id = $1`, agentID); err != nil {
					t.Fatalf("set the agent %s: %v", state, err)
				}
			}
			return agentID
		}
		active, paused, removed := agent(""), agent("active = false"), agent("deleted_at = now()")
		seeded := insertInbox(ctx, t, exec, account, nil, nil)

		repo := NewRepository(exec, testCipher(t))
		settings := NewSettings(SettingsOptions{
			Repo: repo,
			// The case's own transaction plays the one deps.go opens, so the rollback undoes all of it.
			Tx: func(_ context.Context, fn func(SettingsStore, Auditor) error) error {
				return fn(repo, agents.NewRepository(exec))
			},
		})
		set := func(aiAgentID *uuid.UUID) (InboxSettings, error) {
			return settings.SetDefaultAIAgent(ctx, account, operator, seeded.id, aiAgentID)
		}

		if got, err := set(&paused); err != nil || got.DefaultAIAgentID == nil || *got.DefaultAIAgentID != paused {
			t.Fatalf("a paused agent: got %+v, %v, want it assigned, since a pause is not a removal", got, err)
		}
		if _, err := set(&removed); !errors.Is(err, ErrAIAgentNotAssignable) {
			t.Fatalf("a removed agent: got %v, want ErrAIAgentNotAssignable", err)
		}
		if _, err := set(&active); err != nil {
			t.Fatalf("an active agent: %v", err)
		}
		if got, err := set(nil); err != nil || got.DefaultAIAgentID != nil {
			t.Fatalf("null: got %+v, %v, want the number without AI", got, err)
		}

		var changes int
		var firstBefore, lastBefore *string
		if err := exec.QueryRow(ctx, `
			SELECT count(*),
			       (array_agg(changes_json->'before'->>'default_ai_agent_id' ORDER BY id))[1],
			       (array_agg(changes_json->'before'->>'default_ai_agent_id' ORDER BY id DESC))[1]
			FROM   audit_logs
			WHERE  entity_type = 'inboxes' AND entity_id = $1 AND operator_id = $2 AND action = 'update'`,
			seeded.id, operator).Scan(&changes, &firstBefore, &lastBefore); err != nil {
			t.Fatalf("read the audit: %v", err)
		}
		// Three changes and not four: the removed agent was refused before anything was written.
		if changes != 3 || firstBefore != nil || lastBefore == nil || *lastBefore != active.String() {
			t.Fatalf("audit = %d rows, first before %v, last before %v; want 3, NULL and %v", changes, firstBefore, lastBefore, active)
		}

		listed, err := settings.List(ctx, account)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		found := false
		for _, in := range listed {
			found = found || in.ID == seeded.id
		}
		if !found {
			t.Fatalf("the listing of the account does not carry %v", seeded.id)
		}

		// The read the webhook makes before a conversation is born, of the inbox's default as it is now: an active agent is named and
		// answers, a paused one is named and born off, and a removed one, no default at all or another account names none (ADR-0038,
		// reglas 3 y 15).
		for _, tc := range []struct {
			name          string
			account       uuid.UUID
			aiAgentID     *uuid.UUID
			wantNamed     bool
			wantCanAnswer bool
		}{
			{"active", account, &active, true, true},
			{"paused", account, &paused, true, false},
			{"removed", account, &removed, false, false},
			{"no default", account, nil, false, false},
			{"of another account", id.New(), &active, false, false},
		} {
			if _, err := repo.SetDefaultAIAgent(ctx, account, seeded.id, tc.aiAgentID); err != nil {
				t.Fatalf("setting the default for the %s case: %v", tc.name, err)
			}
			named, canAnswer, err := repo.AIAgentForBirth(ctx, tc.account, seeded.id)
			if err != nil {
				t.Fatalf("AIAgentForBirth with the %s agent: %v", tc.name, err)
			}
			if (named != nil) != tc.wantNamed || canAnswer != tc.wantCanAnswer ||
				(tc.wantNamed && *named != *tc.aiAgentID) {
				t.Fatalf("the %s agent: named %v and able to answer %v, want %v and %v", tc.name, named, canAnswer, tc.wantNamed, tc.wantCanAnswer)
			}
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// CAMBIADO POR TOBI
func TestCreateInboxSettingsIntegration(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec, testCipher(t))
		settings := NewSettings(SettingsOptions{Repo: repo})

		inbox, err := settings.Create(ctx, account, "Test Create", "999", "888", "my-access-token", "my-verify-token")
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		if inbox.DisplayName != "Test Create" {
			t.Fatalf("got %q, want Test Create", inbox.DisplayName)
		}

		// Ensure it appears in List
		listed, err := settings.List(ctx, account)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		found := false
		for _, in := range listed {
			if in.ID == inbox.ID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("Created inbox not found in list")
		}

		return errors.New("rollback")
	})

	if err != nil && err.Error() != "rollback" {
		t.Fatalf("transaction failed: %v", err)
	}
}
