package inbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// rowsAt builds n conversations one minute apart, newest first, all in the pool so every role reaches them.
func rowsAt(n int) []Summary {
	start := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	rows := make([]Summary, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, Summary{ID: id.New(), LastActivityAt: start.Add(-time.Duration(i) * time.Minute)})
	}
	return rows
}

var adminScope = Scope{AccountID: id.New(), OperatorID: id.New(), Role: enums.OperatorRoleAdmin}

// TestTheNextCursorIsHandedOutOnlyWhenAnotherPageExists protects the one row read past the page: without it a full last page
// hands out a cursor to nothing, and the frontend asks for an empty page to learn that it is done (ADR-0036, regla 7).
func TestTheNextCursorIsHandedOutOnlyWhenAnotherPageExists(t *testing.T) {
	for _, tc := range []struct {
		name     string
		stored   int
		limit    int
		wantRows int
		wantNext bool
	}{
		{"more rows than the page holds: the cursor names the last row shown", 5, 2, 2, true},
		{"exactly a page: the extra row is not there, so there is no next page", 2, 2, 2, false},
		{"less than a page: no next page", 1, 2, 1, false},
		{"nothing at all: an empty page and no cursor", 0, 2, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &stubStore{rows: rowsAt(tc.stored)}

			page, err := NewService(Options{Repo: store}).List(context.Background(), adminScope, Filter{}, nil, tc.limit)
			if err != nil {
				t.Fatalf("List: got %v, want no error", err)
			}
			if store.lastLimit != tc.limit+1 {
				t.Fatalf("the store was asked for %d rows, want %d: one past the page", store.lastLimit, tc.limit+1)
			}
			if len(page.Items) != tc.wantRows {
				t.Fatalf("the page holds %d rows, want %d", len(page.Items), tc.wantRows)
			}
			if (page.Next != nil) != tc.wantNext {
				t.Fatalf("next cursor present = %v, want %v", page.Next != nil, tc.wantNext)
			}
			if tc.wantNext {
				last := page.Items[len(page.Items)-1]
				if page.Next.ID != last.ID || !page.Next.At.Equal(last.LastActivityAt) {
					t.Fatalf("the cursor names %+v, want the last row shown, %v", page.Next, last.ID)
				}
			}
		})
	}
}

// TestAPageOfZeroIsReadAsAPageOfOne keeps a caller that forgot the limit from indexing the row before the first one.
func TestAPageOfZeroIsReadAsAPageOfOne(t *testing.T) {
	store := &stubStore{rows: rowsAt(3)}

	page, err := NewService(Options{Repo: store}).List(context.Background(), adminScope, Filter{}, nil, 0)
	if err != nil {
		t.Fatalf("List: got %v, want no error", err)
	}
	if len(page.Items) != 1 || page.Next == nil {
		t.Fatalf("got %d rows and next %v, want one row and a cursor", len(page.Items), page.Next)
	}
}

// TestTheListingDoesNotSwallowAFailure separates an empty inbox from a database that did not answer.
func TestTheListingDoesNotSwallowAFailure(t *testing.T) {
	boom := errors.New("connection refused")

	_, err := NewService(Options{Repo: &stubStore{err: boom}}).List(context.Background(), adminScope, Filter{}, nil, 20)
	if !errors.Is(err, boom) {
		t.Fatalf("List: got %v, want the underlying failure", err)
	}
}

func scopeOf(operatorID uuid.UUID, role enums.OperatorRole) Scope {
	return Scope{AccountID: id.New(), OperatorID: operatorID, Role: role}
}

// TestTheThreadHasTheReachOfTheListing is the listing's rule one conversation at a time: an operator reads its own and the pool, the
// two roles above it read the whole team, and an id guessed past the listing is answered as missing (ADR-0035, reglas 2 y 3).
func TestTheThreadHasTheReachOfTheListing(t *testing.T) {
	me, other := id.New(), id.New()

	for _, tc := range []struct {
		name    string
		holder  *uuid.UUID
		role    enums.OperatorRole
		wantErr error
	}{
		{"an operator reads its own conversation", &me, enums.OperatorRoleOperator, nil},
		{"an operator reads the pool, which it has to read before taking it", nil, enums.OperatorRoleOperator, nil},
		{"an operator is told another's conversation does not exist", &other, enums.OperatorRoleOperator, ErrNotFound},
		{"a supervisor reads the whole team (Anexo B)", &other, enums.OperatorRoleSupervisor, nil},
		{"an admin reads the whole team", &other, enums.OperatorRoleAdmin, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			target := d.open(tc.holder)
			want := d.write(target.ConversationID, 2)

			page, err := d.service.Thread(context.Background(), scopeOf(me, tc.role), target.ConversationID, nil, 20)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Thread: got %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			if len(page.Items) != 2 || page.Items[0].ID != want[0].ID || page.Next != nil {
				t.Fatalf("got %d messages, next %v, want the two newest first and no cursor", len(page.Items), page.Next)
			}
		})
	}
}

// TestTheThreadLoadsWhatCameBeforeOnePageAtATime walks the cursor through the service: newest first, each page after the last one
// shown, and no cursor once the oldest message is on screen.
func TestTheThreadLoadsWhatCameBeforeOnePageAtATime(t *testing.T) {
	d := newDesk(&stubStore{})
	target := d.open(nil)
	want := d.write(target.ConversationID, 5)
	scope := scopeOf(id.New(), enums.OperatorRoleOperator)

	seen := []uuid.UUID{}
	var after *Cursor
	for pages := 0; pages < 5; pages++ {
		page, err := d.service.Thread(context.Background(), scope, target.ConversationID, after, 2)
		if err != nil {
			t.Fatalf("Thread: %v", err)
		}
		if d.store.lastLimit != 3 {
			t.Fatalf("the store was asked for %d messages, want one past the page", d.store.lastLimit)
		}
		for _, m := range page.Items {
			seen = append(seen, m.ID)
		}
		if page.Next == nil {
			break
		}
		after = page.Next
	}

	if len(seen) != len(want) {
		t.Fatalf("walked %d messages, want %d", len(seen), len(want))
	}
	for i := range want {
		if seen[i] != want[i].ID {
			t.Fatalf("message %d is %v, want %v: newest first, nothing repeated", i, seen[i], want[i].ID)
		}
	}

	if page, err := d.service.Thread(context.Background(), scope, target.ConversationID, nil, 0); err != nil || len(page.Items) != 1 {
		t.Fatalf("a page of zero: got %d messages and %v, want one", len(page.Items), err)
	}
}

// TestTheThreadDoesNotSwallowAFailure separates an empty thread from a database that did not answer.
func TestTheThreadDoesNotSwallowAFailure(t *testing.T) {
	boom := errors.New("connection refused")

	d := newDesk(&stubStore{})
	target := d.open(nil)
	d.store.threadErr = boom
	if _, err := d.service.Thread(context.Background(), scopeOf(id.New(), enums.OperatorRoleAdmin), target.ConversationID, nil, 20); !errors.Is(err, boom) {
		t.Fatalf("Thread: got %v, want the underlying failure", err)
	}

	d = newDesk(&stubStore{})
	if _, err := d.service.Thread(context.Background(), scopeOf(id.New(), enums.OperatorRoleAdmin), id.New(), nil, 20); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Thread of a conversation that does not exist: got %v, want ErrNotFound", err)
	}
}
