package agents

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/brainclient"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// stubAssembler plays the brain: it records what it was asked and answers what the case says.
type stubAssembler struct {
	calls []brainclient.PreviewRequest
	res   brainclient.PreviewResponse
	err   error
}

func (s *stubAssembler) Preview(_ context.Context, req brainclient.PreviewRequest) (brainclient.PreviewResponse, error) {
	s.calls = append(s.calls, req)
	return s.res, s.err
}

func previewsOver(store *stubComponents, brain *stubAssembler) *Previews {
	return NewPreviews(PreviewOptions{Repo: store, Brain: brain})
}

func mountPreview(role enums.OperatorRole, store *stubComponents, brain *stubAssembler) http.Handler {
	return soniahttp.NewRouter(soniahttp.RouterOptions{
		Controllers:   []soniahttp.Controller{NewPreviewController(previewsOver(store, brain), nil)},
		TokenVerifier: func(string) (uuid.UUID, error) { return testOperator, nil },
		ActorLookup: func(context.Context, uuid.UUID) (soniahttp.Actor, error) {
			return soniahttp.Actor{OperatorID: testOperator, AccountID: testAccount, Role: role}, nil
		},
	})
}

func toneDraft() Draft {
	return Draft{Type: enums.PromptComponentTypeTone, Content: "Formal.", Position: 3}
}

// The rules of a save (ADR-0039), checked before the brain is asked: a preview never shows a prompt the panel could not save.
func TestADraftThatCouldNotBeSavedIsNotPreviewed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		drafts []Draft
		want   error
	}{
		{"a type F01 does not declare", []Draft{{Type: "greeting", Content: "x", Position: 1}}, ErrUnknownComponentType},
		{"a position below 1", []Draft{{Type: enums.PromptComponentTypeTone, Content: "x", Position: 0}}, ErrInvalidPosition},
		{"a position the column cannot hold", []Draft{{Type: enums.PromptComponentTypeTone, Content: "x", Position: 40000}}, ErrInvalidPosition},
		{"two of one type", []Draft{toneDraft(), {Type: enums.PromptComponentTypeTone, Content: "y", Position: 4}}, ErrDuplicateDraftType},
		{"two at one position", []Draft{toneDraft(), {Type: enums.PromptComponentTypeRules, Content: "y", Position: 3}}, ErrDuplicateDraftPosition},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := seeded("Agente")
			brain := &stubAssembler{}

			_, err := previewsOver(newStubComponents(agent), brain).Assemble(context.Background(), testAccount, agent.ID, tc.drafts)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if len(brain.calls) != 0 {
				t.Fatal("the brain was asked for a prompt that could not be saved")
			}
		})
	}
}

// The drafts reach the brain whole, a blank one included: the brain decides what goes in, with the assembler the turn runs.
func TestTheDraftsReachTheBrainAsTheyAre(t *testing.T) {
	agent := seeded("Agente")
	brain := &stubAssembler{res: brainclient.PreviewResponse{System: "Sos Agente.", RulesVersion: 2,
		Components: []enums.PromptComponentType{enums.PromptComponentTypeTone}}}
	drafts := []Draft{toneDraft(), {Type: enums.PromptComponentTypeRules, Content: "  ", Position: 6}}

	shown, err := previewsOver(newStubComponents(agent), brain).Assemble(context.Background(), testAccount, agent.ID, drafts)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if len(brain.calls) != 1 || brain.calls[0].AIAgentID != agent.ID || len(brain.calls[0].Components) != 2 {
		t.Fatalf("the brain was asked %+v, want the agent and its two drafts", brain.calls)
	}
	if shown.System != "Sos Agente." || shown.RulesVersion != 2 || len(shown.Components) != 1 {
		t.Fatalf("Assemble = %+v, want what the brain answered", shown)
	}
}

// An empty screen is a valid preview, and it travels as a list: a nil slice would reach the brain as null and be refused.
func TestAnEmptyScreenTravelsAsAList(t *testing.T) {
	agent := seeded("Agente")
	brain := &stubAssembler{}

	if _, err := previewsOver(newStubComponents(agent), brain).Assemble(context.Background(), testAccount, agent.ID, nil); err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	raw, _ := json.Marshal(brain.calls[0])
	var sent map[string]json.RawMessage
	_ = json.Unmarshal(raw, &sent)
	if string(sent["components"]) != "[]" {
		t.Fatalf("components travelled as %s, want []", sent["components"])
	}
}

// An agent out of reach is a 404 before the brain is asked, and one removed in between is a 404 from the brain's answer.
func TestAnAIAgentOutOfReachIsNotPreviewed(t *testing.T) {
	agent := seeded("Agente")
	brain := &stubAssembler{}
	if _, err := previewsOver(newStubComponents(agent), brain).Assemble(context.Background(), testAccount, uuid.New(), nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown agent: got %v, want ErrNotFound", err)
	}
	if len(brain.calls) != 0 {
		t.Fatal("the brain was asked about an agent the core could not reach")
	}

	gone := &stubAssembler{err: brainclient.ErrAIAgentNotFound}
	if _, err := previewsOver(newStubComponents(agent), gone).Assemble(context.Background(), testAccount, agent.ID, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an agent removed in between: got %v, want ErrNotFound", err)
	}
}

// The admin alone, like the components; everyone else is refused in the router, before the brain is asked.
func TestOnlyTheAdminPreviewsThePrompt(t *testing.T) {
	for _, role := range enums.OperatorRoleValues {
		t.Run(string(role), func(t *testing.T) {
			agent := seeded("Agente")
			want := http.StatusOK
			if role != enums.OperatorRoleAdmin {
				want = http.StatusForbidden
			}
			rec := call(mountPreview(role, newStubComponents(agent), &stubAssembler{}), http.MethodPost,
				"/ai-agents/"+agent.ID.String()+"/preview", `{"components":[]}`)
			if rec.Code != want {
				t.Fatalf("got %d, want %d - %s", rec.Code, want, rec.Body)
			}
		})
	}
}

// The panel gets the system alone, with the types that went in (ADR-0040).
func TestThePreviewAnswersTheSystemAlone(t *testing.T) {
	agent := seeded("Agente")
	brain := &stubAssembler{res: brainclient.PreviewResponse{System: "Sos Agente.", RulesVersion: 2,
		Components: []enums.PromptComponentType{enums.PromptComponentTypeTone}}}

	rec := call(mountPreview(enums.OperatorRoleAdmin, newStubComponents(agent), brain), http.MethodPost,
		"/ai-agents/"+agent.ID.String()+"/preview", `{"components":[{"type":"tone","content":"Formal.","position":3}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d - %s", rec.Code, rec.Body)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(got) != 3 || string(got["system"]) != `"Sos Agente."` || string(got["rules_version"]) != "2" ||
		string(got["components"]) != `["tone"]` {
		t.Fatalf("body = %s, want the system, its rules version and the tone", rec.Body)
	}
}

// Each malformed body is named, and a brain that cannot answer is a 503 the panel can retry on the next keystroke.
func TestThePreviewNamesWhatItCannotShow(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   string
		brain  error
		want   string
		status int
	}{
		{"no components", `{}`, nil, "invalid_body", http.StatusBadRequest},
		{"components that are not a list", `{"components":"tono"}`, nil, "invalid_body", http.StatusBadRequest},
		{"a field the body does not have", `{"components":[],"message":"hola"}`, nil, "unknown_field", http.StatusBadRequest},
		{"a field a draft does not have", `{"components":[{"type":"tone","content":"x","position":3,"active":true}]}`, nil, "unknown_field", http.StatusBadRequest},
		{"a draft without its position", `{"components":[{"type":"tone","content":"x"}]}`, nil, "invalid_body", http.StatusBadRequest},
		{"a type F01 does not declare", `{"components":[{"type":"greeting","content":"x","position":1}]}`, nil, "invalid_component_type", http.StatusBadRequest},
		{"two of one type", `{"components":[{"type":"tone","content":"x","position":1},{"type":"tone","content":"y","position":2}]}`, nil, "duplicate_component_type", http.StatusBadRequest},
		{"two at one position", `{"components":[{"type":"tone","content":"x","position":1},{"type":"rules","content":"y","position":1}]}`, nil, "duplicate_component_position", http.StatusBadRequest},
		{"the brain is down", `{"components":[]}`, errors.New("connection refused"), "preview_unavailable", http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := seeded("Agente")
			rec := call(mountPreview(enums.OperatorRoleAdmin, newStubComponents(agent), &stubAssembler{err: tc.brain}),
				http.MethodPost, "/ai-agents/"+agent.ID.String()+"/preview", tc.body)
			if rec.Code != tc.status || codeOf(t, rec) != tc.want {
				t.Fatalf("got %d %q, want %d %q - %s", rec.Code, codeOf(t, rec), tc.status, tc.want, rec.Body)
			}
		})
	}
}
