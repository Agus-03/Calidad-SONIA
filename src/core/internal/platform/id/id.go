// Package id generates primary keys: UUID v7, in the application (C04).
package id

import (
	"fmt"

	"github.com/google/uuid"
)

// New returns a fresh UUIDv7 and panics on failure: a process that cannot generate an id cannot serve.
func New() uuid.UUID {
	v, err := uuid.NewV7()
	if err != nil {
		panic(fmt.Sprintf("id: cannot generate uuid v7: %v", err))
	}
	return v
}

// Parse validates an identifier that comes from outside before it reaches a query.
func Parse(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}
