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

	// The thread the operator reads before answering again: its own answer on top, sent, and signed with its name.
	thread := "/api/v1/conversations/" + conversation + "/messages"
	status, body = call(t, core, http.MethodGet, thread, first, "")
	if status != http.StatusOK {
		t.Fatalf("reading the thread: got %d, want 200 - %s", status, body)
	}
	if newest := newestOf(t, body); newest.SenderType != "operator" || newest.Status != "sent" || newest.SenderOperatorID != d.firstID.String() {
		t.Fatalf("the newest message is %+v, want the operator's answer, sent and signed - %s", newest, body)
	}
	if status, body = call(t, core, http.MethodGet, thread, second, ""); status != http.StatusNotFound {
		t.Fatalf("the other operator reading the thread: got %d, want 404 - %s", status, body)
	}
}

// TestAStarIsSharedThroughTheRealProcess walks F25 the way the inbox would: one operator stars a conversation of the pool, the other
// sees the same star on its listing, clearing it clears it for both, and the row keeps its place because a star is not activity
// (ADR-0036). It fails if the route is not plugged into the process or the listing stops carrying the flag.
func TestAStarIsSharedThroughTheRealProcess(t *testing.T) {
	core, _, pool := stack(t)
	ctx := context.Background()
	waitForHealth(t, core)

	d := buildDesk(ctx, t, pool)
	first := login(t, core, d.first, walkerPassword)
	second := login(t, core, d.second, walkerPassword)
	conversation := d.conversation.String()
	path := "/api/v1/conversations/" + conversation + "/starred"

	_, body := call(t, core, http.MethodGet, "/api/v1/conversations?limit=100", second, "")
	starred, activity := starOf(t, body, conversation)
	if starred {
		t.Fatalf("the conversation is starred before anyone starred it - %s", body)
	}

	status, body := call(t, core, http.MethodPatch, path, first, `{"starred":true}`)
	if status != http.StatusOK {
		t.Fatalf("starring a conversation of the pool: got %d, want 200 - %s", status, body)
	}
	_, body = call(t, core, http.MethodGet, "/api/v1/conversations?limit=100", second, "")
	if got, moved := starOf(t, body, conversation); !got || moved != activity {
		t.Fatalf("the other operator sees starred %v with activity %q, want true and %q unchanged - %s", got, moved, activity, body)
	}

	if status, body := call(t, core, http.MethodPatch, path, second, `{"starred":false}`); status != http.StatusOK {
		t.Fatalf("clearing the star: got %d, want 200 - %s", status, body)
	}
	_, body = call(t, core, http.MethodGet, "/api/v1/conversations?limit=100", first, "")
	if got, _ := starOf(t, body, conversation); got {
		t.Fatalf("the star cleared by one operator is still there for the other - %s", body)
	}
}

// starOf reads the star and the last activity of one conversation of an inbox page, and fails when the conversation is not on it.
func starOf(t *testing.T, body, conversationID string) (bool, string) {
	t.Helper()

	var page struct {
		Items []struct {
			ID             string `json:"id"`
			Starred        bool   `json:"starred"`
			LastActivityAt string `json:"last_activity_at"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("the inbox is not json: %v - %s", err, body)
	}
	for _, one := range page.Items {
		if one.ID == conversationID {
			return one.Starred, one.LastActivityAt
		}
	}
	t.Fatalf("the conversation %s is not on the inbox page - %s", conversationID, body)
	return false, ""
}

type drawnMessage struct {
	SenderType       string
	Status           string
	SenderOperatorID string
}

// newestOf reads the first message of a thread page, which is the newest one.
func newestOf(t *testing.T, body string) drawnMessage {
	t.Helper()

	var page struct {
		Items []struct {
			SenderType     string  `json:"sender_type"`
			Status         *string `json:"status"`
			SenderOperator *struct {
				ID string `json:"id"`
			} `json:"sender_operator"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil || len(page.Items) == 0 {
		t.Fatalf("the thread is not a page with messages: %v - %s", err, body)
	}
	newest := page.Items[0]
	out := drawnMessage{SenderType: newest.SenderType}
	if newest.Status != nil {
		out.Status = *newest.Status
	}
	if newest.SenderOperator != nil {
		out.SenderOperatorID = newest.SenderOperator.ID
	}
	return out
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

// TestTheVendorGivenTheConversationSwitchesItsBotThroughTheRealProcess walks ADR-0037 end to end: a conversation in the pool is not
// the vendor's to switch, the customer writing hands it to the vendor, the vendor turns the bot off and nothing answers, and the
// supervisor turns it back on and the next message is answered, which is what tells "the mode kept it quiet" from "the stack was down".
//
// It works on the demo contact's open conversation because postWebhook writes as that contact. It starts that conversation in the
// pool and leaves it there, and restores the mode it found, so the turn cases find the conversation they expect.
func TestTheVendorGivenTheConversationSwitchesItsBotThroughTheRealProcess(t *testing.T) {
	core, brain, pool := stack(t)
	ctx := context.Background()
	waitForHealth(t, core)
	waitForHealth(t, brain)

	secret := signingSecret(ctx, t, pool)
	switchers := seedSwitchers(ctx, t, core, pool)
	conversation := demoConversation(ctx, t, core, pool, secret)
	path := "/api/v1/conversations/" + conversation.String() + "/mode"
	// The vendor is made the customer's previous one, so the rule that decides is the customer's and not the turn (ADR-0037, regla 1):
	// the turn would go to any other active vendor the local database has, and this case would fail on rows it did not write.
	makePrevious(ctx, t, pool, conversation, switchers.operatorID)

	// Registered after seedSwitchers, so it runs before the vendors are deleted: their assignments would keep them alive otherwise.
	release := func() {
		if _, err := pool.Exec(context.Background(), `UPDATE conversations SET operator_id = NULL WHERE id = $1`, conversation); err != nil {
			t.Errorf("returning the demo conversation to the pool: %v", err)
		}
		if _, err := pool.Exec(context.Background(),
			`DELETE FROM conversation_assignments WHERE conversation_id = $1 AND to_operator_id = ANY($2)`, conversation, switchers.seeded); err != nil {
			t.Errorf("cleaning up the assignments: %v", err)
		}
	}
	release()
	t.Cleanup(release)

	// ADR-0037, regla 4: the pool is visible to the vendor but not its to switch.
	status, body := call(t, core, http.MethodPatch, path, switchers.operator, `{"mode":"off"}`)
	if status != http.StatusConflict {
		t.Fatalf("a vendor switching a conversation of the pool: got %d, want 409 - %s", status, body)
	}
	if code, _ := refusal(t, body); code != "conversation_not_assigned" {
		t.Fatalf("the refusal says %q, want conversation_not_assigned", code)
	}

	// ADR-0037, regla 1: the customer writing is what hands the conversation to the vendor, through the real webhook.
	arrival := "wamid.smoke.arrival." + uuid.New().String()
	if status := postWebhook(t, core, arrival, secret); status != http.StatusOK {
		t.Fatalf("POST /webhooks/whatsapp: got %d, want 200", status)
	}
	awaitTurn(ctx, t, pool, arrival)
	_, body = call(t, core, http.MethodGet, "/api/v1/conversations?limit=100", switchers.operator, "")
	if holder := holderOf(t, body, conversation.String()); holder != switchers.operatorID.String() {
		t.Fatalf("after the customer wrote, the conversation is held by %q, want the vendor %v - %s", holder, switchers.operatorID, body)
	}

	if status, body := call(t, core, http.MethodPatch, path, switchers.operator, `{"mode":"off"}`); status != http.StatusOK {
		t.Fatalf("the vendor turning off the bot of its conversation: got %d, want 200 - %s", status, body)
	}
	silenced := "wamid.smoke.off." + uuid.New().String()
	if status := postWebhook(t, core, silenced, secret); status != http.StatusOK {
		t.Fatalf("POST /webhooks/whatsapp: got %d, want 200", status)
	}
	if answeredWithin(ctx, t, pool, silenced, silenceWindow) {
		t.Fatalf("the bot answered a conversation its vendor had turned off")
	}

	if status, body := call(t, core, http.MethodPatch, path, switchers.supervisor, `{"mode":"autonomous"}`); status != http.StatusOK {
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

// makePrevious gives the contact of the conversation a resolved one that began after it, held by the vendor: the latest earlier
// conversation PreviousOperator reads. It is deleted before the vendor is, since it would keep the vendor alive otherwise.
func makePrevious(ctx context.Context, t *testing.T, pool db.Pool, conversation, vendor uuid.UUID) {
	t.Helper()

	previous := id.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO conversations (id, contact_id, inbox_id, operator_id, status, mode, last_inbound_at, window_expires_at, resolved_at)
		SELECT $1, contact_id, inbox_id, $3, 'resolved', 'autonomous', now(), now(), now()
		FROM   conversations
		WHERE  id = $2`, previous, conversation, vendor); err != nil {
		t.Fatalf("seeding the customer's previous conversation: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM conversations WHERE id = $1`, previous); err != nil {
			t.Errorf("cleaning up the previous conversation: %v", err)
		}
	})
}

// switchers are the two roles of the toggle: the supervisor, who switches any conversation, and the vendor, who switches its own.
type switchers struct {
	supervisor, operator string
	operatorID           uuid.UUID
	seeded               []uuid.UUID
}

// seedSwitchers logs in a supervisor and a vendor, and deletes both when the case ends.
func seedSwitchers(ctx context.Context, t *testing.T, core string, pool db.Pool) switchers {
	t.Helper()

	var account uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts LIMIT 1`).Scan(&account); err != nil {
		t.Skip("the demo rows are missing: load src/core/seeds/0001_demo.sql first")
	}
	hash, err := operators.Hash(walkerPassword)
	if err != nil {
		t.Fatalf("hashing the password: %v", err)
	}

	s := switchers{}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM operators WHERE id = ANY($1)`, s.seeded); err != nil {
			t.Errorf("cleaning up the operators: %v", err)
		}
	})

	for _, role := range []string{"supervisor", "operator"} {
		operatorID := id.New()
		email := "switch-" + operatorID.String() + "@coop.test"
		if _, err := pool.Exec(ctx, `
			INSERT INTO operators (id, account_id, name, lastname, email, password_hash, role, active)
			VALUES ($1, $2, 'Interruptor', $3, $4, $5, $3, true)`,
			operatorID, account, role, email, hash); err != nil {
			t.Fatalf("seeding the %s: %v", role, err)
		}
		s.seeded = append(s.seeded, operatorID)
		token := login(t, core, email, walkerPassword)
		if role == "supervisor" {
			s.supervisor = token
		} else {
			s.operator, s.operatorID = token, operatorID
		}
	}
	return s
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
