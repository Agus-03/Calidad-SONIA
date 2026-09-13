package inbox

import (
	"bytes"
	"context"
	"sort"
)

// stubStore answers from memory, so the service and the handlers are tested against their own rules and not against the SQL.
//
// It carries the reach and the keyset on purpose: the handlers rest on both, so a double that ignored them would let a case pass
// while the real query leaked the conversations of another operator or repeated a row across pages.
type stubStore struct {
	rows      []Summary
	err       error
	lastLimit int
}

func (s *stubStore) reaches(scope Scope, row Summary) bool {
	if scope.seesTheWholeTeam() {
		return true
	}
	return row.Operator == nil || row.Operator.ID == scope.OperatorID
}

// before is the ORDER BY of the query: newest activity first, and the id when two share the instant.
func before(a, b Summary) bool {
	if !a.LastActivityAt.Equal(b.LastActivityAt) {
		return a.LastActivityAt.After(b.LastActivityAt)
	}
	return bytes.Compare(a.ID[:], b.ID[:]) > 0
}

func (s *stubStore) List(_ context.Context, scope Scope, after *Cursor, limit int) ([]Summary, error) {
	s.lastLimit = limit
	if s.err != nil {
		return nil, s.err
	}

	reached := []Summary{}
	for _, row := range s.rows {
		if s.reaches(scope, row) {
			reached = append(reached, row)
		}
	}
	sort.Slice(reached, func(i, j int) bool { return before(reached[i], reached[j]) })

	out := []Summary{}
	for _, row := range reached {
		if after != nil && !before(Summary{LastActivityAt: after.LastActivityAt, ID: after.ID}, row) {
			continue
		}
		if len(out) == limit {
			break
		}
		out = append(out, row)
	}
	return out, nil
}
