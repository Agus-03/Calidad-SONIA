package whatsapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// mountSettings builds the real router, because the roles these cases assert are declared in routes.go and exist nowhere else.
func mountSettings(role enums.OperatorRole, settings *Settings) http.Handler {
	return soniahttp.NewRouter(soniahttp.RouterOptions{
		Controllers:   []soniahttp.Controller{NewSettingsController(settings, nil)},
		TokenVerifier: func(string) (uuid.UUID, error) { return settingsOperator, nil },
		ActorLookup: func(context.Context, uuid.UUID) (soniahttp.Actor, error) {
			return soniahttp.Actor{OperatorID: settingsOperator, AccountID: settingsAccount, Role: role}, nil
		},
	})
}

func callSettings(engine http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer cualquier-cosa")

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func settingsCode(t *testing.T, rec *httptest.ResponseRecorder) string {
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

// Which agent answers a number is F17 configuration, so both routes are the admin's, reading included (ADR-0038).
func TestOnlyTheAdminConfiguresTheNumbers(t *testing.T) {
	for _, role := range enums.OperatorRoleValues {
		t.Run(string(role), func(t *testing.T) {
			settings, _, _, inbox, agent := numbersOver(nil)
			engine := mountSettings(role, settings)

			list := callSettings(engine, http.MethodGet, "/inboxes", "")
			patch := callSettings(engine, http.MethodPatch, "/inboxes/"+inbox.ID.String(),
				`{"default_ai_agent_id":"`+agent.String()+`"}`)

			wantList, wantPatch := http.StatusOK, http.StatusOK
			if role != enums.OperatorRoleAdmin {
				wantList, wantPatch = http.StatusForbidden, http.StatusForbidden
			}
			if list.Code != wantList || patch.Code != wantPatch {
				t.Fatalf("as %s: GET %d and PATCH %d, want %d and %d - %s %s",
					role, list.Code, patch.Code, wantList, wantPatch, list.Body, patch.Body)
			}
		})
	}
}

// No secret of the number is a field of the response, so none can reach the panel (ADR-0031, regla 11).
func TestTheNumberIsAnsweredWithoutItsSecrets(t *testing.T) {
	settings, _, _, inbox, _ := numbersOver(nil)

	rec := callSettings(mountSettings(enums.OperatorRoleAdmin, settings), http.MethodGet, "/inboxes", "")
	var got struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || len(got.Items) != 1 {
		t.Fatalf("the listing = %s, want one inbox", rec.Body)
	}
	if got.Items[0]["id"] != inbox.ID.String() || got.Items[0]["default_ai_agent_id"] != nil {
		t.Fatalf("item = %v, want %v with no default", got.Items[0], inbox.ID)
	}
	for _, secret := range []string{"verify_token", "access_token", "app_secret", "access_token_encrypted", "app_secret_encrypted"} {
		if _, ok := got.Items[0][secret]; ok {
			t.Fatalf("the listing carries %q", secret)
		}
	}
}

func TestTheDefaultIsReadFromTheBody(t *testing.T) {
	cases := []struct {
		name     string
		path     func(InboxSettings) string
		body     string
		wantCode int
		want     string
	}{
		{"another field of the inbox is not writable here",
			func(in InboxSettings) string { return in.ID.String() }, `{"display_name":"Otro"}`, http.StatusBadRequest, "unknown_field"},
		{"the field is required, null to leave the number without AI",
			func(in InboxSettings) string { return in.ID.String() }, `{}`, http.StatusBadRequest, "invalid_body"},
		{"the agent is a uuid or null",
			func(in InboxSettings) string { return in.ID.String() }, `{"default_ai_agent_id":7}`, http.StatusBadRequest, "invalid_body"},
		{"a text that is not a uuid",
			func(in InboxSettings) string { return in.ID.String() }, `{"default_ai_agent_id":"ventas"}`, http.StatusBadRequest, "invalid_ai_agent_id"},
		{"an agent that does not exist or was removed (ADR-0038)",
			func(in InboxSettings) string { return in.ID.String() },
			`{"default_ai_agent_id":"` + uuid.NewString() + `"}`, http.StatusBadRequest, "ai_agent_not_assignable"},
		{"an inbox id that is not a uuid",
			func(InboxSettings) string { return "no-es-un-id" }, `{"default_ai_agent_id":null}`, http.StatusBadRequest, "invalid_inbox_id"},
		{"an inbox that is not there",
			func(InboxSettings) string { return uuid.NewString() }, `{"default_ai_agent_id":null}`, http.StatusNotFound, "inbox_not_found"},
		{"a body that is not an object",
			func(in InboxSettings) string { return in.ID.String() }, `[]`, http.StatusBadRequest, "invalid_body"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			settings, _, _, inbox, _ := numbersOver(nil)

			rec := callSettings(mountSettings(enums.OperatorRoleAdmin, settings), http.MethodPatch, "/inboxes/"+tc.path(inbox), tc.body)
			if rec.Code != tc.wantCode || settingsCode(t, rec) != tc.want {
				t.Fatalf("got %d %q, want %d %q - %s", rec.Code, settingsCode(t, rec), tc.wantCode, tc.want, rec.Body)
			}
		})
	}
}

// null is written as null and answered as null: leaving a number without AI is a value and not a missing field.
func TestNullIsAnsweredAsANumberWithoutAI(t *testing.T) {
	previous := uuid.New()
	settings, _, _, inbox, _ := numbersOver(&previous)

	rec := callSettings(mountSettings(enums.OperatorRoleAdmin, settings), http.MethodPatch, "/inboxes/"+inbox.ID.String(),
		`{"default_ai_agent_id":null}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"default_ai_agent_id":null`) {
		t.Fatalf("got %d %s, want 200 with the default null", rec.Code, rec.Body)
	}
}
