package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const testSecret = "un-secreto-de-prueba-de-mas-de-32-bytes"

// TestTheTokenCarriesWhoAndWhenAndNothingElse protects regla 1: a claim more would be a permission frozen at login.
func TestTheTokenCarriesWhoAndWhenAndNothingElse(t *testing.T) {
	tokens := NewTokens(testSecret, 12*time.Hour)
	operatorID := uuid.New()
	now := time.Now().UTC()

	raw, expiresAt, err := tokens.Issue(operatorID, now)
	if err != nil {
		t.Fatalf("Issue: got %v, want no error", err)
	}
	if want := now.Add(12 * time.Hour); !expiresAt.Equal(want) {
		t.Fatalf("expiresAt = %v, want %v", expiresAt, want)
	}

	got, err := tokens.Verify(raw)
	if err != nil {
		t.Fatalf("Verify: got %v, want no error", err)
	}
	if got != operatorID {
		t.Fatalf("Verify returned %v, want %v", got, operatorID)
	}

	var claims jwt.RegisteredClaims
	if _, _, err := jwt.NewParser().ParseUnverified(raw, &claims); err != nil {
		t.Fatalf("ParseUnverified: %v", err)
	}
	if claims.Issuer != "" || claims.ID != "" || len(claims.Audience) > 0 {
		t.Fatalf("the token carries claims beyond sub, iat and exp: %+v", claims)
	}
}

// TestVerifyRefusesEveryTokenThisCoreDidNotIssue is the file's reason to exist: each case is a way in that a hand-rolled verifier leaves open.
func TestVerifyRefusesEveryTokenThisCoreDidNotIssue(t *testing.T) {
	operatorID := uuid.New()
	now := time.Now().UTC()
	tokens := NewTokens(testSecret, 12*time.Hour)

	// alg none is the classic: a token with no signature at all, which a verifier that does not name its algorithm accepts (regla 6).
	unsigned, err := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.RegisteredClaims{
		Subject:   operatorID.String(),
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("building the alg none token: %v", err)
	}

	fromAnotherSecret, _, err := NewTokens("otro-secreto-de-mas-de-32-bytes-largo", 12*time.Hour).Issue(operatorID, now)
	if err != nil {
		t.Fatalf("building the foreign token: %v", err)
	}

	notAUUID, err := jwt.NewWithClaims(signingMethod, jwt.RegisteredClaims{
		Subject:   "no-soy-un-uuid",
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	}).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("building the bad subject token: %v", err)
	}

	cases := []struct {
		name string
		raw  string
	}{
		{"a token with no signature, which is what alg none means", unsigned},
		{"a well formed token signed with somebody else's secret", fromAnotherSecret},
		{"a token this core signed, naming an operator that cannot exist", notAUUID},
		{"a string that is not a token at all", "no.es.un.token"},
		{"nothing, which is what a missing header sends", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tokens.Verify(tc.raw); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("Verify: got %v, want ErrInvalidToken", err)
			}
		})
	}
}

// TestAnExpiredTokenIsToldApart is what lets the operator read "your session ran out" instead of a generic refusal (ADR-0030).
func TestAnExpiredTokenIsToldApart(t *testing.T) {
	tokens := NewTokens(testSecret, 12*time.Hour)

	// Issued thirteen hours ago, so the twelve hour life is already spent without the test having to wait for it.
	raw, _, err := tokens.Issue(uuid.New(), time.Now().UTC().Add(-13*time.Hour))
	if err != nil {
		t.Fatalf("Issue: got %v, want no error", err)
	}

	if _, err := tokens.Verify(raw); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("Verify of an expired token: got %v, want ErrExpiredToken", err)
	}
}
