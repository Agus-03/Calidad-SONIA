package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/operators"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/auth"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/config"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/encryption"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

const testSecret = "a-secret-long-enough-to-clear-the-floor"

// codeOf reads the stable code out of the error, which is the only part of a refusal the frontend is allowed to branch on.
func codeOf(t *testing.T, err error) string {
	t.Helper()

	var apiErr *soniahttp.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("the adapter returned %v, want an http error the router can write", err)
	}
	return apiErr.Code
}

// TestAnExpiredSessionIsNotTheSameAnswerAsABrokenToken is why the mapping lives in deps.go: one sends you to log in again, the other does not.
func TestAnExpiredSessionIsNotTheSameAnswerAsABrokenToken(t *testing.T) {
	tokens := auth.NewTokens(testSecret, 12*time.Hour)
	verify := verifyToken(tokens)

	operatorID := uuid.New()
	now := time.Now().UTC()

	valid, _, err := tokens.Issue(operatorID, now)
	if err != nil {
		t.Fatalf("Issue: got %v, want no error", err)
	}
	// Issued thirteen hours ago, so a twelve-hour life has already run out.
	expired, _, err := tokens.Issue(operatorID, now.Add(-13*time.Hour))
	if err != nil {
		t.Fatalf("Issue: got %v, want no error", err)
	}

	got, err := verify(valid)
	if err != nil || got != operatorID {
		t.Fatalf("a token this core signed: got %v, %v, want %v and no error", got, err, operatorID)
	}

	if code := codeOf(t, mustFail(t, verify, expired)); code != "session_expired" {
		t.Fatalf("an expired token answered %q, want session_expired", code)
	}

	for _, raw := range []string{
		"",
		"no-soy-un-token",
		// Signed with another key, which is the shape a stolen token from another deployment would have.
		issuedWith(t, "another-secret-long-enough-for-hs256", operatorID, now),
	} {
		if code := codeOf(t, mustFail(t, verify, raw)); code != "invalid_credentials" {
			t.Fatalf("token %q answered %q, want invalid_credentials", raw, code)
		}
	}
}

// TestTheLookupRefusesWhoeverTheRowNoLongerBacks protects regla 2: the token stays valid and the row is what takes the access away.
func TestTheLookupRefusesWhoeverTheRowNoLongerBacks(t *testing.T) {
	ctx := context.Background()
	active := operators.Operator{ID: uuid.New(), AccountID: uuid.New(), Role: enums.OperatorRoleAdmin, Active: true}
	disabled := operators.Operator{ID: uuid.New(), AccountID: uuid.New(), Role: enums.OperatorRoleOperator, Active: false}

	lookup := resolveActor(operators.NewService(
		&fakeStore{rows: map[uuid.UUID]operators.Operator{active.ID: active, disabled.ID: disabled}}, nil, nil))

	actor, err := lookup(ctx, active.ID)
	if err != nil {
		t.Fatalf("an active operator: got %v, want no error", err)
	}
	if actor.OperatorID != active.ID || actor.AccountID != active.AccountID || actor.Role != enums.OperatorRoleAdmin {
		t.Fatalf("the actor is %+v, want the admin row", actor)
	}

	for _, tc := range []struct {
		name       string
		operatorID uuid.UUID
	}{
		{"an operator that was taken down", disabled.ID},
		{"an operator whose row is not there any more", uuid.New()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := lookup(ctx, tc.operatorID)
			if code := codeOf(t, err); code != "session_no_longer_valid" {
				t.Fatalf("got %q, want session_no_longer_valid", code)
			}
		})
	}
}

// TestTheWiredRouterActuallyServesTheLabelRoutes is the half the labels tests cannot reach: they mount a router of their own, so they
// prove the routes are declared and never that anybody mounts them. A 404 here would mean the package builds, tests green, and nothing serves it.
func TestTheWiredRouterActuallyServesEveryRouteItsContextsDeclare(t *testing.T) {
	engine := wiredRouter(t)

	someLabel := "/api/v1/labels/" + uuid.New().String()
	someContact := "/api/v1/contacts/" + uuid.New().String()

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/labels"},
		{http.MethodPost, "/api/v1/labels"},
		{http.MethodPatch, someLabel},
		{http.MethodDelete, someLabel},

		// The eight of contacts. They were served by a router the package builds for its own tests long before this one served
		// them, which is exactly why they are listed here: a context can be complete and still be plugged into nothing.
		{http.MethodGet, "/api/v1/contacts"},
		{http.MethodPost, "/api/v1/contacts"},
		{http.MethodGet, someContact},
		{http.MethodPatch, someContact},
		{http.MethodDelete, someContact},
		{http.MethodPut, someContact + "/sensitive"},
		{http.MethodPost, someContact + "/labels"},
		{http.MethodDelete, someContact + "/labels/" + uuid.New().String()},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))

			// 401 and not 404: the route is mounted and asked for a token. A 200 would mean it landed in the public zone by mistake.
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("got %d, want 401 - a 404 means nothing mounts it and a 200 means it is public", rec.Code)
			}
		})
	}

	// The guard that keeps the assertions above from being vacuous: an unmounted path under the same prefix must answer 404,
	// or every 401 here would prove nothing more than that the prefix exists.
	t.Run("una ruta que nadie monta", func(t *testing.T) {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/todavia-no-existe", nil))

		if rec.Code != http.StatusNotFound {
			t.Fatalf("got %d, want 404 - if this is 401 the cases above cannot tell mounted from unmounted", rec.Code)
		}
	})
}

// wiredRouter builds the real graph. A nil pool is enough because wire only stores it: nothing here opens a connection, and the
// refusals happen before any handler runs. The key is the one thing that cannot be zero — the cipher is built at wiring time now.
func wiredRouter(t *testing.T) http.Handler {
	t.Helper()

	deps, err := wire(config.Config{
		JWTSecret: testSecret, JWTTTL: 12 * time.Hour, EncryptionKey: make([]byte, encryption.KeyBytes),
	}, nil, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("wire: %v", err)
	}

	return soniahttp.NewRouter(soniahttp.RouterOptions{
		TokenVerifier:     deps.tokenVerifier,
		ActorLookup:       deps.actorLookup,
		Controllers:       deps.controllers,
		PublicControllers: deps.publicControllers,
	})
}

// TestTheBootRefusesAKeyTheCipherCannotUse is the branch config is supposed to make unreachable, kept because the day someone
// relaxes that validation, the failure this catches is a process that starts and writes contacts nobody can read back.
func TestTheBootRefusesAKeyTheCipherCannotUse(t *testing.T) {
	for _, key := range [][]byte{nil, make([]byte, 16), make([]byte, 31)} {
		_, err := wire(config.Config{
			JWTSecret: testSecret, JWTTTL: 12 * time.Hour, EncryptionKey: key,
		}, nil, slog.New(slog.DiscardHandler))
		if err == nil {
			t.Fatalf("wire accepted a %d byte key, want a refusal", len(key))
		}
	}
}

func mustFail(t *testing.T, verify soniahttp.TokenVerifier, raw string) error {
	t.Helper()

	if _, err := verify(raw); err != nil {
		return err
	}
	t.Fatalf("verify accepted %q, want a refusal", raw)
	return nil
}

func issuedWith(t *testing.T, secret string, operatorID uuid.UUID, now time.Time) string {
	t.Helper()

	token, _, err := auth.NewTokens(secret, time.Hour).Issue(operatorID, now)
	if err != nil {
		t.Fatalf("Issue: got %v, want no error", err)
	}
	return token
}

// fakeStore answers from memory because what is under test is the mapping of an absent or disabled row, not the SQL that finds it.
type fakeStore struct {
	rows map[uuid.UUID]operators.Operator
}

func (f *fakeStore) ByEmail(context.Context, string) (operators.Operator, error) {
	return operators.Operator{}, operators.ErrNotFound
}

func (f *fakeStore) ByID(_ context.Context, operatorID uuid.UUID) (operators.Operator, error) {
	row, ok := f.rows[operatorID]
	if !ok {
		return operators.Operator{}, operators.ErrNotFound
	}
	return row, nil
}
