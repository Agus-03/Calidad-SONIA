package whatsapp

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

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
			repo, agg, cs := channel()
			err := service(repo, agg, cs, slog.Default()).Verify(context.Background(), tc.mode, tc.token)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Verify returned %v, want %v", err, tc.want)
			}
		})
	}
}

// TestVerifyDoesNotTurnAnOutageIntoAnIntruder keeps a database that is down from reading as a wrong token.
func TestVerifyDoesNotTurnAnOutageIntoAnIntruder(t *testing.T) {
	boom := errors.New("connection refused")
	repo, agg, cs := channel()
	repo.lookupErr = boom

	err := service(repo, agg, cs, slog.Default()).Verify(context.Background(), subscribeMode, "sonia-demo-verify-token")
	if !errors.Is(err, boom) {
		t.Fatalf("Verify returned %v, want the underlying failure", err)
	}
}

// TestReceiveFollowsTheWriteOrderOf07Secuencia protects the order: the event is logged, then the message, then the event closes.
func TestReceiveFollowsTheWriteOrderOf07Secuencia(t *testing.T) {
	repo, agg, cs := channel()

	inbound, err := service(repo, agg, cs, slog.Default()).Receive(context.Background(), []byte(metaTextPayload), sign(metaTextPayload, testAppSecret), time.Now().UTC())
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
	if !repo.events[0].SignatureValid || repo.events[0].Status != enums.WebhookEventStatusReceived {
		t.Fatalf("a signed event was logged as signature_valid=%v status=%q, want true and received",
			repo.events[0].SignatureValid, repo.events[0].Status)
	}
}

// TestAConversationNobodyHoldsGetsItsVendorWhenTheCustomerWrites is ADR-0037 from the channel's side: the one without a vendor is
// handed to inbox/ inside the webhook's transaction, one already held is never moved from here, and a failure to assign fails the
// webhook, so the conversation never commits without the vendor it was owed.
func TestAConversationNobodyHoldsGetsItsVendorWhenTheCustomerWrites(t *testing.T) {
	holder := uuid.New()
	boom := errors.New("connection reset")

	for _, tc := range []struct {
		name       string
		holder     *uuid.UUID
		redelivery bool
		assignErr  error
		wantAsked  int
		wantErr    error
	}{
		{"a conversation nobody holds is handed to the dispatcher", nil, false, nil, 1, nil},
		{"one already held keeps its vendor: the webhook never reassigns", &holder, false, nil, 0, nil},
		{"a redelivery stores no message, so it is not the customer writing and assigns nothing", nil, true, nil, 0, nil},
		{"a failure to assign fails the webhook, and with it the transaction", nil, false, boom, 1, boom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, agg, cs := channel()
			agg.holder = tc.holder
			agg.duplicate = tc.redelivery
			as := &stubAssigner{err: tc.assignErr}

			_, err := serviceWith(repo, agg, cs, as, slog.Default()).
				Receive(context.Background(), []byte(metaTextPayload), sign(metaTextPayload, testAppSecret), time.Now().UTC())
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Receive: got %v, want %v", err, tc.wantErr)
			}
			if len(as.asked) != tc.wantAsked {
				t.Fatalf("the dispatcher was asked %d times, want %d", len(as.asked), tc.wantAsked)
			}
			if tc.wantAsked == 1 && as.asked[0] != agg.conversationID {
				t.Fatalf("the dispatcher was asked about %v, want the conversation the message opened, %v", as.asked[0], agg.conversationID)
			}
		})
	}
}

// TestAConversationIsBornWithWhatTheAIAgentIsInsideTheTransaction is the birth rule of ADR-0038: the default of the inbox is read
// again inside the webhook's transaction, a paused agent is named but born off, a removed one is not named at all, and a failed read
// fails the webhook instead of guessing.
func TestAConversationIsBornWithWhatTheAIAgentIsInsideTheTransaction(t *testing.T) {
	boom := errors.New("connection reset")
	laterAgent := uuid.New()

	for _, tc := range []struct {
		name          string
		paused        bool
		removed       bool
		noDefault     bool
		changedTo     *uuid.UUID
		changed       bool
		birthErr      error
		wantNamed     bool
		wantCanAnswer bool
		wantErr       error
	}{
		{name: "an agent that can answer is born on", wantNamed: true, wantCanAnswer: true},
		{name: "a paused agent is still named, so resuming it lets the vendor turn it on, but it is born off", paused: true, wantNamed: true},
		{name: "an agent removed since the inbox was read is not named at all, as the removal left the inbox", removed: true},
		{name: "an inbox without an agent is born off", noDefault: true},
		{name: "a default cleared after the inbox was read is obeyed, and the conversation is born off", changed: true},
		{name: "a default given after the inbox was read is obeyed", noDefault: true, changed: true, changedTo: &laterAgent,
			wantNamed: true, wantCanAnswer: true},
		{name: "a failed read fails the webhook, and nothing is born on a guess", birthErr: boom, wantErr: boom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, agg, cs := channel()
			repo.aiAgentPaused, repo.aiAgentRemoved, repo.birthErr = tc.paused, tc.removed, tc.birthErr
			if tc.noDefault {
				repo.inbox.DefaultAIAgentID = nil
			}
			repo.defaultChanged, repo.defaultInTx = tc.changed, tc.changedTo

			_, err := service(repo, agg, cs, slog.Default()).
				Receive(context.Background(), []byte(metaTextPayload), sign(metaTextPayload, testAppSecret), time.Now().UTC())
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Receive: got %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if len(agg.messages) != 0 {
					t.Fatalf("a message was stored after the read failed: %v", agg.messages)
				}
				return
			}
			// Once per webhook, even with no default read before: the inbox may have been given one since.
			if repo.birthAsked != 1 {
				t.Fatalf("the default was read %d times inside the transaction, want 1", repo.birthAsked)
			}
			if tc.changedTo != nil && (agg.bornWith == nil || *agg.bornWith != *tc.changedTo) {
				t.Fatalf("born with %v, want the default given since, %v", agg.bornWith, *tc.changedTo)
			}
			if named := agg.bornWith != nil; named != tc.wantNamed || agg.canAnswer != tc.wantCanAnswer {
				t.Fatalf("born naming an agent %v and able to answer %v, want %v and %v", named, agg.canAnswer, tc.wantNamed, tc.wantCanAnswer)
			}
		})
	}
}

// TestReceiveReadsMetaClockAndNotOurs protects C06: sent_at is the channel's clock, and only a broken one falls back to ours.
func TestReceiveReadsMetaClockAndNotOurs(t *testing.T) {
	repo, agg, cs := channel()
	ours := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

	if _, err := service(repo, agg, cs, slog.Default()).Receive(context.Background(), []byte(metaTextPayload), sign(metaTextPayload, testAppSecret), ours); err != nil {
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

// TestReceiveCreatesAContactFromAnUnknownNumber protects the alta automática: F15 needs an origin for every contact (ADR-0001),
// and there is no ABM screen upstream of a webhook to have created the row first.
func TestReceiveCreatesAContactFromAnUnknownNumber(t *testing.T) {
	repo, agg, cs := channel()
	repo.knownPhone = "5490000000000"

	inbound, err := service(repo, agg, cs, slog.Default()).Receive(context.Background(), []byte(metaTextPayload), sign(metaTextPayload, testAppSecret), time.Now().UTC())
	if err != nil {
		t.Fatalf("Receive: got %v, want no error", err)
	}
	if len(inbound) != 1 || len(agg.messages) != 1 {
		t.Fatalf("a message from a first-time number was not answered: %d inbound, %d stored", len(inbound), len(agg.messages))
	}
	if len(cs.created) != 1 {
		t.Fatalf("created %d contacts, want 1", len(cs.created))
	}
	if created := cs.created[0]; created.PhoneWhatsapp != "5493510000000" || created.Name != "Mateo" {
		t.Fatalf("created phone=%q name=%q, want the number and the profile name the message carried", created.PhoneWhatsapp, created.Name)
	}
	if created := cs.created[0]; created.Type != enums.ContactTypeProspect || created.SourceChannel != enums.SourceChannelDirect {
		t.Fatalf("created type=%q source_channel=%q, want prospect and direct (ADR-0034)", created.Type, created.SourceChannel)
	}
	if len(repo.events) != 1 || len(repo.processed) != 1 {
		t.Fatalf("the event was not logged and closed, so F20 lost the trace of a real delivery")
	}
}

// TestReceiveFallsBackToThePhoneWhenWhatsAppSendsNoProfileName keeps the NOT NULL of contacts.name from failing a first message:
// a business-only number often has no profile name to give.
func TestReceiveFallsBackToThePhoneWhenWhatsAppSendsNoProfileName(t *testing.T) {
	repo, agg, cs := channel()
	repo.knownPhone = "5490000000000"
	noProfile := strings.Replace(metaTextPayload, `"contacts": [{"profile": {"name": "Mateo"}, "wa_id": "5493510000000"}],`, "", 1)

	if _, err := service(repo, agg, cs, slog.Default()).Receive(context.Background(), []byte(noProfile), sign(noProfile, testAppSecret), time.Now().UTC()); err != nil {
		t.Fatalf("Receive: got %v, want no error", err)
	}
	if len(cs.created) != 1 || cs.created[0].Name != "5493510000000" {
		t.Fatalf("created name %q, want the phone number as the fallback", cs.created[0].Name)
	}
}

// TestReceiveReadsTheWinnerWhenTwoDeliveriesRaceToCreateTheSameContact protects the ON CONFLICT DO NOTHING race of
// contacts/: the loser of the insert must read the row the winner just committed, not fail the turn over it.
func TestReceiveReadsTheWinnerWhenTwoDeliveriesRaceToCreateTheSameContact(t *testing.T) {
	repo, agg, cs := channel()
	repo.knownPhone = "5490000000000"
	cs.dupPhone = "5493510000000"
	cs.onDuplicate = func() {
		repo.knownPhone = "5493510000000"
		repo.contactID = uuid.New()
	}

	inbound, err := service(repo, agg, cs, slog.Default()).Receive(context.Background(), []byte(metaTextPayload), sign(metaTextPayload, testAppSecret), time.Now().UTC())
	if err != nil {
		t.Fatalf("Receive: got %v, want no error: a duplicate on insert must read the winner, not fail the turn", err)
	}
	if len(inbound) != 1 {
		t.Fatalf("a message was lost when its contact lost the race to be created")
	}
}

// TestReceiveTreatsARedeliveryAsAlreadyDone protects against the retry loop: a 5xx makes Meta send the same message forever.
func TestReceiveTreatsARedeliveryAsAlreadyDone(t *testing.T) {
	repo, agg, cs := channel()
	agg.duplicate = true

	inbound, err := service(repo, agg, cs, slog.Default()).Receive(context.Background(), []byte(metaTextPayload), sign(metaTextPayload, testAppSecret), time.Now().UTC())
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
	repo, agg, cs := channel()

	_, err := service(repo, agg, cs, slog.Default()).Receive(context.Background(), []byte("{not json"), sign("{not json", testAppSecret), time.Now())
	if !errors.Is(err, ErrMalformedPayload) {
		t.Fatalf("Receive returned %v, want ErrMalformedPayload", err)
	}
	if len(repo.events) != 0 {
		t.Fatalf("an unparseable payload was logged against an inbox it never named")
	}
}

// TestReceiveIgnoresTheOtherWebhooksOfTheSameApp: one app subscribes to several fields and only messages belongs to the turn.
func TestReceiveIgnoresTheOtherWebhooksOfTheSameApp(t *testing.T) {
	repo, agg, cs := channel()
	other := `{"object":"whatsapp_business_account","entry":[{"id":"1","changes":[{"field":"account_update","value":{}}]}]}`

	inbound, err := service(repo, agg, cs, slog.Default()).Receive(context.Background(), []byte(other), sign(other, testAppSecret), time.Now())
	if err != nil {
		t.Fatalf("Receive: got %v, want no error", err)
	}
	if len(inbound) != 0 || len(repo.events) != 0 {
		t.Fatalf("a field that is not messages was processed as one")
	}
}

// TestReceiveLogsAStatusWebhookWithoutStoringAMessage: delivery receipts share the field and are not turns.
func TestReceiveLogsAStatusWebhookWithoutStoringAMessage(t *testing.T) {
	repo, agg, cs := channel()
	status := `{"object":"whatsapp_business_account","entry":[{"id":"1","changes":[{"field":"messages","value":{
		"metadata":{"phone_number_id":"000000000000000"},
		"statuses":[{"id":"wamid.HBg","status":"delivered"}]}}]}]}`

	inbound, err := service(repo, agg, cs, slog.Default()).Receive(context.Background(), []byte(status), sign(status, testAppSecret), time.Now())
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

// TestSignedByMatchesADigestComputedOutsideGo pins the check to a digest Python's hmac produced, so the helper the other cases
// sign with cannot drift from Meta together with the code it is testing.
func TestSignedByMatchesADigestComputedOutsideGo(t *testing.T) {
	body := []byte(`{"object":"whatsapp_business_account","entry":[]}`)
	header := "sha256=5742a79ea7ae8db073bba646acac861bb3d5b8654deead703685d71240aff9c8"

	if !signedBy(body, header, testAppSecret) {
		t.Fatalf("the digest computed outside Go did not validate: the HMAC is not the one Meta sends")
	}
	if sign(string(body), testAppSecret) != header {
		t.Fatalf("the test helper signs %q, want %q", sign(string(body), testAppSecret), header)
	}
}

// TestReceiveKeepsWhatNobodySignedAndStartsNothing protects F20: a forgery is logged as discarded, answered like anything else,
// and never reaches conversations/ nor the turn.
func TestReceiveKeepsWhatNobodySignedAndStartsNothing(t *testing.T) {
	tampered := strings.Replace(metaTextPayload, "precios", "precios y el CBU", 1)

	cases := []struct {
		name      string
		body      string
		header    string
		appSecret string
	}{
		{"a signature under another key is a forgery", metaTextPayload, sign(metaTextPayload, "otra-app"), testAppSecret},
		{"a body changed after it was signed no longer matches", tampered, sign(metaTextPayload, testAppSecret), testAppSecret},
		{"no header is no signature", metaTextPayload, "", testAppSecret},
		{"a digest without the algorithm in front is not one this code checks", metaTextPayload,
			strings.TrimPrefix(sign(metaTextPayload, testAppSecret), signaturePrefix), testAppSecret},
		{"a digest that is not hex is not a signature", metaTextPayload, signaturePrefix + "no-es-hex", testAppSecret},
		{"a number with no secret authenticates nothing, not even an HMAC keyed with nothing", metaTextPayload,
			sign(metaTextPayload, ""), ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, agg, cs := channel()
			repo.inbox.AppSecret = tc.appSecret

			inbound, err := service(repo, agg, cs, slog.Default()).Receive(context.Background(), []byte(tc.body), tc.header, time.Now())
			if err != nil {
				t.Fatalf("Receive: got %v, want no error: an error is a non-200, and Meta would retry the forgery", err)
			}
			if len(inbound) != 0 || len(agg.messages) != 0 {
				t.Fatalf("a payload nobody signed was stored and handed to the turn")
			}
			if len(repo.events) != 1 {
				t.Fatalf("logged %d events, want 1: the attempt must leave its trace", len(repo.events))
			}
			if got := repo.events[0]; got.SignatureValid || got.Status != enums.WebhookEventStatusDiscarded {
				t.Fatalf("logged signature_valid=%v status=%q, want false and discarded", got.SignatureValid, got.Status)
			}
			if len(repo.processed) != 0 {
				t.Fatalf("a discarded event was then marked processed, which reads as a delivery that happened")
			}
		})
	}
}
