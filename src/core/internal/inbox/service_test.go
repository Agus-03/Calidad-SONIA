package inbox

import (
	"context"
	"errors"
	"testing"
	"time"

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
// hands out a cursor to nothing, and the frontend asks for an empty page to learn that it is done.
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

			page, err := NewService(store, nil).List(context.Background(), adminScope, nil, tc.limit)
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
				if page.Next.ID != last.ID || !page.Next.LastActivityAt.Equal(last.LastActivityAt) {
					t.Fatalf("the cursor names %+v, want the last row shown, %v", page.Next, last.ID)
				}
			}
		})
	}
}

// TestAPageOfZeroIsReadAsAPageOfOne keeps a caller that forgot the limit from indexing the row before the first one.
func TestAPageOfZeroIsReadAsAPageOfOne(t *testing.T) {
	store := &stubStore{rows: rowsAt(3)}

	page, err := NewService(store, nil).List(context.Background(), adminScope, nil, 0)
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

	_, err := NewService(&stubStore{err: boom}, nil).List(context.Background(), adminScope, nil, 20)
	if !errors.Is(err, boom) {
		t.Fatalf("List: got %v, want the underlying failure", err)
	}
}
