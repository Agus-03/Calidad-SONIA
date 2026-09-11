package conversations

import "fmt"

// ScopedToOperator renders the condition that answers "does this operator have a conversation with that contact assigned right now".
//
// It lives in this package and not in the one that asks, because it names this package's table: whoever composes it never learns
// the shape of conversations, which is the same reason a write goes through the repository that owns the table (regla 1).
//
// contactColumn must be qualified — "contacts.id" and not "id" — because conversations has an id of its own and an unqualified
// name would resolve inside the subquery, silently comparing the conversation to itself and matching nothing.
//
// The operator id takes the placeholder at paramIndex, which the caller owns because only it knows how many arguments came first.
func ScopedToOperator(contactColumn string, paramIndex int) string {
	return fmt.Sprintf(
		`EXISTS (SELECT 1 FROM conversations assigned
		         WHERE assigned.contact_id = %s AND assigned.operator_id = $%d AND assigned.deleted_at IS NULL)`,
		contactColumn, paramIndex)
}
