package operators

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/auth"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// mount builds the real router and the real issuer, so a token that opens /me here is one login actually handed out.
func mount(t *testing.T, operators ...Operator) http.Handler {
	t.Helper()

	tokens := auth.NewTokens("a-secret-long-enough-to-clear-the-floor", 12*time.Hour)
	service := NewService(newStubStore(operators...), tokens, nil)

	return soniahttp.NewRouter(soniahttp.RouterOptions{
		Controllers:       []soniahttp.Controller{NewController(service, nil)},
		PublicControllers: []soniahttp.Controller{NewLoginController(service, nil)},
		TokenVerifier: func(raw string) (uuid.UUID, error) {
			operatorID, err := tokens.Verify(raw)
			if err != nil {
				return uuid.Nil, soniahttp.Unauthorized("invalid_credentials", "the session token is not valid")
			}
			return operatorID, nil
		},
		ActorLookup: func(ctx context.Context, operatorID uuid.UUID) (soniahttp.Actor, error) {
			operator, err := service.Resolve(ctx, operatorID)
			if err != nil {
				return soniahttp.Actor{}, soniahttp.Unauthorized("session_no_longer_valid", "no such operator")
			}
			return soniahttp.Actor{OperatorID: operator.ID, Role: operator.Role}, nil
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

// TestOnlyTheTokenLoginHandedOutOpensMe is the whole phase in one test: the two routes only work together, and only in that order.
func TestOnlyTheTokenLoginHandedOutOpensMe(t *testing.T) {
	operator := hashed(t, "admin@coop.test", "la-correcta", true)
	engine := mount(t, operator)

	for _, tc := range []struct {
		name      string
		authorize string
	}{
		{"nobody at all", ""},
		{"an operator id, which is what opened this door before ADR-0030", "Bearer " + operator.ID.String()},
		{"a token this core never signed", "Bearer eyJhbGciOiJIUzI1NiJ9.e30.no-es-una-firma"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
			if tc.authorize != "" {
				req.Header.Set("Authorization", tc.authorize)
			}
			engine.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("GET /api/v1/me: got %d, want 401 — body %s", rec.Code, rec.Body)
			}
		})
	}

	login := post(engine, "/auth/login", `{"email":"admin@coop.test","password":"la-correcta"}`)
	if login.Code != http.StatusOK {
		t.Fatalf("POST /auth/login: got %d, want 200 — body %s", login.Code, login.Body)
	}
	var session loginResponse
	if err := json.Unmarshal(login.Body.Bytes(), &session); err != nil {
		t.Fatalf("decoding the login: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
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

// TestMeRefusesARowThatVanishedAfterTheMiddlewareReadIt covers the gap between the two reads: the handler reads again, so it can disagree.
func TestMeRefusesARowThatVanishedAfterTheMiddlewareReadIt(t *testing.T) {
	operator := hashed(t, "admin@coop.test", "la-correcta", true)
	// A service with nobody in it, behind a lookup that still vouches for the operator: the row went away between the two reads.
	service := NewService(newStubStore(), &stubIssuer{}, nil)

	engine := soniahttp.NewRouter(soniahttp.RouterOptions{
		Controllers:   []soniahttp.Controller{NewController(service, nil)},
		TokenVerifier: func(string) (uuid.UUID, error) { return operator.ID, nil },
		ActorLookup: func(context.Context, uuid.UUID) (soniahttp.Actor, error) {
			return soniahttp.Actor{OperatorID: operator.ID, Role: operator.Role}, nil
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer cualquier-cosa")
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/me: got %d, want 401 — body %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "session_no_longer_valid") {
		t.Fatalf("the refusal does not name the reason: %s", rec.Body)
	}
}
