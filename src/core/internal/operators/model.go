// Package operators owns the operators table: the person who attends, never called User (C01, F18).
package operators

import (
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// ErrNotFound is returned by both reads, so the caller decides what a missing operator means in its own case.
var ErrNotFound = errors.New("operators: operator not found")

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
