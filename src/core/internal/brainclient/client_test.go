package brainclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/httpclient"
)

func request() TurnRequest {
	return TurnRequest{
		MessageBatchID: uuid.New(),
		ConversationID: uuid.New(),
		AIAgentID:      uuid.New(),
		Mode:           enums.AgentModeAutonomous,
	}
}

func TestTurnSendsTheContractOfSectionOne(t *testing.T) {
	var gotPath, gotToken string
	var gotBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotToken = r.URL.Path, r.Header.Get(tokenHeader)
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		_, _ = w.Write([]byte(`{"outcome":"generated","generation_id":null,"completion_text":"hola","transfer":null}`))
	}))
	defer server.Close()

	req := request()
	client := New(Options{BaseURL: server.URL, Token: "secreto"})

	res, err := client.Turn(context.Background(), req)
	if err != nil {
		t.Fatalf("Turn() error = %v, want nil", err)
	}

	if gotPath != turnPath {
		t.Fatalf("path = %q, want %q", gotPath, turnPath)
	}
	// The shared secret is the whole authentication of this hop (ADR-0017 (k)); without it the brain answers 401.
	if gotToken != "secreto" {
		t.Fatalf("%s = %q, want %q", tokenHeader, gotToken, "secreto")
	}
	// Four ids and the mode, and nothing else: the brain forbids extra fields, so an extra one here is a 422 in production.
	want := []string{"message_batch_id", "conversation_id", "ai_agent_id", "mode"}
	if len(gotBody) != len(want) {
		t.Fatalf("body has %d fields (%v), want %d", len(gotBody), gotBody, len(want))
	}
	for _, field := range want {
		if _, ok := gotBody[field]; !ok {
			t.Fatalf("body is missing %q: %v", field, gotBody)
		}
	}
	if gotBody["conversation_id"] != req.ConversationID.String() {
		t.Fatalf("conversation_id = %v, want %v", gotBody["conversation_id"], req.ConversationID)
	}
	if res.Outcome != OutcomeGenerated || res.Text() != "hola" {
		t.Fatalf("response = %+v, want a generated turn saying hola", res)
	}
}

// A null in the contract is an absence and not a zero: an empty uuid would name a generation nobody wrote.
func TestTurnKeepsWhatTheBrainLeftAbsent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"outcome":"failed","generation_id":null,"completion_text":null,"transfer":null}`))
	}))
	defer server.Close()

	res, err := New(Options{BaseURL: server.URL}).Turn(context.Background(), request())
	if err != nil {
		t.Fatalf("Turn() error = %v, want nil", err)
	}

	if res.Outcome != OutcomeFailed {
		t.Fatalf("outcome = %q, want %q", res.Outcome, OutcomeFailed)
	}
	if res.GenerationID != nil || res.CompletionText != nil || res.Transfer != nil {
		t.Fatalf("response = %+v, want every optional field absent", res)
	}
	if res.Text() != "" {
		t.Fatalf("Text() = %q, want empty", res.Text())
	}
}

func TestTurnReadsTheTransferProposal(t *testing.T) {
	signal := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"outcome":"generated","completion_text":"ya te paso con alguien",` +
			`"transfer":{"proposed":true,"signal_evaluation_id":"` + signal.String() + `"}}`))
	}))
	defer server.Close()

	res, err := New(Options{BaseURL: server.URL}).Turn(context.Background(), request())
	if err != nil {
		t.Fatalf("Turn() error = %v, want nil", err)
	}

	if res.Transfer == nil || !res.Transfer.Proposed || res.Transfer.SignalEvaluationID != signal {
		t.Fatalf("transfer = %+v, want the proposal the brain sent", res.Transfer)
	}
}

// The brain's 404 means the four ids name no turn: it is our mistake, and it never gets better on a retry.
func TestTurnTranslatesTheBrains404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"detail":{"code":"turn_not_found"}}`))
	}))
	defer server.Close()

	_, err := New(Options{BaseURL: server.URL}).Turn(context.Background(), request())

	if !errors.Is(err, ErrTurnNotFound) {
		t.Fatalf("error = %v, want ErrTurnNotFound", err)
	}
	if httpclient.Retryable(err) {
		t.Fatal("a turn the brain does not know will not appear on a second call")
	}
}

// Everything that is not a 404 travels up as a StatusError, because only the status says whether asking again is worth it.
func TestTurnPassesTheStatusUpForTheOrchestratorToDecide(t *testing.T) {
	cases := []struct {
		name          string
		status        int
		wantRetryable bool
	}{
		{"a 401 is a secret that does not match, and it will not match on the second try either", http.StatusUnauthorized, false},
		{"a 422 is the contract being violated, which two calls do not fix", http.StatusUnprocessableEntity, false},
		{"a 500 is the brain falling over, which is what the turn's retries are for", http.StatusInternalServerError, true},
		{"a 503 is the brain saying not now", http.StatusServiceUnavailable, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer server.Close()

			_, err := New(Options{BaseURL: server.URL}).Turn(context.Background(), request())

			var status *httpclient.StatusError
			if !errors.As(err, &status) {
				t.Fatalf("error = %v, want a *httpclient.StatusError", err)
			}
			if status.Status != tc.status {
				t.Fatalf("status = %d, want %d", status.Status, tc.status)
			}
			if got := httpclient.Retryable(err); got != tc.wantRetryable {
				t.Fatalf("Retryable() = %v, want %v", got, tc.wantRetryable)
			}
		})
	}
}

// This client never loops: the three retries are the orchestrator's, which is the only one that knows about the transaction.
func TestTurnCallsTheBrainOnce(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, _ = New(Options{BaseURL: server.URL}).Turn(context.Background(), request())

	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}
