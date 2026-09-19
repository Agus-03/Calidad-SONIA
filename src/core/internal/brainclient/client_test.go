package brainclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

// The list the core validates an agent against comes back whole, signed with the secret like every call of this hop (ADR-0038).
func TestModelsReadsThePricedList(t *testing.T) {
	var gotMethod, gotPath, gotToken string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotToken = r.Method, r.URL.Path, r.Header.Get(tokenHeader)
		_, _ = w.Write([]byte(`{"pricing_version":3,"models":[{"provider":"cohere","model":"command-a-03-2025"}]}`))
	}))
	defer server.Close()

	res, err := New(Options{BaseURL: server.URL, Token: "secreto"}).Models(context.Background())
	if err != nil {
		t.Fatalf("Models() error = %v, want nil", err)
	}
	if gotMethod != http.MethodGet || gotPath != modelsPath || gotToken != "secreto" {
		t.Fatalf("request = %s %s with token %q, want GET %s signed", gotMethod, gotPath, gotToken, modelsPath)
	}
	want := PricedModel{Provider: "cohere", Model: "command-a-03-2025"}
	if res.PricingVersion != 3 || len(res.Models) != 1 || res.Models[0] != want {
		t.Fatalf("Models() = %+v, want version 3 with %+v", res, want)
	}
}

// The drafts travel as the panel sent them, signed, and the system comes back with the types that went in (ADR-0040).
func TestPreviewSendsTheDraftsAndReadsTheSystem(t *testing.T) {
	var gotMethod, gotPath, gotToken string
	var gotBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotToken = r.Method, r.URL.Path, r.Header.Get(tokenHeader)
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"system":"Sos Agente.","rules_version":2,"components":["tone"]}`))
	}))
	defer server.Close()

	agent := uuid.New()
	res, err := New(Options{BaseURL: server.URL, Token: "secreto"}).Preview(context.Background(), PreviewRequest{
		AIAgentID:  agent,
		Components: []DraftComponent{{Type: enums.PromptComponentTypeTone, Content: "Formal.", Position: 3}},
	})
	if err != nil {
		t.Fatalf("Preview() error = %v, want nil", err)
	}
	if gotMethod != http.MethodPost || gotPath != previewPath || gotToken != "secreto" {
		t.Fatalf("request = %s %s with token %q, want POST %s signed", gotMethod, gotPath, gotToken, previewPath)
	}
	drafts, _ := gotBody["components"].([]any)
	if gotBody["ai_agent_id"] != agent.String() || len(drafts) != 1 {
		t.Fatalf("body = %v, want the agent and its one draft", gotBody)
	}
	if res.System != "Sos Agente." || res.RulesVersion != 2 || len(res.Components) != 1 || res.Components[0] != enums.PromptComponentTypeTone {
		t.Fatalf("Preview() = %+v, want the system, version 2 and the tone", res)
	}
}

// The brain's 404 is an agent removed after the core checked it, and it is named so the panel hears a 404 too.
func TestPreviewTranslatesTheBrains404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, err := New(Options{BaseURL: server.URL}).Preview(context.Background(), PreviewRequest{AIAgentID: uuid.New()})
	if !errors.Is(err, ErrAIAgentNotFound) {
		t.Fatalf("error = %v, want ErrAIAgentNotFound", err)
	}
}

// A refusal of the brain travels up without its body: FastAPI echoes the drafts in a 422, and the error ends in the request log.
func TestPreviewKeepsTheDraftsOutOfTheError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"detail":[{"input":"texto secreto del borrador"}]}`))
	}))
	defer server.Close()

	_, err := New(Options{BaseURL: server.URL}).Preview(context.Background(), PreviewRequest{AIAgentID: uuid.New()})
	if err == nil || strings.Contains(err.Error(), "texto secreto") {
		t.Fatalf("error = %v, want the refusal without the drafts it echoed", err)
	}
}

// A brain that is not there at all is a failure too, and it is not mistaken for a 404.
func TestPreviewPassesAnUnreachableBrainUp(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Close()

	_, err := New(Options{BaseURL: server.URL}).Preview(context.Background(), PreviewRequest{AIAgentID: uuid.New()})
	if err == nil || errors.Is(err, ErrAIAgentNotFound) {
		t.Fatalf("error = %v, want the failure to reach the brain", err)
	}
}

func TestPreviewPassesTheFailureUp(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := New(Options{BaseURL: server.URL}).Preview(context.Background(), PreviewRequest{AIAgentID: uuid.New()})
	if err == nil || errors.Is(err, ErrAIAgentNotFound) {
		t.Fatalf("error = %v, want the brain's failure and not a 404", err)
	}
}

// A brain that cannot answer is an error to the caller, which turns it into a 503 and never into an agent saved unchecked.
func TestModelsPassesTheFailureUp(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	if _, err := New(Options{BaseURL: server.URL}).Models(context.Background()); err == nil {
		t.Fatal("Models() error = nil, want the brain's failure")
	}
}
