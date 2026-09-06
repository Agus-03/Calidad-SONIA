// Package smoke drives the whole Fase 1 path from outside: the only test that can say the two services are wired.
package smoke

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
)

// The fixed rows of src/core/seeds/0001_demo.sql: the inbox the webhook resolves against and the contact that writes.
const (
	seededPhoneNumberID = "000000000000000"
	seededContactPhone  = "5493510000000"
)

// Generous because it is only reached when something is broken: a healthy doubled turn lands in well under a second.
const turnDeadline = 30 * time.Second

// What the phase promised to leave behind, read in one query so a half-written turn cannot look finished.
type outcome struct {
	batchProcessed      bool
	batchError          string
	messageDirection    string
	messageStatus       string
	messageHasWamid     bool
	generationCost      string
	generationState     string
	promptHasComponents bool
	promptHasEnvelope   bool
}

func TestAWebhookBecomesAnAnsweredTurnWithItsEvidence(t *testing.T) {
	core, brain, pool := stack(t)
	ctx := context.Background()

	// Both ends: the core answers /health without the brain, so a booting brain fails the turn thirty seconds later (ADR-0029).
	waitForHealth(t, core)
	waitForHealth(t, brain)

	// Fresh per run: the core ignores a wamid it already stored, so a fixed one would assert the rows of the previous run.
	wamid := "wamid.smoke." + uuid.New().String()

	if status := postWebhook(t, core, wamid); status != http.StatusOK {
		t.Fatalf("POST /webhooks/whatsapp: got %d, want 200", status)
	}

	got := awaitTurn(ctx, t, pool, wamid)

	if !got.batchProcessed {
		t.Error("message_batches.processed_at is NULL: section 5 reads that as a turn nobody finished")
	}
	if got.batchError != "" {
		t.Errorf("message_batches.error_message: got %q, want empty", got.batchError)
	}
	// A link to the inbound message would satisfy the join above and mean the opposite of what the turn promised.
	if got.messageDirection != "outbound" {
		t.Errorf("the message the generation points at: got %q, want outbound", got.messageDirection)
	}
	if got.messageStatus != "sent" {
		t.Errorf("the outbound message status: got %q, want sent", got.messageStatus)
	}
	if !got.messageHasWamid {
		t.Error("the outbound message has no wamid: nothing proves the channel accepted it (ADR-0020)")
	}
	if got.generationCost == "" {
		t.Error("generations has no cost: OE2 compares providers with that number")
	}
	if got.generationState != "auto_sent" {
		t.Errorf("generations.status: got %q, want auto_sent", got.generationState)
	}
	if !got.promptHasComponents || !got.promptHasEnvelope {
		t.Error("prompt_rendered carries neither the components nor the envelope: the assembler of F03 did not run")
	}
}

// Skips instead of failing: this case needs the three services up, which make smoke decides and a plain go test cannot.
func stack(t *testing.T) (string, string, db.Pool) {
	t.Helper()

	core := os.Getenv("SONIA_SMOKE_CORE_URL")
	brain := os.Getenv("SONIA_SMOKE_BRAIN_URL")
	dsn := os.Getenv("SONIA_TEST_DATABASE_URL")
	// Named rather than defaulted: a default on the wrong port fails as a timeout, which reads like a bug in the turn.
	if core == "" || brain == "" || dsn == "" {
		t.Skip("SONIA_SMOKE_CORE_URL, SONIA_SMOKE_BRAIN_URL and SONIA_TEST_DATABASE_URL are not all set: run make smoke")
	}

	pool, err := db.Open(context.Background(), db.Options{DatabaseURL: dsn, MinSize: 1, MaxSize: 2})
	if err != nil {
		t.Fatalf("Open: got %v, want no error", err)
	}
	t.Cleanup(pool.Close)
	return core, brain, pool
}

// Polls the core instead of trusting compose: depends_on waits for the container, not for the schema to be migrated.
func waitForHealth(t *testing.T, base string) {
	t.Helper()

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		res, err := http.Get(base + "/health")
		if err == nil {
			_ = res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("%s/health never answered 200: the stack is not up", base)
}

func postWebhook(t *testing.T, base, wamid string) int {
	t.Helper()

	// Meta's shape and not ours: a fixture built from the core's own parser could not catch the parser drifting from Meta.
	payload := fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":"%[1]s","changes":[{"field":"messages",`+
		`"value":{"metadata":{"display_phone_number":"%[2]s","phone_number_id":"%[1]s"},`+
		`"contacts":[{"wa_id":"%[2]s","profile":{"name":"Contacto Demo"}}],`+
		`"messages":[{"from":"%[2]s","id":"%[3]s","timestamp":"1788700000","type":"text",`+
		`"text":{"body":"Hola, el depto de 2 ambientes en Jesus Maria sigue disponible?"}}]}}]}]}`,
		seededPhoneNumberID, seededContactPhone, wamid)

	res, err := http.Post(base+"/webhooks/whatsapp", "application/json", bytes.NewReader([]byte(payload)))
	if err != nil {
		t.Fatalf("POST /webhooks/whatsapp: got %v, want no error", err)
	}
	defer func() { _ = res.Body.Close() }()
	return res.StatusCode
}

// The answer hangs from generations.message_id, the link of ADR-0017 (i).2: no batch column points at it (F10).
const turnOutcome = `
SELECT b.processed_at IS NOT NULL,
       coalesce(b.error_message, ''),
       o.direction,
       coalesce(o.status, ''),
       o.wamid IS NOT NULL,
       coalesce(g.cost::text, ''),
       coalesce(g.status, ''),
       position('Ayudar a potenciales inquilinos' in g.prompt_rendered) > 0,
       position('[MENSAJE DEL CLIENTE]' in g.prompt_rendered) > 0
FROM   messages i
JOIN   message_batches b ON b.id = i.message_batch_id
JOIN   generations g ON g.message_batch_id = b.id
JOIN   messages o ON o.id = g.message_id
WHERE  i.wamid = $1`

func awaitTurn(ctx context.Context, t *testing.T, pool db.Pool, wamid string) outcome {
	t.Helper()

	deadline := time.Now().Add(turnDeadline)
	for {
		var got outcome
		err := pool.QueryRow(ctx, turnOutcome, wamid).Scan(
			&got.batchProcessed, &got.batchError, &got.messageDirection, &got.messageStatus,
			&got.messageHasWamid, &got.generationCost, &got.generationState,
			&got.promptHasComponents, &got.promptHasEnvelope,
		)
		switch {
		case err == nil:
			return got
		case !errors.Is(err, pgx.ErrNoRows):
			t.Fatalf("reading the turn: got %v, want no error", err)
		case time.Now().After(deadline):
			t.Fatalf("no answered turn %s after %s: %s", wamid, turnDeadline, diagnose(ctx, pool, wamid))
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// Only on the timeout: without it a broken turn and a stack that never got the message fail in exactly the same way.
func diagnose(ctx context.Context, pool db.Pool, wamid string) string {
	var batch, processed, reason string
	var written, linked int
	err := pool.QueryRow(ctx, `
		SELECT b.id::text,
		       coalesce(b.processed_at::text, 'NULL'),
		       coalesce(b.error_message, ''),
		       (SELECT count(*) FROM generations g WHERE g.message_batch_id = b.id),
		       (SELECT count(*) FROM generations g WHERE g.message_batch_id = b.id AND g.message_id IS NOT NULL)
		FROM   messages i
		JOIN   message_batches b ON b.id = i.message_batch_id
		WHERE  i.wamid = $1`, wamid).Scan(&batch, &processed, &reason, &written, &linked)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "the inbound message was never stored, so the webhook did not reach the database"
	case err != nil:
		return fmt.Sprintf("the batch could not be read either: %v", err)
	case reason != "":
		return fmt.Sprintf("batch %s failed: %s", batch, reason)
	case written == 0:
		return fmt.Sprintf("batch %s: processed_at=%s and the brain wrote no generation at all", batch, processed)
	case linked == 0:
		return fmt.Sprintf("batch %s: the generation was written but message_id is NULL, so no answer hangs from it", batch)
	}
	return fmt.Sprintf("batch %s: processed_at=%s, generation written and linked, and the query still matched nothing", batch, processed)
}
