package whatsapp

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// recordingStarter proves the handoff happened without pretending to be turn/, which does not exist yet.
type recordingStarter struct {
	mu      sync.Mutex
	started []uuid.UUID
	done    chan struct{}
}

func (r *recordingStarter) Start(_ context.Context, conversationID, _ uuid.UUID, _ *uuid.UUID) {
	r.mu.Lock()
	r.started = append(r.started, conversationID)
	r.mu.Unlock()
	close(r.done)
}

func mount(starter TurnStarter) (*stubChannel, *stubAggregate, http.Handler) {
	repo, agg, cs := channel()
	controller := NewController(service(repo, agg, cs, slog.Default()), starter, slog.Default())
	return repo, agg, soniahttp.NewRouter(soniahttp.RouterOptions{
		PublicControllers: []soniahttp.Controller{controller},
	})
}

// webhook builds the POST Meta sends; an empty signature leaves the header out, which is what a caller that is not Meta does.
func webhook(body, signature string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, routeBase, strings.NewReader(body))
	if signature != "" {
		req.Header.Set(signatureHeader, signature)
	}
	return req
}

// TestVerifyAnswersMetaWithoutCredentials mounts the real router: the point of the public zone is that no operator header exists.
func TestVerifyAnswersMetaWithoutCredentials(t *testing.T) {
	_, _, engine := mount(nil)

	cases := []struct {
		name       string
		query      string
		wantStatus int
		wantBody   string
	}{
		{"the challenge comes back verbatim, with no JSON around it", "?hub.mode=subscribe&hub.verify_token=sonia-demo-verify-token&hub.challenge=1158201444", http.StatusOK, "1158201444"},
		{"a wrong token is forbidden and echoes nothing", "?hub.mode=subscribe&hub.verify_token=guessed&hub.challenge=1158201444", http.StatusForbidden, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, routeBase+tc.query, nil))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d", rec.Code, tc.wantStatus)
			}
			if tc.wantBody != "" && rec.Body.String() != tc.wantBody {
				t.Fatalf("body %q, want %q", rec.Body.String(), tc.wantBody)
			}
		})
	}
}

// TestReceiveAnswersMetaAndThenStartsTheTurn protects the rule that Meta is not kept waiting for Cohere (07-secuencia).
func TestReceiveAnswersMetaAndThenStartsTheTurn(t *testing.T) {
	starter := &recordingStarter{done: make(chan struct{})}
	_, agg, engine := mount(starter)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, webhook(metaTextPayload, sign(metaTextPayload, testAppSecret)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: Meta retries anything else", rec.Code)
	}
	if len(agg.messages) != 1 {
		t.Fatalf("the message was not stored before the answer went out")
	}

	// The turn is handed off outside the request, so the wait is on the handoff and not on the response.
	<-starter.done
	starter.mu.Lock()
	defer starter.mu.Unlock()
	if len(starter.started) != 1 || starter.started[0] != agg.conversationID {
		t.Fatalf("the turn was started for %v, want the conversation just written", starter.started)
	}
}

// TestReceiveWithoutATurnStillStores protects this phase's own state: with turn/ absent the message must not be lost.
func TestReceiveWithoutATurnStillStores(t *testing.T) {
	_, agg, engine := mount(nil)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, webhook(metaTextPayload, sign(metaTextPayload, testAppSecret)))

	if rec.Code != http.StatusOK || len(agg.messages) != 1 {
		t.Fatalf("status %d and %d messages, want 200 and 1", rec.Code, len(agg.messages))
	}
}

// TestReceiveAnswersAForgeryWithA200AndStartsNoTurn protects the decision of F20 through the real router: the forgery gets the
// same answer as a delivery, so whoever sent it learns nothing, and the turn never hears of it.
func TestReceiveAnswersAForgeryWithA200AndStartsNoTurn(t *testing.T) {
	starter := &recordingStarter{done: make(chan struct{})}
	repo, agg, engine := mount(starter)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, webhook(metaTextPayload, sign(metaTextPayload, "no-soy-meta")))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: anything else tells the sender the signature is what failed", rec.Code)
	}
	if len(agg.messages) != 0 {
		t.Fatalf("a forged message reached conversations/")
	}
	if len(repo.events) != 1 || repo.events[0].SignatureValid {
		t.Fatalf("the forgery left no discarded trace: %v", repo.events)
	}

	// Start is called inside the handler, so by the time ServeHTTP returns a turn would already have been handed off.
	starter.mu.Lock()
	defer starter.mu.Unlock()
	if len(starter.started) != 0 {
		t.Fatalf("a turn was started for a payload nobody signed")
	}
}

// TestReceiveMapsItsFailuresToWhatMetaShouldDo: a 4xx stops the retries and a 5xx asks for them.
func TestReceiveMapsItsFailuresToWhatMetaShouldDo(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{"a body that is not a webhook will not parse on the retry either", "{not json", http.StatusBadRequest, "malformed_payload"},
		{"a number we do not serve is not a transient failure", `{"object":"x","entry":[{"id":"1","changes":[{"field":"messages","value":{"metadata":{"phone_number_id":"999"}}}]}]}`, http.StatusNotFound, "inbox_not_found"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, engine := mount(nil)

			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, routeBase, strings.NewReader(tc.body)))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d", rec.Code, tc.wantStatus)
			}
			if !strings.Contains(rec.Body.String(), tc.wantCode) {
				t.Fatalf("body %q does not carry the code %q", rec.Body.String(), tc.wantCode)
			}
		})
	}
}
