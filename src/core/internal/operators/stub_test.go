package operators

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// stubStore answers from memory, so the login is tested against its own rules and not against the SQL that reads them.
type stubStore struct {
	byEmail map[string]Operator
	byID    map[uuid.UUID]Operator
}

func newStubStore(operators ...Operator) *stubStore {
	s := &stubStore{byEmail: map[string]Operator{}, byID: map[uuid.UUID]Operator{}}
	for _, o := range operators {
		s.byEmail[normalizeEmail(o.Email)] = o
		s.byID[o.ID] = o
	}
	return s
}

func (s *stubStore) ByEmail(_ context.Context, email string) (Operator, error) {
	o, ok := s.byEmail[normalizeEmail(email)]
	if !ok {
		return Operator{}, ErrNotFound
	}
	return o, nil
}

func (s *stubStore) ByID(_ context.Context, operatorID uuid.UUID) (Operator, error) {
	o, ok := s.byID[operatorID]
	if !ok {
		return Operator{}, ErrNotFound
	}
	return o, nil
}

// stubIssuer records who it was asked to sign for, which is how a test tells a granted login from a refused one without reading a token.
type stubIssuer struct {
	issuedFor []uuid.UUID
	err       error
}

func (s *stubIssuer) Issue(operatorID uuid.UUID, now time.Time) (string, time.Time, error) {
	if s.err != nil {
		return "", time.Time{}, s.err
	}
	s.issuedFor = append(s.issuedFor, operatorID)
	return "token-for-" + operatorID.String(), now.Add(12 * time.Hour), nil
}

// hashed builds the operator the way the seed does, because a test against a fake hash would never exercise bcrypt.
func hashed(t interface{ Fatalf(string, ...any) }, email, password string, active bool) Operator {
	hash, err := Hash(password)
	if err != nil {
		t.Fatalf("Hash: got %v, want no error", err)
	}
	return Operator{
		ID: uuid.New(), AccountID: uuid.New(),
		Name: "Prueba", Lastname: "Servicio",
		Email: email, PasswordHash: hash,
		Role: enums.OperatorRoleAdmin, Active: active,
	}
}
