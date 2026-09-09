package operators

import (
	"errors"
	"fmt"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

// Fixed by ADR-0030, and a constant and not an environment variable so nobody lowers it to make a test suite faster.
const passwordCost = 12

var (
	// Surfaced instead of letting bcrypt truncate in silence: the 72 byte ceiling is a cost ADR-0030 already declared.
	ErrPasswordTooLong = errors.New("operators: password is longer than the 72 bytes bcrypt reads")

	// The service answers this the same way it answers an unknown email, so neither says which of the two was wrong.
	ErrWrongPassword = errors.New("operators: password does not match the hash")
)

// Hash is the only writer of operators.password_hash: the seed and any future ABM go through here (F18).
func Hash(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), passwordCost)
	if errors.Is(err, bcrypt.ErrPasswordTooLong) {
		return "", ErrPasswordTooLong
	}
	if err != nil {
		return "", fmt.Errorf("operators: hash password: %w", err)
	}
	return string(hash), nil
}

// Verify reports a mismatch as ErrWrongPassword and anything else as a failure, because a corrupt hash is not a bad password.
func Verify(hash, password string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return ErrWrongPassword
	}
	if err != nil {
		return fmt.Errorf("operators: verify password: %w", err)
	}
	return nil
}

// Computed once and at the real cost, because a cheaper hash here would leave exactly the timing difference it exists to erase.
var discardHash = sync.OnceValue(func() string {
	hash, err := bcrypt.GenerateFromPassword([]byte("sonia-discard"), passwordCost)
	if err != nil {
		panic(fmt.Sprintf("operators: cannot build the discard hash: %v", err))
	}
	return string(hash)
})

// WasteAComparison is called when the email matched nothing, so an unregistered address does not answer faster than a wrong password (ADR-0030).
func WasteAComparison() {
	_ = bcrypt.CompareHashAndPassword([]byte(discardHash()), []byte("sonia-discard-mismatch"))
}
