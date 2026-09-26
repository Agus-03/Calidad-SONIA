package smoke

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/encryption"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// The sentence the doubled extractor reads as a name (src/brain/adapters/llm/double.py), with a DNI the checkpoint takes by pattern.
const introducesHerself = "hola, me llamo Lucía Pérez y mi DNI es 30.123.456"

// TestWhatTheCustomerSaysAboutHerselfIsKeptEncrypted is ADR-0047 through the real process: with the toggle on and the consent
// given, the name comes from the model and the DNI from the checkpoint, the core keeps both encrypted as extracted data of the
// contact, and each names the message it was said in.
func TestWhatTheCustomerSaysAboutHerselfIsKeptEncrypted(t *testing.T) {
	core, brain, pool := stack(t)
	ctx := context.Background()
	waitForHealth(t, core)
	waitForHealth(t, brain)

	customer := freshCustomer()
	t.Cleanup(func() { tearDownCut(t, pool, customer) })

	var account, agent uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT a.id, i.default_ai_agent_id FROM accounts a, inboxes i LIMIT 1`).Scan(&account, &agent); err != nil {
		t.Skip("the demo rows are missing: load src/core/seeds/0001_demo.sql first")
	}
	// The demo agent answers every case, so the toggle is switched on for this one and back off after it.
	if _, err := pool.Exec(ctx, `UPDATE ai_agents SET extraction_enabled = true WHERE id = $1`, agent); err != nil {
		t.Fatalf("switching the extraction on: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `UPDATE ai_agents SET extraction_enabled = false WHERE id = $1`, agent); err != nil {
			t.Errorf("switching the extraction off: %v", err)
		}
	})
	// Consent given, so the DNI is kept (ADR-0047, section 2.6); the webhook resolves this contact by its phone.
	if _, err := pool.Exec(ctx, `
		INSERT INTO contacts (id, account_id, phone_whatsapp, name, lastname, type, source_channel, consent_given_at)
		VALUES ($1, $2, $3, 'Cliente', 'Consintió', 'prospect', 'direct', now())`, id.New(), account, customer); err != nil {
		t.Fatalf("seeding the contact: %v", err)
	}

	wamid := "wamid.smoke.extraction." + uuid.New().String()
	if status := postWebhookSaying(t, core, wamid, signingSecret(ctx, t, pool), customer, introducesHerself); status != http.StatusOK {
		t.Fatalf("POST /webhooks/whatsapp: got %d, want 200", status)
	}
	awaitTurn(ctx, t, pool, wamid)

	cipher := smokeCipher(t)
	rows, err := pool.Query(ctx, `
		SELECT a.key, a.value_encrypted, a.source, a.source_message_id = m.id
		FROM   contact_attributes a
		JOIN   contacts c ON c.id = a.contact_id
		JOIN   messages m ON m.wamid = $2
		WHERE  c.phone_whatsapp = $1 AND a.deleted_at IS NULL`, customer, wamid)
	if err != nil {
		t.Fatalf("reading the attributes: %v", err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var key, source string
		var stored []byte
		var fromTheMessage bool
		if err := rows.Scan(&key, &stored, &source, &fromTheMessage); err != nil {
			t.Fatalf("scanning an attribute: %v", err)
		}
		if strings.Contains(string(stored), "30123456") {
			t.Fatalf("%s is stored in the clear", key)
		}
		if source != "llm" || !fromTheMessage {
			t.Fatalf("%s: source %q, from the message %v — want llm, pointing at what the customer wrote", key, source, fromTheMessage)
		}
		if got[key], err = cipher.Decrypt(stored); err != nil {
			t.Fatalf("decrypting %s: %v", key, err)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the attributes: %v", err)
	}
	if got["first_name"] != "Lucía" || got["last_name"] != "Pérez" || got["dni"] != "30123456" {
		t.Fatalf("attributes = %v, want the name from the model and the DNI from the checkpoint", got)
	}
}

// smokeCipher is the cipher the core runs on, so a value it encrypted can be read back here.
func smokeCipher(t *testing.T) *encryption.Cipher {
	t.Helper()
	key, err := encryption.DecodeKey(os.Getenv("SONIA_SMOKE_ENCRYPTION_KEY"))
	if err != nil {
		t.Fatalf("SONIA_SMOKE_ENCRYPTION_KEY: %v", err)
	}
	cipher, err := encryption.New(key)
	if err != nil {
		t.Fatalf("encryption.New: %v", err)
	}
	return cipher
}
