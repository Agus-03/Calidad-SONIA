package smoke

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
)

// A word of src/brain/rules/profanity.yaml, and the only lever the smoke has on the pipeline from outside: the doubled provider
// echoes what the customer wrote, so a customer who swears gets the insult back and the outbound blocklist refuses that answer.
// It is also the one cut the shipped rules can reach — blocklist.yaml ships empty (decision D4) and the doubled triage always
// answers in_domain, so in a doubled stack neither the inbound blocklist nor the triage can fire.
const refusedWord = "pelotudo"

// What a cut turn promised to leave behind: the refused answer stored and never linked, the fixed text sent in its place, and the
// conversation waiting for a person. Read in one query, so a half-written cut cannot look finished.
type cut struct {
	batchProcessed   bool
	batchError       string
	conversation     string
	generationState  string
	generationLinked bool
	generationCost   string
	noticeSender     string
	noticeEchoed     bool
	steps            int
	lastStep         string
	lastDirection    string
	lastOutcome      string
	lastFile         string
	lastVersion      string
	transfers        int
	evaluations      int
}

// TestAnAnswerNobodyMaySendIsKeptUnsentAndTheTurnWaitsForAPerson is 07-secuencia B.2 with decision O2 on top: the generation was
// paid for and is kept with its verdict, the customer reads the fixed text of the cut point instead, and nothing the AIAgent
// produced reaches the channel.
func TestAnAnswerNobodyMaySendIsKeptUnsentAndTheTurnWaitsForAPerson(t *testing.T) {
	core, brain, pool := stack(t)
	ctx := context.Background()

	waitForHealth(t, core)
	waitForHealth(t, brain)

	// A number nobody wrote from yet, so this conversation holds exactly one turn: the fixed text hangs from no batch, and in the
	// seeded contact's conversation there would be no way to tell this run's notice from the previous one's.
	customer := freshCustomer()
	wamid := "wamid.smoke.cut." + uuid.New().String()
	// Armed before the first row exists: this is the only case of the package that opens a conversation of its own on the demo
	// inbox, so without it every run leaves behind a contact nobody ever wrote from again.
	t.Cleanup(func() { tearDownCut(t, pool, customer) })
	said := fmt.Sprintf("Sos un %s, hace tres dias que espero el depto de 2 ambientes", refusedWord)

	if status := postWebhookSaying(t, core, wamid, signingSecret(ctx, t, pool), customer, said); status != http.StatusOK {
		t.Fatalf("POST /webhooks/whatsapp: got %d, want 200", status)
	}

	got := awaitCut(ctx, t, pool, wamid)

	if !got.batchProcessed {
		t.Error("message_batches.processed_at is NULL: section 5 reads a cut turn as one nobody finished")
	}
	// A cut is a decision of the pipeline and not a failure of the turn: error_message is for the turns that fell over.
	if got.batchError != "" {
		t.Errorf("message_batches.error_message: got %q, want empty", got.batchError)
	}
	if got.conversation != "waiting" {
		t.Errorf("conversations.status: got %q, want waiting — nobody is attending a cut turn (section 4.2)", got.conversation)
	}
	if got.generationState != "blocked" {
		t.Errorf("generations.status: got %q, want blocked", got.generationState)
	}
	// The whole point of a cut on the way out: the answer exists, it cost money, and it is unreachable for ever.
	if got.generationLinked {
		t.Error("generations.message_id points at a message: the answer the pipeline refused was linked to one (section 4.2)")
	}
	if got.generationCost == "" {
		t.Error("generations has no cost: a refused answer was paid for and OE2 counts that money too")
	}
	// Decision O2, and the third arm of the sender check of messages: the text is nobody's answer, so it names no sender.
	if got.noticeSender != "system" {
		t.Errorf("the message that went out: sender_type %q, want system — the AIAgent never wrote this text", got.noticeSender)
	}
	if got.noticeEchoed {
		t.Errorf("the message that went out carries %q: the answer the pipeline refused was sent anyway", refusedWord)
	}
	// Five and not six: the guardrail of the outbound leg never ran, because the blocklist before it ended the turn (ADR-0016).
	if got.steps != 5 {
		t.Errorf("pipeline_steps: got %d rows, want 5 — the trace of a cut ends where the cut happened", got.steps)
	}
	if got.lastStep != "blocklist" || got.lastDirection != "outbound" || got.lastOutcome != "blocked" {
		t.Errorf("the last step of the trace: got %s/%s/%s, want blocklist/outbound/blocked",
			got.lastStep, got.lastDirection, got.lastOutcome)
	}
	// Decision O6b through the real process: the word came from the file the team loads and not from the policy of the business.
	if got.lastFile != "profanity.yaml" {
		t.Errorf("the file the blocked term came from: got %q, want profanity.yaml", got.lastFile)
	}
	// The composite version of the outbound run, which is the amendment this phase owes ADR-0016: loading a word has to move it.
	if !strings.Contains(got.lastVersion, "profanity") {
		t.Errorf("pipeline_steps.version of the outbound blocklist: got %q, want it to name both files", got.lastVersion)
	}
	// A blocklist refuses, it does not delegate: what O1c hands to a person is a question outside the domain, and that cut is the
	// triage's. Nothing here was evaluated as a signal, so nothing may be counted as an escalation (F36, ADR-0024).
	if got.evaluations != 0 || got.transfers != 0 {
		t.Errorf("a refused word left %d evaluations and %d transfers, want none of either", got.evaluations, got.transfers)
	}
}

// The fixed text hangs from the conversation and from no batch, so it is found by the one thing that identifies it: the system is
// the only sender that is neither the AIAgent nor a person (H-09). The step that cut is read by its position, which is the last.
const turnCut = `
SELECT b.processed_at IS NOT NULL,
       coalesce(b.error_message, ''),
       c.status,
       g.status,
       g.message_id IS NOT NULL,
       coalesce(g.cost::text, ''),
       n.sender_type,
       position($2 in n.content_text) > 0,
       (SELECT count(*) FROM pipeline_steps p WHERE p.message_batch_id = b.id),
       s.step,
       s.direction,
       s.outcome,
       coalesce(s.detail_json ->> 'file', ''),
       s.version,
       (SELECT count(*) FROM transfer_events t WHERE t.conversation_id = c.id AND t.turn_number = b.turn_number),
       (SELECT count(*) FROM signal_evaluations e WHERE e.message_batch_id = b.id)
FROM   messages i
JOIN   message_batches b ON b.id = i.message_batch_id
JOIN   conversations c ON c.id = i.conversation_id
JOIN   generations g ON g.message_batch_id = b.id
JOIN   messages n ON n.conversation_id = c.id AND n.sender_type = 'system'
JOIN   pipeline_steps s ON s.message_batch_id = b.id
                      AND  s.position = (SELECT max(p.position) FROM pipeline_steps p WHERE p.message_batch_id = b.id)
WHERE  i.wamid = $1
  AND  n.status = 'sent'`

// Waits on the text having been sent and not on processed_at: the send is the last thing a cut turn does, and everything asserted
// above was committed before it, so no assertion can read a row the core was still halfway through writing.
func awaitCut(ctx context.Context, t *testing.T, pool db.Pool, wamid string) cut {
	t.Helper()

	deadline := time.Now().Add(turnDeadline)
	for {
		var got cut
		err := pool.QueryRow(ctx, turnCut, wamid, refusedWord).Scan(
			&got.batchProcessed, &got.batchError, &got.conversation, &got.generationState,
			&got.generationLinked, &got.generationCost, &got.noticeSender, &got.noticeEchoed,
			&got.steps, &got.lastStep, &got.lastDirection, &got.lastOutcome, &got.lastFile, &got.lastVersion,
			&got.transfers, &got.evaluations,
		)
		switch {
		case err == nil:
			return got
		case !errors.Is(err, pgx.ErrNoRows):
			t.Fatalf("reading the cut turn: got %v, want no error", err)
		case time.Now().After(deadline):
			t.Fatalf("no cut turn %s after %s: %s", wamid, turnDeadline, diagnoseCut(ctx, pool, wamid))
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// Only on the timeout, and its own because every marker of a cut is what the happy path calls a broken turn: there, a generation
// with no message means the answer was lost; here it is the decision under test.
func diagnoseCut(ctx context.Context, pool db.Pool, wamid string) string {
	var batch, processed, reason, state string
	var steps, notices int
	err := pool.QueryRow(ctx, `
		SELECT b.id::text,
		       coalesce(b.processed_at::text, 'NULL'),
		       coalesce(b.error_message, ''),
		       coalesce((SELECT g.status FROM generations g WHERE g.message_batch_id = b.id LIMIT 1), 'none'),
		       (SELECT count(*) FROM pipeline_steps p WHERE p.message_batch_id = b.id),
		       (SELECT count(*) FROM messages n WHERE n.conversation_id = i.conversation_id AND n.sender_type = 'system')
		FROM   messages i
		JOIN   message_batches b ON b.id = i.message_batch_id
		WHERE  i.wamid = $1`, wamid).Scan(&batch, &processed, &reason, &state, &steps, &notices)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "the inbound message was never stored, so the webhook did not reach the database"
	case err != nil:
		return fmt.Sprintf("the batch could not be read either: %v", err)
	case reason != "":
		return fmt.Sprintf("batch %s failed: %s", batch, reason)
	case state != "blocked":
		return fmt.Sprintf("batch %s: %d steps and the generation is %q, so the word went through the outbound blocklist", batch, steps, state)
	case notices == 0:
		return fmt.Sprintf("batch %s: the answer was refused and no fixed text was written, so the customer got silence (O2)", batch)
	}
	return fmt.Sprintf("batch %s: processed_at=%s, refused and answered with the fixed text, and it was never sent", batch, processed)
}

// A number of the seeded country code that no other case is using: a contact nobody wrote from opens a conversation of its own,
// and this case needs one whose only turn is the cut it is about (ADR-0034).
func freshCustomer() string {
	return fmt.Sprintf("54935109%06d", time.Now().UnixNano()%1000000)
}

// Children first, and the order is the order of the foreign keys: the trace and the evidence of the signal hang from the batch,
// the answer from the conversation, and the conversation cannot go before any of them (ADR-0041, regla 12).
func tearDownCut(t *testing.T, pool db.Pool, customer string) {
	t.Helper()

	conversations := `SELECT c.id FROM conversations c JOIN contacts ct ON ct.id = c.contact_id WHERE ct.phone_whatsapp = $1`
	batches := `SELECT id FROM message_batches WHERE conversation_id IN (` + conversations + `)`
	for _, step := range []struct{ what, sql string }{
		{"the transfers", `DELETE FROM transfer_events WHERE conversation_id IN (` + conversations + `)`},
		{"the signal evaluations", `DELETE FROM signal_evaluations WHERE message_batch_id IN (` + batches + `)`},
		{"the pipeline steps", `DELETE FROM pipeline_steps WHERE message_batch_id IN (` + batches + `)`},
		{"the detected intents", `DELETE FROM detected_intents WHERE message_batch_id IN (` + batches + `)`},
		{"the assembled components", `DELETE FROM generation_components WHERE generation_id IN
			(SELECT id FROM generations WHERE conversation_id IN (` + conversations + `))`},
		{"the generations", `DELETE FROM generations WHERE conversation_id IN (` + conversations + `)`},
		{"the messages", `DELETE FROM messages WHERE conversation_id IN (` + conversations + `)`},
		{"the batches", `DELETE FROM message_batches WHERE conversation_id IN (` + conversations + `)`},
		{"the assignments", `DELETE FROM conversation_assignments WHERE conversation_id IN (` + conversations + `)`},
		{"the conversations", `DELETE FROM conversations WHERE id IN (` + conversations + `)`},
		{"the contact", `DELETE FROM contacts WHERE phone_whatsapp = $1`},
	} {
		if _, err := pool.Exec(context.Background(), step.sql, customer); err != nil {
			t.Errorf("cleaning up %s: %v", step.what, err)
		}
	}
}
