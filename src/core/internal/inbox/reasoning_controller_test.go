package inbox

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

func getReasoning(engine http.Handler, conversationID, generationID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+conversationID+"/generations/"+generationID+"/components", nil)
	req.Header.Set("Authorization", "Bearer cualquier-cosa")

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
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
			d := newDesk(&stubStore{})
			target := d.open(&testOperator)
			generationID := assembled(d, target.ConversationID)

			rec := getReasoning(mountDesk(tc.role, d), target.ConversationID.String(), generationID.String())
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

// The two ids are parsed before anything is read, and a generation of another conversation is a 404 with a code of its own.
func TestTheReasoningHandlerHasItsOwnRefusals(t *testing.T) {
	d := newDesk(&stubStore{})
	target := d.open(&testOperator)
	engine := mountDesk(enums.OperatorRoleOperator, d)

	if rec := getReasoning(engine, "no-es-un-id", id.New().String()); errorCode(t, rec) != "invalid_conversation_id" {
		t.Fatalf("a bad conversation id: got %d - %s", rec.Code, rec.Body)
	}
	if rec := getReasoning(engine, target.ConversationID.String(), "no-es-un-id"); errorCode(t, rec) != "invalid_generation_id" {
		t.Fatalf("a bad generation id: got %d - %s", rec.Code, rec.Body)
	}
	rec := getReasoning(engine, target.ConversationID.String(), id.New().String())
	if rec.Code != http.StatusNotFound || errorCode(t, rec) != "generation_not_found" {
		t.Fatalf("a generation that is not of this conversation: got %d - %s, want 404 generation_not_found", rec.Code, rec.Body)
	}
}

// The thread says which messages have a generation behind them, which is how the frontend knows what F05 can be asked about.
func TestTheThreadNamesTheGenerationBehindAMessage(t *testing.T) {
	d := newDesk(&stubStore{})
	target := d.open(&testOperator)
	generationID := id.New()
	d.store.threads = map[uuid.UUID][]ThreadMessage{target.ConversationID: {
		{Message: Message{ID: id.New(), Direction: enums.MessageDirectionOutbound, SenderType: enums.SenderTypeAiAgent}, GenerationID: &generationID},
		{Message: Message{ID: id.New(), Direction: enums.MessageDirectionInbound, SenderType: enums.SenderTypeContact}},
	}}

	rec := getThread(mountDesk(enums.OperatorRoleOperator, d), target.ConversationID.String(), url.Values{})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"generation_id":"`+generationID.String()+`"`) ||
		!strings.Contains(rec.Body.String(), `"generation_id":null`) {
		t.Fatalf("GET messages: got %d - %s, want the generation of the answer and null for the inbound", rec.Code, rec.Body)
	}
}
