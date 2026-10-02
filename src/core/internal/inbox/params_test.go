package inbox

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// TestAQueryTheListingCannotHonourIsRefused names each malformed input: guessing a cursor would answer the first page again and the
// frontend would draw it as the next one (ADR-0036, regla 5).
func TestAQueryTheListingCannotHonourIsRefused(t *testing.T) {
	encoded := func(raw string) string { return base64.RawURLEncoding.EncodeToString([]byte(raw)) }

	for _, tc := range []struct {
		name     string
		query    url.Values
		wantCode string
	}{
		{"a cursor that is not base64", url.Values{"cursor": {"no es base64!"}}, "invalid_cursor"},
		{"a cursor with no separator", url.Values{"cursor": {encoded("sin-separador")}}, "invalid_cursor"},
		{"a cursor whose instant is not one", url.Values{"cursor": {encoded("ayer|" + id.New().String())}}, "invalid_cursor"},
		{"a cursor whose id is not a uuid", url.Values{"cursor": {encoded(time.Now().Format(time.RFC3339Nano) + "|nada")}}, "invalid_cursor"},
		{"a limit of zero", url.Values{"limit": {"0"}}, "invalid_limit"},
		{"a negative limit", url.Values{"limit": {"-3"}}, "invalid_limit"},
		{"a limit that is not a number", url.Values{"limit": {"veinte"}}, "invalid_limit"},
		// A flag is on only with true: "1" read as off would draw the whole inbox as the filtered one (ADR-0050).
		{"a flag that is not true", url.Values{"unread": {"1"}}, "invalid_filter"},
		{"false is not how a flag is turned off", url.Values{"starred": {"false"}}, "invalid_filter"},
		{"a status that is not one of the three", url.Values{"status": {"closed"}}, "invalid_status"},
		{"a label id that is not a uuid", url.Values{"label_id": {"ventas"}}, "invalid_label_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, _, _, _ := team()
			rec := get(mount(enums.OperatorRoleAdmin, store), tc.query)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("got %d, want 400 - body %s", rec.Code, rec.Body)
			}
			var body struct {
				Error struct{ Code string } `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error.Code != tc.wantCode {
				t.Fatalf("code %q, want %q - body %s", body.Error.Code, tc.wantCode, rec.Body)
			}
		})
	}
}

// TestALimitAboveTheCeilingIsClampedNotRefused matches per_page in contacts/: asking for too many is not worth failing the page over
// (ADR-0036, regla 8).
func TestALimitAboveTheCeilingIsClampedNotRefused(t *testing.T) {
	store, _, _, _ := team()

	decode(t, get(mount(enums.OperatorRoleAdmin, store), url.Values{"limit": {"100000"}}))
	if store.lastLimit != maxLimit+1 {
		t.Fatalf("the store was asked for %d rows, want the ceiling plus the one past it, %d", store.lastLimit, maxLimit+1)
	}
}

// filtered is one conversation for each filter of F23 and one that matches none, newest first, so each case can name what it expects.
type filtered struct {
	store                                        *stubStore
	unread, assigned, starred, waiting, resolved Summary
	labelled, reminded, bare                     Summary
	label                                        uuid.UUID
}

func filteredInbox() filtered {
	at := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	row := func(minutes int) Summary {
		return Summary{ID: id.New(), Status: enums.ConversationStatusActive, LastActivityAt: at.Add(-time.Duration(minutes) * time.Minute)}
	}
	f := filtered{
		unread: row(0), assigned: row(1), starred: row(2), waiting: row(3),
		resolved: row(4), labelled: row(5), reminded: row(6), bare: row(7),
		label: id.New(),
	}
	f.assigned.Operator = &Operator{ID: testOperator, Name: "Ana", Lastname: "Propia"}
	f.starred.Starred = true
	f.waiting.Status = enums.ConversationStatusWaiting
	f.resolved.Status = enums.ConversationStatusResolved
	f.store = &stubStore{
		rows:      []Summary{f.bare, f.reminded, f.labelled, f.resolved, f.waiting, f.starred, f.assigned, f.unread},
		unread:    map[uuid.UUID]bool{f.unread.ID: true},
		labels:    map[uuid.UUID][]uuid.UUID{f.labelled.ID: {id.New(), f.label}},
		reminders: map[uuid.UUID]bool{f.reminded.ID: true},
	}
	return f
}

// TestEachFilterOfF23NarrowsTheInboxToItsOwn walks the seven filters one by one through the real router: each one answers the
// conversation it names and nothing else, so no filter is silently ignored or read as another.
func TestEachFilterOfF23NarrowsTheInboxToItsOwn(t *testing.T) {
	f := filteredInbox()

	for _, tc := range []struct {
		name  string
		query url.Values
		want  Summary
	}{
		{"no leídas", url.Values{"unread": {"true"}}, f.unread},
		{"asignadas", url.Values{"assigned": {"true"}}, f.assigned},
		{"destacadas (F25)", url.Values{"starred": {"true"}}, f.starred},
		{"en espera", url.Values{"status": {"waiting"}}, f.waiting},
		{"resueltas", url.Values{"status": {"resolved"}}, f.resolved},
		{"por etiqueta", url.Values{"label_id": {f.label.String()}}, f.labelled},
		{"con recordatorio", url.Values{"reminder": {"true"}}, f.reminded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ids(decode(t, get(mount(enums.OperatorRoleAdmin, f.store), tc.query)))
			if len(got) != 1 || got[0] != tc.want.ID.String() {
				t.Fatalf("the filtered inbox holds %v, want only %v", got, tc.want.ID)
			}
		})
	}

	if got := ids(decode(t, get(mount(enums.OperatorRoleAdmin, f.store), nil))); len(got) != 8 {
		t.Fatalf("with no filter the inbox holds %d conversations, want all 8", len(got))
	}
}

// TestTheFiltersCombineAndNeverWidenTheReach is "combinables" read as AND, applied inside what the role already sees: an operator
// asking for the assigned ones gets its own and never another's (ADR-0035, regla 1; ADR-0050).
func TestTheFiltersCombineAndNeverWidenTheReach(t *testing.T) {
	at := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	mineStarred := Summary{ID: id.New(), Starred: true, LastActivityAt: at, Operator: &Operator{ID: testOperator}}
	mineWaiting := Summary{ID: id.New(), Status: enums.ConversationStatusWaiting, Starred: true, LastActivityAt: at.Add(-time.Minute), Operator: &Operator{ID: testOperator}}
	theirsStarred := Summary{ID: id.New(), Starred: true, LastActivityAt: at.Add(-2 * time.Minute), Operator: &Operator{ID: otherOperator}}
	pooledStarred := Summary{ID: id.New(), Starred: true, LastActivityAt: at.Add(-3 * time.Minute)}
	store := &stubStore{rows: []Summary{mineStarred, mineWaiting, theirsStarred, pooledStarred}}

	for _, tc := range []struct {
		name  string
		role  enums.OperatorRole
		query url.Values
		want  []Summary
	}{
		{"an operator's assigned ones are its own", enums.OperatorRoleOperator, url.Values{"assigned": {"true"}}, []Summary{mineStarred, mineWaiting}},
		{"an operator's starred ones stay inside its reach", enums.OperatorRoleOperator, url.Values{"starred": {"true"}}, []Summary{mineStarred, mineWaiting, pooledStarred}},
		{"two filters are both required", enums.OperatorRoleOperator, url.Values{"starred": {"true"}, "status": {"waiting"}}, []Summary{mineWaiting}},
		{"a supervisor's assigned ones are the whole team's", enums.OperatorRoleSupervisor, url.Values{"assigned": {"true"}}, []Summary{mineStarred, mineWaiting, theirsStarred}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ids(decode(t, get(mount(tc.role, store), tc.query)))
			if len(got) != len(tc.want) {
				t.Fatalf("the inbox holds %v, want %d conversations", got, len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i].ID.String() {
					t.Fatalf("the inbox holds %v, want %v in that order", got, tc.want[i].ID)
				}
			}
		})
	}
}

// TestTheCursorContinuesAFilteredInbox is the rule that closed ADR-0036's open question: no filter changes the order, so the same
// cursor walks a filtered listing, as long as the frontend keeps sending the same filter with it (ADR-0050).
func TestTheCursorContinuesAFilteredInbox(t *testing.T) {
	f := filteredInbox()
	f.store.rows[0].Starred, f.store.rows[2].Starred = true, true // the bare and the labelled one join the starred one
	engine := mount(enums.OperatorRoleAdmin, f.store)

	seen := []string{}
	query := url.Values{"limit": {"1"}, "starred": {"true"}}
	for pages := 0; ; pages++ {
		if pages > 3 {
			t.Fatalf("still walking after %d pages: the cursor never ends", pages)
		}
		page := decode(t, get(engine, query))
		seen = append(seen, ids(page)...)
		if page.NextCursor == nil {
			break
		}
		query.Set("cursor", *page.NextCursor)
	}

	want := []string{f.starred.ID.String(), f.labelled.ID.String(), f.bare.ID.String()}
	if len(seen) != len(want) {
		t.Fatalf("walked %v, want %v", seen, want)
	}
	for i := range seen {
		if seen[i] != want[i] {
			t.Fatalf("walked %v, want %v", seen, want)
		}
	}
}
