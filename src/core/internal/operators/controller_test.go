package operators

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// mount builds the real router, so the zone of each route is asserted by the same guard that runs in production.
func mount(t *testing.T, operators ...Operator) http.Handler {
	t.Helper()

	service := NewService(newStubStore(operators...), &stubIssuer{}, nil)

	return soniahttp.NewRouter(soniahttp.RouterOptions{
		Controllers:       []soniahttp.Controller{NewController(service, nil)},
		PublicControllers: []soniahttp.Controller{NewLoginController(service, nil)},
		// Still the header seam of Fase 1: task 9 replaces it with the Bearer token and this line changes with it.
		ActorLookup: func(_ context.Context, operatorID uuid.UUID) (soniahttp.Actor, error) {
			operator, err := service.Resolve(context.Background(), operatorID)
			if err != nil {
				return soniahttp.Actor{}, soniahttp.Unauthorized("invalid_credentials", "no such operator")
			}
			return soniahttp.Actor{OperatorID: operator.ID, Role: string(operator.Role)}, nil
		},
	})
}

func post(engine http.Handler, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, req)
	return rec
}

// TestLoginAnswersTheTokenAndNeverTheHash is the property a DTO exists for: the row carries the hash and the response must not.
func TestLoginAnswersTheTokenAndNeverTheHash(t *testing.T) {
	operator := hashed(t, "admin@coop.test", "la-correcta", true)
	engine := mount(t, operator)

	rec := post(engine, "/auth/login", `{"email":"admin@coop.test","password":"la-correcta"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /auth/login: got %d, want 200 — body %s", rec.Code, rec.Body)
	}

	var got loginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding the answer: %v", err)
	}
	if got.Token == "" || got.ExpiresAt.IsZero() {
		t.Fatalf("the answer carries no session: %+v", got)
	}
	if got.Operator.ID != operator.ID.String() || got.Operator.Role != "admin" {
		t.Fatalf("the answer names %+v, want %v as admin", got.Operator, operator.ID)
	}
	if strings.Contains(rec.Body.String(), operator.PasswordHash) || strings.Contains(rec.Body.String(), "$2a$") {
		t.Fatalf("the password hash reached the response: %s", rec.Body)
	}
}

// TestLoginRefusesWithoutSayingWhich covers the two shapes a refusal takes at the HTTP border: a bad body and a bad credential.
func TestLoginRefusesWithoutSayingWhich(t *testing.T) {
	engine := mount(t, hashed(t, "admin@coop.test", "la-correcta", true))

	cases := []struct {
		name string
		body string
		want int
	}{
		{"the right address with the wrong password", `{"email":"admin@coop.test","password":"la-incorrecta"}`, http.StatusUnauthorized},
		{"an address nobody registered", `{"email":"nadie@coop.test","password":"la-correcta"}`, http.StatusUnauthorized},
		{"a body with no password at all", `{"email":"admin@coop.test"}`, http.StatusBadRequest},
		{"something that is not an email", `{"email":"admin","password":"la-correcta"}`, http.StatusBadRequest},
		{"not even json", `no soy json`, http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := post(engine, "/auth/login", tc.body)
			if rec.Code != tc.want {
				t.Fatalf("got %d, want %d — body %s", rec.Code, tc.want, rec.Body)
			}
			if strings.Contains(rec.Body.String(), "token") {
				t.Fatalf("a refused login answered with a token: %s", rec.Body)
			}
		})
	}
}

// TestMeIsTheFirstRouteThatRefusesAnAnonymousCaller proves the zones: login answers without credentials and /api/v1 does not.
func TestMeIsTheFirstRouteThatRefusesAnAnonymousCaller(t *testing.T) {
	operator := hashed(t, "admin@coop.test", "la-correcta", true)
	engine := mount(t, operator)

	anonymous := httptest.NewRecorder()
	engine.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/me with no credentials: got %d, want 401", anonymous.Code)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("X-Operator-Id", operator.ID.String())
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/me: got %d, want 200 — body %s", rec.Code, rec.Body)
	}

	var got operatorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding the answer: %v", err)
	}
	if got.Email != operator.Email || got.Role != "admin" {
		t.Fatalf("/me answered %+v, want the seeded admin", got)
	}
	if strings.Contains(rec.Body.String(), "$2a$") {
		t.Fatalf("the password hash reached the response: %s", rec.Body)
	}
}
