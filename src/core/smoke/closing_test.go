package smoke

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/operators"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// The OE1 query of the design, read from its file and not copied: if the query stops working, the closing of the phase fails too.
const oe1File = "../../../docs/design/05-consultas-de-metricas.sql"

// assembledBlock is one row of generation_components, named by what it points at.
type assembledBlock struct {
	kind    string
	version int
}

// TestAnAIAgentTheAdminConfiguresAnswersWithItsOwnPrompt is the closing test of Fase 6. It walks the whole phase through the real
// process: the admin writes an AI agent and its components, previews them, gives it a number, and a customer writing to that number
// is answered with those components. Each generation names the versions it assembled, an edit shows up in the next turn, a component
// switched off leaves the prompt, and OE1, measured by the query of the design, is 100 % (F01, F03, F17, OE1, ADR-0039, ADR-0040).
func TestAnAIAgentTheAdminConfiguresAnswersWithItsOwnPrompt(t *testing.T) {
	core, brain, pool := stack(t)
	ctx := context.Background()
	waitForHealth(t, core)
	waitForHealth(t, brain)

	var account, inboxID uuid.UUID
	var previousDefault *uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts LIMIT 1`).Scan(&account); err != nil {
		t.Skip("there is no account: run core --seed-admin first")
	}
	if err := pool.QueryRow(ctx,
		`SELECT id, default_ai_agent_id FROM inboxes WHERE phone_number_id = $1 AND deleted_at IS NULL`,
		seededPhoneNumberID).Scan(&inboxID, &previousDefault); err != nil {
		t.Skip("no demo inbox: load src/core/seeds/0001_demo.sql first")
	}

	hash, err := operators.Hash(walkerPassword)
	if err != nil {
		t.Fatalf("hashing the password: %v", err)
	}
	adminID := id.New()
	// A number nobody wrote from, so the webhook opens a conversation of its own, born with the default of the inbox (ADR-0038).
	customer := "5493519" + onlyDigits(adminID.String())[:6]
	var agent uuid.UUID

	// Armed before the first row exists, children first. The demo inbox gets its agent back, so the other cases answer as before.
	t.Cleanup(func() {
		conversations := `SELECT c.id FROM conversations c JOIN contacts ct ON ct.id = c.contact_id WHERE ct.phone_whatsapp = $1`
		for _, step := range []struct {
			what string
			sql  string
			args []any
		}{
			{"the inbox default", `UPDATE inboxes SET default_ai_agent_id = $2 WHERE id = $1`, []any{inboxID, previousDefault}},
			{"the assembled components", `DELETE FROM generation_components WHERE generation_id IN
				(SELECT id FROM generations WHERE conversation_id IN (` + conversations + `))`, []any{customer}},
			{"the generations", `DELETE FROM generations WHERE conversation_id IN (` + conversations + `)`, []any{customer}},
			{"the messages", `DELETE FROM messages WHERE conversation_id IN (` + conversations + `)`, []any{customer}},
			// Before the batches they hang from: F11 gave message_batches a second child and the pipeline of ADR-0016 gave it a
			// third, and children go first (ADR-0041, regla 12). The trace is written by every turn and not only by a cut one, so
			// leaving it out made this cleanup fail on the batches the moment the pipeline started writing.
			{"the transfers", `DELETE FROM transfer_events WHERE conversation_id IN (` + conversations + `)`, []any{customer}},
			{"the signal evaluations", `DELETE FROM signal_evaluations WHERE message_batch_id IN
				(SELECT id FROM message_batches WHERE conversation_id IN (` + conversations + `))`, []any{customer}},
			{"the pipeline steps", `DELETE FROM pipeline_steps WHERE message_batch_id IN
				(SELECT id FROM message_batches WHERE conversation_id IN (` + conversations + `))`, []any{customer}},
			{"the detected intents", `DELETE FROM detected_intents WHERE message_batch_id IN
				(SELECT id FROM message_batches WHERE conversation_id IN (` + conversations + `))`, []any{customer}},
			{"the batches", `DELETE FROM message_batches WHERE conversation_id IN (` + conversations + `)`, []any{customer}},
			{"the assignments", `DELETE FROM conversation_assignments WHERE conversation_id IN (` + conversations + `)`, []any{customer}},
			{"the conversations", `DELETE FROM conversations WHERE id IN (` + conversations + `)`, []any{customer}},
			{"the contact", `DELETE FROM contacts WHERE phone_whatsapp = $1`, []any{customer}},
			{"the audit", `DELETE FROM audit_logs WHERE operator_id = $1`, []any{adminID}},
			{"the versions", `DELETE FROM prompt_component_versions WHERE prompt_component_id IN
				(SELECT id FROM prompt_components WHERE ai_agent_id = $1)`, []any{agent}},
			{"the components", `DELETE FROM prompt_components WHERE ai_agent_id = $1`, []any{agent}},
			{"the agent", `DELETE FROM ai_agents WHERE id = $1`, []any{agent}},
			{"the admin", `DELETE FROM operators WHERE id = $1`, []any{adminID}},
		} {
			if _, err := pool.Exec(context.Background(), step.sql, step.args...); err != nil {
				t.Errorf("cleaning up %s: %v", step.what, err)
			}
		}
	})

	if _, err := pool.Exec(ctx, `
		INSERT INTO operators (id, account_id, name, lastname, email, password_hash, role, active)
		VALUES ($1, $2, 'Cierre', 'Fase 6', $3, $4, 'admin', true)`,
		adminID, account, "closing-"+adminID.String()+"@coop.test", hash); err != nil {
		t.Fatalf("seeding the admin: %v", err)
	}
	token := login(t, core, "closing-"+adminID.String()+"@coop.test", walkerPassword)

	// F17: the admin writes the agent. Its model is one the brain prices, the only way this answers 201.
	status, body := call(t, core, http.MethodPost, "/api/v1/ai-agents", token,
		`{"name":"Agente del cierre","llm_provider":"cohere","llm_model":"command-a-03-2025","temperature":0.3}`)
	if status != http.StatusCreated {
		t.Fatalf("creating the agent: got %d - %s", status, body)
	}
	agent = uuid.MustParse(stringField(t, body, "id"))
	base := "/api/v1/ai-agents/" + agent.String()

	// F01: three of the six, each with a text no other case writes, at the positions the panel sends.
	run := adminID.String()[:8]
	objective, tone, toneEdited, rules := "Objetivo del cierre "+run+".", "Tono del cierre "+run+".",
		"Tono editado del cierre "+run+".", "Reglas del cierre "+run+"."
	componentIDs := map[string]string{}
	for _, c := range []struct {
		kind, content string
		position      int
	}{{"objective", objective, 1}, {"tone", tone, 3}, {"rules", rules, 6}} {
		status, body := call(t, core, http.MethodPost, base+"/components", token,
			fmt.Sprintf(`{"type":%q,"content":%q,"position":%d}`, c.kind, c.content, c.position))
		if status != http.StatusCreated {
			t.Fatalf("creating the %s: got %d - %s", c.kind, status, body)
		}
		componentIDs[c.kind] = stringField(t, body, "id")
	}

	// F03: what the admin sees before anything reaches a customer.
	status, body = call(t, core, http.MethodPost, base+"/preview", token, fmt.Sprintf(
		`{"components":[{"type":"objective","content":%q,"position":1},{"type":"tone","content":%q,"position":3},`+
			`{"type":"rules","content":%q,"position":6}]}`, objective, tone, rules))
	if status != http.StatusOK {
		t.Fatalf("the preview: got %d - %s", status, body)
	}
	var preview struct {
		System string `json:"system"`
	}
	if err := json.Unmarshal([]byte(body), &preview); err != nil || preview.System == "" {
		t.Fatalf("decoding the preview: %v - %s", err, body)
	}

	// The number answers with this agent from now on (ADR-0038).
	if status, body := call(t, core, http.MethodPatch, "/api/v1/inboxes/"+inboxID.String(), token,
		fmt.Sprintf(`{"default_ai_agent_id":%q}`, agent)); status != http.StatusOK {
		t.Fatalf("giving the number to the agent: got %d - %s", status, body)
	}
	secret := signingSecret(ctx, t, pool)

	var generations []uuid.UUID
	var first, last time.Time
	turn := func(t *testing.T) (uuid.UUID, string, []assembledBlock) {
		t.Helper()
		wamid := "wamid.smoke.closing." + uuid.New().String()
		if status := postWebhookFrom(t, core, wamid, secret, customer); status != http.StatusOK {
			t.Fatalf("POST /webhooks/whatsapp: got %d, want 200", status)
		}
		awaitTurn(ctx, t, pool, wamid)
		generation, answeredBy, prompt, at := generationOf(ctx, t, pool, wamid)
		if answeredBy != agent {
			t.Fatalf("the generation names the agent %v, want the one the admin wrote, %v", answeredBy, agent)
		}
		generations = append(generations, generation)
		if first.IsZero() {
			first = at
		}
		last = at
		return generation, prompt, assembledOf(ctx, t, pool, generation)
	}

	t.Run("el turno responde con el prompt que el admin previsualizo", func(t *testing.T) {
		_, prompt, blocks := turn(t)
		// prompt_rendered is the system followed by the envelope: the system has to be the preview, character by character.
		if !strings.HasPrefix(prompt, preview.System+"\n\n") {
			t.Fatalf("the prompt the model got does not start with the preview:\npreview: %q\nprompt:  %q", preview.System, prompt)
		}
		want := []assembledBlock{{"objective", 1}, {"tone", 1}, {"rules", 1}}
		if fmt.Sprint(blocks) != fmt.Sprint(want) {
			t.Fatalf("generation_components = %v, want %v", blocks, want)
		}
	})

	t.Run("una edicion aparece en el turno siguiente con su version", func(t *testing.T) {
		if status, body := call(t, core, http.MethodPatch, base+"/components/"+componentIDs["tone"], token,
			fmt.Sprintf(`{"content":%q}`, toneEdited)); status != http.StatusOK {
			t.Fatalf("editing the tone: got %d - %s", status, body)
		}
		_, prompt, blocks := turn(t)
		if !strings.Contains(prompt, toneEdited) || strings.Contains(prompt, tone) {
			t.Fatalf("the prompt does not carry the edited tone alone: %q", prompt)
		}
		want := []assembledBlock{{"objective", 1}, {"tone", 2}, {"rules", 1}}
		if fmt.Sprint(blocks) != fmt.Sprint(want) {
			t.Fatalf("generation_components = %v, want %v", blocks, want)
		}
	})

	t.Run("un componente apagado sale del prompt", func(t *testing.T) {
		if status, body := call(t, core, http.MethodPatch, base+"/components/"+componentIDs["rules"], token,
			`{"active":false}`); status != http.StatusOK {
			t.Fatalf("switching the rules off: got %d - %s", status, body)
		}
		_, prompt, blocks := turn(t)
		if strings.Contains(prompt, rules) {
			t.Fatalf("the rules are still in the prompt after being switched off: %q", prompt)
		}
		want := []assembledBlock{{"objective", 1}, {"tone", 2}}
		if fmt.Sprint(blocks) != fmt.Sprint(want) {
			t.Fatalf("generation_components = %v, want %v", blocks, want)
		}
	})

	t.Run("OE1 da 100 por ciento sobre los turnos del cierre", func(t *testing.T) {
		if len(generations) != 3 {
			t.Skipf("only %d of the three turns ran: the cases above say why", len(generations))
		}
		// The window holds these three and nothing else: the smoke cases run one after the other, never in parallel.
		total, complete, pct := oe1(ctx, t, pool, first, last.Add(time.Microsecond))
		if total != 3 || complete != 3 || pct != "100.00" {
			t.Fatalf("OE1 over the closing turns: %d generations, %d complete, %s %%; want 3, 3 and 100.00", total, complete, pct)
		}
	})
}

// generationOf reads the answer to one inbound message: which agent answered, with what prompt and when.
func generationOf(ctx context.Context, t *testing.T, pool db.Pool, wamid string) (uuid.UUID, uuid.UUID, string, time.Time) {
	t.Helper()
	var generation, agent uuid.UUID
	var prompt string
	var at, assembled time.Time
	if err := pool.QueryRow(ctx, `
		SELECT g.id, g.ai_agent_id, g.prompt_rendered, g.created_at, g.assembled_at
		FROM   messages i
		JOIN   generations g ON g.message_batch_id = i.message_batch_id
		WHERE  i.wamid = $1`, wamid).Scan(&generation, &agent, &prompt, &at, &assembled); err != nil {
		t.Fatalf("reading the generation of %s: %v", wamid, err)
	}
	// The components are read before the model is called, so their instant can never come after the row's (ADR-0039, regla 10).
	if assembled.After(at) {
		t.Fatalf("assembled_at %v comes after created_at %v: it is not the instant of the read", assembled, at)
	}
	return generation, agent, prompt, at
}

// assembledOf reads generation_components through the versions, in the order they went into the prompt.
func assembledOf(ctx context.Context, t *testing.T, pool db.Pool, generation uuid.UUID) []assembledBlock {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT c.type, v.version_number
		FROM   generation_components gc
		JOIN   prompt_component_versions v ON v.id = gc.prompt_component_version_id
		JOIN   prompt_components c ON c.id = v.prompt_component_id
		WHERE  gc.generation_id = $1
		ORDER  BY gc.position`, generation)
	if err != nil {
		t.Fatalf("reading generation_components: %v", err)
	}
	defer rows.Close()
	out := []assembledBlock{}
	for rows.Next() {
		var b assembledBlock
		if err := rows.Scan(&b.kind, &b.version); err != nil {
			t.Fatalf("scanning generation_components: %v", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading generation_components: %v", err)
	}
	return out
}

// oe1 runs the first query of the OE1 section of the design over [from, to), with its two parameters bound and nothing else changed.
func oe1(ctx context.Context, t *testing.T, pool db.Pool, from, to time.Time) (int, int, string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(oe1File))
	if err != nil {
		t.Fatalf("reading the OE1 query: %v", err)
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	start := strings.Index(text, "WITH activos_al_momento AS (")
	end := strings.Index(text, "FROM   por_generacion;")
	if start < 0 || end < start {
		t.Fatalf("the OE1 query is not where %s had it: the closing test cannot measure OE1", oe1File)
	}
	query := text[start : end+len("FROM   por_generacion")]
	query = strings.NewReplacer(":desde", "$1", ":hasta", "$2").Replace(query)

	var total, complete int
	var pct string
	if err := pool.QueryRow(ctx, "SELECT generaciones, completas, oe1_pct::text FROM ("+query+") AS oe1", from, to).
		Scan(&total, &complete, &pct); err != nil {
		t.Fatalf("running the OE1 query: %v", err)
	}
	return total, complete, pct
}
