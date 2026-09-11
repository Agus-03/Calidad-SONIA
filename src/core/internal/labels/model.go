// Package labels owns the labels table: the single catalogue both union tables point at (ADR-0003, F19).
package labels

import (
	"errors"
	"strings"

	"github.com/google/uuid"
)

var (
	// ErrNotFound is returned by every read, so the caller decides what a missing label means in its own case.
	ErrNotFound = errors.New("labels: label not found")

	// ErrDuplicateName names what the unique index rejects, so the caller can say which name is taken instead of returning a bare failure.
	ErrDuplicateName = errors.New("labels: a label with that name already exists")
)

// Label is the catalogue entry and nothing else: who tagged what lives in contact_labels and conversation_labels, owned by contacts/ and inbox/ (struct.md 2).
type Label struct {
	ID                  uuid.UUID
	AccountID           uuid.UUID
	Name                string
	Color               string
	CreatedByOperatorID uuid.UUID
}

// Page carries Total because the pagination decided is by page number, and a page cannot be numbered without knowing how many there are.
type Page struct {
	Items []Label
	Total int
}

// normalizeName only trims: the index folds case with lower(), and lowering the stored value would show the whole catalogue in lowercase.
func normalizeName(name string) string {
	return strings.TrimSpace(name)
}
