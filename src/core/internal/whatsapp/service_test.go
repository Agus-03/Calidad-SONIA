package whatsapp

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// TestVerifyGuardsTheSubscriptionOfF20 protects the handshake: without it Meta never delivers a message to the webhook.
func TestVerifyGuardsTheSubscriptionOfF20(t *testing.T) {
	seeded := "sonia-demo-verify-token"

	cases := []struct {
		name  string
		mode  string
		token string
		want  error
	}{
		{"the seeded token subscribes", subscribeMode, seeded, nil},
		{"another token is a rejection and not a lookup failure", subscribeMode, "guessed", ErrVerificationRejected},
		{"a mode Meta never sends is a rejection", "unsubscribe", seeded, ErrVerificationRejected},
		{"an empty mode is a rejection: a bare GET must not pass", "", seeded, ErrVerificationRejected},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, agg := channel()
			err := NewService(repo, agg, slog.Default()).Verify(context.Background(), tc.mode, tc.token)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Verify returned %v, want %v", err, tc.want)
			}
		})
	}
}

// TestVerifyDoesNotTurnAnOutageIntoAnIntruder keeps a database that is down from reading as a wrong token.
func TestVerifyDoesNotTurnAnOutageIntoAnIntruder(t *testing.T) {
	boom := errors.New("connection refused")
	repo, agg := channel()
	repo.lookupErr = boom

	err := NewService(repo, agg, slog.Default()).Verify(context.Background(), subscribeMode, "sonia-demo-verify-token")
	if !errors.Is(err, boom) {
		t.Fatalf("Verify returned %v, want the underlying failure", err)
	}
}

// TestReceiveFollowsTheWriteOrderOf07Secuencia protects the order: the event is logged, then the message, then the event closes.
func TestReceiveFollowsTheWriteOrderOf07Secuencia(t *testing.T) {
	repo, agg := channel()

	inbound, err := NewService(repo, agg, slog.Default()).Receive(context.Background(), []byte(metaTextPayload), time.Now().UTC())
	if err != nil {
		t.Fatalf("Receive: got %v, want no error", err)
	}

	if len(inbound) != 1 {
		t.Fatalf("reported %d inbound messages, want 1", len(inbound))
	}
	if len(agg.messages) != 1 || agg.messages[0] != "hola, queria consultar por los precios" {
		t.Fatalf("the message body did not reach conversations/: %v", agg.messages)
	}
	if len(repo.events) != 1 || len(repo.processed) != 1 {
		t.Fatalf("logged %d events and closed %d, want 1 and 1", len(repo.events), len(repo.processed))
	}
	if repo.events[0].EventType != enums.WebhookEventTypeMessage {
		t.Fatalf("event_type %q, want message", repo.events[0].EventType)
	}
	if repo.events[0].SignatureValid {
		t.Fatalf("signature_valid is true, but Fase 1 never checks a signature")
	}
}

// TestReceiveReadsMetaClockAndNotOurs protects C06: sent_at is the channel's clock, and only a broken one falls back to ours.
func TestReceiveReadsMetaClockAndNotOurs(t *testing.T) {
	repo, agg := channel()
	ours := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

	if _, err := NewService(repo, agg, slog.Default()).Receive(context.Background(), []byte(metaTextPayload), ours); err != nil {
		t.Fatalf("Receive: got %v, want no error", err)
	}

	metas := time.Unix(1772841600, 0).UTC()
	message := MetaMessage{Timestamp: "1772841600"}
	if !message.SentAt(ours).Equal(metas) {
		t.Fatalf("SentAt returned %v, want Meta's %v", message.SentAt(ours), metas)
	}
	unparseable := MetaMessage{Timestamp: "ayer"}
	if !unparseable.SentAt(ours).Equal(ours) {
		t.Fatalf("a timestamp we cannot parse lost the message instead of falling back to our clock")
	}
}

// TestReceiveDropsAnUnknownContactInsteadOfInventingOne protects the Fase 4 boundary: automatic contact creation is not this phase.
func TestReceiveDropsAnUnknownContactInsteadOfInventingOne(t *testing.T) {
	repo, agg := channel()
	repo.knownPhone = "5490000000000"

	inbound, err := NewService(repo, agg, slog.Default()).Receive(context.Background(), []byte(metaTextPayload), time.Now().UTC())
	if err != nil {
		t.Fatalf("Receive: got %v, want no error", err)
	}
	if len(inbound) != 0 || len(agg.messages) != 0 {
		t.Fatalf("a message from an unknown number was stored anyway")
	}
	if len(repo.events) != 1 || len(repo.processed) != 1 {
		t.Fatalf("the event was not logged and closed, so F20 lost the trace of a real delivery")
	}
}

// TestReceiveTreatsARedeliveryAsAlreadyDone protects against the retry loop: a 5xx makes Meta send the same message forever.
func TestReceiveTreatsARedeliveryAsAlreadyDone(t *testing.T) {
	repo, agg := channel()
	agg.duplicate = true

	inbound, err := NewService(repo, agg, slog.Default()).Receive(context.Background(), []byte(metaTextPayload), time.Now().UTC())
	if err != nil {
		t.Fatalf("Receive: got %v, want no error: 07-secuencia leans on the wamid to cover the redelivery", err)
	}
	if len(inbound) != 0 {
		t.Fatalf("the turn was started a second time for a message that was already answered")
	}
	if len(repo.events) != 1 || len(repo.processed) != 1 {
		t.Fatalf("the redelivery left no trace in the event log, which F20 asks for")
	}
}

// TestReceiveRejectsWhatItCannotParse keeps a broken body from reaching the aggregate.
func TestReceiveRejectsWhatItCannotParse(t *testing.T) {
	repo, agg := channel()

	_, err := NewService(repo, agg, slog.Default()).Receive(context.Background(), []byte("{not json"), time.Now())
	if !errors.Is(err, ErrMalformedPayload) {
		t.Fatalf("Receive returned %v, want ErrMalformedPayload", err)
	}
	if len(repo.events) != 0 {
		t.Fatalf("an unparseable payload was logged against an inbox it never named")
	}
}

// TestReceiveIgnoresTheOtherWebhooksOfTheSameApp: one app subscribes to several fields and only messages belongs to the turn.
func TestReceiveIgnoresTheOtherWebhooksOfTheSameApp(t *testing.T) {
	repo, agg := channel()
	other := `{"object":"whatsapp_business_account","entry":[{"id":"1","changes":[{"field":"account_update","value":{}}]}]}`

	inbound, err := NewService(repo, agg, slog.Default()).Receive(context.Background(), []byte(other), time.Now())
	if err != nil {
		t.Fatalf("Receive: got %v, want no error", err)
	}
	if len(inbound) != 0 || len(repo.events) != 0 {
		t.Fatalf("a field that is not messages was processed as one")
	}
}

// TestReceiveLogsAStatusWebhookWithoutStoringAMessage: delivery receipts share the field and are not turns.
func TestReceiveLogsAStatusWebhookWithoutStoringAMessage(t *testing.T) {
	repo, agg := channel()
	status := `{"object":"whatsapp_business_account","entry":[{"id":"1","changes":[{"field":"messages","value":{
		"metadata":{"phone_number_id":"000000000000000"},
		"statuses":[{"id":"wamid.HBg","status":"delivered"}]}}]}]}`

	inbound, err := NewService(repo, agg, slog.Default()).Receive(context.Background(), []byte(status), time.Now())
	if err != nil {
		t.Fatalf("Receive: got %v, want no error", err)
	}
	if len(inbound) != 0 || len(agg.messages) != 0 {
		t.Fatalf("a delivery receipt was stored as an incoming message")
	}
	if len(repo.events) != 1 || repo.events[0].EventType != enums.WebhookEventTypeStatus {
		t.Fatalf("the status webhook was not logged as such: %v", repo.events)
	}
}
