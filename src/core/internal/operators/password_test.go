package operators

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// TestTheStoredHashIsNeitherThePasswordNorReusable is the property the whole file exists for: the database must not carry the secret.
func TestTheStoredHashIsNeitherThePasswordNorReusable(t *testing.T) {
	const password = "una-clave-de-la-cooperante"

	first, err := Hash(password)
	if err != nil {
		t.Fatalf("Hash: got %v, want no error", err)
	}
	if strings.Contains(first, password) {
		t.Fatal("the stored hash contains the password in the clear")
	}

	// Two hashes of one password must differ, or the salt is not doing its job and equal passwords become visible in the table.
	second, err := Hash(password)
	if err != nil {
		t.Fatalf("Hash again: got %v, want no error", err)
	}
	if first == second {
		t.Fatal("two hashes of the same password came out identical, so nothing is salted")
	}

	cost, err := bcrypt.Cost([]byte(first))
	if err != nil {
		t.Fatalf("Cost: got %v, want a readable hash", err)
	}
	if cost != passwordCost {
		t.Fatalf("cost = %d, want the %d ADR-0030 fixed", cost, passwordCost)
	}
}

// TestVerifyAcceptsOnlyTheRightPassword covers both halves at once, because a Verify that always passed would look identical in the login.
func TestVerifyAcceptsOnlyTheRightPassword(t *testing.T) {
	hash, err := Hash("la-correcta")
	if err != nil {
		t.Fatalf("Hash: got %v, want no error", err)
	}

	if err := Verify(hash, "la-correcta"); err != nil {
		t.Fatalf("Verify with the right password: got %v, want no error", err)
	}
	if err := Verify(hash, "la-incorrecta"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("Verify with the wrong password: got %v, want ErrWrongPassword", err)
	}
}

// TestAPasswordOverTheCeilingIsRefusedAndNotTruncated protects the declared cost of bcrypt: silently cutting at 72 would ignore the rest.
func TestAPasswordOverTheCeilingIsRefusedAndNotTruncated(t *testing.T) {
	if _, err := Hash(strings.Repeat("a", 73)); !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("Hash of 73 bytes: got %v, want ErrPasswordTooLong", err)
	}
	if _, err := Hash(strings.Repeat("a", 72)); err != nil {
		t.Fatalf("Hash of 72 bytes: got %v, want no error", err)
	}
}

// TestTheDiscardedComparisonCostsWhatARealOneCosts is the whole point of regla 8: a cheaper hash would leave the timing oracle open.
func TestTheDiscardedComparisonCostsWhatARealOneCosts(t *testing.T) {
	cost, err := bcrypt.Cost([]byte(discardHash()))
	if err != nil {
		t.Fatalf("Cost of the discard hash: got %v, want a readable hash", err)
	}
	if cost != passwordCost {
		t.Fatalf("the discard hash costs %d and a real one costs %d, so an unknown email answers faster", cost, passwordCost)
	}
	WasteAComparison()
}
