package reasoning

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

func mount(role enums.OperatorRole, d *desk) http.Handler {
	return soniahttp.NewRouter(soniahttp.RouterOptions{
		Controllers:   []soniahttp.Controller{NewController(d.service, nil)},
		TokenVerifier: func(string) (uuid.UUID, error) { return testOperator, nil },
		ActorLookup: func(context.Context, uuid.UUID) (soniahttp.Actor, error) {
			return soniahttp.Actor{OperatorID: testOperator, AccountID: testAccount, Role: role}, nil
		},
	})
}

func getReasoning(engine http.Handler, conversationID, generationID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+conversationID+"/generations/"+generationID+"/components", nil)
	req.Header.Set("Authorization", "Bearer cualquier-cosa")

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct{ Code string } `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the error is not json: %v - %s", err, rec.Body)
	}
	return body.Error.Code
}

// ADR-0049 in the body: for the operator the key content is absent, not null, so the answer does not even say there is a text.
func TestTheReasoningHandlerLeavesTheTextOutForTheOperator(t *testing.T) {
	for _, tc := range []struct {
		role     enums.OperatorRole
		withText bool
	}{
		{enums.OperatorRoleOperator, false},
		{enums.OperatorRoleAdmin, true},
	} {
		t.Run(string(tc.role), func(t *testing.T) {
			d := newDesk()
			conversationID := d.open(&testOperator)
			generationID := d.assembled(conversationID)

			rec := getReasoning(mount(tc.role, d), conversationID.String(), generationID.String())
			if rec.Code != http.StatusOK {
				t.Fatalf("GET components: got %d - %s, want 200", rec.Code, rec.Body)
			}
			var body struct {
				GenerationID string           `json:"generation_id"`
				Components   []map[string]any `json:"components"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("the body is not json: %v", err)
			}
			if body.GenerationID != generationID.String() || len(body.Components) != 2 || body.Components[0]["type"] != "objective" {
				t.Fatalf("body = %s, want the two components of this generation in order", rec.Body)
			}
			if _, has := body.Components[0]["content"]; has != tc.withText {
				t.Fatalf("content present = %v, want %v: %s", has, tc.withText, rec.Body)
			}
		})
	}
}

// The two ids are parsed before anything is read, and each refusal answers the code the inbox answered before (ADR-0063).
func TestTheReasoningHandlerHasItsOwnRefusals(t *testing.T) {
	d := newDesk()
	conversationID := d.open(&testOperator)
	theirs := d.open(new(uuid.UUID))
	engine := mount(enums.OperatorRoleOperator, d)

	if rec := getReasoning(engine, "no-es-un-id", id.New().String()); errorCode(t, rec) != "invalid_conversation_id" {
		t.Fatalf("a bad conversation id: got %d - %s", rec.Code, rec.Body)
	}
	if rec := getReasoning(engine, conversationID.String(), "no-es-un-id"); errorCode(t, rec) != "invalid_generation_id" {
		t.Fatalf("a bad generation id: got %d - %s", rec.Code, rec.Body)
	}
	rec := getReasoning(engine, conversationID.String(), id.New().String())
	if rec.Code != http.StatusNotFound || errorCode(t, rec) != "generation_not_found" {
		t.Fatalf("a generation that is not of this conversation: got %d - %s, want 404 generation_not_found", rec.Code, rec.Body)
	}
	rec = getReasoning(engine, theirs.String(), id.New().String())
	if rec.Code != http.StatusNotFound || errorCode(t, rec) != "conversation_not_found" {
		t.Fatalf("a conversation out of reach: got %d - %s, want 404 conversation_not_found", rec.Code, rec.Body)
	}
}
