package reminders

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

var (
	testAccount  = id.New()
	testOperator = id.New()
)

// mount builds the real router, because the paths and the roles only exist in routes.go.
func mount(role enums.OperatorRole, s *Service) http.Handler {
	return soniahttp.NewRouter(soniahttp.RouterOptions{
		Controllers:   []soniahttp.Controller{NewController(s, nil)},
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

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the error is not json: %v - %s", err, rec.Body)
	}
	return body.Error.Code
}

// TestTheReminderRoutesAnswerWithTheirCodes walks the five routes, because a wrong path or method compiles (ADR-0058).
func TestTheReminderRoutesAnswerWithTheirCodes(t *testing.T) {
	contact, conversation := id.New(), id.New()
	store := newStore()
	store.conversations[conversation] = contact
	engine := mount(enums.OperatorRoleOperator, newService(store, contact))
	later := clock.Add(time.Hour).Format(time.RFC3339)

	rec := call(engine, http.MethodPost, "/contacts/"+contact.String()+"/reminders",
		`{"description":"llamar","due_at":"`+later+`","conversation_id":"`+conversation.String()+`"}`)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"conversation_id":"`+conversation.String()) {
		t.Fatalf("POST: got %d - body %s, want 201 with the conversation", rec.Code, rec.Body)
	}
	var created reminderResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("the reminder is not json: %v", err)
	}

	for _, tc := range []struct {
		method, path, body string
		want               int
		code               string
	}{
		{http.MethodGet, "/contacts/" + contact.String() + "/reminders", "", http.StatusOK, ""},
		{http.MethodGet, "/reminders", "", http.StatusOK, ""},
		{http.MethodGet, "/reminders?due=true", "", http.StatusOK, ""},
		{http.MethodGet, "/reminders?due=false", "", http.StatusBadRequest, "invalid_filter"},
		{http.MethodPost, "/contacts/x/reminders", `{"description":"a","due_at":"` + later + `"}`, http.StatusBadRequest, "invalid_contact_id"},
		{http.MethodPost, "/contacts/" + contact.String() + "/reminders", `{"description":"a"}`, http.StatusBadRequest, "invalid_body"},
		{http.MethodPost, "/contacts/" + contact.String() + "/reminders", `{"description":"a","due_at":"mañana"}`, http.StatusBadRequest, "invalid_due_at"},
		{http.MethodPost, "/contacts/" + contact.String() + "/reminders", `{"description":"a","due_at":"2000-01-01T00:00:00Z"}`, http.StatusBadRequest, "due_at_in_the_past"},
		{http.MethodPost, "/contacts/" + contact.String() + "/reminders", `{"description":"","due_at":"` + later + `"}`, http.StatusBadRequest, "invalid_description"},
		{http.MethodPost, "/contacts/" + contact.String() + "/reminders", `{"description":"a","due_at":"` + later + `","conversation_id":"x"}`, http.StatusBadRequest, "invalid_conversation_id"},
		{http.MethodPost, "/contacts/" + contact.String() + "/reminders", `{"description":"a","due_at":"` + later + `","conversation_id":"` + id.New().String() + `"}`, http.StatusBadRequest, "conversation_not_of_contact"},
		{http.MethodPost, "/contacts/" + id.New().String() + "/reminders", `{"description":"a","due_at":"` + later + `"}`, http.StatusNotFound, "contact_not_found"},
		{http.MethodPatch, "/reminders/" + created.ID, `{"description":"llamar el lunes"}`, http.StatusOK, ""},
		{http.MethodPatch, "/reminders/" + created.ID, `{"due_at":"ayer"}`, http.StatusBadRequest, "invalid_due_at"},
		{http.MethodPatch, "/reminders/" + created.ID, `no`, http.StatusBadRequest, "invalid_body"},
		{http.MethodPatch, "/reminders/x", `{}`, http.StatusBadRequest, "invalid_reminder_id"},
		{http.MethodPatch, "/reminders/" + id.New().String(), `{}`, http.StatusNotFound, "reminder_not_found"},
		{http.MethodDelete, "/reminders/" + created.ID, "", http.StatusNoContent, ""},
	} {
		rec := call(engine, tc.method, tc.path, tc.body)
		if rec.Code != tc.want {
			t.Fatalf("%s %s: got %d, want %d - body %s", tc.method, tc.path, rec.Code, tc.want, rec.Body)
		}
		if tc.code != "" {
			if got := errorCode(t, rec); got != tc.code {
				t.Fatalf("%s %s: code %q, want %q", tc.method, tc.path, got, tc.code)
			}
		}
	}
}

// TestAnotherOperatorsReminderIsForbidden is the one refusal by role, which the frontend shows apart from a missing reminder.
func TestAnotherOperatorsReminderIsForbidden(t *testing.T) {
	store := newStore()
	r := Reminder{ID: id.New(), ContactID: id.New(), OperatorID: id.New(), Description: "ajeno", DueAt: clock.Add(time.Hour)}
	store.rows[r.ID] = r

	rec := call(mount(enums.OperatorRoleOperator, newService(store)), http.MethodDelete, "/reminders/"+r.ID.String(), "")
	if rec.Code != http.StatusForbidden || errorCode(t, rec) != "not_the_owner" {
		t.Fatalf("got %d - body %s, want 403 not_the_owner", rec.Code, rec.Body)
	}
}

// TestTheHandlersRefuseWithoutAnActor guards the wiring the same way the other packages do.
func TestTheHandlersRefuseWithoutAnActor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := NewController(newService(newStore()), nil)
	for _, handler := range []gin.HandlerFunc{c.create, c.ofContact, c.mine, c.update, c.remove} {
		rec := httptest.NewRecorder()
		gc, _ := gin.CreateTestContext(rec)
		gc.Request = httptest.NewRequest(http.MethodGet, "/reminders", nil)
		handler(gc)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("got %d, want 401 without an actor", rec.Code)
		}
	}
}
