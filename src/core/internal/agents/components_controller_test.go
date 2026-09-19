package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// mountComponents builds the real router, because what these cases assert is the roles declared in routes.go.
func mountComponents(role enums.OperatorRole, store *stubComponents) http.Handler {
	return soniahttp.NewRouter(soniahttp.RouterOptions{
		Controllers:   []soniahttp.Controller{NewComponentsController(componentsOver(store), nil)},
		TokenVerifier: func(string) (uuid.UUID, error) { return testOperator, nil },
		ActorLookup: func(context.Context, uuid.UUID) (soniahttp.Actor, error) {
			return soniahttp.Actor{OperatorID: testOperator, AccountID: testAccount, Role: role}, nil
		},
	})
}

const validTone = `{"type":"tone","content":"Formal pero cálido.","position":3}`

// withTone is an agent that already has its tone, which is what the cases about one block start from.
func withTone(t *testing.T) (*stubComponents, AIAgent, Component) {
	t.Helper()
	agent := seeded("Agente")
	store := newStubComponents(agent)
	created, err := componentsOver(store).Create(context.Background(), testAccount, testOperator, agent.ID, tone("Formal."))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return store, agent, created
}

// The Admin of Anexo B "configura los componentes del prompt de los agentes", and nobody else does, reading included (ADR-0039).
func TestOnlyTheAdminConfiguresTheComponents(t *testing.T) {
	for _, role := range enums.OperatorRoleValues {
		t.Run(string(role), func(t *testing.T) {
			store, agent, created := withTone(t)
			base := "/ai-agents/" + agent.ID.String() + "/components"
			one := base + "/" + created.ID.String()

			for _, route := range []struct {
				method, path, body string
				want               int
			}{
				{http.MethodGet, base, "", http.StatusOK},
				{http.MethodPost, base, `{"type":"rules","content":"No tomar pagos.","position":6}`, http.StatusCreated},
				{http.MethodPatch, one, `{"content":"Cálido."}`, http.StatusOK},
				{http.MethodGet, one + "/versions", "", http.StatusOK},
				{http.MethodDelete, one, "", http.StatusNoContent},
			} {
				want := route.want
				if role != enums.OperatorRoleAdmin {
					want = http.StatusForbidden
				}
				if rec := call(mountComponents(role, store), route.method, route.path, route.body); rec.Code != want {
					t.Fatalf("%s %s as %s: got %d, want %d - %s", route.method, route.path, role, rec.Code, want, rec.Body)
				}
			}
		})
	}
}

// The block answers with the version it is at, the one a generation assembled from it names (H-20).
func TestAComponentAnswersWithItsVersion(t *testing.T) {
	agent := seeded("Agente")
	store := newStubComponents(agent)

	rec := call(mountComponents(enums.OperatorRoleAdmin, store), http.MethodPost, "/ai-agents/"+agent.ID.String()+"/components", validTone)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST: got %d - %s", rec.Code, rec.Body)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	for field, want := range map[string]string{"type": `"tone"`, "position": "3", "active": "true", "version": "1"} {
		if string(got[field]) != want {
			t.Fatalf("%s = %s, want %s - %s", field, got[field], want, rec.Body)
		}
	}
}

// Each malformed body is named: the type and the position do not move once written, and a null text is not a text.
func TestTheBodyOfAComponentIsReadFieldByField(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		body   string
		want   string
		status int
	}{
		{"a create without a position", http.MethodPost, `{"type":"rules","content":"x"}`, "invalid_body", http.StatusBadRequest},
		{"a null type", http.MethodPost, `{"type":null,"content":"x","position":6}`, "invalid_body", http.StatusBadRequest},
		{"a position that is not a number", http.MethodPost, `{"type":"rules","content":"x","position":"sexta"}`, "invalid_body", http.StatusBadRequest},
		{"a field a component does not have", http.MethodPost, `{"type":"rules","content":"x","position":6,"active":false}`, "unknown_field", http.StatusBadRequest},
		{"a type F01 does not declare", http.MethodPost, `{"type":"greeting","content":"x","position":6}`, "invalid_component_type", http.StatusBadRequest},
		{"a blank text", http.MethodPost, `{"type":"rules","content":"  ","position":6}`, "invalid_content", http.StatusBadRequest},
		{"a position the column cannot hold", http.MethodPost, `{"type":"rules","content":"x","position":40000}`, "invalid_position", http.StatusBadRequest},
		{"a second tone", http.MethodPost, `{"type":"tone","content":"x","position":6}`, "component_type_taken", http.StatusConflict},
		{"a taken position", http.MethodPost, `{"type":"rules","content":"x","position":3}`, "component_position_taken", http.StatusConflict},
		{"the position does not move after", http.MethodPatch, `{"position":1}`, "unknown_field", http.StatusBadRequest},
		{"the type does not move after", http.MethodPatch, `{"type":"rules"}`, "unknown_field", http.StatusBadRequest},
		{"a null text", http.MethodPatch, `{"content":null}`, "invalid_body", http.StatusBadRequest},
		{"a null switch", http.MethodPatch, `{"active":null}`, "invalid_body", http.StatusBadRequest},
		{"a switch that is not a boolean", http.MethodPatch, `{"active":"no"}`, "invalid_body", http.StatusBadRequest},
		{"a blank text on edit", http.MethodPatch, `{"content":" "}`, "invalid_content", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, agent, created := withTone(t)
			path := "/ai-agents/" + agent.ID.String() + "/components"
			if tc.method == http.MethodPatch {
				path += "/" + created.ID.String()
			}

			rec := call(mountComponents(enums.OperatorRoleAdmin, store), tc.method, path, tc.body)
			if rec.Code != tc.status || codeOf(t, rec) != tc.want {
				t.Fatalf("got %d %q, want %d %q - %s", rec.Code, codeOf(t, rec), tc.status, tc.want, rec.Body)
			}
		})
	}
}

// The history comes newest first with who changed what, and a null note clears it on the next edit.
func TestTheHistoryOfAComponentIsNewestFirst(t *testing.T) {
	store, agent, created := withTone(t)
	engine := mountComponents(enums.OperatorRoleAdmin, store)
	one := "/ai-agents/" + agent.ID.String() + "/components/" + created.ID.String()

	if rec := call(engine, http.MethodPatch, one, `{"content":"Cálido.","change_note":null}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH: got %d - %s", rec.Code, rec.Body)
	}
	rec := call(engine, http.MethodGet, one+"/versions", "")
	var page struct {
		Items []struct {
			Version     int     `json:"version"`
			Content     string  `json:"content"`
			ChangedByID *string `json:"changed_by_operator_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || len(page.Items) != 2 {
		t.Fatalf("want two versions, got %v - %s", err, rec.Body)
	}
	if page.Items[0].Version != 2 || page.Items[0].Content != "Cálido." || page.Items[0].ChangedByID == nil ||
		*page.Items[0].ChangedByID != testOperator.String() {
		t.Fatalf("newest = %+v, want version 2 with the new text by %v", page.Items[0], testOperator)
	}
}

// The history pages by number like contacts/ (ADR-0036, regla 9): total, page and per_page come back, and a bad page is named.
func TestTheHistoryPagesByNumber(t *testing.T) {
	store, agent, created := withTone(t)
	engine := mountComponents(enums.OperatorRoleAdmin, store)
	one := "/ai-agents/" + agent.ID.String() + "/components/" + created.ID.String()
	for _, text := range []string{"Dos.", "Tres."} {
		if rec := call(engine, http.MethodPatch, one, `{"content":"`+text+`"}`); rec.Code != http.StatusOK {
			t.Fatalf("PATCH: got %d - %s", rec.Code, rec.Body)
		}
	}

	rec := call(engine, http.MethodGet, one+"/versions?page=2&per_page=1", "")
	var page struct {
		Items   []struct{ Version int } `json:"items"`
		Total   int                     `json:"total"`
		Page    int                     `json:"page"`
		PerPage int                     `json:"per_page"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decoding: %v - %s", err, rec.Body)
	}
	if len(page.Items) != 1 || page.Items[0].Version != 2 || page.Total != 3 || page.Page != 2 || page.PerPage != 1 {
		t.Fatalf("page 2 of one = %s, want version 2 of 3", rec.Body)
	}

	for _, tc := range []struct{ query, want string }{{"page=0", "invalid_page"}, {"per_page=no", "invalid_per_page"}} {
		if rec := call(engine, http.MethodGet, one+"/versions?"+tc.query, ""); rec.Code != http.StatusBadRequest || codeOf(t, rec) != tc.want {
			t.Fatalf("?%s: got %d %q, want 400 %q", tc.query, rec.Code, codeOf(t, rec), tc.want)
		}
	}
}

// Every id that is not one is a 400 of its own, and a block or an agent out of reach is a 404 of its own.
func TestAComponentOutOfReachIsNamed(t *testing.T) {
	store, agent, _ := withTone(t)
	engine := mountComponents(enums.OperatorRoleAdmin, store)
	base := "/ai-agents/" + agent.ID.String() + "/components"

	for _, tc := range []struct {
		method, path, want string
		status             int
	}{
		{http.MethodGet, "/ai-agents/no-es-un-uuid/components", "invalid_ai_agent_id", http.StatusBadRequest},
		{http.MethodPatch, base + "/no-es-un-uuid", "invalid_prompt_component_id", http.StatusBadRequest},
		{http.MethodDelete, base + "/no-es-un-uuid", "invalid_prompt_component_id", http.StatusBadRequest},
		{http.MethodGet, base + "/no-es-un-uuid/versions", "invalid_prompt_component_id", http.StatusBadRequest},
		{http.MethodGet, "/ai-agents/" + uuid.NewString() + "/components", "ai_agent_not_found", http.StatusNotFound},
		{http.MethodPatch, base + "/" + uuid.NewString(), "prompt_component_not_found", http.StatusNotFound},
		{http.MethodDelete, base + "/" + uuid.NewString(), "prompt_component_not_found", http.StatusNotFound},
		{http.MethodGet, base + "/" + uuid.NewString() + "/versions", "prompt_component_not_found", http.StatusNotFound},
	} {
		body := ""
		if tc.method == http.MethodPatch {
			body = `{"active":false}`
		}
		rec := call(engine, tc.method, tc.path, body)
		if rec.Code != tc.status || codeOf(t, rec) != tc.want {
			t.Fatalf("%s %s: got %d %q, want %d %q", tc.method, tc.path, rec.Code, codeOf(t, rec), tc.status, tc.want)
		}
	}
}
