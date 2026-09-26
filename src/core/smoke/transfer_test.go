package smoke

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mateo-velasquez/sonia/src/core/internal/operators"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// The one sentence the doubled triage reads as a request for a person (src/brain/adapters/llm/double.py), which is what lets the
// smoke walk a transfer with no variable that exists only for tests (ADR-0045, section 2.5).
const askForAPerson = "hola, quiero hablar con una persona"

// What a transfer promised to leave behind, read in one query so a half-written one cannot look finished (ADR-0045).
type handover struct {
	processed   bool
	status      string
	mode        string
	transfers   int
	signal      string
	fired       bool
	generations int
	outbound    int
	cutBy       string
}

// TestAPersonAskedForIsHandedOverAndTheBotGoesOff is ADR-0045 through the real process: the request fires explicit_request against
// the seeded policy, the turn is cut at the triage with nothing said and nothing generated, the transfer points at that evidence,
// and the bot of the conversation goes off so the AIAgent does not answer the next message.
func TestAPersonAskedForIsHandedOverAndTheBotGoesOff(t *testing.T) {
	core, brain, pool := stack(t)
	ctx := context.Background()
	waitForHealth(t, core)
	waitForHealth(t, brain)

	customer := freshCustomer()
	wamid := "wamid.smoke.transfer." + uuid.New().String()
	// Children first: the transfer and the evidence it points at go before the batch, the conversation and the contact.
	t.Cleanup(func() { tearDownCut(t, pool, customer) })

	if status := postWebhookSaying(t, core, wamid, signingSecret(ctx, t, pool), customer, askForAPerson); status != http.StatusOK {
		t.Fatalf("POST /webhooks/whatsapp: got %d, want 200", status)
	}

	got := awaitHandover(ctx, t, pool, wamid)

	if got.status != "waiting" {
		t.Errorf("conversations.status: got %q, want waiting — a person has to pick it up", got.status)
	}
	if got.mode != "off" {
		t.Errorf("conversations.mode: got %q, want off — the AIAgent would answer a conversation handed to a person (2.7)", got.mode)
	}
	if got.transfers != 1 || got.signal != "explicit_request" || !got.fired {
		t.Errorf("transfer_events: got %d pointing at %q (fired %v), want one at the explicit_request that fired",
			got.transfers, got.signal, got.fired)
	}
	if got.generations != 0 {
		t.Errorf("generations: got %d, want none — nothing is generated for a customer who asked for a person", got.generations)
	}
	if got.outbound != 0 {
		t.Errorf("outbound messages: got %d, want none — the cut of the triage says nothing (O1c)", got.outbound)
	}
	if got.cutBy != "explicit_request" {
		t.Errorf("the triage row names %q as the signal that cut, want explicit_request", got.cutBy)
	}
}

const turnHandedOver = `
SELECT b.processed_at IS NOT NULL,
       c.status,
       c.mode,
       (SELECT count(*) FROM transfer_events t WHERE t.conversation_id = c.id),
       coalesce((SELECT e.signal_type FROM transfer_events t JOIN signal_evaluations e ON e.id = t.signal_evaluation_id
                 WHERE t.conversation_id = c.id LIMIT 1), ''),
       coalesce((SELECT e.fired FROM transfer_events t JOIN signal_evaluations e ON e.id = t.signal_evaluation_id
                 WHERE t.conversation_id = c.id LIMIT 1), false),
       (SELECT count(*) FROM generations g WHERE g.message_batch_id = b.id),
       (SELECT count(*) FROM messages o WHERE o.conversation_id = c.id AND o.direction = 'outbound'),
       coalesce((SELECT p.detail_json ->> 'transfer' FROM pipeline_steps p
                 WHERE p.message_batch_id = b.id AND p.step = 'triage'), '')
FROM   messages i
JOIN   message_batches b ON b.id = i.message_batch_id
JOIN   conversations c ON c.id = i.conversation_id
WHERE  i.wamid = $1 AND b.processed_at IS NOT NULL`

func awaitHandover(ctx context.Context, t *testing.T, pool db.Pool, wamid string) handover {
	t.Helper()

	deadline := time.Now().Add(turnDeadline)
	for {
		var got handover
		err := pool.QueryRow(ctx, turnHandedOver, wamid).Scan(&got.processed, &got.status, &got.mode, &got.transfers,
			&got.signal, &got.fired, &got.generations, &got.outbound, &got.cutBy)
		switch {
		case err == nil:
			return got
		case !errors.Is(err, pgx.ErrNoRows):
			t.Fatalf("reading the handed over turn: got %v, want no error", err)
		case time.Now().After(deadline):
			t.Fatalf("no processed turn %s after %s: is the policy of explicit_request seeded? (0001_demo.sql)", wamid, turnDeadline)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// TestASuggestionIsADraftTheOperatorCopiesAndSends is ADR-0046 through the real process: in suggestion mode the turn runs, the
// draft waits in pending_review with nothing sent to the customer, the Operator reads it and sends it as their own, and the draft is
// marked accepted and linked to that message.
func TestASuggestionIsADraftTheOperatorCopiesAndSends(t *testing.T) {
	core, brain, pool := stack(t)
	ctx := context.Background()
	waitForHealth(t, core)
	waitForHealth(t, brain)

	customer := freshCustomer()
	operatorID, email := id.New(), ""
	t.Cleanup(func() {
		tearDownCut(t, pool, customer)
		if _, err := pool.Exec(context.Background(), `DELETE FROM operators WHERE id = $1`, operatorID); err != nil {
			t.Errorf("cleaning up the operator: %v", err)
		}
	})
	email = seedSuggestionDesk(ctx, t, pool, operatorID, customer)
	token := login(t, core, email, walkerPassword)

	wamid := "wamid.smoke.suggestion." + uuid.New().String()
	if status := postWebhookSaying(t, core, wamid, signingSecret(ctx, t, pool), customer, "¿tienen cocheras?"); status != http.StatusOK {
		t.Fatalf("POST /webhooks/whatsapp: got %d, want 200", status)
	}
	conversation, generation := awaitSuggestion(ctx, t, pool, wamid)

	var answered int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE conversation_id = $1 AND direction = 'outbound'`,
		conversation).Scan(&answered); err != nil || answered != 0 {
		t.Fatalf("outbound messages before the Operator sent anything: got %d (%v), want none", answered, err)
	}

	path := "/api/v1/conversations/" + conversation.String()
	status, body := call(t, core, http.MethodGet, path+"/suggestion", token, "")
	if status != http.StatusOK || stringField(t, body, "generation_id") != generation.String() {
		t.Fatalf("GET suggestion: got %d - %s, want the draft %s", status, body, generation)
	}
	text := stringField(t, body, "text")

	status, body = call(t, core, http.MethodPost, path+"/messages", token,
		fmt.Sprintf(`{"text":%q,"generation_id":%q}`, text, generation))
	if status != http.StatusCreated || stringField(t, body, "sender_type") != "operator" {
		t.Fatalf("sending the copied draft: got %d - %s, want 201 as the Operator's", status, body)
	}

	var state string
	var linked, reviewed bool
	if err := pool.QueryRow(ctx, `
		SELECT status, message_id IS NOT NULL, reviewed_by_operator_id = $2 AND reviewed_at IS NOT NULL
		FROM   generations WHERE id = $1`, generation, operatorID).Scan(&state, &linked, &reviewed); err != nil {
		t.Fatalf("reading the reviewed draft: %v", err)
	}
	if state != "accepted" || !linked || !reviewed {
		t.Errorf("the draft after the send: status %q, linked %v, reviewed %v — want accepted, linked and reviewed", state, linked, reviewed)
	}
	if status, _ := call(t, core, http.MethodGet, path+"/suggestion", token, ""); status != http.StatusNoContent {
		t.Errorf("GET suggestion after sending it: got %d, want 204 — the draft was used", status)
	}
}

// The Operator and a conversation in suggestion mode held by them, which nothing creates over HTTP yet; the contact is the one the
// webhook resolves by phone, so the message lands in this conversation and not in a new one (ADR-0002).
func seedSuggestionDesk(ctx context.Context, t *testing.T, pool db.Pool, operatorID uuid.UUID, customer string) string {
	t.Helper()

	var account, inbox, agent uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT a.id, i.id, i.default_ai_agent_id FROM accounts a, inboxes i LIMIT 1`).
		Scan(&account, &inbox, &agent); err != nil {
		t.Skip("the demo rows are missing: load src/core/seeds/0001_demo.sql first")
	}
	hash, err := operators.Hash(walkerPassword)
	if err != nil {
		t.Fatalf("hashing the password: %v", err)
	}
	email := "suggest-" + operatorID.String() + "@coop.test"
	if _, err := pool.Exec(ctx, `
		INSERT INTO operators (id, account_id, name, lastname, email, password_hash, role, active)
		VALUES ($1, $2, 'Sugerencia', 'Vendedor', $3, $4, 'operator', true)`, operatorID, account, email, hash); err != nil {
		t.Fatalf("seeding the operator: %v", err)
	}
	contact := id.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO contacts (id, account_id, phone_whatsapp, name, lastname, type, source_channel)
		VALUES ($1, $2, $3, 'Cliente', 'Sugerido', 'prospect', 'direct')`, contact, account, customer); err != nil {
		t.Fatalf("seeding the contact: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO conversations (id, contact_id, inbox_id, ai_agent_id, operator_id, status, mode, last_inbound_at, window_expires_at)
		VALUES ($1, $2, $3, $4, $5, 'active', 'suggestion', now(), now() + interval '24 hours')`,
		id.New(), contact, inbox, agent, operatorID); err != nil {
		t.Fatalf("seeding the conversation: %v", err)
	}
	return email
}

func awaitSuggestion(ctx context.Context, t *testing.T, pool db.Pool, wamid string) (uuid.UUID, uuid.UUID) {
	t.Helper()

	deadline := time.Now().Add(turnDeadline)
	for {
		var conversation, generation uuid.UUID
		err := pool.QueryRow(ctx, `
			SELECT c.id, g.id
			FROM   messages i
			JOIN   message_batches b ON b.id = i.message_batch_id AND b.processed_at IS NOT NULL
			JOIN   conversations c ON c.id = i.conversation_id
			JOIN   generations g ON g.message_batch_id = b.id AND g.status = 'pending_review' AND g.message_id IS NULL
			WHERE  i.wamid = $1`, wamid).Scan(&conversation, &generation)
		switch {
		case err == nil:
			return conversation, generation
		case !errors.Is(err, pgx.ErrNoRows):
			t.Fatalf("reading the suggestion: got %v, want no error", err)
		case time.Now().After(deadline):
			t.Fatalf("no pending suggestion for %s after %s", wamid, turnDeadline)
		}
		time.Sleep(250 * time.Millisecond)
	}
}
