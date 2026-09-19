package agents

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// mount builds the real router, because what these cases assert is the roles declared in routes.go and those only exist there.
func mount(role enums.OperatorRole, repo *stubRepo, catalogue *stubCatalogue) http.Handler {
	return soniahttp.NewRouter(soniahttp.RouterOptions{
		Controllers:   []soniahttp.Controller{NewController(serviceOver(repo, &stubInboxes{}, catalogue), nil)},
		TokenVerifier: func(string) (uuid.UUID, error) { return testOperator, nil },
		ActorLookup: func(context.Context, uuid.UUID) (soniahttp.Actor, error) {
			return soniahttp.Actor{OperatorID: testOperator, AccountID: testAccount, Role: role}, nil
		},
	})
}

func call(engine http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer cualquier-cosa")

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func codeOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the error body is not json: %v - %s", err, rec.Body)
	}
	return body.Error.Code
}

const validAIAgent = `{"name":"Agente","llm_provider":"cohere","llm_model":"command-a-03-2025"}`

// TestOnlyTheAdminManagesAIAgents is F17 read with Anexo B: the whole management is the admin panel's, reading included (ADR-0038).
func TestOnlyTheAdminManagesAIAgents(t *testing.T) {
	routes := []struct {
		method string
		path   string
		body   string
		want   int
	}{
		{http.MethodGet, "/ai-agents", "", http.StatusOK},
		{http.MethodPost, "/ai-agents", validAIAgent, http.StatusCreated},
		{http.MethodGet, "/ai-agents/", "", http.StatusOK},
		{http.MethodPatch, "/ai-agents/", `{"name":"Otro"}`, http.StatusOK},
		{http.MethodDelete, "/ai-agents/", "", http.StatusNoContent},
	}

	for _, role := range enums.OperatorRoleValues {
		t.Run(string(role), func(t *testing.T) {
			for _, r := range routes {
				existing := seeded("Agente")
				path := r.path
				if strings.HasSuffix(path, "/") {
					path += existing.ID.String()
				}

				rec := call(mount(role, newStubRepo(existing), priced()), r.method, path, r.body)

				want := r.want
				if role != enums.OperatorRoleAdmin {
					want = http.StatusForbidden
				}
				if rec.Code != want {
					t.Fatalf("%s %s as %s: got %d, want %d - body %s", r.method, path, role, rec.Code, want, rec.Body)
				}
			}
		})
	}
}

// The response carries what the administrator edits and nothing that has no effect yet: no default_mode, no extraction_enabled.
func TestTheAIAgentIsAnsweredWithWhatCanBeEdited(t *testing.T) {
	rec := call(mount(enums.OperatorRoleAdmin, newStubRepo(), priced()), http.MethodPost, "/ai-agents",
		`{"name":"Agente","description":"Ventas","llm_provider":"cohere","llm_model":"command-a-03-2025","temperature":0.3,"max_tokens":512}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST: got %d, want 201 - body %s", rec.Code, rec.Body)
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("the created agent is not json: %v", err)
	}
	for _, field := range []string{"id", "name", "description", "llm_provider", "llm_model", "temperature", "max_tokens", "active", "created_at", "updated_at"} {
		if _, ok := got[field]; !ok {
			t.Fatalf("the response has no %q: %v", field, got)
		}
	}
	for _, hidden := range []string{"default_mode", "extraction_enabled", "account_id"} {
		if _, ok := got[hidden]; ok {
			t.Fatalf("the response carries %q, which the API does not offer: %v", hidden, got)
		}
	}
	if got["temperature"] != 0.3 || got["max_tokens"] != float64(512) || got["active"] != true {
		t.Fatalf("response = %v, want temperature 0.3, max_tokens 512 and born active", got)
	}
}

// What the body gets wrong is told apart by code, so the panel can point at the field.
func TestTheBodyIsReadFieldByField(t *testing.T) {
	cases := []struct {
		name   string
		method string
		body   string
		want   string
	}{
		{"a field of Fase 9 is refused and not saved without effect", http.MethodPost,
			`{"name":"A","llm_provider":"cohere","llm_model":"command-a-03-2025","default_mode":"off"}`, "unknown_field"},
		{"active is not writable on create: an agent is born active", http.MethodPost,
			`{"name":"A","llm_provider":"cohere","llm_model":"command-a-03-2025","active":false}`, "unknown_field"},
		{"a create without a model has nothing to answer with", http.MethodPost, `{"name":"A"}`, "invalid_body"},
		{"a body that is not an object", http.MethodPost, `["A"]`, "invalid_body"},
		{"a name has to be text", http.MethodPost,
			`{"name":3,"llm_provider":"cohere","llm_model":"command-a-03-2025"}`, "invalid_body"},
		{"a null name is not a value the row can hold", http.MethodPatch, `{"name":null}`, "invalid_body"},
		{"active has to be true or false", http.MethodPatch, `{"active":null}`, "invalid_body"},
		{"a temperature has to be a number", http.MethodPatch, `{"temperature":"alta"}`, "invalid_body"},
		{"a field an agent does not have", http.MethodPatch, `{"extraction_enabled":true}`, "unknown_field"},
		{"a blank name", http.MethodPatch, `{"name":"  "}`, "invalid_name"},
		{"a negative temperature", http.MethodPatch, `{"temperature":-1}`, "invalid_temperature"},
		{"zero tokens", http.MethodPatch, `{"max_tokens":0}`, "invalid_max_tokens"},
		{"more tokens than the column holds", http.MethodPatch, `{"max_tokens":2147483648}`, "invalid_max_tokens"},
		{"a model the brain cannot price", http.MethodPatch, `{"llm_model":"command-r-plus"}`, "unpriced_model"},
		{"an empty model", http.MethodPatch, `{"llm_model":""}`, "invalid_model"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			existing := seeded("Agente")
			path := "/ai-agents"
			if tc.method == http.MethodPatch {
				path += "/" + existing.ID.String()
			}

			rec := call(mount(enums.OperatorRoleAdmin, newStubRepo(existing), priced()), tc.method, path, tc.body)
			if rec.Code != http.StatusBadRequest || codeOf(t, rec) != tc.want {
				t.Fatalf("got %d %q, want 400 %q - body %s", rec.Code, codeOf(t, rec), tc.want, rec.Body)
			}
		})
	}
}

// null clears the three nullable columns, and a field nobody sent keeps its value: that is the whole point of reading raw keys.
func TestAPatchClearsWhatWasSentAsNullAndKeepsWhatWasNotSent(t *testing.T) {
	existing := seeded("Agente")
	existing.Description, existing.Temperature, existing.MaxTokens = ptr("Ventas"), ptr(0.3), ptr(512)
	repo := newStubRepo(existing)

	rec := call(mount(enums.OperatorRoleAdmin, repo, priced()), http.MethodPatch, "/ai-agents/"+existing.ID.String(),
		`{"description":null,"temperature":null}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH: got %d, want 200 - body %s", rec.Code, rec.Body)
	}

	got := repo.rows[0]
	if got.Description != nil || got.Temperature != nil {
		t.Fatalf("description %v and temperature %v, want both cleared", got.Description, got.Temperature)
	}
	if got.MaxTokens == nil || *got.MaxTokens != 512 || got.Name != "Agente" {
		t.Fatalf("max_tokens %v and name %q, want the values nobody sent kept", got.MaxTokens, got.Name)
	}
}

// The brain being down is a 503 the panel can retry, never a 500 and never an agent saved unchecked.
func TestTheBrainBeingDownIsAnswered503(t *testing.T) {
	catalogue := &stubCatalogue{err: errors.New("brain: unreachable")}

	rec := call(mount(enums.OperatorRoleAdmin, newStubRepo(), catalogue), http.MethodPost, "/ai-agents", validAIAgent)
	if rec.Code != http.StatusServiceUnavailable || codeOf(t, rec) != "models_unavailable" {
		t.Fatalf("got %d %q, want 503 models_unavailable - body %s", rec.Code, codeOf(t, rec), rec.Body)
	}
}

// A save that lost the race to another is a 409 the panel answers by reloading, and not a 400 about a model that may be priced.
func TestASaveThatLostTheRaceIsAConflict(t *testing.T) {
	existing := seeded("Agente")
	repo := newStubRepo(existing)
	repo.beforeLock = func(s *stubRepo) { s.rows[0].LLMModel = "claude-sonnet-4-5" }

	rec := call(mount(enums.OperatorRoleAdmin, repo, priced()), http.MethodPatch, "/ai-agents/"+existing.ID.String(),
		`{"name":"Otro","llm_model":"command-a-03-2025"}`)
	if rec.Code != http.StatusConflict || codeOf(t, rec) != "ai_agent_changed" {
		t.Fatalf("got %d %q, want 409 ai_agent_changed - body %s", rec.Code, codeOf(t, rec), rec.Body)
	}
}

func TestAnAIAgentIDThatIsNotAUUIDIsABadRequest(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
		rec := call(mount(enums.OperatorRoleAdmin, newStubRepo(), priced()), method, "/ai-agents/no-es-un-uuid", `{}`)
		if rec.Code != http.StatusBadRequest || codeOf(t, rec) != "invalid_ai_agent_id" {
			t.Fatalf("%s: got %d %q, want 400 invalid_ai_agent_id", method, rec.Code, codeOf(t, rec))
		}
	}
}

func TestAMissingAIAgentIsA404(t *testing.T) {
	missing := "/ai-agents/" + seeded("Nadie").ID.String()
	for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
		rec := call(mount(enums.OperatorRoleAdmin, newStubRepo(), priced()), method, missing, `{"name":"Otro"}`)
		if rec.Code != http.StatusNotFound || codeOf(t, rec) != "ai_agent_not_found" {
			t.Fatalf("%s: got %d %q, want 404 ai_agent_not_found", method, rec.Code, codeOf(t, rec))
		}
	}
}

// The listing is the account's agents and nobody else's, as a list and never a null.
func TestTheListingIsTheAccountsAIAgents(t *testing.T) {
	mine, foreign := seeded("Mio"), seeded("Ajeno")
	foreign.AccountID = uuid.New()

	rec := call(mount(enums.OperatorRoleAdmin, newStubRepo(mine, foreign), priced()), http.MethodGet, "/ai-agents", "")
	var got listResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("the listing is not json: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].ID != mine.ID.String() {
		t.Fatalf("items = %+v, want only %v", got.Items, mine.ID)
	}

	rec = call(mount(enums.OperatorRoleAdmin, newStubRepo(), priced()), http.MethodGet, "/ai-agents", "")
	if !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("an empty listing = %s, want an empty list and not null", rec.Body)
	}
}
