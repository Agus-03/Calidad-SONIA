package inbox

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/conversations"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// TestWhoMayReassignAndToWhom is ADR-0056 as a table: supervisor and admin move an open conversation to an active person of the account.
func TestWhoMayReassignAndToWhom(t *testing.T) {
	me, vendor, next, disabled, stranger := id.New(), id.New(), id.New(), id.New(), id.New()

	for _, tc := range []struct {
		name    string
		holder  *uuid.UUID
		role    enums.OperatorRole
		shape   func(*Target)
		to      uuid.UUID
		reason  string
		wantErr error
	}{
		{"a supervisor moves a vendor's", &vendor, enums.OperatorRoleSupervisor, func(*Target) {}, next, "ausencia", nil},
		{"an admin gives one of the pool", nil, enums.OperatorRoleAdmin, func(*Target) {}, next, "reparto", nil},
		{"a waiting one moves too", &vendor, enums.OperatorRoleAdmin,
			func(t *Target) { t.Status = enums.ConversationStatusWaiting }, next, "ausencia", nil},
		{"an operator does not reassign (Anexo B)", &me, enums.OperatorRoleOperator, func(*Target) {}, next, "ausencia", ErrCannotReassign},
		{"a reason is required", &vendor, enums.OperatorRoleSupervisor, func(*Target) {}, next, "   ", ErrNoReason},
		{"a resolved one stays with who attended it", &vendor, enums.OperatorRoleSupervisor,
			func(t *Target) { t.Status = enums.ConversationStatusResolved }, next, "ausencia", ErrResolved},
		{"to the vendor it already has", &vendor, enums.OperatorRoleSupervisor, func(*Target) {}, vendor, "ausencia", ErrAlreadyAssigned},
		{"to a disabled person", &vendor, enums.OperatorRoleSupervisor, func(*Target) {}, disabled, "ausencia", ErrOperatorUnavailable},
		{"to a person of another account", &vendor, enums.OperatorRoleSupervisor, func(*Target) {}, stranger, "ausencia", ErrOperatorNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{people: map[uuid.UUID]bool{me: true, vendor: true, next: true, disabled: false}})
			target := d.open(tc.holder)
			tc.shape(&target)
			d.store.targets[target.ConversationID] = target

			err := d.service.Reassign(context.Background(), scopeOf(me, tc.role), target.ConversationID, tc.to, tc.reason)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Reassign: got %v, want %v", err, tc.wantErr)
			}
			got, written := d.aggregate.reassigned[target.ConversationID]
			if written != (tc.wantErr == nil) {
				t.Fatalf("written = %v, want %v", written, tc.wantErr == nil)
			}
			if !written {
				if len(d.store.assignments) != 0 || len(d.events.told) != 0 {
					t.Fatal("a refused reassignment left history or told the hub")
				}
				return
			}
			if got != tc.to || len(d.store.assignments) != 1 {
				t.Fatalf("moved to %v with %d rows of history, want %v and one row", got, len(d.store.assignments), tc.to)
			}
			row := d.store.assignments[0]
			if row.ToOperatorID != tc.to || row.AssignedByOperatorID == nil || *row.AssignedByOperatorID != me ||
				row.Reason != strings.TrimSpace(tc.reason) || (row.FromOperatorID == nil) != (tc.holder == nil) {
				t.Fatalf("the history row %+v does not say who had it, who has it, who moved it and why", row)
			}
			// Only the holder changes: the bot, the star and the rest are not the reassignment's to touch (F26, ADR-0056).
			if len(d.aggregate.modes) != 0 || len(d.aggregate.starred) != 0 || len(d.aggregate.resolvedBy) != 0 {
				t.Fatal("the reassignment touched more than who holds the conversation")
			}
			want := "reassigned"
			if tc.holder == nil {
				want = "left pool"
			}
			if len(d.events.told) != 1 || d.events.told[0] != want {
				t.Fatalf("told the hub %v, want [%s]", d.events.told, want)
			}
		})
	}
}

// TestAReassignmentThatLostTheRaceWritesNothing: the vendor changed between the read and the write, so nothing is recorded.
func TestAReassignmentThatLostTheRaceWritesNothing(t *testing.T) {
	me, vendor, next := id.New(), id.New(), id.New()
	d := newDesk(&stubStore{people: map[uuid.UUID]bool{next: true}})
	d.aggregate.reassignErr = conversations.ErrTaken
	target := d.open(&vendor)

	err := d.service.Reassign(context.Background(), scopeOf(me, enums.OperatorRoleAdmin), target.ConversationID, next, "ausencia")
	if !errors.Is(err, ErrTaken) {
		t.Fatalf("Reassign: got %v, want ErrTaken", err)
	}
	if len(d.store.assignments) != 0 || len(d.events.told) != 0 {
		t.Fatal("a lost race left history or told the hub")
	}
}

// TestAHandOverMovesEveryOpenConversationOfTheVendor is the absence of ADR-0037 settled: the open ones move, each with its history.
func TestAHandOverMovesEveryOpenConversationOfTheVendor(t *testing.T) {
	me, gone, next := id.New(), id.New(), id.New()

	for _, tc := range []struct {
		name      string
		role      enums.OperatorRole
		from, to  uuid.UUID
		reason    string
		wantErr   error
		wantMoved int
	}{
		{"a disabled vendor's open ones go to another", enums.OperatorRoleSupervisor, gone, next, "baja", nil, 2},
		{"an operator does not hand over", enums.OperatorRoleOperator, gone, next, "baja", ErrCannotReassign, 0},
		{"a reason is required", enums.OperatorRoleAdmin, gone, next, "", ErrNoReason, 0},
		{"from and to are the same", enums.OperatorRoleAdmin, next, next, "baja", ErrSameOperator, 0},
		{"from is not of the account", enums.OperatorRoleAdmin, id.New(), next, "baja", ErrOperatorNotFound, 0},
		{"to cannot work", enums.OperatorRoleAdmin, next, gone, "baja", ErrOperatorUnavailable, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{people: map[uuid.UUID]bool{gone: false, next: true}})
			d.open(&gone)
			d.open(&gone)
			closed := d.open(&gone)
			closed.Status = enums.ConversationStatusResolved
			d.store.targets[closed.ConversationID] = closed

			moved, err := d.service.HandOver(context.Background(), scopeOf(me, tc.role), tc.from, tc.to, tc.reason)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("HandOver: got %v, want %v", err, tc.wantErr)
			}
			if moved != tc.wantMoved || len(d.store.assignments) != tc.wantMoved || len(d.events.told) != tc.wantMoved {
				t.Fatalf("moved %d with %d rows and %d events, want %d of each", moved, len(d.store.assignments), len(d.events.told), tc.wantMoved)
			}
			for _, row := range d.store.assignments {
				if row.FromOperatorID == nil || *row.FromOperatorID != gone || row.ToOperatorID != next || row.ConversationID == closed.ConversationID {
					t.Fatalf("the history row %+v is not an open conversation moved from %v to %v", row, gone, next)
				}
			}
		})
	}
}

// TestTheHistoryHasTheReachOfTheThread: whoever reads the conversation reads who held it, and another's does not exist for an operator.
func TestTheHistoryHasTheReachOfTheThread(t *testing.T) {
	me, other := id.New(), id.New()
	d := newDesk(&stubStore{})
	mine, theirs := d.open(&me), d.open(&other)
	d.store.history = map[uuid.UUID][]AssignmentEntry{mine.ConversationID: {{ID: id.New(), To: Operator{ID: me}, Reason: "reparto"}}}

	got, err := d.service.Assignments(context.Background(), scopeOf(me, enums.OperatorRoleOperator), mine.ConversationID)
	if err != nil || len(got) != 1 {
		t.Fatalf("Assignments of its own: got %v and %v, want one row", got, err)
	}
	if _, err := d.service.Assignments(context.Background(), scopeOf(me, enums.OperatorRoleOperator), theirs.ConversationID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Assignments of another's: got %v, want ErrNotFound", err)
	}
}

func postJSON(engine http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer cualquier-cosa")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// TestOnlyTheRolesAboveReachTheReassignRoutes walks both routes with the three roles, because a wrong Roles list compiles (Anexo B).
func TestOnlyTheRolesAboveReachTheReassignRoutes(t *testing.T) {
	next := id.New()
	for _, tc := range []struct {
		role enums.OperatorRole
		want int
	}{
		{enums.OperatorRoleOperator, http.StatusForbidden},
		{enums.OperatorRoleSupervisor, http.StatusOK},
		{enums.OperatorRoleAdmin, http.StatusOK},
	} {
		t.Run(string(tc.role), func(t *testing.T) {
			d := newDesk(&stubStore{people: map[uuid.UUID]bool{otherOperator: true, next: true}})
			target := d.open(&otherOperator)

			rec := postJSON(mountDesk(tc.role, d), http.MethodPost, "/conversations/"+target.ConversationID.String()+"/reassign",
				`{"to_operator_id":"`+next.String()+`","reason":"ausencia"}`)
			if rec.Code != tc.want {
				t.Fatalf("reassign as %s: got %d, want %d - body %s", tc.role, rec.Code, tc.want, rec.Body)
			}
			rec = postJSON(mountDesk(tc.role, d), http.MethodPost, "/inbox/handover",
				`{"from_operator_id":"`+otherOperator.String()+`","to_operator_id":"`+next.String()+`","reason":"ausencia"}`)
			if rec.Code != tc.want {
				t.Fatalf("handover as %s: got %d, want %d - body %s", tc.role, rec.Code, tc.want, rec.Body)
			}
		})
	}
}

// TestEachReassignRefusalHasItsOwnCode is what the frontend switches on when a conversation does not move (ADR-0056).
func TestEachReassignRefusalHasItsOwnCode(t *testing.T) {
	next, disabled := id.New(), id.New()
	body := func(to string) string { return `{"to_operator_id":"` + to + `","reason":"ausencia"}` }

	for _, tc := range []struct {
		name     string
		shape    func(*desk) string
		body     string
		wantCode int
		want     string
	}{
		{"the id is not a uuid", func(*desk) string { return "no-es-un-id" }, body(next.String()), http.StatusBadRequest, "invalid_conversation_id"},
		{"the body has no destination", func(d *desk) string { return d.open(nil).ConversationID.String() }, `{}`, http.StatusBadRequest, "invalid_body"},
		{"the destination is not a uuid", func(d *desk) string { return d.open(nil).ConversationID.String() }, body("x"), http.StatusBadRequest, "invalid_to_operator_id"},
		{"there is no reason", func(d *desk) string { return d.open(nil).ConversationID.String() },
			`{"to_operator_id":"` + next.String() + `"}`, http.StatusBadRequest, "invalid_reason"},
		{"the conversation does not exist", func(*desk) string { return id.New().String() }, body(next.String()), http.StatusNotFound, "conversation_not_found"},
		{"the destination is of another account", func(d *desk) string { return d.open(nil).ConversationID.String() },
			body(id.New().String()), http.StatusNotFound, "operator_not_found"},
		{"the destination is disabled", func(d *desk) string { return d.open(nil).ConversationID.String() },
			body(disabled.String()), http.StatusConflict, "operator_unavailable"},
		{"the destination already holds it", func(d *desk) string { return d.open(&next).ConversationID.String() },
			body(next.String()), http.StatusConflict, "already_assigned"},
		{"the conversation is resolved", func(d *desk) string { return closedBy(d, &next) }, body(id.New().String()), http.StatusConflict, "conversation_resolved"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{people: map[uuid.UUID]bool{next: true, disabled: false}})
			rec := postJSON(mountDesk(enums.OperatorRoleAdmin, d), http.MethodPost, "/conversations/"+tc.shape(d)+"/reassign", tc.body)
			if rec.Code != tc.wantCode {
				t.Fatalf("got %d, want %d - body %s", rec.Code, tc.wantCode, rec.Body)
			}
			if got := errorCode(t, rec); got != tc.want {
				t.Fatalf("code %q, want %q", got, tc.want)
			}
		})
	}
}

// TestEachHandOverRefusalHasItsOwnCode names which of the two ids was wrong, because the frontend fills both from one form.
func TestEachHandOverRefusalHasItsOwnCode(t *testing.T) {
	next := id.New()
	for _, tc := range []struct {
		name     string
		body     string
		wantCode int
		want     string
	}{
		{"the body is not JSON", `no`, http.StatusBadRequest, "invalid_body"},
		{"from is not a uuid", `{"from_operator_id":"x","to_operator_id":"` + next.String() + `","reason":"r"}`, http.StatusBadRequest, "invalid_from_operator_id"},
		{"to is not a uuid", `{"from_operator_id":"` + next.String() + `","to_operator_id":"x","reason":"r"}`, http.StatusBadRequest, "invalid_to_operator_id"},
		{"the same person twice", `{"from_operator_id":"` + next.String() + `","to_operator_id":"` + next.String() + `","reason":"r"}`,
			http.StatusBadRequest, "same_operator"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{people: map[uuid.UUID]bool{next: true}})
			rec := postJSON(mountDesk(enums.OperatorRoleAdmin, d), http.MethodPost, "/inbox/handover", tc.body)
			if rec.Code != tc.wantCode {
				t.Fatalf("got %d, want %d - body %s", rec.Code, tc.wantCode, rec.Body)
			}
			if got := errorCode(t, rec); got != tc.want {
				t.Fatalf("code %q, want %q", got, tc.want)
			}
		})
	}
}

// TestTheHistoryRouteNamesThePeople: the frontend draws the history without a second request for each name (ADR-0056).
func TestTheHistoryRouteNamesThePeople(t *testing.T) {
	d := newDesk(&stubStore{})
	target := d.open(&testOperator)
	d.store.history = map[uuid.UUID][]AssignmentEntry{target.ConversationID: {
		{ID: id.New(), To: Operator{ID: testOperator, Name: "Ana", Lastname: "Paz"}, Reason: "auto_assigned_on_first_reply"},
	}}

	rec := postJSON(mountDesk(enums.OperatorRoleOperator, d), http.MethodGet, "/conversations/"+target.ConversationID.String()+"/assignments", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"Ana"`) || !strings.Contains(rec.Body.String(), `"from":null`) {
		t.Fatalf("got %d - body %s, want the row with its names and a null origin", rec.Code, rec.Body)
	}
}

// TestReachableIsTheReachOfTheThreadOnAnyStatus is the contract notes/ and reasoning/ rest on: the holder or the pool for an operator,
// any conversation for the roles above, a resolved one too, and nothing of another vendor (ADR-0035, regla 2; ADR-0063).
func TestReachableIsTheReachOfTheThreadOnAnyStatus(t *testing.T) {
	me, other := id.New(), id.New()

	for _, tc := range []struct {
		name    string
		holder  *uuid.UUID
		role    enums.OperatorRole
		status  enums.ConversationStatus
		wantErr error
	}{
		{"the vendor reaches its own", &me, enums.OperatorRoleOperator, enums.ConversationStatusActive, nil},
		{"an operator reaches the pool", nil, enums.OperatorRoleOperator, enums.ConversationStatusActive, nil},
		{"a supervisor reaches a vendor's", &other, enums.OperatorRoleSupervisor, enums.ConversationStatusActive, nil},
		{"a resolved one is reached too", &me, enums.OperatorRoleOperator, enums.ConversationStatusResolved, nil},
		{"another's does not exist for an operator", &other, enums.OperatorRoleOperator, enums.ConversationStatusActive, ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			target := d.open(tc.holder)
			target.Status = tc.status
			d.store.targets[target.ConversationID] = target

			if err := d.service.Reachable(context.Background(), scopeOf(me, tc.role), target.ConversationID); !errors.Is(err, tc.wantErr) {
				t.Fatalf("Reachable: got %v, want %v", err, tc.wantErr)
			}
		})
	}
	d := newDesk(&stubStore{})
	if err := d.service.Reachable(context.Background(), scopeOf(me, enums.OperatorRoleAdmin), id.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a conversation that does not exist: got %v, want ErrNotFound", err)
	}
}
