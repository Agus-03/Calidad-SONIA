// Package auth issues and verifies the session token of the operator API, without gin and without the database (ADR-0030).
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Naming the one algorithm accepted is what refuses a token that arrives claiming alg none or another scheme (ADR-0030, regla 6).
var signingMethod = jwt.SigningMethodHS256

var (
	// Told apart from the invalid one because an operator can be told their session ran out, which is not the same as a broken token.
	ErrExpiredToken = errors.New("auth: the session token expired")

	ErrInvalidToken = errors.New("auth: the session token is not one this core issued")
)

// Tokens holds the secret, so it is built in deps.go and injected, never read from a global (regla 10).
type Tokens struct {
	secret []byte
	ttl    time.Duration
}

func NewTokens(secret string, ttl time.Duration) *Tokens {
	return &Tokens{secret: []byte(secret), ttl: ttl}
}

// Issue returns the expiry alongside the token, because otherwise the frontend can only learn it by decoding what it should treat as opaque.
func (t *Tokens) Issue(operatorID uuid.UUID, now time.Time) (string, time.Time, error) {
	expiresAt := now.Add(t.ttl)

	// Only sub, iat and exp: the role and active are read from the row on every request, never frozen in here (ADR-0030, regla 1).
	token := jwt.NewWithClaims(signingMethod, jwt.RegisteredClaims{
		Subject:   operatorID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
	})

	signed, err := token.SignedString(t.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: sign token: %w", err)
	}
	return signed, expiresAt, nil
}

// Verify answers who, and only who: what that operator may do is a question for the database (ADR-0030, regla 2).
func (t *Tokens) Verify(raw string) (uuid.UUID, error) {
	var claims jwt.RegisteredClaims

	_, err := jwt.ParseWithClaims(raw, &claims,
		func(*jwt.Token) (any, error) { return t.secret, nil },
		jwt.WithValidMethods([]string{signingMethod.Alg()}))

	if errors.Is(err, jwt.ErrTokenExpired) {
		return uuid.Nil, ErrExpiredToken
	}
	// The cause travels for the log and the sentinel for the answer: what went wrong is nobody's business outside the process.
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	operatorID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: subject is not a uuid", ErrInvalidToken)
	}
	return operatorID, nil
}
