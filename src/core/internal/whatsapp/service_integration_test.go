package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/mateo-velasquez/sonia/src/core/internal/contacts"
	"github.com/mateo-velasquez/sonia/src/core/internal/conversations"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// TestReceiveCreatesTheContactTheConversationAndTheMessageInOneTransaction protects the alta automática end to end against real
// Postgres, wired the way internal/app/deps.go wires it: a first-time number leaves a contacts row, an open conversation and a
// stored message, or none of them, because the three writes share one transaction that whatsapp/ conducts (regla 5).
func TestReceiveCreatesTheContactTheConversationAndTheMessageInOneTransaction(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)
	cipher := testCipher(t)
	const secret = "secreto-de-integracion"

	// Digits only: it travels as Meta's wa_id, and contacts/ stores phone_whatsapp normalized to digits (C09 aside, ADR-0001).
	phone := fmt.Sprintf("549351%010d", time.Now().UnixNano()%10_000_000_000)
	wamid := "wamid.it." + id.New().String()

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		seeded := insertInbox(ctx, t, exec, account, nil, encrypt(t, cipher, secret))

		payload := fmt.Sprintf(`{
			"object": "whatsapp_business_account",
			"entry": [{"id": "1", "changes": [{"field": "messages", "value": {
				"metadata": {"display_phone_number": "0", "phone_number_id": %q},
				"contacts": [{"profile": {"name": "Integracion"}, "wa_id": %q}],
				"messages": [{"from": %q, "id": %q, "timestamp": "1772841600", "type": "text", "text": {"body": "hola"}}]
			}}]}]
		}`, seeded.phoneNumberID, phone, phone, wamid)

		channelRepo := NewRepository(exec, cipher)
		contactsRepo := contacts.NewRepository(exec, cipher)
		conversationsRepo := conversations.NewService(conversations.NewRepository(exec), slog.New(slog.DiscardHandler))

		// The transaction is a plain call here: exec is already inside db.InTx, and binding the three to it is deps.go's job in production.
		svc := NewService(channelRepo, func(_ context.Context, fn func(Store, Aggregate, Contacts) error) error {
			return fn(channelRepo, conversationsRepo, contactsRepo)
		}, slog.New(slog.DiscardHandler))

		inbound, err := svc.Receive(ctx, []byte(payload), sign(payload, secret), time.Now().UTC())
		if err != nil {
			t.Fatalf("Receive: got %v, want no error", err)
		}
		if len(inbound) != 1 {
			t.Fatalf("started %d turns, want 1", len(inbound))
		}

		var name, sourceChannel, contactType string
		if err := exec.QueryRow(ctx, `SELECT name, source_channel, type FROM contacts WHERE phone_whatsapp = $1`, phone).
			Scan(&name, &sourceChannel, &contactType); err != nil {
			t.Fatalf("read the contact the webhook should have created: %v", err)
		}
		if name != "Integracion" || sourceChannel != string(enums.SourceChannelDirect) || contactType != string(enums.ContactTypeProspect) {
			t.Fatalf("created name=%q source_channel=%q type=%q, want the profile name, direct and prospect (ADR-0034)",
				name, sourceChannel, contactType)
		}

		var messages int
		if err := exec.QueryRow(ctx,
			`SELECT count(*) FROM messages m JOIN conversations c ON c.id = m.conversation_id WHERE c.inbox_id = $1 AND m.wamid = $2`,
			seeded.id, wamid).Scan(&messages); err != nil {
			t.Fatalf("count the stored message: %v", err)
		}
		if messages != 1 {
			t.Fatalf("stored %d messages under the conversation the webhook opened, want 1", messages)
		}

		var processed bool
		if err := exec.QueryRow(ctx, `SELECT processed_at IS NOT NULL FROM webhook_events WHERE inbox_id = $1`, seeded.id).
			Scan(&processed); err != nil {
			t.Fatalf("read the event back: %v", err)
		}
		if !processed {
			t.Fatalf("the event was never marked processed, so a restart would read this delivery as unfinished")
		}

		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}
