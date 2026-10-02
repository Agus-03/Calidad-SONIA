// Package operators owns the operators table: the person who attends, never called User (C01, F18).
package operators

import (
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

var (
	// ErrNotFound is returned by every read, so the caller decides what a missing operator means in its own case.
	ErrNotFound = errors.New("operators: operator not found")

	// ErrDuplicateEmail names what the unique index refuses, so the admin is told the address is taken (ADR-0062).
	ErrDuplicateEmail = errors.New("operators: an operator with that email already exists")

	// ErrPasswordChanged is a password replaced between the read and the write, which the caller has to read again (ADR-0062).
	ErrPasswordChanged = errors.New("operators: the password changed meanwhile")
)

// Operator carries PasswordHash, so no controller may serialize this struct: what leaves the process is a DTO (ADR-0030).
type Operator struct {
	ID           uuid.UUID
	AccountID    uuid.UUID
	Name         string
	Lastname     string
	Email        string
	PasswordHash string
	Role         enums.OperatorRole
	Active       bool
}

// normalizeEmail is applied on both sides of the unique index, which is over the raw column: without it two casings are two operators.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// Filter narrows the roster; a nil field is not asked about, so the empty filter is the whole team.
type Filter struct {
	Role   *enums.OperatorRole
	Active *bool
}

// Page carries Total because a page number cannot tell whether there is a next one without it, as in labels/.
type Page struct {
	Items []Operator
	Total int
}
