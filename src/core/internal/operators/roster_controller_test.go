package operators

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// mountRoster builds the real router as the given person, because the roles of each route only exist in routes.go.
func mountRoster(caller Operator, store *stubRoster) http.Handler {
	return soniahttp.NewRouter(soniahttp.RouterOptions{
		Controllers:   []soniahttp.Controller{NewRosterController(store.service(), nil)},
		TokenVerifier: func(string) (uuid.UUID, error) { return caller.ID, nil },
		ActorLookup: func(context.Context, uuid.UUID) (soniahttp.Actor, error) {
			return soniahttp.Actor{OperatorID: caller.ID, AccountID: caller.AccountID, Role: caller.Role}, nil
		},
	})
}

func send(engine http.Handler, method, path, body string) *httptest.ResponseRecorder {
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
		Error struct{ Code string } `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the error is not json: %v - %s", err, rec.Body)
	}
	return body.Error.Code
}

// TestOnlyTheAdminManagesTheTeam: the supervisor reads it to reassign, the operator does not see it, and only the admin writes.
func TestOnlyTheAdminManagesTheTeam(t *testing.T) {
	target := person(enums.OperatorRoleOperator)
	writes := []struct{ method, path, body string }{
		{http.MethodPost, "/operators", `{"name":"A","lastname":"B","email":"a@sonia.test","password":"ocho car","role":"operator"}`},
		{http.MethodPatch, "/operators/" + target.ID.String(), `{"active":false}`},
		{http.MethodDelete, "/operators/" + target.ID.String(), ""},
		{http.MethodPut, "/operators/" + target.ID.String() + "/password", `{"password":"ocho car"}`},
	}
	for _, role := range []enums.OperatorRole{enums.OperatorRoleOperator, enums.OperatorRoleSupervisor} {
		caller := person(role)
		engine := mountRoster(caller, newRoster(caller, target))
		wantRead := http.StatusForbidden
		if role == enums.OperatorRoleSupervisor {
			wantRead = http.StatusOK
		}
		if rec := send(engine, http.MethodGet, "/operators", ""); rec.Code != wantRead {
			t.Fatalf("%s reading: got %d, want %d", role, rec.Code, wantRead)
		}
		for _, w := range writes {
			if rec := send(engine, w.method, w.path, w.body); rec.Code != http.StatusForbidden {
				t.Fatalf("%s %s by %s: got %d, want 403", w.method, w.path, role, rec.Code)
			}
		}
	}
}

// TestTheRosterRoutesAnswerWithTheirCodes walks every route as the admin, because a wrong path or method compiles.
func TestTheRosterRoutesAnswerWithTheirCodes(t *testing.T) {
	admin, vendor := person(enums.OperatorRoleAdmin), person(enums.OperatorRoleOperator)
	// A real hash, so /me/password reaches the comparison and refuses for the password and not for a malformed hash.
	hash, err := Hash("la del admin")
	if err != nil {
		t.Fatal(err)
	}
	admin.PasswordHash = hash
	store := newRoster(admin, vendor)
	store.held = 2
	engine := mountRoster(admin, store)
	target := "/operators/" + vendor.ID.String()

	rec := send(engine, http.MethodPost, "/operators", `{"name":"Bruno","lastname":"Sosa","email":"bruno@sonia.test","password":"una clave larga","role":"supervisor"}`)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"role":"supervisor","active":true`) || strings.Contains(rec.Body.String(), "password") {
		t.Fatalf("POST: got %d - body %s, want 201, active and no password", rec.Code, rec.Body)
	}

	for _, tc := range []struct {
		method, path, body string
		want               int
		code               string
	}{
		{http.MethodGet, "/operators?role=operator&active=true&page=1&per_page=500", "", http.StatusOK, ""},
		{http.MethodGet, "/operators?role=jefe", "", http.StatusBadRequest, "invalid_role"},
		{http.MethodGet, "/operators?active=quizas", "", http.StatusBadRequest, "invalid_active"},
		{http.MethodGet, "/operators?page=0", "", http.StatusBadRequest, "invalid_page"},
		{http.MethodGet, "/operators?per_page=x", "", http.StatusBadRequest, "invalid_per_page"},
		{http.MethodPost, "/operators", `{"name":"Sin rol"}`, http.StatusBadRequest, "invalid_body"},
		{http.MethodPost, "/operators", `{"name":"A","lastname":"B","email":"bruno@sonia.test","password":"ocho car","role":"operator"}`, http.StatusConflict, "email_taken"},
		{http.MethodPost, "/operators", `{"name":"A","lastname":"B","email":"x","password":"ocho car","role":"operator"}`, http.StatusBadRequest, "invalid_email"},
		{http.MethodPost, "/operators", `{"name":" ","lastname":"B","email":"c@sonia.test","password":"ocho car","role":"operator"}`, http.StatusBadRequest, "invalid_name"},
		{http.MethodPost, "/operators", `{"name":"A","lastname":"B","email":"c@sonia.test","password":"ocho car","role":"jefe"}`, http.StatusBadRequest, "invalid_role"},
		{http.MethodPost, "/operators", `{"name":"A","lastname":"B","email":"c@sonia.test","password":"corta","role":"operator"}`, http.StatusBadRequest, "password_too_short"},
		{http.MethodPost, "/operators", `{"name":"A","lastname":"B","email":"c@sonia.test","password":"` + strings.Repeat("x", 73) + `","role":"operator"}`, http.StatusBadRequest, "password_too_long"},
		{http.MethodPatch, target, `{"active":false}`, http.StatusOK, ""},
		{http.MethodPatch, target, `{"role":"jefe"}`, http.StatusBadRequest, "invalid_role"},
		{http.MethodPatch, target, `no`, http.StatusBadRequest, "invalid_body"},
		{http.MethodPatch, "/operators/x", `{}`, http.StatusBadRequest, "invalid_id"},
		{http.MethodPatch, "/operators/" + id.New().String(), `{}`, http.StatusNotFound, "operator_not_found"},
		{http.MethodPatch, "/operators/" + admin.ID.String(), `{"active":false}`, http.StatusConflict, "not_on_yourself"},
		{http.MethodPut, target + "/password", `{"password":"la nueva del vendedor"}`, http.StatusNoContent, ""},
		{http.MethodPut, target + "/password", `{}`, http.StatusBadRequest, "invalid_body"},
		{http.MethodPut, "/me/password", `{"current_password":"no es","new_password":"la nueva y larga"}`, http.StatusBadRequest, "wrong_current_password"},
		{http.MethodPut, "/me/password", `{"current_password":"no es"}`, http.StatusBadRequest, "invalid_body"},
		{http.MethodDelete, target, "", http.StatusOK, ""},
		{http.MethodDelete, "/operators/x", "", http.StatusBadRequest, "invalid_id"},
	} {
		rec := send(engine, tc.method, tc.path, tc.body)
		if rec.Code != tc.want {
			t.Fatalf("%s %s: got %d, want %d - body %s", tc.method, tc.path, rec.Code, tc.want, rec.Body)
		}
		if tc.code != "" {
			if got := codeOf(t, rec); got != tc.code {
				t.Fatalf("%s %s: code %q, want %q", tc.method, tc.path, got, tc.code)
			}
		}
	}
}

// TestADeactivationAnswersWhatIsLeftToHandOver is decision A of the group: the admin is told, so ADR-0056 can follow.
func TestADeactivationAnswersWhatIsLeftToHandOver(t *testing.T) {
	admin, vendor := person(enums.OperatorRoleAdmin), person(enums.OperatorRoleOperator)
	store := newRoster(admin, vendor)
	store.held = 4
	engine := mountRoster(admin, store)

	rec := send(engine, http.MethodPatch, "/operators/"+vendor.ID.String(), `{"active":false}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"active":false,"held_conversations":4`) {
		t.Fatalf("PATCH: got %d - body %s, want inactive with four held", rec.Code, rec.Body)
	}
	rec = send(engine, http.MethodDelete, "/operators/"+vendor.ID.String(), "")
	if rec.Code != http.StatusOK || rec.Body.String() != `{"held_conversations":4}` {
		t.Fatalf("DELETE: got %d - body %s, want the four held", rec.Code, rec.Body)
	}
}

// TestTheLastAdminAndAChangedPasswordAreConflicts, which the frontend shows apart from a validation error.
func TestTheLastAdminAndAChangedPasswordAreConflicts(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{ErrLastAdmin, "last_admin"},
		{ErrPasswordChanged, "password_changed"},
	} {
		var typed *soniahttp.Error
		if !errors.As(translateRoster(tc.err), &typed) || typed.Status != http.StatusConflict || typed.Code != tc.code {
			t.Fatalf("%v: got %+v, want 409 %s", tc.err, typed, tc.code)
		}
	}
	if err := errors.New("the database is gone"); translateRoster(err) != err {
		t.Fatal("an unknown error was dressed as a 4xx")
	}
}

// TestAFailedListIsA500 lets an error the package does not know fall through.
func TestAFailedListIsA500(t *testing.T) {
	admin := person(enums.OperatorRoleAdmin)
	store := newRoster(admin)
	store.listErr = errors.New("the database is gone")
	if rec := send(mountRoster(admin, store), http.MethodGet, "/operators", ""); rec.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500", rec.Code)
	}
}

// TestTheRosterHandlersRefuseWithoutAnActor guards the wiring the same way the other packages do.
func TestTheRosterHandlersRefuseWithoutAnActor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := NewRosterController(newRoster().service(), nil)
	for _, handler := range []gin.HandlerFunc{c.list, c.create, c.update, c.remove, c.resetPassword, c.changePassword} {
		rec := httptest.NewRecorder()
		gc, _ := gin.CreateTestContext(rec)
		gc.Request = httptest.NewRequest(http.MethodGet, "/operators", nil)
		handler(gc)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("got %d, want 401 without an actor", rec.Code)
		}
	}
}
