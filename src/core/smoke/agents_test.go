package smoke

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/operators"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// TestTheAdminManagesAnAIAgentThroughTheRealProcess is the one case where the check of ADR-0038 meets the real brain: the package tests
// answer the priced list from a stub, and none of them can fail if the core is not asking the deployed brain, or the brain is not
// reading its own pricing.yaml.
func TestTheAdminManagesAnAIAgentThroughTheRealProcess(t *testing.T) {
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

	// Armed before the first row exists, children first: audit_logs points at the operator, the inbox at the agent, and the agents
	// and the inbox are this case's own, so the demo inbox the other cases answer through is never touched.
	var created []uuid.UUID
	inboxID := id.New()
	t.Cleanup(func() {
		for _, step := range []struct {
			what string
			sql  string
			args []any
		}{
			{"the audit", `DELETE FROM audit_logs WHERE operator_id = $1`, []any{adminID}},
			{"the inbox", `DELETE FROM inboxes WHERE id = $1`, []any{inboxID}},
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

	// A number of this case's own, written by hand because connecting one is --connect-inbox and not an endpoint (ADR-0033).
	if _, err := pool.Exec(ctx, `
		INSERT INTO inboxes (id, account_id, phone_number_id, waba_id, display_name, verify_token, status)
		VALUES ($1, $2, $3, 'smoke-waba', 'Numero del recorrido', $4, 'active')`,
		inboxID, account, "smoke-"+id.New().String(), "smoke-"+id.New().String()); err != nil {
		t.Fatalf("seeding the inbox: %v", err)
	}
	inboxPath := "/api/v1/inboxes/" + inboxID.String()

	t.Run("un agente pausado se asigna a un numero y el numero no muestra secretos", func(t *testing.T) {
		status, body := call(t, core, http.MethodPatch, inboxPath, token, fmt.Sprintf(`{"default_ai_agent_id":%q}`, agentID))
		if status != http.StatusOK || stringField(t, body, "default_ai_agent_id") != agentID {
			t.Fatalf("assigning the paused agent: got %d, want 200 naming it - %s", status, body)
		}
		if _, leaked := keysOf(t, body)["verify_token"]; leaked {
			t.Fatalf("the inbox answered with its verify token: %s", body)
		}
	})

	t.Run("la baja la saca de toda lectura y del numero que la tenia", func(t *testing.T) {
		if status, body := call(t, core, http.MethodDelete, "/api/v1/ai-agents/"+agentID, token, ""); status != http.StatusNoContent {
			t.Fatalf("removing: got %d, want 204 - %s", status, body)
		}
		status, body := call(t, core, http.MethodGet, "/api/v1/ai-agents/"+agentID, token, "")
		if code, _ := refusal(t, body); status != http.StatusNotFound || code != "ai_agent_not_found" {
			t.Fatalf("reading a removed agent: got %d %q, want 404 ai_agent_not_found", status, code)
		}

		status, body = call(t, core, http.MethodGet, "/api/v1/inboxes", token, "")
		if status != http.StatusOK || !listed(t, body, inboxID.String()) {
			t.Fatalf("listing the inboxes: got %d without %v - %s", status, inboxID, body)
		}
		var page struct {
			Items []struct {
				ID               string  `json:"id"`
				DefaultAIAgentID *string `json:"default_ai_agent_id"`
			} `json:"items"`
		}
		_ = json.Unmarshal([]byte(body), &page)
		for _, in := range page.Items {
			if in.ID == inboxID.String() && in.DefaultAIAgentID != nil {
				t.Fatalf("the number still names %s after its agent was removed", *in.DefaultAIAgentID)
			}
		}

		status, body = call(t, core, http.MethodPatch, inboxPath, token, fmt.Sprintf(`{"default_ai_agent_id":%q}`, agentID))
		if code, _ := refusal(t, body); status != http.StatusBadRequest || code != "ai_agent_not_assignable" {
			t.Fatalf("assigning a removed agent: got %d %q, want 400 ai_agent_not_assignable", status, code)
		}
	})
}
