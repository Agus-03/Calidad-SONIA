package smoke

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mateo-velasquez/sonia/src/core/internal/operators"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// desk is one conversation in the pool and two operators who could answer it: what the manual inbox of Fase 5 needs to be walked.
type desk struct {
	first, second     string
	firstID, secondID uuid.UUID
	contact           uuid.UUID
	conversation      uuid.UUID
}

// TestAnOperatorTakesThePoolByAnsweringThroughTheRealProcess walks the manual inbox the way an operator would: the pool shows up in
// the listing, answering it sends through the channel and takes it (ADR-0004), and from then on the other operator cannot reach it.
//
// The package tests prove each rule against a router built in memory; this one fails if the inbox is simply not plugged into the
// process, or if the doubled sender of the smoke stack is not the one the inbox was handed.
func TestAnOperatorTakesThePoolByAnsweringThroughTheRealProcess(t *testing.T) {
	core, _, pool := stack(t)
	ctx := context.Background()
	waitForHealth(t, core)

	d := buildDesk(ctx, t, pool)
	first := login(t, core, d.first, walkerPassword)
	second := login(t, core, d.second, walkerPassword)
	conversation := d.conversation.String()

	_, body := call(t, core, http.MethodGet, "/api/v1/conversations?limit=100", first, "")
	if !listed(t, body, conversation) {
		t.Fatalf("the pool is not in the operator's inbox, so nobody could answer it first - %s", body)
	}

	status, body := call(t, core, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages", first, `{"text":"buenas, te ayudo"}`)
	if status != http.StatusCreated {
		t.Fatalf("answering the pool: got %d, want 201 - %s", status, body)
	}
	if got := stringField(t, body, "status"); got != "sent" {
		t.Fatalf("the answer came back %q, want sent through the doubled channel - %s", got, body)
	}
	if got := stringField(t, body, "sender_type"); got != "operator" {
		t.Fatalf("the answer is credited to %q, want the operator - %s", got, body)
	}

	_, body = call(t, core, http.MethodGet, "/api/v1/conversations?limit=100", first, "")
	if holder := holderOf(t, body, conversation); holder != d.firstID.String() {
		t.Fatalf("after answering, the conversation is held by %q, want the operator who answered - %s", holder, body)
	}

	var assignments int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM conversation_assignments WHERE conversation_id = $1`, d.conversation).
		Scan(&assignments); err != nil {
		t.Fatalf("reading the history: %v", err)
	}
	if assignments != 1 {
		t.Fatalf("%d assignments written, want the one F26 will read as history", assignments)
	}

	_, body = call(t, core, http.MethodGet, "/api/v1/conversations?limit=100", second, "")
	if listed(t, body, conversation) {
		t.Fatalf("the other operator still sees a conversation that is no longer in the pool - %s", body)
	}
	status, body = call(t, core, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages", second, `{"text":"yo también"}`)
	if status != http.StatusNotFound {
		t.Fatalf("the other operator answering: got %d, want 404 - %s", status, body)
	}
	if code, _ := refusal(t, body); code != "conversation_not_found" {
		t.Fatalf("the refusal says %q, want conversation_not_found", code)
	}
}

// buildDesk writes by hand what nothing in the system creates over HTTP yet: operators are seeded like the contact walk seeds them,
// and a conversation only exists because a webhook opened it, which is not what this case is about.
func buildDesk(ctx context.Context, t *testing.T, pool db.Pool) desk {
	t.Helper()

	var account, inbox uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT a.id, i.id FROM accounts a, inboxes i LIMIT 1`).Scan(&account, &inbox); err != nil {
		t.Skip("the demo rows are missing: load src/core/seeds/0001_demo.sql first")
	}
	hash, err := operators.Hash(walkerPassword)
	if err != nil {
		t.Fatalf("hashing the password: %v", err)
	}

	d := &desk{}
	// Armed before the first row exists, so a failure halfway through still takes out what got written.
	t.Cleanup(func() { tearDownDesk(t, pool, d) })

	for _, into := range []struct {
		email *string
		id    *uuid.UUID
	}{{&d.first, &d.firstID}, {&d.second, &d.secondID}} {
		operatorID := id.New()
		email := "desk-" + operatorID.String() + "@coop.test"
		if _, err := pool.Exec(ctx, `
			INSERT INTO operators (id, account_id, name, lastname, email, password_hash, role, active)
			VALUES ($1, $2, 'Bandeja', 'Manual', $3, $4, 'operator', true)`,
			operatorID, account, email, hash); err != nil {
			t.Fatalf("seeding an operator: %v", err)
		}
		*into.email, *into.id = email, operatorID
	}

	contact := id.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO contacts (id, account_id, phone_whatsapp, name, lastname, type, source_channel)
		VALUES ($1, $2, $3, 'Cliente', 'DelPool', 'prospect', 'direct')`,
		contact, account, "549351"+onlyDigits(contact.String())); err != nil {
		t.Fatalf("seeding the contact: %v", err)
	}
	d.contact = contact

	// In the pool, with the agent off and the window open: the conversation a manual inbox exists for (ADR-0004, ADR-0008).
	conversation := id.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO conversations (id, contact_id, inbox_id, status, mode, last_inbound_at, window_expires_at)
		VALUES ($1, $2, $3, 'active', 'off', now(), now() + interval '24 hours')`,
		conversation, contact, inbox); err != nil {
		t.Fatalf("seeding the conversation: %v", err)
	}
	d.conversation = conversation

	return *d
}

// The rows are taken out children first, or the foreign keys refuse and the next run starts on top of this one. Deleting messages
// here is fixture cleanup and not the application: F29 and C09 forbid the system to delete one, and nothing in it does.
func tearDownDesk(t *testing.T, pool db.Pool, d *desk) {
	t.Helper()

	ctx := context.Background()
	conversations := written(d.conversation.String())
	for _, step := range []struct {
		what string
		sql  string
		args []any
	}{
		{"the assignments", `DELETE FROM conversation_assignments WHERE conversation_id = ANY($1)`, []any{conversations}},
		{"the messages", `DELETE FROM messages WHERE conversation_id = ANY($1)`, []any{conversations}},
		{"the conversation", `DELETE FROM conversations WHERE id = ANY($1)`, []any{conversations}},
		{"the contact", `DELETE FROM contacts WHERE id = ANY($1)`, []any{written(d.contact.String())}},
		{"the operators", `DELETE FROM operators WHERE id = ANY($1)`, []any{written(d.firstID.String(), d.secondID.String())}},
	} {
		if _, err := pool.Exec(ctx, step.sql, step.args...); err != nil {
			t.Errorf("cleaning up %s: %v", step.what, err)
		}
	}
}

// holderOf reads who holds one conversation of an inbox page, or "" when it is in the pool or not on the page.
func holderOf(t *testing.T, body, conversationID string) string {
	t.Helper()

	var page struct {
		Items []struct {
			ID       string `json:"id"`
			Operator *struct {
				ID string `json:"id"`
			} `json:"operator"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("the inbox is not json: %v - %s", err, body)
	}
	for _, one := range page.Items {
		if one.ID == conversationID && one.Operator != nil {
			return one.Operator.ID
		}
	}
	return ""
}

// How long a message is watched for an answer that must not come. A healthy doubled turn lands well under a second (turnDeadline),
// so three is room enough for the bot to have answered if it was going to; the positive control below is what makes it a proof.
const silenceWindow = 3 * time.Second

// TestTheBotTurnedOffStaysSilentThroughTheRealProcess is the proof that the inbox of Fase 5 is manual: a supervisor turns the agent
// off over the API, the customer writes, and nothing answers. Then the agent is turned back on and the next message is answered,
// which is what tells "the mode kept it quiet" apart from "the stack was down".
//
// It works on the demo contact's open conversation because postWebhook writes as that contact, and it restores the mode it found.
func TestTheBotTurnedOffStaysSilentThroughTheRealProcess(t *testing.T) {
	core, brain, pool := stack(t)
	ctx := context.Background()
	waitForHealth(t, core)
	waitForHealth(t, brain)

	secret := signingSecret(ctx, t, pool)
	conversation := demoConversation(ctx, t, core, pool, secret)
	supervisor, operator := seedSwitchers(ctx, t, core, pool)
	path := "/api/v1/conversations/" + conversation.String() + "/mode"

	// The decision of the team, through the real router: the vendor cannot touch the toggle (it departs from Anexo B).
	if status, body := call(t, core, http.MethodPatch, path, operator, `{"mode":"off"}`); status != http.StatusForbidden {
		t.Fatalf("an operator turning the bot off: got %d, want 403 - %s", status, body)
	}

	if status, body := call(t, core, http.MethodPatch, path, supervisor, `{"mode":"off"}`); status != http.StatusOK {
		t.Fatalf("a supervisor turning the bot off: got %d, want 200 - %s", status, body)
	}
	silenced := "wamid.smoke.off." + uuid.New().String()
	if status := postWebhook(t, core, silenced, secret); status != http.StatusOK {
		t.Fatalf("POST /webhooks/whatsapp: got %d, want 200", status)
	}
	if answeredWithin(ctx, t, pool, silenced, silenceWindow) {
		t.Fatalf("the bot answered a conversation a supervisor had turned off")
	}

	if status, body := call(t, core, http.MethodPatch, path, supervisor, `{"mode":"autonomous"}`); status != http.StatusOK {
		t.Fatalf("a supervisor turning the bot back on: got %d, want 200 - %s", status, body)
	}
	heard := "wamid.smoke.on." + uuid.New().String()
	if status := postWebhook(t, core, heard, secret); status != http.StatusOK {
		t.Fatalf("POST /webhooks/whatsapp: got %d, want 200", status)
	}
	if got := awaitTurn(ctx, t, pool, heard); got.messageStatus != "sent" {
		t.Fatalf("with the bot back on, the answer came out %q, want sent", got.messageStatus)
	}

	// By now the silenced message's turn is long over: if it had been answered, it would show here.
	if answeredWithin(ctx, t, pool, silenced, 0) {
		t.Fatalf("the message sent while the bot was off got an answer after all")
	}
}

// demoConversation returns the open conversation of the demo contact, letting a first webhook open it when a fresh database has
// none yet, and restores its mode when the case ends so the other cases start from the conversation they expect.
func demoConversation(ctx context.Context, t *testing.T, core string, pool db.Pool, secret string) uuid.UUID {
	t.Helper()

	const open = `
		SELECT c.id, c.mode
		FROM   conversations c
		JOIN   contacts ct ON ct.id = c.contact_id
		WHERE  ct.phone_whatsapp = $1 AND ct.deleted_at IS NULL AND c.status <> 'resolved' AND c.deleted_at IS NULL`

	var conversation uuid.UUID
	var mode string
	err := pool.QueryRow(ctx, open, seededContactPhone).Scan(&conversation, &mode)
	if errors.Is(err, pgx.ErrNoRows) {
		opening := "wamid.smoke.opening." + uuid.New().String()
		if status := postWebhook(t, core, opening, secret); status != http.StatusOK {
			t.Fatalf("opening the demo conversation: got %d, want 200", status)
		}
		awaitTurn(ctx, t, pool, opening)
		err = pool.QueryRow(ctx, open, seededContactPhone).Scan(&conversation, &mode)
	}
	if err != nil {
		t.Fatalf("reading the demo conversation: %v", err)
	}

	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `UPDATE conversations SET mode = $2 WHERE id = $1`, conversation, mode); err != nil {
			t.Errorf("restoring the mode of the demo conversation: %v", err)
		}
	})
	return conversation
}

// seedSwitchers logs in one role that may touch the toggle and one that may not.
func seedSwitchers(ctx context.Context, t *testing.T, core string, pool db.Pool) (supervisor, operator string) {
	t.Helper()

	var account uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts LIMIT 1`).Scan(&account); err != nil {
		t.Skip("the demo rows are missing: load src/core/seeds/0001_demo.sql first")
	}
	hash, err := operators.Hash(walkerPassword)
	if err != nil {
		t.Fatalf("hashing the password: %v", err)
	}

	seeded := []uuid.UUID{}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM operators WHERE id = ANY($1)`, seeded); err != nil {
			t.Errorf("cleaning up the operators: %v", err)
		}
	})

	tokens := map[string]string{}
	for _, role := range []string{"supervisor", "operator"} {
		operatorID := id.New()
		email := "switch-" + operatorID.String() + "@coop.test"
		if _, err := pool.Exec(ctx, `
			INSERT INTO operators (id, account_id, name, lastname, email, password_hash, role, active)
			VALUES ($1, $2, 'Interruptor', $3, $4, $5, $3, true)`,
			operatorID, account, role, email, hash); err != nil {
			t.Fatalf("seeding the %s: %v", role, err)
		}
		seeded = append(seeded, operatorID)
		tokens[role] = login(t, core, email, walkerPassword)
	}
	return tokens["supervisor"], tokens["operator"]
}

// answeredWithin watches one inbound message for the turn that would answer it: a batch linked to it is the first trace a turn
// leaves, and a conversation that is off never opens one (turn/orchestrator.go).
func answeredWithin(ctx context.Context, t *testing.T, pool db.Pool, wamid string, window time.Duration) bool {
	t.Helper()

	deadline := time.Now().Add(window)
	for {
		var batched bool
		if err := pool.QueryRow(ctx, `SELECT message_batch_id IS NOT NULL FROM messages WHERE wamid = $1`, wamid).Scan(&batched); err != nil {
			t.Fatalf("the inbound message %s was never stored: %v", wamid, err)
		}
		if batched {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(250 * time.Millisecond)
	}
}
