package operators

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestLoginGrantsTheSessionOnlyToTheRightPassword is the happy path, and the only case in the file where a token comes out.
func TestLoginGrantsTheSessionOnlyToTheRightPassword(t *testing.T) {
	ctx := context.Background()
	operator := hashed(t, "Admin@Coop.test", "la-correcta", true)
	tokens := &stubIssuer{}
	service := NewService(newStubStore(operator), tokens, nil)

	now := time.Now().UTC()
	// Typed with the casing of the day, which the repository lowercases before it ever reaches the index.
	session, err := service.Login(ctx, "  admin@coop.TEST ", "la-correcta", now)
	if err != nil {
		t.Fatalf("Login: got %v, want no error", err)
	}
	if session.Operator.ID != operator.ID {
		t.Fatalf("session names %v, want %v", session.Operator.ID, operator.ID)
	}
	if !session.ExpiresAt.Equal(now.Add(12 * time.Hour)) {
		t.Fatalf("ExpiresAt = %v, want the issuer's", session.ExpiresAt)
	}
	if len(tokens.issuedFor) != 1 || tokens.issuedFor[0] != operator.ID {
		t.Fatalf("issued for %v, want one token for %v", tokens.issuedFor, operator.ID)
	}
}

// TestEveryRefusalLooksTheSameFromOutside is the decision itself: three different reasons, one answer, and no token in any of them.
func TestEveryRefusalLooksTheSameFromOutside(t *testing.T) {
	ctx := context.Background()
	active := hashed(t, "activo@coop.test", "la-correcta", true)
	disabled := hashed(t, "desactivado@coop.test", "la-correcta", false)

	cases := []struct {
		name     string
		email    string
		password string
	}{
		{"an address nobody registered", "nadie@coop.test", "la-correcta"},
		{"a registered address with the wrong password", "activo@coop.test", "la-incorrecta"},
		{"the right password on an account that was taken down", "desactivado@coop.test", "la-correcta"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tokens := &stubIssuer{}
			service := NewService(newStubStore(active, disabled), tokens, nil)

			_, err := service.Login(ctx, tc.email, tc.password, time.Now().UTC())
			if !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("Login: got %v, want ErrInvalidCredentials", err)
			}
			if len(tokens.issuedFor) != 0 {
				t.Fatalf("a refused login still issued a token for %v", tokens.issuedFor)
			}
		})
	}
}

// TestAnUnknownEmailStillPaysForAComparison protects regla 8: answering early is what turns the login into a list of who works here.
func TestAnUnknownEmailStillPaysForAComparison(t *testing.T) {
	ctx := context.Background()
	service := NewService(newStubStore(hashed(t, "activo@coop.test", "la-correcta", true)), &stubIssuer{}, nil)

	started := time.Now()
	if _, err := service.Login(ctx, "nadie@coop.test", "cualquiera", started.UTC()); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login: got %v, want ErrInvalidCredentials", err)
	}

	// A loose floor on purpose: one bcrypt round at cost 12 takes hundreds of milliseconds, so only skipping it entirely fails here.
	if elapsed := time.Since(started); elapsed < 50*time.Millisecond {
		t.Fatalf("the unknown email answered in %v, so no comparison was spent and the timing tells the two apart", elapsed)
	}
}

// TestResolveReadsTheCurrentRowAndNotTheToken protects regla 2: a session outlives a change of state, and the row is what decides.
func TestResolveReadsTheCurrentRowAndNotTheToken(t *testing.T) {
	ctx := context.Background()
	active := hashed(t, "activo@coop.test", "la-correcta", true)
	disabled := hashed(t, "desactivado@coop.test", "la-correcta", false)
	service := NewService(newStubStore(active, disabled), &stubIssuer{}, nil)

	got, err := service.Resolve(ctx, active.ID)
	if err != nil {
		t.Fatalf("Resolve of an active operator: got %v, want no error", err)
	}
	if got.Role != active.Role {
		t.Fatalf("Resolve returned role %q, want %q", got.Role, active.Role)
	}

	if _, err := service.Resolve(ctx, disabled.ID); !errors.Is(err, ErrInactiveOperator) {
		t.Fatalf("Resolve of a disabled operator: got %v, want ErrInactiveOperator", err)
	}
	if _, err := service.Resolve(ctx, hashed(t, "borrado@coop.test", "x", true).ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Resolve of an operator that is not there: got %v, want ErrNotFound", err)
	}
}
