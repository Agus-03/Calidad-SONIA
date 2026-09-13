package smoke

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/operators"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// TestTheAdminManagesAnAgentThroughTheRealProcess is the one case where the check of ADR-0038 meets the real brain: the package tests
// answer the priced list from a stub, and none of them can fail if the core is not asking the deployed brain, or the brain is not
// reading its own pricing.yaml.
func TestTheAdminManagesAnAgentThroughTheRealProcess(t *testing.T) {
	core, _, pool := stack(t)
	ctx := context.Background()
	waitForHealth(t, core)

	var account uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts LIMIT 1`).Scan(&account); err != nil {
		t.Skip("there is no account: run core --seed-admin first")
	}

	hash, err := operators.Hash(walkerPassword)
	if err != nil {
		t.Fatalf("hashing the password: %v", err)
	}
	adminID := id.New()
	email := "agents-" + adminID.String() + "@coop.test"

	// Armed before the first row exists, children first: audit_logs points at the operator, and the agents are this case's own.
	var created []uuid.UUID
	t.Cleanup(func() {
		for _, step := range []struct {
			what string
			sql  string
			args []any
		}{
			{"the audit", `DELETE FROM audit_logs WHERE operator_id = $1`, []any{adminID}},
			{"the agents", `DELETE FROM ai_agents WHERE id = ANY($1)`, []any{created}},
			{"the admin", `DELETE FROM operators WHERE id = $1`, []any{adminID}},
		} {
			if _, err := pool.Exec(context.Background(), step.sql, step.args...); err != nil {
				t.Errorf("cleaning up %s: %v", step.what, err)
			}
		}
	})
	if _, err := pool.Exec(ctx, `
		INSERT INTO operators (id, account_id, name, lastname, email, password_hash, role, active)
		VALUES ($1, $2, 'Recorrido', 'Agentes', $3, $4, 'admin', true)`, adminID, account, email, hash); err != nil {
		t.Fatalf("seeding the admin: %v", err)
	}
	token := login(t, core, email, walkerPassword)

	// The model of the demo agent, which the brain's pricing.yaml declares: the only way this answers 201 is the real list.
	status, body := call(t, core, http.MethodPost, "/api/v1/ai-agents", token,
		`{"name":"Agente del recorrido","llm_provider":"cohere","llm_model":"command-a-03-2025","temperature":0.3}`)
	if status != http.StatusCreated {
		t.Fatalf("creating a priced agent: got %d, want 201 - %s", status, body)
	}
	agentID := stringField(t, body, "id")
	created = append(created, uuid.MustParse(agentID))

	t.Run("un modelo sin tarifa se rechaza al guardar", func(t *testing.T) {
		status, body := call(t, core, http.MethodPost, "/api/v1/ai-agents", token,
			`{"name":"Agente sin tarifa","llm_provider":"cohere","llm_model":"un-modelo-que-no-existe"}`)
		if code, _ := refusal(t, body); status != http.StatusBadRequest || code != "unpriced_model" {
			t.Fatalf("an unpriced model: got %d %q, want 400 unpriced_model - %s", status, code, body)
		}
	})

	t.Run("la pausa conserva la configuracion y queda auditada", func(t *testing.T) {
		status, body := call(t, core, http.MethodPatch, "/api/v1/ai-agents/"+agentID, token, `{"active":false}`)
		if status != http.StatusOK {
			t.Fatalf("pausing: got %d, want 200 - %s", status, body)
		}
		if keys := keysOf(t, body); string(keys["active"]) != "false" || string(keys["temperature"]) != "0.3" {
			t.Fatalf("the paused agent = %s, want inactive with its temperature kept", body)
		}

		var actions []string
		rows, err := pool.Query(ctx, `SELECT action FROM audit_logs WHERE entity_id = $1 ORDER BY id`, created[0])
		if err != nil {
			t.Fatalf("reading the audit: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var action string
			if err := rows.Scan(&action); err != nil {
				t.Fatalf("scanning the audit: %v", err)
			}
			actions = append(actions, action)
		}
		if fmt.Sprint(actions) != "[create update]" {
			t.Fatalf("audit of the agent = %v, want [create update]", actions)
		}
	})

	t.Run("la baja la saca de toda lectura", func(t *testing.T) {
		if status, body := call(t, core, http.MethodDelete, "/api/v1/ai-agents/"+agentID, token, ""); status != http.StatusNoContent {
			t.Fatalf("removing: got %d, want 204 - %s", status, body)
		}
		status, body := call(t, core, http.MethodGet, "/api/v1/ai-agents/"+agentID, token, "")
		if code, _ := refusal(t, body); status != http.StatusNotFound || code != "ai_agent_not_found" {
			t.Fatalf("reading a removed agent: got %d %q, want 404 ai_agent_not_found", status, code)
		}
	})
}
