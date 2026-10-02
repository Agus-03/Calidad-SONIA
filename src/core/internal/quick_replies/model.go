// Package quickreplies owns the quick_replies table: the texts the admin loads and the operators pick from the chat (F09, ADR-0060).
package quickreplies

import (
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

var (
	// ErrNotFound covers a reply of another account too, so an id guessed across accounts reads as missing (ADR-0012).
	ErrNotFound = errors.New("quick_replies: quick reply not found")

	// ErrDuplicateTitle names what the unique index rejects, so the admin is told which title is taken (ADR-0060).
	ErrDuplicateTitle = errors.New("quick_replies: a quick reply with that title already exists")

	// ErrEmptyTitle and ErrEmptyContent are refused before the database, because blanks pass a NOT NULL.
	ErrEmptyTitle   = errors.New("quick_replies: the title is empty")
	ErrEmptyContent = errors.New("quick_replies: the content is empty")
)

// Scope is who asks: the account bounds every read, and the role decides whether the paused replies are listed (ADR-0060).
type Scope struct {
	AccountID  uuid.UUID
	OperatorID uuid.UUID
	Role       enums.OperatorRole
}

type QuickReply struct {
	ID                  uuid.UUID
	AccountID           uuid.UUID
	Title               string
	Content             string
	Active              bool
	CreatedByOperatorID uuid.UUID
}

// Page carries Total because a page number cannot tell whether there is a next one without it, as in labels/.
type Page struct {
	Items []QuickReply
	Total int
}

// Only the title is trimmed: the index folds case with lower(), and the content keeps its line breaks as the admin wrote them.
func normalizeTitle(title string) string {
	return strings.TrimSpace(title)
}
